package gotreesitter

import "unsafe"

type rawShapeRef uint32

const rawShapeRefIndexBits = 20

type rawShape struct {
	// Keep the 8-byte fields first. This holds the sidecar header to 24 bytes
	// on 64-bit targets instead of 32 bytes lost to alignment padding; large
	// GLR parses retain millions of these headers.
	childRange   rawShapeChildRange
	symbol       Symbol
	productionID uint16
	childCount   uint16
	// contentHash is a bottom-up structural fingerprint over (symbol,
	// productionID, childCount) plus every child's (symbol, span, and — when
	// the child itself has a captured raw shape — the child's own
	// contentHash). Computed once in captureRawShape, when the shape is
	// captured (children are always captured strictly before their parent, so
	// every child's contentHash is already populated — this is one linear
	// pass, not a recursive walk).
	//
	// The two existing consumers trust this hash in OPPOSITE directions:
	//
	//   - forestRawShapesExactEqualRec (glr_forest.go) trusts a MISMATCH: a
	//     different hash proves the two shapes are not exactly
	//     interchangeable (the contrapositive of "equal inputs hash
	//     equally"), so it fast-rejects without a walk. A MATCH is never
	//     trusted by itself — it still falls through to the existing exact
	//     recursive walk as a collision safety net. So this consumer's use of
	//     the hash never changes what it returns, only whether a fast-reject
	//     or a full walk produces that answer; it is a provably
	//     answer-preserving optimization.
	//
	//   - rawStackEntryChildPairHashEqual (parser_reduce.go, feeding
	//     compareRawStackEntriesRec) trusts a MATCH: hash equality plus
	//     matching symbol/childCount/span is treated as sufficient on its own
	//     to skip the walk and declare the pair equal, with no fallback
	//     verification. This is a probabilistic shortcut, not a proof (see
	//     that function's doc comment) — it accepts the same
	//     negligible-collision (~2^-64 FNV-1a) tradeoff already used for this
	//     package's GSS merge-key hashing (see rawShapeComputeContentHash
	//     below and glr_gss.go). A MISMATCH there falls through to the real
	//     recursive comparison, unaffected. This direction is the inverse of
	//     forestRawShapesExactEqualRec's, so a hash collision could change
	//     this consumer's answer for one child pair; the effect is limited to
	//     ambiguity tie-break/ordering choices and never touches memory
	//     safety.
	//
	// See the forest link-cap eviction path (glr_forest.go
	// forestCapReplacementIndex): on shapes with a long shared prefix (e.g.
	// C# designer-style repeated-statement blocks), that path re-derives the
	// same "is this exactly the resident's shape" question for every new
	// alternative, and without this fingerprint each question re-walks the
	// whole accumulated subtree.
	contentHash uint64
}

type rawShapeChild struct {
	// packedEntry.state stores the per-edge rawShapeRef snapshot. Raw-shape
	// consumers inspect payload identity/kind but never LR state; packing the
	// two uint32 values into the existing stackEntry slot keeps this record at
	// 16 bytes instead of 24. entry() restores a meaningful current state for
	// callers so the packed representation cannot escape accidentally.
	packedEntry stackEntry
}

func newRawShapeChild(entry stackEntry) rawShapeChild {
	ref := stackEntryRawShapeRef(entry)
	entry.state = StateID(ref)
	return rawShapeChild{packedEntry: entry}
}

func (c rawShapeChild) entry() stackEntry {
	entry := c.packedEntry
	entry.state = stackEntryNodeParseState(entry)
	return entry
}

func (c rawShapeChild) shapeRef() rawShapeRef {
	return rawShapeRef(c.packedEntry.state)
}

func (c *rawShapeChild) retargetNodePreservingShapeRef(node *Node) {
	if c == nil || node == nil || stackEntryNode(c.packedEntry) == nil {
		return
	}
	ref := c.packedEntry.state
	setStackEntryNode(&c.packedEntry, node)
	c.packedEntry.state = ref
}

type rawShapeSlab struct {
	data []rawShape
	used int
}

type rawShapeChildSlab struct {
	data []rawShapeChild
	used int
}

type rawShapeChildRange uint64

