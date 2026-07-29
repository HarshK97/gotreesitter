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
