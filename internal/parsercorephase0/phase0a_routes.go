//go:build gts_parsercorephase0 && gts_parsercorephase0a

package parsercorephase0

import (
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// Phase0APopRouteRecord binds one production pop ordinal to the independently
// re-enumerated physical links that produced it. FirstLink indexes the flat
// Phase0AProofSnapshot.PopRouteLinks slice; linkless zero-child pops are valid.
type Phase0APopRouteRecord struct {
	ID                  uint64
	TransactionID       uint64
	PopOrdinal          uint32
	Head                NodeID
	Predecessor         NodeID
	FirstLink           uint64
	LinkCount           uint32
	Occurrence          ConstructionOccurrenceKey
	Edge                IncomingEdgeKey
	RolledBack          bool
	RollbackTransaction uint64
	RollbackCause       Phase0ARollbackCause
}

type Phase0APopRouteLinkRecord struct {
	Route               uint64
	TransactionID       uint64
	Ordinal             uint32
	Link                LinkID
	Payload             SubtreeID
	RolledBack          bool
	RollbackTransaction uint64
	RollbackCause       Phase0ARollbackCause
}

// Phase0AAcceptedSelectionRecord freezes the unique physical root-to-head
// spine after exact acceptance. The linkless graph seed contributes no row.
type Phase0AAcceptedSelectionRecord struct {
	Namespace  CoreRunNamespace
	Generation uint64
	Capability uint64
	Head       NodeID
	FirstLink  uint64
	LinkCount  uint32
}

// Phase0ASelectionCapabilityRecord authenticates one captured current head.
// Serial is monotonic within the run and never rewinds after rollback, so an
// arena-reused NodeID cannot make an old capability current again.
type Phase0ASelectionCapabilityRecord struct {
	Namespace           CoreRunNamespace
	Serial              uint64
	TransactionID       uint64
	Head                NodeID
	RolledBack          bool
	RollbackTransaction uint64
	RollbackCause       Phase0ARollbackCause
}

// Phase0AAcceptedLinkRecord is one selected physical link in root-to-head
// order. BoundExpression is the link binding; ResolvedExpression is the exact
// Direct leaf selected through any nested FactorChoice expressions.
type Phase0AAcceptedLinkRecord struct {
	Namespace          CoreRunNamespace
	Generation         uint64
	Ordinal            uint32
	Link               LinkID
	Payload            SubtreeID
	BoundExpression    Phase0AExpressionID
	ResolvedExpression Phase0AExpressionID
	Occurrence         ConstructionOccurrenceKey
	Edge               IncomingEdgeKey
}

type phase0ARouteObserver struct {
	popRoutes          []Phase0APopRouteRecord
	popLinks           []Phase0APopRouteLinkRecord
	capabilities       []Phase0ASelectionCapabilityRecord
	acceptedSelections []Phase0AAcceptedSelectionRecord
	acceptedLinks      []Phase0AAcceptedLinkRecord
	frames             []phase0ARouteFrame
	nextPopRoute       uint64
	nextCapability     uint64
	nextSelection      uint64
}

type phase0ARouteFrame struct {
	popRouteStart   uint64
	popLinkStart    uint64
	capabilityStart uint64
}

type phase0APhysicalPopPath struct {
	prev          NodeID
	children      []SubtreeID
	trailing      []SubtreeID
	links         []LinkID
	score         int64
	order         ForkOrder
	startByte     uint32
	structuralEnd uint32
}

type phase0APhysicalDerivation struct {
	links          []LinkID
	payloads       []SubtreeID
	score          int64
	branchOrder    uint64
	hasBranchOrder bool
}

type phase0APhysicalLink struct {
	id     LinkID
	record linkRecord
}

const (
	phase0APopRouteBytes            = uint64(unsafe.Sizeof(Phase0APopRouteRecord{}))
	phase0APopRouteLinkBytes        = uint64(unsafe.Sizeof(Phase0APopRouteLinkRecord{}))
	phase0ASelectionCapabilityBytes = uint64(unsafe.Sizeof(Phase0ASelectionCapabilityRecord{}))
	phase0AAcceptedSelectionBytes   = uint64(unsafe.Sizeof(Phase0AAcceptedSelectionRecord{}))
	phase0AAcceptedLinkBytes        = uint64(unsafe.Sizeof(Phase0AAcceptedLinkRecord{}))
	phase0ARouteFrameBytes          = uint64(unsafe.Sizeof(phase0ARouteFrame{}))
)

func phase0ARouteMarkLocked(observer *phase0AObserver) {
	observer.route.frames = append(observer.route.frames, phase0ARouteFrame{
		popRouteStart:   uint64(len(observer.route.popRoutes)),
		popLinkStart:    uint64(len(observer.route.popLinks)),
		capabilityStart: uint64(len(observer.route.capabilities)),
	})
}

func phase0ARouteRollbackLocked(observer *phase0AObserver, transaction uint64, cause Phase0ARollbackCause) {
	if len(observer.route.frames) == 0 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "route rollback frame mismatch"})
		return
	}
	frame := observer.route.frames[len(observer.route.frames)-1]
	for index := frame.popRouteStart; index < uint64(len(observer.route.popRoutes)); index++ {
		row := &observer.route.popRoutes[index]
		row.RolledBack, row.RollbackTransaction, row.RollbackCause = true, transaction, cause
	}
	for index := frame.popLinkStart; index < uint64(len(observer.route.popLinks)); index++ {
		row := &observer.route.popLinks[index]
		row.RolledBack, row.RollbackTransaction, row.RollbackCause = true, transaction, cause
	}
	for index := frame.capabilityStart; index < uint64(len(observer.route.capabilities)); index++ {
		row := &observer.route.capabilities[index]
		row.RolledBack, row.RollbackTransaction, row.RollbackCause = true, transaction, cause
	}
	observer.route.frames = observer.route.frames[:len(observer.route.frames)-1]
}

