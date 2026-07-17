package parsercorephase0

import (
	"errors"
	"fmt"
)

const inlineSchedulerCohortTargets = 8

// ShiftClassifiedOwned authenticates the scheduler owner, then delegates to
// the same uncheckpointed implementation used by standalone ShiftClassified.
func (c *Core) ShiftClassifiedOwned(owner SchedulerTransactionToken, boundary ClassifiedBoundary, actionOrdinal int, shifted Token, fork ForkOrder) (out Head, err error) {
	err = c.RunSchedulerOwned(owner, func() error {
		var innerErr error
		out, innerErr = c.shiftClassifiedUncheckpointed(boundary, actionOrdinal, shifted, fork)
		return innerErr
	})
	return out, err
}

func (c *Core) shiftClassifiedUncheckpointed(boundary ClassifiedBoundary, actionOrdinal int, shifted Token, fork ForkOrder) (Head, error) {
	act, err := c.classifiedAction(boundary, actionOrdinal)
	if err != nil {
		return Head{}, err
	}
	if act.Type != ActionShift {
		return Head{}, fmt.Errorf("parser-core phase zero: action %d is %v, not shift", actionOrdinal, act.Type)
	}
	if shifted.Symbol != boundary.lookahead {
		return Head{}, fmt.Errorf("parser-core phase zero: token symbol %d != lookahead %d", shifted.Symbol, boundary.lookahead)
	}
	if shifted.Extra != act.Extra {
		return Head{}, fmt.Errorf("parser-core phase zero: token extra=%t disagrees with decoded action extra=%t", shifted.Extra, act.Extra)
	}
	targetState := act.State
	if act.Extra && targetState == 0 {
		targetState = boundary.state
	}
	payload, err := c.appendSubtree(subtreeRecord{
		symbol: shifted.Symbol, startByte: shifted.StartByte, endByte: shifted.EndByte,
		extra: act.Extra, external: shifted.External, terminal: true,
	}, nil, nil, nil)
	if err != nil {
		return Head{}, err
	}
	phase0AObserveTerminalShift(c, payload, boundary.head.Node, targetState, shifted.EndByte, true, act.Extra)
	outcome, err := c.condenseWithOutcomeAtomic(c.shiftedBoundaryKey(targetState, shifted.EndByte), linkInput{
		prev: boundary.head.Node, payload: payload, order: fork,
	})
	if err != nil {
		return Head{}, err
	}
	c.addWork(&c.work.Shifts, 1)
	return outcome.head, nil
}

// ShiftOrdinaryClassifiedCohortOwned authenticates the scheduler owner, then
// delegates to the standalone cohort's uncheckpointed implementation.
func (c *Core) ShiftOrdinaryClassifiedCohortOwned(owner SchedulerTransactionToken, boundaries []ClassifiedBoundary, shifted Token) (out []Head, err error) {
	err = c.RunSchedulerOwned(owner, func() error {
		var inlineTargets [inlineSchedulerCohortTargets]StateID
		targets := inlineTargets[:]
		if len(boundaries) > len(targets) {
			targets = make([]StateID, len(boundaries))
		} else {
			targets = targets[:len(boundaries)]
		}
		if err := c.prepareOrdinaryClassifiedCohortInto(boundaries, shifted, targets); err != nil {
			return err
		}
		var innerErr error
		out, innerErr = c.shiftOrdinaryClassifiedCohortUncheckpointed(boundaries, targets, shifted)
		return innerErr
	})
	return out, err
}

func (c *Core) prepareOrdinaryClassifiedCohortInto(boundaries []ClassifiedBoundary, shifted Token, targets []StateID) error {
	if len(boundaries) == 0 {
		return errors.New("parser-core phase zero: empty ordinary classified shift cohort")
	}
	if len(targets) != len(boundaries) {
		return errors.New("parser-core phase zero: ordinary cohort target storage length mismatch")
	}
	if shifted.Extra || shifted.EndByte <= shifted.StartByte {
		return errors.New("parser-core phase zero: cohort token is not an ordinary positive-width terminal")
	}
	seen := make(map[NodeID]struct{}, len(boundaries))
	for index, boundary := range boundaries {
		if shifted.Symbol != boundary.lookahead {
			return fmt.Errorf("parser-core phase zero: token symbol %d != lookahead %d", shifted.Symbol, boundary.lookahead)
		}
		if _, duplicate := seen[boundary.head.Node]; duplicate {
			return fmt.Errorf("parser-core phase zero: duplicate ordinary cohort head %d", boundary.head.Node)
		}
		seen[boundary.head.Node] = struct{}{}
		action, err := c.classifiedAction(boundary, 0)
		if err != nil {
			return err
		}
		if boundary.actions.Len() != 1 || action.State == 0 || action != (Action{Type: ActionShift, State: action.State}) {
			return fmt.Errorf("parser-core phase zero: head %d does not select one ordinary shift", boundary.head.Node)
		}
		targets[index] = action.State
	}
	return nil
}

