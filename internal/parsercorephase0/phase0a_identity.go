//go:build gts_parsercorephase0 && gts_parsercorephase0a

package parsercorephase0

import (
	"fmt"
	"math"
	"sync"
)

type CoreRunNamespace struct {
	CoreInstance  uint64
	RunGeneration uint64
}

type AttemptKey struct {
	Run          CoreRunNamespace
	AttemptEpoch uint32
}

type ConstructionEventKey struct {
	Attempt AttemptKey
	Serial  uint64
}

type IncomingEdgeKey struct {
	Event  ConstructionEventKey
	Serial uint64
}

// ConstructionOccurrenceKey identifies one occurrence within an event.
type ConstructionOccurrenceKey struct {
	Event ConstructionEventKey
	Slot  uint32
}

type Phase0AConstructionKind uint8

const (
	Phase0AConstructionOrdinaryTerminal Phase0AConstructionKind = iota + 1
	Phase0AConstructionExtraTerminal
)

type Phase0ARollbackCause uint8

const (
	Phase0ARollbackUnknown Phase0ARollbackCause = iota
	Phase0ARollbackReturnedError
	Phase0ARollbackPanic
	Phase0ARollbackSchedulerPoison
)

type Phase0APoisonKind uint8

const (
	Phase0APoisonReturnedError Phase0APoisonKind = iota + 1
	Phase0APoisonPanic
	Phase0APoisonWrongCore
	Phase0APoisonStaleToken
	Phase0APoisonNonTopToken
	Phase0APoisonNestedScheduler
)

type Phase0AMutationKind uint8

const (
	Phase0AMutationEvent Phase0AMutationKind = iota + 1
	// Phase0AMutationEdge is reserved for a terminal occurrence-bound candidate
	// edge. Publication, duplicate, replacement, and selection remain unproven.
	Phase0AMutationEdge
	Phase0AMutationOccurrence
	// Phase0AMutationScaffoldEdge is an identity/lifetime test scaffold. It is
	// excluded from semantic construction and incoming-edge proof.
	Phase0AMutationScaffoldEdge
)

type Phase0AMutationRecord struct {
	Kind                Phase0AMutationKind
	TransactionID       uint64
	Event               ConstructionEventKey
	Edge                IncomingEdgeKey
	Occurrence          ConstructionOccurrenceKey
	Payload             SubtreeID
	Predecessor         NodeID
	ConstructionKind    Phase0AConstructionKind
	RolledBack          bool
	RollbackTransaction uint64
	RollbackCause       Phase0ARollbackCause
}

type Phase0ATransactionFrame struct {
	TransactionID uint64
	ParentID      uint64
	MutationStart uint64
}

type Phase0APoisonRecord struct {
	TransactionID uint64
	Kind          Phase0APoisonKind
}

type Phase0AProofSnapshot struct {
	Namespace CoreRunNamespace
	// Mutations includes explicit scaffold rows. Semantic construction proof
	// must exclude Phase0AMutationScaffoldEdge.
	Mutations       []Phase0AMutationRecord
	Frames          []Phase0ATransactionFrame
	OccurrenceCount uint64
	OccurrenceBytes uint64
	FirstPoison     *Phase0APoisonRecord
	Failure         *Phase0AError
}

type Phase0AErrorKind string

