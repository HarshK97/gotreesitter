package parsercorephase0

import (
	"errors"
	"fmt"
)

// StackSummaryMaxDepth matches tree-sitter's MAX_SUMMARY_DEPTH. Compact S4
// never inspects or mutates a predecessor beyond this bound.
const StackSummaryMaxDepth = 16

const stackSummaryMaxVisitedNodes = 4096

// StackSummaryCandidate authenticates one action-bearing predecessor state.
// Depth counts predecessor links and preserves the original compact S3 probe
// contract. The scheduler performs cost comparison and election separately.
//
// The fields stay private because owner, generation, and source identity are
// one capability. RecoverToAncestorStateOwned rejects copied candidates after
// rollback or reset before it reads a reusable arena ID.
type StackSummaryCandidate struct {
	owner      *Core
	generation uint64
	source     NodeID
	state      StateID
	byteOffset uint32
	depth      uint8
}

// Depth returns the predecessor-link depth of this candidate.
func (c StackSummaryCandidate) Depth() int { return int(c.depth) }

// State returns the parser state recorded for this candidate.
func (c StackSummaryCandidate) State() StateID { return c.state }

// ByteOffset returns the source position recorded for this candidate.
func (c StackSummaryCandidate) ByteOffset() uint32 { return c.byteOffset }

type stackSummaryNodeKey struct {
	node  NodeID
	depth uint8
}

type stackSummaryStateKey struct {
	state StateID
	depth uint8
}

// StackSummaryCandidates enumerates every action-bearing predecessor state in
// depth-major and stable link-insertion order. It visits the complete graph to
// maxDepth and deduplicates metadata by (depth, state). Path deduplication does
// not authorize mutation: RecoverToAncestorStateOwned re-enumerates paths and
// requires exactly one match for the elected pair.
func (c *Core) StackSummaryCandidates(head Head, lookahead Symbol, maxDepth int) ([]StackSummaryCandidate, error) {
	if maxDepth <= 0 {
		return nil, nil
	}
	if maxDepth > StackSummaryMaxDepth {
		return nil, fmt.Errorf("parser-core phase zero: stack-summary depth %d exceeds limit %d", maxDepth, StackSummaryMaxDepth)
	}
	if c == nil {
		return nil, errors.New("parser-core phase zero: stack-summary enumeration on nil core")
	}
	if _, err := c.node(head.Node); err != nil {
		return nil, err
	}

	frontier := []NodeID{head.Node}
	visited := map[stackSummaryNodeKey]struct{}{{node: head.Node}: {}}
	seen := make(map[stackSummaryStateKey]struct{})
	var candidates []StackSummaryCandidate
	for depth := 1; depth <= maxDepth && len(frontier) != 0; depth++ {
		next := make([]NodeID, 0, len(frontier))
		for _, id := range frontier {
			node, err := c.node(id)
			if err != nil {
				return nil, err
			}
			var inline [inlineAdjacencyCapacity]linkRecord
			links, err := c.publishedNodeLinksInto(inline[:0], *node)
			if err != nil {
				return nil, err
			}
			for _, link := range links {
				if link.prev == 0 || link.prev >= id {
					return nil, errors.New("parser-core phase zero: stack-summary predecessor does not decrease")
				}
				key := stackSummaryNodeKey{node: link.prev, depth: uint8(depth)}
				if _, ok := visited[key]; ok {
					continue
				}
				visited[key] = struct{}{}
				if len(visited) > stackSummaryMaxVisitedNodes {
					return nil, errors.New("parser-core phase zero: stack-summary visited-node cap")
				}
				next = append(next, link.prev)

				ancestor, err := c.node(link.prev)
				if err != nil {
					return nil, err
				}
				stateKey := stackSummaryStateKey{state: ancestor.state, depth: uint8(depth)}
				if _, ok := seen[stateKey]; ok {
					continue
				}
				seen[stateKey] = struct{}{}
				row, err := c.tables.Actions(ancestor.state, lookahead)
				if err != nil {
					return nil, err
				}
				if row.Len() == 0 {
					continue
				}
				candidates = append(candidates, StackSummaryCandidate{
					owner: c, generation: c.classificationPhase, source: head.Node,
					state: ancestor.state, byteOffset: ancestor.byteOffset, depth: uint8(depth),
				})
			}
		}
		frontier = next
	}
	return candidates, nil
}

// AncestorStateWithActionExists preserves the S3 compatibility probe. It now
// delegates to the complete candidate enumerator and reports only existence.
func (c *Core) AncestorStateWithActionExists(head Head, lookahead Symbol, maxDepth int) (bool, error) {
	candidates, err := c.StackSummaryCandidates(head, lookahead, maxDepth)
	return len(candidates) != 0, err
}

// RecoverToAncestorStateOwned performs the S4 stack mutation for one elected
// candidate. The scheduler token owns the surrounding atomic transaction.
// This method poisons that owner on every failure, so ignored errors cannot
// commit partial ERROR, child, link, node, or boundary-index publication.
func (c *Core) RecoverToAncestorStateOwned(owner SchedulerTransactionToken, candidate StackSummaryCandidate) (out Head, err error) {
	if err = c.beginSchedulerOwned(owner); err != nil {
		return out, err
	}
	defer c.recoverSchedulerOwnedPanic(owner)
	out, err = c.recoverToAncestorStateUncheckpointed(candidate)
	return out, c.finishSchedulerOwned(owner, err)
}

