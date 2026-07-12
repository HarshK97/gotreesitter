package gotreesitter

import (
	"testing"
	"time"
)

// runWithin runs fn and fails if it does not return within d. A regression that
// reintroduces the un-deduped descent would loop forever on the cyclic trees
// below, so this converts a hang into a deterministic failure.
func runWithin(t *testing.T, d time.Duration, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not terminate within %v (cyclic-descent guard regressed)", name, d)
	}
}

// buildCyclicGoTree builds source_file -> dot -> (back to source_file). A
// recovery-mode transient go tree can contain exactly this kind of back-edge;
// the normalizer must terminate on it rather than re-descend the cycle forever.
func buildCyclicGoTree() (*Node, *nodeArena) {
	arena := newNodeArena(arenaClassFull)
	dot := newLeafNodeInArena(arena, 2 /* dot */, true, 0, 1, Point{}, Point{Column: 1})
	root := newParentNodeInArena(arena, 3 /* source_file */, true, []*Node{dot}, nil, 0)
	dot.children = cloneNodeSliceInArena(arena, []*Node{root}) // back-edge -> cycle
	return root, arena
}

func TestNormalizeGoDotLeafChildrenTerminatesOnCycle(t *testing.T) {
	lang := goDotCompatibilityTestLanguage()
	root, _ := buildCyclicGoTree()
	source := []byte(".")
	runWithin(t, 15*time.Second, "normalizeGoDotLeafChildrenWithStop", func() {
		poller := parseStopPoller{}
		normalizeGoDotLeafChildrenWithStop(root, source, lang, &poller)
	})
}

func TestNormalizeGoCompatibilitySubtreeTerminatesOnCycle(t *testing.T) {
	lang := goDotCompatibilityTestLanguage()
	syms, _ := goCompatibilitySymbolsForLanguage(lang)
	root, _ := buildCyclicGoTree()
	source := []byte(".")
	runWithin(t, 15*time.Second, "normalizeGoCompatibilitySubtreeWithStopAndScratch", func() {
		poller := parseStopPoller{}
		normalizeGoCompatibilitySubtreeWithStopAndScratch(root, source, syms, goCompatibilitySourceFlags{}, nil, &poller, nil)
	})
}

// buildWideDotFinalRefsTree builds a source_file whose n "dot" leaf children are
// stored as final child refs — the transient form the dot walker traverses via
// resultChildAt / view — so the scaling test exercises the real hot path.
func buildWideDotFinalRefsTree(n int) *Node {
	arena := newNodeArena(arenaClassFull)
	childRange, entries := arena.allocPendingChildEntries(n)
	for i := 0; i < n; i++ {
		leaf := newLeafNodeInArena(arena, 2 /* dot */, true, 0, 1, Point{}, Point{Column: 1})
		entries[i] = newPendingChildEntry(newStackEntryNode(leaf.parseState, leaf))
	}
	return newParentNodeInArenaWithFinalChildRefs(arena, 3 /* source_file */, true, childRange, 0, false)
}

func timeDotNormalizerMin(lang *Language, n, runs int) time.Duration {
	// Size the source so the fast-path pop budget (a small multiple of the
	// source length, mirroring the O(len(source)) node-count bound of a real
	// parse) comfortably exceeds the synthetic node count; otherwise a tiny
	// source would falsely trip the cyclic-descent fallback. Leaves span [0,1];
	// only byte 0 is read (the "." check), so the padding is never inspected.
	source := make([]byte, n+64)
	source[0] = '.'
	var best time.Duration
	for r := 0; r < runs; r++ {
		root := buildWideDotFinalRefsTree(n)
		poller := parseStopPoller{}
		start := time.Now()
		normalizeGoDotLeafChildrenWithStop(root, source, lang, &poller)
		d := time.Since(start)
		if r == 0 || d < best {
			best = d
		}
	}
	return best
}

// TestNormalizeGoDotLeafChildrenScalesLinearly asserts the dot walker is linear
// (not quadratic) in the width of a flat sibling list. A 4x width increase costs
// ~4x for a linear walk and ~16x for a quadratic one; the < 8x bound cleanly
// separates the two while tolerating allocator/GC noise.
func TestNormalizeGoDotLeafChildrenScalesLinearly(t *testing.T) {
	lang := goDotCompatibilityTestLanguage()
	const nSmall, nLarge = 20000, 80000
	small := timeDotNormalizerMin(lang, nSmall, 6)
	large := timeDotNormalizerMin(lang, nLarge, 6)
	ratio := float64(large) / float64(small)
	t.Logf("n=%d %v  n=%d %v  4x-width ratio=%.2f", nSmall, small, nLarge, large, ratio)
	if small > 0 && ratio > 8.0 {
		t.Fatalf("dot normalizer scaling looks super-linear: 4x width -> %.2fx time (want < 8x)", ratio)
	}
}
