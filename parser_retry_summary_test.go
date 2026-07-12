package gotreesitter

import "testing"

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
