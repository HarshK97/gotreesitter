package gotreesitter

import "testing"

func TestNormalizeCPONNullLiteralCollapsesTokenChild(t *testing.T) {
	lang := &Language{
		Name:        "cpon",
		SymbolNames: []string{"EOF", "document", "null", "null"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "document", Visible: true, Named: true},
			{Name: "null", Visible: true, Named: true},
			{Name: "null", Visible: true, Named: false},
		},
	}
	source := []byte("null")
	arena := newNodeArena(arenaClassFull)
	token := newLeafNodeInArena(arena, 3, false, 0, 4, Point{}, Point{Column: 4})
	nullNode := newParentNodeInArena(arena, 2, true, []*Node{token}, nil, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{nullNode}, nil, 0)

	normalizeCPONCompatibility(root, source, lang)

	if got, want := nullNode.ChildCount(), 0; got != want {
		t.Fatalf("null child count = %d, want %d", got, want)
	}
	if got, want := nullNode.Type(lang), "null"; got != want {
		t.Fatalf("null type = %q, want %q", got, want)
	}
}
