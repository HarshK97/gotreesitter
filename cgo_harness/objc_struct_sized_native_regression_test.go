//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestObjcStructSizedTypeSpecifierNeedsNoCompatibilityCOracle(t *testing.T) {
	source := []byte("@interface Box : NSObject\n{\n  unsigned long _version;\n}\n@end\n")
	language := parityEntriesByName["objc"].Language()
	goTree, err := gotreesitter.NewParser(language).
		ParseNoResultCompatibilityBenchmarkOnly(source)
	if err != nil {
		t.Fatal(err)
	}
	defer goTree.Release()

	cLanguage, err := ParityCLanguage("objc")
	if err != nil {
		t.Fatal(err)
	}
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
	compareNodes(goTree.RootNode(), language, cTree.RootNode(), "root", &divergences)
	if len(divergences) != 0 {
		t.Fatalf("C-oracle divergences: %v", divergences)
	}
}