const (
	Phase0AErrorCounterOverflow   Phase0AErrorKind = "counter_overflow"
	Phase0AErrorUnregisteredCore  Phase0AErrorKind = "unregistered_core"
	Phase0AErrorRunActive         Phase0AErrorKind = "run_active"
	Phase0AErrorStaleNamespace    Phase0AErrorKind = "stale_namespace"
	Phase0AErrorInvalidEvent      Phase0AErrorKind = "invalid_event"
	Phase0AErrorInvalidOccurrence Phase0AErrorKind = "invalid_occurrence"
	Phase0AErrorSessionInactive   Phase0AErrorKind = "session_inactive"
	Phase0AErrorSessionActive     Phase0AErrorKind = "session_active"
	Phase0AErrorSessionHasRuns    Phase0AErrorKind = "session_has_active_runs"
	Phase0AErrorCoreCap           Phase0AErrorKind = "core_cap"
	Phase0AErrorRecordCap         Phase0AErrorKind = "record_cap"
	Phase0AErrorByteCap           Phase0AErrorKind = "byte_cap"
	Phase0AErrorFrameCap          Phase0AErrorKind = "frame_cap"
	Phase0AErrorMutationCap       Phase0AErrorKind = "mutation_cap"
	Phase0AErrorOccurrenceCap     Phase0AErrorKind = "occurrence_cap"
	Phase0AErrorOccurrenceByteCap Phase0AErrorKind = "occurrence_byte_cap"
	Phase0AErrorTransactionProof  Phase0AErrorKind = "transaction_proof"
	Phase0AErrorAttemptUnproven   Phase0AErrorKind = "attempt_unproven"
)

type Phase0ACounter string

const (
	Phase0ACounterCoreInstance   Phase0ACounter = "core_instance"
	Phase0ACounterRunGeneration  Phase0ACounter = "run_generation"
	Phase0ACounterEventSerial    Phase0ACounter = "event_serial"
	Phase0ACounterEdgeSerial     Phase0ACounter = "edge_serial"
	Phase0ACounterOccurrenceSlot Phase0ACounter = "occurrence_slot"
)

type Phase0AError struct {
	Kind      Phase0AErrorKind
	Counter   Phase0ACounter
	Namespace CoreRunNamespace
	Detail    string
}

func (e *Phase0AError) Error() string {
	if e == nil {
		return "parser-core phase zero A: <nil>"
	}
	if e.Counter != "" {
		return fmt.Sprintf("parser-core phase zero A: %s: %s", e.Kind, e.Counter)
	}
	if e.Detail != "" {
		return fmt.Sprintf("parser-core phase zero A: %s: %s", e.Kind, e.Detail)
	}
	return "parser-core phase zero A: " + string(e.Kind)
}

// Phase0AObserverLimits are fixed for one explicit observer session.
type Phase0AObserverLimits struct {
	MaxCores           uint64
	MaxRecords         uint64
	MaxBytes           uint64
	MaxFrames          uint64
	MaxMutations       uint64
	MaxOccurrences     uint64
	MaxOccurrenceBytes uint64
}

const (
	phase0AEventRecordBytes      uint64 = 40
	phase0AEdgeRecordBytes       uint64 = 48
	phase0AOccurrenceRecordBytes uint64 = 88
	phase0AFrameRecordBytes      uint64 = 24
)

type phase0AObserver struct {
	coreInstance    uint64
	runGeneration   uint64
	run             CoreRunNamespace
	nextEvent       uint64
	nextEdge        uint64
	attempt         uint32
	active          bool
	frames          []Phase0ATransactionFrame
	mutations       []Phase0AMutationRecord
	eventIndex      map[ConstructionEventKey]uint64
	edgeIndex       map[IncomingEdgeKey]uint64
	occurrenceIndex map[ConstructionOccurrenceKey]uint64
	occurrenceCount uint64
	occurrenceBytes uint64
	firstPoison     *Phase0APoisonRecord
	failure         *Phase0AError
	pendingRollback Phase0ARollbackCause
}

var phase0AObservers = struct {
	sync.Mutex
	nextCoreInstance uint64
	active           bool
	limits           Phase0AObserverLimits
	records          uint64
	bytes            uint64
	failure          *Phase0AError
	byCore           map[*Core]*phase0AObserver
}{
	byCore: make(map[*Core]*phase0AObserver),
}

