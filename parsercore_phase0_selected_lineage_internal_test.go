//go:build gts_parsercorephase0 && !gts_no_parsercorephase0

package gotreesitter

import (
	"testing"

	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

func TestDiagnosticParserCoreSelectedLineageDropProof(t *testing.T) {
	tests := []struct {
		name    string
		headers []diagnosticParserCoreHeader
		indices []int
		count   uint64
		proved  bool
	}{
		{
			name: "unselected-to-selected",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 7},
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankUnselected, cleanPathLineage: 7},
			},
			indices: []int{1}, count: 1, proved: true,
		},
		{
			name: "selected-drop",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 7},
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankUnselected, cleanPathLineage: 7},
			},
			indices: []int{0},
		},
		{
			name: "unknown-drop",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 7},
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankUnknown},
			},
			indices: []int{1},
		},
		{
			name: "different-lineage",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 8},
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankUnselected, cleanPathLineage: 7},
			},
			indices: []int{1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			count, proved := diagnosticParserCoreSelectedLineageDrops(test.headers, test.indices)
			if count != test.count || proved != test.proved {
				t.Fatalf("proof = %d/%t, want %d/%t", count, proved, test.count, test.proved)
			}
		})
	}
}

func TestDiagnosticParserCoreSelectedLineageProofAllocations(t *testing.T) {
	headers := []diagnosticParserCoreHeader{
		{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 7},
		{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankUnselected, cleanPathLineage: 7},
	}
	indices := []int{1}
	if allocations := testing.AllocsPerRun(1000, func() {
		count, proved := diagnosticParserCoreSelectedLineageDrops(headers, indices)
		if count != 1 || !proved {
			panic("selected-lineage proof failed")
		}
	}); allocations != 0 {
		t.Fatalf("warm selected-lineage proof allocations = %g, want 0", allocations)
	}
}

func TestDiagnosticParserCoreExternalShiftInvalidatesLineage(t *testing.T) {
	header := diagnosticParserCoreHeader{
		cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 7,
	}
	markDiagnosticParserCoreExternalLineage(&header, Token{ExternalScannerToken: true})
	if header.cleanPathRank != core.CleanPathRankUnknown || header.cleanPathLineage != 0 {
		t.Fatalf("external shift retained selected lineage: %+v", header)
	}
}

// TestDiagnosticParserCoreExternalShiftCarriesAlternativeSet pins the
// opposite property for the new record: unlike the scalar pair above, an
// external shift must never erase altSet (spec.b4b-alternative-set.v1
// section 4, "External shifts: carry, do not erase" -- the F1 fix). Stage 2
// replaces TestDiagnosticParserCoreExternalShiftInvalidatesLineage itself
// with the carry assertion once the set becomes the live proof; this pin
// documents the property from stage 1 on.
func TestDiagnosticParserCoreExternalShiftCarriesAlternativeSet(t *testing.T) {
	defer core.SetAlternativeSetRecordingEnabledForTest(true)()
	header := diagnosticParserCoreHeader{
		cleanPathRank: core.CleanPathRankSelected, cleanPathLineage: 7,
		altSet: core.NewAlternativeSetMember(7),
	}
	before := header.altSet
	markDiagnosticParserCoreExternalLineage(&header, Token{ExternalScannerToken: true})
	if header.altSet != before {
		t.Fatalf("external shift disturbed the carried alternative set: got %+v, want %+v", header.altSet, before)
	}
}

// alternativeSetPinCoverageTable is a minimal core.TableView stub: the
// converged-coverage predicate below never dispatches through a table, it
// only resolves AlternativeSet membership, so every method is unreachable.
type alternativeSetPinCoverageTable struct{}

func (alternativeSetPinCoverageTable) Actions(core.StateID, core.Symbol) (core.ActionRow, error) {
	return core.ActionRow{}, nil
}
func (alternativeSetPinCoverageTable) Goto(core.StateID, core.Symbol) (core.StateID, error) {
	return 0, nil
}
func (alternativeSetPinCoverageTable) ProductionFields(uint16, int) ([]core.FieldMapEntry, error) {
	return nil, nil
}
func (alternativeSetPinCoverageTable) ProductionAliases(uint16, int) ([]core.Symbol, error) {
	return nil, nil
}