func phase0ARouteCommitLocked(observer *phase0AObserver) {
	if len(observer.route.frames) == 0 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "route commit frame mismatch"})
		return
	}
	observer.route.frames = observer.route.frames[:len(observer.route.frames)-1]
}

func phase0ACheckRouteRowsLocked(observer *phase0AObserver, popRoutes, popLinks, selections, acceptedLinks uint64) error {
	checks := []struct {
		current uint64
		add     uint64
		limit   uint64
		detail  string
	}{
		{uint64(len(observer.route.popRoutes)), popRoutes, phase0AObservers.limits.MaxPopRoutes, "pop-route row cap"},
		{uint64(len(observer.route.popLinks)), popLinks, phase0AObservers.limits.MaxPopRouteLinks, "pop-route link cap"},
		{uint64(len(observer.route.acceptedSelections)), selections, phase0AObservers.limits.MaxAcceptedSelections, "accepted-selection row cap"},
		{uint64(len(observer.route.acceptedLinks)), acceptedLinks, phase0AObservers.limits.MaxAcceptedLinks, "accepted-link row cap"},
	}
	for _, check := range checks {
		limit := check.limit
		if limit == 0 {
			limit = phase0AObservers.limits.MaxRecords
		}
		if check.current > limit || check.add > limit-check.current {
			return &Phase0AError{Kind: Phase0AErrorRecordCap, Namespace: observer.run, Detail: check.detail}
		}
	}
	return nil
}

func phase0AReserveRouteRowsLocked(observer *phase0AObserver, popRoutes, popLinks, selections, acceptedLinks uint64) error {
	if err := phase0ACheckRouteRowsLocked(observer, popRoutes, popLinks, selections, acceptedLinks); err != nil {
		return phase0AStickyLocked(observer, err.(*Phase0AError))
	}
	records := popRoutes + popLinks + selections + acceptedLinks
	bytes := popRoutes*phase0APopRouteBytes + popLinks*phase0APopRouteLinkBytes + selections*phase0AAcceptedSelectionBytes + acceptedLinks*phase0AAcceptedLinkBytes
	if err := phase0AChargeManyLocked(records, bytes); err != nil {
		return phase0AStickyLocked(observer, err.(*Phase0AError))
	}
	return nil
}