// BeginPhase0AObserverSession starts the tag-only observer lifetime.
func BeginPhase0AObserverSession(limits Phase0AObserverLimits) error {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if phase0AObservers.active {
		return &Phase0AError{Kind: Phase0AErrorSessionActive}
	}
	phase0AObservers.active = true
	phase0AObservers.limits = limits
	phase0AObservers.records = 0
	phase0AObservers.bytes = 0
	phase0AObservers.failure = nil
	phase0AObservers.byCore = make(map[*Core]*phase0AObserver)
	return nil
}

// EndPhase0AObserverSession clears all pointer identity after proving no run is active.
func EndPhase0AObserverSession() error {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if !phase0AObservers.active {
		return &Phase0AError{Kind: Phase0AErrorSessionInactive}
	}
	for _, observer := range phase0AObservers.byCore {
		if observer.active {
			return &Phase0AError{Kind: Phase0AErrorSessionHasRuns, Namespace: observer.run}
		}
	}
	phase0AObservers.active = false
	phase0AObservers.limits = Phase0AObserverLimits{}
	phase0AObservers.records = 0
	phase0AObservers.bytes = 0
	phase0AObservers.failure = nil
	phase0AObservers.byCore = nil
	return nil
}

func phase0AInvalidateCore(core *Core) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if observer := phase0AObservers.byCore[core]; observer != nil {
		observer.active = false
		observer.frames = nil
		observer.mutations = nil
		observer.eventIndex = nil
		observer.edgeIndex = nil
		observer.occurrenceIndex = nil
		observer.occurrenceCount = 0
		observer.occurrenceBytes = 0
		observer.firstPoison = nil
		observer.failure = nil
		observer.pendingRollback = Phase0ARollbackUnknown
	}
}

func (c *Core) BeginRun() (CoreRunNamespace, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if !phase0AObservers.active {
		return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorSessionInactive}
	}
	if phase0AObservers.failure != nil {
		copy := *phase0AObservers.failure
		return CoreRunNamespace{}, &copy
	}
	if c == nil {
		return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorUnregisteredCore, Detail: "nil core"}
	}
	observer := phase0AObservers.byCore[c]
	if observer == nil {
		if uint64(len(phase0AObservers.byCore)) >= phase0AObservers.limits.MaxCores {
			return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorCoreCap}
		}
		if phase0AObservers.nextCoreInstance == math.MaxUint64 {
			err := &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterCoreInstance}
			copy := *err
			phase0AObservers.failure = &copy
			return CoreRunNamespace{}, err
		}
		phase0AObservers.nextCoreInstance++
		observer = &phase0AObserver{coreInstance: phase0AObservers.nextCoreInstance}
		phase0AObservers.byCore[c] = observer
	}
	if observer.active {
		return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorRunActive, Namespace: observer.run}
	}
	if observer.failure != nil {
		return CoreRunNamespace{}, phase0AExistingFailureLocked(observer)
	}
	if observer.runGeneration == math.MaxUint64 {
		return CoreRunNamespace{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterRunGeneration, Namespace: observer.run})
	}
	observer.runGeneration++
	observer.run = CoreRunNamespace{CoreInstance: observer.coreInstance, RunGeneration: observer.runGeneration}
	observer.nextEvent, observer.nextEdge, observer.attempt, observer.active = 0, 0, 1, true
	observer.frames = nil
	observer.mutations = nil
	observer.eventIndex = make(map[ConstructionEventKey]uint64)
	observer.edgeIndex = make(map[IncomingEdgeKey]uint64)
	observer.occurrenceIndex = make(map[ConstructionOccurrenceKey]uint64)
	observer.occurrenceCount = 0
	observer.occurrenceBytes = 0
	observer.firstPoison = nil
	observer.failure = nil
	observer.pendingRollback = Phase0ARollbackUnknown
	return observer.run, nil
}

