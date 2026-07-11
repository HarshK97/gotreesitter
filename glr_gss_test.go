package gotreesitter

import (
	"testing"
	"unsafe"
)

func TestGSSStackPushCloneAndTruncate(t *testing.T) {
	var scratch gssScratch
	base := newGSSStack(1, &scratch)
	if base.len() != 1 {
		t.Fatalf("base len = %d, want 1", base.len())
	}

	clone := base.clone()
	base.push(2, nil, &scratch)
	base.push(3, nil, &scratch)

	if base.len() != 3 {
		t.Fatalf("base len after pushes = %d, want 3", base.len())
	}
	if clone.len() != 1 {
		t.Fatalf("clone len changed = %d, want 1", clone.len())
	}
	if base.top().state != 3 {
		t.Fatalf("base top state = %d, want 3", base.top().state)
	}

	ok := base.truncate(2)
	if !ok {
		t.Fatal("truncate(2) = false, want true")
	}
	if got := base.top().state; got != 2 {
		t.Fatalf("top after truncate = %d, want 2", got)
	}
}

func TestGSSStackMaterializeAndByteOffset(t *testing.T) {
	var scratch gssScratch
	n1 := &Node{endByte: 5}
	n2 := &Node{endByte: 9}

	var s gssStack
	s.push(1, nil, &scratch)
	s.push(2, n1, &scratch)
	s.push(3, nil, &scratch)
	s.push(4, n2, &scratch)

	got := s.materialize(nil)
	if len(got) != 4 {
		t.Fatalf("materialized len = %d, want 4", len(got))
	}
	if got[0].state != 1 || got[1].state != 2 || got[2].state != 3 || got[3].state != 4 {
		t.Fatalf("unexpected materialized states: %+v", got)
	}

	if off := s.byteOffset(); off != 9 {
		t.Fatalf("byteOffset = %d, want 9", off)
	}

	s.truncate(3)
	if off := s.byteOffset(); off != 5 {
		t.Fatalf("byteOffset after truncate = %d, want 5", off)
	}
}

func TestGLRStackToGSS(t *testing.T) {
	var gScratch gssScratch
	var entryScratch glrEntryScratch
	s := newGLRStackWithScratch(1, &entryScratch)
	s.push(2, nil, &entryScratch, &gScratch)
	s.push(3, nil, &entryScratch, &gScratch)

	gs := s.toGSS(&gScratch)
	mat := gs.materialize(nil)
	want := s.ensureEntries(&entryScratch)
	if len(mat) != len(want) {
		t.Fatalf("materialized len = %d, want %d", len(mat), len(want))
	}
	for i := range mat {
		if mat[i].state != want[i].state {
			t.Fatalf("state[%d] = %d, want %d", i, mat[i].state, want[i].state)
		}
	}
}

func TestGSSStackMaterializePanicsOnCorruptDepth(t *testing.T) {
	head := &gssNode{entry: stackEntry{state: 2}, depth: 3}
	head.prev = &gssNode{entry: stackEntry{state: 1}, depth: 1}
	s := gssStack{head: head}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on corrupt GSS depth metadata")
		}
	}()
	_ = s.materialize(nil)
}

func TestGSSNodeHashComputedLazilyForSingleStackNodes(t *testing.T) {
	var scratch gssScratch
	scratch.singleStackMode = true

	n1 := &Node{symbol: 1, startByte: 0, endByte: 1, parseState: 5}
	n2 := &Node{symbol: 2, startByte: 1, endByte: 3, parseState: 6}

	var s gssStack
	s.push(1, nil, &scratch)
	s.push(2, n1, &scratch)
	s.push(3, n2, &scratch)

	if got := s.head.hash; got != 0 {
		t.Fatalf("head hash before demand = %d, want 0", got)
	}

	got := gssNodeHash(s.head)
	if got == 0 {
		t.Fatal("expected lazy hash to compute non-zero value")
	}
	if s.head.hash != got {
		t.Fatalf("cached head hash = %d, want %d", s.head.hash, got)
	}

	entries := s.materialize(nil)
	want := gssHashSeed
	for i := range entries {
		want = gssEntryHash(want, entries[i])
	}
	if got != want {
		t.Fatalf("lazy hash = %d, want %d", got, want)
	}
}

