//go:build cgo && treesitter_c_parity

package cgoharness

// Tranche T3: C-oracle adjudication of the compact-vs-production route
// divergence witnesses collected by the differential fuzz harness (98
// witnesses: html 88, javascript 8, swift 2). Every witness in this fixture
// showed prodHasError=true, compactHasError=false in the fuzz logs.
//
// This suite parses each witness with the locked C oracle (upstream
// tree-sitter runtime f5afe475deb7c0bae6407fb776c76824f717bb61, grammar
// commits from grammars/languages.lock, -O2 shared object via dlopen; see
// COracleBuildIdentity in parity_c_loader_cgo.go) and adjudicates which Go
// route matches it.
//
// The suite asserts production's HasError matches the C oracle's HasError:
// that boolean is the exact signal the fuzz harness used to collect these
// witnesses (prodHasError=true, compactHasError=false), so it is the
// load-bearing adjudication claim. A HasError divergence here is a
// production bug, not a compact-route issue, and fails this test loudly.
//
// The suite only logs (never asserts) full node-by-node structural parity
// and the compact route's outcome. Full structural parity between
// gotreesitter's and the C runtime's error-recovery trees is a documented,
// currently-passing invariant only for clean/well-formed input (see
// TestParityFreshParse and TestParityHasNoErrors on the smoke corpus); it is
// not an established invariant for malformed-input recovery paths, and this
// suite's own run against these witnesses shows production's recovered tree
// shape diverging from the C oracle's on essentially every witness even
// though the HasError flag agrees. That shape divergence is a distinct,
// secondary finding from the has_error adjudication and is logged, not
// asserted, here. Fixing the compact admission route's false-clean
// acceptance is a separate workstream from this adjudication.
//
// Run with:
//   go test . -tags treesitter_c_parity -run '^TestCompactT3OracleAdjudication$' -v

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type compactT3Witness struct {
	id     string
	class  string
	lang   string
	load   func() *gotreesitter.Language
	source string
}