func (c *Core) EndRun(namespace CoreRunNamespace) error {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForRunLocked(c, namespace)
	if err != nil {
		return err
	}
	if !observer.active {
		if observer.failure != nil {
			return phase0AExistingFailureLocked(observer)
		}
		return &Phase0AError{Kind: Phase0AErrorStaleNamespace, Namespace: namespace}
	}
	if len(c.transactions) != 0 || c.schedulerFrame.active {
		if observer.failure != nil {
			return phase0AExistingFailureLocked(observer)
		}
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: namespace, Detail: "end run with active parser transaction"})
	}
	if len(observer.frames) != 0 {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: namespace, Detail: "end run with active transaction"})
	}
	observer.active = false
	if observer.failure != nil {
		return phase0AExistingFailureLocked(observer)
	}
	return nil
}

// phase0AAttempt returns attempt 1 and rejects unsupported retry claims.
func phase0AAttempt(c *Core, namespace CoreRunNamespace, epoch uint32) (AttemptKey, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForNamespaceLocked(c, namespace)
	if err != nil {
		return AttemptKey{}, err
	}
	if epoch != observer.attempt || epoch != 1 {
		return AttemptKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: namespace})
	}
	return AttemptKey{Run: namespace, AttemptEpoch: 1}, nil
}

func phase0ANextConstructionEvent(c *Core, namespace CoreRunNamespace) (ConstructionEventKey, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForNamespaceLocked(c, namespace)
	if err != nil {
		return ConstructionEventKey{}, err
	}
	if observer.attempt != 1 {
		return ConstructionEventKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: namespace})
	}
	if observer.nextEvent == math.MaxUint64 {
		return ConstructionEventKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterEventSerial, Namespace: namespace})
	}
	if err := phase0AReserveMutationLocked(observer, phase0AEventRecordBytes); err != nil {
		return ConstructionEventKey{}, err
	}
	observer.nextEvent++
	event := ConstructionEventKey{Attempt: AttemptKey{Run: namespace, AttemptEpoch: 1}, Serial: observer.nextEvent}
	record := Phase0AMutationRecord{Kind: Phase0AMutationEvent, Event: event, TransactionID: phase0ACurrentTransaction(observer)}
	observer.eventIndex[event] = uint64(len(observer.mutations))
	observer.mutations = append(observer.mutations, record)
	return event, nil
}

// phase0ANextScaffoldEdge exercises edge serial and transaction lifetime only.
// It never authenticates a semantic construction occurrence or candidate edge.
func phase0ANextScaffoldEdge(c *Core, event ConstructionEventKey) (IncomingEdgeKey, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForNamespaceLocked(c, event.Attempt.Run)
	if err != nil {
		return IncomingEdgeKey{}, err
	}
	if event.Attempt.AttemptEpoch != 1 {
		return IncomingEdgeKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: observer.run})
	}
	index, ok := observer.eventIndex[event]
	if !ok || index >= uint64(len(observer.mutations)) {
		return IncomingEdgeKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidEvent, Namespace: observer.run, Detail: "event is not indexed"})
	}
	eventRecord := observer.mutations[index]
	if eventRecord.Kind != Phase0AMutationEvent || eventRecord.Event != event {
		return IncomingEdgeKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidEvent, Namespace: observer.run, Detail: "event index mismatch"})
	}
	if eventRecord.RolledBack {
		return IncomingEdgeKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidEvent, Namespace: observer.run, Detail: "event was rolled back"})
	}
	if observer.nextEdge == math.MaxUint64 {
		return IncomingEdgeKey{}, phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterEdgeSerial, Namespace: observer.run})
	}
	if err := phase0AReserveMutationLocked(observer, phase0AEdgeRecordBytes); err != nil {
		return IncomingEdgeKey{}, err
	}
	observer.nextEdge++
	edge := IncomingEdgeKey{Event: event, Serial: observer.nextEdge}
	record := Phase0AMutationRecord{Kind: Phase0AMutationScaffoldEdge, Event: event, Edge: edge, TransactionID: phase0ACurrentTransaction(observer)}
	observer.edgeIndex[edge] = uint64(len(observer.mutations))
	observer.mutations = append(observer.mutations, record)
	return edge, nil
}

