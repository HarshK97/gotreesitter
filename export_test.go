package gotreesitter

import (
	"fmt"
	"sort"
)

// This file exposes unexported ParseState-by-table-replay machinery to the
// external gotreesitter_test package so the differential corpus test
// (parsestate_replay_diff_test.go) can compare replay-reconstructed states
// against production-recorded states with full access to node internals.

// ReplayMismatchSample is a single node whose reconstructed state disagreed
// with the production-recorded state.
type ReplayMismatchSample struct {
	Symbol         Symbol
	SymbolName     string
	Class          string
	PreGotoRec     StateID
	PreGotoReplay  StateID
	ParseStateRec  StateID
	ParseStateRep  StateID
	PreGotoDiff    bool
	ParseStateDiff bool
	Depth          int
}

// ReplayClassStat aggregates match/mismatch counts for one node class.
type ReplayClassStat struct {
	Class      string
	Total      int
	Mismatched int
}

// ReplayDiffReport is the result of comparing replay-reconstructed
// (preGotoState, parseState) against production-recorded values over one tree.
type ReplayDiffReport struct {
	TotalNodes          int
	Mismatched          int
	PreGotoMismatched   int
	ParseStateMismatch  int
	RootPreGotoRecorded StateID
	RootPreGotoSeed     StateID
	Classes             map[string]*ReplayClassStat
	Samples             []ReplayMismatchSample
}

// SetTypeScriptMergeWidthDetectorsDisabledForTests toggles every TypeScript
// source-text merge-width detector (typed-arrow, bare-default-parameter, and
// destructured-arrow-return) at once and returns a restore function. The
// external test package uses it to prove that the structure-aware cap-two
// steady state subsumes those per-shape detectors: with the detectors off, the
// detector-era regression families must still parse cleanly.
func SetTypeScriptMergeWidthDetectorsDisabledForTests(disabled bool) func() {
	prev := typeScriptMergeWidthDetectorsDisabled
	typeScriptMergeWidthDetectorsDisabled = disabled
	return func() { typeScriptMergeWidthDetectorsDisabled = prev }
}

// SetTypeScriptCapOneStructurePreferenceForTests selects variant B for
// head-to-head evaluation against the shipping cap-two policy: it keeps the
// TypeScript full-parse cap at one and prefers the structurally-richer fork
// before score at the cap-one discard site. It returns a restore function and
// is never set in production.
func SetTypeScriptCapOneStructurePreferenceForTests(enabled bool) func() {
	prev := typeScriptCapOneStructurePreference
	typeScriptCapOneStructurePreference = enabled
	return func() { typeScriptCapOneStructurePreference = prev }
}

// ReplayDiffTree replays the LR tables over the tree rooted at root and
// compares the reconstructed states against the recorded ones on every node.
// It mutates nothing. maxSamples bounds the retained mismatch examples.
func ReplayDiffTree(p *Parser, root *Node, maxSamples int) ReplayDiffReport {
	rep := ReplayDiffReport{
		Classes: map[string]*ReplayClassStat{},
	}
	if p == nil || root == nil {
		return rep
	}
	rep.RootPreGotoRecorded = root.preGotoState
	rep.RootPreGotoSeed = p.replayRootPreGotoState()

	// depth tracking: recompute via the same worklist shape is awkward, so
	// track depth by walking parents lazily only for samples.
	var scratch []replayFrame
	p.replayParseStates(root, rep.RootPreGotoSeed, func(n *Node, preGoto, parseState StateID) {
		rep.TotalNodes++
		class := p.replayNodeClass(n)
		st := rep.Classes[class]
		if st == nil {
			st = &ReplayClassStat{Class: class}
			rep.Classes[class] = st
		}
		st.Total++

		preDiff := preGoto != n.preGotoState
		psDiff := parseState != n.parseState
		if !preDiff && !psDiff {
			return
		}
		rep.Mismatched++
		st.Mismatched++
		if preDiff {
			rep.PreGotoMismatched++
		}
		if psDiff {
			rep.ParseStateMismatch++
		}
		if len(rep.Samples) < maxSamples {
			rep.Samples = append(rep.Samples, ReplayMismatchSample{
				Symbol:         n.symbol,
				SymbolName:     p.symbolNameForReport(n.symbol),
				Class:          class,
				PreGotoRec:     n.preGotoState,
				PreGotoReplay:  preGoto,
				ParseStateRec:  n.parseState,
				ParseStateRep:  parseState,
				PreGotoDiff:    preDiff,
				ParseStateDiff: psDiff,
				Depth:          nodeDepth(n),
			})
		}
	}, &scratch)
	return rep
}

