//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestRetiredDispatchArmCOracleParity(t *testing.T) {
	tests := []struct {
		name     string
		language string
		source   string
		diverges string
	}{
		{
			name:     "html_mismatched_custom_close",
			language: "html",
			source:   "<my-a><my-b><my-c>text</my-x>\n",
			diverges: `root: ChildCount go=3 c=2`,
		},
		{
			name:     "html_autoclosing_custom_chain",
			language: "html",
			source:   "<div><xyz><xyz><xyz>text</div>\n",
		},
		{name: "ocaml_boolean_true", language: "ocaml", source: "let x = true\n"},
		{name: "ocaml_boolean_false", language: "ocaml", source: "let x = false\n"},
		{name: "ocaml_and_ampersand", language: "ocaml", source: "let x = a & b\n"},
		{name: "ocaml_and_double_ampersand", language: "ocaml", source: "let x = a && b\n"},
		{name: "ocaml_or_keyword", language: "ocaml", source: "let x = a or b\n"},
		{name: "ocaml_or_double_bar", language: "ocaml", source: "let x = a || b\n"},
		{
			name:     "ruby_module_bounds",
			language: "ruby",
			source:   "# comment\nmodule Foo\nend\n",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			scheduleParityMemoryScavenge(t)
			if test.diverges != "" {
				divergences := retiredDispatchArmCOracleDivergences(
					t,
					parityCase{name: test.language, source: test.source},
					[]byte(test.source),
				)
				if len(divergences) != 1 || !strings.Contains(divergences[0], test.diverges) {
					t.Fatalf("C-oracle divergences = %v, want only %q", divergences, test.diverges)
				}
				return
			}
			runParityCase(
				t,
				parityCase{name: test.language, source: test.source},
				"retired-dispatch-arm",
				[]byte(test.source),
			)
		})
	}
}

func retiredDispatchArmCOracleDivergences(t *testing.T, test parityCase, source []byte) []string {
	t.Helper()
	cLanguage, err := ParityCLanguage(test.name)
	if err != nil {
		t.Fatal(err)
	}
	goTree, goLanguage, err := parseWithGo(test, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGoTree(goTree)

	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLanguage); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C reference parser returned a nil tree")
	}
	defer cTree.Close()

	var divergences []string
	compareNodes(goTree.RootNode(), goLanguage, cTree.RootNode(), "root", &divergences)
	return divergences
}