func phase0AObserveTerminalShift(core *Core, payload SubtreeID, predecessor NodeID, extra bool) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.failure != nil {
		return
	}
	kind := Phase0AConstructionOrdinaryTerminal
	if extra {
		kind = Phase0AConstructionExtraTerminal
	}
	phase0AObserveTerminalConstructionLocked(core, observer, payload, kind, 1, func(uint64) NodeID {
		return predecessor
	})
}

func phase0AObserveTerminalCohortShift(core *Core, payload SubtreeID, boundaries []ClassifiedBoundary, extra bool) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.failure != nil {
		return
	}
	count := uint64(len(boundaries))
	if count == 0 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: observer.run, Detail: "terminal cohort has no occurrence"})
		return
	}
	if count > math.MaxUint32 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterOccurrenceSlot, Namespace: observer.run})
		return
	}
	for _, boundary := range boundaries {
		if boundary.owner != core {
			phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: observer.run, Detail: "terminal cohort boundary belongs to another core"})
			return
		}
	}
	kind := Phase0AConstructionOrdinaryTerminal
	if extra {
		kind = Phase0AConstructionExtraTerminal
	}
	phase0AObserveTerminalConstructionLocked(core, observer, payload, kind, count, func(index uint64) NodeID {
		return boundaries[index].head.Node
	})
}

func phase0AObserveTerminalConstructionLocked(core *Core, observer *phase0AObserver, payload SubtreeID, kind Phase0AConstructionKind, count uint64, predecessor func(uint64) NodeID) {
	if count > math.MaxUint32 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterOccurrenceSlot, Namespace: observer.run})
		return
	}
	if payload == 0 || predecessor == nil || count == 0 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: observer.run, Detail: "invalid terminal construction input"})
		return
	}
	subtree, err := core.subtree(payload)
	if err != nil || !subtree.terminal {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: observer.run, Detail: "terminal construction payload is not an existing terminal"})
		return
	}
	wantExtra := kind == Phase0AConstructionExtraTerminal
	if (kind != Phase0AConstructionOrdinaryTerminal && kind != Phase0AConstructionExtraTerminal) || subtree.extra != wantExtra {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: observer.run, Detail: "terminal construction payload kind mismatch"})
		return
	}
	for index := uint64(0); index < count; index++ {
		if _, err := core.node(predecessor(index)); err != nil {
			phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: observer.run, Detail: "terminal construction predecessor does not exist"})
			return
		}
	}
	if observer.attempt != 1 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: observer.run})
		return
	}
	if observer.nextEvent == math.MaxUint64 {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterEventSerial, Namespace: observer.run})
		return
	}
	if observer.nextEdge > math.MaxUint64-count {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterEdgeSerial, Namespace: observer.run})
		return
	}
	mutationCount := 1 + 2*count
	occurrenceBytes := count * (phase0AOccurrenceRecordBytes + phase0AEdgeRecordBytes)
	totalBytes := phase0AEventRecordBytes + occurrenceBytes
	if err := phase0AReserveConstructionLocked(observer, mutationCount, count, totalBytes, occurrenceBytes); err != nil {
		return
	}

	observer.nextEvent++
	event := ConstructionEventKey{
		Attempt: AttemptKey{Run: observer.run, AttemptEpoch: 1},
		Serial:  observer.nextEvent,
	}
	transaction := phase0ACurrentTransaction(observer)
	eventRecord := Phase0AMutationRecord{
		Kind: Phase0AMutationEvent, TransactionID: transaction,
		Event: event, Payload: payload, ConstructionKind: kind,
	}
	observer.eventIndex[event] = uint64(len(observer.mutations))
	observer.mutations = append(observer.mutations, eventRecord)

	for index := uint64(0); index < count; index++ {
		observer.nextEdge++
		occurrence := ConstructionOccurrenceKey{Event: event, Slot: uint32(index + 1)}
		edge := IncomingEdgeKey{Event: event, Serial: observer.nextEdge}
		record := Phase0AMutationRecord{
			TransactionID: transaction, Event: event, Edge: edge, Occurrence: occurrence,
			Payload: payload, Predecessor: predecessor(index), ConstructionKind: kind,
		}
		record.Kind = Phase0AMutationOccurrence
		observer.occurrenceIndex[occurrence] = uint64(len(observer.mutations))
		observer.mutations = append(observer.mutations, record)
		record.Kind = Phase0AMutationEdge
		observer.edgeIndex[edge] = uint64(len(observer.mutations))
		observer.mutations = append(observer.mutations, record)
	}
}

