package gotreesitter

import "testing"

func TestNormalizeDCallExpressionPropertyTypesWrapsQualifiedTarget(t *testing.T) {
	lang := &Language{
		Name:        "d",
		SymbolNames: []string{"EOF", "call_expression", "type", "property_expression", "identifier", ".", "named_arguments"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "call_expression", Visible: true, Named: true},
			{Name: "type", Visible: true, Named: true},
			{Name: "property_expression", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: ".", Visible: true, Named: false},
			{Name: "named_arguments", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	a := newLeafNodeInArena(arena, 4, true, 0, 1, Point{}, Point{Column: 1})
	dot1 := newLeafNodeInArena(arena, 5, false, 1, 2, Point{Column: 1}, Point{Column: 2})
	b := newLeafNodeInArena(arena, 4, true, 2, 3, Point{Column: 2}, Point{Column: 3})
	left := newParentNodeInArena(arena, 3, true, []*Node{a, dot1, b}, nil, 0)
	dot2 := newLeafNodeInArena(arena, 5, false, 3, 4, Point{Column: 3}, Point{Column: 4})
	c := newLeafNodeInArena(arena, 4, true, 4, 5, Point{Column: 4}, Point{Column: 5})
	prop := newParentNodeInArena(arena, 3, true, []*Node{left, dot2, c}, nil, 0)
	args := newLeafNodeInArena(arena, 6, true, 5, 7, Point{Column: 5}, Point{Column: 7})
	call := newParentNodeInArena(arena, 1, true, []*Node{prop, args}, nil, 0)

	normalizeDCallExpressionPropertyTypes(call, lang)

	if got, want := call.children[0].Type(lang), "type"; got != want {
		t.Fatalf("call.children[0].Type = %q, want %q", got, want)
	}
	if got, want := len(call.children[0].children), 5; got != want {
		t.Fatalf("type child count = %d, want %d", got, want)
	}
}

func TestNormalizeDCallExpressionPropertyTypesWrapsQualifiedTemplateTarget(t *testing.T) {
	lang := &Language{
		Name:        "d",
		SymbolNames: []string{"EOF", "call_expression", "type", "property_expression", "identifier", ".", "template_instance", "template_arguments", "!", "named_arguments"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "call_expression", Visible: true, Named: true},
			{Name: "type", Visible: true, Named: true},
			{Name: "property_expression", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: ".", Visible: true, Named: false},
			{Name: "template_instance", Visible: true, Named: true},
			{Name: "template_arguments", Visible: true, Named: true},
			{Name: "!", Visible: true, Named: false},
			{Name: "named_arguments", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	a := newLeafNodeInArena(arena, 4, true, 0, 1, Point{}, Point{Column: 1})
	dot1 := newLeafNodeInArena(arena, 5, false, 1, 2, Point{Column: 1}, Point{Column: 2})
	instance := newLeafNodeInArena(arena, 4, true, 2, 10, Point{Column: 2}, Point{Column: 10})
	left := newParentNodeInArena(arena, 3, true, []*Node{a, dot1, instance}, nil, 0)
	dot2 := newLeafNodeInArena(arena, 5, false, 10, 11, Point{Column: 10}, Point{Column: 11})
	name := newLeafNodeInArena(arena, 4, true, 11, 19, Point{Column: 11}, Point{Column: 19})
	bang := newLeafNodeInArena(arena, 8, false, 19, 20, Point{Column: 19}, Point{Column: 20})
	arg := newLeafNodeInArena(arena, 4, true, 20, 26, Point{Column: 20}, Point{Column: 26})
	templateArgs := newParentNodeInArena(arena, 7, true, []*Node{bang, arg}, nil, 0)
	templateInstance := newParentNodeInArena(arena, 6, true, []*Node{name, templateArgs}, nil, 0)
	prop := newParentNodeInArena(arena, 3, true, []*Node{left, dot2, templateInstance}, nil, 0)
	args := newLeafNodeInArena(arena, 9, true, 26, 28, Point{Column: 26}, Point{Column: 28})
	call := newParentNodeInArena(arena, 1, true, []*Node{prop, args}, nil, 0)

	normalizeDCallExpressionPropertyTypes(call, lang)

	typ := call.children[0]
	if typ == nil || typ.Type(lang) != "type" {
		t.Fatalf("call child[0] = %v, want type", typ)
	}
	if got, want := len(typ.children), 5; got != want {
		t.Fatalf("type child count = %d, want %d", got, want)
	}
	if got, want := typ.children[4].Type(lang), "template_instance"; got != want {
		t.Fatalf("type child[4] = %q, want %q", got, want)
	}
}

func TestNormalizeDCallExpressionSimpleTypeCalleesUnwrapsSingleIdentifier(t *testing.T) {
	lang := &Language{
		Name:        "d",
		SymbolNames: []string{"EOF", "call_expression", "type", "identifier", "named_arguments"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "call_expression", Visible: true, Named: true},
			{Name: "type", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: "named_arguments", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	ident := newLeafNodeInArena(arena, 3, true, 0, 10, Point{}, Point{Column: 10})
	typ := newParentNodeInArena(arena, 2, true, []*Node{ident}, nil, 0)
	args := newLeafNodeInArena(arena, 4, true, 10, 12, Point{Column: 10}, Point{Column: 12})
	call := newParentNodeInArena(arena, 1, true, []*Node{typ, args}, nil, 0)

	normalizeDCallExpressionSimpleTypeCallees(call, lang)

	if got, want := call.children[0].Type(lang), "identifier"; got != want {
		t.Fatalf("call.children[0].Type = %q, want %q", got, want)
	}
}
