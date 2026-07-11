package gotreesitter

import (
	"fmt"
	"os"
	"sync/atomic"
)

// stripResultTreeSelfCycles enforces that no node is its own descendant.
//
// ROOT CAUSE (now fixed at construction time): trailing-EOF error recovery
// clones the GSS (sharing node payload pointers), re-reduces, and wraps a
// dropped stack payload in an ERROR node. On a fresh parse that payload can be a
// TRANSIENT reduce parent whose .parent field doubles as the result-time
// materializer's {nil: unvisited, self: in-progress, other: arena clone}
// sentinel. Wrapping it with the eager-link-wiring constructor corrupted that
// sentinel, so materializeTransientParentNodes then substituted the child's tree
// parent for the child and linked the ERROR wrapper under itself. The raw
// .children slice looked fine while the materialized child view was cyclic, so
// every later full-tree walk (parent-link wiring, the compat normalizers,
// recovery trimming) recursed or looped without end: a fatal stack overflow or
// an indefinite hang (issue #110; the go zerrors_windows.go truncation hang).
//
// The construction hazard is now fixed: appendTrailingEOFRecoveryNodes (parser.go)
// routes through newRecoveryParentNodeInArena (parser_recover_c.go), which skips
// eager wiring while transient parents are active. The GLR-forest fragment-root
// sibling site (glr_forest.go collectForestErrorRoot) is proven safe because the
// forest parse never allocates transient parents. See
// parser_recover_cycle_internal_test.go for the pins.
//
// This pass remains as DEFENSE IN DEPTH. It walks the tree once with an explicit
// stack and a visited set (so it terminates even on an already-cyclic graph),
// materializes each node's children (clearing the arena refs so .children
// becomes authoritative), and drops any child edge that points at the node
// itself or an ancestor on the current path. It runs only on recovered node
// trees, so normal parses are untouched. Every dropped edge is now COUNTED (and,
// under GOT_DEBUG_RECOVERY_CYCLES=1, loudly reported) via
// reportResultTreeSelfCycleStripped: with the root cause fixed the count must
// stay zero, so any future construction bug that corrupts the sentinel again
// surfaces here instead of being silently masked.
func stripResultTreeSelfCycles(root *Node) {
	if root == nil {
		return
	}
	type frame struct {
		n   *Node
		idx int
	}
	black := make(map[*Node]struct{})
	onPath := map[*Node]struct{}{root: {}}
	stack := []frame{{root, 0}}

	for len(stack) > 0 {
		i := len(stack) - 1
		n := stack[i].n
		children := nodeChildrenForReason(n, materializeForNormalization)

		descended := false
		for stack[i].idx < len(children) {
			c := children[stack[i].idx]
			if c == nil {
				stack[i].idx++
				continue
			}
			if _, ancestor := onPath[c]; ancestor {
				reportResultTreeSelfCycleStripped(n, c)
				children = removeResultTreeChildAt(n, children, stack[i].idx)
				continue
			}
			if _, done := black[c]; done {
				stack[i].idx++
				continue
			}
			stack[i].idx++
			onPath[c] = struct{}{}
			stack = append(stack, frame{c, 0})
			descended = true
			break
		}
		if descended {
			continue
		}
		delete(onPath, n)
		black[n] = struct{}{}
		stack = stack[:len(stack)-1]
	}
}

// debugResultTreeSelfCyclesStripped counts back-edges removed by
// stripResultTreeSelfCycles across the process. With the sentinel-corruption
// root cause fixed at construction time it must stay zero; a nonzero value is
// proof that a recovery construction site is corrupting the transient-parent
// sentinel again (defense-in-depth kept the tree well-formed, but the bug is
// real and must be traced, not masked).
//
// Parses can run concurrently (multiple *Parser instances across goroutines),
// so every access goes through sync/atomic: increments use atomic.AddUint64
// and readers (tests, diagnostics) must use atomic.LoadUint64 rather than a
// plain read.
var debugResultTreeSelfCyclesStripped uint64

// reportResultTreeSelfCycleStripped records that the #110 defense-in-depth guard
// had to drop a self/ancestor back-edge. It always bumps the observable counter;
// under GOT_DEBUG_RECOVERY_CYCLES=1 it also emits a loud, budgeted stderr line so
// an env-gated recovery sweep proves the count is zero (or pinpoints the
// offending node span when it is not). Distinct tag from the construction-time
// detector (RECOVERY-CYCLE) so the reporting stage is unambiguous.
func reportResultTreeSelfCycleStripped(parent, child *Node) {
	atomic.AddUint64(&debugResultTreeSelfCyclesStripped, 1)
	if !debugRecoveryCycleChecks || debugRecoveryCycleReportsLeft <= 0 {
		return
	}
	debugRecoveryCycleReportsLeft--
	var psym, csym Symbol
	var ps, pe, cs, ce uint32
	if parent != nil {
		psym, ps, pe = parent.symbol, parent.startByte, parent.endByte
	}
	if child != nil {
		csym, cs, ce = child.symbol, child.startByte, child.endByte
	}
	fmt.Fprintf(os.Stderr,
		"RESULT-TREE-SELF-CYCLE-STRIP #110 guard dropped back-edge: parent sym=%d [%d-%d] -> descendant/self sym=%d [%d-%d] "+
			"(transient-parent sentinel corruption regressed at a recovery construction site)\n",
		psym, ps, pe, csym, cs, ce)
}

func removeResultTreeChildAt(n *Node, children []*Node, idx int) []*Node {
	oldLen := len(children)
	n.children = append(children[:idx], children[idx+1:]...)
	fieldIDs := n.fieldIDs()
	fieldSources := n.fieldSources()
	metadataChanged := false
	if len(fieldIDs) == oldLen {
		fieldIDs = append(fieldIDs[:idx], fieldIDs[idx+1:]...)
		metadataChanged = true
	}
	if len(fieldSources) == oldLen {
		fieldSources = append(fieldSources[:idx], fieldSources[idx+1:]...)
		metadataChanged = true
	}
	if metadataChanged {
		n.setFieldMetadata(fieldIDs, fieldSources)
	}
	return n.children
}
