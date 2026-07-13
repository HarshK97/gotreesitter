package gotreesitter

import (
	"runtime"
	"testing"
)

// This file regression-tests the 2026-07-13 jstswalk-containment-gap fix,
// the JS/TS analogue of the 2026-07-12 gocompat-walk-containment-gap fix
// (see parser_result_go_compat_memory_budget_test.go): the JS/TS fused
// compat walk (rewriteJavaScriptTypeScriptStatementKeywordsCallPrecedenceAndBuildUnaryBinaryIndex,
// driven by normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats)
// had ZERO stop polling of any kind before this fix — no timeout,
// cancellation, or memory-budget check — so it ran to completion (ballooning
// heap growth via its own per-node bookkeeping) regardless of the parse's
// configured runtime memory budget. The fix wires a *parseStopPoller with
// memoryBudgetParser set into the walk (mirroring the Go compat walk's
// poller.memoryBudgetParser hook, parser_result_go_compat.go) and checks the
// poller once per node visited, throttled to the same ~1024-node stride
// (parseStopPollMask) walkGoCompatSubtree uses.
//
// wave11 (juniper) swapped this file's fixture shape from a wide
// unary_expression tree to a wide dynamic-import-leaf tree: the fused walk's
// unary/binary candidate-index bookkeeping this file used to exercise for
// its heap-growth trigger was one of six rewrite classes the wave11 census
// confirmed dead (zero rewrites across ~23MB of real JS/TS/TSX, the 5003ac64
// regression snippets, and independent adversarial precedence chains) and
// was removed along with empty_statement, existential_type, statement-
// keyword, and call-precedence handling. Dynamic-import leaf retyping
// (normalizeJavaScriptTypeScriptDynamicImportLeafWithSymbolChanged) is still
// live, unconditional per matching leaf, and — like the old unary_expression
// shape — does real, non-idempotent per-leaf allocation (a new leaf node
// plus a new one-element children slice per import() call) genuinely
// proportional to tree size, so it reproduces the same containment-gap
// trigger shape.
//
// buildJSTSCompatWideDynamicImportTree below models the trigger shape
// without an end-to-end multi-hundred-MB reproduction: a wide, flat tree of
// many real, distinct dynamic-import leaf children, calibrated so unbounded
// work is clearly larger than a small configured budget while staying well
// under ~1GiB for test safety.

const (
	testJSTSCompatProgramSym       Symbol = 1
	testJSTSCompatDynamicImportSym Symbol = 2 // named "import" (dynamic import() call)
	// Symbol 3 is the unnamed "import" (the retyped leaf child's symbol),
	// resolved internally via lang.symbolByNameAndNamed("import", false).
)

// buildJSTSCompatWideDynamicImportTree builds a "program" root with
// numImport distinct, childless dynamic-import leaf children (no node
// sharing — an ordinary tree, unlike the historical Go DAG/cycle defect) and
// a minimal *Language whose symbol table matches, so
// rewriteJavaScriptTypeScriptStatementKeywordsCallPrecedenceAndBuildUnaryBinaryIndex
// (via normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats)
// resolves the named/unnamed "import" pair the same way it would for a real
// grammar. The returned source is a small fixed buffer containing "import"
// (enough to pass the walk's enableDynamicImport source-level gate); nothing
// in this path reads source at the individual leaf offsets.
func buildJSTSCompatWideDynamicImportTree(numImport int) (*Node, *Language, []byte) {
	arena := newNodeArena(arenaClassFull)
	children := make([]*Node, numImport)
	for i := 0; i < numImport; i++ {
		off := uint32(i)
		children[i] = newLeafNodeInArena(arena, testJSTSCompatDynamicImportSym, true, off, off+1, Point{}, Point{})
	}
	root := newParentNodeInArena(arena, testJSTSCompatProgramSym, true, cloneNodeSliceInArena(arena, children), nil, 0)

	lang := &Language{
		Name:        "javascript",
		SymbolNames: []string{"EOF", "program", "import", "import"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "program", Visible: true, Named: true},
			{Name: "import", Visible: true, Named: true},
			{Name: "import", Visible: true, Named: false},
		},
	}
	return root, lang, []byte("import")
}

