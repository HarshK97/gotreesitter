//go:build gts_eof_recovery_shadow

package parsercorephase0

import (
	"strings"
	"testing"
	"unsafe"
)

func TestDiagnosticEOFRecoveryClonePlanAccountsEveryRequestedByte(t *testing.T) {
	live := &Core{
		metadataConstructionAuthenticated: true,
		nodes:                             make([]nodeRecord, 2),
		nodeLineages:                      make([]nodeLineageRecord, 2),
		nodeCheckpoints:                   make([]CheckpointID, 2),
		links:                             make([]linkRecord, 3),
		subtrees:                          make([]subtreeRecord, 4),
		externalProvenance:                make([]externalPayloadProvenance, 1),
		children:                          make([]SubtreeID, 5),
		fields:                            make([]FieldMapEntry, 6),
		aliases:                           make([]Symbol, 7),
		checkpoints: checkpointInterner{
			records: make([]checkpointRecord, 2),
			bytes:   make([]byte, 9),
		},
		boundaries:            boundaryIndex{slots: make([]boundarySlot, 8)},
		alternativeSpillArena: make([]uint32, 10),
	}
	const payloads = 11
	plan, err := planDiagnosticEOFRecoveryClone(live, payloads)
	if err != nil {
		t.Fatalf("plan clone: %v", err)
	}
	wantCopied := uint64(2)*coreNodeRecordBytes +
		uint64(2)*coreNodeLineageRecordBytes +
		uint64(2)*coreCheckpointIDBytes +
		uint64(3)*coreLinkRecordBytes +
		uint64(4)*coreSubtreeRecordBytes +
		coreExternalProvenanceBytes +
		uint64(5)*coreChildRecordBytes +
		uint64(6)*coreFieldRecordBytes +
		uint64(7)*coreAliasRecordBytes +
		uint64(2)*coreCheckpointRecordBytes +
		9 +
		uint64(8)*coreBoundarySlotBytes +
		uint64(10)*coreUint32Bytes
	wantAppend := coreSubtreeRecordBytes + uint64(payloads)*coreChildRecordBytes
	wantPeak := uint64(unsafe.Sizeof(Core{})) + wantCopied + wantAppend
	if plan.coreHeaderBytes != uint64(unsafe.Sizeof(Core{})) ||
		plan.copiedArenaBytes != wantCopied || plan.appendReserveBytes != wantAppend ||
		plan.mapBytes != 0 || plan.temporaryBytes != 0 || plan.preservationBytes != 0 ||
		plan.peakBytes != wantPeak {
		t.Fatalf("clone plan=%+v want copied=%d append=%d peak=%d", plan, wantCopied, wantAppend, wantPeak)
	}
}

func TestDiagnosticEOFRecoveryClonePlanRejectsRetainedState(t *testing.T) {
	live := &Core{metadataConstructionAuthenticated: true, selectedPolicy: &SelectedStorePolicy{}}
	if _, err := planDiagnosticEOFRecoveryClone(live, 1); err == nil || !strings.Contains(err.Error(), "selected policy") {
		t.Fatalf("retained selected policy error=%v", err)
	}
	live.selectedPolicy = nil
	live.checkpoints.buckets = map[[32]byte]CheckpointID{{1}: 1}
	if _, err := planDiagnosticEOFRecoveryClone(live, 1); err == nil || !strings.Contains(err.Error(), "checkpoint map") {
		t.Fatalf("nonempty checkpoint map error=%v", err)
	}
}