var compactT3Witnesses = []compactT3Witness{
	// HTML erroneous_end_tag class. The first two are the reported minimal
	// reductions; the rest are drawn directly from the fuzz logs
	// (hemlock-fuzz-main.txt / hemlock-fuzz-main2.txt).
	{id: "html_min_a", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<a></a^>"},
	{id: "html_min_html", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html></html^>"},
	{id: "html_log_1", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html>/<body>Hello</body^></html>\n"},
	{id: "html_log_2", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html><bod\">Hello</body></html>\n"},
	{id: "html_log_3", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html><body>Hello</bo^y>:</html>\n"},
	{id: "html_log_4", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html><body>H}llo</boy></h!tml>\n"},
	{id: "html_log_5", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html><body>Hello /bod></html>\n"},
	{id: "html_log_6", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html><body>Hell</body>y></html>\n"},
	{id: "html_log_7", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html><bodyHello</body></html>\n"},
	{id: "html_log_8", class: "html_erroneous_end_tag", lang: "html", load: grammars.HtmlLanguage, source: "<html> body>Hello</body></html>\n"},

	// JavaScript arrow-parameter recovery class: all 8 logged witnesses.
	{id: "js_log_1", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "function f() { return 1; }\nconst x = ()? => x + 1;\n"},
	{id: "js_log_2", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "function f()^{ return 1; }\nconstx = () => x + 1;\n"},
	{id: "js_log_3", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "const f = (a) => a + 1;\nclass  A { m() { return 1 } )}\n"},
	{id: "js_log_4", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "function f()&{ return 1; }\nconst x = () => x + 1;\n"},
	{id: "js_log_5", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "const f = (a) =. + + 1;\nclass A { m() { return 1 } }\n\n"},
	{id: "js_log_6", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "const f = (a) => a + 1;&\nclass A { m() { return 1 } }\n\n"},
	{id: "js_log_7", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "const f = (a) => a + 1;\nclass A ?{ m() { return 1 } }\n"},
	{id: "js_log_8", class: "js_arrow_param_recovery", lang: "javascript", load: grammars.JavascriptLanguage, source: "function f() { return 1; ^}\ncon\nconst x = () => x + 1;\n"},

	// Swift bitwise-shift-vs-comparison class: both logged witnesses.
	{id: "swift_log_1", class: "swift_shift_comparison", lang: "swift", load: grammars.SwiftLanguage, source: "let v = x >> y>>"},
	{id: "swift_log_2", class: "swift_shift_comparison", lang: "swift", load: grammars.SwiftLanguage, source: "letv = x >> \n"},
}

// TestCompactT3OracleAdjudicationProvenance records the exact C-oracle build
// identity (runtime commit, grammar commit, compile flags, artifact hash) for
// every language this adjudication touches.
func TestCompactT3OracleAdjudicationProvenance(t *testing.T) {
	for _, lang := range []string{"html", "javascript", "swift"} {
		lang := lang
		t.Run(lang, func(t *testing.T) {
			id, err := COracleIdentity(lang)
			if err != nil {
				t.Fatalf("%s: C oracle identity unavailable: %v", lang, err)
			}
			t.Logf("contract=%s runtime_commit=%s grammar_repo=%s grammar_commit=%s flags=%s compiler=%s (%s) artifact=%s sha256=%s",
				id.Contract, id.RuntimeCommit, id.GrammarRepo, id.GrammarCommit, id.GrammarCompileFlags,
				id.CompilerPath, id.CompilerVersion, id.GrammarArtifactPath, id.GrammarArtifactSHA256)
		})
	}
}

// TestCompactT3OracleAdjudication adjudicates each route-divergence witness
// class against the locked C oracle. See package doc comment above.
func TestCompactT3OracleAdjudication(t *testing.T) {
	for _, w := range compactT3Witnesses {
		w := w
		t.Run(w.class+"/"+w.id, func(t *testing.T) {
			src := []byte(w.source)

			cLang, err := ParityCLanguage(w.lang)
			if err != nil {
				t.Fatalf("%s: C oracle unavailable: %v", w.lang, err)
			}
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatalf("C SetLanguage: %v", err)
			}
			cTree := cParser.Parse(src, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C oracle parse returned no root")
			}
			defer cTree.Close()
			cHasError := cTree.RootNode().HasError()

			lang := w.load()

			production := gotreesitter.NewParser(lang)
			production.SetAdmissionCandidateRoute(false)
			productionTree, err := production.Parse(src)
			if err != nil {
				t.Fatalf("production parse: %v", err)
			}
			defer productionTree.Release()
			prodHasError := productionTree.RootNode().HasError()

			candidate := gotreesitter.NewParser(lang)
			candidate.SetAdmissionCandidateRoute(true)
			candidateTree, err := candidate.Parse(src)
			if err != nil {
				t.Fatalf("compact parse: %v", err)
			}
			defer candidateTree.Release()
			compHasError := candidateTree.RootNode().HasError()

			t.Logf("src=%q", w.source)
			t.Logf("C oracle:   HasError=%v sexpr=%s", cHasError, cTree.RootNode().ToSexp())
			t.Logf("production: HasError=%v sexpr=%s", prodHasError, productionTree.RootNode().SExpr(lang))
			t.Logf("compact:    HasError=%v sexpr=%s", compHasError, candidateTree.RootNode().SExpr(lang))

			// The adjudication itself: production's HasError must match the C
			// oracle's HasError -- the exact boolean the fuzz harness used to
			// collect this witness. Any failure here is a PRODUCTION bug,
			// distinct from the known compact admission gap this fixture
			// documents.
			if prodHasError != cHasError {
				t.Fatalf("PRODUCTION-VS-C HAS-ERROR DIVERGENCE (production bug, not compact): production HasError=%v, C oracle HasError=%v", prodHasError, cHasError)
			}

			// Documentary only: full node-by-node structural parity is a
			// currently-passing invariant for clean input (see
			// TestParityFreshParse on the smoke corpus) but is not established
			// for malformed-input error-recovery paths. Log the shape
			// divergence for evidence without failing the fixture on it.
			var structuralErrs []string
			compareNodes(productionTree.RootNode(), lang, cTree.RootNode(), "root", &structuralErrs)
			if len(structuralErrs) > 0 {
				shown := structuralErrs
				if len(shown) > 5 {
					shown = shown[:5]
				}
				t.Logf("structural shape divergence from C oracle (%d node-level diffs, informational only):\n%s", len(structuralErrs), strings.Join(shown, "\n"))
			}

			// Documentary only: record whether the compact route's known
			// false-clean acceptance is still present. Fixing it is a separate
			// workstream, so this fixture never fails on it.
			if compHasError == cHasError {
				t.Logf("NOTE: compact route now agrees with the C oracle for this witness (regression check: confirm the compact-route fix landed)")
			} else {
				t.Logf("KNOWN COMPACT DIVERGENCE (tracked, not asserted here): compact HasError=%v, C oracle HasError=%v", compHasError, cHasError)
			}
		})
	}
}
