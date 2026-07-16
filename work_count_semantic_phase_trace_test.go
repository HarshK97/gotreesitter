//go:build gts_workcount

package gotreesitter_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

func TestDiagnosticSemanticPhaseTraceObserverEquality(t *testing.T) {
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if fixtures[0].Fixture.ID != "query_compile" {
		t.Fatalf("first canonical fixture=%q want query_compile", fixtures[0].Fixture.ID)
	}
	cases := []struct {
		name       string
		source     []byte
		wantDigest string
		wantGLR    bool
		want       semanticTraceReceipt
	}{
		{
			name: "query_compile", source: fixtures[0].Source, wantDigest: fixtures[0].Fixture.DeepTreeSHA256, wantGLR: true,
			want: semanticTraceReceipt{eventsSeen: 83689, retained: 8192, dropped: 75497, lookups: 1797, executions: 3239, decisions: 3156, unique: 749, repeated: 1048, maxMultiplicity: 4},
		},
		{
			name: "straight_lr", source: loadWorkCountAttributionFixture(t, straightLRFixturePath, straightLRFixtureBytes, straightLRFixtureSHA256),
			want: semanticTraceReceipt{eventsSeen: 193, retained: 193, lookups: 61, executions: 130, decisions: 2, unique: 61, maxMultiplicity: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			offCounts, offDigest := parseSemanticPhaseTraceFixture(t, tc.source)
			onCounts, onDigest, trace := parseSemanticPhaseTraceFixtureObserved(t, tc.source)
			if offDigest != onDigest {
				t.Fatalf("observer changed tree digest: off=%s on=%s", offDigest, onDigest)
			}
			if tc.wantDigest != "" && onDigest != tc.wantDigest {
				t.Fatalf("tree digest=%s want frozen=%s", onDigest, tc.wantDigest)
			}
			if !reflect.DeepEqual(offCounts, onCounts) {
				t.Fatal("semantic trace changed diagnostic work counts")
			}
			if trace.Contract != gotreesitter.DiagnosticSemanticPhaseTraceContract || trace.MaxEvents != gotreesitter.DiagnosticSemanticPhaseTraceMaxEvents {
				t.Fatalf("trace contract=%q max=%d", trace.Contract, trace.MaxEvents)
			}
			if trace.EventsSeen == 0 || len(trace.Events) == 0 || trace.ArithmeticOverflow {
				t.Fatalf("trace events_seen=%d retained=%d overflow=%v", trace.EventsSeen, len(trace.Events), trace.ArithmeticOverflow)
			}
			var lookupEvents, executionEvents, executionUnknownOrdinal, decisionEvents, convergenceDecisions int
			lookupCounts := make(map[coarseLookupClass]int)
			executionPhases := make(map[string]int)
			decisionPhases := make(map[string]int)
			for _, event := range trace.Events {
				switch event.Kind {
				case "action_lookup":
					lookupEvents++
					lookupCounts[coarseLookupClass{
						token: event.TokenOrdinal, byteOffset: event.ByteOffset, state: event.State,
						lookahead: event.LookaheadSymbol, cell: event.ActionCellFingerprint,
						boundary: event.CoarseBoundaryClass, ordinal: event.ActionOrdinal,
					}]++
					if event.ActionCellFingerprint == 0 || event.CoarseBoundaryClass == 0 || event.ActionOrdinal < -1 || event.ActionType < -1 {
						t.Fatalf("invalid lookup event: %+v", event)
					}
				case "action_execution":
					executionEvents++
					executionPhases[event.Phase]++
					if event.ActionOrdinal < 0 {
						executionUnknownOrdinal++
					}
					if event.ActionCellFingerprint == 0 || event.CoarseBoundaryClass == 0 || event.ActionOrdinal < -1 || event.ActionType < 0 {
						t.Fatalf("invalid execution event: %+v", event)
					}
				case "decision":
					decisionEvents++
					decisionPhases[event.Phase+"/"+event.Outcome]++
					if event.Phase != "final_select" && event.Phase != "terminal_accept" {
						convergenceDecisions++
					}
				default:
					t.Fatalf("unknown event kind %q", event.Kind)
				}
			}
			if lookupEvents == 0 || executionEvents == 0 {
				t.Fatalf("trace retained lookups=%d executions=%d", lookupEvents, executionEvents)
			}
			if tc.wantGLR && decisionEvents == 0 {
				t.Fatal("GLR fixture retained no post-reduction decisions")
			}
			if tc.wantGLR && executionPhases["extra-shift"] == 0 {
				t.Fatal("GLR fixture retained no extra-shift fast-path execution")
			}
			if !tc.wantGLR && convergenceDecisions != 0 {
				t.Fatalf("straight-LR control nonterminal convergence decisions=%d", convergenceDecisions)
			}
			uniqueClasses, repeatedClassEvents, maxClassMultiplicity := len(lookupCounts), 0, 0
			for _, count := range lookupCounts {
				if count > 1 {
					repeatedClassEvents += count - 1
				}
				if count > maxClassMultiplicity {
					maxClassMultiplicity = count
				}
			}
			gotReceipt := semanticTraceReceipt{
				eventsSeen: trace.EventsSeen, retained: len(trace.Events), dropped: trace.EventsDropped,
				lookups: lookupEvents, executions: executionEvents, decisions: decisionEvents,
				unique: uniqueClasses, repeated: repeatedClassEvents, maxMultiplicity: maxClassMultiplicity,
			}
			if gotReceipt != tc.want {
				t.Fatalf("trace receipt=%+v want=%+v", gotReceipt, tc.want)
			}
			if executionUnknownOrdinal != 0 {
				t.Fatalf("execution events with unknown action ordinal=%d", executionUnknownOrdinal)
			}
			t.Logf("semantic_phase_trace fixture=%s events_seen=%d retained=%d dropped=%d lookups=%d executions=%d execution_unknown_ordinal=%d execution_phases=%v unique_coarse_classes=%d repeated_coarse_class_events=%d max_coarse_class_multiplicity=%d decisions=%d decision_phases=%v digest=%s", tc.name, trace.EventsSeen, len(trace.Events), trace.EventsDropped, lookupEvents, executionEvents, executionUnknownOrdinal, executionPhases, uniqueClasses, repeatedClassEvents, maxClassMultiplicity, decisionEvents, decisionPhases, onDigest)
		})
	}
}