// reclaimRawShapeStorage releases parser-only reduction-shape sidecars after
// the returned result has been selected and fully materialized. Raw shapes are
// not part of the public tree: queries, cursors, edits, and incremental reuse
// read the materialized node structure instead.
//
// Clear every arena-owned payload's ref before resetting and trimming the
// slabs. Returned trees can lazily materialize compact leaves and pending
// parents after parse finalization, so clearing only the currently materialized
// Node values would let a stale arena-relative ref escape through that later
// materialization. Clearing all payload forms also prevents a reused subtree
// from carrying a ref into a different parse arena, where the same numeric ref
// could name an unrelated shape.
func (a *nodeArena) reclaimRawShapeStorage() {
	if a == nil {
		return
	}
	clearNodeRawShapeRefs := func(nodes []Node, used int) {
		if used > len(nodes) {
			used = len(nodes)
		}
		for i := 0; i < used; i++ {
			nodes[i].rawShape = 0
		}
	}
	clearNoTreeRawShapeRefs := func(nodes []noTreeNode, used int) {
		if used > len(nodes) {
			used = len(nodes)
		}
		for i := 0; i < used; i++ {
			nodes[i].rawShape = 0
		}
	}

	primaryUsed := a.used
	if primaryUsed > len(a.nodes) {
		primaryUsed = len(a.nodes)
	}
	clearNodeRawShapeRefs(a.nodes, primaryUsed)
	for i := range a.nodeSlabs {
		clearNodeRawShapeRefs(a.nodeSlabs[i].data, a.nodeSlabs[i].used)
	}
	for i := range a.noTreeNodeSlabs {
		clearNoTreeRawShapeRefs(a.noTreeNodeSlabs[i].data, a.noTreeNodeSlabs[i].used)
	}
	for i := range a.compactFullLeafSlabs {
		slab := &a.compactFullLeafSlabs[i]
		used := slab.used
		if used > len(slab.data) {
			used = len(slab.data)
		}
		for j := 0; j < used; j++ {
			slab.data[j].rawShape = 0
		}
	}
	for i := range a.pendingParentSlabs {
		slab := &a.pendingParentSlabs[i]
		used := slab.used
		if used > len(slab.data) {
			used = len(slab.data)
		}
		for j := 0; j < used; j++ {
			slab.data[j].rawShape = 0
		}
	}
	for i := range a.compactCheckpointLeafSlabs {
		slab := &a.compactCheckpointLeafSlabs[i]
		used := slab.used
		if used > len(slab.data) {
			used = len(slab.data)
		}
		for j := 0; j < used; j++ {
			slab.data[j].rawShape = 0
		}
	}

	// Reuse the arena's existing bounded reset policy: it drops pathological
	// overflow while retaining a small warm prefix for the next parse. Nil'ing
	// every slab here would force the 2 MiB full-parse base slabs to be allocated
	// again on each pooled parse, turning reclamation into an allocation
	// regression on ordinary files.
	a.resetRawShapeSlabs()
	a.resetRawShapeChildSlabs()
	a.recomputeAllocatedBytes()
}

func rawShapeBytesForCap(n int) int64 {
	if n <= 0 {
		return 0
	}
	return int64(n) * int64(unsafe.Sizeof(rawShape{}))
}

func rawShapeChildBytesForCap(n int) int64 {
	if n <= 0 {
		return 0
	}
	return int64(n) * int64(unsafe.Sizeof(rawShapeChild{}))
}

func defaultRawShapeSlabCap(class arenaClass) int {
	slabBytes := incrementalArenaSlab
	if class == arenaClassFull {
		slabBytes = fullParseArenaSlab
	}
	size := int(unsafe.Sizeof(rawShape{}))
	if size <= 0 {
		return minArenaNodeCap
	}
	capacity := slabBytes / size
	if capacity < minArenaNodeCap {
		return minArenaNodeCap
	}
	return capacity
}

func defaultRawShapeChildSlabCap(class arenaClass) int {
	slabBytes := incrementalArenaSlab
	if class == arenaClassFull {
		slabBytes = fullParseArenaSlab
	}
	size := int(unsafe.Sizeof(rawShapeChild{}))
	if size <= 0 {
		return minArenaNodeCap
	}
	capacity := slabBytes / size
	if capacity < minArenaNodeCap {
		return minArenaNodeCap
	}
	return capacity
}

func makeRawShapeChildRange(slab, start, count int) rawShapeChildRange {
	return rawShapeChildRange((uint64(slab+1) << 48) | (uint64(start) << 16) | uint64(count))
}

func (r rawShapeChildRange) slabIndex() int {
	return int((uint64(r)>>48)&0xffff) - 1
}

func (r rawShapeChildRange) start() int {
	return int((uint64(r) >> 16) & 0xffffffff)
}

