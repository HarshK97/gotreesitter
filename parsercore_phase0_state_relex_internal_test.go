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
