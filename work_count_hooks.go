//go:build !gts_workcount

package gotreesitter

type workCountAttemptToken uint32

const workCountInstrumentationEnabled = false

// These hooks are compile-time instrumentation seams. The production build
// supplies empty, inlineable bodies; the gts_workcount build replaces them
// with parse-local saturating counters.
func workCountSetNextParseAttempt(string, string)                                       {}
func workCountBeginParseAttempt(int, int, int) workCountAttemptToken                    { return 0 }
func workCountResolveParseAttempt(workCountAttemptToken, int, bool, int, int, int, int) {}
func workCountBeginFinalizeParseAttempt(workCountAttemptToken)                          {}
func workCountEndFinalizeParseAttempt(workCountAttemptToken, ParseStopReason, *Tree)    {}
func workCountRecordLexerFrontDoor()                                                    {}
func workCountRecordTableLookup()                                                       {}
func workCountAddActionEntries(int)                                                     {}
func workCountRecordShift()                                                             {}
func workCountRecordReduce()                                                            {}
func workCountRecordAccept()                                                            {}
func workCountRecordExplicitRecover()                                                   {}
func workCountObserveReductionPop(*glrStack, int)                                       {}
func workCountAddPopPaths(uint64, uint64)                                               {}
func workCountObservePopWindow([]stackEntry)                                            {}
func workCountRecordVersionCreation()                                                   {}
func workCountRecordMergeAttempt()                                                      {}
func workCountRecordMergeSuccess()                                                      {}
func workCountRecordGraphLinkAddition()                                                 {}
func workCountRecordLeafConstruction()                                                  {}
func workCountRecordParentConstruction()                                                {}
func workCountRecordPendingParentConstruction()                                         {}
func workCountRecordNoTreeParentConstruction()                                          {}
