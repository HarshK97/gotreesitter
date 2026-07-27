package gotreesitter

import "testing"

func TestNormalizeHaskellDeclarationsSpanExtendsToTrailingTrivia(t *testing.T) {
	lang := &Language{
		Name:        "haskell",
		SymbolNames: []string{"EOF", "haskell", "declarations"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "haskell", Visible: true, Named: true},
			{Name: "declarations", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	decls := newLeafNodeInArena(arena, 2, true, 10, 14, Point{Row: 1}, Point{Row: 1, Column: 4})
	root := newParentNodeInArena(arena, 1, true, []*Node{decls}, nil, 0)
	root.endByte = 15
	root.endPoint = Point{Row: 2}

	normalizeHaskellDeclarationsSpan(root, []byte("0123456789body\n"), lang)

	if got, want := decls.endByte, uint32(15); got != want {
		t.Fatalf("decls.endByte = %d, want %d", got, want)
	}
	if got, want := decls.endPoint, root.endPoint; got != want {
		t.Fatalf("decls.endPoint = %#v, want %#v", got, want)
	}
}
