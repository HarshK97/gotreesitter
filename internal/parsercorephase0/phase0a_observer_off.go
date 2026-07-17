//go:build !gts_parsercorephase0 || !gts_parsercorephase0a

package parsercorephase0

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

func phase0AInvalidateCore(*Core) {}

func phase0AObserveMark(*Core, uint64, uint64)                                          {}
func phase0AObserveRollback(*Core, uint64, Phase0ARollbackCause)                        {}
func phase0AObserveCommit(*Core, uint64)                                                {}
func phase0AObserveFirstPoison(*Core, uint64, Phase0APoisonKind)                        {}
func phase0ASetRollbackCause(*Core, Phase0ARollbackCause)                               {}
func phase0ATakeRollbackCause(*Core) Phase0ARollbackCause                               { return Phase0ARollbackUnknown }
func phase0AObserveSchedulerPoison(*Core, SchedulerTransactionToken, Phase0APoisonKind) {}
func phase0AObserveTerminalShift(*Core, SubtreeID, NodeID, bool)                        {}
func phase0AObserveTerminalCohortShift(*Core, SubtreeID, []ClassifiedBoundary, bool)    {}