func phase0AReserveSelectionCapabilityLocked(observer *phase0AObserver) error {
	limit := phase0AObservers.limits.MaxSelectionCapabilities
	if limit == 0 {
		limit = phase0AObservers.limits.MaxRecords
	}
	if uint64(len(observer.route.capabilities)) >= limit {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorRecordCap, Namespace: observer.run, Detail: "selection-capability row cap"})
	}
	if err := phase0AChargeManyLocked(1, phase0ASelectionCapabilityBytes); err != nil {
		return phase0AStickyLocked(observer, err.(*Phase0AError))
	}
	return nil
}

func phase0AStablePhysicalLinks(core *Core, id NodeID) ([]phase0APhysicalLink, error) {
	node, err := core.node(id)
	if err != nil {
		return nil, err
	}
	if uint64(node.linkCount) > uint64(core.limits.MaxLinks) || uint64(node.linkCount) > uint64(len(core.links)) {
		return nil, errors.New("parser-core phase zero A: physical adjacency exceeds cap")
	}
	out := make([]phase0APhysicalLink, node.linkCount)
	next := LinkID(node.firstLink)
	for index := len(out) - 1; index >= 0; index-- {
		if next == 0 || uint64(next) > uint64(len(core.links)) {
			return nil, errors.New("parser-core phase zero A: physical adjacency is unavailable")
		}
		link := core.links[next-1]
		out[index] = phase0APhysicalLink{id: next, record: link}
		next = link.next
	}
	if next != 0 {
		return nil, errors.New("parser-core phase zero A: physical adjacency exceeds recorded width")
	}
	return out, nil
}

// phase0AEnumeratePopRoutes deliberately mirrors popPaths without sharing its
// scratch or result. The caller compares every semantic field before binding
// production pop ordinals to physical LinkIDs.
func phase0AEnumeratePopRoutes(core *Core, head NodeID, childCount int) ([]phase0APhysicalPopPath, error) {
	if childCount == 0 {
		node, err := core.node(head)
		if err != nil {
			return nil, err
		}
		return []phase0APhysicalPopPath{{prev: head, startByte: node.byteOffset, structuralEnd: node.byteOffset}}, nil
	}
	var out []phase0APhysicalPopPath
	var revLinks []LinkID
	var revPayloads []SubtreeID
	var revScores []int64
	var revOrders []ForkOrder
	var trailingLinks []LinkID
	var trailingPayloads []SubtreeID
	var trailingOrders []ForkOrder
	var walk func(NodeID, int, bool, uint32) error
	walk = func(id NodeID, remaining int, peelingTrailing bool, structuralEnd uint32) error {
		links, err := phase0AStablePhysicalLinks(core, id)
		if err != nil {
			return err
		}
		for _, physical := range links {
			link := physical.record
			if link.prev == 0 || link.prev >= id {
				return errors.New("parser-core phase zero A: physical pop predecessor does not decrease")
			}
			payload, err := core.subtree(link.payload)
			if err != nil {
				return err
			}
			order := ForkOrder{Value: link.order, Present: link.hasOrder()}
			if payload.extra && peelingTrailing {
				trailingLinks = append(trailingLinks, physical.id)
				trailingPayloads = append(trailingPayloads, link.payload)
				trailingOrders = append(trailingOrders, order)
				if err := walk(link.prev, remaining, true, structuralEnd); err != nil {
					return err
				}
				trailingLinks = trailingLinks[:len(trailingLinks)-1]
				trailingPayloads = trailingPayloads[:len(trailingPayloads)-1]
				trailingOrders = trailingOrders[:len(trailingOrders)-1]
				continue
			}
			nextStructuralEnd := structuralEnd
			if !payload.extra && peelingTrailing {
				nextStructuralEnd = payload.endByte
			}
			revLinks = append(revLinks, physical.id)
			revPayloads = append(revPayloads, link.payload)
			revScores = append(revScores, link.scoreDelta)
			revOrders = append(revOrders, order)
			next := remaining
			if !payload.extra {
				next--
			}
			if next == 0 {
				if uint64(len(out)) >= core.limits.MaxPopPaths {
					return errors.New("parser-core phase zero A: physical pop enumeration cap")
				}
				path := phase0APhysicalPopPath{prev: link.prev, startByte: payload.startByte, structuralEnd: nextStructuralEnd}
				for index := len(revLinks) - 1; index >= 0; index-- {
					path.links = append(path.links, revLinks[index])
					path.children = append(path.children, revPayloads[index])
					path.score, err = checkedAddScore(path.score, revScores[index])
					if err != nil {
						return err
					}
					if revOrders[index].Present {
						path.order = revOrders[index]
					}
				}
				for index := len(trailingLinks) - 1; index >= 0; index-- {
					path.links = append(path.links, trailingLinks[index])
					path.trailing = append(path.trailing, trailingPayloads[index])
					if trailingOrders[index].Present {
						path.order = trailingOrders[index]
					}
				}
				out = append(out, path)
			} else if err := walk(link.prev, next, false, nextStructuralEnd); err != nil {
				return err
			}
			revLinks = revLinks[:len(revLinks)-1]
			revPayloads = revPayloads[:len(revPayloads)-1]
			revScores = revScores[:len(revScores)-1]
			revOrders = revOrders[:len(revOrders)-1]
		}
		return nil
	}
	if err := walk(head, childCount, true, 0); err != nil {
		return nil, err
	}
	return out, nil
}