func TestGSSEntryHashMatchesAccessorSemantics(t *testing.T) {
	node := &Node{
		children:     []*Node{{symbol: 20, startByte: 1, endByte: 2, preGotoState: 8, fieldIDs: []FieldID{3}, flags: nodeFlagNamed}},
		fieldIDs:     []FieldID{2},
		symbol:       10,
		startByte:    1,
		endByte:      3,
		parseState:   4,
		preGotoState: 14,
		productionID: 5,
		flags:        nodeFlagNamed | nodeFlagHasError,
	}
	noTree := &noTreeNode{
		symbol:       11,
		startByte:    2,
		endByte:      5,
		parseState:   6,
		preGotoState: 16,
		productionID: 7,
		flags:        nodeFlagExtra,
	}
	compactLeaf := &compactFullLeaf{
		noTreeNode: noTreeNode{
			symbol:       12,
			startByte:    8,
			endByte:      13,
			parseState:   9,
			preGotoState: 19,
			productionID: 10,
			flags:        nodeFlagNamed | nodeFlagMissing,
		},
	}
	pending := &pendingParent{
		noTreeNode: noTreeNode{
			symbol:       13,
			startByte:    21,
			endByte:      34,
			parseState:   11,
			preGotoState: 21,
			productionID: 12,
			flags:        nodeFlagNamed | nodeFlagExtra,
		},
		childRange: newPendingChildRange(0, 0, 3),
	}

	entries := []stackEntry{
		{state: 1},
		newStackEntryNode(2, node),
		newStackEntryNoTreeNode(3, noTree),
		newStackEntryCompactFullLeaf(4, compactLeaf),
		newStackEntryPendingParent(5, pending),
	}
	for _, entry := range entries {
		got := gssEntryHash(gssHashSeed, entry)
		want := gssEntryHashViaAccessors(gssHashSeed, entry)
		if got != want {
			t.Fatalf("gssEntryHash(%+v) = %d, want %d", entry, got, want)
		}
	}
}

func TestGSSEntryHashIncludesDynamicPrecedence(t *testing.T) {
	low := &Node{symbol: 10, startByte: 1, endByte: 3, parseState: 4, flags: nodeFlagNamed}
	high := &Node{symbol: 10, startByte: 1, endByte: 3, parseState: 4, flags: nodeFlagNamed, dynamicPrecedence: 7}

	lowHash := gssEntryHash(gssHashSeed, newStackEntryNode(2, low))
	highHash := gssEntryHash(gssHashSeed, newStackEntryNode(2, high))
	if lowHash == highHash {
		t.Fatal("gssEntryHash ignored node dynamic precedence")
	}

	lowChild := &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed}
	highChild := &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: 5}
	lowParent := &Node{symbol: 11, startByte: 1, endByte: 3, parseState: 4, flags: nodeFlagNamed, children: []*Node{lowChild}}
	highParent := &Node{symbol: 11, startByte: 1, endByte: 3, parseState: 4, flags: nodeFlagNamed, children: []*Node{highChild}}

	lowParentHash := gssEntryHash(gssHashSeed, newStackEntryNode(2, lowParent))
	highParentHash := gssEntryHash(gssHashSeed, newStackEntryNode(2, highParent))
	if lowParentHash == highParentHash {
		t.Fatal("gssEntryHash ignored shallow child dynamic precedence")
	}
}

func TestGSSMainAddLinkAtCapRejectsUnsafeEquivalentReplacement(t *testing.T) {
	head := &gssNode{
		entry: newStackEntryNode(10, &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: 1}),
		prev:  &gssNode{entry: stackEntry{state: 100}, depth: 1},
		depth: 2,
	}
	for i := 1; i < maxMainLinkCount; i++ {
		head.extraLinks = append(head.extraLinks, gssMainLink{
			prev:  &gssNode{entry: stackEntry{state: StateID(100 + i)}, depth: 1},
			entry: newStackEntryNode(10, &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: int32(i + 1)}),
		})
	}
	if got := head.linkCount(); got != maxMainLinkCount {
		t.Fatalf("head link count = %d, want cap %d", got, maxMainLinkCount)
	}

	latePrev := &gssNode{entry: stackEntry{state: 200}, depth: 1}
	lateEntry := newStackEntryNode(10, &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: 99})
	if gssMainAddLink(head, latePrev, lateEntry) {
		t.Fatal("unsafe capped equivalent replacement was reported as incorporated")
	}

	if got := head.linkCount(); got != maxMainLinkCount {
		t.Fatalf("head link count after late add = %d, want cap %d", got, maxMainLinkCount)
	}
	var foundLate, foundLowest bool
	for i := 0; i < head.linkCount(); i++ {
		prev, entry := head.link(i)
		if prev == latePrev && stackEntryDynamicPrecedence(entry) == 99 {
			foundLate = true
		}
		if stackEntryDynamicPrecedence(entry) == 1 {
			foundLowest = true
		}
	}
	if foundLate {
		t.Fatal("late non-mergeable equivalent link was retained at cap")
	}
	if !foundLowest {
		t.Fatal("original lowest dynamic-precedence branch was lost")
	}
}

