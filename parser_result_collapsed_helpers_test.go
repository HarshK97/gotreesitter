package gotreesitter

import "testing"

func TestNormalizeCollapsedNamedLeafChildrenRestoresCollapsedImplicitTypeVar(t *testing.T) {
	lang := &Language{
		SymbolNames: []string{"EOF", "root", "implicit_type", "var"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
			{Name: "implicit_type", Visible: true, Named: true},
			{Name: "var", Visible: true, Named: false},
		},
	}
	arena := newNodeArena(arenaClassFull)
	implicitType := newLeafNodeInArena(arena, 2, true, 4, 7, Point{Row: 0, Column: 4}, Point{Row: 0, Column: 7})
	root := newParentNodeInArena(arena, 1, true, []*Node{implicitType}, nil, 0)

	normalizeCollapsedNamedLeafChildren(root, lang, "implicit_type", "var")

	if got, want := implicitType.ChildCount(), 1; got != want {
		t.Fatalf("implicitType.ChildCount() = %d, want %d", got, want)
	}
	child := implicitType.Child(0)
	if child == nil {
		t.Fatal("implicitType.Child(0) = nil")
	}
	if got, want := child.Type(lang), "var"; got != want {
		t.Fatalf("child.Type() = %q, want %q", got, want)
	}
	if child.IsNamed() {
		t.Fatal("restored var child should remain anonymous")
	}
	if got, want := child.StartByte(), uint32(4); got != want {
		t.Fatalf("child.StartByte() = %d, want %d", got, want)
	}
	if got, want := child.EndByte(), uint32(7); got != want {
		t.Fatalf("child.EndByte() = %d, want %d", got, want)
	}
}

func TestNormalizeResultCompatibilityRestoresOCamlBooleanChild(t *testing.T) {
	lang := &Language{
		Name:        "ocaml",
		SymbolNames: []string{"EOF", "compilation_unit", "boolean", "true", "false", "or_operator", "||", "or"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "compilation_unit", Visible: true, Named: true},
			{Name: "boolean", Visible: true, Named: true},
			{Name: "true", Visible: true, Named: false},
			{Name: "false", Visible: true, Named: false},
			{Name: "or_operator", Visible: true, Named: true},
			{Name: "||", Visible: true, Named: false},
			{Name: "or", Visible: true, Named: false},
		},
	}
	arena := newNodeArena(arenaClassFull)
	boolean := newLeafNodeInArena(arena, 2, true, 0, 4, Point{}, Point{Column: 4})
	orOperator := newLeafNodeInArena(arena, 5, true, 5, 7, Point{Column: 5}, Point{Column: 7})
	root := newParentNodeInArena(arena, 1, true, []*Node{boolean, orOperator}, nil, 0)

	normalizeResultCompatibility(root, []byte("true ||"), &Parser{language: lang}, nil)

	child := boolean.Child(0)
	if child == nil {
		t.Fatal("boolean.Child(0) = nil")
	}
	if got, want := child.Type(lang), "true"; got != want {
		t.Fatalf("child.Type() = %q, want %q", got, want)
	}
	if child.IsNamed() {
		t.Fatal("restored true child should remain anonymous")
	}

	operatorChild := orOperator.Child(0)
	if operatorChild == nil {
		t.Fatal("orOperator.Child(0) = nil")
	}
	if got, want := operatorChild.Type(lang), "||"; got != want {
		t.Fatalf("operator child Type() = %q, want %q", got, want)
	}
	if operatorChild.IsNamed() {
		t.Fatal("restored || child should remain anonymous")
	}
}

// TestNormalizeResultCollapsedNamedLeafChildrenApexKeywordWrapper covers the
// Apex `keyword -> 'keyword'` family, where the named wrapper and the anonymous
// token child share a name but differ in named-ness. The collapsed leaf must
// regain its anonymous same-name token child.
func TestNormalizeResultCollapsedNamedLeafChildrenApexKeywordWrapper(t *testing.T) {
	lang := &Language{
		Name:        "apex",
		SymbolNames: []string{"EOF", "root", "super", "super"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
			{Name: "super", Visible: true, Named: true},  // symbol 2: named wrapper
			{Name: "super", Visible: true, Named: false}, // symbol 3: anonymous token
		},
	}
	arena := newNodeArena(arenaClassFull)
	superNode := newLeafNodeInArena(arena, 2, true, 621, 626, Point{Row: 0, Column: 621}, Point{Row: 0, Column: 626})
	root := newParentNodeInArena(arena, 1, true, []*Node{superNode}, nil, 0)

	normalizeResultCollapsedNamedLeafChildren(root, nil, lang)

	if got, want := superNode.ChildCount(), 1; got != want {
		t.Fatalf("super.ChildCount() = %d, want %d", got, want)
	}
	child := superNode.Child(0)
	if child == nil {
		t.Fatal("super.Child(0) = nil")
	}
	if got, want := child.Type(lang), "super"; got != want {
		t.Fatalf("child.Type() = %q, want %q", got, want)
	}
	if child.IsNamed() {
		t.Fatal("restored super token child should be anonymous")
	}
	if got, want := child.StartByte(), uint32(621); got != want {
		t.Fatalf("child.StartByte() = %d, want %d", got, want)
	}
	if got, want := child.EndByte(), uint32(626); got != want {
		t.Fatalf("child.EndByte() = %d, want %d", got, want)
	}
}