func newAlternativeSetPinScheduler(t *testing.T) *diagnosticParserCoreGenericScheduler {
	t.Helper()
	compact, err := core.New(alternativeSetPinCoverageTable{}, core.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return &diagnosticParserCoreGenericScheduler{compact: compact}
}

func TestDiagnosticParserCoreConvergedCoverageDropsProof(t *testing.T) {
	defer core.SetAlternativeSetRecordingEnabledForTest(true)()
	member := func(members ...uint16) core.AlternativeSet {
		var set core.AlternativeSet
		compact, err := core.New(alternativeSetPinCoverageTable{}, core.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range members {
			compact.UnionAlternativeSet(&set, core.NewAlternativeSetMember(m))
		}
		return set
	}
	tests := []struct {
		name    string
		headers []diagnosticParserCoreHeader
		indices []int
		count   uint64
		proved  bool
	}{
		{
			name: "single-member-containment",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, altSet: member(7)},
				{convergedReductionSplit: true, altSet: member(7)},
			},
			indices: []int{1}, count: 1, proved: true,
		},
		{
			name: "multi-member-superset-containment",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, altSet: member(2, 5, 9)},
				{convergedReductionSplit: true, altSet: member(2, 5)},
			},
			indices: []int{1}, count: 1, proved: true,
		},
		{
			name: "partial-containment-fails-closed",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, altSet: member(2, 9)},
				{convergedReductionSplit: true, altSet: member(2, 5)},
			},
			indices: []int{1},
		},
		{
			// F2 (spec section 2): an exact cross-path tie zeroes the scalar
			// lineage (cleanPathRank stays Unknown) but never zeroes the set,
			// so containment still proves the drop.
			name: "unknown-rank-still-proves-by-set",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, altSet: member(7)},
				{convergedReductionSplit: true, cleanPathRank: core.CleanPathRankUnknown, altSet: member(7)},
			},
			indices: []int{1}, count: 1, proved: true,
		},
		{
			// F1 (spec section 2): an external shift erased cleanPathLineage
			// to zero, but the set was carried, not erased.
			name: "erased-scalar-lineage-still-proves-by-set",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, altSet: member(7)},
				{convergedReductionSplit: true, cleanPathLineage: 0, altSet: member(7)},
			},
			indices: []int{1}, count: 1, proved: true,
		},
		{
			name: "empty-dropped-set-fails-closed",
			headers: []diagnosticParserCoreHeader{
				{convergedReductionSplit: true, altSet: member(7)},
				{convergedReductionSplit: true},
			},
			indices: []int{1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler := newAlternativeSetPinScheduler(t)
			scheduler.headers = test.headers
			count, proved := scheduler.diagnosticParserCoreConvergedCoverageDrops(test.indices)
			if count != test.count || proved != test.proved {
				t.Fatalf("proof = %d/%t, want %d/%t", count, proved, test.count, test.proved)
			}
		})
	}
}

func TestDiagnosticParserCoreConvergedCoverageDropsOverflowedDroppedFailsClosed(t *testing.T) {
	defer core.SetAlternativeSetRecordingEnabledForTest(true)()
	scheduler := newAlternativeSetPinScheduler(t)
	dropped := core.NewAlternativeSetMember(1)
	for member := uint16(2); member <= 40; member++ {
		scheduler.compact.UnionAlternativeSet(&dropped, core.NewAlternativeSetMember(member))
	}
	if !dropped.Overflowed() {
		t.Fatal("setup did not overflow the dropped set")
	}
	survivor := dropped // recorded members are a subset either way; still must fail closed
	scheduler.headers = []diagnosticParserCoreHeader{
		{convergedReductionSplit: true, altSet: survivor},
		{convergedReductionSplit: true, altSet: dropped},
	}
	count, proved := scheduler.diagnosticParserCoreConvergedCoverageDrops([]int{1})
	if count != 0 || proved {
		t.Fatalf("proof = %d/%t, want 0/false (overflowed dropped set is incomplete)", count, proved)
	}
}

// TestDiagnosticParserCoreConvergedCoverageDropsProofAllocations is the
// converged-coverage counterpart to
// TestDiagnosticParserCoreSelectedLineageProofAllocations
// (spec.b4b-alternative-set.v1 section 3.4's second new pin).
func TestDiagnosticParserCoreConvergedCoverageDropsProofAllocations(t *testing.T) {
	defer core.SetAlternativeSetRecordingEnabledForTest(true)()
	scheduler := newAlternativeSetPinScheduler(t)
	survivorSet := core.NewAlternativeSetMember(7)
	scheduler.compact.UnionAlternativeSet(&survivorSet, core.NewAlternativeSetMember(9))
	scheduler.headers = []diagnosticParserCoreHeader{
		{convergedReductionSplit: true, altSet: survivorSet},
		{convergedReductionSplit: true, altSet: core.NewAlternativeSetMember(7)},
	}
	indices := []int{1}
	if allocations := testing.AllocsPerRun(1000, func() {
		count, proved := scheduler.diagnosticParserCoreConvergedCoverageDrops(indices)
		if count != 1 || !proved {
			panic("converged-coverage proof failed")
		}
	}); allocations != 0 {
		t.Fatalf("warm converged-coverage proof allocations = %g, want 0", allocations)
	}
}
