package gotreesitter

import (
	"bytes"
	"testing"
)

func TestNormalizeReturnedTreeForParseExtendsHTMLInnerChain(t *testing.T) {
	lang := &Language{
		Name:        "html",
		SymbolNames: []string{"EOF", "document", "element", "start_tag", "end_tag", "</", "tag_name", ">"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "document", Visible: true, Named: true},
			{Name: "element", Visible: true, Named: true},
			{Name: "start_tag", Visible: true, Named: true},
			{Name: "end_tag", Visible: true, Named: true},
			{Name: "</", Visible: true, Named: false},
			{Name: "tag_name", Visible: true, Named: true},
			{Name: ">", Visible: true, Named: false},
		},
	}

	arena := newNodeArena(arenaClassFull)
	start0 := newLeafNodeInArena(arena, 3, true, 0, 5, Point{}, Point{Column: 5})
	start1 := newLeafNodeInArena(arena, 3, true, 6, 11, Point{Row: 1}, Point{Row: 1, Column: 5})
	start2 := newLeafNodeInArena(arena, 3, true, 11, 16, Point{Row: 1, Column: 5}, Point{Row: 1, Column: 10})
	leaf := newParentNodeInArena(arena, 2, true, []*Node{start2}, nil, 0)
	leaf.endByte = 20
	leaf.endPoint = Point{Row: 3}
	inner := newParentNodeInArena(arena, 2, true, []*Node{start1, leaf}, nil, 0)
	inner.endByte = 20
	inner.endPoint = Point{Row: 3}
	closeTok := newLeafNodeInArena(arena, 5, false, 21, 23, Point{Row: 4}, Point{Row: 4, Column: 2})
	tagName := newLeafNodeInArena(arena, 6, true, 23, 26, Point{Row: 4, Column: 2}, Point{Row: 4, Column: 5})
	closeAngle := newLeafNodeInArena(arena, 7, false, 26, 27, Point{Row: 4, Column: 5}, Point{Row: 4, Column: 6})
	endTag := newParentNodeInArena(arena, 4, true, []*Node{closeTok, tagName, closeAngle}, nil, 0)
	outer := newParentNodeInArena(arena, 2, true, []*Node{start0, inner, endTag}, nil, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{outer}, nil, 0)

	source := bytes.Repeat([]byte{'x'}, 27)
	source[20] = '\n'
	tree := &Tree{root: root, source: source, language: lang, resultCompatibilityApplied: true}
	(&Parser{language: lang}).normalizeReturnedTreeForParse(tree, source)

	if got, want := inner.endByte, uint32(21); got != want {
		t.Fatalf("inner.endByte = %d, want %d", got, want)
	}
	if got, want := leaf.endByte, uint32(21); got != want {
		t.Fatalf("leaf.endByte = %d, want %d", got, want)
	}
}
