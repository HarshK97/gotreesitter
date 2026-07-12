package gotreesitter

import (
	"runtime"
	"runtime/debug"
	"testing"
)

func TestRuntimeMemoryBudgetStopReasonSamplesHeapGrowth(t *testing.T) {
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   1,
		parseRuntimeMemoryBaselineBytes: 0,
		parseRuntimeMemoryPoll:          parseRuntimeMemoryTightPollMask,
		parseMemoryBudgetDiagActive:     true,
	}
	if got := parser.runtimeMemoryBudgetStopReason(0); got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReason(0) = %q, want %q", got, ParseStopMemoryBudget)
	}
	if got, want := parser.parseMemoryBudgetDiag.source, parseMemoryBudgetStopSourceRuntimeHeap; got != want {
		t.Fatalf("memory budget stop source = %q, want %q", got, want)
	}
	if parser.parseMemoryBudgetDiag.runtimeHeapGrowthBytes < 1 {
		t.Fatalf("runtime heap growth = %d, want >= 1", parser.parseMemoryBudgetDiag.runtimeHeapGrowthBytes)
	}
}

func TestRuntimeMemoryPollMaskKeepsTightBudgetsResponsive(t *testing.T) {
	for _, tt := range []struct {
		name   string
		budget int64
		want   uint64
	}{
		{name: "tight", budget: parseRuntimeMemoryTightBudget - 1, want: parseRuntimeMemoryTightPollMask},
		{name: "tight-boundary", budget: parseRuntimeMemoryTightBudget, want: parseRuntimeMemoryTightPollMask},
		{name: "default-sized", budget: parseRuntimeMemoryTightBudget + 1, want: parseRuntimeMemoryPollMask},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeMemoryPollMask(tt.budget); got != tt.want {
				t.Fatalf("runtimeMemoryPollMask(%d) = %d, want %d", tt.budget, got, tt.want)
			}
			if tt.want&(tt.want+1) != 0 {
				t.Fatalf("poll mask %d is not a power-of-two interval minus one", tt.want)
			}
		})
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

// TestRuntimeMemoryVolumeTriggerBypassesPollMask exercises the volume-triggered
// forced poll (2026-07-12 budget-containment RCA, item 1). A budget above the
// tight-budget threshold uses poll mask 255, so an ordinary poll call is
// skipped roughly 255 times out of 256. This test picks a poll count the mask
// would normally skip and shows that a large enough tracked-allocation-volume
// delta since the last real check forces the check through anyway.
func TestRuntimeMemoryVolumeTriggerBypassesPollMask(t *testing.T) {
	const skippedPollCount = 1 // 1 & parseRuntimeMemoryPollMask(255) == 1 != 0 -> mask alone would skip.
	if skippedPollCount&parseRuntimeMemoryPollMask == 0 {
		t.Fatalf("test setup invariant broken: poll count %d not actually masked", skippedPollCount)
	}

	// Disable GC for the measured window: this package's full test binary
	// allocates heavily elsewhere, and a concurrent GC cycle reclaiming that
	// unrelated garbage can otherwise offset this test's own ballast growth
	// enough that HeapAlloc undercounts it relative to Sys, flipping which
	// source (runtime_heap vs runtime_sys) the check attributes the stop to.
	oldGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGC)

	// A loose-mask-selecting budget (255 mask, > parseRuntimeMemoryTightBudget)
	// is itself at least ~64MiB, so proving "heapGrowth >= budget" needs a real
	// allocation of that scale rather than relying on ambient process heap
	// size. Capture a real baseline, then deliberately grow the live heap past
	// the budget so runtimeMemoryBudgetStopReasonNow's comparison is genuine.
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	const budget = parseRuntimeMemoryTightBudget + 8<<20 // ~72MiB: > tight threshold, selects mask 255
	ballast := make([]byte, budget+(16<<20))             // guarantee >= budget bytes of real growth
	for i := range ballast {
		ballast[i] = byte(i)
	}
	t.Cleanup(func() { runtime.KeepAlive(ballast) })

	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:   budget,
		parseRuntimeMemoryBaselineBytes: before.HeapAlloc,
		parseRuntimeMemoryPoll:          skippedPollCount - 1,
		parseRuntimeMemoryVolumeAtPoll:  0,
		parseMemoryBudgetDiagActive:     true,
	}

	// Below the volume threshold: the mask governs and the poll is skipped.
	if got := parser.runtimeMemoryBudgetStopReason(parseRuntimeMemoryVolumePollThresholdBytes - 1); got != ParseStopNone {
		t.Fatalf("runtimeMemoryBudgetStopReason(small volume delta) = %q, want %q (mask should still gate)", got, ParseStopNone)
	}

	parser.parseRuntimeMemoryPoll = skippedPollCount - 1 // restore the masked poll count
	// At/above the volume threshold: forces a real check regardless of the mask.
	if got := parser.runtimeMemoryBudgetStopReason(parseRuntimeMemoryVolumePollThresholdBytes); got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReason(volume >= threshold) = %q, want %q (volume trigger should bypass the mask)", got, ParseStopMemoryBudget)
	}
	if got, want := parser.parseMemoryBudgetDiag.source, parseMemoryBudgetStopSourceRuntimeHeap; got != want {
		t.Fatalf("memory budget stop source = %q, want %q", got, want)
	}
	runtime.KeepAlive(ballast)
}

