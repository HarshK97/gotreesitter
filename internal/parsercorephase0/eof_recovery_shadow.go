//go:build gts_eof_recovery_shadow

package parsercorephase0

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"
)

const (
	// DiagnosticEOFRecoveryMaxPayloads bounds one private recover_eof fold.
	DiagnosticEOFRecoveryMaxPayloads = 4096
	// DiagnosticEOFRecoveryMaxCloneBytes bounds all requested clone storage.
	DiagnosticEOFRecoveryMaxCloneBytes = 16 << 20
)

// DiagnosticEOFRecoveryForkReceipt proves one bounded private fold. The
// equality fields cover only copied headers, arenas, and work.
type DiagnosticEOFRecoveryForkReceipt struct {
	Steps                    uint32
	MaxSteps                 uint32
	Payloads                 uint32
	MaxPayloads              uint32
	SourceFootprintBytes     uint64
	CoreHeaderBytes          uint64
	CopiedArenaBytes         uint64
	AppendReserveBytes       uint64
	MapBytes                 uint64
	TemporaryBytes           uint64
	PreservationBytes        uint64
	PeakCloneBytes           uint64
	MaxCloneBytes            uint64
	StartByte                uint32
	EndByte                  uint32
	SubtreesBefore           uint32
	SubtreesAfter            uint32
	ChildrenBefore           uint32
	ChildrenAfter            uint32
	CheckpointMapEntries     uint32
	RetainedSelectedPolicy   bool
	SourceSchedulerActive    bool
	SchedulerFrameDetached   bool
	ProviderPointersDetached bool
	CopiedArenaPrefixesEqual bool
	CopiedHeadersEqual       bool
	RootChildrenExact        bool
	MutableStorageDisjoint   bool
	WorkBefore               Work
	WorkAfter                Work
}

type diagnosticEOFRecoveryClonePlan struct {
	coreHeaderBytes    uint64
	copiedArenaBytes   uint64
	appendReserveBytes uint64
	mapBytes           uint64
	temporaryBytes     uint64
	preservationBytes  uint64
	peakBytes          uint64
}

// ForkDiagnosticEOFRecovery copies the stable compact arenas and appends one
// private ERROR root. It does not retain a live parser or provider pointer.
func ForkDiagnosticEOFRecovery(
	live *Core,
	head Head,
	payloads []SubtreeID,
) (*Core, SubtreeID, DiagnosticEOFRecoveryForkReceipt, error) {
	receipt := DiagnosticEOFRecoveryForkReceipt{
		MaxSteps:      1,
		MaxPayloads:   DiagnosticEOFRecoveryMaxPayloads,
		MaxCloneBytes: DiagnosticEOFRecoveryMaxCloneBytes,
	}
	if live == nil {
		return nil, 0, receipt, errors.New("parser-core phase zero: nil EOF recovery source")
	}
	if _, err := live.node(head.Node); err != nil {
		return nil, 0, receipt, err
	}
	if len(payloads) == 0 || len(payloads) > DiagnosticEOFRecoveryMaxPayloads {
		return nil, 0, receipt, fmt.Errorf(
			"parser-core phase zero: EOF recovery payload count %d is outside 1..%d",
			len(payloads), DiagnosticEOFRecoveryMaxPayloads,
		)
	}

	receipt.Payloads = uint32(len(payloads))
	receipt.SourceFootprintBytes = live.FootprintBytes()
	receipt.CheckpointMapEntries = uint32(len(live.checkpoints.buckets))
	receipt.RetainedSelectedPolicy = live.selectedPolicy != nil
	receipt.SourceSchedulerActive = live.schedulerFrame.active
	plan, err := planDiagnosticEOFRecoveryClone(live, len(payloads))
	receipt.CoreHeaderBytes = plan.coreHeaderBytes
	receipt.CopiedArenaBytes = plan.copiedArenaBytes
	receipt.AppendReserveBytes = plan.appendReserveBytes
	receipt.MapBytes = plan.mapBytes
	receipt.TemporaryBytes = plan.temporaryBytes
	receipt.PreservationBytes = plan.preservationBytes
	receipt.PeakCloneBytes = plan.peakBytes
	if err != nil {
		return nil, 0, receipt, err
	}

	shadow := cloneDiagnosticEOFRecoveryCore(live, len(payloads))
	receipt.SchedulerFrameDetached = !shadow.schedulerFrame.active && len(shadow.transactions) == 0
	receipt.ProviderPointersDetached = shadow.tables == nil && shadow.plans == nil && shadow.selectedProvider == nil
	receipt.MutableStorageDisjoint = diagnosticEOFRecoveryStorageDisjoint(live, shadow)
	if !receipt.MutableStorageDisjoint {
		return nil, 0, receipt, errors.New("parser-core phase zero: EOF recovery clone aliases copied storage")
	}

	first, err := shadow.subtree(payloads[0])
	if err != nil {
		return nil, 0, receipt, err
	}
	last, err := shadow.subtree(payloads[len(payloads)-1])
	if err != nil {
		return nil, 0, receipt, err
	}
	if last.endByte < first.startByte {
		return nil, 0, receipt, errors.New("parser-core phase zero: EOF recovery payload span is reversed")
	}

	receipt.StartByte = first.startByte
	receipt.EndByte = last.endByte
	receipt.SubtreesBefore = uint32(len(shadow.subtrees))
	receipt.ChildrenBefore = uint32(len(shadow.children))
	receipt.WorkBefore = shadow.Work()
	root, err := shadow.appendSubtreeRecord(subtreeRecord{
		symbol: ErrorRegionSymbol, startByte: first.startByte, endByte: last.endByte,
	}, payloads, nil, nil)
	if err != nil {
		return nil, 0, receipt, err
	}
	receipt.Steps = 1
	receipt.SubtreesAfter = uint32(len(shadow.subtrees))
	receipt.ChildrenAfter = uint32(len(shadow.children))
	receipt.CopiedArenaPrefixesEqual = diagnosticEOFRecoveryCopiedArenasEqual(live, shadow)
	receipt.CopiedHeadersEqual = diagnosticEOFRecoveryCopiedHeadersEqual(live, shadow)
	receipt.RootChildrenExact = diagnosticEOFRecoveryRootChildrenEqual(shadow, root, payloads)
	receipt.WorkAfter = shadow.Work()
	return shadow, root, receipt, nil
}

