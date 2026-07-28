//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"os"
	"path/filepath"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestLinkerscriptRetirementCOracleParity(t *testing.T) {
	entry, ok := parityEntriesByName["linkerscript"]
	if !ok {
		t.Fatal("missing Linker Script grammar entry")
	}
	language := entry.Language()
	cLanguage, err := ParityCLanguage("linkerscript")
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"clean.lds", "recovered.lds"} {
		name := name
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(
				filepath.Join("..", "testdata", "linkerscript_retirement", name),
			)
			if err != nil {
				t.Fatal(err)
			}

			goTree, err := gotreesitter.NewParser(language).
				ParseNoResultCompatibilityBenchmarkOnly(source)
			if err != nil {
				t.Fatal(err)
			}
			defer goTree.Release()

			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLanguage); err != nil {
				t.Fatal(err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("C oracle returned a nil tree")
			}
			defer cTree.Close()

			var divergences []string
			compareNodes(
				goTree.RootNode(),
				language,
				cTree.RootNode(),
				"root",
				&divergences,
			)
			if len(divergences) != 0 {
				t.Fatalf("C-oracle divergences: %v", divergences)
			}
		})
	}
}
