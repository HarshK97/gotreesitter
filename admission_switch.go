package gotreesitter

import (
	"os"
	"strings"
	"sync/atomic"
)

// Phase-3 dual-route admission switch.
//
// The switch selects, per full parse, between two engines:
//
//   - the production route: the mature GLR engine that Parser.Parse has always
//     used and that every reuse-consuming path (ParseIncremental and the
//     token-invariant leaf fast path) uses unconditionally; and
//   - the compact candidate route: the parsercorephase0 engine measured by
//     BenchmarkParserCoreFreshFullCanonical, which streams a compact derivation
//     into a materialized public tree.
//
// The switch only ever changes which engine serves a FRESH FULL parse. A
// compact result may later feed incremental reuse only when materialization
// attached a complete table-replay proof and scanner quiescence is proven;
// every other compact tree retains the hard fail-closed reuse bar. See
// admissionCandidateFullParseEligible and the materialization proof gate.
//
// Precedence, highest to lowest:
//
//  1. a per-Parser override set with (*Parser).SetAdmissionCandidateRoute;
//  2. the process-wide default set with SetAdmissionCandidateRouteDefault;
//  3. the process-wide default seeded from the GTS_ADMISSION_CANDIDATE
//     environment variable at package initialization;
//  4. ON.
//
// Phase-3 admission flipped the default to ON: the compact route is now the
// default full-parse route for eligible languages. Set GTS_ADMISSION_CANDIDATE=0
// (or false/off/no) to force the production route -- the documented escape
// hatch. An emergency build (-tags gts_no_parsercorephase0) compiles the
// compact engine out entirely and every eligible full parse then falls back to
// production.

// admissionRouteMode is the per-Parser override state. The zero value follows
// the process-wide default.
type admissionRouteMode uint8

const (
	admissionRouteFollowDefault admissionRouteMode = iota
	admissionRouteCandidateForced
	admissionRouteProductionForced
)

// admissionCandidateRouteDefault is the process-wide default the switch applies
// when a Parser sets no explicit override. init seeds it from the environment;
// SetAdmissionCandidateRouteDefault changes it at runtime.
var admissionCandidateRouteDefault atomic.Bool

// admissionCandidateRouted counts full parses served by the compact candidate
// route. admissionCandidateFallback counts eligible full parses that attempted
// the compact route, were declined, and fell back to production. A moving
// fallback counter is the loud, fail-closed signal that the candidate route
// declined an input rather than silently diverging.
var (
	admissionCandidateRouted   atomic.Uint64
	admissionCandidateFallback atomic.Uint64
)

// admissionCandidateLastFallbackReason records the most recent decline detail
// so an operator can triage why a full parse fell back to production.
var admissionCandidateLastFallbackReason atomic.Value // string

func init() {
	admissionCandidateRouteDefault.Store(admissionCandidateEnvEnabled())
}

// admissionCandidateEnvEnabled resolves the process-wide default the switch
// seeds from GTS_ADMISSION_CANDIDATE at package initialization.
//
// Phase-3 admission made the compact route the default full-parse route, so an
// unset or unrecognized value resolves ON. Only an explicit off value
// ("0", "false", "off", "no", any case) resolves OFF -- the documented escape
// hatch that forces every eligible full parse back to the production route.
func admissionCandidateEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GTS_ADMISSION_CANDIDATE"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// SetAdmissionCandidateRouteDefault sets the process-wide default the Phase-3
// admission switch applies to Parsers with no explicit override. A per-Parser
// override still wins.
//
// The candidate route does not enforce the automatic large-input memory budget
// at scheduler granularity, so the switch declines every input at or above the
// source-length floor where the production route arms that budget
// (parseRuntimeMemoryMinSourceBytes). Such inputs stay on production and honor
// ParseStopMemoryBudget. Explicit timeouts, cancellation flags, included ranges,
// and observability hooks also keep a parse on production.
func SetAdmissionCandidateRouteDefault(enabled bool) {
	admissionCandidateRouteDefault.Store(enabled)
}

// AdmissionCandidateRouteDefault reports the current process-wide default.
func AdmissionCandidateRouteDefault() bool {
	return admissionCandidateRouteDefault.Load()
}

