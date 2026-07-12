package gotreesitter

import "testing"

func TestNormalizeResultTerminalLeafNodesCollapsesRedundantAnonymousTokenChild(t *testing.T) {
	lang := &Language{
		TokenCount: 3,
		SymbolNames: []string{
			"EOF",
			".",
			"any_character",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: ".", Visible: true, Named: false},
			{Name: "any_character", Visible: true, Named: true},
			{Name: "root", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	child := newLeafNodeInArena(arena, 1, false, 11, 12, Point{Row: 0, Column: 11}, Point{Row: 0, Column: 12})
	token := newParentNodeInArena(arena, 2, true, []*Node{child}, nil, 0)
	token.startByte = 10
	token.endByte = 12
	token.startPoint = Point{Row: 0, Column: 10}
	token.endPoint = Point{Row: 0, Column: 12}
	root := newParentNodeInArena(arena, 3, true, []*Node{token}, nil, 0)

	counters, _, _ := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if got, want := counters.nodesRewritten, uint64(1); got != want {
		t.Fatalf("nodesRewritten = %d, want %d", got, want)
	}
	if got, want := token.ChildCount(), 0; got != want {
		t.Fatalf("token.ChildCount() = %d, want %d", got, want)
	}
	if got, want := token.Type(lang), "any_character"; got != want {
		t.Fatalf("token.Type() = %q, want %q", got, want)
	}
	if !token.IsNamed() {
		t.Fatal("terminal parent named flag was not preserved")
	}
	if got, want := token.StartByte(), uint32(11); got != want {
		t.Fatalf("token.StartByte() = %d, want %d", got, want)
	}
	if got, want := token.EndByte(), uint32(12); got != want {
		t.Fatalf("token.EndByte() = %d, want %d", got, want)
	}
}

func TestNormalizeResultTerminalLeafNodesStopsOnActiveParseBudget(t *testing.T) {
	lang := &Language{
		TokenCount: 3,
		SymbolNames: []string{
			"EOF",
			".",
			"token",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: ".", Visible: true, Named: false},
			{Name: "token", Visible: true, Named: true},
			{Name: "root", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	children := make([]*Node, 0, parseStopPollMask*2)
	for i := 0; i < parseStopPollMask*2; i++ {
		start := uint32(i)
		leaf := newLeafNodeInArena(arena, 1, false, start, start+1, Point{Column: start}, Point{Column: start + 1})
		parent := newParentNodeInArena(arena, 2, true, []*Node{leaf}, nil, 0)
		children = append(children, parent)
	}
	root := newParentNodeInArena(arena, 3, true, children, nil, 0)
	checks := 0
	stopCheck := func() ParseStopReason {
		checks++
		if checks >= 2 {
			return ParseStopTimeout
		}
		return ParseStopNone
	}

	counters, reason, errorSummary := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, stopCheck)

	if reason != ParseStopTimeout {
		t.Fatalf("stop reason = %q, want %q", reason, ParseStopTimeout)
	}
	if counters.nodesVisited == 0 {
		t.Fatal("nodesVisited = 0, want partial traversal before stop")
	}
	if counters.nodesVisited >= uint64(len(children)+1) {
		t.Fatalf("nodesVisited = %d, want traversal to stop before full tree", counters.nodesVisited)
	}
	if counters.nodesRewritten >= uint64(len(children)) {
		t.Fatalf("nodesRewritten = %d, want partial rewrite before stop", counters.nodesRewritten)
	}
	if errorSummary != resultErrorSummaryUnknown {
		t.Fatalf("error summary = %d, want unknown after interrupted traversal", errorSummary)
	}
}

func TestNormalizeResultTerminalLeafNodesSummarizesCleanTree(t *testing.T) {
	lang := &Language{
		TokenCount: 2,
		SymbolNames: []string{
			"EOF",
			"token",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "token", Visible: true},
			{Name: "root", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	leaf := newLeafNodeInArena(arena, 1, false, 0, 1, Point{}, Point{Column: 1})
	root := newParentNodeInArena(arena, 2, true, []*Node{leaf}, nil, 0)

	_, reason, errorSummary := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if reason != ParseStopNone {
		t.Fatalf("stop reason = %q, want none", reason)
	}
	if errorSummary != resultErrorSummaryClean {
		t.Fatalf("error summary = %d, want clean", errorSummary)
	}
}

func TestNormalizeResultTerminalLeafNodesFindsUnderFlaggedErrorDescendant(t *testing.T) {
	lang := &Language{
		TokenCount: 1,
		SymbolNames: []string{
			"EOF",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	errNode := newLeafNodeInArena(arena, errorSymbol, true, 0, 1, Point{}, Point{Column: 1})
	errNode.setHasError(true)
	root := newParentNodeInArena(arena, 1, true, []*Node{errNode}, nil, 0)
	root.setHasError(false)

	_, reason, errorSummary := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if reason != ParseStopNone {
		t.Fatalf("stop reason = %q, want none", reason)
	}
	if root.HasError() {
		t.Fatal("test setup changed root HasError to true")
	}
	if errorSummary != resultErrorSummaryPresent {
		t.Fatalf("error summary = %d, want present", errorSummary)
	}
}

func TestNormalizeResultTerminalLeafNodesWalksUnderFlaggedErrorRoot(t *testing.T) {
	lang := &Language{
		TokenCount: 3,
		SymbolNames: []string{
			"EOF",
			".",
			"token",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: ".", Visible: true},
			{Name: "token", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	leaf := newLeafNodeInArena(arena, 1, false, 0, 1, Point{}, Point{Column: 1})
	token := newParentNodeInArena(arena, 2, true, []*Node{leaf}, nil, 0)
	root := newParentNodeInArena(arena, errorSymbol, true, []*Node{token}, nil, 0)
	root.setHasError(false)

	counters, reason, errorSummary := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if reason != ParseStopNone {
		t.Fatalf("stop reason = %q, want none", reason)
	}
	if got, want := counters.nodesRewritten, uint64(1); got != want {
		t.Fatalf("nodesRewritten = %d, want %d", got, want)
	}
	if got := token.ChildCount(); got != 0 {
		t.Fatalf("token.ChildCount() = %d, want 0", got)
	}
	if errorSummary != resultErrorSummaryPresent {
		t.Fatalf("error summary = %d, want present", errorSummary)
	}
}

func TestNormalizeResultTerminalLeafNodesPreservesNonterminalWrapper(t *testing.T) {
	lang := &Language{
		TokenCount: 2,
		SymbolNames: []string{
			"EOF",
			"var",
			"implicit_type",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "var", Visible: true, Named: false},
			{Name: "implicit_type", Visible: true, Named: true},
			{Name: "root", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	child := newLeafNodeInArena(arena, 1, false, 4, 7, Point{Row: 0, Column: 4}, Point{Row: 0, Column: 7})
	wrapper := newParentNodeInArena(arena, 2, true, []*Node{child}, nil, 0)
	root := newParentNodeInArena(arena, 3, true, []*Node{wrapper}, nil, 0)

	counters, _, _ := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if got, want := counters.nodesRewritten, uint64(0); got != want {
		t.Fatalf("nodesRewritten = %d, want %d", got, want)
	}
	if got, want := wrapper.ChildCount(), 1; got != want {
		t.Fatalf("wrapper.ChildCount() = %d, want %d", got, want)
	}
	if got := wrapper.Child(0); got != child {
		t.Fatalf("wrapper.Child(0) = %p, want original child %p", got, child)
	}
}

func TestNormalizeResultTerminalLeafNodesCollapsesTerminalAliasTarget(t *testing.T) {
	lang := &Language{
		TokenCount: 2,
		SymbolNames: []string{
			"EOF",
			"]",
			"root",
			"]",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "]", Visible: true, Named: false},
			{Name: "root", Visible: true, Named: true},
			{Name: "]", Visible: true, Named: false},
		},
		AliasSequences: [][]Symbol{
			{3},
		},
	}
	arena := newNodeArena(arenaClassFull)
	child := newLeafNodeInArena(arena, 1, false, 1910, 1911, Point{Row: 12, Column: 0}, Point{Row: 12, Column: 1})
	alias := newParentNodeInArena(arena, 3, false, []*Node{child}, nil, 0)
	alias.startByte = 1909
	alias.endByte = 1911
	alias.startPoint = Point{Row: 11, Column: 42}
	alias.endPoint = Point{Row: 12, Column: 1}
	root := newParentNodeInArena(arena, 2, true, []*Node{alias}, nil, 0)

	counters, _, _ := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if got, want := counters.nodesRewritten, uint64(1); got != want {
		t.Fatalf("nodesRewritten = %d, want %d", got, want)
	}
	if got, want := alias.ChildCount(), 0; got != want {
		t.Fatalf("alias.ChildCount() = %d, want %d", got, want)
	}
	if alias.IsNamed() {
		t.Fatal("alias named flag changed")
	}
	if got, want := alias.StartByte(), uint32(1910); got != want {
		t.Fatalf("alias.StartByte() = %d, want %d", got, want)
	}
	if got, want := alias.EndByte(), uint32(1911); got != want {
		t.Fatalf("alias.EndByte() = %d, want %d", got, want)
	}
}

// TestNormalizeResultTerminalLeafNodesPreservesDistinctChildUnderReusedAliasTargetID
// mirrors norg's "_word" alias: a symbol ID can simultaneously be a genuine
// visible terminal (ID within TokenCount) AND a visible alias target
// registered by some other production's AliasSequences entry. When that
// reused-ID node wraps a DIFFERENT-named visible child (e.g. "_uppercase"),
// the child is a real, distinct AST node C tree-sitter keeps -- it must not
// be collapsed away just because the parent's ID happens to also denote a
// plain terminal. Pre-fix, resultSymbolIsVisibleTerminal(n.symbol) alone
// short-circuited the name-equality guard and dropped the child.
func TestNormalizeResultTerminalLeafNodesPreservesDistinctChildUnderReusedAliasTargetID(t *testing.T) {
	lang := &Language{
		TokenCount: 3,
		SymbolNames: []string{
			"EOF",
			"_lowercase_word",
			"_word",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "_lowercase_word", Visible: true, Named: false},
			{Name: "_word", Visible: true, Named: false},
			{Name: "root", Visible: true, Named: true},
		},
		AliasSequences: [][]Symbol{
			// Some other production aliases target symbol 2 ("_word"), which
			// is ALSO a genuine visible terminal ID (< TokenCount) -- the
			// reused-ID scenario that trips the pre-fix guard.
			{2},
		},
	}
	arena := newNodeArena(arenaClassFull)
	child := newLeafNodeInArena(arena, 1, false, 20, 25, Point{Row: 0, Column: 20}, Point{Row: 0, Column: 25})
	parent := newParentNodeInArena(arena, 2, false, []*Node{child}, nil, 0)
	parent.startByte = 20
	parent.endByte = 25
	parent.startPoint = Point{Row: 0, Column: 20}
	parent.endPoint = Point{Row: 0, Column: 25}
	root := newParentNodeInArena(arena, 3, true, []*Node{parent}, nil, 0)

	counters, _, _ := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if got, want := counters.nodesRewritten, uint64(0); got != want {
		t.Fatalf("nodesRewritten = %d, want %d", got, want)
	}
	if got, want := parent.ChildCount(), 1; got != want {
		t.Fatalf("parent.ChildCount() = %d, want %d", got, want)
	}
	if got := parent.Child(0); got != child {
		t.Fatalf("parent.Child(0) = %p, want original child %p", got, child)
	}
}

func TestNormalizeResultTerminalLeafNodesPreservesFieldedTerminal(t *testing.T) {
	lang := &Language{
		TokenCount: 3,
		SymbolNames: []string{
			"EOF",
			"]",
			"decorated_token",
			"root",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "]", Visible: true, Named: false},
			{Name: "decorated_token", Visible: true, Named: true},
			{Name: "root", Visible: true, Named: true},
		},
	}
	arena := newNodeArena(arenaClassFull)
	child := newLeafNodeInArena(arena, 1, false, 10, 11, Point{Row: 0, Column: 10}, Point{Row: 0, Column: 11})
	token := newParentNodeInArena(arena, 2, true, []*Node{child}, []FieldID{1}, 0)
	root := newParentNodeInArena(arena, 3, true, []*Node{token}, nil, 0)

	counters, _, _ := normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root, lang, nil)

	if got, want := counters.nodesRewritten, uint64(0); got != want {
		t.Fatalf("nodesRewritten = %d, want %d", got, want)
	}
	if got, want := token.ChildCount(), 1; got != want {
		t.Fatalf("token.ChildCount() = %d, want %d", got, want)
	}
	if got, want := token.fieldIDs()[0], FieldID(1); got != want {
		t.Fatalf("token.fieldIDs()[0] = %d, want %d", got, want)
	}
}
