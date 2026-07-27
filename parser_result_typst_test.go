package gotreesitter

import "testing"

func TestNormalizeTypstNestedItemsMergesIndentedSiblings(t *testing.T) {
	lang := &Language{
		Name:        "typst",
		SymbolNames: []string{"EOF", "content", "item", "-", "text", "parbreak"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "content", Visible: true, Named: true},
			{Name: "item", Visible: true, Named: true},
			{Name: "-", Visible: true, Named: false},
			{Name: "text", Visible: true, Named: true},
			{Name: "parbreak", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	top := typstTestItem(arena, 2, 0, 0, 10)
	nestedA := typstTestItem(arena, 2, 2, 12, 20)
	nestedB := typstTestItem(arena, 2, 2, 22, 30)
	sibling := typstTestItem(arena, 2, 0, 31, 40)
	parbreak := newLeafNodeInArena(arena, 5, true, 40, 42, Point{Row: 4, Column: 9}, Point{Row: 6})
	content := newParentNodeInArena(arena, 1, true, []*Node{top, nestedA, nestedB, sibling, parbreak}, nil, 0)
	content.startByte = 0
	content.startPoint = Point{}
	content.endByte = 42
	content.endPoint = Point{Row: 6}

	normalizeTypstCompatibility(content, []byte("012345678901234567890123456789012345678901"), lang)

	if got, want := resultChildCount(content), 3; got != want {
		t.Fatalf("content child count = %d, want %d", got, want)
	}
	if got, want := content.startByte, uint32(0); got != want {
		t.Fatalf("content startByte = %d, want %d", got, want)
	}
	if got := resultChildAt(content, 0); got != top {
		t.Fatalf("content child 0 = %#v, want top", got)
	}
	if got := resultChildAt(content, 1); got != sibling {
		t.Fatalf("content child 1 = %#v, want sibling", got)
	}
	if got := resultChildAt(content, 2); got != parbreak {
		t.Fatalf("content child 2 = %#v, want parbreak", got)
	}
	if got, want := resultChildCount(top), 4; got != want {
		t.Fatalf("top item child count = %d, want %d", got, want)
	}
	if got := resultChildAt(top, 2); got != nestedA {
		t.Fatalf("top child 2 = %#v, want nestedA", got)
	}
	if got := resultChildAt(top, 3); got != nestedB {
		t.Fatalf("top child 3 = %#v, want nestedB", got)
	}
	if got, want := top.endByte, nestedB.endByte; got != want {
		t.Fatalf("top endByte = %d, want %d", got, want)
	}
}

func TestNormalizeTypstNestedItemsSkipsErrorTree(t *testing.T) {
	lang := &Language{
		Name:        "typst",
		SymbolNames: []string{"EOF", "content", "item", "-", "text"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "content", Visible: true, Named: true},
			{Name: "item", Visible: true, Named: true},
			{Name: "-", Visible: true, Named: false},
			{Name: "text", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	top := typstTestItem(arena, 2, 0, 0, 10)
	nested := typstTestItem(arena, 2, 2, 12, 20)
	root := newParentNodeInArena(arena, 1, true, []*Node{top, nested}, nil, 0)
	root.endByte = 20
	root.endPoint = nested.endPoint
	root.setHasError(true)

	normalizeTypstCompatibility(root, []byte("01234567890123456789"), lang)

	if got, want := resultChildCount(root), 2; got != want {
		t.Fatalf("root child count = %d, want %d", got, want)
	}
	if got, want := resultChildCount(top), 2; got != want {
		t.Fatalf("top item child count = %d, want %d", got, want)
	}
}

func typstTestItem(arena *nodeArena, itemSym Symbol, column uint32, start, end uint32) *Node {
	dash := newLeafNodeInArena(arena, 3, false, start, start+1, Point{Row: start, Column: column}, Point{Row: start, Column: column + 1})
	text := newLeafNodeInArena(arena, 4, true, start+2, end, Point{Row: start, Column: column + 2}, Point{Row: start, Column: column + 2 + (end - start - 2)})
	item := newParentNodeInArena(arena, itemSym, true, []*Node{dash, text}, nil, 0)
	item.startByte = start
	item.endByte = end
	item.startPoint = Point{Row: start, Column: column}
	item.endPoint = text.endPoint
	return item
}
