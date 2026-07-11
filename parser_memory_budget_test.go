package gotreesitter_test

import (
	"bytes"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestParserMemoryBudgetStopsParse(t *testing.T) {
	t.Setenv("GOT_PARSE_MEMORY_BUDGET_MB", "1")
	gotreesitter.ResetParseEnvConfigCacheForTests()
	defer gotreesitter.ResetParseEnvConfigCacheForTests()

	parser := gotreesitter.NewParser(grammars.GoLanguage())
	var source bytes.Buffer
	source.WriteString("package p\nfunc f() {\n")
	for i := 0; i < 20000; i++ {
		source.WriteString("var x = 1\n")
	}
	source.WriteString("}\n")
	tree, err := parser.Parse(source.Bytes())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	defer tree.Release()

	if got, want := tree.ParseStopReason(), gotreesitter.ParseStopMemoryBudget; got != want {
		t.Fatalf("ParseStopReason() = %q, want %q (runtime=%s)", got, want, tree.ParseRuntime().Summary())
	}
	if !tree.ParseStoppedEarly() {
		t.Fatal("ParseStoppedEarly() = false, want true")
	}
	rt := tree.ParseRuntime()
	if rt.MemoryBudgetBytes <= 0 {
		t.Fatalf("MemoryBudgetBytes = %d, want > 0", rt.MemoryBudgetBytes)
	}
	switch rt.MemoryBudgetStopSource {
	case "arena":
		if growth := rt.ArenaBytesAllocated - rt.ArenaBaselineBytes; growth < rt.MemoryBudgetBytes {
			t.Fatalf("arena growth = %d, want >= budget %d (runtime=%s)", growth, rt.MemoryBudgetBytes, rt.Summary())
		}
	case "scratch":
		if growth := rt.ScratchBytesAllocated - rt.ScratchBaselineBytes; growth < rt.MemoryBudgetBytes {
			t.Fatalf("scratch growth = %d, want >= budget %d (runtime=%s)", growth, rt.MemoryBudgetBytes, rt.Summary())
		}
	case "runtime_heap":
		if rt.RuntimeHeapGrowthBytes < uint64(rt.MemoryBudgetBytes) {
			t.Fatalf("runtime heap growth = %d, want >= budget %d (runtime=%s)", rt.RuntimeHeapGrowthBytes, rt.MemoryBudgetBytes, rt.Summary())
		}
	case "runtime_sys":
		if rt.RuntimeSysGrowthBytes < uint64(rt.MemoryBudgetBytes) {
			t.Fatalf("runtime sys growth = %d, want >= budget %d (runtime=%s)", rt.RuntimeSysGrowthBytes, rt.MemoryBudgetBytes, rt.Summary())
		}
	default:
		t.Fatalf("MemoryBudgetStopSource = %q, want exact attribution (runtime=%s)", rt.MemoryBudgetStopSource, rt.Summary())
	}
}
