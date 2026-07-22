package gotreesitter

import "testing"

func TestNormalizeElixirDropsNewlineBeforeCommentExtras(t *testing.T) {
	lang := testElixirCompatibilityLanguage()
	arena := newNodeArena(arenaClassFull)
	firstComment := newLeafNodeInArena(arena, 17, true, 0, 7, Point{}, Point{Column: 7})
	newline := newLeafNodeInArena(arena, 18, true, 7, 8, Point{Column: 7}, Point{Row: 1})
	secondComment := newLeafNodeInArena(arena, 17, true, 8, 15, Point{Row: 1}, Point{Row: 1, Column: 7})
	list := newLeafNodeInArena(arena, 19, true, 17, 19, Point{Row: 2}, Point{Row: 2, Column: 2})
	root := newParentNodeInArena(arena, 1, true, []*Node{firstComment, newline, secondComment, list}, nil, 0)

	normalizeElixirCompatibility(root, []byte("# one\n# two\n\n[]"), lang)

	if got, want := root.ChildCount(), 3; got != want {
		t.Fatalf("root child count = %d, want %d", got, want)
	}
	for i, want := range []string{"comment", "comment", "list"} {
		if got := root.Child(i).Type(lang); got != want {
			t.Fatalf("root child[%d] = %q, want %q", i, got, want)
		}
	}
	if got := secondComment.Parent(); got != root {
		t.Fatalf("second comment parent = %v, want root", got)
	}
	if got := secondComment.childIndex; got != 1 {
		t.Fatalf("second comment childIndex = %d, want 1", got)
	}
}

func TestNormalizeElixirMapContentKeywordPairs(t *testing.T) {
	lang := testElixirCompatibilityLanguage()
	arena := newNodeArena(arenaClassFull)
	keyword := newLeafNodeInArena(arena, 5, true, 0, 9, Point{}, Point{Column: 9})
	value := newLeafNodeInArena(arena, 6, true, 9, 17, Point{Column: 9}, Point{Column: 17})
	pair := newParentNodeInArena(arena, 4, true, []*Node{keyword, value}, nil, 0)
	content := newParentNodeInArena(arena, 2, true, []*Node{pair}, nil, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{content}, nil, 0)

	normalizeElixirCompatibility(root, []byte(`shortcut:"syntax"`), lang)

	if got, want := content.ChildCount(), 1; got != want {
		t.Fatalf("map_content child count = %d, want %d", got, want)
	}
	keywords := content.Child(0)
	if keywords == nil || keywords.Type(lang) != "keywords" {
		t.Fatalf("map_content child = %v, want keywords", keywords)
	}
	if got := keywords.Child(0); got != pair {
		t.Fatalf("keywords child = %v, want original pair", got)
	}
	if got := pair.Parent(); got != keywords {
		t.Fatalf("pair parent = %v, want keywords", got)
	}
}

func TestNormalizeElixirMapContentUpdateOperatorWrapsRightKeywordPair(t *testing.T) {
	lang := testElixirCompatibilityLanguage()
	arena := newNodeArena(arenaClassFull)
	ident := newLeafNodeInArena(arena, 11, true, 2, 5, Point{Column: 2}, Point{Column: 5})
	bar := newLeafNodeInArena(arena, 12, false, 6, 7, Point{Column: 6}, Point{Column: 7})
	keyword := newLeafNodeInArena(arena, 5, true, 8, 13, Point{Column: 8}, Point{Column: 13})
	value := newLeafNodeInArena(arena, 6, true, 13, 20, Point{Column: 13}, Point{Column: 20})
	pair := newParentNodeInArena(arena, 4, true, []*Node{keyword, value}, nil, 0)
	content := newParentNodeInArena(arena, 2, true, []*Node{ident, bar, pair}, []FieldID{1, 2, 3}, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{content}, nil, 0)

	normalizeElixirCompatibility(root, []byte(`%{map | name:"Silly"}`), lang)

	if got, want := content.ChildCount(), 1; got != want {
		t.Fatalf("map_content child count = %d, want %d", got, want)
	}
	binary := content.Child(0)
	if binary == nil || binary.Type(lang) != "binary_operator" {
		t.Fatalf("map_content child = %v, want binary_operator", binary)
	}
	if got := content.FieldNameForChild(0, lang); got != "" {
		t.Fatalf("map_content child field = %q, want empty", got)
	}
	if got, want := binary.ChildCount(), 3; got != want {
		t.Fatalf("binary_operator child count = %d, want %d", got, want)
	}
	for i, want := range []string{"left", "operator", "right"} {
		if got := binary.FieldNameForChild(i, lang); got != want {
			t.Fatalf("binary_operator child[%d] field = %q, want %q", i, got, want)
		}
	}
	if got := binary.Child(0); got != ident {
		t.Fatalf("binary_operator child[0] = %v, want identifier", got)
	}
	if got := binary.Child(1); got != bar {
		t.Fatalf("binary_operator child[1] = %v, want | token", got)
	}
	keywords := binary.Child(2)
	if keywords == nil || keywords.Type(lang) != "keywords" {
		t.Fatalf("binary_operator child[2] = %v, want keywords", keywords)
	}
	if got := keywords.Child(0); got != pair {
		t.Fatalf("keywords child = %v, want original pair", got)
	}
}

func testElixirCompatibilityLanguage() *Language {
	return &Language{
		Name: "elixir",
		FieldNames: []string{
			"",
			"left",
			"operator",
			"right",
		},
		SymbolNames: []string{
			"EOF",
			"source",
			"map_content",
			"keywords",
			"pair",
			"keyword",
			"string",
			",",
			"boolean",
			"true",
			"false",
			"identifier",
			"|",
			"binary_operator",
			"=>",
			"nil",
			"nil",
			"comment",
			"_newline_before_comment",
			"list",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source", Visible: true, Named: true},
			{Name: "map_content", Visible: true, Named: true},
			{Name: "keywords", Visible: true, Named: true},
			{Name: "pair", Visible: true, Named: true},
			{Name: "keyword", Visible: true, Named: true},
			{Name: "string", Visible: true, Named: true},
			{Name: ",", Visible: true, Named: false},
			{Name: "boolean", Visible: true, Named: true},
			{Name: "true", Visible: true, Named: false},
			{Name: "false", Visible: true, Named: false},
			{Name: "identifier", Visible: true, Named: true},
			{Name: "|", Visible: true, Named: false},
			{Name: "binary_operator", Visible: true, Named: true},
			{Name: "=>", Visible: true, Named: false},
			{Name: "nil", Visible: true, Named: true},
			{Name: "nil", Visible: true, Named: false},
			{Name: "comment", Visible: true, Named: true},
			{Name: "_newline_before_comment", Visible: true, Named: true},
			{Name: "list", Visible: true, Named: true},
		},
	}
}
