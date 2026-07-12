package gotreesitter

import "runtime"

const (
	// Arena and parser scratch enforce their budgets at every materialization
	// boundary. The runtime-wide guard covers untracked Go heap growth, but the
	// coarse iteration-count poll mask alone lets overshoot balloon 16-26x past
	// the configured budget on lanes that allocate multiple GB across only a
	// few poll-site calls (see the 2026-07-12 budget-containment RCA). Two
	// additional layers close that gap without tightening the mask globally
	// (some legitimate parses, e.g. the Poppler JS witness, rely on bounded
	// overshoot at the default budget to complete): a volume-triggered forced
	// poll (parseRuntimeMemoryVolumePollThresholdBytes) and an absolute,
	// decoupled hard ceiling (parseMemoryHardCeilingMB) checked every time a
	// real poll fires.
	parseRuntimeMemoryPollMask       = 255
	parseRuntimeMemoryTightPollMask  = 15
	parseRuntimeMemoryTightBudget    = 64 << 20
	parseRuntimeMemoryMinSourceBytes = 64 * 1024

	// parseRuntimeMemoryVolumePollThresholdBytes forces a real runtime memory
	// check (bypassing the iteration-count mask) once tracked allocation
	// volume has grown by at least this much since the last real check. This
	// catches lanes that allocate gigabytes across very few poll-site calls,
	// where the mask alone would let many multiples of the budget pass
	// unnoticed before the next sampled check.
	parseRuntimeMemoryVolumePollThresholdBytes = 64 << 20

	parseMemoryBudgetStopSourceArena       = "arena"
	parseMemoryBudgetStopSourceScratch     = "scratch"
	parseMemoryBudgetStopSourceRuntimeHeap = "runtime_heap"
	parseMemoryBudgetStopSourceRuntimeSys  = "runtime_sys"
	parseMemoryBudgetStopSourceHardCeiling = "hard_ceiling"
)

type parseMemoryBudgetDiagnostic struct {
	source                 string
	runtimeHeapGrowthBytes uint64
	runtimeSysGrowthBytes  uint64
}

type runtimeMemoryBudgetRestore struct {
	parser       *Parser
	budget       int64
	baseline     uint64
	baselineSys  uint64
	poll         uint64
	volumeAtPoll uint64
	hardCeiling  int64
}

func runtimeMemoryBudgetEnabled(p *Parser, bytes int64, sourceLen int) bool {
	return p != nil && bytes > 0 && sourceLen >= parseRuntimeMemoryMinSourceBytes
}

// runtimeMemoryHardCeilingEnabled reports whether the absolute hard ceiling
// should arm for this parse. It shares the same trivial-source floor as the
// soft budget (no point instrumenting parses too small to plausibly balloon
// to GB scale) but is otherwise independent of whether the soft budget itself
// is enabled, so it keeps working even if a caller explicitly disables the
// soft budget (GOT_PARSE_MEMORY_BUDGET_MB=0).
func runtimeMemoryHardCeilingEnabled(p *Parser, sourceLen int) bool {
	return p != nil && sourceLen >= parseRuntimeMemoryMinSourceBytes && parseMemoryHardCeilingBytes() > 0
}

// enterRuntimeMemoryBudget arms the runtime-wide poll for the current parse.
func (p *Parser) enterRuntimeMemoryBudget(bytes int64, sourceLen int) runtimeMemoryBudgetRestore {
	softEnabled := runtimeMemoryBudgetEnabled(p, bytes, sourceLen)
	hardEnabled := runtimeMemoryHardCeilingEnabled(p, sourceLen)
	if p == nil || (!softEnabled && !hardEnabled) {
		return runtimeMemoryBudgetRestore{}
	}
	hardCeiling := int64(0)
	if hardEnabled {
		hardCeiling = parseMemoryHardCeilingBytes()
	}
	restore := runtimeMemoryBudgetRestore{
		parser:       p,
		budget:       p.parseRuntimeMemoryBudgetBytes,
		baseline:     p.parseRuntimeMemoryBaselineBytes,
		baselineSys:  p.parseRuntimeMemoryBaselineSys,
		poll:         p.parseRuntimeMemoryPoll,
		volumeAtPoll: p.parseRuntimeMemoryVolumeAtPoll,
		hardCeiling:  p.parseRuntimeMemoryHardCeilingBytes,
	}

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if softEnabled {
		p.parseRuntimeMemoryBudgetBytes = bytes
	} else {
		p.parseRuntimeMemoryBudgetBytes = 0
	}
	p.parseRuntimeMemoryBaselineBytes = stats.HeapAlloc
	p.parseRuntimeMemoryBaselineSys = stats.Sys
	p.parseRuntimeMemoryPoll = 0
	p.parseRuntimeMemoryVolumeAtPoll = 0
	p.parseRuntimeMemoryHardCeilingBytes = hardCeiling

	return restore
}