func phase0ASameSubtrees(left, right []SubtreeID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func phase0AObservePopRoutes(core *Core, head NodeID, childCount int, production []popPath) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.failure != nil {
		return
	}
	pending := &observer.reduction
	if !pending.active || pending.observed != 0 || phase0ACurrentTransaction(observer) != pending.transaction || uint64(len(production)) != uint64(pending.expected) {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "physical pop proof is outside its reduction"})
		return
	}
	routes, err := phase0AEnumeratePopRoutes(core, head, childCount)
	if err != nil {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorUnsupportedProof, Namespace: observer.run, Detail: err.Error()})
		return
	}
	if len(routes) != len(production) {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: fmt.Sprintf("physical pop path count %d differs from production %d", len(routes), len(production))})
		return
	}
	linkCount := uint64(0)
	for index := range routes {
		got, want := routes[index], production[index]
		wantTrailing := make([]SubtreeID, len(want.trailing))
		for trailingIndex := range want.trailing {
			wantTrailing[trailingIndex] = want.trailing[trailingIndex].payload
		}
		if got.prev != want.prev || got.score != want.score || got.order != want.order || got.startByte != want.startByte || got.structuralEnd != want.structuralEnd || !phase0ASameSubtrees(got.children, want.children) || !phase0ASameSubtrees(got.trailing, wantTrailing) {
			phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "physical pop path does not exactly match production ordinal"})
			return
		}
		if linkCount > math.MaxUint64-uint64(len(got.links)) {
			phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Namespace: observer.run, Detail: "physical pop link count"})
			return
		}
		linkCount += uint64(len(got.links))
	}
	if uint64(len(routes)) > math.MaxUint32 || linkCount > math.MaxUint32 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Namespace: observer.run, Detail: "physical pop route width"})
		return
	}
	if observer.route.nextPopRoute > math.MaxUint64-uint64(len(routes)) {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Namespace: observer.run, Detail: "physical pop route serial"})
		return
	}
	if err := phase0AReserveRouteRowsLocked(observer, uint64(len(routes)), linkCount, 0, 0); err != nil {
		return
	}
	pending.routeStart = uint64(len(observer.route.popRoutes))
	pending.routeCount = uint32(len(routes))
	pending.routeProof = true
	for index, route := range routes {
		observer.route.nextPopRoute++
		id := observer.route.nextPopRoute
		first := uint64(len(observer.route.popLinks))
		observer.route.popRoutes = append(observer.route.popRoutes, Phase0APopRouteRecord{
			ID: id, TransactionID: pending.transaction, PopOrdinal: uint32(index), Head: head,
			Predecessor: route.prev, FirstLink: first, LinkCount: uint32(len(route.links)),
		})
		for ordinal, linkID := range route.links {
			link := core.links[linkID-1]
			observer.route.popLinks = append(observer.route.popLinks, Phase0APopRouteLinkRecord{
				Route: id, TransactionID: pending.transaction, Ordinal: uint32(ordinal), Link: linkID, Payload: link.payload,
			})
		}
	}
}

