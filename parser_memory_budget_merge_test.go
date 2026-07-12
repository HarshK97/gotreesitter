package gotreesitter

import (
	"runtime"
	"runtime/debug"
	"testing"
)

// TestMergeStacksWithScratchLargeCapPollsMemoryBudgetMidGrind exercises the
// coarse-stride in-merge poll added to mergeStacksWithScratchLargeCap (2026-07-12
// budget-containment RCA, item 2). Before that fix, the large-cap merge's
// O(survivors^2) deep-equivalence grind could allocate multiple GB without ever
// returning to a materialization-boundary poll site, so no memory-budget stop
// was structurally reachable during the grind. This test constructs many
// pairwise-distinct stacks that all share a single merge key (same top state,
// same byte offset), forcing the large-cap path to compare each new candidate
// against every retained survivor up to perKeyCap -- the same O(n^2) shape the
// RCA identified -- and arms a 1-byte runtime memory budget so any real check
// trips immediately. The assertion is that the merge stops early (partial
// result, far short of the full input) with mergeBudgetStopReason set, proving
// the stride poll actually fires mid-grind rather than only at entry/exit.
func TestMergeStacksWithScratchLargeCapPollsMemoryBudgetMidGrind(t *testing.T) {
	// Disable GC for the duration of the measured window. This package's full
	// test binary allocates heavily elsewhere (including deliberately huge,
	// short-lived allocations in nearby memory-budget tests); a concurrent GC
	// cycle reclaiming that unrelated garbage can otherwise make this test's
	// own net HeapAlloc delta read as flat or negative at the exact instant
	// the poll samples it, even though real, live growth occurred. Disabling
	// GC makes HeapAlloc monotonically non-decreasing for the test's duration.
	oldGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGC)

	const n = 4000
	stacks := make([]glrStack, n)
	for i := range stacks {
		stacks[i] = glrStack{
			entries: []stackEntry{
				// Distinct bottom entry -> distinct hash -> distinct hash bucket,
				// so equivalence never short-circuits into a cheap duplicate merge.
				{state: StateID(1000 + i)},
				// Shared top state -> every stack maps to the same merge key,
				// forcing genuine per-key survivor-list growth and O(slot.count)
				// comparisons per candidate once the key's slot is populated.
				{state: StateID(42)},
			},
		}
	}

	parser := &Parser{}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	// Arm the runtime budget directly (bypassing enterRuntimeMemoryBudget's
	// min-source-length gate, irrelevant when calling the merge function
	// directly): baseline "now", budget of 1 byte so any further heap growth
	// trips the very first real check the stride poll performs.
	parser.parseRuntimeMemoryBudgetBytes = 1
	parser.parseRuntimeMemoryBaselineBytes = stats.HeapAlloc
	parser.parseRuntimeMemoryBaselineSys = stats.Sys
	parser.parseMemoryBudgetDiagActive = true

	scratch := &glrMergeScratch{
		perKeyCap: maxStacksPerMergeKeyCeiling,
		parser:    parser,
	}
	scratch.beginEquivEpoch()

	result := mergeStacksWithScratchLargeCap(stacks, scratch, maxStacksPerMergeKeyCeiling)

	if scratch.mergeBudgetStopReason != ParseStopMemoryBudget {
		t.Fatalf("mergeBudgetStopReason = %v, want %v", scratch.mergeBudgetStopReason, ParseStopMemoryBudget)
	}
	if len(result) >= n {
		t.Fatalf("merge processed all %d survivors (len(result)=%d), want an early partial stop far short of n", n, len(result))
	}
	if parser.parseMemoryBudgetDiag.source == "" {
		t.Fatal("expected a diagnostic source to be recorded for the in-merge budget stop")
	}
}

// TestMergeStacksWithScratchLargeCapNoParserNeverPolls confirms the in-merge
// poll is a strict no-op (never touches scratch.mergeBudgetStopReason, never
// short-circuits) when scratch.parser is nil, e.g. the package-internal
// mergeStacks() test helper. This guards against the stride poll silently
// changing behavior for callers that were never wired to a live Parser.
func TestMergeStacksWithScratchLargeCapNoParserNeverPolls(t *testing.T) {
	const n = 64
	stacks := make([]glrStack, n)
	for i := range stacks {
		stacks[i] = glrStack{
			entries: []stackEntry{
				{state: StateID(2000 + i)},
				{state: StateID(7)},
			},
		}
	}
	scratch := &glrMergeScratch{perKeyCap: maxStacksPerMergeKeyCeiling, mergeBudgetStopReason: ParseStopNone}
	scratch.beginEquivEpoch()

	result := mergeStacksWithScratchLargeCap(stacks, scratch, maxStacksPerMergeKeyCeiling)
	if scratch.mergeBudgetStopReason != ParseStopNone {
		t.Fatalf("mergeBudgetStopReason = %v, want %v when scratch.parser is nil", scratch.mergeBudgetStopReason, ParseStopNone)
	}
	if len(result) != n {
		t.Fatalf("len(result) = %d, want %d (no early stop expected without a parser)", len(result), n)
	}
}
