//go:build !gts_no_parsercorephase0

package gotreesitter_test

import (
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestAdmissionCandidateConvergedPathSplitFailsClosed covers a compact-route
// divergence found by the refreshed Kotlin corpus. The visibility-modifier and
// identifier conflict paths merge, then split during a later reduction.
// Production and tree-sitter C recover the identifier path. The compact route
// otherwise drops that path and returns a different clean tree.
func TestAdmissionCandidateConvergedPathSplitFailsClosed(t *testing.T) {
	source := []byte("internal actual fun f(): String = \"x\"\n")
	lang := grammars.KotlinLanguage()

	production := gts.NewParser(lang)
	production.SetAdmissionCandidateRoute(false)
	productionTree, err := production.Parse(source)
	if err != nil {
		t.Fatalf("production parse: %v", err)
	}
	defer productionTree.Release()
	productionSExpr := productionTree.RootNode().SExpr(lang)
	if !productionTree.RootNode().HasError() || !strings.Contains(productionSExpr, "infix_expression") {
		t.Fatalf("production/C witness changed: %s", productionSExpr)
	}

	gts.ResetAdmissionCandidateCountersForTest()
	candidate := gts.NewParser(lang)
	candidate.SetAdmissionCandidateRoute(true)
	candidateTree, err := candidate.Parse(source)
	if err != nil {
		t.Fatalf("candidate parse: %v", err)
	}
	defer candidateTree.Release()

	routed, fallback := gts.AdmissionCandidateCounters()
	if routed != 0 || fallback != 1 {
		t.Fatalf("converged-path split did not fail closed: routed=%d fallback=%d", routed, fallback)
	}
	if reason := gts.AdmissionCandidateLastFallbackReason(); !strings.Contains(reason, "converged-path reduction split") {
		t.Fatalf("fallback reason=%q", reason)
	}
	if candidateSExpr := candidateTree.RootNode().SExpr(lang); candidateSExpr != productionSExpr {
		t.Fatalf("fallback tree diverged:\nproduction=%s\ncandidate=%s", productionSExpr, candidateSExpr)
	}
}
