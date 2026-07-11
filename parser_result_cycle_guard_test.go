package gotreesitter

import (
	"sync/atomic"
	"testing"
)

func TestStripResultTreeSelfCycles(t *testing.T) {
	x := &Node{}
	x.children = []*Node{x}
	stripResultTreeSelfCycles(x)
	for _, c := range x.children {
		if c == x {
			t.Fatal("self-reference not removed")
		}
	}

	a := &Node{}
	b := &Node{}
	a.children = []*Node{b}
	b.children = []*Node{a}
	stripResultTreeSelfCycles(a)
	for _, c := range b.children {
		if c == a {
			t.Fatal("ancestor back-edge not removed")
		}
	}

	leaf := &Node{}
	mid := &Node{children: []*Node{leaf}}
	root := &Node{children: []*Node{mid}}
	stripResultTreeSelfCycles(root)
	if len(root.children) != 1 || root.children[0] != mid || len(mid.children) != 1 || mid.children[0] != leaf {
		t.Fatal("clean tree was mutated")
	}

	y := &Node{}
	kept := &Node{}
	y.children = []*Node{y, kept}
	y.setFieldMetadata(
		[]FieldID{1, 2},
		[]uint8{fieldSourceDirect, fieldSourceInherited},
	)
	stripResultTreeSelfCycles(y)
	if len(y.children) != 1 || y.children[0] != kept {
		t.Fatalf("children after cycle strip = %v, want only kept child", y.children)
	}
	if len(y.fieldIDs()) != 1 || y.fieldIDs()[0] != 2 {
		t.Fatalf("fieldIDs after cycle strip = %v, want [2]", y.fieldIDs())
	}
	if len(y.fieldSources()) != 1 || y.fieldSources()[0] != fieldSourceInherited {
		t.Fatalf("fieldSources after cycle strip = %v, want inherited source", y.fieldSources())
	}
}

// TestStripResultTreeSelfCyclesIsObservable pins the #110 band-aid conversion:
// the strip is no longer silent. A clean tree leaves the counter untouched;
// every dropped self/ancestor back-edge bumps debugResultTreeSelfCyclesStripped
// so a future sentinel-corruption regression surfaces instead of being masked.
func TestStripResultTreeSelfCyclesIsObservable(t *testing.T) {
	leaf := &Node{}
	mid := &Node{children: []*Node{leaf}}
	root := &Node{children: []*Node{mid}}
	before := atomic.LoadUint64(&debugResultTreeSelfCyclesStripped)
	stripResultTreeSelfCycles(root)
	if atomic.LoadUint64(&debugResultTreeSelfCyclesStripped) != before {
		t.Fatalf("clean tree bumped the strip counter: %d -> %d", before, atomic.LoadUint64(&debugResultTreeSelfCyclesStripped))
	}

	self := &Node{}
	self.children = []*Node{self}
	before = atomic.LoadUint64(&debugResultTreeSelfCyclesStripped)
	stripResultTreeSelfCycles(self)
	if len(self.children) != 0 {
		t.Fatalf("self-edge not stripped; children=%v", self.children)
	}
	if atomic.LoadUint64(&debugResultTreeSelfCyclesStripped) != before+1 {
		t.Fatalf("self-cycle strip counter = %d, want %d", atomic.LoadUint64(&debugResultTreeSelfCyclesStripped), before+1)
	}

	a := &Node{}
	b := &Node{}
	a.children = []*Node{b}
	b.children = []*Node{a}
	before = atomic.LoadUint64(&debugResultTreeSelfCyclesStripped)
	stripResultTreeSelfCycles(a)
	if atomic.LoadUint64(&debugResultTreeSelfCyclesStripped) != before+1 {
		t.Fatalf("ancestor back-edge strip counter = %d, want %d", atomic.LoadUint64(&debugResultTreeSelfCyclesStripped), before+1)
	}
}