func planDiagnosticEOFRecoveryClone(live *Core, payloadCount int) (diagnosticEOFRecoveryClonePlan, error) {
	plan := diagnosticEOFRecoveryClonePlan{coreHeaderBytes: uint64(unsafe.Sizeof(Core{}))}
	if live.selectedPolicy != nil {
		return plan, errors.New("parser-core phase zero: EOF recovery clone does not support a retained selected policy")
	}
	if len(live.checkpoints.buckets) != 0 {
		return plan, errors.New("parser-core phase zero: EOF recovery clone does not support a nonempty checkpoint map")
	}
	if len(live.transactions) != 0 || live.condenseScopeActive {
		return plan, errors.New("parser-core phase zero: EOF recovery source has active transactional state")
	}
	if !live.metadataConstructionAuthenticated {
		return plan, errors.New("parser-core phase zero: EOF recovery source metadata is not authenticated")
	}

	for _, item := range []struct {
		count int
		bytes uint64
	}{
		{len(live.nodes), coreNodeRecordBytes},
		{len(live.nodeLineages), coreNodeLineageRecordBytes},
		{len(live.nodeCheckpoints), coreCheckpointIDBytes},
		{len(live.links), coreLinkRecordBytes},
		{len(live.subtrees), coreSubtreeRecordBytes},
		{len(live.externalProvenance), coreExternalProvenanceBytes},
		{len(live.children), coreChildRecordBytes},
		{len(live.fields), coreFieldRecordBytes},
		{len(live.aliases), coreAliasRecordBytes},
		{len(live.checkpoints.records), coreCheckpointRecordBytes},
		{len(live.checkpoints.bytes), 1},
		{len(live.boundaries.slots), coreBoundarySlotBytes},
		{len(live.alternativeSpillArena), coreUint32Bytes},
	} {
		bytes, ok := diagnosticEOFRecoveryMul(uint64(item.count), item.bytes)
		if !ok || !diagnosticEOFRecoveryAdd(&plan.copiedArenaBytes, bytes) {
			return plan, errors.New("parser-core phase zero: EOF recovery copied-arena byte count overflow")
		}
	}
	if !diagnosticEOFRecoveryAdd(&plan.appendReserveBytes, coreSubtreeRecordBytes) {
		return plan, errors.New("parser-core phase zero: EOF recovery subtree reserve overflow")
	}
	childReserve, ok := diagnosticEOFRecoveryMul(uint64(payloadCount), coreChildRecordBytes)
	if !ok || !diagnosticEOFRecoveryAdd(&plan.appendReserveBytes, childReserve) {
		return plan, errors.New("parser-core phase zero: EOF recovery child reserve overflow")
	}

	plan.peakBytes = plan.coreHeaderBytes
	for _, bytes := range []uint64{
		plan.copiedArenaBytes,
		plan.appendReserveBytes,
		plan.mapBytes,
		plan.temporaryBytes,
		plan.preservationBytes,
	} {
		if !diagnosticEOFRecoveryAdd(&plan.peakBytes, bytes) {
			return plan, errors.New("parser-core phase zero: EOF recovery peak byte count overflow")
		}
	}
	if plan.peakBytes > DiagnosticEOFRecoveryMaxCloneBytes {
		return plan, fmt.Errorf(
			"parser-core phase zero: EOF recovery clone peak %d exceeds %d",
			plan.peakBytes, DiagnosticEOFRecoveryMaxCloneBytes,
		)
	}
	return plan, nil
}