func phase0ABindReductionRouteLocked(observer *phase0AObserver, pending *phase0AReductionConstruction, occurrence ConstructionOccurrenceKey, edge IncomingEdgeKey, predecessor NodeID) bool {
	if pending.routeCount != pending.expected || pending.observed >= pending.routeCount {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "reduction occurrence has no physical pop route"})
		return false
	}
	index := pending.routeStart + uint64(pending.observed)
	if index >= uint64(len(observer.route.popRoutes)) {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "physical pop route row is unavailable"})
		return false
	}
	route := &observer.route.popRoutes[index]
	if route.RolledBack || route.TransactionID != pending.transaction || route.PopOrdinal != pending.observed || route.Predecessor != predecessor || route.Occurrence != (ConstructionOccurrenceKey{}) || route.Edge != (IncomingEdgeKey{}) {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "physical pop route cannot bind this occurrence"})
		return false
	}
	route.Occurrence, route.Edge = occurrence, edge
	return true
}

func phase0AEnumeratePhysicalDerivations(core *Core, head Head) ([]phase0APhysicalDerivation, error) {
	visiting := make(map[NodeID]bool)
	var walk func(NodeID) ([]phase0APhysicalDerivation, error)
	walk = func(id NodeID) ([]phase0APhysicalDerivation, error) {
		if visiting[id] {
			return nil, errors.New("parser-core phase zero A: accepted physical spine cycle")
		}
		node, err := core.node(id)
		if err != nil {
			return nil, err
		}
		if node.linkCount == 0 {
			if node.pathCount != 1 {
				return nil, errors.New("parser-core phase zero A: accepted physical seed path count")
			}
			return []phase0APhysicalDerivation{{}}, nil
		}
		visiting[id] = true
		defer delete(visiting, id)
		links, err := phase0AStablePhysicalLinks(core, id)
		if err != nil {
			return nil, err
		}
		var out []phase0APhysicalDerivation
		for _, physical := range links {
			if physical.record.prev == 0 || physical.record.prev >= id {
				return nil, errors.New("parser-core phase zero A: accepted physical predecessor does not decrease")
			}
			prefixes, err := walk(physical.record.prev)
			if err != nil {
				return nil, err
			}
			for _, prefix := range prefixes {
				if uint64(len(out)) >= core.limits.MaxDerivations {
					return nil, ErrDerivationEnumerationCap
				}
				score, err := checkedAddScore(prefix.score, physical.record.scoreDelta)
				if err != nil {
					return nil, err
				}
				path := phase0APhysicalDerivation{score: score, branchOrder: prefix.branchOrder, hasBranchOrder: prefix.hasBranchOrder}
				path.links = append(path.links, prefix.links...)
				path.links = append(path.links, physical.id)
				path.payloads = append(path.payloads, prefix.payloads...)
				path.payloads = append(path.payloads, physical.record.payload)
				if physical.record.hasOrder() {
					path.branchOrder, path.hasBranchOrder = physical.record.order, true
				}
				out = append(out, path)
			}
		}
		if node.pathCount != math.MaxUint64 && uint64(len(out)) != node.pathCount {
			return nil, errors.New("parser-core phase zero A: accepted physical path-count mismatch")
		}
		return out, nil
	}
	return walk(head.Node)
}

func phase0AExactBindingLocked(observer *phase0AObserver, link LinkID) (Phase0AExpressionID, error) {
	var found Phase0AExpressionID
	live, rolled := 0, 0
	for index := range observer.factor.bindings {
		binding := observer.factor.bindings[index]
		if binding.Link != link {
			continue
		}
		if binding.RolledBack {
			rolled++
			continue
		}
		live++
		found = binding.Expression
	}
	if live > 1 {
		return 0, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "accepted link has multiple live bindings"}
	}
	if live == 0 && rolled != 0 {
		return 0, &Phase0AError{Kind: Phase0AErrorRolledBackReference, Namespace: observer.run, Detail: "accepted link resolves only to rolled-back bindings"}
	}
	if live == 0 {
		return 0, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "accepted link has no binding"}
	}
	return found, nil
}