func phase0AReserveConstructionLocked(observer *phase0AObserver, mutationCount, occurrenceCount, totalBytes, occurrenceBytes uint64) error {
	mutationLimit := phase0AObservers.limits.MaxMutations
	if mutationLimit == 0 {
		mutationLimit = math.MaxUint64
	}
	if uint64(len(observer.mutations)) > mutationLimit || mutationCount > mutationLimit-uint64(len(observer.mutations)) {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorMutationCap, Namespace: observer.run})
	}
	occurrenceLimit := phase0AObservers.limits.MaxOccurrences
	if occurrenceLimit == 0 {
		occurrenceLimit = math.MaxUint64
	}
	if observer.occurrenceCount > occurrenceLimit || occurrenceCount > occurrenceLimit-observer.occurrenceCount {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorOccurrenceCap, Namespace: observer.run})
	}
	occurrenceByteLimit := phase0AObservers.limits.MaxOccurrenceBytes
	if occurrenceByteLimit == 0 {
		occurrenceByteLimit = math.MaxUint64
	}
	if observer.occurrenceBytes > occurrenceByteLimit || occurrenceBytes > occurrenceByteLimit-observer.occurrenceBytes {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorOccurrenceByteCap, Namespace: observer.run})
	}
	if err := phase0AChargeManyLocked(mutationCount, totalBytes); err != nil {
		return phase0AStickyLocked(observer, err.(*Phase0AError))
	}
	observer.occurrenceCount += occurrenceCount
	observer.occurrenceBytes += occurrenceBytes
	return nil
}

// Phase0AResolveCommittedConstructionOccurrence returns only a construction
// attempt that survived transaction rollback. It does not prove that condense
// published, retained, or selected the attempt's candidate graph edge.
func Phase0AResolveCommittedConstructionOccurrence(core *Core, namespace CoreRunNamespace, key ConstructionOccurrenceKey) (Phase0AMutationRecord, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForNamespaceLocked(core, namespace)
	if err != nil {
		return Phase0AMutationRecord{}, err
	}
	index, ok := observer.occurrenceIndex[key]
	if !ok || index >= uint64(len(observer.mutations)) {
		return Phase0AMutationRecord{}, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: namespace, Detail: "occurrence is not indexed"}
	}
	record := observer.mutations[index]
	if record.Kind != Phase0AMutationOccurrence || record.Occurrence != key || record.RolledBack {
		return Phase0AMutationRecord{}, &Phase0AError{Kind: Phase0AErrorInvalidOccurrence, Namespace: namespace, Detail: "occurrence did not survive transaction rollback"}
	}
	return record, nil
}

func phase0AReserveMutationLocked(observer *phase0AObserver, bytes uint64) error {
	limit := phase0AObservers.limits.MaxMutations
	if limit == 0 {
		limit = math.MaxUint64
	}
	if uint64(len(observer.mutations)) >= limit {
		return phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorMutationCap, Namespace: observer.run})
	}
	if err := phase0AChargeLocked(bytes); err != nil {
		return phase0AStickyLocked(observer, err.(*Phase0AError))
	}
	return nil
}

