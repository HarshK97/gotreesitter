package parsercorephase0

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func newAncestorRecoveryTestCore(t testing.TB, tables *fakeTable, limits Limits) *Core {
	t.Helper()
	if limits.MaxDerivations == 0 {
		limits.MaxDerivations = 32
	}
	if limits.MaxPopPaths == 0 {
		limits.MaxPopPaths = 32
	}
	compact, err := New(tables, limits)
	if err != nil {
		t.Fatal(err)
	}
	compact.diagnostics.foldSamePredecessorShallowPayloads = false
	return compact
}

func appendAncestorRecoveryPayload(t testing.TB, compact *Core, symbol Symbol, startByte, endByte uint32, extra bool) SubtreeID {
	t.Helper()
	payload, err := compact.appendSubtree(subtreeRecord{
		symbol: symbol, startByte: startByte, endByte: endByte, extra: extra, terminal: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func appendAncestorRecoveryHead(t testing.TB, compact *Core, head Head, state StateID, payload SubtreeID) Head {
	t.Helper()
	record, err := compact.subtree(payload)
	if err != nil {
		t.Fatal(err)
	}
	out, err := compact.appendPrivate(state, record.endByte, linkInput{prev: head.Node, payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStackSummaryCandidatesVisitFanoutInStableDepthOrder(t *testing.T) {
	const lookahead = Symbol(9)
	tables := &fakeTable{actions: map[tableCell][]Action{
		{state: 11, symbol: lookahead}: {{Type: ActionShift, State: 31}},
		{state: 12, symbol: lookahead}: {{Type: ActionShift, State: 32}},
		{state: 21, symbol: lookahead}: {{Type: ActionShift, State: 33}},
	}}
	compact := newAncestorRecoveryTestCore(t, tables, Limits{})
	seed, err := compact.Seed(21, 0)
	if err != nil {
		t.Fatal(err)
	}
	left := appendAncestorRecoveryHead(t, compact, seed, 11,
		appendAncestorRecoveryPayload(t, compact, 1, 0, 1, false))
	right := appendAncestorRecoveryHead(t, compact, seed, 12,
		appendAncestorRecoveryPayload(t, compact, 2, 0, 1, false))
	leftTop := appendAncestorRecoveryPayload(t, compact, 3, 1, 2, false)
	rightTop := appendAncestorRecoveryPayload(t, compact, 4, 1, 2, false)
	key := compact.shiftedBoundaryKey(30, 2)
	head, err := compact.condense(key, linkInput{prev: left.Node, payload: leftTop})
	if err != nil {
		t.Fatal(err)
	}
	head, err = compact.condense(key, linkInput{prev: right.Node, payload: rightTop})
	if err != nil {
		t.Fatal(err)
	}

	before := captureSchedulerTransactionState(compact)
	candidates, err := compact.StackSummaryCandidates(head, lookahead, 2)
	if err != nil {
		t.Fatalf("StackSummaryCandidates: %v", err)
	}
	after := captureSchedulerTransactionState(compact)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("candidate enumeration changed compact storage: before=%+v after=%+v", before, after)
	}
	want := [][2]int{{1, 11}, {1, 12}, {2, 21}}
	if len(candidates) != len(want) {
		t.Fatalf("candidate count=%d, want %d: %+v", len(candidates), len(want), candidates)
	}
	for index, candidate := range candidates {
		if candidate.Depth() != want[index][0] || candidate.State() != StateID(want[index][1]) {
			t.Fatalf("candidate[%d]=depth %d state %d, want depth %d state %d",
				index, candidate.Depth(), candidate.State(), want[index][0], want[index][1])
		}
	}
	if candidates[2].ByteOffset() != 0 {
		t.Fatalf("deduplicated depth-2 position=%d, want 0", candidates[2].ByteOffset())
	}
	exists, err := compact.AncestorStateWithActionExists(head, lookahead, 2)
	if err != nil || !exists {
		t.Fatalf("compatibility wrapper exists=%t err=%v, want true", exists, err)
	}
	exists, err = compact.AncestorStateWithActionExists(head, 10, 2)
	if err != nil || exists {
		t.Fatalf("empty compatibility wrapper exists=%t err=%v, want false", exists, err)
	}
}

func TestStackSummaryCandidatesEnforceDepthAndRecordSize(t *testing.T) {
	if got := unsafe.Sizeof(StackSummaryCandidate{}); got != 32 {
		t.Fatalf("StackSummaryCandidate size=%d, want 32", got)
	}
	compact := newAncestorRecoveryTestCore(t, &fakeTable{}, Limits{})
	seed, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if candidates, err := compact.StackSummaryCandidates(seed, 9, 0); err != nil || candidates != nil {
		t.Fatalf("zero-depth candidates=%v err=%v, want nil", candidates, err)
	}
	if _, err := compact.StackSummaryCandidates(seed, 9, StackSummaryMaxDepth+1); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("over-depth error=%v", err)
	}
}

func TestRecoverToAncestorStateRejectsAmbiguousDeduplicatedCandidate(t *testing.T) {
	const lookahead = Symbol(9)
	tables := &fakeTable{actions: map[tableCell][]Action{
		{state: 7, symbol: lookahead}: {{Type: ActionShift, State: 8}},
	}}
	compact := newAncestorRecoveryTestCore(t, tables, Limits{})
	left, err := compact.Seed(7, 0)
	if err != nil {
		t.Fatal(err)
	}
	right, err := compact.Seed(7, 1)
	if err != nil {
		t.Fatal(err)
	}
	key := compact.shiftedBoundaryKey(20, 2)
	head, err := compact.condense(key, linkInput{
		prev: left.Node, payload: appendAncestorRecoveryPayload(t, compact, 1, 0, 2, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	head, err = compact.condense(key, linkInput{
		prev: right.Node, payload: appendAncestorRecoveryPayload(t, compact, 2, 1, 2, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := compact.StackSummaryCandidates(head, lookahead, 1)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("deduplicated candidates=%+v err=%v, want one", candidates, err)
	}

	before := captureSchedulerTransactionState(compact)
	err = compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		_, innerErr := compact.RecoverToAncestorStateOwned(owner, candidates[0])
		return innerErr
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous pop paths") {
		t.Fatalf("ambiguous recovery error=%v", err)
	}
	if after := captureSchedulerTransactionState(compact); !reflect.DeepEqual(after, before) {
		t.Fatalf("ambiguous recovery changed storage: before=%+v after=%+v", before, after)
	}
	assertTransactionJournalClean(t, compact)
}

func TestRecoverToAncestorStateRejectsLimitsAndRollsBackPublication(t *testing.T) {
	const lookahead = Symbol(9)
	tables := &fakeTable{actions: map[tableCell][]Action{
		{state: 1, symbol: lookahead}: {{Type: ActionShift, State: 2}},
	}}
	compact := newAncestorRecoveryTestCore(t, tables, Limits{})
	seed, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	head := appendAncestorRecoveryHead(t, compact, seed, 3,
		appendAncestorRecoveryPayload(t, compact, 1, 0, 1, false))
	candidates, err := compact.StackSummaryCandidates(head, lookahead, 1)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%+v err=%v, want one", candidates, err)
	}

	compact.limits.MaxNodes = uint32(len(compact.nodes))
	before := captureSchedulerTransactionState(compact)
	err = compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		if _, innerErr := compact.RecoverToAncestorStateOwned(owner, candidates[0]); innerErr == nil {
			t.Fatal("node-capped recovery succeeded")
		}
		return nil // Prove that owner poisoning still forces rollback.
	})
	if err == nil || !strings.Contains(err.Error(), "poisoned scheduler transaction") || !strings.Contains(err.Error(), "node arena cap") {
		t.Fatalf("node-capped recovery error=%v", err)
	}
	if after := captureSchedulerTransactionState(compact); !reflect.DeepEqual(after, before) {
		t.Fatalf("node-capped recovery changed storage: before=%+v after=%+v", before, after)
	}
	assertTransactionJournalClean(t, compact)
}

func TestRecoverToAncestorStateOuterRollbackRestoresArenasAndJournal(t *testing.T) {
	const lookahead = Symbol(9)
	tables := &fakeTable{actions: map[tableCell][]Action{
		{state: 1, symbol: lookahead}: {{Type: ActionShift, State: 2}},
	}}
	compact := newAncestorRecoveryTestCore(t, tables, Limits{})
	seed, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	head := appendAncestorRecoveryHead(t, compact, seed, 3,
		appendAncestorRecoveryPayload(t, compact, 1, 0, 1, false))
	candidates, err := compact.StackSummaryCandidates(head, lookahead, 1)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%+v err=%v, want one", candidates, err)
	}

	before := captureSchedulerTransactionState(compact)
	sentinel := errors.New("force outer recovery rollback")
	err = compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		if _, innerErr := compact.RecoverToAncestorStateOwned(owner, candidates[0]); innerErr != nil {
			return innerErr
		}
		if len(compact.boundaryJournal) == 0 {
			t.Fatal("successful recovery did not journal its boundary publication")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("outer rollback error=%v, want sentinel", err)
	}
	if after := captureSchedulerTransactionState(compact); !reflect.DeepEqual(after, before) {
		t.Fatalf("outer rollback changed storage: before=%+v after=%+v", before, after)
	}
	assertTransactionJournalClean(t, compact)
}

func TestRecoverToAncestorStateRejectsStaleOwnerAndCandidate(t *testing.T) {
	const lookahead = Symbol(9)
	tables := &fakeTable{actions: map[tableCell][]Action{
		{state: 1, symbol: lookahead}: {{Type: ActionShift, State: 2}},
	}}
	compact := newAncestorRecoveryTestCore(t, tables, Limits{})
	seed, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	head := appendAncestorRecoveryHead(t, compact, seed, 3,
		appendAncestorRecoveryPayload(t, compact, 1, 0, 1, false))
	candidates, err := compact.StackSummaryCandidates(head, lookahead, 1)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%+v err=%v, want one", candidates, err)
	}
	var staleOwner SchedulerTransactionToken
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		staleOwner = owner
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := captureSchedulerTransactionState(compact)
	if _, err := compact.RecoverToAncestorStateOwned(staleOwner, candidates[0]); err == nil || !strings.Contains(err.Error(), "stale scheduler transaction token") {
		t.Fatalf("stale owner error=%v", err)
	}
	if after := captureSchedulerTransactionState(compact); !reflect.DeepEqual(after, before) {
		t.Fatalf("stale owner changed storage: before=%+v after=%+v", before, after)
	}

	if err := compact.ApplyAtomic(func() error { return errors.New("stale candidate rollback") }); err == nil {
		t.Fatal("candidate-staling rollback succeeded")
	}
	before = captureSchedulerTransactionState(compact)
	err = compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		_, innerErr := compact.RecoverToAncestorStateOwned(owner, candidates[0])
		return innerErr
	})
	if err == nil || !strings.Contains(err.Error(), "stale stack-summary candidate") {
		t.Fatalf("stale candidate error=%v", err)
	}
	if after := captureSchedulerTransactionState(compact); !reflect.DeepEqual(after, before) {
		t.Fatalf("stale candidate changed storage: before=%+v after=%+v", before, after)
	}
}

func TestRecoverToAncestorStateRepushesTrailingExtrasOutsideError(t *testing.T) {
	const lookahead = Symbol(9)
	tables := &fakeTable{actions: map[tableCell][]Action{
		{state: 1, symbol: lookahead}: {{Type: ActionShift, State: 2}},
	}}
	compact := newAncestorRecoveryTestCore(t, tables, Limits{})
	seed, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	child := appendAncestorRecoveryPayload(t, compact, 1, 0, 1, false)
	firstExtra := appendAncestorRecoveryPayload(t, compact, 2, 1, 2, true)
	secondExtra := appendAncestorRecoveryPayload(t, compact, 3, 2, 3, true)
	head := appendAncestorRecoveryHead(t, compact, seed, 2, child)
	head = appendAncestorRecoveryHead(t, compact, head, 3, firstExtra)
	head = appendAncestorRecoveryHead(t, compact, head, 4, secondExtra)
	candidates, err := compact.StackSummaryCandidates(head, lookahead, 3)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%+v err=%v, want one", candidates, err)
	}
	before, err := compact.Stats(head)
	if err != nil {
		t.Fatal(err)
	}

	var recovered Head
	err = compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		var innerErr error
		recovered, innerErr = compact.RecoverToAncestorStateOwned(owner, candidates[0])
		return innerErr
	})
	if err != nil {
		t.Fatalf("RecoverToAncestorStateOwned: %v", err)
	}
	state, byteOffset, err := compact.Boundary(recovered)
	if err != nil || state != 1 || byteOffset != 3 {
		t.Fatalf("recovered boundary=(%d,%d) err=%v, want (1,3)", state, byteOffset, err)
	}
	paths, err := compact.Derivations(recovered)
	if err != nil || len(paths) != 1 || len(paths[0].Payloads) != 3 {
		t.Fatalf("recovered derivations=%+v err=%v", paths, err)
	}
	errorPayload := paths[0].Payloads[0]
	if paths[0].Payloads[1] != firstExtra || paths[0].Payloads[2] != secondExtra {
		t.Fatalf("recovered payload order=%v, want [ERROR %d %d]", paths[0].Payloads, firstExtra, secondExtra)
	}
	errorView, err := compact.Subtree(errorPayload)
	if err != nil {
		t.Fatal(err)
	}
	if errorView.Symbol != ErrorRegionSymbol || !errorView.Extra || errorView.StartByte != 0 || errorView.EndByte != 1 ||
		!reflect.DeepEqual(errorView.Children, []SubtreeID{child}) {
		t.Fatalf("ERROR view=%+v, want child-only [0,1) extra container", errorView)
	}
	after, err := compact.Stats(recovered)
	if err != nil {
		t.Fatal(err)
	}
	if after.Nodes != before.Nodes+3 || after.Links != before.Links+3 ||
		after.Subtrees != before.Subtrees+1 || after.Children != before.Children+1 {
		t.Fatalf("recovery storage delta=%+v -> %+v, want N+3/L+3/S+1/C+1", before, after)
	}
}
