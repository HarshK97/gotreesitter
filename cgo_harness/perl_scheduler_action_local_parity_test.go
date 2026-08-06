//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestPerlSchedulerActionLoadBearingCOracleParity is an A3
// (spec.campaign.v7, Workstream A tranche A3) adversarial probe for the
// dispatch.perl arm (parser_result_perl.go,
// testdata/result_compat_ownership_v1.json). It pins the one sub-pass the
// real-corpus dispatcher census observed firing
// (normalizePerlPushExpressionLists;
// cgo_harness/corpus_real/perl/medium__unicode_ranges.pl, "push @found,
// $_;") plus generalizations of its trigger shape, and proves each witness
// is load-bearing two ways: the RAW production tree (result-compatibility
// tail off) diverges from the locked C oracle, and the NORMALIZED tree
// (compat tail on) matches it exactly. If a RAW witness here ever stops
// diverging, the underlying gotreesitter grammar/scheduler defect has been
// fixed upstream and dispatch.perl may be retirable for that witness --
// investigate before treating this test's failure as a regression.
//
// dispatch.perl cannot retire yet: this is the arm's uniform retirement
// condition (testdata/result_compat_ownership_v1.json) failing on the
// authoritative owner (scheduler_action_semantics). The root cause is a
// GLR/LALR derivation-election tie-break in the generated perl grammar
// tables for "ambiguous_function_call_expression" (a bareword call
// immediately followed by a comma-separated tail): gotreesitter's runtime
// elects the split derivation (the call keeps only its first argument; the
// remainder becomes sibling list items) where the C reference elects the
// grouped derivation (the call keeps every comma-separated item as its
// argument list) -- see TestPerlSchedulerActionKnownGapCOracleParity for
// witnesses (unshift, bare join, join-with-assignment, return) where this
// same defect class reaches acceptance uncorrected today. That table
// selection is grammar-specific (tree-sitter-perl's generated conflict
// resolution), not a small language-neutral fix, so this pins the arm as
// blocked-with-mechanism per spec.campaign.v7 workstream A3 rather than
// forcing a root fix.
func TestPerlSchedulerActionLoadBearingCOracleParity(t *testing.T) {
	goLang := grammars.PerlLanguage()
	cLang, err := COracleLanguage("perl")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		source string
	}{
		{
			// Real-corpus witness:
			// cgo_harness/corpus_real/perl/medium__unicode_ranges.pl,
			// inside the byte-class-list loop: "push @found, $_;". This is
			// the sole firing the dispatcher census records for dispatch.perl
			// (parser_result_test/dispatcher_census_test.go,
			// TestDispatcherArmCensusOverRealCorpus).
			name:   "push_two_args_real_corpus_witness",
			source: "push @found, $_;\n",
		},
		{
			name:   "push_three_args",
			source: "push @found, $a, $b;\n",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			src := []byte(test.source)

			goParser := gotreesitter.NewParser(goLang)
			goParser.SetAdmissionCandidateRoute(false)
			rawTree, err := goParser.ParseNoResultCompatibilityBenchmarkOnly(src)
			if err != nil {
				t.Fatalf("raw parse: %v", err)
			}
			defer rawTree.Release()

			normParser := gotreesitter.NewParser(goLang)
			normParser.SetAdmissionCandidateRoute(false)
			normTree, err := normParser.Parse(src)
			if err != nil {
				t.Fatalf("normalized parse: %v", err)
			}
			defer normTree.Release()

			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(src, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C parse returned a nil tree")
			}
			defer cTree.Close()

			var rawVsC, normVsC []string
			compareNodes(rawTree.RootNode(), goLang, cTree.RootNode(), "root", &rawVsC)
			compareNodes(normTree.RootNode(), goLang, cTree.RootNode(), "root", &normVsC)

			if len(rawVsC) == 0 {
				t.Fatalf(
					"raw tree now matches the C oracle for %q; the upstream grammar/scheduler election "+
						"defect this arm patches around may be fixed -- investigate dispatch.perl retirement "+
						"before accepting this as passing",
					test.name,
				)
			}
			if len(normVsC) != 0 {
				t.Fatalf("normalized (dispatch.perl-corrected) tree diverges from the C oracle: %s", strings.Join(normVsC, " | "))
			}
		})
	}
}

