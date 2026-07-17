//go:build !gts_parsercorephase0 || !gts_parsercorephase0a

package parsercorephase0

const phase0AEnabled = false

type Phase0ARollbackCause uint8

const (
	Phase0ARollbackUnknown Phase0ARollbackCause = iota
	Phase0ARollbackReturnedError
	Phase0ARollbackPanic
	Phase0ARollbackSchedulerPoison
)

type Phase0APoisonKind uint8

type phase0ATransitionKind uint8

const (
	phase0ATransitionDuplicateDrop phase0ATransitionKind = iota + 1
	phase0ATransitionPrecedenceDrop
	phase0ATransitionPrecedenceReplacement
	phase0ATransitionAlternateAppend
)

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
func phase0AObserveTerminalShift(*Core, SubtreeID, NodeID, StateID, uint32, bool, bool) {}
func phase0AObserveTerminalCohortShift(*Core, SubtreeID, []ClassifiedBoundary, []StateID, uint32, bool) {
}
func phase0ABeginReductionConstruction(*Core, uint64)                                               {}
func phase0AObserveReductionOccurrence(*Core, SubtreeID, NodeID, boundaryKey, bool)                 {}
func phase0AFinishReductionConstruction(*Core)                                                      {}
func phase0AObserveCandidateDrop(*Core, boundaryKey, linkInput, NodeID, int, phase0ATransitionKind) {}
func phase0AObserveDirectPublication(*Core, boundaryKey, linkInput, LinkID, NodeID, NodeID)         {}
func phase0AObservePrivatePublication(*Core, StateID, uint32, linkInput, LinkID, NodeID)            {}
func phase0ABeginReplacement(*Core, boundaryKey, linkInput, NodeID, int)                            {}
func phase0AObserveReplacementPublished(*Core, boundaryKey, linkInput, NodeID, LinkID, int, int)    {}
func phase0ABeginPredecessorMerge(*Core, NodeID, NodeID)                                            {}
func phase0AMergeDecision(*Core, int, phase0ATransitionKind)                                        {}
func phase0AAbortPredecessorMerge(*Core)                                                            {}
func phase0AObserveAdjacencyPublished(*Core, NodeID)                                                {}
func phase0AObserveFactorNoChange(*Core, boundaryKey, linkInput, NodeID, int)                       {}
func phase0APrepareFactorOuter(*Core, boundaryKey, linkInput, NodeID, int, NodeID)                  {}
func phase0AObserveFactorPublished(*Core, NodeID, NodeID)                                           {}
