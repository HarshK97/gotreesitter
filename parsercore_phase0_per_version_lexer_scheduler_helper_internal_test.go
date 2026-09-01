//go:build gts_parsercorephase0

package gotreesitter

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

// DiagnosticParserCoreVersionLexerRequestWitnessForTest advances a scheduler
// to the Swift ragged-token frontier and exercises its private cursor requests.
// The helper stays in an internal test file so production callers cannot arm it.
func DiagnosticParserCoreVersionLexerRequestWitnessForTest(
	lang *Language,
	source []byte,
) ([]DiagnosticParserCoreVersionLexerRequest, error) {
	if lang == nil {
		return nil, fmt.Errorf("version lexer witness requires a language")
	}
	parser := NewParser(lang)
	runner, err := newAdmissionCandidateRunner(parser)
	if err != nil {
		return nil, err
	}
	runner.options.ReceiptMode = DiagnosticParserCoreReceiptFull
	tokenSource := parser.acquireParserDFATokenSource(source)
	if tokenSource == nil {
		return nil, fmt.Errorf("acquire parser DFA token source returned nil")
	}
	defer tokenSource.Close()

	tokenSource.SetParserState(lang.InitialState)
	tokenSource.SetGLRStates(nil)
	var scannerScratch []byte
	initialBytes := tokenSource.captureExternalScannerStateInto(&scannerScratch)
	initialID, initialInfo, err := diagnosticParserCoreInternCheckpoint(runner.compact, initialBytes)
	if err != nil {
		return nil, fmt.Errorf("intern initial scanner checkpoint: %w", err)
	}
	if err := runner.compact.SetPhaseCheckpoint(initialID); err != nil {
		return nil, fmt.Errorf("set initial scanner checkpoint phase: %w", err)
	}
	seed, err := runner.compact.Seed(core.StateID(lang.InitialState), 0)
	if err != nil {
		return nil, fmt.Errorf("seed compact parser core: %w", err)
	}
	scheduler, err := newDiagnosticParserCoreGenericScheduler(
		runner.compact, tokenSource, &scannerScratch, seed, initialID, initialInfo,
		diagnosticParserCoreSeedObserver{}, runner.options,
	)
	if err != nil {
		return nil, fmt.Errorf("new generic scheduler: %w", err)
	}
	if err := scheduler.captureSharedElectionSnapshot(); err != nil {
		return nil, fmt.Errorf("capture initial lexer election: %w", err)
	}
	if err := scheduler.elect(true); err != nil {
		return nil, fmt.Errorf("initial election: %w", err)
	}
	foundWitness := false
	for step := 0; step < 32; step++ {
		stop, dispatchErr := scheduler.dispatchPass()
		if dispatchErr != nil {
			return nil, fmt.Errorf("dispatch step %d: %w", step, dispatchErr)
		}
		if stop != nil {
			return nil, fmt.Errorf("scheduler stopped before the ragged witness: %+v", stop)
		}
		if len(scheduler.headers) == 2 && scheduler.token.StartByte == 3 && scheduler.token.EndByte == 5 {
			states := make([]StateID, len(scheduler.headers))
			for index, header := range scheduler.headers {
				state, byteOffset, boundaryErr := scheduler.compact.Boundary(header.head)
				if boundaryErr != nil {
					return nil, fmt.Errorf("read witness header %d boundary: %w", index, boundaryErr)
				}
				if byteOffset != 3 {
					return nil, fmt.Errorf("witness header %d byte offset=%d, want 3", index, byteOffset)
				}
				states[index] = StateID(state)
			}
			if states[0] == StateID(47) && states[1] == StateID(524) {
				foundWitness = true
				break
			}
		}
		allClosed := true
		for _, header := range scheduler.headers {
			if !header.shifted && !header.accepted {
				allClosed = false
				break
			}
		}
		if allClosed {
			if err := scheduler.captureSharedElectionSnapshot(); err != nil {
				return nil, fmt.Errorf("capture lexer election after dispatch step %d: %w", step, err)
			}
			if err := scheduler.elect(false); err != nil {
				return nil, fmt.Errorf("next election after dispatch step %d: %w", step, err)
			}
		}
	}
	if !foundWitness {
		return nil, fmt.Errorf("did not reach two-header byte-3 witness")
	}
	capturedElection := scheduler.versionLexerBeforeElection
	scheduler.versionLexerBeforeElection--
	if err := scheduler.seedVersionLexerOwnership(); err == nil {
		return nil, fmt.Errorf("stale lexer election snapshot was accepted")
	}
	scheduler.versionLexerBeforeElection = capturedElection
	if err := scheduler.seedVersionLexerOwnership(); err != nil {
		return nil, fmt.Errorf("seed per-header lexer ownership: %w", err)
	}
	if scheduler.headers[0].versionState != scheduler.headers[1].versionState {
		return nil, fmt.Errorf("equal lexer cursors did not share one version identity")
	}

	for index := range scheduler.headers {
		priorDFA := tokenSource.snapshotRelexState()
		priorState := tokenSource.state
		priorGLRStates := append([]StateID(nil), tokenSource.glrStates...)
		if err := scheduler.requestHeaderLexerToken(index); err != nil {
			return nil, fmt.Errorf("request header lexer token %d: %w", index, err)
		}
		if got := tokenSource.snapshotRelexState(); !reflect.DeepEqual(got, priorDFA) {
			return nil, fmt.Errorf("request %d changed the shared DFA cursor: got=%+v want=%+v", index, got, priorDFA)
		}
		if tokenSource.state != priorState {
			return nil, fmt.Errorf("request %d changed the shared parser state: got=%d want=%d", index, tokenSource.state, priorState)
		}
		if !reflect.DeepEqual(tokenSource.glrStates, priorGLRStates) {
			return nil, fmt.Errorf("request %d changed the shared GLR states: got=%v want=%v", index, tokenSource.glrStates, priorGLRStates)
		}
	}

	if len(scheduler.versionLexerRequests) != 2 {
		return nil, fmt.Errorf("owned lexer requests=%d, want 2", len(scheduler.versionLexerRequests))
	}
	scheduler.electionIndex++
	if request := scheduler.versionLexerRequestForHeader(0); request != nil {
		return nil, fmt.Errorf("owned lexer request survived its election")
	}
	scheduler.electionIndex--
	if scheduler.work.PerVersionLexRequests != 2 || scheduler.work.PerVersionLexRestores != 2 ||
		scheduler.work.PerVersionLexPublications != 6 || scheduler.work.PeakLiveVersions != 2 {
		return nil, fmt.Errorf("owned lexer work counters=%+v", scheduler.work)
	}
	scheduler.publishTotals()
	if scheduler.receipt.PerVersionLexRequests != 2 || scheduler.receipt.PerVersionLexRestores != 2 ||
		scheduler.receipt.PerVersionLexPublications != 6 || scheduler.receipt.PeakLiveVersions != 2 {
		return nil, fmt.Errorf("owned lexer receipt totals=%+v", scheduler.receipt)
	}
	want := map[StateID]struct {
		symbol      Symbol
		endByte     uint32
		external    bool
		internalDFA bool
	}{
		47:  {symbol: 35, endByte: 4, internalDFA: true},
		524: {symbol: 217, endByte: 5, external: true},
	}
	for _, request := range scheduler.versionLexerRequests {
		expect, ok := want[request.state]
		if !ok {
			return nil, fmt.Errorf("unexpected owned request state=%d", request.state)
		}
		if request.token.Symbol != expect.symbol || request.token.StartByte != 3 || request.token.EndByte != expect.endByte {
			return nil, fmt.Errorf("state %d token=%+v, want symbol=%d span=3..%d", request.state, request.token, expect.symbol, expect.endByte)
		}
		if request.token.ExternalScannerToken != expect.external || request.token.lexerInternalDFALexed != expect.internalDFA {
			return nil, fmt.Errorf("state %d token provenance external=%t internalDFA=%t, want external=%t internalDFA=%t", request.state, request.token.ExternalScannerToken, request.token.lexerInternalDFALexed, expect.external, expect.internalDFA)
		}
		if request.before == nil || request.after == nil || request.beforeCheckpoint.Length != 9 || request.afterCheckpoint.Length != 9 {
			return nil, fmt.Errorf("state %d request lost owned scanner checkpoints: %+v", request.state, request)
		}
		if request.before.dfa.lexerPos != 3 || request.after.dfa.lexerPos != int(expect.endByte) {
			return nil, fmt.Errorf("state %d cursor positions=%d..%d, want 3..%d", request.state, request.before.dfa.lexerPos, request.after.dfa.lexerPos, expect.endByte)
		}
	}
	return scheduler.receipt.VersionLexerRequests, nil
}

