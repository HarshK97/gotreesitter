//go:build gts_parsercorephase0

package gotreesitter

import (
	"bytes"
	"testing"
)

// TestCompactMaterializedTreeBarsIncrementalReuse seals Phase-3 Lane 3 review
// amendment 2: a tree built by the compact route carries table-REPLAYED (not
// live-recorded) parser states and no scanner checkpoints, so ParseIncremental
// over it must fall all the way back to a fresh full parse -- it must NOT reuse
// any node, including via the token-invariant leaf fast path that a
// forestFastPath disabled tree would still take.
func TestCompactMaterializedTreeBarsIncrementalReuse(t *testing.T) {
	runner, err := newParserCoreFreshFullRunner(parserCoreWarmGoScanner, parserCoreFreshFullCanonicalOptions())
	if err != nil {
		t.Fatal(err)
	}
	lang := runner.lang

	src := []byte("package p\n\nvar x = 1\n")
	treeA, err := runner.parse(src)
	if err != nil {
		t.Fatalf("compact parse: %v", err)
	}
	defer treeA.Release()

	if !treeA.compactMaterialized {
		t.Fatal("compact tree is missing the compactMaterialized provenance flag")
	}
	if !oldTreeDisablesIncrementalReuse(treeA) {
		t.Fatal("compact tree must disable incremental reuse")
	}

	// Same-length, token-invariant edit ('1' -> '2'): a forestFastPath disabled
	// Go tree WOULD reuse this via the token-invariant leaf path. The compact
	// tree must refuse it.
	pos := bytes.IndexByte(src, '1')
	if pos < 0 {
		t.Fatal("fixture invariant: expected a '1' digit to edit")
	}
	newSrc := []byte("package p\n\nvar x = 2\n")
	col := uint32(pos - bytes.LastIndexByte(src[:pos], '\n') - 1)
	edit := InputEdit{
		StartByte: uint32(pos), OldEndByte: uint32(pos + 1), NewEndByte: uint32(pos + 1),
		StartPoint:  Point{Row: 2, Column: col},
		OldEndPoint: Point{Row: 2, Column: col + 1},
		NewEndPoint: Point{Row: 2, Column: col + 1},
	}
	treeA.Edit(edit)

	p := NewParser(lang)
	// Direct assertion on the reuse gate: it refuses the compact tree.
	if reused, ok := p.tryTokenInvariantReuseForDisabledOldTree(newSrc, treeA, nil); ok || reused != nil {
		if reused != nil {
			reused.Release()
		}
		t.Fatal("compact tree was reused via the token-invariant leaf path; reuse bar not enforced")
	}

	// End-to-end: ParseIncremental over the compact tree yields a tree identical
	// to a fresh parse of the edited source (the fresh-parse fallback fired).
	got, err := p.ParseIncremental(newSrc, treeA)
	if err != nil {
		t.Fatalf("ParseIncremental over compact tree: %v", err)
	}
	defer got.Release()
	if got == treeA {
		t.Fatal("ParseIncremental returned the compact old tree unchanged for a real edit")
	}
	want, err := NewParser(lang).Parse(newSrc)
	if err != nil {
		t.Fatalf("fresh Parse of edited source: %v", err)
	}
	defer want.Release()
	parserCoreWarmRequireDeepEqual(t, got, want, lang)
}
