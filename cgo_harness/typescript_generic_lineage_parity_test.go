//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"os"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestTypeScriptGenericLineageCOracleParity(t *testing.T) {
	stress, err := os.ReadFile("../grammars/testdata/typescript_issue_544.ts")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		source []byte
	}{
		{
			name:   "issue_541_nested_union_call",
			source: []byte("f<Record<string, never> | Array<Record<string, never>>>();"),
		},
		{
			name:   "issue_544_zod_union",
			source: stress,
		},
		{
			name:   "right_shift",
			source: []byte("const shifted = value >> 1;"),
		},
		{
			name:   "unsigned_right_shift",
			source: []byte("const shifted = value >>> 1;"),
		},
	}

	goLang := grammars.TypescriptLanguage()
	cLang, err := COracleLanguage("typescript")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			goParser := gotreesitter.NewParser(goLang)
			goParser.SetAdmissionCandidateRoute(false)
			goTree, err := goParser.Parse(test.source)
			if err != nil {
				t.Fatal(err)
			}
			defer goTree.Release()
			goRoot := goTree.RootNode()
			if goRoot == nil || goRoot.HasError() || goRoot.EndByte() != uint32(len(test.source)) {
				t.Fatalf("Go parse is incomplete: %s", goRoot.SExpr(goLang))
			}

			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(test.source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C parse returned a nil tree")
			}
			defer cTree.Close()
			cRoot := cTree.RootNode()
			if cRoot.HasError() || cRoot.EndByte() != uint(len(test.source)) {
				t.Fatalf("C parse is incomplete: %s", dumpCTree(cRoot, 0))
			}

			var mismatches []string
			compareNodes(goRoot, goLang, cRoot, "root", &mismatches)
			if len(mismatches) != 0 {
				t.Fatalf("Go and C trees differ:\n%s", strings.Join(mismatches, "\n"))
			}
		})
	}
}
