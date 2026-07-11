package gotreesitter

import "testing"

func TestRuntimeMemoryBudgetStopReasonSamplesHeapGrowth(t *testing.T) {
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   1,
		parseRuntimeMemoryBaselineBytes: 0,
		parseRuntimeMemoryPoll:          parseRuntimeMemoryPollMask,
		parseMemoryBudgetDiagActive:     true,
	}
	if got := parser.runtimeMemoryBudgetStopReason(); got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReason() = %q, want %q", got, ParseStopMemoryBudget)
	}
	if got, want := parser.parseMemoryBudgetDiag.source, parseMemoryBudgetStopSourceRuntimeHeap; got != want {
		t.Fatalf("memory budget stop source = %q, want %q", got, want)
	}
	if parser.parseMemoryBudgetDiag.runtimeHeapGrowthBytes < 1 {
		t.Fatalf("runtime heap growth = %d, want >= 1", parser.parseMemoryBudgetDiag.runtimeHeapGrowthBytes)
	}
}

func TestRuntimeMemoryBudgetStopReasonSamplesSysGrowth(t *testing.T) {
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   1,
		parseRuntimeMemoryBaselineBytes: ^uint64(0),
		parseRuntimeMemoryBaselineSys:   0,
		parseMemoryBudgetDiagActive:     true,
	}
	if got := parser.runtimeMemoryBudgetStopReasonNow(); got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReasonNow() = %q, want %q", got, ParseStopMemoryBudget)
	}
	if got, want := parser.parseMemoryBudgetDiag.source, parseMemoryBudgetStopSourceRuntimeSys; got != want {
		t.Fatalf("memory budget stop source = %q, want %q", got, want)
	}
	if parser.parseMemoryBudgetDiag.runtimeSysGrowthBytes < 1 {
		t.Fatalf("runtime sys growth = %d, want >= 1", parser.parseMemoryBudgetDiag.runtimeSysGrowthBytes)
	}
}

func TestMemoryBudgetDiagnosticLatchesFirstSource(t *testing.T) {
	parser := &Parser{parseMemoryBudgetDiagActive: true}

	parser.noteMemoryBudgetStop(parseMemoryBudgetStopSourceScratch)
	parser.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceRuntimeSys, 123, 456)

	if got, want := parser.parseMemoryBudgetDiag.source, parseMemoryBudgetStopSourceScratch; got != want {
		t.Fatalf("memory budget stop source = %q, want first source %q", got, want)
	}
	if parser.parseMemoryBudgetDiag.runtimeHeapGrowthBytes != 0 || parser.parseMemoryBudgetDiag.runtimeSysGrowthBytes != 0 {
		t.Fatalf(
			"runtime growth overwritten after first source: heap=%d sys=%d",
			parser.parseMemoryBudgetDiag.runtimeHeapGrowthBytes,
			parser.parseMemoryBudgetDiag.runtimeSysGrowthBytes,
		)
	}
}

func TestRuntimeMemoryBudgetDisabledForSmallSource(t *testing.T) {
	parser := &Parser{}
	restore := parser.enterRuntimeMemoryBudget(1, parseRuntimeMemoryMinSourceBytes-1)
	if restore.parser != nil {
		t.Fatal("enterRuntimeMemoryBudget enabled for small source")
	}
	if parser.parseRuntimeMemoryBudgetBytes != 0 {
		t.Fatalf("parseRuntimeMemoryBudgetBytes = %d, want 0", parser.parseRuntimeMemoryBudgetBytes)
	}
}

func TestRuntimeMemoryBudgetEnabledForLargeSource(t *testing.T) {
	parser := &Parser{}
	restore := parser.enterRuntimeMemoryBudget(1, parseRuntimeMemoryMinSourceBytes)
	if restore.parser != parser {
		t.Fatal("enterRuntimeMemoryBudget did not return parser restore state")
	}
	defer restore.restore()
	if parser.parseRuntimeMemoryBudgetBytes != 1 {
		t.Fatalf("parseRuntimeMemoryBudgetBytes = %d, want 1", parser.parseRuntimeMemoryBudgetBytes)
	}
}
