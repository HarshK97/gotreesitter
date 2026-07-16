package gotreesitter_test

import (
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestDescendantForByteRangeCParity locks the tree-sitter C boundary/out-of-
// range semantics (audit D1): an empty range at a token's end boundary belongs
// to the enclosing node, and an unsatisfiable range returns the node itself,
// never nil.
func TestDescendantForByteRangeCParity(t *testing.T) {
	src := []byte("package main\n\nimport \"fmt\"\n")
	lang := grammars.GoLanguage()
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	root := tree.RootNode()
	typ := func(n *gts.Node) string {
		if n == nil {
			return "<nil>"
		}
		return n.Type(lang)
	}

	if got := root.DescendantForByteRange(7, 7); typ(got) != "package_clause" {
		t.Fatalf("(7,7) = %s, want package_clause (end boundary belongs to parent)", typ(got))
	}
	if got := root.DescendantForByteRange(12, 12); typ(got) != "source_file" {
		t.Fatalf("(12,12) = %s, want source_file", typ(got))
	}
	if got := root.DescendantForByteRange(1000, 1005); got != root {
		t.Fatalf("out-of-bounds range must return the node itself, got %s", typ(got))
	}
	if got := root.DescendantForByteRange(8, 12); typ(got) != "package_identifier" {
		t.Fatalf("(8,12) = %s, want package_identifier", typ(got))
	}
	if got := root.DescendantForPointRange(gts.Point{Row: 0, Column: 12}, gts.Point{Row: 0, Column: 12}); typ(got) != "source_file" {
		t.Fatalf("point (0,12) = %s, want source_file", typ(got))
	}
}