func (c *Core) shiftOrdinaryClassifiedCohortUncheckpointed(boundaries []ClassifiedBoundary, targets []StateID, shifted Token) ([]Head, error) {
	payload, err := c.appendSubtree(subtreeRecord{
		symbol: shifted.Symbol, startByte: shifted.StartByte, endByte: shifted.EndByte,
		external: shifted.External, terminal: true,
	}, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	phase0AObserveTerminalCohortShift(c, payload, boundaries, targets, shifted.EndByte, false)
	out := make([]Head, len(boundaries))
	for index, boundary := range boundaries {
		outcome, err := c.condenseWithOutcomeAtomic(c.shiftedBoundaryKey(targets[index], shifted.EndByte), linkInput{
			prev: boundary.head.Node, payload: payload,
		})
		if err != nil {
			return nil, err
		}
		out[index] = outcome.head
	}
	c.addWork(&c.work.Shifts, uint64(len(boundaries)))
	return out, nil
}

// ShiftExtraClassifiedCohortOwned authenticates the scheduler owner, then
// delegates to the standalone cohort's uncheckpointed implementation.
func (c *Core) ShiftExtraClassifiedCohortOwned(owner SchedulerTransactionToken, boundaries []ClassifiedBoundary, shifted Token) (out []Head, err error) {
	err = c.RunSchedulerOwned(owner, func() error {
		var inlineTargets [inlineSchedulerCohortTargets]StateID
		targets := inlineTargets[:]
		if len(boundaries) > len(targets) {
			targets = make([]StateID, len(boundaries))
		} else {
			targets = targets[:len(boundaries)]
		}
		if err := c.prepareExtraClassifiedCohortInto(boundaries, shifted, targets); err != nil {
			return err
		}
		var innerErr error
		out, innerErr = c.shiftExtraClassifiedCohortUncheckpointed(boundaries, targets, shifted)
		return innerErr
	})
	return out, err
}

func (c *Core) prepareExtraClassifiedCohortInto(boundaries []ClassifiedBoundary, shifted Token, targets []StateID) error {
	if len(boundaries) == 0 {
		return errors.New("parser-core phase zero: empty extra classified shift cohort")
	}
	if len(targets) != len(boundaries) {
		return errors.New("parser-core phase zero: extra cohort target storage length mismatch")
	}
	if !shifted.Extra || shifted.EndByte <= shifted.StartByte {
		return errors.New("parser-core phase zero: cohort token is not a positive-width extra terminal")
	}
	seen := make(map[NodeID]struct{}, len(boundaries))
	for index, boundary := range boundaries {
		if shifted.Symbol != boundary.lookahead {
			return fmt.Errorf("parser-core phase zero: token symbol %d != lookahead %d", shifted.Symbol, boundary.lookahead)
		}
		if _, duplicate := seen[boundary.head.Node]; duplicate {
			return fmt.Errorf("parser-core phase zero: duplicate extra cohort head %d", boundary.head.Node)
		}
		seen[boundary.head.Node] = struct{}{}
		action, err := c.classifiedAction(boundary, 0)
		if err != nil {
			return err
		}
		if boundary.actions.Len() != 1 || action != (Action{Type: ActionShift, State: action.State, Extra: true}) {
			return fmt.Errorf("parser-core phase zero: head %d does not select one extra shift", boundary.head.Node)
		}
		targets[index] = action.State
		if targets[index] == 0 {
			targets[index] = boundary.state
		}
	}
	return nil
}

func (c *Core) shiftExtraClassifiedCohortUncheckpointed(boundaries []ClassifiedBoundary, targets []StateID, shifted Token) ([]Head, error) {
	payload, err := c.appendSubtree(subtreeRecord{
		symbol: shifted.Symbol, startByte: shifted.StartByte, endByte: shifted.EndByte,
		extra: true, external: shifted.External, terminal: true,
	}, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	phase0AObserveTerminalCohortShift(c, payload, boundaries, targets, shifted.EndByte, true)
	out := make([]Head, len(boundaries))
	for index, boundary := range boundaries {
		outcome, err := c.condenseWithOutcomeAtomic(c.shiftedBoundaryKey(targets[index], shifted.EndByte), linkInput{
			prev: boundary.head.Node, payload: payload,
		})
		if err != nil {
			return nil, err
		}
		out[index] = outcome.head
	}
	c.addWork(&c.work.Shifts, uint64(len(boundaries)))
	return out, nil
}

// ReduceOutputsClassifiedIntoOwned authenticates the scheduler owner, then
// delegates to the standalone reduction's uncheckpointed implementation.
func (c *Core) ReduceOutputsClassifiedIntoOwned(owner SchedulerTransactionToken, dst []ReductionOutput, boundary ClassifiedBoundary, actionOrdinal int, fork ForkOrder) (frontier []ReductionOutput, err error) {
	err = c.RunSchedulerOwned(owner, func() error {
		var innerErr error
		frontier, innerErr = c.reduceOutputsClassifiedIntoUncheckpointed(dst, boundary, actionOrdinal, fork)
		return innerErr
	})
	return frontier, err
}

func (c *Core) reduceOutputsClassifiedIntoUncheckpointed(dst []ReductionOutput, boundary ClassifiedBoundary, actionOrdinal int, fork ForkOrder) ([]ReductionOutput, error) {
	frontier := dst[:0]
	if c.popScratch.busy {
		return nil, errors.New("parser-core phase zero: reentrant reduction while pop scratch is active")
	}
	c.popScratch.busy = true
	defer c.popScratch.resetLogical()
	c.reductionScratch.begin()
	defer c.reductionScratch.finish()
	act, err := c.classifiedAction(boundary, actionOrdinal)
	if err != nil {
		return nil, err
	}
	if act.Type != ActionReduce {
		return nil, fmt.Errorf("parser-core phase zero: action %d is %v, not reduce", actionOrdinal, act.Type)
	}
	plan, err := c.reductionPlan(act)
	if err != nil {
		return nil, err
	}
	paths, err := c.popPaths(boundary.head.Node, int(act.ChildCount))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, errors.New("parser-core phase zero: reduction has no exact pop path")
	}
	phase0ABeginReductionConstruction(c, uint64(len(paths)))
	c.addWork(&c.work.Reductions, 1)
	c.addWork(&c.work.ReductionPopRequests, 1)
	c.addWork(&c.work.EmittedPopPaths, uint64(len(paths)))
	for _, path := range paths {
		c.addWork(&c.work.EmittedPopPayloads, uint64(len(path.children)+len(path.trailing)))
	}
	scratch := &c.reductionScratch
	for _, path := range paths {
		prev, err := c.node(path.prev)
		if err != nil {
			return nil, err
		}
		gotoState, err := c.tables.Goto(prev.state, act.Symbol)
		if err != nil {
			return nil, err
		}
		if gotoState == 0 {
			return nil, fmt.Errorf("parser-core phase zero: no goto from state %d for reduced symbol %d", prev.state, act.Symbol)
		}
		key := c.boundaryKey(gotoState, path.structuralEnd)
		payload, scoreDelta, order, err := c.reductionParentForPath(act, &plan, path, key, fork, scratch)
		if err != nil {
			return nil, err
		}
		phase0AObserveReductionOccurrence(c, payload, path.prev, key, len(path.trailing) != 0)
		parentLink := linkInput{prev: path.prev, payload: payload, scoreDelta: scoreDelta, order: order}
		var out Head
		var outcome condenseOutcome
		if len(path.trailing) == 0 {
			outcome, err = c.condenseWithOutcomeAtomic(key, parentLink)
			out = outcome.head
		} else {
			out, err = c.appendPrivate(gotoState, path.structuralEnd, parentLink)
		}
		if err != nil {
			return nil, err
		}
		for index, trailing := range path.trailing {
			extra, err := c.subtree(trailing.payload)
			if err != nil {
				return nil, err
			}
			key = c.boundaryKey(gotoState, extra.endByte)
			extraLink := linkInput{prev: out.Node, payload: trailing.payload, scoreDelta: trailing.scoreDelta}
			if index == len(path.trailing)-1 {
				outcome, err = c.condenseWithOutcomeAtomic(key, extraLink)
				out = outcome.head
			} else {
				out, err = c.appendPrivate(gotoState, extra.endByte, extraLink)
			}
			if err != nil {
				return nil, err
			}
		}
		boundaryIndex, seen := scratch.boundary(key)
		var previous reductionBoundaryOutput
		if seen {
			previous = scratch.boundaries[boundaryIndex]
		}
		freshness := previous.freshness
		switch outcome.change {
		case condenseUnchanged:
			if !seen {
				freshness = ReductionUnchanged
			}
		case condenseNew:
			freshness = ReductionNew
		case condenseUpdated:
			if freshness != ReductionNew {
				freshness = ReductionUpdated
			}
		}
		scratch.store(boundaryIndex, seen, reductionBoundaryOutput{key: key, head: out, freshness: freshness})
	}
	for _, output := range scratch.boundaries {
		frontier = append(frontier, ReductionOutput{Head: output.head, Freshness: output.freshness})
	}
	phase0AFinishReductionConstruction(c)
	return frontier, nil
}
