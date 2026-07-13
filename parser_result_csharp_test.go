package gotreesitter

import "testing"

func TestCSharpLargeNamespaceAltStatsCanSkipPrimary(t *testing.T) {
	child := csharpRecoveredDeclarationStats{total: 103, methods: 93}
	alt := csharpRecoveredDeclarationStats{total: 126, methods: 116}
	if !csharpLargeNamespaceAltStatsCanSkipPrimary(csharpLargeNamespaceRecoveryBodyBytes, child, alt) {
		t.Fatal("large, clear alt win should skip primary source recovery")
	}
	if csharpLargeNamespaceAltStatsCanSkipPrimary(csharpLargeNamespaceRecoveryBodyBytes-1, child, alt) {
		t.Fatal("small namespace must keep primary source recovery")
	}
	if csharpLargeNamespaceAltStatsCanSkipPrimary(csharpLargeNamespaceRecoveryBodyBytes, child, csharpRecoveredDeclarationStats{total: 110, methods: 96}) {
		t.Fatal("marginal alt win must keep primary source recovery")
	}
	if csharpLargeNamespaceAltStatsCanSkipPrimary(csharpLargeNamespaceRecoveryBodyBytes, child, csharpRecoveredDeclarationStats{total: 140, methods: 90}) {
		t.Fatal("alt with fewer methods must keep primary source recovery")
	}
}

func TestCSharpParseQueryExpressionSpecWithGroupIntoOrder(t *testing.T) {
	src := []byte("from a in sourceA\n" +
		"        join b in sourceB on a.FK equals b.PK\n" +
		"        group a by a.X into g\n" +
		"        orderby g ascending\n" +
		"        select new { A.A, B.B }")
	spec, ok := csharpParseQueryExpressionSpec(src, csharpQueryAssignmentSpec{
		queryStart: 0,
		queryEnd:   uint32(len(src)),
	})
	if !ok {
		t.Fatal("expected query expression spec")
	}
	if got, want := len(spec.clauses), 5; got != want {
		t.Fatalf("clause count = %d, want %d", got, want)
	}
}

func TestCSharpFindTopLevelOperatorHandlesLambdaArrow(t *testing.T) {
	src := []byte("scoped => null")
	pos, ok := csharpFindTopLevelOperator(src, 0, uint32(len(src)), "=>")
	if !ok {
		t.Fatal("expected lambda arrow")
	}
	if want := uint32(len("scoped ")); pos != want {
		t.Fatalf("arrow pos = %d, want %d", pos, want)
	}
}

func TestCSharpTopLevelChunkSpansHandleAttributeCorpus(t *testing.T) {
	src := []byte("[A(B.C)]\n" +
		"class D {}\n\n" +
		"[NS.A(B.C)]\n" +
		"class D {}\n\n" +
		"[One][Two]\n" +
		"[Three]\n" +
		"class A { }\n\n" +
		"[A,B()][C]\n" +
		"struct A { }\n\n" +
		"class Zzz {\n" +
		"  [A,B()][C]\n" +
		"  public int Z;\n" +
		"}\n\n" +
		"class Methods {\n" +
		"  [ValidatedContract]\n" +
		"  int Method1() { return 0; }\n\n" +
		"  [method: ValidatedContract]\n" +
		"  int Method2() { return 0; }\n\n" +
		"  [return: ValidatedContract]\n" +
		"  int Method3() { return 0; }\n" +
		"}\n\n" +
		"[Single]\n" +
		"enum A { B, C }\n\n" +
		"class Zzz {\n" +
		"  [A,B()][C]\n" +
		"  public event EventHandler SomeEvent { add { } remove { } }\n" +
		"}\n\n" +
		"class Class<[A, B][C()]T1> {\n" +
		"  void Method<[E] [F, G(1)] T2>() {\n" +
		"  }\n" +
		"}\n\n" +
		"class Zzz {\n" +
		"  public event EventHandler SomeEvent {\n" +
		"    [A,B()][C] add { }\n" +
		"    [A,B()][C] remove { }\n" +
		"  }\n" +
		"}\n")
	spans := csharpTopLevelChunkSpans(src)
	if got, want := len(spans), 10; got != want {
		t.Fatalf("chunk span count = %d, want %d: %#v", got, want, spans)
	}
}