func cloneDiagnosticEOFRecoveryCore(live *Core, payloadCount int) *Core {
	shadow := new(Core)
	*shadow = *live
	shadow.tables = nil
	shadow.plans = nil
	shadow.selectedProvider = nil
	shadow.selectedPolicy = nil
	shadow.nodes = cloneDiagnosticSlice(live.nodes, 0)
	shadow.nodeLineages = cloneDiagnosticSlice(live.nodeLineages, 0)
	shadow.nodeCheckpoints = cloneDiagnosticSlice(live.nodeCheckpoints, 0)
	shadow.links = cloneDiagnosticSlice(live.links, 0)
	shadow.subtrees = cloneDiagnosticSlice(live.subtrees, 1)
	shadow.externalProvenance = cloneDiagnosticSlice(live.externalProvenance, 0)
	shadow.children = cloneDiagnosticSlice(live.children, payloadCount)
	shadow.fields = cloneDiagnosticSlice(live.fields, 0)
	shadow.aliases = cloneDiagnosticSlice(live.aliases, 0)
	shadow.checkpoints.records = cloneDiagnosticSlice(live.checkpoints.records, 0)
	shadow.checkpoints.bytes = cloneDiagnosticSlice(live.checkpoints.bytes, 0)
	shadow.checkpoints.buckets = nil
	shadow.boundaries.slots = cloneDiagnosticSlice(live.boundaries.slots, 0)
	shadow.alternativeSpillArena = cloneDiagnosticSlice(live.alternativeSpillArena, 0)

	// One append does not use live journals, schedulers, or retained builders.
	shadow.boundaryJournal = nil
	shadow.nodeLineageJournal = nil
	shadow.condenseCandidates = nil
	shadow.condenseNewNode = 0
	shadow.condenseScopeActive = false
	shadow.reductionSourceOwner = 0
	shadow.transactions = nil
	shadow.popScratch = popEnumerationScratch{}
	shadow.reductionScratch = reductionOutputScratch{}
	shadow.historicalNodeScratch = nil
	shadow.cohortHeadScratch = nil
	shadow.factorLinkScratch = nil
	shadow.selectedBuild = selectedStoreBuildScratch{}
	shadow.selectedPoolMu = sync.Mutex{}
	shadow.selectedPool = selectedStoreBacking{}
	shadow.schedulerFrame = schedulerTransactionFrame{}
	return shadow
}

func cloneDiagnosticSlice[T any](source []T, extraCapacity int) []T {
	if len(source) == 0 && extraCapacity == 0 {
		return nil
	}
	out := make([]T, len(source), len(source)+extraCapacity)
	copy(out, source)
	return out
}

