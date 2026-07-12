package gotreesitter

import "testing"

// buildPrefixForkLanguage constructs a hand-built LR grammar with a genuine
// single-stack prefix reduce, followed by a real GLR reduce/reduce conflict
// (fork). It exists to differentially test the single-stack raw-shape
// capture elision (see gssScratch.mayElideRawShape /
// spore.2026-07-12.hazel.rawshape-elision-rca): the "pre" node is reduced
// while exactly one GLR stack is live (a candidate for elision), and it is
// then embedded as a child of both fork alternatives, so the fork's
// tie-break comparison (compareRawStackEntriesRec, via
// compareAcceptedStackRawShapePreference) walks back through it.
//
//	pre  -> x            (production 0, 1 child)   -- single-stack, deterministic
//	A    -> pre x         (production 1, 2 children) DynamicPrecedence 0
//	B    -> pre x         (production 2, 2 children) DynamicPrecedence 0
//
// Symbols: 0 EOF, 1 x (terminal), 2 pre, 3 A, 4 B, 5 y (terminal).
//
// States:
//
//	State 0 (start):        x -> shift(1); pre -> goto(2); A -> goto(4); B -> goto(4)
//	State 1 (saw x):        any -> reduce pre -> x (1 child)
//	State 2 (saw pre):      x -> shift(3)
//	State 3 (saw pre, x):   y -> FORK: reduce A->pre x (prod 1) OR reduce B->pre x (prod 2)
//	State 4 (saw A or B):   y -> shift(5)
//	State 5 (saw A/B, y):   EOF -> accept
//
// Feeding "x x y" shifts once, reduces "pre" deterministically while
// singleStackMode is true (never having forked yet), shifts the second x,
// and only then hits the reduce/reduce conflict that forks the stack on the
// still-pending "y" lookahead. The trailing "y" is deliberate: reduces do not
// consume the lookahead token, so without it the fork and both alternatives'
// accept would resolve within the single iteration that detects the
// conflict, and the peak stack count (sampled only at each outer loop
// iteration's top, see maxStacksSeen in parser.go) would never observe the
// fork. Requiring both alternatives to independently shift "y" forces a real
// iteration boundary (a fresh token acquisition) while 2 stacks are alive.
func buildPrefixForkLanguage() *Language {
	return &Language{
		Name:               "prefix_fork",
		SymbolCount:        6,
		TokenCount:         3,
		ExternalTokenCount: 0,
		StateCount:         6,
		LargeStateCount:    0,
		FieldCount:         0,
		ProductionIDCount:  3,

		SymbolNames: []string{"EOF", "x", "pre", "A", "B", "y"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "x", Visible: true, Named: true},
			{Name: "pre", Visible: true, Named: true},
			{Name: "A", Visible: true, Named: true},
			{Name: "B", Visible: true, Named: true},
			{Name: "y", Visible: true, Named: true},
		},
		FieldNames: []string{""},

		ParseActions: []ParseActionEntry{
			// 0: error / no action
			{Actions: nil},
			// 1: state0, col x: shift to state 1
			{Actions: []ParseAction{{Type: ParseActionShift, State: 1}}},
			// 2: state1, cols EOF/x/y: reduce pre -> x
			{Actions: []ParseAction{{Type: ParseActionReduce, Symbol: 2, ChildCount: 1, ProductionID: 0}}},
			// 3: state0, col pre: goto state 2
			{Actions: []ParseAction{{Type: ParseActionShift, State: 2}}},
			// 4: state2, col x: shift to state 3 (second x)
			{Actions: []ParseAction{{Type: ParseActionShift, State: 3}}},
			// 5: state0, col A: goto state 4
			{Actions: []ParseAction{{Type: ParseActionShift, State: 4}}},
			// 6: state0, col B: goto state 4
			{Actions: []ParseAction{{Type: ParseActionShift, State: 4}}},
			// 7: state3, col y: TWO actions -- GLR reduce/reduce fork
			{Actions: []ParseAction{
				{Type: ParseActionReduce, Symbol: 3, ChildCount: 2, ProductionID: 1, DynamicPrecedence: 0},
				{Type: ParseActionReduce, Symbol: 4, ChildCount: 2, ProductionID: 2, DynamicPrecedence: 0},
			}},
			// 8: state4, col y: shift to state 5
			{Actions: []ParseAction{{Type: ParseActionShift, State: 5}}},
			// 9: state5, col EOF: accept
			{Actions: []ParseAction{{Type: ParseActionAccept}}},
		},

		// Columns: EOF(0), x(1), pre(2), A(3), B(4), y(5)
		ParseTable: [][]uint16{
			// State 0
			{0, 1, 3, 5, 6, 0},
			// State 1: reduce pre->x regardless of lookahead
			{2, 2, 0, 0, 0, 2},
			// State 2: shift second x
			{0, 4, 0, 0, 0, 0},
			// State 3: fork on y
			{0, 0, 0, 0, 0, 7},
			// State 4: shift y
			{0, 0, 0, 0, 0, 8},
			// State 5: accept on EOF
			{9, 0, 0, 0, 0, 0},
		},

		LexModes: []LexMode{
			{LexState: 0},
			{LexState: 0},
			{LexState: 0},
			{LexState: 0},
			{LexState: 0},
			{LexState: 0},
		},

		LexStates: []LexState{
			// State 0: start
			{
				AcceptToken: 0,
				Skip:        false,
				Default:     -1,
				EOF:         -1,
				Transitions: []LexTransition{
					{Lo: 'x', Hi: 'x', NextState: 1},
					{Lo: 'y', Hi: 'y', NextState: 3},
					{Lo: ' ', Hi: ' ', NextState: 2},
				},
			},
			// State 1: accept x (symbol 1)
			{
				AcceptToken: 1,
				Skip:        false,
				Default:     -1,
				EOF:         -1,
			},
			// State 2: whitespace (skip)
			{
				AcceptToken: 0,
				Skip:        true,
				Default:     -1,
				EOF:         -1,
			},
			// State 3: accept y (symbol 5)
			{
				AcceptToken: 5,
				Skip:        false,
				Default:     -1,
				EOF:         -1,
			},
		},
	}
}

