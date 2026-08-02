package parsercorephase0

import (
	"errors"
	"fmt"
)

// ---------------------------------------------------------------------------
// recovery_cost.go -- campaign v7 tranche B3 stage S2: an inert error-cost
// model over immutable compact subtree records.
//
// THE PRODUCTION GO PORT IS THE EXECUTABLE SPECIFICATION for this file
// (spec.compact-recovery-ownership.v1, section 6): parser_recover_c.go's
// cNodeErrorCostLang, cSymbolVisibleLang, and cVersionStatus, themselves
// faithful ports of tree-sitter C's ts_subtree_error_cost /
// ts_subtree_summarize_children (subtree.c) and
// ts_parser__version_status / ts_parser__compare_versions (parser.c). Every
// constant, branch, and evaluation order below mirrors those functions
// exactly; a divergence gets a receipt, not a silent "improvement"
// (decision 0007, hypha://m31labs/gotreesitter).
//
// Nothing in this package calls into this file. It is a pure, on-demand
// library: no Core method, no scheduler path, and no driver file references
// any declaration here. TestRecoveryCostNoOutsideCallSitesRatchet enforces
// this as a compile-time property, in the style of
// condense_outcome_provenance_ratchet_test.go. Stage S3 wires the ERROR-
// region records this model reads over; stage S4 wires the recovery
// election that reads RecoveryErrorStatus/RecoveryCompareVersions. Until
// then this file is dead code by construction (gates G2/G3, design section
// 8) -- reachable from nowhere except its own tests.
//
// Record shape note: RecoveryCostNode is a strict superset of the existing
// SubtreeView (core.go): it adds Missing and two row fields (StartRow,
// EndRow) that no compact subtree needs to populate today, because nothing
// in the shipped compact core has ever produced a MISSING node or an ERROR
// region -- compact has no error recovery yet. cNodeErrorCostLang only ever
// reads a node's Missing flag or row span when that node either is itself
// missing or is an ERROR node (parser_recover_c.go:1238,1249-1268); an
// ordinary clean node's Missing is always false and its row span is never
// read. So every real SubtreeView-backed RecoveryCostNode in service today
// leaves Missing=false and StartRow==EndRow==0, and this cost model still
// returns exactly 0 for it (see the parity corpus test in
// recovery_cost_test.go and the root-package differential test). The two
// added fields exist for the paused-header MISSING/ERROR records stage
// S3/S5 construct (design section 4); this stage defines the shape those
// stages populate, and does not approximate production *Node state -- it
// defines a compact record able to carry all of it.
// ---------------------------------------------------------------------------

// Cost weights: a literal port of parser_recover_c.go:52-58's error_costs.h
// constants (ERROR_COST_PER_RECOVERY=500, ERROR_COST_PER_MISSING_TREE=110,
// ERROR_COST_PER_SKIPPED_TREE=100, ERROR_COST_PER_SKIPPED_LINE=30,
// ERROR_COST_PER_SKIPPED_CHAR=1). Keep the production warning attached: an
// older internal doc said the skipped-line cost was 2; the C header (and
// this port) is 30.
const (
	RecoveryCostPerRecovery    = 500
	RecoveryCostPerMissingTree = 110
	RecoveryCostPerSkippedTree = 100
	RecoveryCostPerSkippedLine = 30
	RecoveryCostPerSkippedChar = 1
)

// recoveryMaxCostDifference mirrors cRecoverMaxCostDifference
// (parser_recover_c.go:62-69): 18*RecoveryCostPerSkippedTree, matching
// tree-sitter v0.25.0 (parser.c:83, the release the pinned oracle links).
// Older tree-sitter releases used 16*ERROR_COST_PER_SKIPPED_TREE; do not
// "correct" this back to 16 -- this port tracks the pinned oracle version.
const recoveryMaxCostDifference = 18 * RecoveryCostPerSkippedTree

// RecoveryErrorSymbol is tree-sitter's well-known builtin ERROR symbol id
// (ts_builtin_sym_error). It is fixed across every grammar, not a
// per-language table value, matching the root package's own errorSymbol
// constant (parser_api.go:997).
const RecoveryErrorSymbol Symbol = 65535