func TestGSSMainAddLinkAtCapReplacesEquivalentSamePredecessorWithHigherDynamic(t *testing.T) {
	sharedPrev := &gssNode{entry: stackEntry{state: 100}, depth: 1}
	head := &gssNode{
		entry: newStackEntryNode(10, &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: 1}),
		prev:  sharedPrev,
		depth: 2,
	}
	for i := 1; i < maxMainLinkCount; i++ {
		head.extraLinks = append(head.extraLinks, gssMainLink{
			prev:  &gssNode{entry: stackEntry{state: StateID(100 + i)}, depth: 1},
			entry: newStackEntryNode(10, &Node{symbol: Symbol(20 + i), startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: int32(i + 1)}),
		})
	}
	if got := head.linkCount(); got != maxMainLinkCount {
		t.Fatalf("head link count = %d, want cap %d", got, maxMainLinkCount)
	}

	lateEntry := newStackEntryNode(10, &Node{symbol: 20, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: 99})
	if !gssMainAddLink(head, sharedPrev, lateEntry) {
		t.Fatal("same-predecessor capped equivalent replacement was rejected")
	}
	if got := head.linkCount(); got != maxMainLinkCount {
		t.Fatalf("head link count after late add = %d, want cap %d", got, maxMainLinkCount)
	}
	prev, entry := head.link(0)
	if prev != sharedPrev {
		t.Fatal("replacement changed predecessor identity")
	}
	if got := stackEntryDynamicPrecedence(entry); got != 99 {
		t.Fatalf("dynamic precedence = %d, want 99", got)
	}
}

func TestGSSMainAddLinkMergesNestedPackedPredecessorLinks(t *testing.T) {
	baseA := &gssNode{entry: stackEntry{state: 100}, depth: 1}
	baseB := &gssNode{entry: stackEntry{state: 101}, depth: 1}
	baseC := &gssNode{entry: stackEntry{state: 102}, depth: 1}

	left := func() stackEntry {
		return newStackEntryNode(2, &Node{symbol: 30, startByte: 0, endByte: 1, flags: nodeFlagNamed})
	}
	right := func() stackEntry {
		return newStackEntryNode(3, &Node{symbol: 31, startByte: 1, endByte: 2, flags: nodeFlagNamed})
	}

	packedPred := &gssNode{
		entry: left(),
		prev:  baseA,
		depth: 2,
		extraLinks: []gssMainLink{{
			prev:  baseB,
			entry: left(),
		}},
	}
	head := &gssNode{
		entry: right(),
		prev:  packedPred,
		depth: 3,
	}
	incomingPred := &gssNode{
		entry: left(),
		prev:  baseC,
		depth: 2,
	}

	if !gssMainAddLink(head, incomingPred, right()) {
		t.Fatal("nested packed predecessor link was not incorporated")
	}

	if got := head.linkCount(); got != 1 {
		t.Fatalf("head link count = %d, want 1 merged top link", got)
	}
	if got := packedPred.linkCount(); got != 3 {
		t.Fatalf("packed predecessor link count = %d, want 3", got)
	}

	stack := glrStack{gss: gssStack{head: head}}
	forks := reduceWindowsFromGSS(&stack, 2, maxMainLinkCount)
	if len(forks) != 3 {
		t.Fatalf("reduce windows = %d, want 3", len(forks))
	}
	seenTopStates := make(map[StateID]bool)
	for _, fork := range forks {
		if len(fork.window) != 2 {
			t.Fatalf("window length = %d, want 2", len(fork.window))
		}
		if stackEntryNodeSymbol(fork.window[0]) != 30 || stackEntryNodeSymbol(fork.window[1]) != 31 {
			t.Fatalf("unexpected window symbols: %d, %d", stackEntryNodeSymbol(fork.window[0]), stackEntryNodeSymbol(fork.window[1]))
		}
		seenTopStates[fork.topState] = true
	}
	for _, state := range []StateID{100, 101, 102} {
		if !seenTopStates[state] {
			t.Fatalf("reduce windows did not include branch with top state %d", state)
		}
	}
}

