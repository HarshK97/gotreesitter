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

// TestMissingLeafRejectsNonTerminalSymbols proves the two symbols that can
// never name a missing token fail closed rather than publishing a record the
// cost model would then read as a missing subtree.
func TestMissingLeafRejectsNonTerminalSymbols(t *testing.T) {
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