func TestDiagnosticParserCoreVersionLexerSidecarFootprintAndReset(t *testing.T) {
	compact, snapshot := newDiagnosticParserCoreVersionLexerTestSnapshot(t)
	requests := make([]diagnosticParserCoreVersionLexerRequest, 1, 3)
	requests[0] = diagnosticParserCoreVersionLexerRequest{before: snapshot, after: snapshot, valid: true}
	requests[:cap(requests)][2] = diagnosticParserCoreVersionLexerRequest{before: snapshot, after: snapshot, valid: true}
	scheduler := diagnosticParserCoreGenericScheduler{
		compact:                      compact,
		versionLexerBefore:           cloneDiagnosticParserCoreDFARelexSnapshot(snapshot.dfa),
		versionLexerBeforeValid:      true,
		versionLexerBeforeElection:   7,
		versionLexerBeforeCheckpoint: snapshot.beforeCheckpoint,
		versionLexerRequests:         requests,
	}
	base := diagnosticParserCoreSchedulerFootprintBytes(&diagnosticParserCoreGenericScheduler{compact: compact})
	delta := diagnosticParserCoreSchedulerFootprintBytes(&scheduler) - base
	want := uint64(cap(requests))*uint64(unsafe.Sizeof(diagnosticParserCoreVersionLexerRequest{})) +
		diagnosticParserCoreVersionLexerSnapshotFootprintBytes(snapshot) +
		diagnosticParserCoreDFARelexSnapshotRetainedBytes(scheduler.versionLexerBefore) +
		uint64(cap(scheduler.footprintRefs))*uint64(unsafe.Sizeof(diagnosticParserCoreFootprintRef{}))
	if delta != want {
		t.Fatalf("version lexer sidecar footprint=%d, want %d", delta, want)
	}
	if err := resetDiagnosticParserCoreGenericScheduler(&scheduler); err != nil {
		t.Fatalf("reset scheduler: %v", err)
	}
	for index, request := range requests[:cap(requests)] {
		if request.before != nil || request.after != nil || request.valid {
			t.Fatalf("request backing slot %d retained a snapshot: %+v", index, request)
		}
	}
	if scheduler.versionLexerBeforeValid || scheduler.versionLexerBeforeElection != 0 || scheduler.versionLexerBeforeCheckpoint != 0 ||
		scheduler.versionLexerBefore.externalPayload != nil ||
		scheduler.versionLexerBefore.externalTokenStart != nil || scheduler.versionLexerBefore.externalTokenEnd != nil ||
		scheduler.versionLexerBefore.extZeroTried != nil {
		t.Fatalf("reset retained the shared election snapshot: %+v", scheduler.versionLexerBefore)
	}
}