func TestGSSMainMergeFailureLeavesIncumbentPredecessorUnchanged(t *testing.T) {
	branchEntry := func(sym Symbol) stackEntry {
		return newStackEntryNode(2, &Node{symbol: sym, startByte: 0, endByte: 3, flags: nodeFlagNamed})
	}
	topEntry := func(sym Symbol) stackEntry {
		return newStackEntryNode(9, &Node{symbol: sym, startByte: 3, endByte: 4, flags: nodeFlagNamed})
	}
	base := func(state StateID) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, depth: 1}
	}

	wBase := base(1000)
	w := &gssNode{
		entry: branchEntry(100),
		prev:  wBase,
		depth: 2,
	}
	for i := 1; i < maxMainLinkCount-1; i++ {
		w.extraLinks = append(w.extraLinks, gssMainLink{
			prev:  base(StateID(1000 + i)),
			entry: branchEntry(Symbol(100 + i)),
		})
	}
	w.extraLinks = append(w.extraLinks, gssMainLink{
		prev:  base(1999),
		entry: branchEntry(199),
	})
	if got := w.linkCount(); got != maxMainLinkCount {
		t.Fatalf("incumbent predecessor link count = %d, want %d", got, maxMainLinkCount)
	}

	x := &gssNode{
		entry: branchEntry(100),
		prev:  wBase,
		depth: 2,
	}
	y := &gssNode{
		entry: branchEntry(200),
		prev:  base(3000),
		depth: 2,
	}
	incumbentHead := &gssNode{
		entry: topEntry(1),
		prev:  w,
		depth: 3,
		extraLinks: []gssMainLink{{
			prev:  x,
			entry: topEntry(2),
		}},
	}
	incomingHead := &gssNode{
		entry: topEntry(1),
		prev:  y,
		depth: 3,
		extraLinks: []gssMainLink{{
			prev:  x,
			entry: topEntry(1),
		}},
	}
	beforePred := snapshotGSSMainLinks(w)
	beforeX := snapshotGSSMainLinks(x)
	beforeHead := snapshotGSSMainLinks(incumbentHead)

	incumbent := glrStack{gss: gssStack{head: incumbentHead}, byteOffset: 2}
	candidate := glrStack{gss: gssStack{head: incomingHead}, byteOffset: 2}
	if gssMainMerge(&incumbent, &candidate) {
		t.Fatal("gssMainMerge reported success after omitting a virtual source link")
	}
	assertGSSMainLinksEqual(t, w, beforePred)
	assertGSSMainLinksEqual(t, x, beforeX)
	assertGSSMainLinksEqual(t, incumbentHead, beforeHead)
}

func TestGSSMainCanMergeNodesEnumeratesVirtualSourceLinks(t *testing.T) {
	entry := func(sym Symbol) stackEntry {
		return newStackEntryNode(2, &Node{symbol: sym, startByte: 0, endByte: 1, flags: nodeFlagNamed})
	}
	base := func(state StateID) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, depth: 1}
	}

	sharedPrev := base(100)
	source := &gssNode{
		entry: entry(10),
		prev:  sharedPrev,
		depth: 2,
	}
	dest := &gssNode{
		entry: entry(10),
		prev:  sharedPrev,
		depth: 2,
	}
	for i := 1; i < maxMainLinkCount; i++ {
		dest.extraLinks = append(dest.extraLinks, gssMainLink{
			prev:  base(StateID(200 + i)),
			entry: entry(Symbol(20 + i)),
		})
	}
	if got := dest.linkCount(); got != maxMainLinkCount {
		t.Fatalf("destination link count = %d, want %d", got, maxMainLinkCount)
	}

	p := newGSSMainPreflight(make(map[gssMergePair]bool))
	virtualPrev := base(300)
	virtualEntry := entry(99)
	if !p.canAddLink(source, virtualPrev, virtualEntry) {
		t.Fatal("failed to stage virtual source link")
	}
	if got := source.linkCount(); got != 1 {
		t.Fatalf("source real link count = %d, want 1", got)
	}
	if got := p.linkCount(source); got != 2 {
		t.Fatalf("source preflight link count = %d, want 2 including virtual source link", got)
	}

	if p.canMergeNodes(dest, source) {
		t.Fatal("canMergeNodes skipped the virtual-only source link and accepted an over-cap merge")
	}
}

