//go:build gts_workcount

package gotreesitter

import "testing"

func TestWorkCountRefreshConvergenceLookaheadKeepsElectionOrdinal(t *testing.T) {
	previous := activeWorkCountConvergence
	t.Cleanup(func() { activeWorkCountConvergence = previous })
	activeWorkCountConvergence.electionOrdinal = 0
	activeWorkCountConvergence.lookahead = Token{}
	activeWorkCountConvergence.lookaheadPresent = false

	initial := Token{Symbol: 7, StartByte: 11, EndByte: 14}
	relexed := Token{Symbol: 9, StartByte: 11, EndByte: 15}
	workCountSetConvergenceLookahead(initial)
	ordinal := activeWorkCountConvergence.electionOrdinal
	workCountRefreshConvergenceLookahead(relexed)

	if ordinal != 1 || activeWorkCountConvergence.electionOrdinal != ordinal {
		t.Fatalf("refresh changed election ordinal: before=%d after=%d", ordinal, activeWorkCountConvergence.electionOrdinal)
	}
	if activeWorkCountConvergence.lookahead != relexed || !activeWorkCountConvergence.lookaheadPresent {
		t.Fatalf("refresh did not publish current lookahead: got=%+v present=%v want=%+v", activeWorkCountConvergence.lookahead, activeWorkCountConvergence.lookaheadPresent, relexed)
	}
}

func TestSemanticPhaseTraceEOFPrefixTableRoute(t *testing.T) {
	previousConvergence := activeWorkCountConvergence
	previousTrace := activeDiagnosticSemanticPhaseTrace
	t.Cleanup(func() {
		activeWorkCountConvergence = previousConvergence
		activeDiagnosticSemanticPhaseTrace = previousTrace
	})
	activeWorkCountConvergence.electionOrdinal = 0
	activeWorkCountConvergence.iteration = 17
	activeWorkCountConvergence.lookahead = Token{}
	activeWorkCountConvergence.lookaheadPresent = false
	activeDiagnosticSemanticPhaseTrace = nil

	lang := &Language{
		Name:            "semantic-trace-eof-prefix",
		SymbolCount:     3,
		TokenCount:      1,
		StateCount:      3,
		LargeStateCount: 3,
		InitialState:    1,
		SymbolNames:     []string{"end", "unused", "root"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "end"},
			{Name: "unused"},
			{Name: "root", Visible: true, Named: true},
		},
		ParseTable: [][]uint16{
			{0, 0, 0},
			{1, 0, 2}, // EOF reduces root; root GOTO enters state 2.
			{2, 0, 0}, // EOF then accepts.
		},
		ParseActions: []ParseActionEntry{
			{},
			{Actions: []ParseAction{{Type: ParseActionReduce, Symbol: 2}}},
			{Actions: []ParseAction{{Type: ParseActionAccept}}},
		},
	}
	parser := NewParser(lang)
	stack := newGLRStack(1)
	tok := Token{}
	workCountSetConvergenceLookahead(tok)
	BeginDiagnosticSemanticPhaseTrace()

	arena := newNodeArena(arenaClassFull)
	var entryScratch glrEntryScratch
	var gssScratch gssScratch
	var tmpEntries []stackEntry
	nodeCount := 0
	insertedMissing, advanced := parser.advanceTrailingEOFPrefix(&stack, tok, 0, nil, &nodeCount, arena, &entryScratch, &gssScratch, &tmpEntries)
	trace := EndDiagnosticSemanticPhaseTrace()

	if insertedMissing || !advanced || !stack.accepted {
		t.Fatalf("EOF prefix route inserted_missing=%v advanced=%v accepted=%v", insertedMissing, advanced, stack.accepted)
	}
	var lookups, executions int
	wantPhases := map[string]int{"eof-prefix-reduce": 1, "eof-prefix-accept": 1}
	for _, event := range trace.Events {
		switch event.Kind {
		case "action_lookup":
			lookups++
			if event.ActionOrdinal != 0 || event.TokenOrdinal != 1 || event.Iteration != 17 {
				t.Fatalf("EOF lookup context=%+v", event)
			}
		case "action_execution":
			executions++
			wantPhases[event.Phase]--
			if event.ActionOrdinal != 0 || event.TokenOrdinal != 1 || event.Iteration != 17 {
				t.Fatalf("EOF execution context=%+v", event)
			}
		}
	}
	if lookups != 2 || executions != 2 || wantPhases["eof-prefix-reduce"] != 0 || wantPhases["eof-prefix-accept"] != 0 {
		t.Fatalf("EOF route lookups=%d executions=%d phases=%v", lookups, executions, wantPhases)
	}
}