// TestJSTSFusedWalkUnboundedBaselineCompletes establishes the baseline: with
// no memory-budget parser wired into the poller (poller == nil — the pre-fix
// shape, since the fused walk previously had no poller parameter at all), the
// walk runs to completion and visits every child regardless of tree size.
// This is the control for TestJSTSFusedWalkStopsOnMemoryBudget below.
func TestJSTSFusedWalkUnboundedBaselineCompletes(t *testing.T) {
	const numImport = 2000000
	root, lang, source := buildJSTSCompatWideDynamicImportTree(numImport)

	stats, reason := normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats(root, source, lang, nil)
	if reason != ParseStopNone {
		t.Fatalf("reason = %q, want %q (unbounded walk must not stop)", reason, ParseStopNone)
	}
	// indexNodesVisited counts every node the fused walk visited: root, plus
	// every dynamic-import leaf, plus the single replacement keyword-leaf
	// child normalizeJavaScriptTypeScriptDynamicImportLeafWithSymbolChanged
	// gives each import leaf in place (the walk recurses into it too, since
	// the retype mutates the leaf's own children rather than replacing it at
	// the parent) — so 1 + 2*numImport, not 1 + numImport. An unbounded walk
	// must visit all of them.
	if got, want := stats.indexNodesVisited, uint64(2*numImport+1); got != want {
		t.Fatalf("indexNodesVisited = %d, want %d (unbounded walk must visit every node)", got, want)
	}
	// Every dynamic-import leaf must actually have been retyped: proof the
	// walk did real, non-idempotent per-node work the whole way through, not
	// just cheap traversal.
	for i := 0; i < numImport; i++ {
		child := resultChildAt(root, i)
		if got, want := resultChildCount(child), 1; got != want {
			t.Fatalf("import leaf %d child count = %d, want %d (unbounded walk must retype every leaf)", i, got, want)
		}
	}
}

// TestJSTSFusedWalkStopsOnMemoryBudget is the core regression: wiring a
// tiny-budget *Parser into the poller (parseStopPoller.memoryBudgetParser,
// the fused-walk-side fix) must make the walk (a) return
// ParseStopMemoryBudget, (b) stop well before the last child is reached
// (proving it bailed out mid-walk rather than finishing then reporting the
// budget as an afterthought), and (c) keep total heap growth within a
// generous multiple of the configured budget — even though an unbounded walk
// over the identical tree (see the baseline test above) would keep going
// regardless.
func TestJSTSFusedWalkStopsOnMemoryBudget(t *testing.T) {
	const numImport = 2000000
	root, lang, source := buildJSTSCompatWideDynamicImportTree(numImport)

	const budgetBytes = 1 << 20 // 1MiB: comfortably smaller than the growth
	// this walk's own per-leaf retyping allocates (a new leaf node plus a new
	// one-element children slice per dynamic-import leaf, across all
	// numImport children) if it ran to completion, so the trip must land
	// partway through, not at the end.
	parser := newGoCompatBudgetTestParser(budgetBytes)
	parser.language = lang

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	poller := parseStopPoller{memoryBudgetParser: parser}
	stats, reason := normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats(root, source, lang, &poller)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if reason != ParseStopMemoryBudget {
		t.Fatalf("reason = %q, want %q", reason, ParseStopMemoryBudget)
	}
	if !parser.compatMemoryBudgetTripped {
		t.Fatal("compatMemoryBudgetTripped = false, want true (sticky latch for resultMaterializationStopReason's later recheck)")
	}
	if stats.indexNodesVisited == 0 {
		t.Fatal("indexNodesVisited = 0, want some real work done before the trip")
	}
	// 2*numImport+1 is the full unbounded node-visit count (see the baseline
	// test's doc comment: root + every import leaf + every replacement
	// keyword-leaf child).
	if stats.indexNodesVisited >= uint64(2*numImport+1) {
		t.Fatalf("indexNodesVisited = %d, want a partial prefix < %d (walk must have stopped before reaching the end)", stats.indexNodesVisited, 2*numImport+1)
	}

	growth := int64(0)
	if after.HeapAlloc > before.HeapAlloc {
		growth = int64(after.HeapAlloc - before.HeapAlloc)
	}
	const maxOvershoot = 20 * budgetBytes
	t.Logf("nodesVisited=%d/%d heap growth=%d bytes (budget=%d, %.2fx)",
		stats.indexNodesVisited, 2*numImport+1, growth, int64(budgetBytes), float64(growth)/float64(budgetBytes))
	if growth > maxOvershoot {
		t.Fatalf("heap growth = %d bytes, want <= %d (20x budget=%d)", growth, maxOvershoot, int64(budgetBytes))
	}
}

