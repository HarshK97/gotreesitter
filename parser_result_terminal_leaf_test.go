package gotreesitter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

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

	counters, _, _ := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

	counters, reason, errorSummary := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), stopCheck)

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

	_, reason, errorSummary := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

	_, reason, errorSummary := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

	counters, reason, errorSummary := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

	counters, _, _ := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

	counters, _, _ := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

func TestVisibleAliasTargetsExcludeHiddenAliases(t *testing.T) {
	lang := &Language{
		TokenCount:  2,
		SymbolNames: []string{"EOF", "]", "root", "]"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "]", Visible: true},
			{Name: "root", Visible: true, Named: true},
			{Name: "]", Visible: false},
		},
		AliasSequences: [][]Symbol{{3}},
	}
	parser := NewParser(lang)
	if !parser.aliasTargetSymbol[3] {
		t.Fatal("general alias-target cache omitted hidden alias")
	}
	if parser.visibleAliasTargetSymbol[3] {
		t.Fatal("visible alias-target cache included hidden alias")
	}

	arena := newNodeArena(arenaClassFull)
	child := newLeafNodeInArena(arena, 1, false, 0, 1, Point{}, Point{Column: 1})
	hiddenAlias := newParentNodeInArena(arena, 3, false, []*Node{child}, nil, 0)
	root := newParentNodeInArena(arena, 2, true, []*Node{hiddenAlias}, nil, 0)
	counters, _, _ := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, parser.visibleAliasTargetSymbol, nil)
	if counters.nodesRewritten != 0 || hiddenAlias.ChildCount() != 1 {
		t.Fatalf("hidden alias was normalized as visible: rewritten=%d children=%d", counters.nodesRewritten, hiddenAlias.ChildCount())
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

	counters, _, _ := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

	counters, _, _ := normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, buildVisibleAliasTargetSymbols(lang), nil)

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

// terminalLeafCensusRewrites returns how many nodes the generic.terminal-leaf
// pass rewrote during the parse that produced this parser's current stats.
func terminalLeafCensusRewrites(p *Parser) (visited, rewritten uint64, sawPass bool) {
	if p == nil {
		return 0, 0, false
	}
	for i := range p.normalizationStats.namedPasses {
		pass := &p.normalizationStats.namedPasses[i]
		if pass.name != "generic.terminal-leaf" {
			continue
		}
		return pass.nodesVisited, pass.nodesRewritten, true
	}
	return 0, 0, false
}

// TestGenericTerminalLeafCensusProbeIsWired is the positive control required by
// docs/root-normalization-retirement.md before a zero-rewrite census can be
// acted on. It proves the census counter is wired to the PRODUCTION call site
// and observes real traffic, so a zero rewrite count means "the pass found
// nothing to do", not "the probe was never called".
//
// That the pass is capable of rewriting at all is proven separately, and
// already, by the registered witnesses in parser_result_terminal_leaf_test.go:
// TestNormalizeResultTerminalLeafNodesCollapsesRedundantAnonymousTokenChild and
// TestNormalizeResultTerminalLeafNodesCollapsesTerminalAliasTarget both assert
// nodesRewritten == 1 on constructed input. Those are the "probe can fire"
// control; this is the "probe is connected" control.
func TestGenericTerminalLeafCensusProbeIsWired(t *testing.T) {
	lang := loadGoTestLanguageForCensus(t)
	parser := NewParser(lang)
	if _, err := parser.Parse([]byte("package p\n\nfunc F() int { return 1 }\n")); err != nil {
		t.Fatalf("parse: %v", err)
	}
	visited, _, sawPass := terminalLeafCensusRewrites(parser)
	if !sawPass {
		t.Fatal("generic.terminal-leaf census counter never recorded: the probe is not wired to the production call site, so any zero-rewrite census result would be vacuous")
	}
	if visited == 0 {
		t.Fatal("generic.terminal-leaf visited 0 nodes on a tree with nodes: the probe records but does not observe")
	}
	t.Logf("probe is wired and observing: visited=%d", visited)
}

// TestGenericTerminalLeafCensusOverRepoCorpus is the census itself. It parses
// this repository's own Go sources, which is a large authenticated corpus that
// is always present, and reports whether the last live generic compatibility
// pass ever rewrites a node.
//
// This test does not assert zero. It records the receipt. Turning a zero result
// into a deletion is a separate step that also needs the production, compact,
// forest, incremental and per-grammar C-oracle route receipts the retirement
// plan requires. Set GTS_TERMINAL_LEAF_CENSUS_STRICT=1 to fail on any rewrite,
// which is how a later PR pins the pass as inert once it has been retired.
func TestGenericTerminalLeafCensusOverRepoCorpus(t *testing.T) {
	lang := loadGoTestLanguageForCensus(t)

	var files []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if info.Size() > 512*1024 {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(files) == 0 {
		t.Skip("no Go sources found for census")
	}

	var totalVisited, totalRewritten uint64
	rewritingFiles := map[string]uint64{}
	parsed := 0
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		parser := NewParser(lang)
		if _, err := parser.Parse(src); err != nil {
			continue
		}
		parsed++
		visited, rewritten, _ := terminalLeafCensusRewrites(parser)
		totalVisited += visited
		totalRewritten += rewritten
		if rewritten > 0 {
			rewritingFiles[path] = rewritten
		}
	}

	t.Logf("generic.terminal-leaf census: files=%d visited=%d rewritten=%d rewriting_files=%d",
		parsed, totalVisited, totalRewritten, len(rewritingFiles))
	shown := 0
	for path, n := range rewritingFiles {
		if shown >= 10 {
			break
		}
		t.Logf("  rewrote %d node(s): %s", n, path)
		shown++
	}

	if totalVisited == 0 {
		t.Fatal("census visited no nodes across the corpus: the probe did not observe, so the result is vacuous")
	}
	if strings.TrimSpace(os.Getenv("GTS_TERMINAL_LEAF_CENSUS_STRICT")) != "" && totalRewritten != 0 {
		t.Fatalf("strict census: generic.terminal-leaf rewrote %d node(s) across %d file(s)", totalRewritten, len(rewritingFiles))
	}
}

