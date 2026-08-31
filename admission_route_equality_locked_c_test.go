//go:build !gts_no_parsercorephase0

package gotreesitter_test

import (
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

func TestAdmissionRouteEqualityLockedCExceptions(t *testing.T) {
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	for _, witness := range loadRouteEqualityLockedCWitnesses(t) {
		witness := witness
		t.Run(witness.ID, func(t *testing.T) {
			entry := grammars.DetectLanguageByName(witness.Language)
			if entry == nil {
				t.Fatalf("language %q is not registered", witness.Language)
			}
			language := entry.Language()
			source := []byte(witness.SourceUTF8)

			production := gts.NewParser(language)
			production.SetAdmissionCandidateRoute(false)
			productionTree, err := production.Parse(source)
			if err != nil {
				t.Fatalf("production parse: %v", err)
			}
			defer productionTree.Release()

			gts.ResetAdmissionCandidateCountersForTest()
			candidate := gts.NewParser(language)
			candidate.SetAdmissionCandidateRoute(true)
			candidateTree, err := candidate.Parse(source)
			if err != nil {
				t.Fatalf("compact parse: %v", err)
			}
			defer candidateTree.Release()
			if routed, fallback := gts.AdmissionCandidateCounters(); routed != 1 || fallback != 0 {
				t.Fatalf("route counters=%d/%d, want 1/0; reason=%q", routed, fallback, gts.AdmissionCandidateLastFallbackReason())
			}

			compactRoot := candidateTree.RootNode()
			productionRoot := productionTree.RootNode()
			if got, want := compactRoot.HasError(), *witness.Expected.CompactHasError; got != want {
				t.Fatalf("compact root HasError=%t, want %t", got, want)
			}
			if got, want := productionRoot.HasError(), *witness.Expected.ProductionHasError; got != want {
				t.Fatalf("production root HasError=%t, want %t", got, want)
			}
			compactInspection, err := benchfixtures.InspectGoTree(compactRoot, language)
			if err != nil {
				t.Fatalf("inspect compact tree: %v", err)
			}
			if compactInspection.SHA256 != witness.Expected.CompactCDeepSHA256 {
				t.Fatalf("compact digest=%s, want locked C digest %s", compactInspection.SHA256, witness.Expected.CompactCDeepSHA256)
			}
			productionInspection, err := benchfixtures.InspectGoTree(productionRoot, language)
			if err != nil {
				t.Fatalf("inspect production tree: %v", err)
			}
			if productionInspection.SHA256 != witness.Expected.ProductionDeepSHA256 {
				t.Fatalf("production digest=%s, want %s", productionInspection.SHA256, witness.Expected.ProductionDeepSHA256)
			}
			if compactRoot.ChildCount() != witness.Expected.CompactRootChildren || productionRoot.ChildCount() != witness.Expected.ProductionRootChildren {
				t.Fatalf(
					"root child counts compact=%d production=%d, want %d/%d",
					compactRoot.ChildCount(), productionRoot.ChildCount(),
					witness.Expected.CompactRootChildren, witness.Expected.ProductionRootChildren,
				)
			}
			if diff := routeEqualityFirstDivergence(compactRoot, productionRoot, language, "root"); diff == "" {
				t.Fatal("production now matches compact; remove the byte-exact exception")
			}
		})
	}
}