func TestGSSMainPreflightCachedReachSeesVirtualLinkAfterFalse(t *testing.T) {
	base := func(state StateID, prev *gssNode) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, prev: prev, depth: 1}
	}

	tail := base(3, nil)
	mid := base(2, nil)
	head := base(1, mid)
	p := newGSSMainPreflight(make(map[gssMergePair]bool))

	if p.canReach(head, tail) {
		t.Fatal("head reached isolated tail before virtual link")
	}
	p.addVirtualLink(mid, tail, stackEntry{state: 9})
	if !p.canReach(head, tail) {
		t.Fatal("cached preflight reachability missed newly added virtual link")
	}
}

func TestGSSMainPreflightCachedReachInvalidatesFalseBeforeCycleCheck(t *testing.T) {
	base := func(state StateID, prev *gssNode) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, prev: prev, depth: 1}
	}

	mid := base(2, nil)
	head := base(1, mid)
	tail := base(3, nil)
	p := newGSSMainPreflight(make(map[gssMergePair]bool))

	if p.canReach(tail, head) {
		t.Fatal("tail reached head before virtual link")
	}
	p.addVirtualLink(tail, head, stackEntry{state: 9})
	if p.canAddLink(head, tail, stackEntry{state: 10}) {
		t.Fatal("stale false reachability allowed a virtual cycle")
	}
	if got := p.linkCount(head); got != 1 {
		t.Fatalf("head preflight link count = %d, want 1 after rejected cycle link", got)
	}
}

func TestGSSMainPreflightCleanZeroCacheInvalidatesAfterVirtualErrorLink(t *testing.T) {
	base := func(state StateID, prev *gssNode, depth int) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, prev: prev, depth: depth}
	}

	tail := base(3, nil, 0)
	mid := base(2, tail, 1)
	head := base(1, mid, 2)
	errorPrev := base(4, nil, 0)
	errorNode := &Node{symbol: errorSymbol, startByte: 0, endByte: 0, flags: nodeFlagNamed | nodeFlagHasError}
	errorEntry := newStackEntryNode(5, errorNode)
	p := newGSSMainPreflight(make(map[gssMergePair]bool))

	if !p.cleanZeroErrorAllLinks(head) {
		t.Fatal("clean preflight graph was rejected before virtual error link")
	}
	p.addVirtualLink(mid, errorPrev, errorEntry)
	if p.cleanZeroErrorAllLinks(head) {
		t.Fatal("cached clean-zero result survived virtual error link")
	}
}

func TestGSSMainMergeRejectsVirtualCycleWithoutPartialMutation(t *testing.T) {
	entry := func(sym Symbol, start, end uint32) stackEntry {
		return newStackEntryNode(2, &Node{symbol: sym, startByte: start, endByte: end, flags: nodeFlagNamed})
	}
	topEntry := func(sym Symbol) stackEntry {
		return newStackEntryNode(9, &Node{symbol: sym, startByte: 3, endByte: 4, flags: nodeFlagNamed})
	}
	base := func(state StateID) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, depth: 1}
	}

	a := &gssNode{
		entry: entry(10, 0, 1),
		prev:  base(100),
		depth: 2,
	}
	x := &gssNode{
		entry: entry(11, 0, 3),
		prev:  base(101),
		depth: 2,
	}
	y := &gssNode{
		entry: entry(12, 0, 3),
		prev:  a,
		depth: 2,
	}

	incumbentHead := &gssNode{
		entry: topEntry(20),
		prev:  a,
		depth: 3,
		extraLinks: []gssMainLink{{
			prev:  x,
			entry: topEntry(21),
		}},
	}
	for i := incumbentHead.linkCount(); i < maxMainLinkCount; i++ {
		incumbentHead.extraLinks = append(incumbentHead.extraLinks, gssMainLink{
			prev:  base(StateID(200 + i)),
			entry: topEntry(Symbol(30 + i)),
		})
	}
	if got := incumbentHead.linkCount(); got != maxMainLinkCount {
		t.Fatalf("incumbent head link count = %d, want %d", got, maxMainLinkCount)
	}

	candidate := &gssNode{
		entry: topEntry(21),
		prev:  y,
		depth: 3,
		extraLinks: []gssMainLink{{
			prev:  x,
			entry: topEntry(20),
		}},
	}

	beforeHead := snapshotGSSMainLinks(incumbentHead)
	beforeX := snapshotGSSMainLinks(x)

	incumbentStack := glrStack{gss: gssStack{head: incumbentHead}, byteOffset: 4}
	candidateStack := glrStack{gss: gssStack{head: candidate}, byteOffset: 4}
	if gssMainMerge(&incumbentStack, &candidateStack) {
		t.Fatal("gssMainMerge reported success for virtual cycle")
	}
	assertGSSMainLinksEqual(t, incumbentHead, beforeHead)
	assertGSSMainLinksEqual(t, x, beforeX)
}

