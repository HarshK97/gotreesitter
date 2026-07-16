package gotreesitter_test

// Focused C-parity regression tests for the four tree-sitter query-engine
// parity fixes landed on this branch:
//
//   - D8: #is?/#is-not? are inert property predicates and must not filter
//     matches.
//   - D4: (MISSING) / (MISSING kind) query patterns test Node.IsMissing(),
//     instead of matching every node like an unqualified wildcard.
//   - D3: supertype node-type patterns (e.g. (expression)) expand to their
//     subtype symbols at match time instead of matching nothing.
//   - D5: captures inside quantified multi-step groups (e.g.
//     ((a) (b)?)*) are collected across every repetition instead of being
//     dropped.
//
// Each test's expected result was cross-checked against the real C
// tree-sitter runtime (github.com/tree-sitter/go-tree-sitter) via the
// parity-audit probe harness; the numbers baked in here are the C behavior,
// not a description of the old (buggy) Go behavior.

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func parseWithLanguage(t *testing.T, lang *gotreesitter.Language, src []byte) *gotreesitter.Tree {
	t.Helper()
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		t.Fatal("Parse returned nil tree/root")
	}
	return tree
}

// --- D8: #is?/#is-not? must not filter matches -----------------------------

func TestParityD8IsPredicateDoesNotFilterMatches(t *testing.T) {
	lang := grammars.GoLanguage()
	src := []byte("package main\nvar count = 1\n")
	tree := parseWithLanguage(t, lang, src)
	defer tree.Release()

	for _, q := range []string{
		`((identifier) @id (#is? local))`,
		`((identifier) @id (#is-not? local))`,
		`((identifier) @id (#is? "local"))`,
	} {
		query, err := gotreesitter.NewQuery(q, lang)
		if err != nil {
			t.Fatalf("NewQuery(%q): %v", q, err)
		}
		matches := query.Execute(tree)
		if len(matches) == 0 {
			t.Errorf("query %q: got 0 matches, want at least 1 (C treats #is?/#is-not? as inert metadata, never filtering)", q)
			continue
		}
		var sawCount bool
		for _, m := range matches {
			for _, c := range m.Captures {
				if c.Text(src) == "count" {
					sawCount = true
				}
			}
		}
		if !sawCount {
			t.Errorf("query %q: expected the \"count\" identifier to be captured (C does not drop it)", q)
		}
	}
}

// --- D4: (MISSING) / (MISSING kind) -----------------------------------------

func TestParityD4MissingPatternMatchesOnlyMissingNode(t *testing.T) {
	lang := grammars.CLanguage()
	src := []byte("int a\n")
	tree := parseWithLanguage(t, lang, src)
	defer tree.Release()

	bare, err := gotreesitter.NewQuery(`(MISSING) @m`, lang)
	if err != nil {
		t.Fatalf("NewQuery((MISSING)): %v", err)
	}
	matches := bare.Execute(tree)
	if len(matches) != 1 {
		t.Fatalf("(MISSING) @m: got %d matches, want 1 (C matches only the missing ';')", len(matches))
	}
	if got := len(matches[0].Captures); got != 1 {
		t.Fatalf("(MISSING) @m: got %d captures, want 1", got)
	}
	mc := matches[0].Captures[0]
	if !mc.Node.IsMissing() {
		t.Fatalf("(MISSING) @m: matched node is not IsMissing()")
	}
	if mc.Node.StartByte() != mc.Node.EndByte() {
		t.Fatalf("(MISSING) @m: matched node is not zero-width (start=%d end=%d)", mc.Node.StartByte(), mc.Node.EndByte())
	}

	kinded, err := gotreesitter.NewQuery(`(MISSING ";") @m`, lang)
	if err != nil {
		t.Fatalf(`NewQuery((MISSING ";")): %v`, err)
	}
	matches = kinded.Execute(tree)
	if len(matches) != 1 {
		t.Fatalf(`(MISSING ";") @m: got %d matches, want 1`, len(matches))
	}
	if got := len(matches[0].Captures); got != 1 || !matches[0].Captures[0].Node.IsMissing() {
		t.Fatalf(`(MISSING ";") @m: expected exactly one missing-node capture`)
	}

	// A pattern that treats MISSING as an unqualified wildcard (the old,
	// buggy Go behavior) would match every node in the tree, not just the
	// missing token -- guard against regressing back to that.
	allNodes, err := gotreesitter.NewQuery(`(_) @all`, lang)
	if err != nil {
		t.Fatalf("NewQuery((_)): %v", err)
	}
	if got := len(allNodes.Execute(tree)); got <= 1 {
		t.Fatalf("sanity check failed: (_) @all matched %d nodes, expected the tree to contain more than just the missing node", got)
	}

	// (MISSING (child)) must be rejected at compile time (TSQueryErrorSyntax
	// upstream): MISSING only accepts a bare form, a kind identifier, or a
	// string-literal token qualifier -- never a nested child pattern.
	if _, err := gotreesitter.NewQuery(`(MISSING (identifier))`, lang); err == nil {
		t.Fatal("(MISSING (identifier)): expected a compile error, got nil")
	}
}

