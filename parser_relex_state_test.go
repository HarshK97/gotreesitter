package gotreesitter

import "testing"

type recordingParserStateTokenSource struct {
	parserStates []StateID
	glrStates    [][]StateID
}

type zeroWidthExternalRelexTokenSource struct {
	relexed      Token
	parserStates []StateID
	glrStates    [][]StateID
}

func (s *zeroWidthExternalRelexTokenSource) Next() Token { return Token{} }

func (s *zeroWidthExternalRelexTokenSource) SetParserState(state StateID) {
	s.parserStates = append(s.parserStates, state)
}

func (s *zeroWidthExternalRelexTokenSource) SetGLRStates(states []StateID) {
	s.glrStates = append(s.glrStates, append([]StateID(nil), states...))
}

func (s *zeroWidthExternalRelexTokenSource) CanRelexFromTokenStart(Token) bool { return true }

func (s *zeroWidthExternalRelexTokenSource) RelexFromTokenStart(Token) (Token, bool) {
	return s.relexed, true
}

func (s *recordingParserStateTokenSource) Next() Token { return Token{} }

func (s *recordingParserStateTokenSource) SetParserState(state StateID) {
	s.parserStates = append(s.parserStates, state)
}

func (s *recordingParserStateTokenSource) SetGLRStates(states []StateID) {
	s.glrStates = append(s.glrStates, append([]StateID(nil), states...))
}

func TestUpdateCurrentRelexParserStateTokenSourceExcludesShiftedStacks(t *testing.T) {
	p := &Parser{}
	ts := &recordingParserStateTokenSource{}
	scratch := &parserScratch{}
	stacks := []glrStack{
		{entries: []stackEntry{{state: 10}}, shifted: true},
		{entries: []stackEntry{{state: 20}}},
		{entries: []stackEntry{{state: 30}}, shifted: true},
		{entries: []stackEntry{{state: 40}}},
	}

	if ok := p.updateCurrentRelexParserStateTokenSource(ts, stacks, scratch); !ok {
		t.Fatal("updateCurrentRelexParserStateTokenSource returned false, want true")
	}
	if got, want := len(ts.parserStates), 1; got != want {
		t.Fatalf("SetParserState calls = %d, want %d", got, want)
	}
	if got, want := ts.parserStates[0], StateID(20); got != want {
		t.Fatalf("parser state = %d, want first live unshifted state %d", got, want)
	}
	if got, want := len(ts.glrStates), 1; got != want {
		t.Fatalf("SetGLRStates calls = %d, want %d", got, want)
	}
	wantStates := []StateID{20, 40}
	if len(ts.glrStates[0]) != len(wantStates) {
		t.Fatalf("GLR states = %v, want %v", ts.glrStates[0], wantStates)
	}
	for i, want := range wantStates {
		if ts.glrStates[0][i] != want {
			t.Fatalf("GLR states = %v, want %v", ts.glrStates[0], wantStates)
		}
	}
}

func TestSameSurfaceRelexTokenRequiresSameSpanAndSurface(t *testing.T) {
	p := &Parser{language: &Language{
		SymbolNames: []string{"end", "<", "<", ">"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "end"},
			{Name: "<"},
			{Name: "<"},
			{Name: ">"},
		},
	}}
	original := Token{Symbol: 1, StartByte: 10, EndByte: 11}

	if !p.sameSurfaceRelexToken(original, Token{Symbol: 2, StartByte: 10, EndByte: 11}) {
		t.Fatal("same-surface duplicate token was not accepted")
	}
	if p.sameSurfaceRelexToken(original, Token{Symbol: 2, StartByte: 10, EndByte: 12}) {
		t.Fatal("same-surface token with different span was accepted")
	}
	if p.sameSurfaceRelexToken(original, Token{Symbol: 3, StartByte: 10, EndByte: 11}) {
		t.Fatal("different-surface token was accepted")
	}
}

func TestNoLiveStackCanAcceptLookaheadRequiresEveryEligibleVersionToReject(t *testing.T) {
	lang := &Language{
		StateCount:  4,
		SymbolCount: 3,
		ParseTable: [][]uint16{
			make([]uint16, 3),
			make([]uint16, 3),
			make([]uint16, 3),
			make([]uint16, 3),
		},
		ParseActions: []ParseActionEntry{
			{},
			{Actions: []ParseAction{{Type: ParseActionShift, State: 3}}},
		},
	}
	lang.ParseTable[2][1] = 1
	p := NewParser(lang)
	stacks := []glrStack{
		{entries: []stackEntry{{state: 1}}},
		{entries: []stackEntry{{state: 2}}},
	}
	tok := Token{Symbol: 1, StartByte: 10, EndByte: 11}

	if p.noLiveStackCanAcceptLookahead(stacks, tok) {
		t.Fatal("mixed frontier accepted replacement even though state 2 can consume the real lookahead")
	}
	stacks[1].shifted = true
	if !p.noLiveStackCanAcceptLookahead(stacks, tok) {
		t.Fatal("shifted versions should not block replacement for the remaining rejecting frontier")
	}
}

func TestTryRelexSingleParserStateAcceptsOnlyZeroWidthExternalShift(t *testing.T) {
	lang := &Language{
		StateCount:  4,
		SymbolCount: 3,
		ParseTable: [][]uint16{
			make([]uint16, 3),
			make([]uint16, 3),
			make([]uint16, 3),
			make([]uint16, 3),
		},
		ParseActions: []ParseActionEntry{
			{},
			{Actions: []ParseAction{{Type: ParseActionShift, State: 3}}},
		},
	}
	lang.ParseTable[1][2] = 1
	p := NewParser(lang)
	stacks := []glrStack{
		{entries: []stackEntry{{state: 1}}},
		{entries: []stackEntry{{state: 2}}},
	}
	original := Token{Symbol: 1, StartByte: 10, EndByte: 11}
	ts := &zeroWidthExternalRelexTokenSource{relexed: Token{
		Symbol:               2,
		StartByte:            10,
		EndByte:              10,
		ExternalScannerToken: true,
	}}

	got, ok := p.tryRelexSingleParserState(original, 1, ts, stacks, &parserScratch{})
	if !ok {
		t.Fatal("zero-width external shift was rejected")
	}
	if got.Symbol != 2 || got.StartByte != 10 || got.EndByte != 10 {
		t.Fatalf("relexed token = %+v, want zero-width external symbol 2 at byte 10", got)
	}
	if len(ts.parserStates) != 1 || ts.parserStates[0] != 1 {
		t.Fatalf("parser state calls = %v, want [1]", ts.parserStates)
	}
	if len(ts.glrStates) != 1 || len(ts.glrStates[0]) != 0 {
		t.Fatalf("GLR state calls = %v, want one cleared frontier", ts.glrStates)
	}

	ts.relexed.EndByte = 11
	if _, ok := p.tryRelexSingleParserState(original, 1, ts, stacks, &parserScratch{}); ok {
		t.Fatal("non-zero-width replacement was accepted")
	}
	if got := ts.glrStates[len(ts.glrStates)-1]; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("failed relex restored GLR states = %v, want [1 2]", got)
	}
}