func TestFlattenInvisibleRootChildrenHandlesCycles(t *testing.T) {
	meta := []SymbolMetadata{
		{Name: "root", Visible: true},
		{Name: "_hidden", Visible: false},
		{Name: "visible", Visible: true},
	}

	self := &Node{symbol: 1}
	self.children = []*Node{self}
	root := &Node{symbol: 0, children: []*Node{self}}
	flattenInvisibleRootChildren(root, nil, &Language{SymbolMetadata: meta})
	if len(root.children) != 0 {
		t.Fatalf("self-cycle flatten children = %d, want 0", len(root.children))
	}

	visible := &Node{symbol: 2}
	outer := &Node{symbol: 1}
	inner := &Node{symbol: 1}
	outer.children = []*Node{inner}
	inner.children = []*Node{outer, visible}
	root = &Node{symbol: 0, children: []*Node{outer}}
	flattenInvisibleRootChildren(root, nil, &Language{SymbolMetadata: meta})
	if len(root.children) != 1 || root.children[0] != visible {
		t.Fatalf("ancestor-cycle flatten children = %v, want visible leaf", root.children)
	}
}

func TestFlattenInvisibleRootChildrenStillFlattensCleanHiddenWrappers(t *testing.T) {
	meta := []SymbolMetadata{
		{Name: "root", Visible: true},
		{Name: "_hidden", Visible: false},
		{Name: "visible", Visible: true},
	}
	visible := &Node{symbol: 2}
	hidden := &Node{symbol: 1, children: []*Node{visible}}
	root := &Node{symbol: 0, children: []*Node{hidden}}
	flattenInvisibleRootChildren(root, nil, &Language{SymbolMetadata: meta})
	if len(root.children) != 1 || root.children[0] != visible {
		t.Fatalf("clean flatten children = %v, want visible leaf", root.children)
	}
}

// TestFlattenInvisibleRootChildrenPreservesSpanWhenHiddenLeafVanishes pins the
// wave-2 doxygen fix: when a trailing (or any) root child is an invisible,
// non-extra, childless (leaf) hidden symbol, appendFlattenedInvisibleRootChild
// has nothing to substitute for it — it is dropped from the children array
// entirely, with no sibling replacing its byte span. populateParentNode (via
// replaceNodeChildrenUnfielded) then recomputes root's span strictly from the
// SURVIVING children, which would otherwise silently shrink the root below
// the real content it structurally absorbed (doxygen's whole-block-comment
// ERROR fallback: a hidden `_text_line` leaf trailing an ERROR-wrapped hidden
// delimiter shrank the root from the full 67-byte comment down to just the
// first 3 bytes). flattenInvisibleRootChildren must widen back to the
// pre-flatten span afterward — a hidden node's bytes are still part of its
// parent's span in tree-sitter C even though the node itself never appears in
// the concrete tree.
func TestFlattenInvisibleRootChildrenPreservesSpanWhenHiddenLeafVanishes(t *testing.T) {
	meta := []SymbolMetadata{
		{Name: "root", Visible: true},
		{Name: "_hidden_delim", Visible: false},
		{Name: "wrapped", Visible: true},
		{Name: "_hidden_leaf", Visible: false},
	}
	wrapped := &Node{symbol: 2, startByte: 0, endByte: 3, endPoint: Point{Column: 3}}
	// A hidden LEAF trailing child: invisible, non-extra, zero children — the
	// exact shape appendFlattenedInvisibleRootChildWalk cannot substitute
	// anything for.
	hiddenLeaf := &Node{symbol: 3, startByte: 3, endByte: 10, startPoint: Point{Column: 3}, endPoint: Point{Column: 10}}
	root := &Node{symbol: 0, startByte: 0, endByte: 10, endPoint: Point{Column: 10}, children: []*Node{wrapped, hiddenLeaf}}

	flattenInvisibleRootChildren(root, nil, &Language{SymbolMetadata: meta})

	if len(root.children) != 1 || root.children[0] != wrapped {
		t.Fatalf("children = %v, want only the wrapped (visible) child", root.children)
	}
	if root.startByte != 0 || root.endByte != 10 {
		t.Fatalf("root span = [%d,%d), want [0,10) — span must not shrink when a hidden leaf child is dropped", root.startByte, root.endByte)
	}
	if root.endPoint.Column != 10 {
		t.Fatalf("root endPoint = %+v, want Column=10", root.endPoint)
	}
}