// --- D3: supertype patterns expand to subtype symbols -----------------------

func TestParityD3SupertypePatternMatchesSubtypes(t *testing.T) {
	lang := grammars.GoLanguage()
	src := []byte("package main\nfunc f() { x := 1 + 2 }\n")
	tree := parseWithLanguage(t, lang, src)
	defer tree.Release()

	// Nested/field-constrained supertype position: the go blob's _expression
	// supertype table is intact, so this achieves exact parity with C (unlike
	// the bare/unconstrained form, which is a known approximation -- see the
	// worklog notes in the branch report).
	query, err := gotreesitter.NewQuery(`(binary_expression left: (_expression) @l)`, lang)
	if err != nil {
		t.Fatalf("NewQuery: %v", err)
	}
	matches := query.Execute(tree)
	if len(matches) != 1 {
		t.Fatalf("(binary_expression left: (_expression) @l): got %d matches, want 1", len(matches))
	}
	if got := len(matches[0].Captures); got != 1 {
		t.Fatalf("expected exactly 1 capture, got %d", got)
	}
	capture := matches[0].Captures[0]
	if capture.Node.Type(lang) != "int_literal" {
		t.Fatalf("captured node type = %q, want %q (the literal \"1\", a subtype of _expression)", capture.Node.Type(lang), "int_literal")
	}
	if capture.Text(src) != "1" {
		t.Fatalf("captured text = %q, want %q", capture.Text(src), "1")
	}

	// Bare/unconstrained supertype pattern must, at minimum, stop matching
	// nothing (the pre-fix bug) and must include known subtype instances.
	stmtQuery, err := gotreesitter.NewQuery(`(_statement) @st`, lang)
	if err != nil {
		t.Fatalf("NewQuery((_statement)): %v", err)
	}
	stmtMatches := stmtQuery.Execute(tree)
	if len(stmtMatches) == 0 {
		t.Fatal("(_statement) @st: got 0 matches, want at least 1 (supertype patterns must match concrete subtype nodes)")
	}
	var sawShortVarDecl bool
	for _, m := range stmtMatches {
		for _, c := range m.Captures {
			if c.Node.Type(lang) == "short_var_declaration" {
				sawShortVarDecl = true
			}
		}
	}
	if !sawShortVarDecl {
		t.Fatal("(_statement) @st: expected a short_var_declaration match (x := 1 + 2), a direct _statement subtype")
	}
}

// --- D5: captures inside quantified multi-step groups ----------------------

func TestParityD5QuantifiedGroupCapturesAllRepetitions(t *testing.T) {
	lang := grammars.JavascriptLanguage()
	src := []byte("function add(x, y) { return x; }\n")
	tree := parseWithLanguage(t, lang, src)
	defer tree.Release()

	cases := []struct {
		name      string
		query     string
		wantTexts []string // in order; nil means "expect zero matches"
	}{
		{
			name:      "star",
			query:     `(formal_parameters ((identifier) @p (",")?)*)`,
			wantTexts: []string{"x", "y"},
		},
		{
			name:      "plus",
			query:     `(formal_parameters ((identifier) @p (",")?)+)`,
			wantTexts: []string{"x", "y"},
		},
		{
			name:      "optional",
			query:     `(formal_parameters ((identifier) @p ",")?)`,
			wantTexts: []string{"x"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query, err := gotreesitter.NewQuery(tc.query, lang)
			if err != nil {
				t.Fatalf("NewQuery(%q): %v", tc.query, err)
			}
			matches := query.Execute(tree)
			if len(matches) != 1 {
				t.Fatalf("%s: got %d matches, want 1", tc.query, len(matches))
			}
			var got []string
			for _, c := range matches[0].Captures {
				got = append(got, c.Text(src))
			}
			if len(got) != len(tc.wantTexts) {
				t.Fatalf("%s: got %d captures %v, want %v", tc.query, len(got), got, tc.wantTexts)
			}
			for i, want := range tc.wantTexts {
				if got[i] != want {
					t.Fatalf("%s: capture[%d] = %q, want %q (full: %v)", tc.query, i, got[i], want, got)
				}
			}
		})
	}
}
