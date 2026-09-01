//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestCompactPackedGSSVersionOrderIssue984MatchesC(t *testing.T) {
	entry, ok := parityEntriesByName["erlang"]
	if !ok {
		t.Fatal("Erlang is not registered")
	}
	language := entry.Language()
	if !language.CompactPackedGSSVersionOrderCertified {
		t.Fatal("the built-in Erlang grammar did not enable packed GSS version ordering")
	}
	cLanguage, err := COracleLanguage("erlang")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name       string
		source     string
		deepSHA256 string
	}{
		{
			name:       "no_newline",
			source:     "000\"0A!A \"A\"=0:A0!)A\"0%0000",
			deepSHA256: "93429d2e59861512ce66b0d09a095889b67b640cda8bb916c61b93ba4195811e",
		},
		{
			name:       "trailing_newline",
			source:     "000\"0A!A \"A\"=0:A0!)A\"0%0000\n",
			deepSHA256: "dcfdecf279b3537deb52c3a47d5a7a7f3ef66085f6b1a4249156618c26cd718e",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLanguage); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C returned a nil tree")
			}
			defer cTree.Close()

			parser := gotreesitter.NewParser(language)
			parser.SetAdmissionCandidateRoute(false)
			tree, err := parser.Parse(source)
			if err != nil {
				t.Fatalf("production parse: %v", err)
			}
			defer tree.Release()

			root := tree.RootNode()
			if diff := FirstDivergenceDumpV1(root, language, cTree.RootNode()); diff != nil {
				t.Fatalf("production tree diverges from C: %+v", diff)
			}
			if diff := firstLockedCTreeFlagDivergence(root, language, cTree.RootNode(), "/"+root.Type(language)); diff != nil {
				t.Fatalf("production flags diverge from C: %v", diff)
			}
			inspection, err := benchfixtures.InspectGoTree(root, language)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.SHA256 != test.deepSHA256 {
				t.Fatalf("production digest=%s, want C digest %s", inspection.SHA256, test.deepSHA256)
			}
		})
	}
}
