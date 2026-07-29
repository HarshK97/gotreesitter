//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestCSharpJsonTextReaderRecoveredStructureRequiresCompatibility(t *testing.T) {
	source, err := os.ReadFile("../testdata/parser_result/csharp/jsontextreader_excerpt.cs")
	if err != nil {
		t.Fatal(err)
	}
	goLang := grammars.CSharpLanguage()
	parser := gotreesitter.NewParser(goLang)
	parser.SetAdmissionCandidateRoute(false)
	rawTree, err := parser.ParseNoResultCompatibilityBenchmarkOnly(source)
	if err != nil {
		t.Fatal(err)
	}
	defer rawTree.Release()

	cLang, err := ParityCLanguage("c_sharp")
	if err != nil {
		t.Fatalf("C parser unavailable: %v", err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("C SetLanguage: %v", err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C parse returned no root")
	}
	defer cTree.Close()

	rawRoot := rawTree.RootNode()
	cRoot := cTree.RootNode()
	if rawRoot.ChildCount() != 5 || cRoot.ChildCount() != 6 {
		t.Fatalf("root child counts go=%d c=%d, want 5 and 6", rawRoot.ChildCount(), cRoot.ChildCount())
	}
	rawNamespace := rawRoot.Child(4)
	cNamespace := cRoot.Child(4)
	cTrailing := cRoot.Child(5)
	if rawNamespace.Type(goLang) != "namespace_declaration" ||
		rawNamespace.EndByte() != 12407 ||
		cNamespace.Kind() != "namespace_declaration" ||
		cNamespace.EndByte() != 12405 ||
		cTrailing.Kind() != "ERROR" ||
		cTrailing.StartByte() != 12406 ||
		cTrailing.EndByte() != 12407 {
		t.Fatalf(
			"unexpected negative-control boundary: go=%s[%d,%d) c=%s[%d,%d) trailing=%s[%d,%d)",
			rawNamespace.Type(goLang),
			rawNamespace.StartByte(),
			rawNamespace.EndByte(),
			cNamespace.Kind(),
			cNamespace.StartByte(),
			cNamespace.EndByte(),
			cTrailing.Kind(),
			cTrailing.StartByte(),
			cTrailing.EndByte(),
		)
	}

	productionParser := gotreesitter.NewParser(goLang)
	productionParser.SetAdmissionCandidateRoute(false)
	productionTree, err := productionParser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	defer productionTree.Release()
	if productionTree.RootNode().HasError() {
		t.Fatal("JsonTextReader bypassed required C# compatibility reconstruction")
	}
}

func TestCSharpIssue454RecoveredStructureMatchesCBeforeCompatibility(t *testing.T) {
	src := csharpIssue454Source(t)
	assertCSharpRecoveredStructureMatchesC(t, src)
}

func csharpIssue454Source(t *testing.T) []byte {
	t.Helper()
	var source strings.Builder
	source.Grow(137*1024 + 256)
	source.WriteString("namespace Bench {\n")
	for i := 0; source.Len() < 137*1024; i++ {
		fmt.Fprintf(
			&source,
			"public static class C%d { public static int F%d() { var x%d = %d; return x%d; } }\n",
			i,
			i,
			i,
			i,
			i,
		)
	}
	source.WriteString("}\n")
	src := []byte(source.String())
	site := bytes.Index(src, []byte("x0"))
	if site < 0 {
		t.Fatal("C# edit marker is absent")
	}
	return append(append([]byte(nil), src[:site]...), src[site+1:]...)
}

func assertCSharpRecoveredStructureMatchesC(t *testing.T, src []byte) {
	t.Helper()
	goLang := grammars.CSharpLanguage()
	cLang, err := ParityCLanguage("c_sharp")
	if err != nil {
		t.Fatalf("C parser unavailable: %v", err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("C SetLanguage: %v", err)
	}
	cTree := cParser.Parse(src, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C parse returned no root")
	}
	defer cTree.Close()

	tests := []struct {
		name  string
		parse func(*gotreesitter.Parser, []byte) (*gotreesitter.Tree, error)
	}{
		{name: "production", parse: func(parser *gotreesitter.Parser, source []byte) (*gotreesitter.Tree, error) {
			return parser.Parse(source)
		}},
		{name: "before_compatibility", parse: func(parser *gotreesitter.Parser, source []byte) (*gotreesitter.Tree, error) {
			return parser.ParseNoResultCompatibilityBenchmarkOnly(source)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := gotreesitter.NewParser(goLang)
			parser.SetAdmissionCandidateRoute(false)
			goTree, err := test.parse(parser, src)
			if err != nil {
				t.Fatalf("Go parse: %v", err)
			}
			defer goTree.Release()
			var errs []string
			compareNodes(goTree.RootNode(), goLang, cTree.RootNode(), "root", &errs)
			if goTree.RootNode().HasError() != cTree.RootNode().HasError() {
				errs = append(
					errs,
					fmt.Sprintf(
						"root: HasError go=%t c=%t",
						goTree.RootNode().HasError(),
						cTree.RootNode().HasError(),
					),
				)
			}
			compareCSharpRecoveryFields(
				goTree.RootNode(),
				goLang,
				cTree.RootNode(),
				"root",
				&errs,
			)
			if len(errs) > 0 {
				if len(errs) > 20 {
					errs = errs[:20]
				}
				t.Fatalf("C# recovered tree diverged from C:\n%s", strings.Join(errs, "\n"))
			}
		})
	}
}

func compareCSharpRecoveryFields(
	goNode *gotreesitter.Node,
	goLang *gotreesitter.Language,
	cNode *sitter.Node,
	path string,
	errs *[]string,
) {
	if int(goNode.ChildCount()) != int(cNode.ChildCount()) {
		return
	}
	for i := 0; i < int(goNode.ChildCount()); i++ {
		childPath := fmt.Sprintf("%s[%d]", path, i)
		goField := goNode.FieldNameForChild(i, goLang)
		cField := cNode.FieldNameForChild(uint32(i))
		if goField != cField {
			*errs = append(
				*errs,
				fmt.Sprintf("%s: FieldName go=%q c=%q", childPath, goField, cField),
			)
		}
		compareCSharpRecoveryFields(
			goNode.Child(i),
			goLang,
			cNode.Child(uint(i)),
			childPath,
			errs,
		)
	}
}