// SetAdmissionCandidateRoute sets a per-Parser override that takes precedence
// over the process-wide default. enabled=true forces the candidate route on for
// eligible full parses; enabled=false forces the production route.
//
// enabled=true still respects every per-input eligibility decline: the switch
// declines inputs at or above the source-length floor where the production
// route arms the automatic memory budget (parseRuntimeMemoryMinSourceBytes), so
// large inputs stay on production and honor ParseStopMemoryBudget. Explicit
// timeouts, cancellation flags, included ranges, and observability hooks also
// keep a parse on production.
func (p *Parser) SetAdmissionCandidateRoute(enabled bool) {
	if p == nil {
		return
	}
	if enabled {
		p.admissionCandidateRoute = admissionRouteCandidateForced
	} else {
		p.admissionCandidateRoute = admissionRouteProductionForced
	}
}

// ClearAdmissionCandidateRoute drops the per-Parser override so the Parser
// follows the process-wide default again.
func (p *Parser) ClearAdmissionCandidateRoute() {
	if p != nil {
		p.admissionCandidateRoute = admissionRouteFollowDefault
	}
}

// AdmissionCandidateCounters returns the number of full parses the compact
// candidate route served, and the number of eligible full parses that fell back
// to production.
func AdmissionCandidateCounters() (routed, fallbacks uint64) {
	return admissionCandidateRouted.Load(), admissionCandidateFallback.Load()
}

// AdmissionCandidateLastFallbackReason returns the most recent candidate-route
// decline detail, or the empty string if none has been recorded.
func AdmissionCandidateLastFallbackReason() string {
	if v, ok := admissionCandidateLastFallbackReason.Load().(string); ok {
		return v
	}
	return ""
}

// resetAdmissionCandidateCounters clears the counters. Test-only.
func resetAdmissionCandidateCounters() {
	admissionCandidateRouted.Store(0)
	admissionCandidateFallback.Store(0)
	admissionCandidateLastFallbackReason.Store("")
}

// admissionCandidateRouteEnabled resolves the switch precedence for p.
func (p *Parser) admissionCandidateRouteEnabled() bool {
	if p == nil {
		return false
	}
	switch p.admissionCandidateRoute {
	case admissionRouteCandidateForced:
		return true
	case admissionRouteProductionForced:
		return false
	default:
		return admissionCandidateRouteDefault.Load()
	}
}

// admissionCandidateFullParseEligible reports whether a fresh full parse may be
// routed through the compact candidate. It fails closed for:
//
//   - every reuse-consuming call (oldTree != nil), because admission selects a
//     fresh-full engine only; incremental reuse is decided later from the old
//     tree's replay/scanner proof; and
//   - any lexer other than the production DFA, because the compact route
//     reproduces the production DFA token stream and cannot honor a caller-
//     supplied token source.
func (p *Parser) admissionCandidateFullParseEligible(oldTree *Tree, usingProductionDFA bool) bool {
	if p == nil || oldTree != nil || p.admissionRouteSuppressed > 0 {
		return false
	}
	if !usingProductionDFA {
		return false
	}
	// A language with an enabled, certified/explicit forest route already has a
	// more specific full-parse policy. Preserve that route's correctness and
	// incremental contracts instead of letting the generic compact candidate
	// preempt it merely because both are capable of accepting the same input.
	if p.admissionCandidateRoute == admissionRouteFollowDefault && glrForestEnabled && parserWantsForest(p) {
		return false
	}
	// Resolve the switch first. This keeps the shipped OFF hot path cheap: none
	// of the fidelity probes below (including the os.Getenv in the observability
	// check) run unless the switch is actually on for this parser.
	if !p.admissionCandidateRouteEnabled() {
		return false
	}
	// The compact runner lexes the whole source with its own DFA token source and
	// does not apply included ranges, so decline when a caller set any. Production
	// then honors the ranges exactly.
	if len(p.included) > 0 {
		return false
	}
	// Preserve liveness fidelity: the compact scheduler does not poll an explicit
	// timeout or cancellation flag, so decline when a caller set one. Production
	// then honors the deadline exactly. (The automatic large-input memory budget
	// is honored by the per-input size decline in attemptAdmissionCandidateFullParse,
	// which keeps every budget-eligible input on the production route.)
	if p.timeoutMicros != 0 || p.cancellationFlag != nil {
		return false
	}
	// Preserve callback fidelity: the compact route does not emit the parser's
	// logger, GLR-trace, ambiguity-profile, or parse-progress events, so decline
	// (fall back to production) whenever a consumer has attached one. Production
	// then fires every hook exactly as it does today.
	if p.hasActiveParseObservability() {
		return false
	}
	return true
}