func loadGoTestLanguageForCensus(t *testing.T) *Language {
	t.Helper()
	blob, err := os.ReadFile("grammars/grammar_blobs/go.bin")
	if err != nil {
		t.Skipf("go grammar blob unavailable: %v", err)
	}
	lang, err := LoadLanguage(blob)
	if err != nil {
		t.Fatalf("load go grammar: %v", err)
	}
	return lang
}

// TestGenericTerminalLeafCensusMultiLanguage widens the census beyond Go across
// the real-source corpus in cgo_harness/corpus_real, which carries files for
// roughly 48 grammars.
//
// The registry lists generic.terminal-leaf as languages:["*"], so a Go-only
// zero is not sufficient evidence to retire it. This reports per-language
// coverage explicitly: a language counts as censused only when the probe
// actually observed nodes for it. Languages whose probe never fires are
// reported as UNCOVERED rather than being silently folded into the zero, which
// is the same vacuous-zero failure mode the wiring control guards against.
//
// This is a receipt, not a gate. It does not assert zero rewrites, because
// discovering which grammars still need the collapse is the point.
//
// KNOWN COVERAGE LIMIT. This test loads grammars with the root package's raw
// LoadLanguage, which attaches no external scanner and no native result
// compatibility, because the root package cannot import gotreesitter/grammars
// without an import cycle. Grammars that need a scanner therefore parse to an
// error root here, and the pass returns before visiting anything when the root
// carries an error. Those languages are reported as UNCOVERED with their error
// root counts so the zero above is never mistaken for evidence about them.
// Closing that gap needs the census to run against grammars loaded the way
// production loads them, which in turn needs a small exported accessor for the
// normalization counters. Until then the covered set is the receipt.
func TestGenericTerminalLeafCensusMultiLanguage(t *testing.T) {
	const corpusRoot = "cgo_harness/corpus_real"
	entries, err := os.ReadDir(corpusRoot)
	if err != nil {
		t.Skipf("real corpus unavailable: %v", err)
	}

	type langResult struct {
		files      int
		visited    uint64
		rewritten  uint64
		errorRoots int
	}
	results := map[string]*langResult{}
	var uncovered, noBlob, parseFailed []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		blob, err := os.ReadFile(filepath.Join("grammars", "grammar_blobs", name+".bin"))
		if err != nil {
			noBlob = append(noBlob, name)
			continue
		}
		lang, err := LoadLanguage(blob)
		if err != nil {
			noBlob = append(noBlob, name)
			continue
		}
		files, err := os.ReadDir(filepath.Join(corpusRoot, name))
		if err != nil {
			continue
		}
		res := &langResult{}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			src, err := os.ReadFile(filepath.Join(corpusRoot, name, f.Name()))
			if err != nil || len(src) == 0 || len(src) > 512*1024 {
				continue
			}
			parser := NewParser(lang)
			tree, err := parser.Parse(src)
			if err != nil {
				parseFailed = append(parseFailed, name+"/"+f.Name())
				continue
			}
			res.files++
			if tree != nil && tree.RootNode() != nil && tree.RootNode().HasError() {
				res.errorRoots++
			}
			visited, rewritten, _ := terminalLeafCensusRewrites(parser)
			res.visited += visited
			res.rewritten += rewritten
		}
		if res.files == 0 {
			continue
		}
		if res.visited == 0 {
			// Record why. The pass returns immediately when the root carries an
			// error, so an all-error-root language is uncovered because this
			// harness could not parse it cleanly, not because the pass skips it.
			uncovered = append(uncovered, fmt.Sprintf("%s(files=%d,errorRoots=%d)", name, res.files, res.errorRoots))
			continue
		}
		results[name] = res
	}

	var totalVisited, totalRewritten uint64
	var rewriting []string
	for name, res := range results {
		totalVisited += res.visited
		totalRewritten += res.rewritten
		if res.rewritten > 0 {
			rewriting = append(rewriting, name)
		}
	}
	sort.Strings(rewriting)
	sort.Strings(uncovered)

	t.Logf("generic.terminal-leaf multi-language census: censused_languages=%d visited=%d rewritten=%d",
		len(results), totalVisited, totalRewritten)
	if len(rewriting) > 0 {
		t.Logf("  languages that STILL REWRITE (%d): %s", len(rewriting), strings.Join(rewriting, " "))
		for _, name := range rewriting {
			t.Logf("    %-12s files=%d visited=%d rewritten=%d", name, results[name].files, results[name].visited, results[name].rewritten)
		}
	} else {
		t.Logf("  no censused language rewrote any node")
	}
	if len(uncovered) > 0 {
		t.Logf("  UNCOVERED, probe never observed (%d): %s", len(uncovered), strings.Join(uncovered, " "))
	}
	if len(noBlob) > 0 {
		t.Logf("  no usable grammar blob (%d)", len(noBlob))
	}
	if len(parseFailed) > 0 {
		t.Logf("  parse errors (%d)", len(parseFailed))
	}

	if len(results) == 0 {
		t.Fatal("multi-language census covered no language: the result is vacuous")
	}
}