// TestNormalizeJavaScriptCompatibilityPropagatesMemoryBudgetStop exercises the
// full production entry point (normalizeJavaScriptCompatibility) instead of
// normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats
// directly, confirming the poller wiring done inside normalizeJavaScriptCompatibility
// reaches all the way from a *Parser's configured budget to the returned
// ParseStopReason, and that resultMaterializationStopReason (what
// finalizeResultRoot consults afterward) reliably reports the trip via the
// shared compatMemoryBudgetTripped sticky latch.
func TestNormalizeJavaScriptCompatibilityPropagatesMemoryBudgetStop(t *testing.T) {
	const numImport = 2000000
	root, lang, source := buildJSTSCompatWideDynamicImportTree(numImport)

	const budgetBytes = 1 << 20
	parser := newGoCompatBudgetTestParser(budgetBytes)
	parser.language = lang

	reason := normalizeJavaScriptCompatibility(root, source, parser, lang)
	if reason != ParseStopMemoryBudget {
		t.Fatalf("normalizeJavaScriptCompatibility reason = %q, want %q", reason, ParseStopMemoryBudget)
	}
	if !parser.compatMemoryBudgetTripped {
		t.Fatal("compatMemoryBudgetTripped = false, want true")
	}
	if got := parser.resultMaterializationStopReason(nil); got != ParseStopMemoryBudget {
		t.Fatalf("resultMaterializationStopReason() = %q, want %q after a JS/TS compat memory-budget trip", got, ParseStopMemoryBudget)
	}
}

// TestNormalizeTypeScriptTreeCompatibilityPropagatesMemoryBudgetStop is the
// TypeScript-side counterpart of the JavaScript test above, exercising
// normalizeTypeScriptTreeCompatibilityWithParser's fast (non-metrics) path —
// the one every real TypeScript/TSX parse takes.
func TestNormalizeTypeScriptTreeCompatibilityPropagatesMemoryBudgetStop(t *testing.T) {
	const numImport = 2000000
	root, lang, source := buildJSTSCompatWideDynamicImportTree(numImport)
	lang.Name = "typescript"

	const budgetBytes = 1 << 20
	parser := newGoCompatBudgetTestParser(budgetBytes)
	parser.language = lang

	reason := normalizeTypeScriptTreeCompatibilityWithParser(root, source, parser, lang)
	if reason != ParseStopMemoryBudget {
		t.Fatalf("normalizeTypeScriptTreeCompatibilityWithParser reason = %q, want %q", reason, ParseStopMemoryBudget)
	}
	if !parser.compatMemoryBudgetTripped {
		t.Fatal("compatMemoryBudgetTripped = false, want true")
	}
	if got := parser.resultMaterializationStopReason(nil); got != ParseStopMemoryBudget {
		t.Fatalf("resultMaterializationStopReason() = %q, want %q after a JS/TS compat memory-budget trip", got, ParseStopMemoryBudget)
	}
}