func TestGSSMainEquivalentReplacementFailureLeavesWorstPredecessorUnchanged(t *testing.T) {
	equivEntry := func(dynamic int32) stackEntry {
		return newStackEntryNode(9, &Node{symbol: 60, startByte: 1, endByte: 2, flags: nodeFlagNamed, dynamicPrecedence: dynamic})
	}
	branchEntry := func(sym Symbol) stackEntry {
		return newStackEntryNode(2, &Node{symbol: sym, startByte: 0, endByte: 1, flags: nodeFlagNamed})
	}
	base := func(state StateID) *gssNode {
		return &gssNode{entry: stackEntry{state: state}, depth: 1}
	}

	worstPrev := &gssNode{
		entry: branchEntry(300),
		prev:  base(3000),
		depth: 2,
	}
	for i := 1; i < maxMainLinkCount-1; i++ {
		worstPrev.extraLinks = append(worstPrev.extraLinks, gssMainLink{
			prev:  base(StateID(3000 + i)),
			entry: branchEntry(Symbol(300 + i)),
		})
	}
	if got := worstPrev.linkCount(); got != maxMainLinkCount-1 {
		t.Fatalf("worst predecessor link count = %d, want %d", got, maxMainLinkCount-1)
	}

	head := &gssNode{entry: equivEntry(1), prev: worstPrev, depth: 3}
	for i := 1; i < maxMainLinkCount; i++ {
		head.extraLinks = append(head.extraLinks, gssMainLink{
			prev:  base(StateID(4000 + i)),
			entry: equivEntry(int32(10 + i)),
		})
	}

	incomingPrev := &gssNode{
		entry: branchEntry(400),
		prev:  base(5000),
		depth: 2,
		extraLinks: []gssMainLink{{
			prev:  base(5001),
			entry: branchEntry(401),
		}},
	}
	beforeWorst := snapshotGSSMainLinks(worstPrev)
	beforeHead := snapshotGSSMainLinks(head)

	if gssMainReplaceWorstEquivalentLinkIfBetter(head, incomingPrev, equivEntry(99)) {
		t.Fatal("capped equivalent replacement reported success for overfull nested predecessor merge")
	}
	assertGSSMainLinksEqual(t, worstPrev, beforeWorst)
	assertGSSMainLinksEqual(t, head, beforeHead)
}

func snapshotGSSMainLinks(n *gssNode) []gssMainLink {
	links := make([]gssMainLink, n.linkCount())
	for i := range links {
		prev, entry := n.link(i)
		links[i] = gssMainLink{prev: prev, entry: entry}
	}
	return links
}

func assertGSSMainLinksEqual(t *testing.T, n *gssNode, want []gssMainLink) {
	t.Helper()
	if got := n.linkCount(); got != len(want) {
		t.Fatalf("link count = %d, want %d", got, len(want))
	}
	for i, wantLink := range want {
		gotPrev, gotEntry := n.link(i)
		if gotPrev != wantLink.prev || gotEntry != wantLink.entry {
			t.Fatalf("link %d changed: got (%p,%+v), want (%p,%+v)", i, gotPrev, gotEntry, wantLink.prev, wantLink.entry)
		}
	}
}

