package gotreesitter

import "testing"

func TestDFARelexFailureRestoresThroughOuterTransaction(t *testing.T) {
	lang := &Language{
		ExternalScanner: byteStateExternalScanner{},
		LexStates: []LexState{
			{
				Default: -1,
				EOF:     -1,
				Transitions: []LexTransition{
					{Lo: ' ', Hi: ' ', NextState: 0, Skip: true},
					{Lo: 'a', Hi: 'a', NextState: 1},
				},
			},
			{AcceptToken: 1, Default: -1, EOF: -1},
		},
		LexModes: []LexMode{{LexState: 0}},
	}
	payload := lang.ExternalScanner.Create()
	*payload.(*byte) = 9
	ts := &dfaTokenSource{
		lexer:              NewLexer(lang.LexStates, []byte(" a")),
		language:           lang,
		externalPayload:    payload,
		hasExternalScanner: true,
		extZeroPos:         5,
		extZeroState:       7,
		extZeroTried:       []bool{true, false},
		zeroWidthPos:       9,
		zeroWidthCount:     3,
	}
	ts.lexer.pos = 2
	ts.lexer.row = 4
	ts.lexer.col = 6
	tok := Token{
		Symbol:     1,
		StartByte:  0,
		EndByte:    1,
		StartPoint: Point{},
		EndPoint:   Point{Column: 1},
	}

	snapshot := ts.snapshotRelexState()
	if got, ok := ts.relexFromTokenStartInTransaction(tok); ok {
		t.Fatalf("relexFromTokenStartInTransaction = (%+v, true), want false for skipped-start token", got)
	}
	if ts.lexer.pos == 2 && ts.lexer.row == 4 && ts.lexer.col == 6 {
		t.Fatal("failed probe restored itself; want the outer transaction to own rollback")
	}
	snapshot.restore(ts)

	if ts.lexer.pos != 2 || ts.lexer.row != 4 || ts.lexer.col != 6 {
		t.Fatalf("lexer = pos %d row %d col %d, want restored 2/4/6", ts.lexer.pos, ts.lexer.row, ts.lexer.col)
	}
	if got := *ts.externalPayload.(*byte); got != 9 {
		t.Fatalf("external payload = %d, want restored state 9", got)
	}
	if ts.extZeroPos != 5 || ts.extZeroState != 7 || len(ts.extZeroTried) != 2 || !ts.extZeroTried[0] || ts.extZeroTried[1] {
		t.Fatalf("external zero-width guard = pos %d state %d tried %v, want 5/7/[true false]", ts.extZeroPos, ts.extZeroState, ts.extZeroTried)
	}
	if ts.zeroWidthPos != 9 || ts.zeroWidthCount != 3 {
		t.Fatalf("zero-width guard = pos %d count %d, want 9/3", ts.zeroWidthPos, ts.zeroWidthCount)
	}
}

func TestDFATokenSourceRelexFailureRestoresProgressAndBookkeeping(t *testing.T) {
	lang := &Language{
		Name:            "python",
		SymbolNames:     []string{"end", "word"},
		ExternalScanner: byteStateExternalScanner{},
		LexStates: []LexState{
			{
				Default: -1,
				EOF:     -1,
				Transitions: []LexTransition{
					{Lo: ' ', Hi: ' ', NextState: 0, Skip: true},
					{Lo: 'a', Hi: 'z', NextState: 1},
				},
			},
			{
				AcceptToken: 1,
				Default:     -1,
				EOF:         -1,
			},
		},
		LexModes: []LexMode{{LexState: 0}},
	}
	externalPayload := lang.ExternalScanner.Create()
	*externalPayload.(*byte) = 9
	ts := &dfaTokenSource{
		lexer:                      NewLexer(lang.LexStates, []byte(" a")),
		language:                   lang,
		externalPayload:            externalPayload,
		hasExternalScanner:         true,
		usesExternalCheckpoints:    true,
		lastExternalTokenStartByte: 0,
		lastExternalTokenEndByte:   1,
		lastExternalTokenValid:     true,
		externalTokenStart:         []byte{2},
		externalTokenEnd:           []byte{9},
		extZeroPos:                 5,
		extZeroState:               7,
		extZeroTried:               []bool{true, false, true},
		zeroWidthPos:               9,
		zeroWidthCount:             3,
	}
	ts.lexer.pos = 2
	ts.lexer.row = 4
	ts.lexer.col = 6

	tok := Token{
		Symbol:     1,
		StartByte:  0,
		EndByte:    1,
		StartPoint: Point{},
		EndPoint:   Point{Column: 1},
	}
	if got, ok := ts.RelexFromTokenStart(tok); ok {
		t.Fatalf("RelexFromTokenStart = (%+v, true), want false for skipped-start token", got)
	}

	if ts.lexer.pos != 2 || ts.lexer.row != 4 || ts.lexer.col != 6 {
		t.Fatalf("lexer = pos %d row %d col %d, want 2/4/6", ts.lexer.pos, ts.lexer.row, ts.lexer.col)
	}
	if got := *ts.externalPayload.(*byte); got != 9 {
		t.Fatalf("external payload = %d, want restored post-token state 9", got)
	}
	if !ts.lastExternalTokenValid || ts.lastExternalTokenStartByte != 0 || ts.lastExternalTokenEndByte != 1 {
		t.Fatalf("last external token state = valid %t start %d end %d, want true/0/1",
			ts.lastExternalTokenValid, ts.lastExternalTokenStartByte, ts.lastExternalTokenEndByte)
	}
	if got, want := string(ts.externalTokenStart), string([]byte{2}); got != want {
		t.Fatalf("externalTokenStart = %v, want [2]", ts.externalTokenStart)
	}
	if got, want := string(ts.externalTokenEnd), string([]byte{9}); got != want {
		t.Fatalf("externalTokenEnd = %v, want [9]", ts.externalTokenEnd)
	}
	if ts.extZeroPos != 5 || ts.extZeroState != 7 {
		t.Fatalf("extZero = pos %d state %d, want 5/7", ts.extZeroPos, ts.extZeroState)
	}
	if len(ts.extZeroTried) != 3 || !ts.extZeroTried[0] || ts.extZeroTried[1] || !ts.extZeroTried[2] {
		t.Fatalf("extZeroTried = %v, want [true false true]", ts.extZeroTried)
	}
	if ts.zeroWidthPos != 9 || ts.zeroWidthCount != 3 {
		t.Fatalf("zeroWidth = pos %d count %d, want 9/3", ts.zeroWidthPos, ts.zeroWidthCount)
	}
}
