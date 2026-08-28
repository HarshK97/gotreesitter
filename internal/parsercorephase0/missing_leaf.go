package parsercorephase0

import "errors"

// ---------------------------------------------------------------------------
// missing_leaf.go -- campaign v7 tranche B3 stage S5 substrate: the compact
// representation of C's recovery-inserted MISSING terminal.
//
// THE PRODUCTION GO PORT IS THE EXECUTABLE SPECIFICATION for the mechanism
// that will produce these (cHandleError's missing-token search,
// parser_recover_c.go, itself a port of ts_parser__handle_error step 2,
// parser.c:2154-2230). The pinned C oracle is the parity arbiter
// (decision 0007).
//
// NOTHING IN THE SHIPPED PARSER CALLS MissingLeaf TODAY. It is inert
// substrate, landed on its own so the storage and materialization change can
// be reviewed apart from the scheduler mechanism that will use it. The
// admission census reports six real-corpus rows whose first decline point is
// owned by this mechanism, and three of those six already carry a production
// tree that is structurally identical to the locked C oracle, so the
// mechanism has a measured target to hit.
// ---------------------------------------------------------------------------

// MissingLeaf publishes one zero-width recovery-inserted terminal.
//
// C constructs the same object with ts_subtree_new_missing_leaf
// (subtree.c:534-546): a leaf whose size is length_zero() and whose
// is_missing bit is set. Two consequences of that construction are
// reproduced here and must not be "simplified" away:
//
//   - The leaf is ZERO WIDTH. atByte is both its start and its end. C
//     positions it at the stack's current byte offset: ts_parser__handle_error
//     resets the lexer to the stack position and immediately marks the end
//     (parser.c), so the computed padding is zero and the leaf carries no
//     source text at all. A caller that passes a span here is describing
//     something that is not a missing token.
//
//   - The leaf is NOT extra and NOT external. C passes false for external
//     tokens and builds the leaf outside the scanner entirely; it is a
//     synthetic terminal the table demanded, not lexed input. Marking it
//     extra would additionally make popPaths skip it when counting a
//     production's structural arity (see ErrorRegionResume, where extra IS
//     correct and load-bearing for the opposite reason), which would silently
//     change every enclosing reduce.
//
// The record's missing bit is what makes the cost model correct later:
// ts_subtree_error_cost (subtree.h:331-337) short-circuits on it and returns
// ERROR_COST_PER_MISSING_TREE + ERROR_COST_PER_RECOVERY (610), ignoring any
// stored cost. Materialization also reads it to set the public node's own
// missing and has-error bits, matching ts_node_has_error, which C defines as
// error_cost > 0 (node.c:520-522) and which is therefore true on the missing
// node itself, not only on its ancestors.
//
// appendSubtree, not appendSubtreeRecord: like ErrorRegionLeaf, a missing
// terminal is published outside the grammar-table-authenticated shift seam,
// so metadataConstructionAuthenticated must clear and the record earns full
// materialization-time metadata validation rather than skipping it.
func (c *Core) MissingLeaf(symbol Symbol, atByte uint32) (id SubtreeID, err error) {
	if symbol == 0 {
		return 0, errors.New("parser-core phase zero: missing leaf requires a real terminal symbol")
	}
	if symbol == ErrorRegionSymbol {
		return 0, errors.New("parser-core phase zero: missing leaf cannot carry the ERROR symbol")
	}
	mark := c.mark()
	defer c.completeTransaction(mark, &err)
	return c.appendSubtree(subtreeRecord{
		symbol:    symbol,
		startByte: atByte,
		endByte:   atByte,
		terminal:  true,
		missing:   true,
	}, nil, nil, nil)
}