// TestPerlSchedulerActionKnownGapCOracleParity pins the same
// ambiguous_function_call_expression election defect
// (TestPerlSchedulerActionLoadBearingCOracleParity's doc comment) on shapes
// dispatch.perl's three sub-passes do not reach today: a second built-in
// list operator (unshift, uncovered because normalizePerlPushExpressionLists
// matches only fn.Text=="push"), and the two sub-passes
// (normalizePerlJoinAssignmentLists, normalizePerlReturnExpressionLists)
// whose match preconditions never fire against real grammar output --
// their preconditions require the RAW tree to already show the grouped
// (C-correct) shape and split it apart, but raw gotreesitter output for
// these forms is the split (C-incorrect) shape from the start, matching
// push's before-fix state. Zero corpus and constructed witnesses were found
// where either sub-pass fires and improves on raw. This is evidence for a
// spore finding, not a deletion in this tranche: dispatch.perl's overall
// arm remains blocked (see TestPerlSchedulerActionLoadBearingCOracleParity),
// so no route/registry disposition changes here.
//
// wantDivergence stays true for every case: if one flips to false, the same
// grammar/scheduler defect has been fixed upstream for that shape.
func TestPerlSchedulerActionKnownGapCOracleParity(t *testing.T) {
	goLang := grammars.PerlLanguage()
	cLang, err := COracleLanguage("perl")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		source         string
		wantDivergence bool
	}{
		{
			// Same shape as the push witness, uncovered: the push sub-pass
			// matches only fn.Text(source) == "push".
			name:           "unshift_two_args_uncovered_by_push_subpass",
			source:         "unshift @found, $_;\n",
			wantDivergence: true,
		},
		{
			// normalizePerlJoinAssignmentLists's intended trigger
			// (parser_result_bash_perl_test.go,
			// TestNormalizePerlJoinAssignmentListsRewritesBareListOperatorShape's
			// source). Real gotreesitter output for this source is already
			// the split list_expression shape the sub-pass tries to
			// *produce*, so its precondition (top position ==
			// assignment_expression) never matches -- dead code on real
			// derivations.
			name:           "join_assignment_precondition_never_matches",
			source:         "my $x = join \"\\n\", \"a\", \"b\";\n",
			wantDivergence: true,
		},
		{
			name:           "join_bare_uncovered_no_assignment_subpass",
			source:         "join \"\\n\", \"a\", \"b\";\n",
			wantDivergence: true,
		},
		{
			// normalizePerlReturnExpressionLists's intended trigger; same
			// dead-precondition shape as the join-assignment case.
			name:           "return_precondition_never_matches",
			source:         "sub f { return $a, $b; }\n",
			wantDivergence: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			src := []byte(test.source)

			goParser := gotreesitter.NewParser(goLang)
			goParser.SetAdmissionCandidateRoute(false)
			rawTree, err := goParser.ParseNoResultCompatibilityBenchmarkOnly(src)
			if err != nil {
				t.Fatalf("raw parse: %v", err)
			}
			defer rawTree.Release()

			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(src, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C parse returned a nil tree")
			}
			defer cTree.Close()

			var mismatches []string
			compareNodes(rawTree.RootNode(), goLang, cTree.RootNode(), "root", &mismatches)
			if test.wantDivergence {
				if len(mismatches) == 0 {
					t.Fatalf(
						"expected %q to diverge from the C oracle, but the raw tree now matches; the "+
							"underlying scheduler-election defect may be fixed -- flip wantDivergence to "+
							"false and re-verify before treating dispatch.perl as retirable for this shape",
						test.name,
					)
				}
				t.Skipf("known scheduler-action gap, not covered by any dispatch.perl sub-pass today:\n%s", strings.Join(mismatches, "\n"))
				return
			}
			if len(mismatches) != 0 {
				t.Fatalf("raw and C trees differ:\n%s", strings.Join(mismatches, "\n"))
			}
		})
	}
}
