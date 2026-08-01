//go:build gts_parsercorephase0

package gotreesitter_test

// B4b stage 1 shadow census runner, smoke-corpus half (spec.b4b-alternative-
// set.v1 section 7, "Agreement census on the smoke corpus"). Opt-in and
// diagnostic-only, mirroring TestAdmissionCandidateScorecard206's own gating:
// it drives the compact candidate route across every registered language's
// smoke fixture, resetting the shadow census
// (parsercore_phase0_alternative_set_census.go) around each source so the
// per-language agree/old-proved-new-unproved/new-proved-old-unproved counts
// can be attributed and reported. The four-canonical-fixture half lives in
// parsercore_phase0_b4b_shadow_census_internal_test.go (package
// gotreesitter), which reuses the canonical fixture loader already wired for
// TestDiagnosticParserCoreCanonicalAdmissions.
//
// Run with:
//
//	GTS_B4B_SHADOW_CENSUS_REPORT=1 go test -tags gts_parsercorephase0 \
//	  -run TestDiagnosticParserCoreB4bShadowCensusSmokeCorpusReport -v .

import (
	"os"
	"sort"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type b4bShadowCensusRow struct {
	name   string
	totals gts.DiagnosticParserCoreShadowCensusTotals
}

func TestDiagnosticParserCoreB4bShadowCensusSmokeCorpusReport(t *testing.T) {
	if os.Getenv("GTS_B4B_SHADOW_CENSUS_REPORT") != "1" {
		t.Skip("set GTS_B4B_SHADOW_CENSUS_REPORT=1 to run the B4b stage 1 shadow census over the smoke corpus")
	}
	restore := gts.SetDiagnosticParserCoreShadowCensusEnabledForTest(true)
	defer restore()
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })

	var rows []b4bShadowCensusRow
	var allFalsifiers []gts.DiagnosticParserCoreShadowCensusFalsifier

	entries := grammars.AllLanguages()
	for _, entry := range entries {
		gts.DiagnosticParserCoreShadowCensusResetForTest()
		func() {
			defer func() { recover() }() // a candidate parse failure/panic must not lose the census so far
			runB4bShadowCensusLanguageSmoke(entry)
		}()
		totals := gts.DiagnosticParserCoreShadowCensusSnapshotForTest()
		rows = append(rows, b4bShadowCensusRow{name: entry.Name, totals: totals})
		allFalsifiers = append(allFalsifiers, gts.DiagnosticParserCoreShadowCensusFalsifiersForTest()...)
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	var grandTotal gts.DiagnosticParserCoreShadowCensusTotals
	t.Logf("=== B4b stage 1 shadow census: smoke corpus (%d languages) ===", len(rows))
	for _, row := range rows {
		total := row.totals
		grandTotal.Agree += total.Agree
		grandTotal.OldProvedNewUnproved += total.OldProvedNewUnproved
		grandTotal.NewProvedOldUnproved += total.NewProvedOldUnproved
		grandTotal.NeitherProved += total.NeitherProved
		if total == (gts.DiagnosticParserCoreShadowCensusTotals{}) {
			continue // no converged-split drop attempts observed for this source
		}
		t.Logf("%-20s agree=%-6d old-proved/new-unproved=%-6d new-proved/old-unproved=%-6d neither=%-6d",
			row.name, total.Agree, total.OldProvedNewUnproved, total.NewProvedOldUnproved, total.NeitherProved)
	}
	t.Logf("--- totals: agree=%d old-proved/new-unproved=%d new-proved/old-unproved=%d neither=%d falsifiers=%d ---",
		grandTotal.Agree, grandTotal.OldProvedNewUnproved, grandTotal.NewProvedOldUnproved, grandTotal.NeitherProved, len(allFalsifiers))

	if len(allFalsifiers) > 0 {
		for index, falsifier := range allFalsifiers {
			t.Logf("FALSIFIER[%d]: %s", index, falsifier.Detail)
		}
		t.Fatalf("design falsifier: %d old-proved/new-unproved drop(s); the alternative-set containment predicate must be a superset of the scalar proof (spec.b4b-alternative-set.v1 section 7 stop rule)", len(allFalsifiers))
	}
}

func runB4bShadowCensusLanguageSmoke(entry grammars.LangEntry) {
	lang := entry.Language()
	if lang == nil {
		return
	}
	support := grammars.EvaluateParseSupport(entry, lang)
	if support.Backend != grammars.ParseBackendDFA {
		return
	}
	source := []byte(grammars.ParseSmokeSample(entry.Name))
	candidate := gts.NewParser(lang)
	candidate.SetAdmissionCandidateRoute(true)
	tree, err := candidate.Parse(source)
	if err != nil || tree == nil {
		return
	}
	tree.Release()
}