// TestPrefixForkLanguageActuallyForks instruments (via ParseRuntime, which
// this hand-built language reaches through the normal parseInternal loop,
// not any forest fast path) that "x x" genuinely drives more than one live
// GLR stack. This is the precondition the differential tests below depend
// on: without a real fork, they would pass vacuously.
func TestPrefixForkLanguageActuallyForks(t *testing.T) {
	lang := buildPrefixForkLanguage()
	tree := mustParse(t, NewParser(lang), []byte("x x y"))
	defer tree.Release()

	rt := tree.ParseRuntime()
	if rt.StopReason != ParseStopAccepted {
		t.Fatalf("StopReason = %v, want accepted", rt.StopReason)
	}
	if rt.MaxStacksSeen <= 1 {
		t.Fatalf("MaxStacksSeen = %d, want > 1 (test input must actually fork)", rt.MaxStacksSeen)
	}
	if rt.MultiStackIterations == 0 {
		t.Fatal("MultiStackIterations = 0, want > 0 (test input must actually fork)")
	}
}

// The zero-bytes-on-pure-single-stack and nonzero-bytes-on-forking
// counterparts live alongside the reclaim/accounting invariant in
// raw_shape_reclaim_test.go
// (TestParseReclaimsRawShapeStorageAccountsForZeroOnSingleStackParse and
// TestParseReclaimsRawShapeStorageAfterAccounting), which is the revision the
// RCA asked for of the pre-existing reclaim test. This file focuses on the
// gate-on-vs-gate-off differential, which is the load-bearing safety gate.

// TestRawShapeElisionDifferentialPureSingleStack is the gate-on-vs-gate-off
// differential for scenario (a): a pure single-stack parse must produce an
// identical tree whether or not the elision optimization is active.
func TestRawShapeElisionDifferentialPureSingleStack(t *testing.T) {
	lang := buildArithmeticLanguage()
	source := []byte("1+2+3+4")

	SetRawShapeElisionDisabledForDiagnostics(false)
	gateOn := mustParse(t, NewParser(lang), source)
	defer gateOn.Release()

	SetRawShapeElisionDisabledForDiagnostics(true)
	defer SetRawShapeElisionDisabledForDiagnostics(false)
	gateOff := mustParse(t, NewParser(lang), source)
	defer gateOff.Release()

	if got, want := gateOn.RootNode().SExpr(lang), gateOff.RootNode().SExpr(lang); got != want {
		t.Fatalf("gate-on tree = %s, gate-off (elision disabled) tree = %s, want identical", got, want)
	}
}

// TestRawShapeElisionDifferentialPrefixThenFork is the load-bearing
// differential gate the RCA asked for: a parse with a genuine single-stack
// prefix (the "pre" reduce, elided under the gate) followed by a real GLR
// fork (proven by TestPrefixForkLanguageActuallyForks) whose tie-break
// (compareAcceptedStackRawShapePreference / compareRawStackEntriesRec) walks
// back through that prefix node. The selected tree and the winning
// alternative (A vs B) must be identical whether the elision gate is on or
// forced off.
func TestRawShapeElisionDifferentialPrefixThenFork(t *testing.T) {
	lang := buildPrefixForkLanguage()
	source := []byte("x x y")

	SetRawShapeElisionDisabledForDiagnostics(false)
	gateOn := mustParse(t, NewParser(lang), source)
	defer gateOn.Release()
	gateOnRuntime := gateOn.ParseRuntime()

	SetRawShapeElisionDisabledForDiagnostics(true)
	defer SetRawShapeElisionDisabledForDiagnostics(false)
	gateOff := mustParse(t, NewParser(lang), source)
	defer gateOff.Release()
	gateOffRuntime := gateOff.ParseRuntime()

	if gateOnRuntime.MaxStacksSeen <= 1 || gateOffRuntime.MaxStacksSeen <= 1 {
		t.Fatalf("MaxStacksSeen gate-on=%d gate-off=%d, want both > 1 (fork precondition)", gateOnRuntime.MaxStacksSeen, gateOffRuntime.MaxStacksSeen)
	}
	if got, want := gateOn.RootNode().Symbol(), gateOff.RootNode().Symbol(); got != want {
		t.Fatalf("gate-on winning symbol = %d, gate-off winning symbol = %d, want identical tie-break selection", got, want)
	}
	if got, want := gateOn.RootNode().SExpr(lang), gateOff.RootNode().SExpr(lang); got != want {
		t.Fatalf("gate-on tree = %s, gate-off (elision disabled) tree = %s, want identical", got, want)
	}
}
