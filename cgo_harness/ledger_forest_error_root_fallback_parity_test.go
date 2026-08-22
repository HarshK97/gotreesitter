//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestLedgerForestExperimentalErrorRootFallbackLockedC proves that both
// Ledger forest declines use production trees that match locked C exactly.
func TestLedgerForestExperimentalErrorRootFallbackLockedC(t *testing.T) {
	entry, ok := parityEntriesByName["ledger"]
	if !ok {
		t.Fatal("missing Ledger grammar entry")
	}
	language := entry.Language()
	cLanguage, err := COracleLanguage("ledger")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		source []byte
	}{
		{
			name:   "recovered-date-suffix",
			source: []byte("\n15.03.2006 Exxon\n    Expenses:Auto:Gas          10,00 EUR\n    Liabilities:MasterCard    -10,00 EUR\n"),
		},
		{
			name: "year-directive",
			source: []byte(`
--input-date-format %d.%m

Y2010
03.01 * Foo
    A                 10.00 EUR
    B

05.02 * Bar
    A                 20.00 EUR
    B

test reg A
10-Jan-03 Foo                   A                         10.00 EUR    10.00 EUR
10-Feb-05 Bar                   A                         20.00 EUR    30.00 EUR
end test

`),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			cParser := sitter.NewParser()
			t.Cleanup(cParser.Close)
			if err := cParser.SetLanguage(cLanguage); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(test.source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("locked C parser returned no tree")
			}
			t.Cleanup(cTree.Close)
			cDigest, err := COracleDeepDigest(cTree)
			if err != nil {
				t.Fatal(err)
			}

			forestParser := gts.NewParser(language)
			forest, ok := forestParser.ParseForestExperimental(test.source)
			if ok || forest != nil {
				if forest != nil {
					forest.Release()
				}
				t.Fatalf("forest result ok=%t tree_nil=%t, want error_root decline", ok, forest == nil)
			}
			_, _, reason, _ := forestParser.ForestDeclineInfo()
			if reason != "error_root" {
				t.Fatalf("forest decline reason=%q, want error_root", reason)
			}

			fallback, err := forestParser.Parse(test.source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(fallback.Release)
			if fallback.ParseRuntime().ForestFastPath {
				t.Fatal("fallback used the forest route")
			}
			assertForestFallbackLockedC(t, fallback, language, cTree, cDigest)
		})
	}
}

// TestForestExperimentalCleanControlLockedC proves that a clean forest result
// remains direct and matches locked C after the error-root guard.
func TestForestExperimentalCleanControlLockedC(t *testing.T) {
	entry, ok := parityEntriesByName["bash"]
	if !ok {
		t.Fatal("missing Bash grammar entry")
	}
	language := entry.Language()
	cLanguage, err := COracleLanguage("bash")
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("x=1\n")
	cParser := sitter.NewParser()
	t.Cleanup(cParser.Close)
	if err := cParser.SetLanguage(cLanguage); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("locked C parser returned no tree")
	}
	t.Cleanup(cTree.Close)
	cDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		t.Fatal(err)
	}

	forest, ok := gts.NewParser(language).ParseForestExperimental(source)
	if !ok || forest == nil {
		t.Fatalf("clean forest result ok=%t tree_nil=%t", ok, forest == nil)
	}
	t.Cleanup(forest.Release)
	if !forest.ParseRuntime().ForestFastPath || forest.RootNode().IsError() || forest.RootNode().HasErrorOrMissing() {
		t.Fatalf("clean forest runtime/root invalid: %s", forest.ParseRuntime().Summary())
	}
	assertForestFallbackLockedC(t, forest, language, cTree, cDigest)
}

func assertForestFallbackLockedC(
	t *testing.T,
	goTree *gts.Tree,
	goLanguage *gts.Language,
	cTree *sitter.Tree,
	wantDigest string,
) {
	t.Helper()
	if diff := FirstDivergenceDumpV1(goTree.RootNode(), goLanguage, cTree.RootNode()); diff != nil {
		t.Fatalf("tree diverges from locked C: %+v", diff)
	}
	if diff := firstLockedCTreeFlagDivergence(goTree.RootNode(), goLanguage, cTree.RootNode(), "/"+goTree.RootNode().Type(goLanguage)); diff != nil {
		t.Fatalf("tree flags diverge from locked C: %v", diff)
	}
	inspection, err := benchfixtures.InspectGoTree(goTree.RootNode(), goLanguage)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.SHA256 != wantDigest {
		t.Fatalf("Go digest=%s, locked C digest=%s", inspection.SHA256, wantDigest)
	}
	t.Logf("locked C parity digest=%s", inspection.SHA256)
}