func (a *nodeArena) rawShapeForRef(ref rawShapeRef) (*rawShape, bool) {
	if a == nil || ref == 0 {
		return nil, false
	}
	slabIdx := int(uint32(ref)>>rawShapeRefIndexBits) - 1
	entryIdx := int(uint32(ref) & ((uint32(1) << rawShapeRefIndexBits) - 1))
	if slabIdx < 0 || slabIdx >= len(a.rawShapeSlabs) {
		return nil, false
	}
	slab := &a.rawShapeSlabs[slabIdx]
	if entryIdx < 0 || entryIdx >= slab.used || entryIdx >= len(slab.data) {
		return nil, false
	}
	return &slab.data[entryIdx], true
}

func (a *nodeArena) rawShapeChildren(shape *rawShape) []rawShapeChild {
	if a == nil || shape == nil || shape.childCount == 0 || shape.childRange == 0 {
		return nil
	}
	slabIdx := shape.childRange.slabIndex()
	start := shape.childRange.start()
	count := int(shape.childCount)
	if slabIdx < 0 || slabIdx >= len(a.rawShapeChildSlabs) {
		return nil
	}
	slab := &a.rawShapeChildSlabs[slabIdx]
	if start < 0 || count < 0 || start+count > slab.used || start+count > len(slab.data) {
		return nil
	}
	return slab.data[start : start+count]
}

func (a *nodeArena) rawShapeChildrenForNode(node *Node) []rawShapeChild {
	if a == nil || node == nil || node.rawShape == 0 {
		return nil
	}
	shape, ok := a.rawShapeForRef(node.rawShape)
	if !ok {
		return nil
	}
	return a.rawShapeChildren(shape)
}

