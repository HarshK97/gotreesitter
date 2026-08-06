//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestPythonSchedulerActionLoadBearingCOracleParity is an A3
// (spec.campaign.v7, Workstream A tranche A3) adversarial probe for the
// dispatch.python arm (parser_result_python.go,
// normalizePythonCompatibilityWithParser). It pins the one rewrite the
// real-corpus dispatcher census observed firing: the assignment-right
// expression-list rewrite inside normalizePythonFusedPreorder
// (cgo_harness/corpus_real/python/large__python3.8_grammar.py, "xyz = x, y,
// z"). Each witness is proven load-bearing two ways: the RAW production
// tree (result-compatibility tail off) diverges from the locked C oracle,
// and the NORMALIZED tree (compat tail on) matches it exactly.
//
// dispatch.python cannot retire yet: this is the arm's uniform retirement
// condition (testdata/result_compat_ownership_v1.json) failing on the
// authoritative owner (scheduler_action_semantics). The root cause is a
// grammar/scheduler derivation-election tie-break: for an unparenthesized
// comma-tuple on the right-hand side of a plain assignment (`x = a, b`),
// gotreesitter's runtime elects the same shape (pattern_list) the grammar
// uses for assignment *targets* and for-loop/except/with targets, where the
// C reference elects expression_list. See
// TestPythonSchedulerActionKnownGapCOracleParity for a sibling shape (the
// identical pattern_list/expression_list tie inside f-string interpolation)
// where no existing sub-pass reaches the fix. Fixing the tie generally in
// the scheduler risks every other pattern_list/expression_list consumer
// (for, except, with, del, match targets) across all grammars, so this is
// not a small language-neutral change; this pins the arm as
// blocked-with-mechanism per spec.campaign.v7 workstream A3 rather than
// forcing a root fix.
func TestPythonSchedulerActionLoadBearingCOracleParity(t *testing.T) {
	goLang := grammars.PythonLanguage()
	cLang, err := COracleLanguage("python")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		source string
	}{
		{
			// Real-corpus witness:
			// cgo_harness/corpus_real/python/large__python3.8_grammar.py
			// byte offset 23794, "xyz = x, y, z". This is the sole firing
			// the dispatcher census records for dispatch.python
			// (parser_result_test/dispatcher_census_test.go,
			// TestDispatcherArmCensusOverRealCorpus).
			name:   "assignment_bare_tuple_real_corpus_witness",
			source: "x, y, z = 1, 2, 3\nxyz = x, y, z\n",
		},
		{
			name:   "assignment_bare_pair",
			source: "a = 1\nb = 2\npair = a, b\n",
		},
		{
			name:   "assignment_bare_single_trailing_comma",
			source: "a = 1\nsingle = a,\n",
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
						"defect this arm patches around may be fixed -- investigate dispatch.python "+
						"retirement before accepting this as passing",
					test.name,
				)
			}
			if len(normVsC) != 0 {
				t.Fatalf("normalized (dispatch.python-corrected) tree diverges from the C oracle: %s", strings.Join(normVsC, " | "))
			}
		})
	}
}

