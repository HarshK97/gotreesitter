package gotreesitter

import "runtime"

const (
	// Arena and parser scratch enforce their budgets at every materialization
	// boundary. The runtime-wide guard covers untracked Go heap growth, so a
	// coarser poll avoids repeatedly stopping the world on recovery-heavy
	// parses while keeping the maximum default-budget overshoot small.
	parseRuntimeMemoryPollMask       = 255
	parseRuntimeMemoryTightPollMask  = 15
	parseRuntimeMemoryTightBudget    = 64 << 20
	parseRuntimeMemoryMinSourceBytes = 64 * 1024

	parseMemoryBudgetStopSourceArena       = "arena"
	parseMemoryBudgetStopSourceScratch     = "scratch"
	parseMemoryBudgetStopSourceRuntimeHeap = "runtime_heap"
	parseMemoryBudgetStopSourceRuntimeSys  = "runtime_sys"
)

type parseMemoryBudgetDiagnostic struct {
	source                 string
	runtimeHeapGrowthBytes uint64
	runtimeSysGrowthBytes  uint64
}

type runtimeMemoryBudgetRestore struct {
	parser      *Parser
	budget      int64
	baseline    uint64
	baselineSys uint64
	poll        uint64
}

func runtimeMemoryBudgetEnabled(p *Parser, bytes int64, sourceLen int) bool {
	return p != nil && bytes > 0 && sourceLen >= parseRuntimeMemoryMinSourceBytes
}

func (p *Parser) enterRuntimeMemoryBudget(bytes int64, sourceLen int) runtimeMemoryBudgetRestore {
	if !runtimeMemoryBudgetEnabled(p, bytes, sourceLen) {
		return runtimeMemoryBudgetRestore{}
	}
	restore := runtimeMemoryBudgetRestore{
		parser:      p,
		budget:      p.parseRuntimeMemoryBudgetBytes,
		baseline:    p.parseRuntimeMemoryBaselineBytes,
		baselineSys: p.parseRuntimeMemoryBaselineSys,
		poll:        p.parseRuntimeMemoryPoll,
	}

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	p.parseRuntimeMemoryBudgetBytes = bytes
	p.parseRuntimeMemoryBaselineBytes = stats.HeapAlloc
	p.parseRuntimeMemoryBaselineSys = stats.Sys
	p.parseRuntimeMemoryPoll = 0

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
}

func (p *Parser) runtimeMemoryBudgetStopReason() ParseStopReason {
	if p == nil || p.parseRuntimeMemoryBudgetBytes <= 0 {
		return ParseStopNone
	}
	p.parseRuntimeMemoryPoll++
	if p.parseRuntimeMemoryPoll&runtimeMemoryPollMask(p.parseRuntimeMemoryBudgetBytes) != 0 {
		return ParseStopNone
	}
	return p.runtimeMemoryBudgetStopReasonNow()
}

func runtimeMemoryPollMask(budgetBytes int64) uint64 {
	if budgetBytes <= parseRuntimeMemoryTightBudget {
		return parseRuntimeMemoryTightPollMask
	}
	return parseRuntimeMemoryPollMask
}

func (p *Parser) runtimeMemoryBudgetStopReasonNow() ParseStopReason {
	if p == nil || p.parseRuntimeMemoryBudgetBytes <= 0 {
		return ParseStopNone
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	heapGrowth := runtimeMemoryGrowth(stats.HeapAlloc, p.parseRuntimeMemoryBaselineBytes)
	sysGrowth := runtimeMemoryGrowth(stats.Sys, p.parseRuntimeMemoryBaselineSys)
	budget := uint64(p.parseRuntimeMemoryBudgetBytes)
	if heapGrowth >= budget {
		return p.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceRuntimeHeap, heapGrowth, sysGrowth)
	}
	if sysGrowth >= budget {
		return p.noteRuntimeMemoryBudgetStop(parseMemoryBudgetStopSourceRuntimeSys, heapGrowth, sysGrowth)
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