// RecoveryCostNode is the immutable, per-subtree view the cost model reads.
// See the file doc comment above for why it carries two fields SubtreeView
// does not.
type RecoveryCostNode struct {
	Symbol    Symbol
	Extra     bool
	Missing   bool
	StartByte uint32
	EndByte   uint32
	StartRow  uint32
	EndRow    uint32
	Children  []SubtreeID
}

// RecoveryCostSource resolves a SubtreeID to its cost-model view. Compact
// subtree records are structurally immutable once published (core.go's
// subtreeRecord doc comment); an implementation must return an equal
// RecoveryCostNode for the same id for the lifetime of one parse. id is
// never 0 (SubtreeIDs are one-based, core.go): callers in this file never
// invoke RecoveryCostNode with id == 0, mirroring cNodeErrorCostLang's
// `if n == nil { return 0 }` base case.
type RecoveryCostSource interface {
	RecoveryCostNode(id SubtreeID) (RecoveryCostNode, error)
}

// ErrRecoveryCostNodeMissing reports that a RecoveryCostSource has no record
// for a referenced SubtreeID -- a malformed or foreign handle, never
// expected from a well-formed compact tree.
var ErrRecoveryCostNodeMissing = errors.New("parser-core phase zero: recovery cost source has no record for subtree id")

// RecoverySymbolVisible is the compact equivalent of cSymbolVisibleLang
// (parser_recover_c.go:1140-1148). symbols is the compact analogue of
// Language.SymbolMetadata: SelectedStorePolicy.Symbols, sourced from the
// identical Language.SymbolMetadata[...].Visible signal
// (parsercore_phase0_driver.go:486-489), not an approximation of it.
func RecoverySymbolVisible(symbols []SelectedSymbolPolicy, sym Symbol) bool {
	if sym == RecoveryErrorSymbol {
		return true
	}
	if int(sym) >= len(symbols) {
		return false
	}
	return symbols[sym].Visible
}

// recoveryVisibleChildCount is the compact equivalent of
// cNodeVisibleChildCountLang (parser_recover_c.go:1160-1176): direct
// children that are visible, plus the visible children of invisible
// internal children.
func recoveryVisibleChildCount(symbols []SelectedSymbolPolicy, src RecoveryCostSource, id SubtreeID) (int, error) {
	if id == 0 {
		return 0, nil
	}
	node, err := src.RecoveryCostNode(id)
	if err != nil {
		return 0, fmt.Errorf("parser-core phase zero: recovery cost node %d: %w", id, err)
	}
	count := 0
	for _, childID := range node.Children {
		if childID == 0 {
			continue
		}
		child, err := src.RecoveryCostNode(childID)
		if err != nil {
			return 0, fmt.Errorf("parser-core phase zero: recovery cost node %d: %w", childID, err)
		}
		if RecoverySymbolVisible(symbols, child.Symbol) {
			count++
		} else if len(child.Children) > 0 {
			sub, err := recoveryVisibleChildCount(symbols, src, childID)
			if err != nil {
				return 0, err
			}
			count += sub
		}
	}
	return count, nil
}

// RecoveryCostMemo memoizes RecoveryNodeErrorCostMemo results per SubtreeID.
// Unlike production's cNodeMemoCacheEntry (parser_recover_c.go:1324-1349),
// which pointer-hashes into a 2-way set-associative cache and carries an
// equivVersion stamp because a live *Node mutates in place under GLR
// forking, a published compact SubtreeID never changes once created -- the
// open error region is the only mutable recovery state, and it lives on the
// header, not the arena (design section 4, stage S2 scope). A memoized cost
// is therefore valid for the rest of the parse with no version check, and
// SubtreeID's dense, one-based arena numbering (core.go) lets the memo be a
// flat, growable slice instead of a hash structure -- exact, no eviction, no
// collision handling.
//
// The zero value is ready to use.
type RecoveryCostMemo struct {
	cost []uint32
	has  []bool
}

func (m *RecoveryCostMemo) lookup(id SubtreeID) (uint32, bool) {
	if m == nil {
		return 0, false
	}
	idx := int(id)
	if idx <= 0 || idx > len(m.has) || !m.has[idx-1] {
		return 0, false
	}
	return m.cost[idx-1], true
}

