//go:build gts_workcount

package gotreesitter

import "math"

const (
	DiagnosticSemanticPhaseTraceContract  = "gts-semantic-phase-trace/v4"
	DiagnosticSemanticPhaseTraceMaxEvents = 8192
)

// DiagnosticSemanticPhaseTrace is a bounded, diagnostic-only ledger for
// locating repeated coarse action-cell classes after the parse table lookup.
// The coarse class deliberately excludes Go pointers and physical IDs, but is
// not a canonical predecessor-spine identity and may contain collisions.
type DiagnosticSemanticPhaseTrace struct {
	Contract           string                         `json:"contract"`
	MaxEvents          uint32                         `json:"max_events"`
	EventsSeen         uint64                         `json:"events_seen"`
	EventsDropped      uint64                         `json:"events_dropped"`
	ArithmeticOverflow bool                           `json:"arithmetic_overflow"`
	Events             []DiagnosticSemanticPhaseEvent `json:"events"`
}

// DiagnosticSemanticPhaseEvent identifies either an action-table lookup or a
// post-reduction convergence decision. Coarse boundary classes summarize only
// the current head; they are attribution hints, not cross-engine equality.
type DiagnosticSemanticPhaseEvent struct {
	Sequence uint64 `json:"sequence"`
	Kind     string `json:"kind"`

	TokenOrdinal uint64 `json:"token_ordinal"`
	Iteration    uint64 `json:"iteration"`
	ByteOffset   uint32 `json:"byte_offset"`
	State        uint32 `json:"state"`

	LookaheadSymbol    uint32 `json:"lookahead_symbol"`
	LookaheadStartByte uint32 `json:"lookahead_start_byte"`
	LookaheadEndByte   uint32 `json:"lookahead_end_byte"`
	LookaheadFlags     uint8  `json:"lookahead_flags"`

	ActionCellFingerprint  uint64 `json:"action_cell_fingerprint"`
	CoarseBoundaryClass    uint64 `json:"coarse_boundary_class"`
	CandidateBoundaryClass uint64 `json:"candidate_coarse_boundary_class,omitempty"`
	ActionOrdinal          int16  `json:"action_ordinal"`
	ActionType             int16  `json:"action_type"`

	Phase                string `json:"phase"`
	Outcome              string `json:"outcome"`
	Reason               string `json:"reason,omitempty"`
	CandidateCountBefore uint32 `json:"candidate_count_before"`
	CandidateCountAfter  uint32 `json:"candidate_count_after"`
}

var activeDiagnosticSemanticPhaseTrace *DiagnosticSemanticPhaseTrace

func semanticPhaseTraceActive() bool {
	return activeDiagnosticSemanticPhaseTrace != nil
}

// BeginDiagnosticSemanticPhaseTrace starts a parse-local trace. The trace is
// only available in gts_workcount builds and must be paired with
// BeginDiagnosticWorkCount when convergence decisions are required.
func BeginDiagnosticSemanticPhaseTrace() {
	if activeDiagnosticSemanticPhaseTrace != nil {
		panic("gotreesitter: diagnostic semantic-phase trace already active")
	}
	activeDiagnosticSemanticPhaseTrace = &DiagnosticSemanticPhaseTrace{
		Contract:  DiagnosticSemanticPhaseTraceContract,
		MaxEvents: DiagnosticSemanticPhaseTraceMaxEvents,
		Events:    make([]DiagnosticSemanticPhaseEvent, 0, DiagnosticSemanticPhaseTraceMaxEvents),
	}
}

// EndDiagnosticSemanticPhaseTrace returns the current trace and disables
// recording. All retained events own value data only.
func EndDiagnosticSemanticPhaseTrace() DiagnosticSemanticPhaseTrace {
	if activeDiagnosticSemanticPhaseTrace == nil {
		panic("gotreesitter: diagnostic semantic-phase trace is not active")
	}
	out := *activeDiagnosticSemanticPhaseTrace
	activeDiagnosticSemanticPhaseTrace = nil
	return out
}

