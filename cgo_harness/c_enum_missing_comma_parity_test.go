//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"os"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestCEnumThreeValuedEnumeratorsLeadingTriviaMatchesCReference locks the
// reduced PR #739 witness against the pinned C parser. Leading trivia selects
// the production GLR route, so this case must not rely on the forest fallback.
func TestCEnumThreeValuedEnumeratorsLeadingTriviaMatchesCReference(t *testing.T) {
	const src = "\ntypedef enum { A = 0, B = 1, C = MAX } t;"
	source := []byte(src)
	restoreForest := os.Getenv("GOT_GLR_FOREST") != "0"
	gotreesitter.SetGLRForestEnabled(false)
	t.Cleanup(func() { gotreesitter.SetGLRForestEnabled(restoreForest) })

	cLang, err := ParityCLanguage("c")
	if err != nil {
		t.Fatalf("load locked C parser: %v", err)
	}
	cParser := sitter.NewParser()
	t.Cleanup(cParser.Close)
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("set locked C language: %v", err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("locked C parser returned no tree")
	}
	t.Cleanup(cTree.Close)

	production := false
	goTree, goLang, err := parseWithGo(parityCase{
		name:           "c",
		source:         src,
		candidateRoute: &production,
	}, source, nil)
	if err != nil {
		t.Fatalf("parse C with Go production: %v", err)
	}
	t.Cleanup(func() { releaseGoTree(goTree) })
	assertLockedCTreeExact(t, "PR #739 C enum witness", goTree, goLang, cTree)
}
