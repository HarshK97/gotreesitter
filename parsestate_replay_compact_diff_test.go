//go:build gts_parsercorephase0

package gotreesitter

import (
	"fmt"
	"sort"
	"testing"
)

// TestParseStateReplayCompactDifferential is the go/no-go exactness proof for
// Phase-3 Lane 2. For each canonical Go fixture it builds the tree TWICE --
// once through production (which records parseState/preGotoState per node) and
// once through the compact route with table-replay materialization -- and
// compares the two states on every node in lockstep. The compact and
// production trees are already proven structurally byte-identical (deep-tree
// digest), so any state divergence is a real replay-model gap.
//
// This is the artifact the visible-tree differential cannot be: replay runs
// over the FULL derivation (real grammar symbols + hidden nodes), so aliasing
// and hidden-node elision -- the two walls the visible tree hit -- are resolved
// before the states are stamped.
func TestParseStateReplayCompactDifferential(t *testing.T) {
	setParserCoreReplayParseStatesForTest(true)
	defer setParserCoreReplayParseStatesForTest(false)

	runner, err := newParserCoreFreshFullRunner(parserCoreWarmGoScanner, parserCoreFreshFullCanonicalOptions())
	if err != nil {
		t.Fatal(err)
	}
	lang := runner.lang

	type classStat struct{ total, mismatch, preMismatch, psMismatch int }
	corpusClasses := map[string]*classStat{}
	var corpusNodes, corpusMismatch int

	for _, row := range diagnosticParserCoreCanonicalAdmissions {
		row := row
		t.Run(row.id, func(t *testing.T) {
			fixture := loadDiagnosticParserCoreCanonicalFixture(t, row.id)
			requireDiagnosticParserCoreCanonicalFixtureIdentity(t, fixture, row)

			compactTree, err := runner.parse(fixture.Source)
			if err != nil {
				t.Fatalf("compact parse %s: %v", row.id, err)
			}
			defer compactTree.Release()

			production, err := NewParser(lang).Parse(fixture.Source)
			if err != nil {
				t.Fatalf("production parse %s: %v", row.id, err)
			}
			defer production.Release()

			// Structural identity guard: if this fails the lockstep walk is
			// meaningless, so assert it up front.
			parserCoreWarmRequireDeepEqual(t, compactTree, production, lang)

			classes := map[string]*classStat{}
			var nodes, mismatch, preMismatch, psMismatch int
			var samples []string

			type pair struct{ compact, prod *Node }
			stack := []pair{{compact: compactTree.root, prod: production.root}}
			for len(stack) != 0 {
				last := len(stack) - 1
				cur := stack[last]
				stack = stack[:last]
				a, b := cur.compact, cur.prod
				if a == nil || b == nil {
					continue
				}
				nodes++
				class := replayCompactNodeClass(b)
				st := classes[class]
				if st == nil {
					st = &classStat{}
					classes[class] = st
				}
				st.total++
				preBad := a.preGotoState != b.preGotoState
				psBad := a.parseState != b.parseState
				isTerminal := len(b.children) == 0
				// Invariant 1: parseState is EXACTLY table-derivable for every
				// terminal (shift reconstruction is exact). A terminal
				// parseState mismatch would break the go/no-go claim.
				if psBad && isTerminal {
					t.Errorf("%s: terminal parseState not table-derivable: %s %d..%d compact=%d prod=%d",
						row.id, b.Type(lang), b.StartByte(), b.EndByte(), a.parseState, b.parseState)
				}
				// Invariant 2: preGotoState is EXACTLY reconstructable for every
				// NON-extra node. Extras float to a tree position that does not
				// match their live-parse lex-time stack state, so only their
				// preGotoState may diverge (their parseState stays exact).
				if preBad && !b.isExtra() {
					t.Errorf("%s: non-extra preGotoState not reconstructable: %s %d..%d compact=%d prod=%d",
						row.id, b.Type(lang), b.StartByte(), b.EndByte(), a.preGotoState, b.preGotoState)
				}
				if preBad || psBad {
					mismatch++
					st.mismatch++
					if preBad {
						preMismatch++
						st.preMismatch++
					}
					if psBad {
						psMismatch++
						st.psMismatch++
					}
					if len(samples) < 20 {
						samples = append(samples, fmt.Sprintf(
							"%s (%s) %d..%d pre compact=%d prod=%d ps compact=%d prod=%d",
							b.Type(lang), class, b.StartByte(), b.EndByte(),
							a.preGotoState, b.preGotoState, a.parseState, b.parseState))
					}
				}
				for i := a.ChildCount() - 1; i >= 0; i-- {
					stack = append(stack, pair{compact: a.Child(i), prod: b.Child(i)})
				}
			}

			pct := 100.0
			if nodes > 0 {
				pct = 100.0 * float64(nodes-mismatch) / float64(nodes)
			}
			t.Logf("%s: nodes=%d exact=%d (%.4f%%) mismatched=%d (preGoto=%d parseState=%d)",
				row.id, nodes, nodes-mismatch, pct, mismatch, preMismatch, psMismatch)
			for _, class := range sortedClassKeys(classes) {
				st := classes[class]
				if st.mismatch == 0 {
					continue
				}
				t.Logf("  class %-26s total=%-6d mismatched=%-6d (pre=%d ps=%d)",
					class, st.total, st.mismatch, st.preMismatch, st.psMismatch)
			}
			for i, s := range samples {
				t.Logf("  sample[%d] %s", i, s)
			}

			corpusNodes += nodes
			corpusMismatch += mismatch
			for class, st := range classes {
				agg := corpusClasses[class]
				if agg == nil {
					agg = &classStat{}
					corpusClasses[class] = agg
				}
				agg.total += st.total
				agg.mismatch += st.mismatch
				agg.preMismatch += st.preMismatch
				agg.psMismatch += st.psMismatch
			}
		})
	}

	pct := 100.0
	if corpusNodes > 0 {
		pct = 100.0 * float64(corpusNodes-corpusMismatch) / float64(corpusNodes)
	}
	t.Logf("COMPACT-REPLAY CORPUS: nodes=%d exact=%d (%.6f%%) mismatched=%d", corpusNodes, corpusNodes-corpusMismatch, pct, corpusMismatch)
	for _, class := range sortedClassKeys(corpusClasses) {
		st := corpusClasses[class]
		if st.mismatch == 0 {
			continue
		}
		t.Logf("  %-28s total=%-7d mismatched=%-7d (pre=%d ps=%d)", class, st.total, st.mismatch, st.preMismatch, st.psMismatch)
	}

	// Corpus-level go/no-go floor. Per-node invariants above already fail the
	// test on any terminal parseState or non-extra preGoto divergence; this
	// bounds the two documented residual classes so a regression that widens
	// them is caught:
	//   - preGoto residual == extra (comment) leaves only;
	//   - parseState residual == the collapse-vs-lookahead divergence, which is
	//     confined to internal nodes in grammargen_lr today.
	if corpusNodes == 0 {
		t.Fatal("compact-replay differential visited no nodes")
	}
	if pct < 99.0 {
		t.Fatalf("compact-replay corpus exactness regressed below 99%%: %.4f%% (mismatched=%d)", pct, corpusMismatch)
	}
}

func replayCompactNodeClass(n *Node) string {
	kind := "internal"
	if len(n.children) == 0 {
		kind = "leaf"
	}
	var mods string
	if n.symbol == errorSymbol {
		mods += "+error"
	}
	if n.isMissing() {
		mods += "+missing"
	}
	if n.isExtra() {
		mods += "+extra"
	}
	if n.isExternalScannerToken() {
		mods += "+extscan"
	}
	if n.hasError() && n.symbol != errorSymbol {
		mods += "+haserr"
	}
	return kind + mods
}

func sortedClassKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