func gssEntryHashViaAccessors(prev uint64, entry stackEntry) uint64 {
	h := prev ^ uint64(entry.state)
	h *= gssHashPrime

	if !stackEntryHasNode(entry) {
		h ^= gssNilNodeSentinel
		h *= gssHashPrime
		return h
	}

	h ^= uint64(stackEntryNodeSymbol(entry))
	h *= gssHashPrime
	h ^= (uint64(stackEntryNodeStartByte(entry)) << 32) | uint64(stackEntryNodeEndByte(entry))
	h *= gssHashPrime
	h ^= uint64(stackEntryNodeParseState(entry))
	h *= gssHashPrime
	h ^= uint64(stackEntryNodePreGotoState(entry))
	h *= gssHashPrime
	h ^= uint64(stackEntryNodeProductionID(entry))
	h *= gssHashPrime
	h ^= uint64(stackEntryNodeChildCount(entry))
	h *= gssHashPrime
	h ^= uint64(uint32(stackEntryDynamicPrecedence(entry)))
	h *= gssHashPrime

	var flags uint64
	if stackEntryNodeIsExtra(entry) {
		flags |= 1
	}
	if stackEntryNodeIsNamed(entry) {
		flags |= 1 << 1
	}
	if stackEntryNodeHasError(entry) {
		flags |= 1 << 2
	}
	if stackEntryNodeIsMissing(entry) {
		flags |= 1 << 3
	}
	h ^= flags
	h *= gssHashPrime
	if n := stackEntryNode(entry); n != nil {
		h = gssNodeShallowMergeHashViaAccessors(h, n)
	}
	return h
}

func gssNodeShallowMergeHashViaAccessors(h uint64, n *Node) uint64 {
	if n == nil {
		h ^= gssNilNodeSentinel
		h *= gssHashPrime
		return h
	}
	h ^= uint64(len(n.fieldIDs))
	h *= gssHashPrime
	for i := range n.fieldIDs {
		h ^= uint64(n.fieldIDs[i])
		h *= gssHashPrime
	}
	for i := range n.children {
		child := n.children[i]
		if child == nil {
			h ^= gssNilNodeSentinel
			h *= gssHashPrime
			continue
		}
		h ^= uint64(child.symbol)
		h *= gssHashPrime
		h ^= (uint64(child.startByte) << 32) | uint64(child.endByte)
		h *= gssHashPrime
		h ^= uint64(child.preGotoState)
		h *= gssHashPrime
		h ^= uint64(nodeChildCountNoMaterialize(child))
		h *= gssHashPrime
		h ^= uint64(len(child.fieldIDs))
		h *= gssHashPrime
		h ^= uint64(uint32(child.dynamicPrecedence))
		h *= gssHashPrime
		h ^= gssEntryFlagHash(child.flags & nodeStackEquivNoMissingFlagMask)
		h *= gssHashPrime
	}
	return h
}

func TestGSSStacksEqualWithLazyHashes(t *testing.T) {
	var scratch gssScratch
	scratch.singleStackMode = true

	left := &Node{symbol: 1, startByte: 0, endByte: 1}
	right := &Node{symbol: 2, startByte: 1, endByte: 2}

	build := func() gssStack {
		var s gssStack
		s.push(1, nil, &scratch)
		s.push(2, left, &scratch)
		s.push(3, right, &scratch)
		return s
	}

	a := build()
	b := build()
	if a.head.hash != 0 || b.head.hash != 0 {
		t.Fatal("expected stacks to start with lazy hashes")
	}
	if !gssStacksEqual(a, b) {
		t.Fatal("expected equal GSS stacks with lazy hashes")
	}
	if a.head.hash == 0 || b.head.hash == 0 {
		t.Fatal("expected equality check to populate lazy hashes")
	}
}

func TestGLRStackDemoteLinearGSSPreservesStackAndCanRefork(t *testing.T) {
	var entryScratch glrEntryScratch
	var gssScratch gssScratch
	stack := newGLRStackWithScratch(1, &entryScratch)
	first := &Node{symbol: 11, startByte: 0, endByte: 3}
	second := &Node{symbol: 12, startByte: 3, endByte: 7}
	stack.push(2, first, &entryScratch, &gssScratch)
	stack.push(3, second, &entryScratch, &gssScratch)
	stack.score = 9
	stack.recoverabilityKnown = true
	stack.mayRecover = true
	stack.ensureGSS(&gssScratch)
	stack.entries = nil

	if !stack.demoteLinearGSS(&entryScratch) {
		t.Fatal("demoteLinearGSS() = false, want true for a linear GSS")
	}
	if stack.gss.head != nil {
		t.Fatalf("GSS head after demotion = %p, want nil", stack.gss.head)
	}
	if got, want := len(stack.entries), 3; got != want {
		t.Fatalf("entry depth after demotion = %d, want %d", got, want)
	}
	if got, want := stack.entries[0].state, StateID(1); got != want {
		t.Fatalf("root state after demotion = %d, want %d", got, want)
	}
	if got := stackEntryNode(stack.entries[1]); got != first {
		t.Fatalf("first node after demotion = %p, want %p", got, first)
	}
	if got := stackEntryNode(stack.entries[2]); got != second {
		t.Fatalf("second node after demotion = %p, want %p", got, second)
	}
	if got, want := stack.byteOffset, uint32(7); got != want {
		t.Fatalf("byte offset after demotion = %d, want %d", got, want)
	}
	if stack.score != 9 || !stack.recoverabilityKnown || !stack.mayRecover {
		t.Fatalf("stack metadata changed after demotion: %+v", stack)
	}

	usedBeforeRefork := gssScratch.usedTotal
	clone := stack.cloneWithScratch(&gssScratch)
	if stack.gss.head == nil || clone.gss.head != stack.gss.head {
		t.Fatalf("refork did not restore shared GSS head: stack=%p clone=%p", stack.gss.head, clone.gss.head)
	}
	if got, want := gssScratch.usedTotal-usedBeforeRefork, len(stack.entries); got != want {
		t.Fatalf("refork GSS allocations = %d, want stack depth %d", got, want)
	}
}

