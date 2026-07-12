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
// heap growth via its own candidate-index bookkeeping) regardless of the
// parse's configured runtime memory budget. The fix wires a *parseStopPoller
// with memoryBudgetParser set into the walk (mirroring the Go compat walk's
// poller.memoryBudgetParser hook, parser_result_go_compat.go) and checks the
// poller once per node visited, throttled to the same ~1024-node stride
// (parseStopPollMask) walkGoCompatSubtree uses.
//
// buildJSTSCompatWideUnaryTree below models the trigger shape without an
// end-to-end multi-hundred-MB reproduction: a wide, flat tree of many real,
// distinct unary_expression leaf children whose fused-walk cost (the
// index.unaryCandidates slice growing by one entry per child, genuinely
// proportional to tree size) is real, unavoidable, non-idempotent work —
// calibrated so unbounded work is clearly larger than a small configured
// budget while staying well under ~1GiB for test safety.

const (
	testJSTSCompatProgramSym         Symbol = 1
	testJSTSCompatUnaryExpressionSym Symbol = 2
)

// buildJSTSCompatWideUnaryTree builds a "program" root with numUnary distinct
// unary_expression leaf children (no node sharing — an ordinary tree, unlike
// the historical Go DAG/cycle defect) and a minimal *Language whose symbol
// table matches, so rewriteJavaScriptTypeScriptStatementKeywordsCallPrecedenceAndBuildUnaryBinaryIndex
// (via normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats)
// resolves "unary_expression" the same way it would for a real grammar.
func buildJSTSCompatWideUnaryTree(numUnary int) (*Node, *Language) {
	arena := newNodeArena(arenaClassFull)
	children := make([]*Node, numUnary)
	for i := 0; i < numUnary; i++ {
		off := uint32(i)
		children[i] = newLeafNodeInArena(arena, testJSTSCompatUnaryExpressionSym, true, off, off+1, Point{}, Point{})
	}
	root := newParentNodeInArena(arena, testJSTSCompatProgramSym, true, cloneNodeSliceInArena(arena, children), nil, 0)

	lang := &Language{
		Name:        "javascript",
		SymbolNames: []string{"EOF", "program", "unary_expression"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "program", Visible: true, Named: true},
			{Name: "unary_expression", Visible: true, Named: true},
		},
	}
	return root, lang
}

// TestJSTSFusedWalkUnboundedBaselineCompletes establishes the baseline: with
// no memory-budget parser wired into the poller (poller == nil — the pre-fix
// shape, since the fused walk previously had no poller parameter at all), the
// walk runs to completion and visits every child regardless of tree size.
// This is the control for TestJSTSFusedWalkStopsOnMemoryBudget below.
func TestJSTSFusedWalkUnboundedBaselineCompletes(t *testing.T) {
	const numUnary = 2000000
	root, lang := buildJSTSCompatWideUnaryTree(numUnary)
	source := make([]byte, numUnary)

	stats, reason := normalizeJavaScriptTypeScriptStatementKeywordsAndPrecedenceWithDetailedStats(root, source, lang, nil)
	if reason != ParseStopNone {
		t.Fatalf("reason = %q, want %q (unbounded walk must not stop)", reason, ParseStopNone)
	}
	// indexNodesVisited counts every node the fused walk visited (root plus
	// every child); an unbounded walk must visit all of them.
	if got, want := stats.indexNodesVisited, uint64(numUnary+1); got != want {
		t.Fatalf("indexNodesVisited = %d, want %d (unbounded walk must visit every node)", got, want)
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
	const numUnary = 2000000
	root, lang := buildJSTSCompatWideUnaryTree(numUnary)
	source := make([]byte, numUnary)

	const budgetBytes = 1 << 20 // 1MiB: comfortably smaller than the growth
	// this walk's own index.unaryCandidates bookkeeping allocates (~16 bytes
	// per candidate = ~32MB across all numUnary children) if it ran to
	// completion, so the trip must land partway through, not at the end.
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
	if stats.indexNodesVisited >= uint64(numUnary+1) {
		t.Fatalf("indexNodesVisited = %d, want a partial prefix < %d (walk must have stopped before reaching the end)", stats.indexNodesVisited, numUnary+1)
	}

	growth := int64(0)
	if after.HeapAlloc > before.HeapAlloc {
		growth = int64(after.HeapAlloc - before.HeapAlloc)
	}
	const maxOvershoot = 20 * budgetBytes
	t.Logf("nodesVisited=%d/%d heap growth=%d bytes (budget=%d, %.2fx)",
		stats.indexNodesVisited, numUnary+1, growth, int64(budgetBytes), float64(growth)/float64(budgetBytes))
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
	const numUnary = 2000000
	root, lang := buildJSTSCompatWideUnaryTree(numUnary)
	source := make([]byte, numUnary)

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
	const numUnary = 2000000
	root, lang := buildJSTSCompatWideUnaryTree(numUnary)
	lang.Name = "typescript"
	source := make([]byte, numUnary)

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
