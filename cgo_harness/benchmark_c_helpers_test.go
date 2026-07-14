//go:build cgo && treesitter_c_bench_legacy

package cgoharness

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

func requireCompleteCTree(tb testing.TB, tree *sitter.Tree, src []byte, phase string) *sitter.Node {
	tb.Helper()
	if tree == nil {
		tb.Fatalf("%s parse returned nil tree", phase)
		return nil
	}
	root := tree.RootNode()
	if root == nil {
		tb.Fatalf("%s parse returned nil root", phase)
		return nil
	}
	if got, want := uint32(root.EndByte()), uint32(len(src)); got != want {
		tb.Fatalf("%s parse truncated: root.EndByte=%d want=%d type=%q hasError=%v", phase, got, want, root.Type(), root.HasError())
	}
	return root
}

func parseCTree(tb testing.TB, parser *sitter.Parser, oldTree *sitter.Tree, src []byte, phase string) *sitter.Tree {
	tb.Helper()
	tree, err := parser.ParseCtx(context.Background(), oldTree, src)
	if err != nil {
		tb.Fatalf("%s parse error: %v", phase, err)
	}
	return tree
}