type semanticTraceReceipt struct {
	eventsSeen      uint64
	retained        int
	dropped         uint64
	lookups         int
	executions      int
	decisions       int
	unique          int
	repeated        int
	maxMultiplicity int
}

type coarseLookupClass struct {
	token      uint64
	byteOffset uint32
	state      uint32
	lookahead  uint32
	cell       uint64
	boundary   uint64
	ordinal    int16
}

func TestDiagnosticSemanticPhaseTraceContractIdentity(t *testing.T) {
	const wantSHA256 = "cbe46fcf6f41da65f44d7191456fae86d5c8b9f845fd516fc3111b3a62095076"
	data, err := os.ReadFile("cgo_harness/work_count/semantic_phase_trace_contract_v4.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantSHA256 {
		t.Fatalf("semantic trace contract sha256=%s want=%s", got, wantSHA256)
	}
}

func parseSemanticPhaseTraceFixture(t *testing.T, source []byte) (gotreesitter.DiagnosticWorkCount, string) {
	t.Helper()
	parser := gotreesitter.NewParser(grammars.GoLanguage())
	gotreesitter.BeginDiagnosticWorkCount()
	tree, err := parser.Parse(source)
	counts := gotreesitter.EndDiagnosticWorkCount()
	if err != nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatal(err)
	}
	defer tree.Release()
	inspection, err := benchfixtures.InspectGoTree(tree.RootNode(), grammars.GoLanguage())
	if err != nil {
		t.Fatal(err)
	}
	return counts, inspection.SHA256
}

func parseSemanticPhaseTraceFixtureObserved(t *testing.T, source []byte) (gotreesitter.DiagnosticWorkCount, string, gotreesitter.DiagnosticSemanticPhaseTrace) {
	t.Helper()
	parser := gotreesitter.NewParser(grammars.GoLanguage())
	gotreesitter.BeginDiagnosticWorkCount()
	gotreesitter.BeginDiagnosticSemanticPhaseTrace()
	tree, err := parser.Parse(source)
	trace := gotreesitter.EndDiagnosticSemanticPhaseTrace()
	counts := gotreesitter.EndDiagnosticWorkCount()
	if err != nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatal(err)
	}
	defer tree.Release()
	inspection, err := benchfixtures.InspectGoTree(tree.RootNode(), grammars.GoLanguage())
	if err != nil {
		t.Fatal(err)
	}
	return counts, inspection.SHA256, trace
}
