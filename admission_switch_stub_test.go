//go:build !gts_parsercorephase0

package gotreesitter_test

import (
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
)

// TestAdmissionSwitchDefaultBuildFallsBackLoudly proves the fail-closed
// contract in the shipped default build: with the switch on, an eligible fresh
// full parse attempts the candidate route, is declined (the compact engine is
// not compiled in), bumps the fallback counter, records a loud reason, and
// returns the correct production tree.
func TestAdmissionSwitchDefaultBuildFallsBackLoudly(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(false)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	parser.SetAdmissionCandidateRoute(true)
	routedBefore, fallbackBefore := gts.AdmissionCandidateCounters()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	requireCleanFullTree(t, tree, source, "default-build-fallback")

	routedAfter, fallbackAfter := gts.AdmissionCandidateCounters()
	if routedAfter != routedBefore {
		t.Fatalf("default build must not route to the candidate: routed %d -> %d", routedBefore, routedAfter)
	}
	if fallbackAfter != fallbackBefore+1 {
		t.Fatalf("default build must fall back loudly: fallback %d -> %d", fallbackBefore, fallbackAfter)
	}
	if reason := gts.AdmissionCandidateLastFallbackReason(); !strings.Contains(reason, "not compiled in") {
		t.Fatalf("fallback reason must name the missing engine, got %q", reason)
	}
}
