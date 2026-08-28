package parsercorephase0

import "testing"

// B3 stage S5 substrate. MissingLeaf is the compact representation of C's
// ts_subtree_new_missing_leaf (subtree.c:534-546). No shipped parser path
// publishes one yet, so these tests drive the Core API directly.

func newMissingLeafTestCore(t *testing.T) *Core {
	t.Helper()
	compact, err := New(&fakeTable{}, Limits{MaxDerivations: 8, MaxPopPaths: 8})
	if err != nil {
		t.Fatal(err)
	}
	return compact
}

// TestMissingLeafPublishesZeroWidthTerminal pins every field C's construction
// fixes: zero width at the stack position, terminal, missing, and neither
// extra nor external.
func TestMissingLeafPublishesZeroWidthTerminal(t *testing.T) {
	compact := newMissingLeafTestCore(t)
	const symbol = Symbol(7)
	const atByte = uint32(41)

	id, err := compact.MissingLeaf(symbol, atByte)
	if err != nil {
		t.Fatalf("MissingLeaf: %v", err)
	}
	view, err := compact.Subtree(id)
	if err != nil {
		t.Fatalf("Subtree: %v", err)
	}
	if view.Symbol != symbol {
		t.Fatalf("symbol = %d, want %d", view.Symbol, symbol)
	}
	if view.StartByte != atByte || view.EndByte != atByte {
		t.Fatalf("span = [%d,%d), want the zero-width span [%d,%d)",
			view.StartByte, view.EndByte, atByte, atByte)
	}
	if !view.Terminal {
		t.Fatal("a missing leaf must be a terminal record")
	}
	if !view.Missing {
		t.Fatal("a missing leaf must carry the missing bit")
	}
	// C passes false for external tokens and never marks the leaf extra.
	// Marking it extra would additionally make popPaths skip it when counting
	// a production's structural arity, silently changing every enclosing
	// reduce.
	if view.Extra {
		t.Fatal("a missing leaf must not be extra")
	}
	if view.External {
		t.Fatal("a missing leaf must not be external")
	}
	if len(view.Children) != 0 {
		t.Fatalf("a missing leaf must have no children, got %d", len(view.Children))
	}
}

// TestMissingLeafRejectsReservedSymbols proves the two RESERVED symbols that
// can never name a missing token fail closed. It deliberately does not claim
// more: MissingLeaf takes no StateID, so it cannot check that the grammar
// actually demands the token, and an ordinary grammar nonterminal is accepted.
// See MissingLeaf's own doc comment for what the caller still owes.
func TestMissingLeafRejectsReservedSymbols(t *testing.T) {
	for name, symbol := range map[string]Symbol{
		"end-of-file": 0,
		"error":       ErrorRegionSymbol,
	} {
		t.Run(name, func(t *testing.T) {
			compact := newMissingLeafTestCore(t)
			if _, err := compact.MissingLeaf(symbol, 0); err == nil {
				t.Fatalf("MissingLeaf accepted the %s symbol", name)
			}
		})
	}
}

// TestMissingLeafSurfacesThroughMaterializationView proves the bit reaches the
// materializer, which is the consumer that turns it into the public node's own
// missing and has-error bits.
func TestMissingLeafSurfacesThroughMaterializationView(t *testing.T) {
	compact := newMissingLeafTestCore(t)
	missingID, err := compact.MissingLeaf(Symbol(3), 12)
	if err != nil {
		t.Fatalf("MissingLeaf: %v", err)
	}
	ordinaryID, err := compact.ErrorRegionLeaf(Symbol(3), 12, 15, false)
	if err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}

	missingView, err := compact.MaterializationView(missingID)
	if err != nil {
		t.Fatalf("MaterializationView(missing): %v", err)
	}
	if !missingView.Missing {
		t.Fatal("materialization view lost the missing bit")
	}
	ordinaryView, err := compact.MaterializationView(ordinaryID)
	if err != nil {
		t.Fatalf("MaterializationView(ordinary): %v", err)
	}
	if ordinaryView.Missing {
		t.Fatal("an ordinary published terminal reported the missing bit")
	}
}

// TestMissingLeafIsInertSubstrate states the stage boundary as an executable
// claim: publishing a missing leaf is the ONLY way a missing record can enter
// a compact arena today, so every record any other Core operation publishes
// reports Missing false. The scheduler mechanism that will call MissingLeaf is
// B3 stage S5 and does not exist yet.
func TestMissingLeafIsInertSubstrate(t *testing.T) {
	compact := newMissingLeafTestCore(t)
	seed, err := compact.Seed(StateID(1), 0)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if _, err := compact.appendDiagnosticPayload(seed, StateID(1),
		Token{Symbol: 5, StartByte: 0, EndByte: 2}, pathMeta{}); err != nil {
		t.Fatalf("appendDiagnosticPayload: %v", err)
	}
	if _, err := compact.ErrorRegionLeaf(Symbol(6), 2, 4, false); err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}
	for id := SubtreeID(1); uint64(id) <= uint64(len(compact.subtrees)); id++ {
		view, err := compact.Subtree(id)
		if err != nil {
			t.Fatalf("Subtree(%d): %v", id, err)
		}
		if view.Missing {
			t.Fatalf("subtree %d reported the missing bit without a MissingLeaf call", id)
		}
	}
}

