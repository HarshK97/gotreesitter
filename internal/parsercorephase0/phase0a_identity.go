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

// RawOccurrenceKey identifies a selected raw occurrence within an event.
type RawOccurrenceKey struct {
	Event ConstructionEventKey
	Slot  uint32
}

type Phase0AErrorKind string

const (
	Phase0AErrorCounterOverflow  Phase0AErrorKind = "counter_overflow"
	Phase0AErrorUnregisteredCore Phase0AErrorKind = "unregistered_core"
	Phase0AErrorRunActive        Phase0AErrorKind = "run_active"
	Phase0AErrorStaleNamespace   Phase0AErrorKind = "stale_namespace"
	Phase0AErrorInvalidEvent     Phase0AErrorKind = "invalid_event"
	Phase0AErrorSessionInactive  Phase0AErrorKind = "session_inactive"
	Phase0AErrorSessionActive    Phase0AErrorKind = "session_active"
	Phase0AErrorSessionHasRuns   Phase0AErrorKind = "session_has_active_runs"
	Phase0AErrorCoreCap          Phase0AErrorKind = "core_cap"
	Phase0AErrorRecordCap        Phase0AErrorKind = "record_cap"
	Phase0AErrorByteCap          Phase0AErrorKind = "byte_cap"
	Phase0AErrorAttemptUnproven  Phase0AErrorKind = "attempt_unproven"
)

type Phase0ACounter string

const (
	Phase0ACounterCoreInstance  Phase0ACounter = "core_instance"
	Phase0ACounterRunGeneration Phase0ACounter = "run_generation"
	Phase0ACounterEventSerial   Phase0ACounter = "event_serial"
	Phase0ACounterEdgeSerial    Phase0ACounter = "edge_serial"
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
	MaxCores   uint64
	MaxRecords uint64
	MaxBytes   uint64
}

const (
	phase0AEventRecordBytes uint64 = 40
	phase0AEdgeRecordBytes  uint64 = 48
)

type phase0AObserver struct {
	coreInstance  uint64
	runGeneration uint64
	run           CoreRunNamespace
	nextEvent     uint64
	nextEdge      uint64
	attempt       uint32
	active        bool
}

var phase0AObservers = struct {
	sync.Mutex
	nextCoreInstance uint64
	active           bool
	limits           Phase0AObserverLimits
	records          uint64
	bytes            uint64
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
	phase0AObservers.byCore = nil
	return nil
}

func phase0AInvalidateCore(core *Core) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if observer := phase0AObservers.byCore[core]; observer != nil {
		observer.active = false
	}
}

func (c *Core) BeginRun() (CoreRunNamespace, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	if !phase0AObservers.active {
		return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorSessionInactive}
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
			return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterCoreInstance}
		}
		phase0AObservers.nextCoreInstance++
		observer = &phase0AObserver{coreInstance: phase0AObservers.nextCoreInstance}
		phase0AObservers.byCore[c] = observer
	}
	if observer.active {
		return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorRunActive, Namespace: observer.run}
	}
	if observer.runGeneration == math.MaxUint64 {
		return CoreRunNamespace{}, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterRunGeneration, Namespace: observer.run}
	}
	observer.runGeneration++
	observer.run = CoreRunNamespace{CoreInstance: observer.coreInstance, RunGeneration: observer.runGeneration}
	observer.nextEvent, observer.nextEdge, observer.attempt, observer.active = 0, 0, 1, true
	return observer.run, nil
}

func (c *Core) EndRun(namespace CoreRunNamespace) error {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForNamespaceLocked(c, namespace)
	if err != nil {
		return err
	}
	observer.active = false
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
		return AttemptKey{}, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: namespace}
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
		return ConstructionEventKey{}, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: namespace}
	}
	if observer.nextEvent == math.MaxUint64 {
		return ConstructionEventKey{}, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterEventSerial, Namespace: namespace}
	}
	if err := phase0AChargeLocked(phase0AEventRecordBytes); err != nil {
		return ConstructionEventKey{}, err
	}
	observer.nextEvent++
	return ConstructionEventKey{Attempt: AttemptKey{Run: namespace, AttemptEpoch: 1}, Serial: observer.nextEvent}, nil
}

func phase0ANextIncomingEdge(c *Core, event ConstructionEventKey) (IncomingEdgeKey, error) {
	phase0AObservers.Lock()
	defer phase0AObservers.Unlock()
	observer, err := phase0AObserverForNamespaceLocked(c, event.Attempt.Run)
	if err != nil {
		return IncomingEdgeKey{}, err
	}
	if event.Attempt.AttemptEpoch != 1 {
		return IncomingEdgeKey{}, &Phase0AError{Kind: Phase0AErrorAttemptUnproven, Namespace: observer.run}
	}
	if event.Serial == 0 || event.Serial > observer.nextEvent {
		return IncomingEdgeKey{}, &Phase0AError{Kind: Phase0AErrorInvalidEvent, Namespace: observer.run}
	}
	if observer.nextEdge == math.MaxUint64 {
		return IncomingEdgeKey{}, &Phase0AError{Kind: Phase0AErrorCounterOverflow, Counter: Phase0ACounterEdgeSerial, Namespace: observer.run}
	}
	if err := phase0AChargeLocked(phase0AEdgeRecordBytes); err != nil {
		return IncomingEdgeKey{}, err
	}
	observer.nextEdge++
	return IncomingEdgeKey{Event: event, Serial: observer.nextEdge}, nil
}

func phase0AChargeLocked(bytes uint64) error {
	if phase0AObservers.records >= phase0AObservers.limits.MaxRecords {
		return &Phase0AError{Kind: Phase0AErrorRecordCap}
	}
	if phase0AObservers.bytes > phase0AObservers.limits.MaxBytes ||
		bytes > phase0AObservers.limits.MaxBytes-phase0AObservers.bytes {
		return &Phase0AError{Kind: Phase0AErrorByteCap}
	}
	phase0AObservers.records++
	phase0AObservers.bytes += bytes
	return nil
}

func phase0AObserverForNamespaceLocked(core *Core, namespace CoreRunNamespace) (*phase0AObserver, error) {
	if !phase0AObservers.active {
		return nil, &Phase0AError{Kind: Phase0AErrorSessionInactive, Namespace: namespace}
	}
	observer := phase0AObservers.byCore[core]
	if observer == nil || !observer.active || observer.run != namespace {
		return nil, &Phase0AError{Kind: Phase0AErrorStaleNamespace, Namespace: namespace}
	}
	return observer, nil
}
