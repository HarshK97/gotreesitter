package gotreesitter

import "testing"

func TestNormalizeHCLConfigFileRootDropsTopLevelWhitespace(t *testing.T) {
	lang := &Language{
		Name:        "hcl",
		SymbolNames: []string{"EOF", "config_file", "comment", "_whitespace", "body"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "config_file", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
			{Name: "_whitespace", Visible: false, Named: false},
			{Name: "body", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	comment := newLeafNodeInArena(arena, 2, true, 0, 4, Point{}, Point{Column: 4})
	ws1 := newLeafNodeInArena(arena, 3, false, 4, 5, Point{Column: 4}, Point{Row: 1})
	body := newLeafNodeInArena(arena, 4, true, 5, 9, Point{Row: 1}, Point{Row: 1, Column: 4})
	ws2 := newLeafNodeInArena(arena, 3, false, 9, 10, Point{Row: 1, Column: 4}, Point{Row: 2})
	root := newParentNodeInArena(arena, 1, true, []*Node{comment, ws1, body, ws2}, nil, 0)

	normalizeHCLConfigFileRoot(root, nil, lang)

	if got, want := len(root.children), 2; got != want {
		t.Fatalf("len(root.children) = %d, want %d", got, want)
	}
	if got, want := root.children[0].Type(lang), "comment"; got != want {
		t.Fatalf("root.children[0].Type = %q, want %q", got, want)
	}
	if got, want := root.children[1].Type(lang), "body"; got != want {
		t.Fatalf("root.children[1].Type = %q, want %q", got, want)
	}
}

func TestNormalizeHCLConfigFileRootFiltersFinalRefsWithoutDrain(t *testing.T) {
	lang := &Language{
		Name:        "hcl",
		SymbolNames: []string{"EOF", "config_file", "comment", "_whitespace", "body"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "config_file", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
			{Name: "_whitespace", Visible: false, Named: false},
			{Name: "body", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	arena.finalChildRefs = true
	comment := newCompactFullLeafInArena(arena, 2, true, 0, 4, Point{}, Point{Column: 4})
	comment.parseState = 12
	ws := newCompactFullLeafInArena(arena, 3, false, 4, 5, Point{Column: 4}, Point{Row: 1})
	ws.parseState = 13
	body := newCompactFullLeafInArena(arena, 4, true, 5, 9, Point{Row: 1}, Point{Row: 1, Column: 4})
	body.parseState = 14
	parent := newPendingParentInArena(arena, 1, true, 0, []stackEntry{
		newStackEntryCompactFullLeaf(comment.parseState, comment),
		newStackEntryCompactFullLeaf(ws.parseState, ws),
		newStackEntryCompactFullLeaf(body.parseState, body),
	}, 0, 9, Point{}, Point{Row: 1, Column: 4}, false)
	parent.parseState = 15
	entry := newStackEntryPendingParent(parent.parseState, parent)
	root := materializeStackEntryPendingParent(arena, &entry, pendingParentMaterializeForFinalTree)

	normalizeHCLConfigFileRoot(root, nil, lang)

	if got := arena.finalChildRefsMaterializedParents; got != 0 {
		t.Fatalf("final child ref range materialized parents = %d, want 0", got)
	}
	if !nodeHasFinalChildRefs(root) {
		t.Fatal("root lost final-child refs")
	}
	if got := root.ChildCount(); got != 2 {
		t.Fatalf("root child count = %d, want 2", got)
	}
	if got := root.Child(0).Type(lang); got != "comment" {
		t.Fatalf("root child 0 = %q, want comment", got)
	}
	if got := root.Child(1).Type(lang); got != "body" {
		t.Fatalf("root child 1 = %q, want body", got)
	}
}

func TestNormalizeHCLConfigFileRootSnapsBodyToStructuralChildren(t *testing.T) {
	lang := &Language{
		Name:        "hcl",
		SymbolNames: []string{"EOF", "config_file", "body", "block", "comment"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "config_file", Visible: true, Named: true},
			{Name: "body", Visible: true, Named: true},
			{Name: "block", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	block := newLeafNodeInArena(arena, 3, true, 10, 20, Point{Row: 1}, Point{Row: 1, Column: 10})
	comment := newLeafNodeInArena(arena, 4, true, 22, 30, Point{Row: 2}, Point{Row: 2, Column: 8})
	body := newParentNodeInArena(arena, 2, true, []*Node{block, comment}, nil, 0)
	body.startByte = 0
	body.startPoint = Point{}
	body.endByte = 40
	body.endPoint = Point{Row: 4}
	root := newParentNodeInArena(arena, 1, true, []*Node{body}, nil, 0)

	normalizeHCLConfigFileRoot(root, nil, lang)

	if got, want := body.startByte, uint32(10); got != want {
		t.Fatalf("body.startByte = %d, want %d", got, want)
	}
	if got, want := body.endByte, uint32(30); got != want {
		t.Fatalf("body.endByte = %d, want %d", got, want)
	}
	if got, want := body.startPoint, (Point{Row: 1}); got != want {
		t.Fatalf("body.startPoint = %#v, want %#v", got, want)
	}
	if got, want := body.endPoint, (Point{Row: 2, Column: 8}); got != want {
		t.Fatalf("body.endPoint = %#v, want %#v", got, want)
	}
}

func TestNormalizeSQLRecoveredSelectRootWrapsFlatSelectClause(t *testing.T) {
	lang := &Language{
		Name: "sql",
		SymbolNames: []string{
			"EOF", "source_file", "SELECT", "_aliasable_expression", "_expression", "type_cast",
			"select_clause_body_repeat1", ",", "comment", "select_statement", "select_clause",
			"select_clause_body", "NULL", "NULL",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "SELECT", Visible: true, Named: false},
			{Name: "_aliasable_expression", Visible: true, Named: true},
			{Name: "_expression", Visible: true, Named: true},
			{Name: "type_cast", Visible: true, Named: true},
			{Name: "select_clause_body_repeat1", Visible: true, Named: true},
			{Name: ",", Visible: true, Named: false},
			{Name: "comment", Visible: true, Named: true},
			{Name: "select_statement", Visible: true, Named: true},
			{Name: "select_clause", Visible: true, Named: true},
			{Name: "select_clause_body", Visible: true, Named: true},
			{Name: "NULL", Visible: true, Named: true},
			{Name: "NULL", Visible: true, Named: false},
		},
	}

	arena := newNodeArena(arenaClassFull)
	selectTok := newLeafNodeInArena(arena, 2, false, 0, 6, Point{}, Point{Column: 6})
	firstExprLeaf := newLeafNodeInArena(arena, 5, true, 7, 8, Point{Column: 7}, Point{Column: 8})
	firstExpr := newParentNodeInArena(arena, 4, true, []*Node{firstExprLeaf}, nil, 0)
	firstAlias := newParentNodeInArena(arena, 3, true, []*Node{firstExpr}, nil, 0)
	comma1 := newLeafNodeInArena(arena, 7, false, 8, 9, Point{Column: 8}, Point{Column: 9})
	comment1 := newLeafNodeInArena(arena, 8, true, 10, 20, Point{Column: 10}, Point{Row: 1})
	secondExprLeaf := newLeafNodeInArena(arena, 5, true, 21, 22, Point{Row: 1}, Point{Row: 1, Column: 1})
	secondExpr := newParentNodeInArena(arena, 4, true, []*Node{secondExprLeaf}, nil, 0)
	secondAlias := newParentNodeInArena(arena, 3, true, []*Node{secondExpr}, nil, 0)
	repeat := newParentNodeInArena(arena, 6, true, []*Node{comma1, comment1, secondAlias}, nil, 0)
	comma2 := newLeafNodeInArena(arena, 7, false, 22, 23, Point{Row: 1, Column: 1}, Point{Row: 1, Column: 2})
	comment2 := newLeafNodeInArena(arena, 8, true, 24, 30, Point{Row: 1, Column: 3}, Point{Row: 1, Column: 9})
	root := newParentNodeInArena(arena, 1, true, []*Node{selectTok, firstAlias, repeat, comma2, comment2}, nil, 0)

	normalizeSQLRecoveredSelectRoot(root, lang)

	if got, want := len(root.children), 1; got != want {
		t.Fatalf("len(root.children) = %d, want %d", got, want)
	}
	if got, want := root.children[0].Type(lang), "select_statement"; got != want {
		t.Fatalf("root.children[0].Type = %q, want %q", got, want)
	}
	body := root.children[0].children[0].children[1]
	if got, want := body.Type(lang), "select_clause_body"; got != want {
		t.Fatalf("body.Type = %q, want %q", got, want)
	}
	if got, want := body.children[0].Type(lang), "type_cast"; got != want {
		t.Fatalf("body.children[0].Type = %q, want %q", got, want)
	}
	if got, want := body.children[len(body.children)-1].Type(lang), "NULL"; got != want {
		t.Fatalf("body.children[last].Type = %q, want %q", got, want)
	}
	if !root.HasError() {
		t.Fatalf("root.HasError = false, want true")
	}
}

func TestNormalizeSQLRecoveredSelectRootWrapsDirectTypeCastFragments(t *testing.T) {
	lang := &Language{
		Name: "sql",
		SymbolNames: []string{
			"EOF", "source_file", "SELECT", "type_cast", "select_clause_body_repeat1",
			",", "comment", "select_statement", "select_clause", "select_clause_body", "NULL", "NULL",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "SELECT", Visible: true, Named: false},
			{Name: "type_cast", Visible: true, Named: true},
			{Name: "select_clause_body_repeat1", Visible: false, Named: false},
			{Name: ",", Visible: true, Named: false},
			{Name: "comment", Visible: true, Named: true},
			{Name: "select_statement", Visible: true, Named: true},
			{Name: "select_clause", Visible: true, Named: true},
			{Name: "select_clause_body", Visible: true, Named: true},
			{Name: "NULL", Visible: true, Named: true},
			{Name: "NULL", Visible: true, Named: false},
		},
	}

	arena := newNodeArena(arenaClassFull)
	selectTok := newLeafNodeInArena(arena, 2, false, 0, 6, Point{}, Point{Column: 6})
	firstTypeCast := newLeafNodeInArena(arena, 3, true, 9, 18, Point{Row: 1, Column: 2}, Point{Row: 1, Column: 11})
	comma1 := newLeafNodeInArena(arena, 5, false, 18, 19, Point{Row: 1, Column: 11}, Point{Row: 1, Column: 12})
	comment1 := newLeafNodeInArena(arena, 6, true, 20, 45, Point{Row: 2}, Point{Row: 2, Column: 25})
	secondTypeCast := newLeafNodeInArena(arena, 3, true, 48, 57, Point{Row: 3, Column: 2}, Point{Row: 3, Column: 11})
	repeat := newParentNodeInArena(arena, 4, false, []*Node{comma1, comment1, secondTypeCast}, nil, 0)
	comma2 := newLeafNodeInArena(arena, 5, false, 57, 58, Point{Row: 3, Column: 11}, Point{Row: 3, Column: 12})
	comment2 := newLeafNodeInArena(arena, 6, true, 59, 84, Point{Row: 4}, Point{Row: 4, Column: 25})
	root := newParentNodeInArena(arena, 1, true, []*Node{selectTok, firstTypeCast, repeat, comma2, comment2}, nil, 0)

	normalizeSQLRecoveredSelectRoot(root, lang)

	if got, want := len(root.children), 1; got != want {
		t.Fatalf("len(root.children) = %d, want %d", got, want)
	}
	body := root.children[0].children[0].children[1]
	if got, want := body.Type(lang), "select_clause_body"; got != want {
		t.Fatalf("body.Type = %q, want %q", got, want)
	}
	if got, want := body.children[0].Type(lang), "type_cast"; got != want {
		t.Fatalf("body.children[0].Type = %q, want %q", got, want)
	}
	if got, want := body.children[3].Type(lang), "type_cast"; got != want {
		t.Fatalf("body.children[3].Type = %q, want %q", got, want)
	}
	if got, want := body.children[len(body.children)-1].Type(lang), "NULL"; got != want {
		t.Fatalf("body.children[last].Type = %q, want %q", got, want)
	}
	if !root.HasError() {
		t.Fatalf("root.HasError = false, want true")
	}
}

func TestNormalizeSQLTrailingSelectListErrorFoldsRootRecovery(t *testing.T) {
	lang := &Language{
		Name: "sql",
		SymbolNames: []string{
			"EOF", "source_file", "SELECT", "type_cast", ",", "comment",
			"select_statement", "select_clause", "select_clause_body", "NULL", "NULL",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "SELECT", Visible: true, Named: false},
			{Name: "type_cast", Visible: true, Named: true},
			{Name: ",", Visible: true, Named: false},
			{Name: "comment", Visible: true, Named: true},
			{Name: "select_statement", Visible: true, Named: true},
			{Name: "select_clause", Visible: true, Named: true},
			{Name: "select_clause_body", Visible: true, Named: true},
			{Name: "NULL", Visible: true, Named: true},
			{Name: "NULL", Visible: true, Named: false},
		},
	}

	arena := newNodeArena(arenaClassFull)
	selectTok := newLeafNodeInArena(arena, 2, false, 0, 6, Point{}, Point{Column: 6})
	typeCast := newLeafNodeInArena(arena, 3, true, 9, 18, Point{Row: 1, Column: 2}, Point{Row: 1, Column: 11})
	body := newParentNodeInArena(arena, 8, true, []*Node{typeCast}, nil, 0)
	selectClause := newParentNodeInArena(arena, 7, true, []*Node{selectTok, body}, nil, 0)
	stmt := newParentNodeInArena(arena, 6, true, []*Node{selectClause}, nil, 0)
	comma := newLeafNodeInArena(arena, 4, false, 18, 19, Point{Row: 1, Column: 11}, Point{Row: 1, Column: 12})
	errComma := newParentNodeInArena(arena, errorSymbol, true, []*Node{comma}, nil, 0)
	errComma.setHasError(true)
	errComma.setExtra(true)
	comment := newLeafNodeInArena(arena, 5, true, 20, 45, Point{Row: 2}, Point{Row: 2, Column: 25})
	comment.setExtra(true)
	root := newParentNodeInArena(arena, 1, true, []*Node{stmt, errComma, comment}, nil, 0)

	normalizeSQLTrailingSelectListError(root, lang)

	if got, want := len(root.children), 1; got != want {
		t.Fatalf("len(root.children) = %d, want %d", got, want)
	}
	body = root.children[0].children[0].children[1]
	if got, want := len(body.children), 4; got != want {
		t.Fatalf("len(body.children) = %d, want %d", got, want)
	}
	if got, want := body.children[1].Type(lang), ","; got != want {
		t.Fatalf("body.children[1].Type = %q, want %q", got, want)
	}
	if got, want := body.children[2].Type(lang), "comment"; got != want {
		t.Fatalf("body.children[2].Type = %q, want %q", got, want)
	}
	if got, want := body.children[3].Type(lang), "NULL"; got != want {
		t.Fatalf("body.children[3].Type = %q, want %q", got, want)
	}
	if !root.HasError() || !root.children[0].HasError() || !body.HasError() {
		t.Fatalf("expected root, select_statement, and body to retain errors")
	}
}

func TestNormalizeErlangSourceFileFormsSnapsBounds(t *testing.T) {
	lang := &Language{
		Name:        "erlang",
		FieldNames:  []string{"", "forms_only"},
		SymbolNames: []string{"EOF", "source_file", "comment", "fun_decl", "function_clause", "."},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
			{Name: "fun_decl", Visible: true, Named: true},
			{Name: "function_clause", Visible: true, Named: true},
			{Name: ".", Visible: true, Named: false},
		},
	}

	arena := newNodeArena(arenaClassFull)
	comment := newLeafNodeInArena(arena, 2, true, 0, 10, Point{}, Point{Column: 10})
	comment.setExtra(true)
	clause := newLeafNodeInArena(arena, 4, true, 12, 20, Point{Row: 1}, Point{Row: 1, Column: 8})
	innerComment := newLeafNodeInArena(arena, 2, true, 21, 30, Point{Row: 2}, Point{Row: 2, Column: 9})
	innerComment.setExtra(true)
	dot := newLeafNodeInArena(arena, 5, false, 31, 32, Point{Row: 3}, Point{Row: 3, Column: 1})
	funDecl := newParentNodeInArena(arena, 3, true, []*Node{clause, innerComment, dot}, nil, 0)
	funDecl.startByte = 0
	funDecl.startPoint = Point{}
	root := newParentNodeInArena(arena, 1, true, []*Node{comment, funDecl}, nil, 0)

	normalizeErlangSourceFileForms(root, lang)

	if got, want := funDecl.startByte, clause.startByte; got != want {
		t.Fatalf("funDecl.startByte = %d, want %d", got, want)
	}
	if got, want := funDecl.endByte, dot.endByte; got != want {
		t.Fatalf("funDecl.endByte = %d, want %d", got, want)
	}
}

func TestNormalizeErlangSourceFileFormsSkipsExprsMode(t *testing.T) {
	lang := &Language{
		Name:        "erlang",
		FieldNames:  []string{"", "forms_only"},
		SymbolNames: []string{"EOF", "source_file", "comment", "atom"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
			{Name: "atom", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	comment := newLeafNodeInArena(arena, 2, true, 0, 2, Point{}, Point{Column: 2})
	comment.setExtra(true)
	expr := newLeafNodeInArena(arena, 3, true, 3, 7, Point{Column: 3}, Point{Column: 7})
	root := newParentNodeInArena(arena, 1, true, []*Node{comment, expr}, nil, 0)

	normalizeErlangSourceFileForms(root, lang)

	if got := root.FieldNameForChild(1, lang); got != "" {
		t.Fatalf("root.FieldNameForChild(1) = %q, want empty", got)
	}
}

func TestNormalizeErlangReplacementClauses(t *testing.T) {
	lang := &Language{
		Name: "erlang",
		SymbolNames: []string{
			"EOF",
			"source_file",
			"replacement_cr_clauses",
			"replacement_function_clauses",
			"cr_clause",
			"function_clause",
			"atom",
			"call",
			"var",
			"expr_args",
			"clause_body",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "replacement_cr_clauses", Visible: true, Named: true},
			{Name: "replacement_function_clauses", Visible: true, Named: true},
			{Name: "cr_clause", Visible: true, Named: true},
			{Name: "function_clause", Visible: true, Named: true},
			{Name: "atom", Visible: true, Named: true},
			{Name: "call", Visible: true, Named: true},
			{Name: "var", Visible: true, Named: true},
			{Name: "expr_args", Visible: true, Named: true},
			{Name: "clause_body", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	name := newLeafNodeInArena(arena, 8, true, 0, 4, Point{}, Point{Column: 4})
	args := newLeafNodeInArena(arena, 9, true, 4, 8, Point{Column: 4}, Point{Column: 8})
	call := newParentNodeInArena(arena, 7, true, []*Node{name, args}, nil, 0)
	body := newLeafNodeInArena(arena, 10, true, 9, 20, Point{Column: 9}, Point{Column: 20})
	crClause := newParentNodeInArena(arena, 4, true, []*Node{call, body}, nil, 0)
	replacement := newParentNodeInArena(arena, 2, true, []*Node{crClause}, nil, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{replacement}, nil, 0)

	normalizeErlangReplacementClauses(root, lang)

	if got, want := replacement.Type(lang), "replacement_function_clauses"; got != want {
		t.Fatalf("replacement.Type = %q, want %q", got, want)
	}
	if got, want := crClause.Type(lang), "function_clause"; got != want {
		t.Fatalf("crClause.Type = %q, want %q", got, want)
	}
	if got, want := crClause.ChildCount(), 3; got != want {
		t.Fatalf("crClause.ChildCount = %d, want %d", got, want)
	}
	if got, want := crClause.Child(0).Type(lang), "var"; got != want {
		t.Fatalf("crClause.Child(0).Type = %q, want %q", got, want)
	}
	if got, want := crClause.Child(1).Type(lang), "expr_args"; got != want {
		t.Fatalf("crClause.Child(1).Type = %q, want %q", got, want)
	}
	if got, want := crClause.Child(2).Type(lang), "clause_body"; got != want {
		t.Fatalf("crClause.Child(2).Type = %q, want %q", got, want)
	}
}

func TestNormalizeElixirNestedCallTargetFields(t *testing.T) {
	lang := &Language{
		Name:        "elixir",
		FieldNames:  []string{"", "target", "existing"},
		SymbolNames: []string{"EOF", "source", "call", "arguments", "identifier"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "source", Visible: true, Named: true},
			{Name: "call", Visible: true, Named: true},
			{Name: "arguments", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	inner := newLeafNodeInArena(arena, 2, true, 0, 3, Point{}, Point{Column: 3})
	args := newLeafNodeInArena(arena, 3, true, 3, 5, Point{Column: 3}, Point{Column: 5})
	outer := newParentNodeInArena(arena, 2, true, []*Node{inner, args}, nil, 0)
	alreadySetInner := newLeafNodeInArena(arena, 2, true, 6, 9, Point{Column: 6}, Point{Column: 9})
	alreadySetArgs := newLeafNodeInArena(arena, 3, true, 9, 11, Point{Column: 9}, Point{Column: 11})
	alreadySet := newParentNodeInArena(arena, 2, true, []*Node{alreadySetInner, alreadySetArgs}, []FieldID{2, 0}, 0)
	root := newParentNodeInArena(arena, 1, true, []*Node{outer, alreadySet}, nil, 0)

	normalizeElixirNestedCallTargetFields(root, lang)

	if got, want := outer.FieldNameForChild(0, lang), "target"; got != want {
		t.Fatalf("outer.FieldNameForChild(0) = %q, want %q", got, want)
	}
	if got := outer.FieldNameForChild(1, lang); got != "" {
		t.Fatalf("outer.FieldNameForChild(1) = %q, want empty", got)
	}
	if got, want := alreadySet.FieldNameForChild(0, lang), "existing"; got != want {
		t.Fatalf("alreadySet.FieldNameForChild(0) = %q, want %q", got, want)
	}
}