// TestMissingLeafSurfacesThroughPostorderVisitor covers the path the driver
// actually materializes through.
//
// TestMissingLeafSurfacesThroughMaterializationView above exercises the
// RANDOM-ACCESS accessor, but public-node construction runs from
// VisitMaterializationPostorder(WithScratch), which builds its view at a
// different site (materialization_postorder_scratch.go). Without this test,
// deleting the Missing field from that construction would leave every other
// test in this file green while silently disabling the feature on the only
// path that ships.
func TestMissingLeafSurfacesThroughPostorderVisitor(t *testing.T) {
	compact := newMissingLeafTestCore(t)
	missingID, err := compact.MissingLeaf(Symbol(3), 12)
	if err != nil {
		t.Fatalf("MissingLeaf: %v", err)
	}
	ordinaryID, err := compact.ErrorRegionLeaf(Symbol(3), 12, 15, false)
	if err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}

	seen := map[SubtreeID]bool{}
	visited := 0
	err = compact.VisitMaterializationPostorder(
		[]SubtreeID{missingID, ordinaryID},
		func() error { return nil },
		func(id SubtreeID, view MaterializationSubtreeView) error {
			seen[id] = view.Missing
			visited++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("VisitMaterializationPostorder: %v", err)
	}
	if visited != 2 {
		t.Fatalf("visited %d subtrees, want 2", visited)
	}
	if !seen[missingID] {
		t.Fatal("the postorder visitor lost the missing bit on the missing leaf")
	}
	if seen[ordinaryID] {
		t.Fatal("the postorder visitor reported an ordinary terminal as missing")
	}
}

// TestMissingLeafIsNotStructurallyEqualToCleanPayload pins the fail-closed
// half of the duplicate-drop gate.
//
// subtreesStructurallyEqual authorizes DROPPING one payload as a duplicate of
// another. A recovery-inserted MISSING leaf and a clean zero-width payload
// with the same symbol and span agree on every other compared field, so
// without the missing bit in that predicate the MISSING record would be
// discarded and its error lost from the published tree.
func TestMissingLeafIsNotStructurallyEqualToCleanPayload(t *testing.T) {
	compact := newMissingLeafTestCore(t)
	missingID, err := compact.MissingLeaf(Symbol(3), 12)
	if err != nil {
		t.Fatalf("MissingLeaf: %v", err)
	}
	// Same symbol, same zero-width span, published the ordinary way.
	cleanID, err := compact.ErrorRegionLeaf(Symbol(3), 12, 12, false)
	if err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}
	equal, err := compact.subtreesStructurallyEqual(missingID, cleanID)
	if err != nil {
		t.Fatalf("subtreesStructurallyEqual: %v", err)
	}
	if equal {
		t.Fatal("a MISSING payload compared structurally equal to a clean zero-width payload; the duplicate-drop gate would discard the error")
	}
	// Control: the predicate still folds two genuinely identical clean payloads.
	otherCleanID, err := compact.ErrorRegionLeaf(Symbol(3), 12, 12, false)
	if err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}
	equal, err = compact.subtreesStructurallyEqual(cleanID, otherCleanID)
	if err != nil {
		t.Fatalf("subtreesStructurallyEqual: %v", err)
	}
	if !equal {
		t.Fatal("two identical clean payloads stopped comparing equal; the missing bit over-separated them")
	}
}

// TestMissingLeafDropCohortReceiptDistinguishesTheBit proves the drop-cohort
// receipt digest and its total-order comparator both authenticate the bit.
// Both already tracked `fragile`, the other late-added record bit, so omitting
// `missing` would have been a missed site rather than a scoping decision: two
// receipts over different parses would hash identically.
func TestMissingLeafDropCohortReceiptDistinguishesTheBit(t *testing.T) {
	compact := newMissingLeafTestCore(t)
	missingID, err := compact.MissingLeaf(Symbol(3), 12)
	if err != nil {
		t.Fatalf("MissingLeaf: %v", err)
	}
	cleanID, err := compact.ErrorRegionLeaf(Symbol(3), 12, 12, false)
	if err != nil {
		t.Fatalf("ErrorRegionLeaf: %v", err)
	}
	order, err := compact.dropCohortCompareSubtree(missingID, cleanID)
	if err != nil {
		t.Fatalf("dropCohortCompareSubtree: %v", err)
	}
	if order == 0 {
		t.Fatal("the drop-cohort comparator ordered a MISSING payload equal to a clean one")
	}
}
