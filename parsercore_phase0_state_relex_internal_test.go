//go:build gts_parsercorephase0

package gotreesitter

import "testing"

func TestDiagnosticParserCoreRelexedSymbolReconstructsExactToken(t *testing.T) {
	shared := Token{
		Symbol:                   3,
		Text:                     "token",
		StartByte:                7,
		EndByte:                  12,
		StartPoint:               Point{Row: 2, Column: 4},
		EndPoint:                 Point{Row: 2, Column: 9},
		ExternalScannerToken:     true,
		ExternalScannerStartByte: 5,
	}
	relexed := shared
	relexed.Symbol = 8
	relexed.ExternalScannerToken = false
	relexed.ExternalScannerStartByte = 0

	symbol, ok := diagnosticParserCoreRelexedSymbol(shared, relexed)
	if !ok || symbol != relexed.Symbol {
		t.Fatalf("relexed symbol = %d/%t, want %d/true", symbol, ok, relexed.Symbol)
	}
	cell := diagnosticParserCoreGenericCell{relexedSymbol: symbol}
	if got := cell.dispatchToken(shared); got != relexed {
		t.Fatalf("reconstructed token = %+v, want %+v", got, relexed)
	}
}

// TestDispatchTokenClearsIsKeywordOnRelexedSymbolOverride pins finding F7:
// when relexedSymbol overrides the shared token's symbol, dispatchToken must
// clear isKeyword alongside the external-scanner fields. isKeyword records
// that the ORIGINAL symbol was reached through the keyword-adoption
// promotion path; that fact does not carry over to an unrelated relexed
// symbol replacing it, so a dispatch view built from a promoted keyword
// token must not still claim the relexed token is a keyword adoption.
func TestDispatchTokenClearsIsKeywordOnRelexedSymbolOverride(t *testing.T) {
	shared := Token{
		Symbol:                   2,
		Text:                     "if",
		StartByte:                0,
		EndByte:                  2,
		EndPoint:                 Point{Column: 2},
		ExternalScannerToken:     true,
		ExternalScannerStartByte: 1,
		isKeyword:                true,
	}
	cell := diagnosticParserCoreGenericCell{relexedSymbol: 9}

	got := cell.dispatchToken(shared)
	if got.isKeyword {
		t.Fatal("dispatchToken with a relexedSymbol override: isKeyword = true, want false")
	}
	if got.Symbol != 9 {
		t.Fatalf("dispatchToken symbol = %d, want 9 (the relexed symbol)", got.Symbol)
	}
	if got.ExternalScannerToken || got.ExternalScannerStartByte != 0 {
		t.Fatalf("dispatchToken external-scanner fields = %v/%d, want false/0",
			got.ExternalScannerToken, got.ExternalScannerStartByte)
	}
}

// TestDispatchTokenPreservesIsKeywordWithoutRelexedSymbolOverride pins the
// other half of finding F7: dispatchToken must leave isKeyword untouched
// when there is no relexedSymbol override at all (cell.relexedSymbol == 0),
// so the clear above is scoped to the override branch only.
func TestDispatchTokenPreservesIsKeywordWithoutRelexedSymbolOverride(t *testing.T) {
	shared := Token{Symbol: 2, isKeyword: true}
	cell := diagnosticParserCoreGenericCell{}

	got := cell.dispatchToken(shared)
	if !got.isKeyword {
		t.Fatal("dispatchToken without a relexedSymbol override: isKeyword = false, want true (unmodified)")
	}
	if got != shared {
		t.Fatalf("dispatchToken without an override = %+v, want unmodified %+v", got, shared)
	}
}

func TestDiagnosticParserCoreRelexedSymbolRejectsTokenFieldChanges(t *testing.T) {
	shared := Token{
		Symbol:                   3,
		Text:                     "token",
		StartByte:                7,
		EndByte:                  12,
		StartPoint:               Point{Row: 2, Column: 4},
		EndPoint:                 Point{Row: 2, Column: 9},
		ExternalScannerToken:     true,
		ExternalScannerStartByte: 5,
	}
	exact := shared
	exact.Symbol = 8
	exact.ExternalScannerToken = false
	exact.ExternalScannerStartByte = 0

	tests := []struct {
		name   string
		change func(*Token)
	}{
		{name: "zero symbol", change: func(token *Token) { token.Symbol = 0 }},
		{name: "shared symbol", change: func(token *Token) { token.Symbol = shared.Symbol }},
		{name: "text", change: func(token *Token) { token.Text = "other" }},
		{name: "start byte", change: func(token *Token) { token.StartByte++ }},
		{name: "end byte", change: func(token *Token) { token.EndByte++ }},
		{name: "start row", change: func(token *Token) { token.StartPoint.Row++ }},
		{name: "start column", change: func(token *Token) { token.StartPoint.Column++ }},
		{name: "end row", change: func(token *Token) { token.EndPoint.Row++ }},
		{name: "end column", change: func(token *Token) { token.EndPoint.Column++ }},
		{name: "missing", change: func(token *Token) { token.Missing = true }},
		{name: "no lookahead", change: func(token *Token) { token.NoLookahead = true }},
		{name: "external token", change: func(token *Token) { token.ExternalScannerToken = true }},
		{name: "external start", change: func(token *Token) { token.ExternalScannerStartByte = 5 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			relexed := exact
			test.change(&relexed)
			if symbol, ok := diagnosticParserCoreRelexedSymbol(shared, relexed); ok || symbol != 0 {
				t.Fatalf("relexed symbol = %d/%t, want 0/false; token=%+v", symbol, ok, relexed)
			}
		})
	}
}

func RunStateDependentRelexSchedulerForTest(lang *Language, source []byte) (DiagnosticParserCoreGenericScheduler, error) {
	parser := NewParser(lang)
	runner, err := newAdmissionCandidateRunner(parser)
	if err != nil {
		return DiagnosticParserCoreGenericScheduler{}, err
	}
	runner.options.ReceiptMode = DiagnosticParserCoreReceiptFull
	tokenSource := parser.acquireParserDFATokenSource(source)
	if tokenSource == nil {
		return DiagnosticParserCoreGenericScheduler{}, nil
	}
	defer tokenSource.Close()
	scheduler, runErr := executeDiagnosticParserCoreGenericSchedulerFromSeed(
		runner.compact,
		tokenSource,
		&runner.scannerScratch,
		lang.InitialState,
		runner.options,
		diagnosticParserCoreSeedObserver{},
	)
	if scheduler == nil || scheduler.receipt == nil {
		return DiagnosticParserCoreGenericScheduler{}, runErr
	}
	return *scheduler.receipt, runErr
}
