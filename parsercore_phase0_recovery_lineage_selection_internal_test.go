//go:build !gts_no_parsercorephase0

package gotreesitter

import (
	"testing"

	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

// selectCompetingRecoveryLineage is the acceptance half of C's version
// competition: when more than one head accepts, price the competitors and
// publish the cheaper tree instead of declining. These tests drive it through
// a hand-built scheduler, because no live path forks a recovery lineage yet.

type lineageSelectionTable struct{}

func (lineageSelectionTable) Actions(core.StateID, core.Symbol) (core.ActionRow, error) {
	return core.ActionRow{}, nil
}
func (lineageSelectionTable) Goto(core.StateID, core.Symbol) (core.StateID, error) { return 0, nil }
func (lineageSelectionTable) ProductionFields(uint16, int) ([]core.FieldMapEntry, error) {
	return nil, nil
}
func (lineageSelectionTable) ProductionAliases(uint16, int) ([]core.Symbol, error) { return nil, nil }

// newLineageSelectionScheduler builds a scheduler carrying two competing
// accepted heads: one that inserted a MISSING leaf, one that absorbed a span
// into an ERROR region. The spans reproduce the measured php witness, where
// absorbing nine bytes with one visible child costs 609 against the
// insertion's flat 610.
func newLineageSelectionScheduler(t *testing.T, armed bool) *diagnosticParserCoreGenericScheduler {
	t.Helper()
	compact, err := core.New(lineageSelectionTable{}, core.Limits{MaxDerivations: 8, MaxPopPaths: 8})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := compact.Seed(core.StateID(1), 0)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	missingHead, err := compact.ShiftMissingLeaf(seed, core.StateID(2), core.Symbol(3), 16)
	if err != nil {
		t.Fatalf("ShiftMissingLeaf: %v", err)
	}
	absorbed, err := compact.ErrorRegionLeaf(core.Symbol(4), 6, 15, false)
	if err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}
	absorbHead, err := compact.ErrorRegionResume(seed, core.StateID(3), 6, 15, []core.SubtreeID{absorbed})
	if err != nil {
		t.Fatalf("ErrorRegionResume: %v", err)
	}

	lang := &Language{SymbolMetadata: make([]SymbolMetadata, 16)}
	for i := range lang.SymbolMetadata {
		lang.SymbolMetadata[i] = SymbolMetadata{Visible: true, Named: true}
	}
	scheduler := &diagnosticParserCoreGenericScheduler{compact: compact}
	scheduler.tokenSource = &dfaTokenSource{language: lang}
	scheduler.options.materializationSource = []byte("<?php namespace ; ?>")
	scheduler.options.allowCompactRecoveryLineageSelection = armed
	scheduler.headers = make([]diagnosticParserCoreHeader, 2)
	scheduler.headers[0].head = missingHead
	scheduler.headers[1].head = absorbHead
	for index := range scheduler.headers {
		if err := scheduler.headers[index].markRecoveryLineage(0); err != nil {
			t.Fatalf("markRecoveryLineage: %v", err)
		}
	}
	return scheduler
}

// TestSelectCompetingRecoveryLineagePublishesTheCheaperTree is the claim: on
// the php witness C keeps the ERROR tree, and so must this.
func TestSelectCompetingRecoveryLineagePublishesTheCheaperTree(t *testing.T) {
	scheduler := newLineageSelectionScheduler(t, true)
	winner, resolved, err := scheduler.selectCompetingRecoveryLineage()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if !resolved {
		t.Fatal("two priceable recovery lineages were not resolved")
	}
	if winner != 1 {
		t.Fatalf("winner=%d, want 1: the 609 absorb beats the 610 insertion", winner)
	}
}

// TestSelectCompetingRecoveryLineageDeclinesUnarmed proves the capability
// actually gates the path, which is what keeps this unreachable today.
func TestSelectCompetingRecoveryLineageDeclinesUnarmed(t *testing.T) {
	scheduler := newLineageSelectionScheduler(t, false)
	_, resolved, err := scheduler.selectCompetingRecoveryLineage()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if resolved {
		t.Fatal("an unarmed scheduler resolved a competing frontier")
	}
}