func phase0AChargeLocked(bytes uint64) error {
	return phase0AChargeManyLocked(1, bytes)
}

func phase0AChargeManyLocked(records, bytes uint64) error {
	if phase0AObservers.records > phase0AObservers.limits.MaxRecords ||
		records > phase0AObservers.limits.MaxRecords-phase0AObservers.records {
		return &Phase0AError{Kind: Phase0AErrorRecordCap}
	}
	if phase0AObservers.bytes > phase0AObservers.limits.MaxBytes ||
		bytes > phase0AObservers.limits.MaxBytes-phase0AObservers.bytes {
		return &Phase0AError{Kind: Phase0AErrorByteCap}
	}
	phase0AObservers.records += records
	phase0AObservers.bytes += bytes
	return nil
}

func phase0AObserverForRunLocked(core *Core, namespace CoreRunNamespace) (*phase0AObserver, error) {
	if !phase0AObservers.active {
		return nil, &Phase0AError{Kind: Phase0AErrorSessionInactive, Namespace: namespace}
	}
	observer := phase0AObservers.byCore[core]
	if observer == nil || observer.run != namespace {
		return nil, &Phase0AError{Kind: Phase0AErrorStaleNamespace, Namespace: namespace}
	}
	return observer, nil
}

func phase0AObserverForNamespaceLocked(core *Core, namespace CoreRunNamespace) (*phase0AObserver, error) {
	observer, err := phase0AObserverForRunLocked(core, namespace)
	if err != nil {
		return nil, err
	}
	if observer.failure != nil {
		return nil, phase0AExistingFailureLocked(observer)
	}
	if !observer.active {
		return nil, &Phase0AError{Kind: Phase0AErrorStaleNamespace, Namespace: namespace}
	}
	return observer, nil
}

func phase0ACurrentTransaction(observer *phase0AObserver) uint64 {
	if len(observer.frames) == 0 {
		return 0
	}
	return observer.frames[len(observer.frames)-1].TransactionID
}

func phase0AStickyLocked(observer *phase0AObserver, err *Phase0AError) error {
	if observer.failure == nil {
		copy := *err
		observer.failure = &copy
	}
	copy := *observer.failure
	return &copy
}

func phase0AExistingFailureLocked(observer *phase0AObserver) error {
	copy := *observer.failure
	return &copy
}

func Phase0AObserverProof(core *Core, namespace CoreRunNamespace) (Phase0AProofSnapshot, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForRunLocked(core, namespace)
	if err != nil {
		return Phase0AProofSnapshot{}, err
	}
	if !observer.active && observer.failure == nil {
		return Phase0AProofSnapshot{}, &Phase0AError{Kind: Phase0AErrorStaleNamespace, Namespace: namespace}
	}
	snapshot := Phase0AProofSnapshot{
		Namespace:       namespace,
		OccurrenceCount: observer.occurrenceCount,
		OccurrenceBytes: observer.occurrenceBytes,
	}
	snapshot.Mutations = append(snapshot.Mutations, observer.mutations...)
	snapshot.Frames = append(snapshot.Frames, observer.frames...)
	if observer.firstPoison != nil {
		copy := *observer.firstPoison
		snapshot.FirstPoison = &copy
	}
	if observer.failure != nil {
		copy := *observer.failure
		snapshot.Failure = &copy
		return snapshot, snapshot.Failure
	}
	return snapshot, nil
}