func (r runtimeMemoryBudgetRestore) restore() {
	if r.parser == nil {
		return
	}
	r.parser.parseRuntimeMemoryBudgetBytes = r.budget
	r.parser.parseRuntimeMemoryBaselineBytes = r.baseline
	r.parser.parseRuntimeMemoryBaselineSys = r.baselineSys
	r.parser.parseRuntimeMemoryPoll = r.poll
	r.parser.parseRuntimeMemoryVolumeAtPoll = r.volumeAtPoll
	r.parser.parseRuntimeMemoryHardCeilingBytes = r.hardCeiling
}

// runtimeMemoryBudgetStopReason is the mask-gated poll called from the many
// materialization-boundary sites throughout the parse loop. volumeBytes is a
// cheap, already-tracked allocation-volume signal (e.g. arena.allocatedBytes)
// sampled at the call site with no extra syscalls. When that signal has grown
// by at least parseRuntimeMemoryVolumePollThresholdBytes since the last real
// check, the poll is forced through regardless of the iteration-count mask --
// this is what catches a lane that allocates gigabytes across only a handful
// of poll-site calls (mask undersampling), without lowering the mask (and
// therefore the tolerated overshoot) for every parse.
func (p *Parser) runtimeMemoryBudgetStopReason(volumeBytes uint64) ParseStopReason {
	if p == nil || (p.parseRuntimeMemoryBudgetBytes <= 0 && p.parseRuntimeMemoryHardCeilingBytes <= 0) {
		return ParseStopNone
	}
	p.parseRuntimeMemoryPoll++
	forced := runtimeMemoryVolumeForcesPoll(volumeBytes, p.parseRuntimeMemoryVolumeAtPoll)
	if !forced && p.parseRuntimeMemoryPoll&runtimeMemoryPollMask(p.parseRuntimeMemoryBudgetBytes) != 0 {
		return ParseStopNone
	}
	p.parseRuntimeMemoryVolumeAtPoll = volumeBytes
	return p.runtimeMemoryBudgetStopReasonNow()
}

// runtimeMemoryVolumeForcesPoll reports whether tracked allocation volume has
// grown enough since the last real check to force one now, bypassing the
// iteration-count mask.
func runtimeMemoryVolumeForcesPoll(current, last uint64) bool {
	if current <= last {
		return false
	}
	return current-last >= parseRuntimeMemoryVolumePollThresholdBytes
}

func runtimeMemoryPollMask(budgetBytes int64) uint64 {
	if budgetBytes <= parseRuntimeMemoryTightBudget {
		return parseRuntimeMemoryTightPollMask
	}
	return parseRuntimeMemoryPollMask
}

// runtimeMemoryBudgetStopReasonNow performs the actual runtime.ReadMemStats
// sample and comparison, unconditionally (no mask, no volume gating). It is
// used both by the masked poll above once it decides to fire, and directly by
// the GLR merge survivor loop's coarse-stride poll (mergeStacksWithScratchLargeCap),
// which has no other route back to a materialization-boundary poll site
// during its O(survivors^2) grind. The hard ceiling is checked first and is
// intentionally decoupled from the soft per-parse budget's overshoot
// tolerance: it fires at a fixed absolute heap-growth regardless of how loose
// the soft budget's poll cadence is for this parse.
func (p *Parser) runtimeMemoryBudgetStopReasonNow() ParseStopReason {
	if p == nil || (p.parseRuntimeMemoryBudgetBytes <= 0 && p.parseRuntimeMemoryHardCeilingBytes <= 0) {
		return ParseStopNone
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	heapGrowth := runtimeMemoryGrowth(stats.HeapAlloc, p.parseRuntimeMemoryBaselineBytes)
	sysGrowth := runtimeMemoryGrowth(stats.Sys, p.parseRuntimeMemoryBaselineSys)
	if ceiling := p.parseRuntimeMemoryHardCeilingBytes; ceiling > 0 && heapGrowth >= uint64(ceiling) {
		return p.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceHardCeiling, heapGrowth, sysGrowth)
	}
	if p.parseRuntimeMemoryBudgetBytes > 0 {
		budget := uint64(p.parseRuntimeMemoryBudgetBytes)
		if heapGrowth >= budget {
			return p.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceRuntimeHeap, heapGrowth, sysGrowth)
		}
		if sysGrowth >= budget {
			return p.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceRuntimeSys, heapGrowth, sysGrowth)
		}
	}
	return ParseStopNone
}

func runtimeMemoryGrowth(current, baseline uint64) uint64 {
	if current <= baseline {
		return 0
	}
	return current - baseline
}

func (p *Parser) noteMemoryBudgetStop(source string) ParseStopReason {
	return p.noteRuntimeMemoryBudgetStop(source, 0, 0)
}

func (p *Parser) noteRuntimeMemoryBudgetStop(source string, heapGrowth, sysGrowth uint64) ParseStopReason {
	if p == nil || !p.parseMemoryBudgetDiagActive || p.parseMemoryBudgetDiag.source != "" {
		return ParseStopMemoryBudget
	}
	p.parseMemoryBudgetDiag.source = source
	p.parseMemoryBudgetDiag.runtimeHeapGrowthBytes = heapGrowth
	p.parseMemoryBudgetDiag.runtimeSysGrowthBytes = sysGrowth
	return ParseStopMemoryBudget
}