// SortedClassStats returns the per-class stats in a stable, mismatch-first order
// for reporting.
func (r ReplayDiffReport) SortedClassStats() []ReplayClassStat {
	out := make([]ReplayClassStat, 0, len(r.Classes))
	for _, st := range r.Classes {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mismatched != out[j].Mismatched {
			return out[i].Mismatched > out[j].Mismatched
		}
		return out[i].Class < out[j].Class
	})
	return out
}

// replayNodeClass produces a stable multi-dimensional class key for a node,
// used to localize which node classes are/are not replay-derivable.
func (p *Parser) replayNodeClass(n *Node) string {
	kind := "internal"
	if p.replaySymbolIsTerminal(n.symbol) {
		kind = "terminal"
	}
	if len(n.children) == 0 && kind == "internal" {
		kind = "empty-nonterminal"
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
	if mods == "" {
		return kind
	}
	return kind + mods
}

// symbolNameForReport returns a human-readable symbol name for reporting.
func (p *Parser) symbolNameForReport(sym Symbol) string {
	if sym == errorSymbol {
		return "ERROR"
	}
	if p == nil || p.language == nil {
		return fmt.Sprintf("sym#%d", sym)
	}
	if int(sym) < len(p.language.SymbolNames) {
		if name := p.language.SymbolNames[sym]; name != "" {
			return name
		}
	}
	return fmt.Sprintf("sym#%d", sym)
}

func nodeDepth(n *Node) int {
	d := 0
	for n != nil && n.parent != nil {
		d++
		n = n.parent
	}
	return d
}

// ReplayResyncReport isolates the two error sources in visible-tree replay:
// (1) whether advance() reproduces parseState GIVEN the recorded preGotoState
// (tests the goto/shift model in isolation), and (2) whether the first child's
// preGotoState equals its parent's recorded preGotoState (tests the straight
// line intra-production chain). preGoto threading across hidden-node boundaries
// is deliberately NOT threaded here: each node is seeded from its own recorded
// preGotoState.
type ReplayResyncReport struct {
	TotalNodes             int
	ParseStateExact        int // advance(recordedPre, n) == recordedParseState
	ParseStateExactByClass map[string]*ReplayClassStat
	FirstChildPreExact     int // child0.recordedPre == parent.recordedPre
	FirstChildTotal        int
	SiblingChainExact      int // child[i].recordedPre == advance(child[i-1].recordedPre, child[i-1])
	SiblingChainTotal      int
}

// ReplayDiffResync walks the tree and, for every node, checks whether
// advance(recordedPreGoto, node) reproduces the recorded parseState, plus the
// two structural chain invariants. It reveals that parseState IS exactly a
// table transition of the (correct) preGotoState, and that the ONLY thing the
// visible tree cannot reconstruct is preGotoState across hidden-node edges.
func ReplayDiffResync(p *Parser, root *Node) ReplayResyncReport {
	rep := ReplayResyncReport{ParseStateExactByClass: map[string]*ReplayClassStat{}}
	if p == nil || root == nil {
		return rep
	}
	var walk func(n *Node)
	walk = func(n *Node) {
		rep.TotalNodes++
		class := p.replayNodeClass(n)
		st := rep.ParseStateExactByClass[class]
		if st == nil {
			st = &ReplayClassStat{Class: class}
			rep.ParseStateExactByClass[class] = st
		}
		st.Total++
		adv, _ := p.replayAdvance(n.preGotoState, n)
		if adv == n.parseState {
			rep.ParseStateExact++
		} else {
			st.Mismatched++
		}
		if len(n.children) > 0 {
			rep.FirstChildTotal++
			if n.children[0].preGotoState == n.preGotoState {
				rep.FirstChildPreExact++
			}
			for i := 1; i < len(n.children); i++ {
				rep.SiblingChainTotal++
				prevAdv, _ := p.replayAdvance(n.children[i-1].preGotoState, n.children[i-1])
				if n.children[i].preGotoState == prevAdv {
					rep.SiblingChainExact++
				}
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	return rep
}

func (r ReplayResyncReport) SortedClassStats() []ReplayClassStat {
	out := make([]ReplayClassStat, 0, len(r.ParseStateExactByClass))
	for _, st := range r.ParseStateExactByClass {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mismatched != out[j].Mismatched {
			return out[i].Mismatched > out[j].Mismatched
		}
		return out[i].Class < out[j].Class
	})
	return out
}