func TestNormalizeCSharpCollapsedLeafChildrenRestoresMatrixBlockers(t *testing.T) {
	lang := &Language{
		Name: "c_sharp",
		SymbolNames: []string{
			"EOF",
			"root",
			"boolean_literal",
			"true",
			"false",
			"modifier",
			"public",
			"alias_qualified_name",
			"identifier",
			"global",
			"lambda_expression",
			"argument",
			"string_literal",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
			{Name: "boolean_literal", Visible: true, Named: true},
			{Name: "true", Visible: true, Named: false},
			{Name: "false", Visible: true, Named: false},
			{Name: "modifier", Visible: true, Named: true},
			{Name: "public", Visible: true, Named: false},
			{Name: "alias_qualified_name", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: "global", Visible: true, Named: false},
			{Name: "lambda_expression", Visible: true, Named: true},
			{Name: "argument", Visible: true, Named: true},
			{Name: "string_literal", Visible: true, Named: true},
		},
		FieldNames: []string{"", "type"},
	}
	source := []byte("public false global async $\"x\"")
	arena := newNodeArena(arenaClassFull)
	modifier := newLeafNodeInArena(arena, 5, true, 0, 6, Point{}, Point{Column: 6})
	boolLit := newLeafNodeInArena(arena, 2, true, 7, 12, Point{Column: 7}, Point{Column: 12})
	identifier := newLeafNodeInArena(arena, 8, true, 13, 19, Point{Column: 13}, Point{Column: 19})
	alias := newParentNodeInArena(arena, 7, true, []*Node{identifier}, nil, 0)
	asyncIdent := newLeafNodeInArena(arena, 8, true, 20, 25, Point{Column: 20}, Point{Column: 25})
	lambdaFields := cloneFieldIDSliceInArena(arena, []FieldID{1})
	lambda := newParentNodeInArena(arena, 10, true, []*Node{asyncIdent}, lambdaFields, 0)
	stringLiteral := newLeafNodeInArena(arena, 12, true, 27, 30, Point{Column: 27}, Point{Column: 30})
	argument := newParentNodeInArena(arena, 11, true, []*Node{stringLiteral}, nil, 0)
	argument.startByte = 27
	argument.startPoint = Point{Column: 27}
	root := newParentNodeInArena(arena, 1, true, []*Node{modifier, boolLit, alias, lambda, argument}, nil, 0)

	normalizeCSharpSurfaceCompatibility(root, source, lang)

	if child := modifier.Child(0); child == nil || child.Type(lang) != "public" || child.IsNamed() {
		t.Fatalf("modifier child = %#v, want anonymous public token", child)
	}
	if child := boolLit.Child(0); child == nil || child.Type(lang) != "false" || child.IsNamed() {
		t.Fatalf("boolean_literal child = %#v, want anonymous false token", child)
	}
	if child := identifier.Child(0); child == nil || child.Type(lang) != "global" || child.IsNamed() {
		t.Fatalf("alias-qualified identifier child = %#v, want anonymous global token", child)
	}
	if got := lambda.FieldNameForChild(0, lang); got != "" {
		t.Fatalf("async lambda marker field = %q, want empty", got)
	}
	if got := asyncIdent.Type(lang); got != "modifier" {
		t.Fatalf("async lambda marker type = %q, want modifier", got)
	}
	if got := asyncIdent.ChildCount(); got != 0 {
		t.Fatalf("async modifier child count = %d, want 0", got)
	}
	if got, want := argument.StartByte(), uint32(26); got != want {
		t.Fatalf("interpolated argument start = %d, want %d", got, want)
	}
}