// hasActiveParseObservability reports whether a consumer attached any parse-time
// observability hook that the compact route would not emit.
func (p *Parser) hasActiveParseObservability() bool {
	if p == nil {
		return false
	}
	if p.logger != nil || p.glrTrace || p.ambiguityProfile != nil {
		return true
	}
	return strings.TrimSpace(os.Getenv("GOT_PARSE_PROGRESS")) == "1"
}

// suppressAdmissionCandidateRoute forces the production route for the returned
// function's lifetime. It is nestable: reuse-consuming calls and production-only
// correctness reparses raise it around any delegated fresh Parse so that a
// compact tree can never leak out of a path that must stay on production.
func (p *Parser) suppressAdmissionCandidateRoute() func() {
	if p == nil {
		return func() {}
	}
	p.admissionRouteSuppressed++
	return func() { p.admissionRouteSuppressed-- }
}

// pinToProductionRoute permanently forces an internally-created sub-parser onto
// the production route, independent of the process-wide default. Recovery,
// snippet, and injection sub-parsers parse fragments that feed recovery splicing
// or injection subtrees -- contexts the admission scorecard never validated --
// so they must never route a compact tree even if the global default flips on.
func (p *Parser) pinToProductionRoute() {
	if p == nil {
		return
	}
	p.admissionCandidateRoute = admissionRouteProductionForced
	p.admissionRouteSuppressed = 0
	p.admissionCandidateRunner = nil
}

// attemptAdmissionCandidateFullParse routes a fresh full parse through the
// compact candidate when the switch is on and the call is eligible. It returns
// (tree, true) when the candidate route produced a public tree, and (nil,
// false) otherwise. On an eligible-but-declined attempt it bumps the fallback
// counter (fail-closed, loud) so the caller re-runs the production route.
func (p *Parser) attemptAdmissionCandidateFullParse(source []byte, oldTree *Tree, usingProductionDFA bool) (*Tree, bool) {
	if !p.admissionCandidateFullParseEligible(oldTree, usingProductionDFA) {
		return nil, false
	}
	// Decline any input large enough that the production route would arm the
	// automatic memory budget, which the compact scheduler cannot poll. This is
	// a never-attempted eligibility decline, not an engine decline, so it does
	// not move the fallback counter -- the parse simply stays on production.
	if !admissionCandidateInputSizeEligible(len(source)) {
		return nil, false
	}
	tree, ok, reason := p.tryCompactFullParseRoute(source)
	if ok && tree != nil {
		admissionCandidateRouted.Add(1)
		return tree, true
	}
	admissionCandidateFallback.Add(1)
	admissionCandidateLastFallbackReason.Store(reason)
	return nil, false
}

// admissionCandidateInputSizeEligible reports whether an input is small enough
// to route through the compact candidate without weakening the automatic
// large-input memory budget contract.
//
// The compact scheduler does not poll the automatic memory budget at scheduler
// granularity (the documented Phase-3 boundary), so a pathological large input
// that the production route would truncate with ParseStopMemoryBudget could
// instead parse to completion on the candidate route. The production route arms
// that budget (soft default 512 MiB and hard ceiling 2 GiB) only once a source
// reaches parseRuntimeMemoryMinSourceBytes (see runtimeMemoryBudgetEnabled and
// runtimeMemoryHardCeilingEnabled in parser_memory_budget_runtime.go). Declining
// every input at or above that same floor keeps every parse where the budget
// could engage on the production route, so routing preserves the budget
// contract exactly. Inputs below the floor never arm the budget on either
// route, and the compact arena caps (admissionCandidateLimits) bound their
// growth, so they route freely.
func admissionCandidateInputSizeEligible(sourceLen int) bool {
	return sourceLen < parseRuntimeMemoryMinSourceBytes
}