// TestParseMemoryHardCeilingStopsIndependentlyOfSoftBudget exercises the
// decoupled hard ceiling (item 3): it fires at its own fixed heap-growth
// threshold even when the soft per-parse budget has plenty of headroom left
// (i.e. it is not simply reusing the soft budget's overshoot tolerance).
func TestParseMemoryHardCeilingStopsIndependentlyOfSoftBudget(t *testing.T) {
	parser := &Parser{
		parseRuntimeMemoryBudgetBytes:      1 << 30, // 1GiB soft budget: nowhere near tripped
		parseRuntimeMemoryBaselineBytes:    0,
		parseRuntimeMemoryHardCeilingBytes: 1, // 1-byte hard ceiling: trips immediately
		parseMemoryBudgetDiagActive:        true,
	}
	got := parser.runtimeMemoryBudgetStopReasonNow()
	if got != ParseStopMemoryBudget {
		t.Fatalf("runtimeMemoryBudgetStopReasonNow() = %q, want %q", got, ParseStopMemoryBudget)
	}
	if got, want := parser.parseMemoryBudgetDiag.source, parseMemoryBudgetStopSourceHardCeiling; got != want {
		t.Fatalf("memory budget stop source = %q, want %q (soft budget should not have fired first)", got, want)
	}
}

// TestParseMemoryHardCeilingDefaultClearsPopplerMargin locks in the property
// the RCA's fix explicitly requires: the default hard ceiling must sit above
// the known-good Poppler JS witness completion point (~1.67GiB heap growth at
// the default soft budget) with real margin, so it never interferes with a
// legitimate large parse that relies on bounded soft-budget overshoot to
// finish. If this test ever needs to change, the Poppler docker perf lane
// witness must be re-verified before merge.
func TestParseMemoryHardCeilingDefaultClearsPopplerMargin(t *testing.T) {
	ResetParseEnvConfigCacheForTests()
	defer ResetParseEnvConfigCacheForTests()

	// 1.67GiB expressed in whole bytes: 1.67 * 1024 * 1024 * 1024, rounded down.
	const popplerWitnessGrowthBytes = int64(1793147207)
	ceiling := parseMemoryHardCeilingBytes()
	if ceiling <= popplerWitnessGrowthBytes {
		t.Fatalf("default hard ceiling %d bytes does not clear the Poppler witness growth %d bytes", ceiling, popplerWitnessGrowthBytes)
	}
	margin := ceiling - popplerWitnessGrowthBytes
	const minMargin = 256 << 20 // at least 256MiB of headroom
	if margin < minMargin {
		t.Fatalf("hard ceiling margin over the Poppler witness = %d bytes, want >= %d bytes", margin, minMargin)
	}
}

// TestParseMemoryHardCeilingEnvOverride confirms GOT_PARSE_MEMORY_HARD_CEILING_MB
// is honored, including the documented 0=off escape hatch.
func TestParseMemoryHardCeilingEnvOverride(t *testing.T) {
	t.Setenv("GOT_PARSE_MEMORY_HARD_CEILING_MB", "5")
	ResetParseEnvConfigCacheForTests()
	defer ResetParseEnvConfigCacheForTests()
	if got, want := parseMemoryHardCeilingBytes(), int64(5)*1024*1024; got != want {
		t.Fatalf("parseMemoryHardCeilingBytes() = %d, want %d", got, want)
	}

	t.Setenv("GOT_PARSE_MEMORY_HARD_CEILING_MB", "0")
	ResetParseEnvConfigCacheForTests()
	if got := parseMemoryHardCeilingBytes(); got != 0 {
		t.Fatalf("parseMemoryHardCeilingBytes() with 0 override = %d, want 0 (disabled)", got)
	}
	if runtimeMemoryHardCeilingEnabled(&Parser{}, parseRuntimeMemoryMinSourceBytes) {
		t.Fatal("runtimeMemoryHardCeilingEnabled() = true with hard ceiling disabled via env, want false")
	}
}
