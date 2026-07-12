package grammargen

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// TestBuildFieldMapsDedupesSharedProductionIDs guards against a regression
// where buildFieldMaps appended one entry run per production instead of one
// run per ProductionID. compactProductionIDs collapses productions that share
// an identical alias+field fingerprint onto the same ProductionID, so a
// grammar with many syntactically-different-but-field-identical productions
// (e.g. `foo: 'a' NAME` and `bar: 'b' NAME`, both assigning field "name" to
// child 1) legitimately shares one ProductionID across many productions. The
// old code appended a fresh, unreachable entry run for every production after
// the first sharing that ID (only the last write survived in
// FieldMapSlices), inflating FieldMapEntries until the uint16 start offset
// overflowed on large real-world grammars (bash, dart).
func TestBuildFieldMapsDedupesSharedProductionIDs(t *testing.T) {
	ng := &NormalizedGrammar{
		GrammarName: "synthetic",
		FieldNames:  []string{"", "name"},
		Productions: []Production{
			// Three distinct productions sharing field-bearing ProductionID 1.
			{LHS: 10, RHS: []int{1, 2}, ProductionID: 1, Fields: []FieldAssign{{ChildIndex: 1, FieldName: "name"}}},
			{LHS: 11, RHS: []int{3, 2}, ProductionID: 1, Fields: []FieldAssign{{ChildIndex: 1, FieldName: "name"}}},
			{LHS: 12, RHS: []int{4, 2}, ProductionID: 1, Fields: []FieldAssign{{ChildIndex: 1, FieldName: "name"}}},
			// A second field-bearing ID with its own distinct field set,
			// also shared by more than one production.
			{LHS: 13, RHS: []int{2, 5}, ProductionID: 2, Fields: []FieldAssign{{ChildIndex: 0, FieldName: "name"}}},
			{LHS: 14, RHS: []int{2, 6}, ProductionID: 2, Fields: []FieldAssign{{ChildIndex: 0, FieldName: "name"}}},
			// A fieldless production (ID 0) must not contribute any entries.
			{LHS: 15, RHS: []int{7}, ProductionID: 0},
		},
	}

	lang := &gotreesitter.Language{}
	if err := buildFieldMaps(lang, ng); err != nil {
		t.Fatalf("buildFieldMaps: %v", err)
	}

	if got, want := len(lang.FieldMapEntries), 2; got != want {
		t.Fatalf("len(FieldMapEntries) = %d, want %d (one run per distinct field-bearing ProductionID, not one per production)", got, want)
	}

	// (a) every entry must be reachable via exactly one FieldMapSlices run,
	// and the reachable count must equal len(entries) — no orphaned entries.
	reachable := 0
	runsPerID := make(map[int]int)
	for pid, slice := range lang.FieldMapSlices {
		start, count := int(slice[0]), int(slice[1])
		if count == 0 {
			continue
		}
		runsPerID[pid]++
		reachable += count
		if start+count > len(lang.FieldMapEntries) {
			t.Fatalf("FieldMapSlices[%d] = {start:%d count:%d} runs past len(entries)=%d", pid, start, count, len(lang.FieldMapEntries))
		}
	}
	if reachable != len(lang.FieldMapEntries) {
		t.Fatalf("reachable entries = %d, want %d (len(FieldMapEntries)) — orphaned/unreachable entries present", reachable, len(lang.FieldMapEntries))
	}

	// (b) no duplicate runs per ProductionID.
	for pid, count := range runsPerID {
		if count != 1 {
			t.Fatalf("ProductionID %d has %d runs recorded, want exactly 1", pid, count)
		}
	}

	// Sanity-check the actual field content survives for both IDs.
	slice1 := lang.FieldMapSlices[1]
	if slice1[1] != 1 {
		t.Fatalf("FieldMapSlices[1] count = %d, want 1", slice1[1])
	}
	entry1 := lang.FieldMapEntries[slice1[0]]
	if entry1.ChildIndex != 1 || entry1.FieldID != gotreesitter.FieldID(1) {
		t.Fatalf("FieldMapEntries for ProductionID 1 = %+v, want {FieldID:1 ChildIndex:1}", entry1)
	}

	slice2 := lang.FieldMapSlices[2]
	if slice2[1] != 1 {
		t.Fatalf("FieldMapSlices[2] count = %d, want 1", slice2[1])
	}
	entry2 := lang.FieldMapEntries[slice2[0]]
	if entry2.ChildIndex != 0 || entry2.FieldID != gotreesitter.FieldID(1) {
		t.Fatalf("FieldMapEntries for ProductionID 2 = %+v, want {FieldID:1 ChildIndex:0}", entry2)
	}

	// ProductionID 0 (fieldless) must remain the zero-value slice.
	if slice0 := lang.FieldMapSlices[0]; slice0 != ([2]uint16{}) {
		t.Fatalf("FieldMapSlices[0] = %+v, want zero-value (no fields)", slice0)
	}
}
