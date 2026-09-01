//go:build gts_parsercorephase0 && !gts_no_parsercorephase0

package gotreesitter_test

import (
	"fmt"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestDiagnosticParserCoreVersionLexerProductionActivation proves the
// production fresh-full scheduler preempts the shared ragged pass before the
// wide-token sibling reduces, while recording both owned lexer requests.
func TestDiagnosticParserCoreVersionLexerProductionActivation(t *testing.T) {
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entry := grammars.DetectLanguageByName("swift")
	if entry == nil || entry.Language() == nil {
		t.Fatal("swift grammar is unavailable")
	}
	receipt, err := gts.DiagnosticParserCoreVersionLexerProductionActivationForTest(
		entry.Language(), []byte("a<A<#"),
	)
	if err != nil {
		t.Fatalf("production scheduler: %v", err)
	}
	if !strings.HasPrefix(receipt.Stop.Detail, gts.DiagnosticParserCoreOwnedDispatchPendingDetailForTest()) {
		t.Fatalf("scheduler stop detail=%q, want owned-dispatch-pending activation", receipt.Stop.Detail)
	}
	if receipt.PerVersionLexRequests != 2 || receipt.PerVersionLexRestores != 2 ||
		receipt.PerVersionLexPublications != 6 || receipt.PerVersionLexAcceptedRaggedSpans != 0 {
		t.Fatalf("published lexer counters=%d/%d/%d/%d, want 2/2/6/0",
			receipt.PerVersionLexRequests, receipt.PerVersionLexRestores,
			receipt.PerVersionLexPublications, receipt.PerVersionLexAcceptedRaggedSpans)
	}
	if receipt.Stop.Work.PerVersionLexRequests != 2 || receipt.Stop.Work.PerVersionLexRestores != 2 ||
		receipt.Stop.Work.PerVersionLexPublications != 6 || receipt.Stop.Work.PerVersionLexAcceptedRaggedSpans != 0 {
		t.Fatalf("stop lexer counters=%d/%d/%d/%d, want 2/2/6/0",
			receipt.Stop.Work.PerVersionLexRequests, receipt.Stop.Work.PerVersionLexRestores,
			receipt.Stop.Work.PerVersionLexPublications, receipt.Stop.Work.PerVersionLexAcceptedRaggedSpans)
	}
	if receipt.PeakLiveVersions != 2 || receipt.Stop.Work.PeakLiveVersions != 2 {
		t.Fatalf("peak live versions=%d/%d, want 2/2", receipt.PeakLiveVersions, receipt.Stop.Work.PeakLiveVersions)
	}
	if got := receipt.Stop.Work.Reductions; got != 2 {
		t.Fatalf("shared reductions=%d, want 2 before state-524 reduction", got)
	}
	if receipt.Stop.ElectionIndex < 0 || receipt.Stop.ElectionIndex >= len(receipt.Elections) {
		t.Fatalf("stop election index=%d outside %d elections", receipt.Stop.ElectionIndex, len(receipt.Elections))
	}
	election := receipt.Elections[receipt.Stop.ElectionIndex]
	// Elections record the parser states at token-election start. The shared
	// state 10 reduces later in this same election into headers 47 and 524.
	if len(election.States) != 1 || election.States[0] != 10 {
		t.Fatalf("activation election index=%d states=%v, want initial state [10]", receipt.Stop.ElectionIndex, election.States)
	}
	if election.Token.Symbol != 217 || election.Token.StartByte != 3 || election.Token.EndByte != 5 || !election.Token.ExternalScannerToken {
		t.Fatalf("activation election token=%+v, want external 217 over bytes 3..5", election.Token)
	}
	if receipt.Stop.Token != election.Token || receipt.Stop.ElectionIndex != electionIndexForRequests(receipt.VersionLexerRequests) {
		t.Fatalf("stop/election identity: stop=(election=%d token=%+v), election token=%+v, request election=%d",
			receipt.Stop.ElectionIndex, receipt.Stop.Token, election.Token, electionIndexForRequests(receipt.VersionLexerRequests))
	}
	assertNineByteCheckpoint := func(label string, checkpoint gts.DiagnosticParserCoreScannerCheckpoint) {
		t.Helper()
		if checkpoint.Length != 9 {
			t.Fatalf("%s checkpoint length=%d, want exact Swift length 9", label, checkpoint.Length)
		}
		if checkpoint.SHA256 == [32]byte{} {
			t.Fatalf("%s checkpoint has an empty digest", label)
		}
	}
	assertNineByteCheckpoint("election scanner-before", election.ScannerBefore)
	assertNineByteCheckpoint("election scanner-after", election.ScannerAfter)
	if !election.CurrentCheckpointValid || election.CurrentCheckpointStart != election.ScannerBefore || election.CurrentCheckpointEnd != election.ScannerAfter {
		t.Fatalf("activation election lost scanner checkpoint identity: %+v", election)
	}
	if election.CurrentCheckpointBytes != [2]uint32{3, 5} {
		t.Fatalf("activation election checkpoint bytes=%v, want [3 5]", election.CurrentCheckpointBytes)
	}
	want := map[gts.StateID]struct {
		symbol   gts.Symbol
		endByte  uint32
		external bool
		internal bool
	}{
		47:  {symbol: 35, endByte: 4, internal: true},
		524: {symbol: 217, endByte: 5, external: true},
	}
	if len(receipt.VersionLexerRequests) != len(want) {
		t.Fatalf("owned lexer requests=%d, want %d", len(receipt.VersionLexerRequests), len(want))
	}
	requestByState := make(map[gts.StateID]gts.DiagnosticParserCoreVersionLexerRequest, len(want))
	for _, request := range receipt.VersionLexerRequests {
		expect, ok := want[request.State]
		if !ok {
			t.Fatalf("unexpected owned request state=%d", request.State)
		}
		if request.Token.Symbol != expect.symbol || request.Token.StartByte != 3 || request.Token.EndByte != expect.endByte {
			t.Fatalf("state %d token=%+v, want symbol=%d span=3..%d", request.State, request.Token, expect.symbol, expect.endByte)
		}
		if request.Token.ExternalScannerToken != expect.external || request.InternalDFAToken != expect.internal {
			t.Fatalf("state %d provenance external=%t/internal=%t, want external=%t/internal=%t", request.State, request.Token.ExternalScannerToken, request.InternalDFAToken, expect.external, expect.internal)
		}
		if request.ElectionIndex != receipt.Stop.ElectionIndex {
			t.Fatalf("state %d request election=%d, want activation election %d", request.State, request.ElectionIndex, receipt.Stop.ElectionIndex)
		}
		assertNineByteCheckpoint(fmt.Sprintf("state %d scanner-before", request.State), request.ScannerBefore)
		assertNineByteCheckpoint(fmt.Sprintf("state %d scanner-after", request.State), request.ScannerAfter)
		if request.ScannerBefore != election.ScannerBefore {
			t.Fatalf("state %d scanner-before=%+v, want shared election-before=%+v", request.State, request.ScannerBefore, election.ScannerBefore)
		}
		requestByState[request.State] = request
	}
	if len(requestByState) != len(want) {
		t.Fatalf("owned request state identities=%v, want states 47 and 524", requestByState)
	}
	if len(receipt.Stop.Headers) != 2 {
		t.Fatalf("stop headers=%d, want 2 activation headers", len(receipt.Stop.Headers))
	}
	for index, path := range receipt.Stop.Headers {
		header := path.Header
		request, ok := requestByState[header.State]
		if !ok {
			t.Fatalf("stop header %d state=%d has no matching owned request", index, header.State)
		}
		if header.CreationSeq != request.HeaderCreationSeq || header.ByteOffset != 3 {
			t.Fatalf("stop header %d identity=%+v, want request seq=%d at byte 3", index, header, request.HeaderCreationSeq)
		}
		if header.Checkpoint != election.ScannerAfter.SHA256 {
			t.Fatalf("stop header %d checkpoint=%x, want shared election-after=%x", index, header.Checkpoint, election.ScannerAfter.SHA256)
		}
	}
}

func electionIndexForRequests(requests []gts.DiagnosticParserCoreVersionLexerRequest) int {
	if len(requests) == 0 {
		return -1
	}
	index := requests[0].ElectionIndex
	for _, request := range requests[1:] {
		if request.ElectionIndex != index {
			return -1
		}
	}
	return index
}
