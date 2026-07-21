//go:build !gts_no_parsercorephase0

package gotreesitter

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"

	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

// parserCoreReplayParseStatesEnabled reports whether the compact route should
// reconstruct and stamp per-node parser states by table replay. It is gated by
// GTS_REPLAY_PARSESTATE so the states-free compact route can be A/B'd against
// the replay-materialized route. The value is latched once for stable A/B runs.
var (
	parserCoreReplayParseStatesOnce  int32
	parserCoreReplayParseStatesValue int32
)

func parserCoreReplayParseStatesEnabled() bool {
	if atomic.LoadInt32(&parserCoreReplayParseStatesOnce) == 0 {
		v := int32(0)
		switch os.Getenv("GTS_REPLAY_PARSESTATE") {
		case "1", "true", "on", "yes":
			v = 1
		}
		atomic.StoreInt32(&parserCoreReplayParseStatesValue, v)
		atomic.StoreInt32(&parserCoreReplayParseStatesOnce, 1)
	}
	return atomic.LoadInt32(&parserCoreReplayParseStatesValue) == 1
}

// setParserCoreReplayParseStatesForTest overrides the gate for differential
// tests without mutating process environment mid-run.
func setParserCoreReplayParseStatesForTest(on bool) {
	v := int32(0)
	if on {
		v = 1
	}
	atomic.StoreInt32(&parserCoreReplayParseStatesValue, v)
	atomic.StoreInt32(&parserCoreReplayParseStatesOnce, 1)
}

// ParseState-by-table-replay over the compact derivation (Phase-3 Lane 2).
//
// The visible production tree cannot reconstruct parseState because
// materialization erases the two things replay needs: hidden-node goto
// transitions (repeat/supertype aux symbols carry state between visible
// siblings) and the pre-alias grammar symbol (aliased leaves store the alias,
// not the terminal that was shifted). The compact derivation retains BOTH --
// every reduce keeps one record per RHS symbol, including hidden ones, with the
// real grammar symbol and an authoritative terminal flag -- so replay over the
// derivation, before hidden elision and aliasing, is exact.
//
// The pass is a single push-shaped top-down sweep over the derivation DAG,
// threading the LR state chain exactly as the parser did: root seeded from
// InitialState, first child inherits the parent's exposed state, each later
// child advances from the previous sibling's state. Results are stored per
// SubtreeID; the postorder materialization then stamps psByID/preByID onto the
// surviving public nodes (hidden ids are dropped, aliased ids keep the state
// computed from their real symbol).

// compactReplayFrame is one entry on the explicit worklist used by
// replayCompactDerivation to avoid deep Go recursion on pathologically deep
// derivations.
type compactReplayFrame struct {
	id      core.SubtreeID
	preGoto StateID
}

// compactReplayStates holds the reconstructed per-derivation-node parser
// states, indexed 1-based by SubtreeID (index 0 unused). The three per-id
// backing slices AND the traversal worklist (frames) are pooled across parses,
// so once the pool is warm the top-down pass adds no steady-state allocation to
// the materialization phase; release() returns them once stamping is done.
type compactReplayStates struct {
	parseState   []StateID
	preGotoState []StateID
	// psKnown[id] is true only when the top-down replay found an authoritative
	// shift/goto transition for id's parseState. preKnown[id] additionally
	// requires that id's preGotoState is authoritative: it is false for extra
	// (comment) nodes, whose tree position floats away from their live-parse
	// lex-time stack state, so their inherited preGoto is not reconstructable
	// even though their parseState is exact. When a flag is false the
	// corresponding state is NOT written (abstained): get() reports it so the
	// materializer leaves the node's state at its zero value
	// ("unknown -> recompute") rather than stamping a known-wrong but trusted
	// non-zero state (Phase-3 Lane 3 review amendment 1).
	psKnown  []bool
	preKnown []bool
	// frames is the pooled traversal worklist backing store, reused across
	// parses so the top-down pass is allocation-free once the pool is warm
	// (Phase-3 Lane 3 review amendment 6).
	frames []compactReplayFrame
}

var compactReplayStatePool = sync.Pool{New: func() any { return &compactReplayStates{} }}