func phase0AExactExpressionLocked(observer *phase0AObserver, id Phase0AExpressionID) (Phase0AExpressionRecord, error) {
	var found Phase0AExpressionRecord
	live, rolled := 0, 0
	for _, expression := range observer.factor.expressions {
		if expression.ID != id {
			continue
		}
		if expression.RolledBack {
			rolled++
			continue
		}
		live++
		found = expression
	}
	if live > 1 {
		return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "expression ID has multiple live rows"}
	}
	if live == 0 && rolled != 0 {
		return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorRolledBackReference, Namespace: observer.run, Detail: "expression ID resolves only to a rolled-back row"}
	}
	if live == 0 {
		return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "expression ID is unavailable"}
	}
	return found, nil
}

func phase0ASelectorBranchLocked(observer *phase0AObserver, selector Phase0ASelectorID, selectedLower LinkID) (Phase0ASelectorBranch, LinkID, error) {
	selectorLive, selectorRolled := 0, 0
	for _, row := range observer.factor.selectors {
		if row.ID != selector {
			continue
		}
		if row.RolledBack {
			selectorRolled++
		} else {
			selectorLive++
		}
	}
	if selectorLive > 1 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "factor selector has multiple live rows"}
	}
	if selectorLive == 0 && selectorRolled != 0 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorRolledBackReference, Namespace: observer.run, Detail: "factor selector is rolled back"}
	}
	if selectorLive == 0 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "factor selector is unavailable"}
	}
	var branch Phase0ASelectorBranch
	var sourceLower LinkID
	routeLive, routeRolled := 0, 0
	for _, route := range observer.factor.selectorRoutes {
		if route.Selector != selector || route.SelectedLowerLink != selectedLower {
			continue
		}
		if route.RolledBack {
			routeRolled++
		} else {
			routeLive++
			branch = route.Branch
			sourceLower = route.SourceLowerLink
		}
	}
	if routeLive > 1 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "factor selector has multiple routes for selected lower link"}
	}
	if routeLive == 0 && routeRolled != 0 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorRolledBackReference, Namespace: observer.run, Detail: "factor route is rolled back"}
	}
	if routeLive == 0 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "factor route does not contain selected lower link"}
	}
	if (branch != Phase0ASelectorLeft && branch != Phase0ASelectorRight) || sourceLower == 0 {
		return 0, 0, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "factor route has invalid branch or source link"}
	}
	return branch, sourceLower, nil
}

func phase0AResolveAcceptedExpressionLocked(observer *phase0AObserver, initial Phase0AExpressionID, selectedLower LinkID) (Phase0AExpressionRecord, error) {
	current := initial
	visited := make(map[Phase0AExpressionID]struct{})
	stepCap := len(observer.factor.expressions) + 1
	for steps := 0; steps < stepCap; steps++ {
		if _, duplicate := visited[current]; duplicate {
			return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorCyclicReference, Namespace: observer.run, Detail: "factor expression cycle"}
		}
		visited[current] = struct{}{}
		expression, err := phase0AExactExpressionLocked(observer, current)
		if err != nil {
			return Phase0AExpressionRecord{}, err
		}
		switch expression.Kind {
		case Phase0AExpressionDirect:
			if expression.Occurrence.Event.Attempt.Run != observer.run || expression.Edge.Event.Attempt.Run != observer.run || expression.Occurrence.Event.Attempt.AttemptEpoch != 1 || expression.Edge.Event.Attempt.AttemptEpoch != 1 {
				return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "direct expression belongs to another run"}
			}
			return expression, nil
		case Phase0AExpressionFactorChoice:
			if selectedLower == 0 {
				return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "factor expression has no selected lower link"}
			}
			branch, sourceLower, err := phase0ASelectorBranchLocked(observer, expression.Selector, selectedLower)
			if err != nil {
				return Phase0AExpressionRecord{}, err
			}
			if branch == Phase0ASelectorLeft {
				current = expression.Left
			} else {
				current = expression.Right
			}
			// A selector route translates the selected link in the current
			// merged adjacency back into the source adjacency used to build the
			// chosen expression. Nested FactorChoice resolution must carry that
			// physical identity inward rather than reusing the copied link.
			selectedLower = sourceLower
		default:
			return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "unknown expression kind"}
		}
	}
	return Phase0AExpressionRecord{}, &Phase0AError{Kind: Phase0AErrorCyclicReference, Namespace: observer.run, Detail: "factor expression step cap"}
}