// captureRawShape captures the reduction-shape sidecar documented on rawShape
// (raw_shape.go). gssScratch gates a single-stack happy-path optimization:
// every reader of the resulting ref is nil-guarded with an explicit fallback
// (memory-safe unconditionally — see rawShapeForStackEntry and callers), so
// skipping the capture entirely while gssScratch.mayElideRawShape() is true
// is always safe. It is NOT always behavior-neutral across a prefix-then-fork
// transition (later tie-break comparisons, e.g. compareRawStackEntriesRec,
// can consult a shape's pre-flattening structure), which is why
// mayElideRawShape latches permanently false for the rest of a parse the
// first time it ever forks (see gssScratch.everForked). Pass nil for
// gssScratch from call sites that are already proven fork-only/reachable
// only outside the pure single-stack path (e.g. reduceForkTemporaryParent) or
// that are out of scope for this optimization (e.g. the noTreeBenchmarkOnly
// lane): nil never elides, matching the pre-optimization behavior. See
// spore.2026-07-12.hazel.rawshape-elision-rca.
func (p *Parser) captureRawShape(gssScratch *gssScratch, arena *nodeArena, symbol Symbol, productionID uint16, entries []stackEntry, start, end int) rawShapeRef {
	if arena == nil || start < 0 || end < start || end > len(entries) {
		return 0
	}
	if gssScratch.mayElideRawShape() {
		return 0
	}
	count := 0
	for i := start; i < end; i++ {
		if stackEntryHasNode(entries[i]) {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	ref, shape := arena.allocRawShape()
	if shape == nil {
		return 0
	}
	shape.symbol = symbol
	shape.productionID = productionID
	if count > 0xffff {
		count = 0xffff
	}
	shape.childCount = uint16(count)
	if count == 0 {
		return ref
	}
	childRange := arena.allocRawShapeChildren(count)
	children := arena.rawShapeChildren(&rawShape{childRange: childRange, childCount: uint16(count)})
	out := 0
	for i := start; i < end && out < count; i++ {
		entry := entries[i]
		if !stackEntryHasNode(entry) {
			continue
		}
		children[out] = newRawShapeChild(entry)
		out++
	}
	shape.childRange = childRange
	shape.contentHash = rawShapeComputeContentHash(arena, symbol, productionID, uint16(count), children[:out])
	return ref
}

// rawShapeComputeContentHash builds the bottom-up structural fingerprint
// documented on rawShape.contentHash. It folds in the same fields the exact
// raw-shape comparators inspect (symbol, productionID, childCount, and per
// child: whether it has a node, its symbol, its span, and — recursively —
// its own already-computed contentHash when it has a captured shape,
// otherwise its own child count as a coarse stand-in for a leaf's shape).
// Reusing the package's existing 64-bit FNV-1a combiner (gssHashSeed/
// gssHashPrime/gssNilNodeSentinel, glr_gss.go) keeps this consistent with the
// GSS merge-key hashing that already accepts the same negligible-collision
// tradeoff for equivalence decisions.
func rawShapeComputeContentHash(arena *nodeArena, symbol Symbol, productionID uint16, childCount uint16, children []rawShapeChild) uint64 {
	h := gssHashSeed
	h ^= uint64(symbol)
	h *= gssHashPrime
	h ^= uint64(productionID)
	h *= gssHashPrime
	h ^= uint64(childCount)
	h *= gssHashPrime
	for i := range children {
		entry := children[i].entry()
		if !stackEntryHasNode(entry) {
			h ^= gssNilNodeSentinel
			h *= gssHashPrime
			continue
		}
		h ^= uint64(stackEntryNodeSymbol(entry))
		h *= gssHashPrime
		h ^= (uint64(stackEntryNodeStartByte(entry)) << 32) | uint64(stackEntryNodeEndByte(entry))
		h *= gssHashPrime
		if ref := children[i].shapeRef(); ref != 0 && arena != nil {
			if childShape, ok := arena.rawShapeForRef(ref); ok {
				h ^= childShape.contentHash
				h *= gssHashPrime
				continue
			}
		}
		h ^= uint64(stackEntryNodeChildCount(entry))
		h *= gssHashPrime
	}
	return h
}

func stackEntryRawShapeRef(entry stackEntry) rawShapeRef {
	if n := stackEntryNode(entry); n != nil {
		return n.rawShape
	}
	if n := stackEntryNoTreeNode(entry); n != nil {
		return n.rawShape
	}
	if n := stackEntryCompactFullLeaf(entry); n != nil {
		return n.rawShape
	}
	if n := stackEntryPendingParent(entry); n != nil {
		return n.rawShape
	}
	return 0
}

func setStackEntryRawShapeRef(entry *stackEntry, ref rawShapeRef) {
	if entry == nil {
		return
	}
	if n := stackEntryNode(*entry); n != nil {
		n.rawShape = ref
		nodeBumpEquivVersionMetadata(n)
		return
	}
	if n := stackEntryNoTreeNode(*entry); n != nil {
		n.rawShape = ref
		return
	}
	if n := stackEntryCompactFullLeaf(*entry); n != nil {
		n.rawShape = ref
		return
	}
	if n := stackEntryPendingParent(*entry); n != nil {
		n.rawShape = ref
	}
}

func compareAcceptedStackRawShapePreference(p *Parser, arena *nodeArena, a, b glrStack) int {
	if !a.accepted || !b.accepted || arena == nil {
		return 0
	}
	aCount := stackMaterializingResultEntryCount(a)
	if aCount == 0 || aCount != stackMaterializingResultEntryCount(b) {
		return 0
	}
	const maxBufferedRawShapeEntries = 8
	if aCount > maxBufferedRawShapeEntries {
		return 0
	}
	var aBuf, bBuf [maxBufferedRawShapeEntries]stackEntry
	aEntries, aOK := stackMaterializingResultEntries(a, aBuf[:0], aCount)
	bEntries, bOK := stackMaterializingResultEntries(b, bBuf[:0], aCount)
	if !aOK || !bOK {
		return 0
	}
	if !rawStackEntriesContainShape(arena, aEntries) && !rawStackEntriesContainShape(arena, bEntries) {
		return 0
	}
	for i := 0; i < aCount; i++ {
		cmp := p.compareRawStackEntries(arena, aEntries[i], bEntries[i])
		if cmp != 0 {
			if cmp < 0 {
				return 1
			}
			return -1
		}
	}
	return 0
}

func rawStackEntriesContainShape(arena *nodeArena, entries []stackEntry) bool {
	for i := range entries {
		if rawStackEntryContainsShape(arena, entries[i], 0) {
			return true
		}
	}
	return false
}

func rawStackEntryContainsShape(arena *nodeArena, entry stackEntry, depth int) bool {
	if depth > maxTreeWalkDepth {
		return false
	}
	if shape, ok := rawShapeForStackEntry(arena, entry); ok {
		if shape.childCount > 0 {
			return true
		}
	}
	childCount := stackEntryNodeChildCount(entry)
	for i := 0; i < childCount; i++ {
		child, ok := rawStackEntryChildAt(arena, entry, i)
		if !ok {
			continue
		}
		if rawStackEntryContainsShape(arena, child, depth+1) {
			return true
		}
	}
	return false
}