func diagnosticEOFRecoveryCopiedArenasEqual(live, shadow *Core) bool {
	if live == nil || shadow == nil || len(shadow.subtrees) < len(live.subtrees) || len(shadow.children) < len(live.children) {
		return false
	}
	return equalDiagnosticSlice(live.nodes, shadow.nodes) &&
		equalDiagnosticSlice(live.nodeLineages, shadow.nodeLineages) &&
		equalDiagnosticSlice(live.nodeCheckpoints, shadow.nodeCheckpoints) &&
		equalDiagnosticSlice(live.links, shadow.links) &&
		equalDiagnosticSlice(live.subtrees, shadow.subtrees[:len(live.subtrees)]) &&
		equalDiagnosticSlice(live.externalProvenance, shadow.externalProvenance) &&
		equalDiagnosticSlice(live.children, shadow.children[:len(live.children)]) &&
		equalDiagnosticSlice(live.fields, shadow.fields) &&
		equalDiagnosticSlice(live.aliases, shadow.aliases) &&
		equalDiagnosticSlice(live.checkpoints.records, shadow.checkpoints.records) &&
		equalDiagnosticSlice(live.checkpoints.bytes, shadow.checkpoints.bytes) &&
		equalDiagnosticSlice(live.boundaries.slots, shadow.boundaries.slots) &&
		equalDiagnosticSlice(live.alternativeSpillArena, shadow.alternativeSpillArena)
}

func diagnosticEOFRecoveryCopiedHeadersEqual(live, shadow *Core) bool {
	if live == nil || shadow == nil {
		return false
	}
	return live.limits == shadow.limits &&
		live.diagnostics == shadow.diagnostics &&
		live.frontier == shadow.frontier &&
		live.checkpoint == shadow.checkpoint &&
		live.nextTransaction == shadow.nextTransaction &&
		live.classificationPhase == shadow.classificationPhase &&
		live.metadataConstructionAuthenticated == shadow.metadataConstructionAuthenticated &&
		live.reduceConflictContext == shadow.reduceConflictContext &&
		live.reduceNoLookaheadContext == shadow.reduceNoLookaheadContext &&
		live.externalPayloadsQuiescent == shadow.externalPayloadsQuiescent &&
		live.externalTokenScannerStart == shadow.externalTokenScannerStart &&
		live.externalTokenScannerEnd == shadow.externalTokenScannerEnd &&
		live.externalTokenScannerExact == shadow.externalTokenScannerExact
}

func diagnosticEOFRecoveryRootChildrenEqual(shadow *Core, root SubtreeID, payloads []SubtreeID) bool {
	record, err := shadow.subtree(root)
	if err != nil || uint64(record.firstChild)+uint64(record.childCount) > uint64(len(shadow.children)) {
		return false
	}
	children := shadow.children[record.firstChild : record.firstChild+record.childCount]
	return equalDiagnosticSlice(children, payloads)
}

func diagnosticEOFRecoveryStorageDisjoint(live, shadow *Core) bool {
	if live == nil || shadow == nil || live == shadow {
		return false
	}
	return disjointDiagnosticSlice(live.nodes, shadow.nodes) &&
		disjointDiagnosticSlice(live.nodeLineages, shadow.nodeLineages) &&
		disjointDiagnosticSlice(live.nodeCheckpoints, shadow.nodeCheckpoints) &&
		disjointDiagnosticSlice(live.links, shadow.links) &&
		disjointDiagnosticSlice(live.subtrees, shadow.subtrees) &&
		disjointDiagnosticSlice(live.externalProvenance, shadow.externalProvenance) &&
		disjointDiagnosticSlice(live.children, shadow.children) &&
		disjointDiagnosticSlice(live.fields, shadow.fields) &&
		disjointDiagnosticSlice(live.aliases, shadow.aliases) &&
		disjointDiagnosticSlice(live.checkpoints.records, shadow.checkpoints.records) &&
		disjointDiagnosticSlice(live.checkpoints.bytes, shadow.checkpoints.bytes) &&
		disjointDiagnosticSlice(live.boundaries.slots, shadow.boundaries.slots) &&
		disjointDiagnosticSlice(live.alternativeSpillArena, shadow.alternativeSpillArena)
}

func disjointDiagnosticSlice[T any](left, right []T) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	return &left[0] != &right[0]
}

func equalDiagnosticSlice[T comparable](left, right []T) bool {
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

func diagnosticEOFRecoveryMul(left, right uint64) (uint64, bool) {
	if left != 0 && right > math.MaxUint64/left {
		return 0, false
	}
	return left * right, true
}

func diagnosticEOFRecoveryAdd(total *uint64, value uint64) bool {
	if total == nil || value > math.MaxUint64-*total {
		return false
	}
	*total += value
	return true
}