func (m *RecoveryCostMemo) store(id SubtreeID, cost uint32) {
	if m == nil {
		return
	}
	idx := int(id)
	if idx <= 0 {
		return
	}
	if idx > len(m.has) {
		grownHas := make([]bool, idx)
		copy(grownHas, m.has)
		m.has = grownHas
		grownCost := make([]uint32, idx)
		copy(grownCost, m.cost)
		m.cost = grownCost
	}
	m.cost[idx-1] = cost
	m.has[idx-1] = true
}

// Reset clears every memoized entry while retaining capacity, so one
// RecoveryCostMemo can be reused across parses without reallocating,
// mirroring Core.Reset's retained-capacity discipline (core.go).
func (m *RecoveryCostMemo) Reset() {
	if m == nil {
		return
	}
	for i := range m.has {
		m.has[i] = false
	}
}

// Len reports the memo's current slot capacity (test/diagnostic use only).
func (m *RecoveryCostMemo) Len() int {
	if m == nil {
		return 0
	}
	return len(m.has)
}

// RecoveryNodeErrorCost is the compact equivalent of cNodeErrorCostLang
// (parser_recover_c.go:1216-1271), unmemoized.
func RecoveryNodeErrorCost(symbols []SelectedSymbolPolicy, src RecoveryCostSource, id SubtreeID) (uint32, error) {
	return recoveryNodeErrorCost(symbols, src, nil, id)
}

// RecoveryNodeErrorCostMemo is RecoveryNodeErrorCost with memoization,
// mirroring cNodeErrorCostLangWithScratch (parser_recover_c.go:1273-1322). A
// nil memo falls back to the unmemoized walk, matching the production
// function's `if scratch == nil` fallback.
func RecoveryNodeErrorCostMemo(symbols []SelectedSymbolPolicy, src RecoveryCostSource, memo *RecoveryCostMemo, id SubtreeID) (uint32, error) {
	return recoveryNodeErrorCost(symbols, src, memo, id)
}

func recoveryNodeErrorCost(symbols []SelectedSymbolPolicy, src RecoveryCostSource, memo *RecoveryCostMemo, id SubtreeID) (uint32, error) {
	if id == 0 {
		return 0, nil
	}
	if memo != nil {
		if cost, ok := memo.lookup(id); ok {
			return cost, nil
		}
	}
	node, err := src.RecoveryCostNode(id)
	if err != nil {
		return 0, fmt.Errorf("parser-core phase zero: recovery cost node %d: %w", id, err)
	}
	if node.Missing && len(node.Children) == 0 {
		cost := uint32(RecoveryCostPerMissingTree + RecoveryCostPerRecovery)
		if memo != nil {
			memo.store(id, cost)
		}
		return cost, nil
	}
	var cost uint32
	for _, childID := range node.Children {
		if childID == 0 {
			continue
		}
		child, err := src.RecoveryCostNode(childID)
		if err != nil {
			return 0, fmt.Errorf("parser-core phase zero: recovery cost node %d: %w", childID, err)
		}
		if child.Symbol == RecoveryErrorSymbol && len(child.Children) == 0 {
			// Compact mirror of the C ERROR-leaf rule: subtree error_cost is
			// 0 for a childless ERROR node (parser_recover_c.go:1243-1246).
			continue
		}
		childCost, err := recoveryNodeErrorCost(symbols, src, memo, childID)
		if err != nil {
			return 0, err
		}
		cost += childCost
	}
	if node.Symbol == RecoveryErrorSymbol {
		for _, childID := range node.Children {
			if childID == 0 {
				continue
			}
			child, err := src.RecoveryCostNode(childID)
			if err != nil {
				return 0, fmt.Errorf("parser-core phase zero: recovery cost node %d: %w", childID, err)
			}
			if child.Extra {
				continue
			}
			if RecoverySymbolVisible(symbols, child.Symbol) {
				cost += RecoveryCostPerSkippedTree
			} else if len(child.Children) > 0 {
				vis, err := recoveryVisibleChildCount(symbols, src, childID)
				if err != nil {
					return 0, err
				}
				cost += RecoveryCostPerSkippedTree * uint32(vis)
			}
		}
		spanBytes := uint32(0)
		if node.EndByte > node.StartByte {
			spanBytes = node.EndByte - node.StartByte
		}
		spanRows := uint32(0)
		if node.EndRow > node.StartRow {
			spanRows = node.EndRow - node.StartRow
		}
		cost += RecoveryCostPerRecovery + RecoveryCostPerSkippedChar*spanBytes + RecoveryCostPerSkippedLine*spanRows
	}
	if memo != nil {
		memo.store(id, cost)
	}
	return cost, nil
}