func semanticPhaseTraceRecordActionCell(_ *Parser, stack *glrStack, state StateID, tok Token, actions []ParseAction) {
	trace := activeDiagnosticSemanticPhaseTrace
	if trace == nil {
		return
	}
	cellHash := semanticPhaseActionCellFingerprint(actions)
	boundaryClass := semanticPhaseCoarseBoundaryClass(stack)
	if len(actions) == 0 {
		semanticPhaseTraceAppend(DiagnosticSemanticPhaseEvent{
			Kind: "action_lookup", TokenOrdinal: activeWorkCountConvergence.electionOrdinal, Iteration: activeWorkCountConvergence.iteration,
			ByteOffset: semanticPhaseStackByte(stack), State: uint32(state),
			LookaheadSymbol: uint32(tok.Symbol), LookaheadStartByte: tok.StartByte, LookaheadEndByte: tok.EndByte,
			LookaheadFlags: semanticPhaseLookaheadFlags(tok), ActionCellFingerprint: cellHash,
			CoarseBoundaryClass: boundaryClass, ActionOrdinal: -1, ActionType: -1,
			Phase: "action_cell", Outcome: "empty",
		})
		return
	}
	for ordinal, action := range actions {
		ordinalValue := int16(ordinal)
		if ordinal > math.MaxInt16 {
			ordinalValue = math.MaxInt16
			trace.ArithmeticOverflow = true
		}
		semanticPhaseTraceAppend(DiagnosticSemanticPhaseEvent{
			Kind: "action_lookup", TokenOrdinal: activeWorkCountConvergence.electionOrdinal, Iteration: activeWorkCountConvergence.iteration,
			ByteOffset: semanticPhaseStackByte(stack), State: uint32(state),
			LookaheadSymbol: uint32(tok.Symbol), LookaheadStartByte: tok.StartByte, LookaheadEndByte: tok.EndByte,
			LookaheadFlags: semanticPhaseLookaheadFlags(tok), ActionCellFingerprint: cellHash,
			CoarseBoundaryClass: boundaryClass, ActionOrdinal: ordinalValue, ActionType: int16(action.Type),
			Phase: "action_cell", Outcome: "candidate",
		})
	}
}

func semanticPhaseTraceRecordActionExecution(p *Parser, stack *glrStack, tok Token, action ParseAction, actionOrdinal int, phase string, cycle bool) {
	if activeDiagnosticSemanticPhaseTrace == nil || p == nil || stack == nil || stack.depth() == 0 {
		return
	}
	state := stack.top().state
	actions := semanticPhaseActionsAt(p, state, tok.Symbol)
	cellHash := semanticPhaseActionCellFingerprint(actions)
	// -2 is emitted only by the production deterministic-conflict seam. Keep
	// uniqueness reconstruction entirely inside the diagnostic build so an
	// untagged parser executes no scan, call, or helper symbol for tracing.
	if actionOrdinal == -2 {
		actionOrdinal = semanticPhaseUniqueActionOrdinal(actions, action)
	}
	ordinal := int16(actionOrdinal)
	if actionOrdinal < 0 {
		ordinal = -1
	} else if actionOrdinal > math.MaxInt16 {
		ordinal = math.MaxInt16
		activeDiagnosticSemanticPhaseTrace.ArithmeticOverflow = true
	}
	outcome := "dispatched"
	if cycle {
		outcome = "cycle_rejected"
	}
	semanticPhaseTraceAppend(DiagnosticSemanticPhaseEvent{
		Kind: "action_execution", TokenOrdinal: activeWorkCountConvergence.electionOrdinal, Iteration: activeWorkCountConvergence.iteration,
		ByteOffset: semanticPhaseStackByte(stack), State: uint32(state),
		LookaheadSymbol: uint32(tok.Symbol), LookaheadStartByte: tok.StartByte, LookaheadEndByte: tok.EndByte,
		LookaheadFlags: semanticPhaseLookaheadFlags(tok), ActionCellFingerprint: cellHash,
		CoarseBoundaryClass: semanticPhaseCoarseBoundaryClass(stack), ActionOrdinal: ordinal, ActionType: int16(action.Type),
		Phase: phase, Outcome: outcome,
	})
}

func semanticPhaseUniqueActionOrdinal(actions []ParseAction, chosen ParseAction) int {
	ordinal := -1
	for index := range actions {
		if actions[index] != chosen {
			continue
		}
		if ordinal != -1 {
			return -1
		}
		ordinal = index
	}
	return ordinal
}

func semanticPhaseActionsAt(p *Parser, state StateID, symbol Symbol) []ParseAction {
	if p == nil || p.language == nil {
		return nil
	}
	var actionIndex uint16
	if int(state) < p.denseLimit {
		if int(state) >= len(p.language.ParseTable) || int(symbol) >= len(p.language.ParseTable[state]) {
			return nil
		}
		actionIndex = p.language.ParseTable[state][symbol]
	} else {
		actionIndex = p.lookupActionIndexSmall(state, symbol)
	}
	if actionIndex == 0 || int(actionIndex) >= len(p.language.ParseActions) {
		return nil
	}
	return p.language.ParseActions[actionIndex].Actions
}

func semanticPhaseTraceRecordDecision(_ *Parser, phase, outcome, reason string, target, candidate *glrStack, before, after int) {
	if activeDiagnosticSemanticPhaseTrace == nil {
		return
	}
	stack := target
	if stack == nil {
		stack = candidate
	}
	lookahead := activeWorkCountConvergence.lookahead
	beforeValue, beforeOverflow := semanticPhaseBoundedUint32(before)
	afterValue, afterOverflow := semanticPhaseBoundedUint32(after)
	if beforeOverflow || afterOverflow {
		activeDiagnosticSemanticPhaseTrace.ArithmeticOverflow = true
	}
	semanticPhaseTraceAppend(DiagnosticSemanticPhaseEvent{
		Kind: "decision", TokenOrdinal: activeWorkCountConvergence.electionOrdinal, Iteration: activeWorkCountConvergence.iteration,
		ByteOffset: semanticPhaseStackByte(stack), State: uint32(semanticPhaseStackState(stack)),
		LookaheadSymbol: uint32(lookahead.Symbol), LookaheadStartByte: lookahead.StartByte, LookaheadEndByte: lookahead.EndByte,
		LookaheadFlags: semanticPhaseLookaheadFlags(lookahead), ActionCellFingerprint: 0,
		CoarseBoundaryClass: semanticPhaseCoarseBoundaryClass(target), CandidateBoundaryClass: semanticPhaseCoarseBoundaryClass(candidate),
		ActionOrdinal: -1, ActionType: -1, Phase: phase, Outcome: outcome, Reason: reason,
		CandidateCountBefore: beforeValue, CandidateCountAfter: afterValue,
	})
}

