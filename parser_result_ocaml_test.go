package gotreesitter

import "testing"

func TestNormalizeResultCompatibilityRestoresOCamlBooleanChild(t *testing.T) {
	lang := &Language{
		Name:        "ocaml",
		SymbolNames: []string{"EOF", "compilation_unit", "boolean", "true", "false", "or_operator", "||", "or"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "compilation_unit", Visible: true, Named: true},
			{Name: "boolean", Visible: true, Named: true},
			{Name: "true", Visible: true},
			{Name: "false", Visible: true},
			{Name: "or_operator", Visible: true, Named: true},
			{Name: "||", Visible: true},
			{Name: "or", Visible: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	boolean := newLeafNodeInArena(arena, 2, true, 0, 4, Point{}, Point{Column: 4})
	orOperator := newLeafNodeInArena(arena, 5, true, 5, 7, Point{Column: 5}, Point{Column: 7})
	root := newParentNodeInArena(arena, 1, true, []*Node{boolean, orOperator}, nil, 0)

	normalizeResultCompatibility(root, []byte("true ||"), &Parser{language: lang}, nil)

	child := boolean.Child(0)
	if child == nil || child.Type(lang) != "true" || child.IsNamed() {
		t.Fatalf("boolean child = %v, want anonymous true", child)
	}
	operatorChild := orOperator.Child(0)
	if operatorChild == nil || operatorChild.Type(lang) != "||" || operatorChild.IsNamed() {
		t.Fatalf("operator child = %v, want anonymous ||", operatorChild)
	}
}
