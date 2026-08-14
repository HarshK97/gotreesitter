//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestYAMLOwnedGrammarLockedCParity(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "single quoted continuation",
			src:  "root: 'a\nb\nc'\n",
		},
		{
			name: "single quoted closing delimiter",
			src:  "root:\n  key: 'line1\n'\n",
		},
		{
			name: "double quoted closing delimiter",
			src:  "root:\n  key: \"line1\n\"\n",
		},
		{
			name: "Kubernetes document stream",
			src: `apiVersion: v1
kind: Service
metadata:
  annotations:
    pod.alpha.kubernetes.io/init-containers: '[
        {
          "name": "bootstrap"
        }
    ]'
  selector:
    app: test
---
apiVersion: v1
kind: Service
metadata:
  name: second-service` + "\n",
		},
	}

	entry := grammars.DetectLanguageByName("yaml")
	if entry == nil || entry.Language == nil {
		t.Fatal("missing owned YAML grammar")
	}
	goLang := entry.Language()
	cLang, err := COracleLanguage("yaml")
	if err != nil {
		t.Fatalf("load patched YAML C oracle: %v", err)
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.src)
			goTree, err := gotreesitter.NewParser(goLang).Parse(source)
			if err != nil {
				t.Fatalf("parse owned YAML: %v", err)
			}
			if goTree == nil {
				t.Fatal("owned YAML parser returned a nil tree")
			}
			t.Cleanup(goTree.Release)
			goRoot := goTree.RootNode()
			if goRoot == nil {
				t.Fatal("owned YAML parser returned a nil root")
			}
			if goRoot.HasError() {
				t.Fatalf("owned YAML parse has an error: %s", goRoot.SExpr(goLang))
			}

			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatalf("set YAML C oracle language: %v", err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("YAML C oracle returned a nil root")
			}
			defer cTree.Close()
			cRoot := cTree.RootNode()
			if cRoot.HasError() {
				t.Fatalf("YAML C oracle parse has an error: %s", dumpCTree(cRoot, 0))
			}

			var errs []string
			compareNodes(goRoot, goLang, cRoot, "root", &errs)
			if len(errs) > 0 {
				t.Fatalf("owned YAML/C tree mismatch:\n%s", strings.Join(errs, "\n"))
			}
		})
	}
}