func (c *Core) recoverToAncestorStateUncheckpointed(candidate StackSummaryCandidate) (Head, error) {
	if candidate.owner != c {
		return Head{}, errors.New("parser-core phase zero: stack-summary candidate belongs to a different core")
	}
	if candidate.generation == 0 || candidate.generation != c.classificationPhase {
		return Head{}, errors.New("parser-core phase zero: stale stack-summary candidate")
	}
	if candidate.source == 0 || candidate.depth == 0 || candidate.depth > StackSummaryMaxDepth {
		return Head{}, errors.New("parser-core phase zero: invalid stack-summary candidate")
	}
	if _, err := c.node(candidate.source); err != nil {
		return Head{}, err
	}

	links, target, err := c.uniqueAncestorRecoveryPath(candidate)
	if err != nil {
		return Head{}, err
	}
	depth := len(links)
	trailing := 0
	for trailing < depth {
		payload, err := c.subtree(links[trailing].payload)
		if err != nil {
			return Head{}, err
		}
		if !payload.extra {
			break
		}
		trailing++
	}
	if trailing == depth {
		return Head{}, errors.New("parser-core phase zero: ancestor recovery path has only trailing extras")
	}

	children := make([]SubtreeID, 0, depth-trailing)
	var score int64
	var order ForkOrder
	for index := depth - 1; index >= trailing; index-- {
		link := links[index]
		children = append(children, link.payload)
		score, err = checkedAddScore(score, link.scoreDelta)
		if err != nil {
			return Head{}, err
		}
		if link.hasOrder() {
			order = ForkOrder{Present: true, Value: link.order}
		}
	}
	first, err := c.subtree(children[0])
	if err != nil {
		return Head{}, err
	}
	last, err := c.subtree(children[len(children)-1])
	if err != nil {
		return Head{}, err
	}
	errorPayload, err := c.appendSubtree(subtreeRecord{
		symbol: ErrorRegionSymbol, startByte: first.startByte, endByte: last.endByte, extra: true,
	}, children, nil, nil)
	if err != nil {
		return Head{}, err
	}

	out := Head{Node: target}
	errorLink := linkInput{prev: target, payload: errorPayload, scoreDelta: score, order: order}
	if trailing == 0 {
		outcome, err := c.condenseWithOutcomeAtomic(c.shiftedBoundaryKey(candidate.state, last.endByte), errorLink)
		return outcome.head, err
	}
	out, err = c.appendPrivate(candidate.state, last.endByte, errorLink)
	if err != nil {
		return Head{}, err
	}
	for index := trailing - 1; index >= 0; index-- {
		link := links[index]
		payload, err := c.subtree(link.payload)
		if err != nil {
			return Head{}, err
		}
		input := linkInput{prev: out.Node, payload: link.payload, scoreDelta: link.scoreDelta}
		if link.hasOrder() {
			input.order = ForkOrder{Present: true, Value: link.order}
		}
		if index == 0 {
			outcome, err := c.condenseWithOutcomeAtomic(c.shiftedBoundaryKey(candidate.state, payload.endByte), input)
			if err != nil {
				return Head{}, err
			}
			out = outcome.head
			continue
		}
		out, err = c.appendPrivate(candidate.state, payload.endByte, input)
		if err != nil {
			return Head{}, err
		}
	}
	return out, nil
}

func (c *Core) uniqueAncestorRecoveryPath(candidate StackSummaryCandidate) ([]linkRecord, NodeID, error) {
	wantDepth := int(candidate.depth)
	var route [StackSummaryMaxDepth]linkRecord
	var selected [StackSummaryMaxDepth]linkRecord
	var selectedTarget NodeID
	var completePaths uint64
	matches := 0
	steps := 0
	const maxSteps = stackSummaryMaxVisitedNodes * StackSummaryMaxDepth

	var walk func(NodeID, int) error
	walk = func(id NodeID, depth int) error {
		if depth == wantDepth {
			completePaths++
			if completePaths > c.limits.MaxPopPaths {
				return errors.New("parser-core phase zero: ancestor recovery pop enumeration cap")
			}
			target, err := c.node(id)
			if err != nil {
				return err
			}
			if target.state != candidate.state {
				return nil
			}
			matches++
			if matches > 1 {
				return errors.New("parser-core phase zero: ancestor recovery candidate has ambiguous pop paths")
			}
			if target.byteOffset != candidate.byteOffset {
				return errors.New("parser-core phase zero: stale stack-summary candidate position")
			}
			copy(selected[:wantDepth], route[:wantDepth])
			selectedTarget = id
			return nil
		}

		node, err := c.node(id)
		if err != nil {
			return err
		}
		var inline [inlineAdjacencyCapacity]linkRecord
		links, err := c.publishedNodeLinksInto(inline[:0], *node)
		if err != nil {
			return err
		}
		for _, link := range links {
			steps++
			if steps > maxSteps {
				return errors.New("parser-core phase zero: ancestor recovery traversal cap")
			}
			if link.prev == 0 || link.prev >= id {
				return errors.New("parser-core phase zero: ancestor recovery predecessor does not decrease")
			}
			route[depth] = link
			if err := walk(link.prev, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(candidate.source, 0); err != nil {
		return nil, 0, err
	}
	if matches == 0 {
		return nil, 0, errors.New("parser-core phase zero: stack-summary candidate has no exact pop path")
	}
	return append([]linkRecord(nil), selected[:wantDepth]...), selectedTarget, nil
}
