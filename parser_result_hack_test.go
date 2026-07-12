package gotreesitter

import "testing"

// Hack's true/false/null literal-wrapper rules used to live in a dedicated
// normalizeHackCompatibility adapter (parser_result_hack.go, now removed).
// They are data-driven now: see the "hack" entries in
// resultCollapsedNamedLeafRules (parser_result_collapsed_helpers.go), applied
// by normalizeResultCollapsedNamedLeafChildren for every language. The tests
// below exercise that shared entry point directly.

func TestNormalizeHackBooleanLiteralsRestoreTokenChildren(t *testing.T) {
	lang := &Language{
		Name:        "hack",
		SymbolNames: []string{"EOF", "script", "true", "false", "true", "false"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "script", Visible: true, Named: true},
			{Name: "true", Visible: true, Named: true},
			{Name: "false", Visible: true, Named: true},
			{Name: "true", Visible: true, Named: false},
			{Name: "false", Visible: true, Named: false},
		},
	}
	source := []byte("true false")
	arena := newNodeArena(arenaClassFull)
	trueNode := newLeafNodeInArena(arena, 2, true, 0, 4, Point{}, Point{Column: 4})
	falseNode := newLeafNodeInArena(arena, 3, true, 5, 10, Point{Column: 5}, Point{Column: 10})
	root := newParentNodeInArena(arena, 1, true, []*Node{trueNode, falseNode}, nil, 0)

	normalizeResultCollapsedNamedLeafChildren(root, source, lang)

	if got, want := trueNode.ChildCount(), 1; got != want {
		t.Fatalf("true child count = %d, want %d", got, want)
	}
	if got, want := trueNode.Child(0).Type(lang), "true"; got != want {
		t.Fatalf("true child type = %q, want %q", got, want)
	}
	if trueNode.Child(0).IsNamed() {
		t.Fatal("true token child should be anonymous")
	}
	if got, want := falseNode.ChildCount(), 1; got != want {
		t.Fatalf("false child count = %d, want %d", got, want)
	}
	if got, want := falseNode.Child(0).Type(lang), "false"; got != want {
		t.Fatalf("false child type = %q, want %q", got, want)
	}
	if falseNode.Child(0).IsNamed() {
		t.Fatal("false token child should be anonymous")
	}
}

func TestNormalizeHackNullLiteralRestoresTokenChild(t *testing.T) {
	lang := &Language{
		Name:        "hack",
		SymbolNames: []string{"EOF", "script", "null", "null"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "script", Visible: true, Named: true},
			{Name: "null", Visible: true, Named: true},
			{Name: "null", Visible: true, Named: false},
		},
	}
	source := []byte("null")
	arena := newNodeArena(arenaClassFull)
	nullNode := newLeafNodeInArena(arena, 2, true, 0, 4, Point{}, Point{Column: 4})
	root := newParentNodeInArena(arena, 1, true, []*Node{nullNode}, nil, 0)

	normalizeResultCollapsedNamedLeafChildren(root, source, lang)

	if got, want := nullNode.ChildCount(), 1; got != want {
		t.Fatalf("null child count = %d, want %d", got, want)
	}
	if got, want := nullNode.Child(0).Type(lang), "null"; got != want {
		t.Fatalf("null child type = %q, want %q", got, want)
	}
	if nullNode.Child(0).IsNamed() {
		t.Fatal("null token child should be anonymous")
	}
}