func phase0AValidateDirectOccurrenceLocked(observer *phase0AObserver, expression Phase0AExpressionRecord) error {
	occurrenceIndex, ok := observer.occurrenceIndex[expression.Occurrence]
	if !ok || occurrenceIndex >= uint64(len(observer.mutations)) {
		return &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "accepted direct occurrence is not indexed"}
	}
	occurrence := observer.mutations[occurrenceIndex]
	if occurrence.RolledBack {
		return &Phase0AError{Kind: Phase0AErrorRolledBackReference, Namespace: observer.run, Detail: "accepted direct occurrence was rolled back"}
	}
	if occurrence.Kind != Phase0AMutationOccurrence || occurrence.Occurrence != expression.Occurrence || occurrence.Edge != expression.Edge {
		return &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "accepted direct occurrence index mismatch"}
	}
	edgeIndex, ok := observer.edgeIndex[expression.Edge]
	if !ok || edgeIndex >= uint64(len(observer.mutations)) {
		return &Phase0AError{Kind: Phase0AErrorMissingReference, Namespace: observer.run, Detail: "accepted direct edge is not indexed"}
	}
	edge := observer.mutations[edgeIndex]
	if edge.RolledBack {
		return &Phase0AError{Kind: Phase0AErrorRolledBackReference, Namespace: observer.run, Detail: "accepted direct edge was rolled back"}
	}
	if edge.Kind != Phase0AMutationEdge || edge.Occurrence != expression.Occurrence || edge.Edge != expression.Edge {
		return &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "accepted direct edge index mismatch"}
	}
	return nil
}

// CapturePhase0ASelectionCapability authenticates one current head with a
// run-scoped serial that does not rewind after rollback. The record follows
// the parser transaction that was active at capture time.
func (core *Core) CapturePhase0ASelectionCapability(head Head) (Phase0AAcceptedSelectionCapability, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if !phase0AObservers.active {
		return Phase0AAcceptedSelectionCapability{head: head}, nil
	}
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active {
		return Phase0AAcceptedSelectionCapability{head: head}, nil
	}
	if observer.failure != nil {
		return Phase0AAcceptedSelectionCapability{}, phase0AExistingFailureLocked(observer)
	}
	if _, err := core.node(head.Node); err != nil {
		return Phase0AAcceptedSelectionCapability{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "selection capability head is unavailable"})
	}
	if observer.route.nextCapability == math.MaxUint64 {
		return Phase0AAcceptedSelectionCapability{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterSelectionCapability, Namespace: observer.run})
	}
	if err := phase0AReserveSelectionCapabilityLocked(observer); err != nil {
		return Phase0AAcceptedSelectionCapability{}, err
	}
	observer.route.nextCapability++
	capability := Phase0AAcceptedSelectionCapability{
		coreInstance: observer.run.CoreInstance, runGeneration: observer.run.RunGeneration,
		serial: observer.route.nextCapability, head: head,
	}
	observer.route.capabilities = append(observer.route.capabilities, Phase0ASelectionCapabilityRecord{
		Namespace: observer.run, Serial: capability.serial,
		TransactionID: phase0ACurrentTransaction(observer), Head: head.Node,
	})
	return capability, nil
}

func phase0AValidateSelectionCapabilityLocked(observer *phase0AObserver, capability Phase0AAcceptedSelectionCapability) (Head, error) {
	namespace := CoreRunNamespace{CoreInstance: capability.coreInstance, RunGeneration: capability.runGeneration}
	if namespace != observer.run || capability.serial == 0 || capability.serial > uint64(len(observer.route.capabilities)) {
		return Head{}, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "selection capability is not current in this run"}
	}
	record := observer.route.capabilities[capability.serial-1]
	if record.Namespace != observer.run || record.Serial != capability.serial || record.Head != capability.head.Node || record.RolledBack {
		return Head{}, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "selection capability was rolled back or replaced"}
	}
	return capability.head, nil
}

