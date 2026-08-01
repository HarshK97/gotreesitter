//go:build !gts_no_parsercorephase0

package gotreesitter

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

// The converged-alternative-set shadow proof and its census.
//
// A converged-split no-action drop is currently proved admissible by a
// scalar (rank, lineage) pair recorded on each header
// (diagnosticParserCoreSelectedLineageDrops). This file adds an alternative
// proof: a drop is admissible when a surviving header's recorded
// alternative-membership set contains every member of the dropped header's
// set (diagnosticParserCoreConvergedCoverageDrops). The alternative-set
// record (diagnosticParserCoreHeader.altSet and core.AlternativeSet) is
// established and propagated everywhere the scalar pair already is, but the
// new predicate runs only in shadow, next to the deciding scalar proof: it
// never changes which drops dropGenericNoActionHeads allows. This file adds
// that shadow predicate and an opt-in census that tallies how often the two
// proofs agree.
//
// Mirrors admission_census.go's pattern: gated behind an env var, cached via
// sync.Once, zero cost when off.

// diagnosticParserCoreConvergedCoverageDrops shadow-proves converged-split
// no-action drops by alternative-set containment
// (spec.b4b-alternative-set.v1 section 5): a drop is admissible when one
// surviving header's recorded set contains every recorded member of the
// dropped header's set, and the dropped set is complete (non-empty, not
// overflowed, with a resolvable spill reference). It never walks the
// derivation DAG and never allocates once the shared spill arena has warmed.
func (s *diagnosticParserCoreGenericScheduler) diagnosticParserCoreConvergedCoverageDrops(
	indices []int,
) (uint64, bool) {
	var proved uint64
	for _, index := range indices {
		if index < 0 || index >= len(s.headers) {
			return 0, false
		}
		dropped := s.headers[index]
		if !dropped.convergedReductionSplit {
			continue
		}
		droppedMembers, ok := s.compact.AlternativeSetMembers(dropped.altSet)
		if !ok || len(droppedMembers) == 0 || dropped.altSet.Overflowed() {
			return 0, false
		}
		matched := false
		for survivorIndex, survivor := range s.headers {
			dropIndex := sort.SearchInts(indices, survivorIndex)
			if dropIndex < len(indices) && indices[dropIndex] == survivorIndex {
				continue
			}
			survivorMembers, ok := s.compact.AlternativeSetMembers(survivor.altSet)
			if !ok {
				continue
			}
			if alternativeSetContainsAll(droppedMembers, survivorMembers) {
				matched = true
				break
			}
		}
		if !matched {
			return 0, false
		}
		proved++
	}
	return proved, true
}

// alternativeSetContainsAll reports whether every member of dropped is
// present in survivor. Both slices are sorted ascending (core.AlternativeSet
// section 3.2's invariant), so this is a single merge-scan, never
// allocating: O(len(dropped)+len(survivor)), each side bounded by
// alternativeSetHardCap.
func alternativeSetContainsAll(dropped, survivor []uint16) bool {
	if len(dropped) > len(survivor) {
		return false
	}
	survivorIndex := 0
	for _, member := range dropped {
		for survivorIndex < len(survivor) && survivor[survivorIndex] < member {
			survivorIndex++
		}
		if survivorIndex >= len(survivor) || survivor[survivorIndex] != member {
			return false
		}
		survivorIndex++
	}
	return true
}

// DiagnosticParserCoreShadowCensusTotals tallies the comparison between the
// scalar rank/lineage proof (diagnosticParserCoreSelectedLineageDrops) and
// the alternative-set containment proof
// (diagnosticParserCoreConvergedCoverageDrops) across every converged-split
// no-action drop attempt observed while the census is enabled.
type DiagnosticParserCoreShadowCensusTotals struct {
	// Agree: both proofs reached the same verdict (both proved or both
	// declined).
	Agree uint64
	// OldProvedNewUnproved is the design's stop rule
	// (spec.b4b-alternative-set.v1 section 7): the scalar proof proved a drop
	// the set proof could not. A non-zero count is a design falsifier, not
	// an implementation bug to paper over.
	OldProvedNewUnproved uint64
	// NewProvedOldUnproved is the expected, desired direction: the set proof
	// is strictly more capable (spec section 2, families F1-F3).
	NewProvedOldUnproved uint64
	// NeitherProved: both proofs declined (for example an F4 resurrection,
	// out of B4b stage 1 scope).
	NeitherProved uint64
}

// DiagnosticParserCoreShadowCensusFalsifier captures one old-proved/new-
// unproved drop context for root-causing a stop-rule hit.
type DiagnosticParserCoreShadowCensusFalsifier struct {
	Detail string
}

var (
	diagnosticParserCoreShadowCensusEnabledOnce sync.Once
	diagnosticParserCoreShadowCensusEnabledVal  bool

	diagnosticParserCoreShadowCensusMu    sync.Mutex
	diagnosticParserCoreShadowCensusTotal DiagnosticParserCoreShadowCensusTotals
	diagnosticParserCoreShadowFalsifiers  []DiagnosticParserCoreShadowCensusFalsifier
)

// diagnosticParserCoreShadowCensusEnabled reports whether
// GTS_B4B_SHADOW_CENSUS requests the alternative-set shadow proof
// comparison. The result is cached after the first read (sync.Once), so
// every later drop attempt pays only one cached boolean read when the
// census is off -- the same zero-cost-when-off shape as
// admissionCensusEnabled (admission_census.go).
func diagnosticParserCoreShadowCensusEnabled() bool {
	diagnosticParserCoreShadowCensusEnabledOnce.Do(func() {
		switch os.Getenv("GTS_B4B_SHADOW_CENSUS") {
		case "1", "true", "TRUE", "True", "on", "ON", "yes", "YES":
			diagnosticParserCoreShadowCensusEnabledVal = true
		}
	})
	return diagnosticParserCoreShadowCensusEnabledVal
}