// TestSelectCompetingRecoveryLineageDeclinesNonRecoveryHead proves the route
// refuses ordinary grammar ambiguity. Error cost answers "which recovery is
// cheaper", which is not the question a plain ambiguous frontier is asking, so
// pricing one would silently substitute a different decision procedure.
func TestSelectCompetingRecoveryLineageDeclinesNonRecoveryHead(t *testing.T) {
	scheduler := newLineageSelectionScheduler(t, true)
	scheduler.headers[1].recoveryCompetition = 0
	_, resolved, err := scheduler.selectCompetingRecoveryLineage()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if resolved {
		t.Fatal("a frontier containing a non-recovery head was resolved by error cost")
	}
}

// TestSelectCompetingRecoveryLineageChargesOpenSegments proves the header's
// open-recovery count reaches pricing. Charging the missing lineage nothing
// and the absorbing lineage one paused segment inverts the winner, because the
// margin between them is a single point.
func TestSelectCompetingRecoveryLineageChargesOpenSegments(t *testing.T) {
	scheduler := newLineageSelectionScheduler(t, true)
	if err := scheduler.headers[1].markRecoveryLineage(1); err != nil {
		t.Fatalf("markRecoveryLineage: %v", err)
	}

	winner, resolved, err := scheduler.selectCompetingRecoveryLineage()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if !resolved {
		t.Fatal("frontier was not resolved")
	}
	if winner != 0 {
		t.Fatalf("winner=%d, want 0: charging the absorb one paused segment (+500) puts it above the insertion", winner)
	}
}

// TestSelectCompetingRecoveryLineageDeclinesWithoutSource proves pricing
// refuses to run without the source text. Rows are read from it, and a missing
// source would silently price every ERROR region at zero rows.
func TestSelectCompetingRecoveryLineageDeclinesWithoutSource(t *testing.T) {
	scheduler := newLineageSelectionScheduler(t, true)
	scheduler.options.materializationSource = nil
	_, resolved, err := scheduler.selectCompetingRecoveryLineage()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if resolved {
		t.Fatal("pricing ran without a source text")
	}
}

// TestSelectCompetingRecoveryLineageDeclinesSingleHead proves the helper only
// speaks to genuinely competing frontiers; a sole head takes the ordinary
// path.
func TestSelectCompetingRecoveryLineageDeclinesSingleHead(t *testing.T) {
	scheduler := newLineageSelectionScheduler(t, true)
	scheduler.headers = scheduler.headers[:1]
	_, resolved, err := scheduler.selectCompetingRecoveryLineage()
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if resolved {
		t.Fatal("a single-head frontier was routed through competition")
	}
}

// TestRecoveryLineageMarkerPacksIntoOneByte pins the encoding, because it is
// the reason the header did not grow. Two plain fields would push s3Region
// past its alignment and take the header from 224 to 232 bytes, which two
// separate size ratchets pin.
func TestRecoveryLineageMarkerPacksIntoOneByte(t *testing.T) {
	var header diagnosticParserCoreHeader
	if header.isRecoveryLineage() {
		t.Fatal("a zero header reported itself as a recovery lineage")
	}
	if got := header.recoveryOpenSegments(); got != 0 {
		t.Fatalf("zero header reported %d open segments", got)
	}
	for _, segments := range []int{0, 1, 7, diagnosticParserCoreMaxRecoveryOpenSegments} {
		if err := header.markRecoveryLineage(segments); err != nil {
			t.Fatalf("markRecoveryLineage(%d): %v", segments, err)
		}
		if !header.isRecoveryLineage() {
			t.Fatalf("marking with %d segments lost the lineage bit", segments)
		}
		if got := header.recoveryOpenSegments(); got != segments {
			t.Fatalf("stored %d segments, read back %d", segments, got)
		}
	}
	// Fail closed rather than truncate: a silently smaller count under-prices
	// the lineage by 500 per lost segment.
	if err := header.markRecoveryLineage(diagnosticParserCoreMaxRecoveryOpenSegments + 1); err == nil {
		t.Fatal("an out-of-range segment count was accepted")
	}
	if err := header.markRecoveryLineage(-1); err == nil {
		t.Fatal("a negative segment count was accepted")
	}
}