func TestGLRStackDemoteLinearGSSRejectsPackedLinks(t *testing.T) {
	var scratch gssScratch
	stack := glrStack{gss: buildGSSStack([]stackEntry{{state: 1}, {state: 2}, {state: 3}}, &scratch)}
	packed := stack.gss.head.prev
	packed.extraLinks = append(packed.extraLinks, gssMainLink{entry: stackEntry{state: 4}})
	originalHead := stack.gss.head

	if stack.demoteLinearGSS(nil) {
		t.Fatal("demoteLinearGSS() = true, want false for a deep packed link")
	}
	if stack.gss.head != originalHead || stack.entries != nil {
		t.Fatalf("packed stack mutated on rejected demotion: head=%p entries=%d", stack.gss.head, len(stack.entries))
	}
}

func TestGSSScratchResetClearsTouchedSlots(t *testing.T) {
	var scratch gssScratch
	node := &Node{endByte: 1}
	var stack gssStack
	stack.push(1, node, &scratch)
	stack.push(2, node, &scratch)
	if len(scratch.slabs) == 0 || scratch.slabs[0].used != 2 {
		t.Fatalf("expected two used GSS slots, slabs=%d used=%d", len(scratch.slabs), scratch.slabs[0].used)
	}

	scratch.reset()

	slab := scratch.slabs[0]
	if slab.used != 0 {
		t.Fatalf("slab.used after reset = %d, want 0", slab.used)
	}
	for i := 0; i < 2; i++ {
		if stackEntryNode(slab.data[i].entry) != nil {
			t.Fatalf("slab.data[%d].entry node after reset = %p, want nil", i, stackEntryNode(slab.data[i].entry))
		}
		if slab.data[i].prev != nil {
			t.Fatalf("slab.data[%d].prev after reset = %p, want nil", i, slab.data[i].prev)
		}
	}
}

func TestGSSScratchOverflowSlabGrowthBounded(t *testing.T) {
	elemSize := int(unsafe.Sizeof(gssNode{}))
	ceiling := maxOverflowSlabGrowthBytes / elemSize
	if ceiling <= 0 {
		t.Fatalf("invalid GSS slab ceiling %d", ceiling)
	}

	var scratch gssScratch
	// Start close enough to the ceiling that the old lastCap*2 policy would
	// immediately overshoot it, while keeping the test allocation modest.
	scratch.initialCap = ceiling * 3 / 4
	total := scratch.initialCap + ceiling*3
	var prev *gssNode
	for depth := 1; depth <= total; depth++ {
		prev = scratch.allocNode(stackEntry{state: 1}, prev, depth)
	}

	if len(scratch.slabs) < 3 {
		t.Fatalf("expected multiple GSS overflow slabs, got %d", len(scratch.slabs))
	}
	capacity := 0
	for i := range scratch.slabs {
		slabCap := len(scratch.slabs[i].data)
		capacity += slabCap
		if i > 0 && slabCap > ceiling {
			t.Fatalf("GSS overflow slab %d capacity=%d exceeds ceiling=%d", i, slabCap, ceiling)
		}
	}
	if waste := capacity - scratch.usedTotal; waste > ceiling {
		t.Fatalf("GSS capacity waste=%d nodes exceeds one ceiling slab=%d", waste, ceiling)
	}
	if got, want := scratch.allocatedBytes, gssNodeBytesForCap(capacity); got != want {
		t.Fatalf("GSS allocatedBytes=%d, want capacity bytes=%d", got, want)
	}
}