// diagnosticParserCoreRunShadowCensus evaluates the alternative-set
// containment proof next to the scalar decision dropGenericNoActionHeads
// already computed, and tallies the comparison. Diagnostic-only: it never
// influences dropGenericNoActionHeads' actual decision. Called only when
// diagnosticParserCoreShadowCensusEnabled is true.
func (s *diagnosticParserCoreGenericScheduler) diagnosticParserCoreRunShadowCensus(indices []int, oldProved bool) {
	_, newProved := s.diagnosticParserCoreConvergedCoverageDrops(indices)
	diagnosticParserCoreShadowCensusMu.Lock()
	defer diagnosticParserCoreShadowCensusMu.Unlock()
	switch {
	case oldProved && newProved:
		diagnosticParserCoreShadowCensusTotal.Agree++
	case oldProved && !newProved:
		diagnosticParserCoreShadowCensusTotal.OldProvedNewUnproved++
		diagnosticParserCoreShadowFalsifiers = append(
			diagnosticParserCoreShadowFalsifiers,
			s.diagnosticParserCoreShadowCensusFalsifierDetail(indices),
		)
	case !oldProved && newProved:
		diagnosticParserCoreShadowCensusTotal.NewProvedOldUnproved++
	default:
		diagnosticParserCoreShadowCensusTotal.NeitherProved++
	}
}

// diagnosticParserCoreShadowCensusFalsifierDetail formats the exact dropped-
// header and best-candidate-survivor context for a captured falsifier, so a
// stop-rule hit can be root-caused without rerunning under a debugger.
func (s *diagnosticParserCoreGenericScheduler) diagnosticParserCoreShadowCensusFalsifierDetail(indices []int) DiagnosticParserCoreShadowCensusFalsifier {
	var b []byte
	for _, index := range indices {
		if index < 0 || index >= len(s.headers) {
			continue
		}
		header := s.headers[index]
		members, _ := s.compact.AlternativeSetMembers(header.altSet)
		state, byteOffset, boundaryErr := s.compact.Boundary(header.head)
		b = fmt.Appendf(b, "[drop idx=%d state=%d byte=%d boundaryErr=%v rank=%v lineage=%d converged=%t altSet=%v overflowed=%t] ",
			index, state, byteOffset, boundaryErr, header.cleanPathRank, header.cleanPathLineage,
			header.convergedReductionSplit, members, header.altSet.Overflowed())
	}
	for survivorIndex, survivor := range s.headers {
		dropIndex := sort.SearchInts(indices, survivorIndex)
		if dropIndex < len(indices) && indices[dropIndex] == survivorIndex {
			continue
		}
		members, _ := s.compact.AlternativeSetMembers(survivor.altSet)
		b = fmt.Appendf(b, "[survivor idx=%d rank=%v lineage=%d altSet=%v] ",
			survivorIndex, survivor.cleanPathRank, survivor.cleanPathLineage, members)
	}
	return DiagnosticParserCoreShadowCensusFalsifier{Detail: string(b)}
}

// DiagnosticParserCoreShadowCensusSnapshotForTest returns the accumulated
// shadow-census totals since the last reset. A harness resets around one
// language's corpus run to attribute totals to that language.
func DiagnosticParserCoreShadowCensusSnapshotForTest() DiagnosticParserCoreShadowCensusTotals {
	diagnosticParserCoreShadowCensusMu.Lock()
	defer diagnosticParserCoreShadowCensusMu.Unlock()
	return diagnosticParserCoreShadowCensusTotal
}

// DiagnosticParserCoreShadowCensusResetForTest clears the accumulated
// shadow-census totals and captured falsifiers.
func DiagnosticParserCoreShadowCensusResetForTest() {
	diagnosticParserCoreShadowCensusMu.Lock()
	defer diagnosticParserCoreShadowCensusMu.Unlock()
	diagnosticParserCoreShadowCensusTotal = DiagnosticParserCoreShadowCensusTotals{}
	diagnosticParserCoreShadowFalsifiers = nil
}

// DiagnosticParserCoreShadowCensusFalsifiersForTest returns every captured
// old-proved/new-unproved drop context observed since the last reset: the
// design's stop rule (spec.b4b-alternative-set.v1 section 7). A non-empty
// result means stop, root-cause, and re-spec -- not silently proceed.
func DiagnosticParserCoreShadowCensusFalsifiersForTest() []DiagnosticParserCoreShadowCensusFalsifier {
	diagnosticParserCoreShadowCensusMu.Lock()
	defer diagnosticParserCoreShadowCensusMu.Unlock()
	out := make([]DiagnosticParserCoreShadowCensusFalsifier, len(diagnosticParserCoreShadowFalsifiers))
	copy(out, diagnosticParserCoreShadowFalsifiers)
	return out
}

// SetDiagnosticParserCoreShadowCensusEnabledForTest overrides the
// GTS_B4B_SHADOW_CENSUS gate for one test process, mirroring
// setParserCoreReplayParseStatesForTest's override pattern
// (parsestate_replay_compact.go). Restore the previous value (the returned
// func) when done.
func SetDiagnosticParserCoreShadowCensusEnabledForTest(on bool) func() {
	diagnosticParserCoreShadowCensusEnabledOnce.Do(func() {})
	previous := diagnosticParserCoreShadowCensusEnabledVal
	diagnosticParserCoreShadowCensusEnabledVal = on
	return func() { diagnosticParserCoreShadowCensusEnabledVal = previous }
}