// RecoveryErrorStatus is the compact equivalent of parser_recover_c.go's
// cErrorStatus (:2172-2177): the four values ts_parser__compare_versions
// competes on.
type RecoveryErrorStatus struct {
	Cost      uint32
	NodeCount int
	DynPrec   int
	IsInError bool
}

// RecoveryVersionStatus is the compact equivalent of cVersionStatus
// (parser_recover_c.go:2189-2201). Stage S2 owns no header type: the paused
// flag, the node-count-since-error baseline, dynamic precedence, and
// open-recovery existence all live on the paused header stage S3
// introduces ("The open error region is the only mutable piece and lives
// on the header, not in the arena", design section 4), so this function
// takes them as explicit parameters instead of reading them off a header
// that does not exist yet. subtreeCost is the caller-supplied result of
// RecoveryNodeErrorCost(Memo) over the head's accumulated subtrees -- the
// compact analogue of cStackErrorCost(s).
//
// cNodeVisibleSubtreeCount and cNodeCountSinceError
// (parser_recover_c.go:1178-1214,2155-2166) are deliberately not ported
// here: they accumulate a visible-node count across a live GLR stack since
// an error baseline, which is per-head bookkeeping, not a property of one
// compact subtree. That machinery belongs to the stage that owns paused
// headers (S3/S4), not this inert stage; NodeCount stays an explicit input
// for the same reason.
func RecoveryVersionStatus(subtreeCost uint32, paused bool, nodeCount int, dynPrec int, hasOpenRecovery bool) RecoveryErrorStatus {
	cost := subtreeCost
	if paused {
		cost += RecoveryCostPerSkippedTree
	}
	return RecoveryErrorStatus{
		Cost:      cost,
		NodeCount: nodeCount,
		DynPrec:   dynPrec,
		IsInError: paused || hasOpenRecovery,
	}
}

// RecoveryComparison is the compact equivalent of cErrorComparison
// (parser_recover_c.go:2179-2187).
type RecoveryComparison int

const (
	RecoveryComparisonTakeLeft RecoveryComparison = iota
	RecoveryComparisonPreferLeft
	RecoveryComparisonNone
	RecoveryComparisonPreferRight
	RecoveryComparisonTakeRight
)

// RecoveryCompareVersions is a literal port of cCompareVersions
// (parser_recover_c.go:2264-2297), itself a literal port of
// ts_parser__compare_versions. This function carries exact-parity
// obligation 3 (design section 6): in-error class first, then cost with the
// node-count-scaled max-cost-difference band, then dynamic precedence.
func RecoveryCompareVersions(a, b RecoveryErrorStatus) RecoveryComparison {
	if !a.IsInError && b.IsInError {
		if a.Cost < b.Cost {
			return RecoveryComparisonTakeLeft
		}
		return RecoveryComparisonPreferLeft
	}
	if a.IsInError && !b.IsInError {
		if b.Cost < a.Cost {
			return RecoveryComparisonTakeRight
		}
		return RecoveryComparisonPreferRight
	}
	if a.Cost < b.Cost {
		if (b.Cost-a.Cost)*uint32(1+a.NodeCount) > recoveryMaxCostDifference {
			return RecoveryComparisonTakeLeft
		}
		return RecoveryComparisonPreferLeft
	}
	if b.Cost < a.Cost {
		if (a.Cost-b.Cost)*uint32(1+b.NodeCount) > recoveryMaxCostDifference {
			return RecoveryComparisonTakeRight
		}
		return RecoveryComparisonPreferRight
	}
	if a.DynPrec > b.DynPrec {
		return RecoveryComparisonPreferLeft
	}
	if b.DynPrec > a.DynPrec {
		return RecoveryComparisonPreferRight
	}
	return RecoveryComparisonNone
}
