//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestAdaElectionRawCOracleParity compares the raw production tree (compat
// tail off) against the locked C oracle for the three registered
// dispatch.ada subpasses' witnesses
// (parser_result_test/materialization_subpass_census_test.go).
func TestAdaElectionRawCOracleParity(t *testing.T) {
	goLang := grammars.AdaLanguage()
	tests := []struct {
		name   string
		source []byte
	}{
		{
			// dispatch.ada.constraint-kind-election witness
			// (parser_result_test/materialization_subpass_census_test.go
			// "ada_attribute_constraint").
			name: "attribute_constraint",
			source: []byte("procedure P is\n" +
				"begin\n" +
				"   A := new T (F => Pkg.Obj'Access);\n" +
				"end;\n"),
		},
		{
			// dispatch.ada.aggregate-kind-election witness
			// ("ada_locked_positional_array_aggregate"; tree-sitter-ada
			// test/corpus/arrays.txt, commit 6b58259a).
			name: "locked_positional_array_aggregate",
			source: []byte("package P is\n" +
				"   type A is array (1 .. 3) of Boolean;\n" +
				"   V : constant A := (1, 2, 3);\n" +
				"end;\n"),
		},
		{
			// dispatch.ada.aggregate-kind-election +
			// dispatch.ada.association-choice-materialization witness
			// ("ada_array_others_choice").
			name: "array_others_choice",
			source: []byte("procedure P is\n" +
				"begin\n" +
				"   A := (others => 0);\n" +
				"end;\n"),
		},
	}

	cLang, err := COracleLanguage("ada")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			goParser := gotreesitter.NewParser(goLang)
			goParser.SetAdmissionCandidateRoute(false)
			goTree, err := goParser.ParseNoResultCompatibilityBenchmarkOnly(test.source)
			if err != nil {
				t.Fatal(err)
			}
			defer goTree.Release()

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

			var mismatches []string
			compareNodes(goTree.RootNode(), goLang, cTree.RootNode(), "root", &mismatches)
			if len(mismatches) != 0 {
				t.Fatalf("raw Go and C trees differ:\n%s", strings.Join(mismatches, "\n"))
			}
		})
	}
}
