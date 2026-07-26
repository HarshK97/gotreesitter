package gotreesitter

import "testing"

func TestSummarizeResultErrorsStopsOnActiveParseBudget(t *testing.T) {
	arena := newNodeArena(arenaClassFull)
	children := make([]*Node, 0, parseStopPollMask*2)
	for index := 0; index < parseStopPollMask*2; index++ {
		start := uint32(index)
		children = append(children, newLeafNodeInArena(arena, 1, false, start, start+1, Point{Column: start}, Point{Column: start + 1}))
	}
	root := newParentNodeInArena(arena, 2, true, children, nil, 0)
	checks := 0
	stopCheck := func() ParseStopReason {
		checks++
		if checks >= 2 {
			return ParseStopTimeout
		}
		return ParseStopNone
	}

	reason, summary := summarizeResultErrorsWithStop(root, stopCheck)

	if reason != ParseStopTimeout {
		t.Fatalf("stop reason = %q, want %q", reason, ParseStopTimeout)
	}
	if summary != resultErrorSummaryUnknown {
		t.Fatalf("error summary = %d, want unknown", summary)
	}
	if checks != 2 {
		t.Fatalf("stop checks = %d, want 2", checks)
	}
}

func TestSummarizeResultErrorsReturnsClean(t *testing.T) {
	arena := newNodeArena(arenaClassFull)
	leaf := newLeafNodeInArena(arena, 1, false, 0, 1, Point{}, Point{Column: 1})
	root := newParentNodeInArena(arena, 2, true, []*Node{leaf}, nil, 0)

	reason, summary := summarizeResultErrorsWithStop(root, nil)

	if reason != ParseStopNone {
		t.Fatalf("stop reason = %q, want none", reason)
	}
	if summary != resultErrorSummaryClean {
		t.Fatalf("error summary = %d, want clean", summary)
	}
}

func TestSummarizeResultErrorsFindsUnderFlaggedDescendant(t *testing.T) {
	arena := newNodeArena(arenaClassFull)
	errNode := newLeafNodeInArena(arena, errorSymbol, true, 0, 1, Point{}, Point{Column: 1})
	errNode.setHasError(true)
	root := newParentNodeInArena(arena, 1, true, []*Node{errNode}, nil, 0)
	root.setHasError(false)

	reason, summary := summarizeResultErrorsWithStop(root, nil)

	if reason != ParseStopNone {
		t.Fatalf("stop reason = %q, want none", reason)
	}
	if root.HasError() {
		t.Fatal("test setup changed root HasError to true")
	}
	if summary != resultErrorSummaryPresent {
		t.Fatalf("error summary = %d, want present", summary)
	}
}

func TestSummarizeResultErrorsFindsUnderFlaggedErrorRoot(t *testing.T) {
	arena := newNodeArena(arenaClassFull)
	leaf := newLeafNodeInArena(arena, 1, false, 0, 1, Point{}, Point{Column: 1})
	root := newParentNodeInArena(arena, errorSymbol, true, []*Node{leaf}, nil, 0)
	root.setHasError(false)

	reason, summary := summarizeResultErrorsWithStop(root, nil)

	if reason != ParseStopNone {
		t.Fatalf("stop reason = %q, want none", reason)
	}
	if root.HasError() {
		t.Fatal("test setup changed root HasError to true")
	}
	if summary != resultErrorSummaryPresent {
		t.Fatalf("error summary = %d, want present", summary)
	}
}

func TestRetryTreeHasErrorUsesKnownSummaryAndUnknownFallback(t *testing.T) {
	root := newLeafNodeInArena(nil, 1, true, 0, 1, Point{}, Point{Column: 1})
	tree := NewTree(root, []byte("x"), nil)

	tree.resultErrorSummary = resultErrorSummaryClean
	if retryTreeHasError(tree) {
		t.Fatal("known-clean tree reported an error")
	}

	tree.resultErrorSummary = resultErrorSummaryPresent
	if !retryTreeHasError(tree) {
		t.Fatal("known-error tree reported clean")
	}

	errNode := newLeafNodeInArena(nil, errorSymbol, true, 0, 1, Point{}, Point{Column: 1})
	errNode.setHasError(true)
	root.children = []*Node{errNode}
	root.setHasError(false)
	tree.resultErrorSummary = resultErrorSummaryUnknown
	if !retryTreeHasError(tree) {
		t.Fatal("unknown tree did not find descendant error through fallback walk")
	}
}

func TestParseRecordsCleanRetryErrorSummary(t *testing.T) {
	parser := NewParser(buildArithmeticLanguage())
	tree, err := parser.Parse([]byte("1+2"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Release()

	if tree.resultErrorSummary != resultErrorSummaryClean {
		t.Fatalf("result error summary = %d, want clean", tree.resultErrorSummary)
	}
	if !tree.resultCompatibilityApplied {
		t.Fatal("result compatibility was not recorded as applied")
	}
	if retryTreeHasError(tree) {
		t.Fatal("clean parsed tree reported an error")
	}
	if parser.goCompatFrames != nil {
		t.Fatal("parser retained active Go compatibility scratch after Parse")
	}
}
