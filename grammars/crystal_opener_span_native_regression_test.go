//go:build !grammar_subset

package grammars

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func TestCrystalBraceOpenersNeedNoResultCompatibility(t *testing.T) {
	for _, test := range []struct {
		name      string
		source    string
		container string
		wantStart uint32
		wantEnd   uint32
	}{
		{
			name:      "inline_hash",
			source:    "x = {1 => 2}\n",
			container: "hash",
			wantStart: 4,
			wantEnd:   5,
		},
		{
			name:      "multiline_hash",
			source:    "x = {\n  1 => 2,\n}\n",
			container: "hash",
			wantStart: 5,
			wantEnd:   5,
		},
		{
			name:      "inline_named_tuple",
			source:    "x = {a: 1}\n",
			container: "named_tuple",
			wantStart: 4,
			wantEnd:   5,
		},
		{
			name:      "multiline_named_tuple",
			source:    "x = {\n  a: 1,\n}\n",
			container: "named_tuple",
			wantStart: 5,
			wantEnd:   5,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			language := CrystalLanguage()
			tree, err := gotreesitter.NewParser(language).
				ParseNoResultCompatibilityBenchmarkOnly(source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(tree.Release)

			root := tree.RootNode()
			if root == nil || root.HasError() {
				t.Fatalf("root = %v, want a clean tree", root)
			}
			container := findCrystalNamedNode(root, language, test.container)
			if container == nil {
				t.Fatalf("missing %s: %s", test.container, root.SExpr(language))
			}
			open := container.Child(0)
			if open == nil || open.Type(language) != "{" {
				t.Fatalf("%s opener = %v", test.container, open)
			}
			if got := open.StartByte(); got != test.wantStart {
				t.Fatalf("opener start = %d, want %d", got, test.wantStart)
			}
			if got := open.EndByte(); got != test.wantEnd {
				t.Fatalf("opener end = %d, want %d", got, test.wantEnd)
			}
			if got := container.StartByte(); got != test.wantStart {
				t.Fatalf("%s start = %d, want %d", test.container, got, test.wantStart)
			}
		})
	}
}

func findCrystalNamedNode(
	node *gotreesitter.Node,
	language *gotreesitter.Language,
	name string,
) *gotreesitter.Node {
	if node == nil {
		return nil
	}
	if node.Type(language) == name {
		return node
	}
	for index := 0; index < node.NamedChildCount(); index++ {
		if found := findCrystalNamedNode(node.NamedChild(index), language, name); found != nil {
			return found
		}
	}
	return nil
}