// ObservePhase0AAcceptedSelection freezes one exact accepted physical spine.
// It is called only after the production diagnostic scheduler has proved one
// accepted head and one derivation. No selected-visible chain is claimed here.
func (core *Core) ObservePhase0AAcceptedSelection(capability Phase0AAcceptedSelectionCapability) error {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if !phase0AObservers.active {
		return nil
	}
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active {
		return nil
	}
	if observer.failure != nil {
		return phase0AExistingFailureLocked(observer)
	}
	if len(core.transactions) != 0 || core.schedulerFrame.active || len(observer.frames) != 0 {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "accepted selection observed inside a transaction"})
	}
	head, err := phase0AValidateSelectionCapabilityLocked(observer, capability)
	if err != nil {
		return phase0AStickyLocked(observer, err.(*Phase0AError))
	}
	if _, err := core.node(head.Node); err != nil {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorStaleReference, Namespace: observer.run, Detail: "selection capability head is unavailable"})
	}
	production, err := core.Derivations(head)
	if err != nil {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorUnsupportedProof, Namespace: observer.run, Detail: err.Error()})
	}
	physical, err := phase0AEnumeratePhysicalDerivations(core, head)
	if err != nil {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorUnsupportedProof, Namespace: observer.run, Detail: err.Error()})
	}
	if len(production) != 1 || len(physical) != 1 {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "accepted selection is not one exact physical derivation"})
	}
	want, got := production[0], physical[0]
	if !phase0ASameSubtrees(want.Payloads, got.payloads) || want.Score != got.score || want.BranchOrder != got.branchOrder || want.HasBranchOrder != got.hasBranchOrder {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAmbiguousReference, Namespace: observer.run, Detail: "accepted physical spine differs from production derivation"})
	}
	if observer.route.nextSelection == math.MaxUint64 {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterAcceptedSelectionGeneration, Namespace: observer.run})
	}
	if uint64(len(got.links)) > math.MaxUint32 {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Namespace: observer.run, Detail: "accepted physical spine width"})
	}
	resolved := make([]Phase0AAcceptedLinkRecord, len(got.links))
	for index, link := range got.links {
		bound, err := phase0AExactBindingLocked(observer, link)
		if err != nil {
			return phase0AStickyLocked(observer, err.(*Phase0AError))
		}
		selectedLower := LinkID(0)
		if index > 0 {
			selectedLower = got.links[index-1]
		}
		direct, err := phase0AResolveAcceptedExpressionLocked(observer, bound, selectedLower)
		if err != nil {
			return phase0AStickyLocked(observer, err.(*Phase0AError))
		}
		if err := phase0AValidateDirectOccurrenceLocked(observer, direct); err != nil {
			return phase0AStickyLocked(observer, err.(*Phase0AError))
		}
		resolved[index] = Phase0AAcceptedLinkRecord{
			Ordinal: uint32(index), Link: link, Payload: got.payloads[index], BoundExpression: bound,
			ResolvedExpression: direct.ID, Occurrence: direct.Occurrence, Edge: direct.Edge,
		}
	}
	if err := phase0AReserveRouteRowsLocked(observer, 0, 0, 1, uint64(len(resolved))); err != nil {
		return err
	}
	observer.route.nextSelection++
	generation := observer.route.nextSelection
	first := uint64(len(observer.route.acceptedLinks))
	observer.route.acceptedSelections = append(observer.route.acceptedSelections, Phase0AAcceptedSelectionRecord{
		Namespace: observer.run, Generation: generation, Capability: capability.serial,
		Head: head.Node, FirstLink: first, LinkCount: uint32(len(resolved)),
	})
	for index := range resolved {
		resolved[index].Namespace, resolved[index].Generation = observer.run, generation
		observer.route.acceptedLinks = append(observer.route.acceptedLinks, resolved[index])
	}
	return nil
}