func phase0AObserveMark(core *Core, transaction, parent uint64) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.failure != nil {
		return
	}
	if transaction == 0 || (len(observer.frames) == 0 && parent != 0) || (len(observer.frames) != 0 && observer.frames[len(observer.frames)-1].TransactionID != parent) {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "mark parent mismatch"})
		return
	}
	limit := phase0AObservers.limits.MaxFrames
	if limit == 0 {
		limit = math.MaxUint64
	}
	if uint64(len(observer.frames)) >= limit {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorFrameCap, Namespace: observer.run})
		return
	}
	if err := phase0AChargeLocked(phase0AFrameRecordBytes); err != nil {
		phase0AStickyLocked(observer, err.(*Phase0AError))
		return
	}
	observer.frames = append(observer.frames, Phase0ATransactionFrame{TransactionID: transaction, ParentID: parent, MutationStart: uint64(len(observer.mutations))})
}

func phase0AObserveRollback(core *Core, transaction uint64, cause Phase0ARollbackCause) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active {
		return
	}
	if observer.failure != nil {
		if len(observer.frames) != 0 && observer.frames[len(observer.frames)-1].TransactionID == transaction {
			frame := observer.frames[len(observer.frames)-1]
			for index := frame.MutationStart; index < uint64(len(observer.mutations)); index++ {
				observer.mutations[index].RolledBack = true
				observer.mutations[index].RollbackTransaction = transaction
				observer.mutations[index].RollbackCause = cause
			}
			observer.frames = observer.frames[:len(observer.frames)-1]
		}
		return
	}
	if len(observer.frames) == 0 || observer.frames[len(observer.frames)-1].TransactionID != transaction {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "rollback transaction mismatch"})
		return
	}
	frame := observer.frames[len(observer.frames)-1]
	for index := frame.MutationStart; index < uint64(len(observer.mutations)); index++ {
		observer.mutations[index].RolledBack = true
		observer.mutations[index].RollbackTransaction = transaction
		observer.mutations[index].RollbackCause = cause
	}
	observer.frames = observer.frames[:len(observer.frames)-1]
}

func phase0AObserveCommit(core *Core, transaction uint64) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active {
		return
	}
	if observer.failure != nil {
		if len(observer.frames) != 0 && observer.frames[len(observer.frames)-1].TransactionID == transaction {
			observer.frames = observer.frames[:len(observer.frames)-1]
		}
		return
	}
	if len(observer.frames) == 0 || observer.frames[len(observer.frames)-1].TransactionID != transaction {
		phase0AStickyLocked(observer, &Phase0AError{Kind: Phase0AErrorTransactionProof, Namespace: observer.run, Detail: "commit transaction mismatch"})
		return
	}
	observer.frames = observer.frames[:len(observer.frames)-1]
}

func phase0AObserveFirstPoison(core *Core, transaction uint64, kind Phase0APoisonKind) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.firstPoison != nil {
		return
	}
	observer.firstPoison = &Phase0APoisonRecord{TransactionID: transaction, Kind: kind}
}

func phase0ASetRollbackCause(core *Core, cause Phase0ARollbackCause) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if observer := phase0AObservers.byCore[core]; observer != nil && observer.active {
		observer.pendingRollback = cause
	}
}

func phase0ATakeRollbackCause(core *Core) Phase0ARollbackCause {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active {
		return Phase0ARollbackUnknown
	}
	cause := observer.pendingRollback
	observer.pendingRollback = Phase0ARollbackUnknown
	return cause
}

func phase0AObserveSchedulerPoison(core *Core, token SchedulerTransactionToken, kind Phase0APoisonKind) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.firstPoison != nil || !core.schedulerFrame.active || core.schedulerFrame.poisoned == nil {
		return
	}
	if token.owner != core {
		kind = Phase0APoisonWrongCore
	} else if token.epoch == 0 || token.epoch != core.schedulerFrame.epoch || token.transaction != core.schedulerFrame.mark.transaction {
		kind = Phase0APoisonStaleToken
	} else if len(core.transactions) == 0 || core.transactions[len(core.transactions)-1] != token.transaction {
		kind = Phase0APoisonNonTopToken
	}
	observer.firstPoison = &Phase0APoisonRecord{TransactionID: core.schedulerFrame.mark.transaction, Kind: kind}
}
