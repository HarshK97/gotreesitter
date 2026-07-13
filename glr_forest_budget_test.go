package gotreesitter

import (
	"strings"
	"testing"
)

func TestEnterForestMemoryBudgetArmsAndRestoresState(t *testing.T) {
	t.Setenv("GOT_PARSE_MEMORY_BUDGET_MB", "8")
	t.Setenv("GOT_PARSE_MEMORY_HARD_CEILING_MB", "0")
	ResetParseEnvConfigCacheForTests()
	t.Cleanup(ResetParseEnvConfigCacheForTests)
	DrainArenaPools()

	parser := NewParser(loadBlobForDecode(t, "json"))
	parser.parseRuntimeMemoryBudgetBytes = 11
	parser.parseRuntimeMemoryBaselineBytes = 22
	parser.parseRuntimeMemoryBaselineSys = 33
	parser.parseRuntimeMemoryPoll = 44
	parser.parseRuntimeMemoryVolumeAtPoll = 55
	parser.parseRuntimeMemoryHardCeilingBytes = 66

	arena := acquireNodeArena(arenaClassFull)
	restore := parser.enterForestMemoryBudget(arena, parseRuntimeMemoryMinSourceBytes)
	if restore.parser != parser {
		t.Fatal("forest runtime memory budget did not arm for a large source")
	}
	if got, want := parser.parseRuntimeMemoryBudgetBytes, int64(8<<20); got != want {
		t.Fatalf("armed runtime budget = %d, want %d", got, want)
	}
	if got, want := arena.budgetBytes, int64(8<<20); got != want {
		t.Fatalf("armed arena budget = %d, want %d", got, want)
	}
	if parser.parseRuntimeMemoryHardCeilingBytes != 0 {
		t.Fatalf("armed hard ceiling = %d, want disabled", parser.parseRuntimeMemoryHardCeilingBytes)
	}
	restore.restore()
	arena.Release()

	if got := parser.parseRuntimeMemoryBudgetBytes; got != 11 {
		t.Fatalf("runtime budget after parseForest = %d, want 11", got)
	}
	if got := parser.parseRuntimeMemoryBaselineBytes; got != 22 {
		t.Fatalf("runtime heap baseline after parseForest = %d, want 22", got)
	}
	if got := parser.parseRuntimeMemoryBaselineSys; got != 33 {
		t.Fatalf("runtime sys baseline after parseForest = %d, want 33", got)
	}
	if got := parser.parseRuntimeMemoryPoll; got != 44 {
		t.Fatalf("runtime poll after parseForest = %d, want 44", got)
	}
	if got := parser.parseRuntimeMemoryVolumeAtPoll; got != 55 {
		t.Fatalf("runtime volume checkpoint after parseForest = %d, want 55", got)
	}
	if got := parser.parseRuntimeMemoryHardCeilingBytes; got != 66 {
		t.Fatalf("runtime hard ceiling after parseForest = %d, want 66", got)
	}
}

func TestParseForestMemoryBudgetDeclines(t *testing.T) {
	t.Setenv("GOT_PARSE_MEMORY_BUDGET_MB", "512")
	t.Setenv("GOT_PARSE_MEMORY_HARD_CEILING_MB", "1")
	ResetParseEnvConfigCacheForTests()
	t.Cleanup(ResetParseEnvConfigCacheForTests)
	DrainArenaPools()

	parser := NewParser(loadBlobForDecode(t, "json"))
	parser.parseRuntimeMemoryBudgetBytes = 17
	parser.parseRuntimeMemoryHardCeilingBytes = 23
	// Clear the runtime guard's 64 KiB arming floor. The 1 MiB runtime hard
	// ceiling must decline before the intentionally roomy 512 MiB node arena;
	// this makes the regression sensitive to the forest's runtime-wide poll.
	source := []byte("[" + strings.Repeat("0,", 40_000) + "0]")
	arena := acquireNodeArena(arenaClassFull)
	root, ok := parser.parseForest(arena, source, false)
	arenaBudgetExhausted := arena.budgetExhausted()
	arena.Release()
	if ok || root != nil {
		t.Fatal("parseForest accepted despite the 1 MiB runtime hard ceiling")
	}
	if _, _, reason, _ := parser.ForestDeclineInfo(); reason != "budget" {
		t.Fatalf("forest decline reason = %q, want budget", reason)
	}
	if arenaBudgetExhausted {
		t.Fatal("node arena exhausted; test did not isolate the runtime-wide forest budget")
	}
	if got := parser.parseRuntimeMemoryBudgetBytes; got != 17 {
		t.Fatalf("runtime budget after parseForest decline = %d, want 17", got)
	}
	if got := parser.parseRuntimeMemoryHardCeilingBytes; got != 23 {
		t.Fatalf("runtime hard ceiling after parseForest decline = %d, want 23", got)
	}
}

func TestForestMemoryBudgetFinalCheckSamplesRuntimeUnmasked(t *testing.T) {
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   1,
		parseRuntimeMemoryBaselineBytes: 0,
	}
	arena := acquireNodeArena(arenaClassFull)
	defer arena.Release()
	arena.setBudget(1 << 30)

	// The ordinary masked poll should skip its first sample. The final check
	// must bypass that mask so EOF/root materialization cannot return a tree
	// whose forest-only heap crossed the budget after the last token boundary.
	if parser.forestMemoryBudgetExceeded(arena, false) {
		t.Fatal("ordinary masked poll unexpectedly sampled on its first call")
	}
	if !parser.forestMemoryBudgetExceeded(arena, true) {
		t.Fatal("final forest budget check did not sample live runtime memory")
	}
}
