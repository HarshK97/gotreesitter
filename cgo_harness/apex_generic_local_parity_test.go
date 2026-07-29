//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestApexGenericLocalRawCOracleParity(t *testing.T) {
	source := []byte("public class C {\n" +
		"  void m() {\n" +
		"    List<List<SObject>> searchResults = [FIND :keyword IN ALL FIELDS];\n" +
		"  }\n" +
		"}\n")

	goLang := grammars.ApexLanguage()
	goParser := gotreesitter.NewParser(goLang)
	goParser.SetAdmissionCandidateRoute(false)
	goTree, err := goParser.ParseNoResultCompatibilityBenchmarkOnly(source)
	if err != nil {
		t.Fatal(err)
	}
	defer goTree.Release()

	cLang, err := COracleLanguage("apex")
	if err != nil {
		t.Fatal(err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C parse returned a nil tree")
	}
	defer cTree.Close()

	var mismatches []string
	compareNodes(goTree.RootNode(), goLang, cTree.RootNode(), "root", &mismatches)
	if len(mismatches) != 0 {
		t.Fatalf("raw Go and C trees differ:\n%s", strings.Join(mismatches, "\n"))
	}
}