// TestPythonSchedulerActionKnownGapCOracleParity pins the same
// pattern_list/expression_list election tie
// (TestPythonSchedulerActionLoadBearingCOracleParity's doc comment) inside
// f-string interpolation, where normalizePythonInterpolationPatterns
// (parser_result_python.go) does not reach it: that sub-pass only rewrites
// an already-expression_list node found under "interpolation" into
// pattern_list, but raw gotreesitter output for a bare tuple inside an
// f-string interpolation is already pattern_list (the same election as the
// uncorrected assignment case), so the sub-pass's precondition never
// matches -- it converts the opposite direction from the one this witness
// needs. dispatch.python's overall arm remains blocked (see
// TestPythonSchedulerActionLoadBearingCOracleParity), so no route/registry
// disposition changes here; this is evidence for a spore finding.
//
// wantDivergence stays true for every case: if one flips to false, the same
// grammar/scheduler defect has been fixed upstream for that shape.
func TestPythonSchedulerActionKnownGapCOracleParity(t *testing.T) {
	goLang := grammars.PythonLanguage()
	cLang, err := COracleLanguage("python")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		source         string
		wantDivergence bool
	}{
		{
			name:           "fstring_interpolation_bare_tuple_uncovered",
			source:         "x = 1\ny = 2\nz = f\"{x, y}\"\n",
			wantDivergence: true,
		},
		{
			name:           "fstring_interpolation_splat_uncovered",
			source:         "xs = [1, 2]\nz = f\"{*xs,}\"\n",
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
							"false and re-verify before treating dispatch.python as retirable for this shape",
						test.name,
					)
				}
				t.Skipf("known scheduler-action gap, not covered by any dispatch.python sub-pass today:\n%s", strings.Join(mismatches, "\n"))
				return
			}
			if len(mismatches) != 0 {
				t.Fatalf("raw and C trees differ:\n%s", strings.Join(mismatches, "\n"))
			}
		})
	}
}

// TestPythonSchedulerActionNeutralSubpassCOracleParity pins every other
// trigger shape normalizePythonFusedPreorder guards for (collapsed
// pass/continue/break, inline return/raise/yield/tuple single-line blocks,
// wildcard imports, match-case as-patterns and wildcard patterns) plus
// normalizePythonStringContinuationEscapes, and confirms the raw production
// tree already matches the C oracle for every one of them: none of these
// sub-passes fired on cgo_harness/corpus_real/python (the dispatcher census
// recorded exactly one rewrite total, the assignment-list witness above),
// and none fires here either. This does not retire dispatch.python (the
// arm still guards the load-bearing assignment-list witness), but it is a
// receipt that most of the fused preorder walk is dead weight on current
// grammar output -- filed as a spore finding for a future, narrowly scoped
// cleanup tranche rather than acted on in this batch.
func TestPythonSchedulerActionNeutralSubpassCOracleParity(t *testing.T) {
	goLang := grammars.PythonLanguage()
	cLang, err := COracleLanguage("python")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		source string
	}{
		{name: "print_chevron", source: "print >>sys.stderr, \"x\"\n"},
		{name: "collapsed_pass_own_line", source: "def f():\n    pass\n"},
		{name: "collapsed_pass_inline_block", source: "if True: pass\n"},
		{name: "collapsed_continue_inline_block", source: "while True:\n    if True: continue\n"},
		{name: "collapsed_break_inline_block", source: "while True:\n    if True: break\n"},
		{name: "inline_return_block", source: "def f():\n    if True: return 1\n"},
		{name: "inline_raise_block", source: "def f():\n    if True: raise ValueError(\"x\")\n"},
		{name: "inline_yield_block", source: "def f():\n    if True: yield 1\n"},
		{name: "inline_tuple_block", source: "def f():\n    if True: a, b = 1, 2\n"},
		{name: "wildcard_import", source: "from os import *\n"},
		{name: "match_case_as_pattern", source: "match p:\n    case (a, b) as pair:\n        pass\n"},
		{name: "match_case_wildcard", source: "match p:\n    case _:\n        pass\n"},
		{name: "string_continuation_escape", source: "s = \"line one \\\nline two\"\n"},
		{name: "string_continuation_escape_raw", source: "s = r\"line one \\\nline two\"\n"},
		// Negative controls: legitimate pattern_list contexts the
		// assignment-list rewrite must not touch.
		{name: "for_target_tuple_negative_control", source: "pairs = [(1, 2)]\nfor a, b in pairs:\n    pass\n"},
		{name: "chained_assignment_lhs_negative_control", source: "a, b = c, d = 1, 2\n"},
		{name: "del_tuple_negative_control", source: "a = 1\nb = 2\ndel a, b\n"},
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
			if len(mismatches) != 0 {
				t.Fatalf(
					"raw tree now diverges from the C oracle for %q (was previously confirmed inert); "+
						"this sub-pass trigger shape may now be reachable and require a fix:\n%s",
					test.name, strings.Join(mismatches, "\n"),
				)
			}
		})
	}
}