func acquireCompactReplayStates(n int) *compactReplayStates {
	s := compactReplayStatePool.Get().(*compactReplayStates)
	if cap(s.parseState) < n {
		s.parseState = make([]StateID, n)
		s.preGotoState = make([]StateID, n)
		s.psKnown = make([]bool, n)
		s.preKnown = make([]bool, n)
	} else {
		s.parseState = s.parseState[:n]
		s.preGotoState = s.preGotoState[:n]
		s.psKnown = s.psKnown[:n]
		s.preKnown = s.preKnown[:n]
		for i := 0; i < n; i++ {
			s.parseState[i] = 0
			s.preGotoState[i] = 0
			s.psKnown[i] = false
			s.preKnown[i] = false
		}
	}
	return s
}

func (s *compactReplayStates) release() {
	if s == nil {
		return
	}
	compactReplayStatePool.Put(s)
}

// get returns the reconstructed states for id and whether each is authoritative.
// preOk / psOk are independent: an extra leaf has an exact parseState (psOk) but
// a non-reconstructable, floated preGotoState (preOk=false).
func (s *compactReplayStates) get(id core.SubtreeID) (pre, ps StateID, preOk, psOk bool) {
	if s == nil || uint64(id) >= uint64(len(s.parseState)) {
		return 0, 0, false, false
	}
	return s.preGotoState[id], s.parseState[id], s.preKnown[id], s.psKnown[id]
}

// replayCompactDerivation reconstructs parser states for every node in the
// accepted compact derivation rooted at roots. roots are the accepted payload
// ids in tree order; each is seeded with the parser's InitialState (the state
// exposed below the final reduce). The traversal uses an explicit worklist over
// SubtreeIDs to avoid deep recursion.
func (p *Parser) replayCompactDerivation(compact *core.Core, roots []core.SubtreeID) (*compactReplayStates, error) {
	if p == nil || p.language == nil || compact == nil {
		return nil, errors.New("parser-core phase zero: replay requires parser and core")
	}
	n := compact.SubtreeArenaLen() + 1
	states := acquireCompactReplayStates(n)
	seed := p.replayRootPreGotoState()

	stack := states.frames[:0]
	for _, root := range roots {
		stack = append(stack, compactReplayFrame{id: root, preGoto: seed})
	}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		view, err := compact.MaterializationView(top.id)
		if err != nil {
			// Return the pooled state object before bailing so the pool contract
			// is honoured (otherwise it is dropped and silently reallocated).
			states.frames = stack[:0]
			states.release()
			return nil, err
		}
		ps, ok := p.replayTransition(top.preGoto, Symbol(view.Symbol), view.Terminal)
		if ok && uint64(top.id) < uint64(len(states.parseState)) {
			// Record an AUTHORITATIVE parseState: replayTransition found a real
			// shift/goto for this id from top.preGoto. When ok is false the
			// transition fell back to top.preGoto (a node whose shape is not a
			// plain shift/goto of its visible symbol); recording that fallback
			// would stamp a known-wrong trusted state, so we abstain and leave
			// the id at its zero "unknown -> recompute" sentinel.
			states.parseState[top.id] = ps
			states.psKnown[top.id] = true
			// The preGotoState is authoritative only for non-extra nodes. Extras
			// (comments) float to a tree position that does not match their
			// live-parse stack state, so their inherited preGoto is not
			// reconstructable; abstain on it (leave 0) while keeping the exact
			// parseState above (Lane 3 review amendment 1).
			if !view.Extra {
				states.preGotoState[top.id] = top.preGoto
				states.preKnown[top.id] = true
			}
		}
		kids := view.Children
		if len(kids) == 0 {
			continue
		}
		if len(kids) == 1 {
			stack = append(stack, compactReplayFrame{id: kids[0], preGoto: top.preGoto})
			continue
		}
		base := len(stack)
		cursor := top.preGoto
		for _, child := range kids {
			cview, err := compact.MaterializationView(child)
			if err != nil {
				// Return the pooled state object before bailing (see above).
				states.frames = stack[:0]
				states.release()
				return nil, err
			}
			stack = append(stack, compactReplayFrame{id: child, preGoto: cursor})
			cursor, _ = p.replayTransition(cursor, Symbol(cview.Symbol), cview.Terminal)
		}
		// Reverse the just-appended child frames so pop order is left-to-right.
		for i, j := base, len(stack)-1; i < j; i, j = i+1, j-1 {
			stack[i], stack[j] = stack[j], stack[i]
		}
	}
	// Retain the (possibly grown) worklist backing store for the next parse.
	states.frames = stack[:0]
	return states, nil
}