func semanticPhaseTraceAppend(event DiagnosticSemanticPhaseEvent) {
	trace := activeDiagnosticSemanticPhaseTrace
	if trace == nil {
		return
	}
	if trace.EventsSeen == math.MaxUint64 {
		trace.ArithmeticOverflow = true
	} else {
		trace.EventsSeen++
	}
	event.Sequence = trace.EventsSeen
	if len(trace.Events) < int(trace.MaxEvents) {
		trace.Events = append(trace.Events, event)
		return
	}
	if trace.EventsDropped == math.MaxUint64 {
		trace.ArithmeticOverflow = true
	} else {
		trace.EventsDropped++
	}
}

func semanticPhaseActionCellFingerprint(actions []ParseAction) uint64 {
	h := semanticPhaseHashWord(semanticPhaseHashSeed, uint64(len(actions)))
	for _, action := range actions {
		h = semanticPhaseHashWord(h, uint64(action.Type))
		h = semanticPhaseHashWord(h, uint64(action.State))
		h = semanticPhaseHashWord(h, uint64(action.Symbol))
		h = semanticPhaseHashWord(h, uint64(action.ChildCount))
		h = semanticPhaseHashWord(h, uint64(uint16(action.DynamicPrecedence)))
		h = semanticPhaseHashWord(h, uint64(action.ProductionID))
		var flags uint64
		if action.Extra {
			flags |= 1
		}
		if action.ExtraChain {
			flags |= 2
		}
		if action.Repetition {
			flags |= 4
		}
		h = semanticPhaseHashWord(h, flags)
	}
	return h
}

func semanticPhaseCoarseBoundaryClass(stack *glrStack) uint64 {
	if stack == nil {
		return 0
	}
	h := semanticPhaseHashWord(semanticPhaseHashSeed, uint64(semanticPhaseStackByte(stack)))
	h = semanticPhaseHashWord(h, uint64(stack.depth()))
	if stack.depth() == 0 {
		return h
	}
	entry := stack.top()
	h = semanticPhaseHashWord(h, uint64(entry.state))
	h = semanticPhaseHashWord(h, uint64(stackEntryNodeSymbol(entry)))
	h = semanticPhaseHashWord(h, uint64(stackEntryNodeStartByte(entry))<<32|uint64(stackEntryNodeEndByte(entry)))
	h = semanticPhaseHashWord(h, uint64(stackEntryNodeChildCount(entry)))
	h = semanticPhaseHashWord(h, uint64(stackEntryNodeProductionID(entry)))
	h = semanticPhaseHashWord(h, uint64(uint32(stackEntryDynamicPrecedence(entry))))
	var flags uint64
	if stackEntryNodeIsExtra(entry) {
		flags |= 1
	}
	if stackEntryNodeIsMissing(entry) {
		flags |= 2
	}
	if stackEntryNodeHasError(entry) {
		flags |= 4
	}
	if stack.dead {
		flags |= 8
	}
	if stack.accepted {
		flags |= 16
	}
	if stack.cPaused {
		flags |= 32
	}
	h = semanticPhaseHashWord(h, flags)
	return h
}

func semanticPhaseStackState(stack *glrStack) StateID {
	if stack == nil || stack.depth() == 0 {
		return 0
	}
	return stack.top().state
}

func semanticPhaseStackByte(stack *glrStack) uint32 {
	if stack == nil {
		return 0
	}
	return stack.byteOffset
}

func semanticPhaseLookaheadFlags(tok Token) uint8 {
	var flags uint8
	if tok.NoLookahead {
		flags |= 1
	}
	if tok.ExternalScannerToken {
		flags |= 2
	}
	return flags
}

func semanticPhaseBoundedUint32(value int) (uint32, bool) {
	if value < 0 {
		return 0, true
	}
	if uint64(value) > math.MaxUint32 {
		return math.MaxUint32, true
	}
	return uint32(value), false
}

const (
	semanticPhaseHashSeed  uint64 = 1469598103934665603
	semanticPhaseHashPrime uint64 = 1099511628211
)

func semanticPhaseHashWord(hash, value uint64) uint64 {
	for shift := uint(0); shift < 64; shift += 8 {
		hash ^= (value >> shift) & 0xff
		hash *= semanticPhaseHashPrime
	}
	return hash
}