func TestNormalizeCSharpImplicitVarTypes(t *testing.T) {
	lang := &Language{
		Name: "c_sharp",
		SymbolNames: []string{
			"EOF",
			"root",
			"variable_declaration",
			"identifier",
			"implicit_type",
			"var",
			"declaration_pattern",
			"recursive_pattern",
			"positional_pattern_clause",
			"subpattern",
			"parenthesized_pattern",
			"(",
			")",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
			{Name: "variable_declaration", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: "implicit_type", Visible: true, Named: true},
			{Name: "var", Visible: true, Named: false},
			{Name: "declaration_pattern", Visible: true, Named: true},
			{Name: "recursive_pattern", Visible: true, Named: true},
			{Name: "positional_pattern_clause", Visible: true, Named: true},
			{Name: "subpattern", Visible: true, Named: true},
			{Name: "parenthesized_pattern", Visible: true, Named: true},
			{Name: "(", Visible: true, Named: false},
			{Name: ")", Visible: true, Named: false},
		},
	}
	source := []byte("var x = y;")
	arena := newNodeArena(arenaClassFull)
	varIdent := newLeafNodeInArena(arena, 3, true, 0, 3, Point{}, Point{Column: 3})
	nameIdent := newLeafNodeInArena(arena, 3, true, 4, 5, Point{Column: 4}, Point{Column: 5})
	decl := newParentNodeInArena(arena, 2, true, []*Node{varIdent, nameIdent}, nil, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{decl}, nil, 0)

	normalizeCSharpImplicitVarTypes(root, source, lang)

	typeNode := decl.Child(0)
	if typeNode == nil || typeNode.Type(lang) != "implicit_type" {
		t.Fatalf("type child = %#v, want implicit_type", typeNode)
	}
	varTok := typeNode.Child(0)
	if varTok == nil || varTok.Type(lang) != "var" || varTok.IsNamed() {
		t.Fatalf("implicit type token = %#v, want anonymous var", varTok)
	}
	if got := decl.Child(1).Type(lang); got != "identifier" {
		t.Fatalf("name child type = %q, want identifier", got)
	}
}

func TestNormalizeCSharpParenthesizedVarPatterns(t *testing.T) {
	lang := &Language{
		Name: "c_sharp",
		SymbolNames: []string{
			"EOF",
			"root",
			"identifier",
			"implicit_type",
			"var",
			"declaration_pattern",
			"recursive_pattern",
			"positional_pattern_clause",
			"subpattern",
			"parenthesized_pattern",
			"(",
			")",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: "implicit_type", Visible: true, Named: true},
			{Name: "var", Visible: true, Named: false},
			{Name: "declaration_pattern", Visible: true, Named: true},
			{Name: "recursive_pattern", Visible: true, Named: true},
			{Name: "positional_pattern_clause", Visible: true, Named: true},
			{Name: "subpattern", Visible: true, Named: true},
			{Name: "parenthesized_pattern", Visible: true, Named: true},
			{Name: "(", Visible: true, Named: false},
			{Name: ")", Visible: true, Named: false},
		},
	}
	source := []byte("(var a)")
	arena := newNodeArena(arenaClassFull)
	varIdent := newLeafNodeInArena(arena, 2, true, 1, 4, Point{Column: 1}, Point{Column: 4})
	nameIdent := newLeafNodeInArena(arena, 2, true, 5, 6, Point{Column: 5}, Point{Column: 6})
	decl := newParentNodeInArena(arena, 5, true, []*Node{varIdent, nameIdent}, nil, 0)
	subpattern := newParentNodeInArena(arena, 8, true, []*Node{decl}, nil, 0)
	clause := newParentNodeInArena(arena, 7, true, []*Node{subpattern}, nil, 0)
	recursive := newParentNodeInArena(arena, 6, true, []*Node{clause}, nil, 0)
	recursive.startByte = 0
	recursive.endByte = 7
	recursive.startPoint = Point{}
	recursive.endPoint = Point{Column: 7}
	root := newParentNodeInArena(arena, 1, true, []*Node{recursive}, nil, 0)

	normalizeCSharpImplicitVarTypes(root, source, lang)
	normalizeCSharpParenthesizedVarPatterns(root, source, lang)

	if got := recursive.Type(lang); got != "parenthesized_pattern" {
		t.Fatalf("pattern type = %q, want parenthesized_pattern", got)
	}
	if got := recursive.ChildCount(); got != 3 {
		t.Fatalf("pattern child count = %d, want 3", got)
	}
	if got := recursive.Child(1).Type(lang); got != "declaration_pattern" {
		t.Fatalf("pattern payload type = %q, want declaration_pattern", got)
	}
	if got := recursive.Child(1).Child(0).Type(lang); got != "implicit_type" {
		t.Fatalf("declaration type = %q, want implicit_type", got)
	}
}
