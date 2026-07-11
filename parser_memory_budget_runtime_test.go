package gotreesitter

import "testing"

func TestRuntimeMemoryBudgetStopReasonSamplesHeapGrowth(t *testing.T) {
	diagnostic := &parseMemoryBudgetDiagnostic{}
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   1,
		parseRuntimeMemoryBaselineBytes: 0,
		parseRuntimeMemoryPoll:          parseRuntimeMemoryPollMask,
		parseMemoryBudgetDiag:           diagnostic,
	}
	if got := parser.runtimeMemoryBudgetStopReason(); got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReason() = %q, want %q", got, ParseStopMemoryBudget)
	}
	if got, want := diagnostic.source, parseMemoryBudgetStopSourceRuntimeHeap; got != want {
		t.Fatalf("memory budget stop source = %q, want %q", got, want)
	}
	if diagnostic.runtimeHeapGrowthBytes < 1 {
		t.Fatalf("runtime heap growth = %d, want >= 1", diagnostic.runtimeHeapGrowthBytes)
	}
}

func TestRuntimeMemoryBudgetStopReasonSamplesSysGrowth(t *testing.T) {
	diagnostic := &parseMemoryBudgetDiagnostic{}
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   1,
		parseRuntimeMemoryBaselineBytes: ^uint64(0),
		parseRuntimeMemoryBaselineSys:   0,
		parseMemoryBudgetDiag:           diagnostic,
	}
	if got := parser.runtimeMemoryBudgetStopReasonNow(); got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReasonNow() = %q, want %q", got, ParseStopMemoryBudget)
	}
	if got, want := diagnostic.source, parseMemoryBudgetStopSourceRuntimeSys; got != want {
		t.Fatalf("memory budget stop source = %q, want %q", got, want)
	}
	if diagnostic.runtimeSysGrowthBytes < 1 {
		t.Fatalf("runtime sys growth = %d, want >= 1", diagnostic.runtimeSysGrowthBytes)
	}
}

func TestMemoryBudgetDiagnosticLatchesFirstSource(t *testing.T) {
	diagnostic := &parseMemoryBudgetDiagnostic{}
	parser := &Parser{parseMemoryBudgetDiag: diagnostic}

	parser.noteMemoryBudgetStop(parseMemoryBudgetStopSourceScratch)
	parser.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceRuntimeSys, 123, 456)

	if got, want := diagnostic.source, parseMemoryBudgetStopSourceScratch; got != want {
		t.Fatalf("memory budget stop source = %q, want first source %q", got, want)
	}
	if diagnostic.runtimeHeapGrowthBytes != 0 || diagnostic.runtimeSysGrowthBytes != 0 {
		t.Fatalf(
			"runtime growth overwritten after first source: heap=%d sys=%d",
			diagnostic.runtimeHeapGrowthBytes,
			diagnostic.runtimeSysGrowthBytes,
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
