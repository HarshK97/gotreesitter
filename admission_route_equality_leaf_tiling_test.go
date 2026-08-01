//go:build !gts_no_parsercorephase0

package gotreesitter_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// compactT3RouteEqualityWitnessManifestPath is the manifest committed by PR
// #573 and read directly by cgo_harness/compact_t3_oracle_adjudication_test.go
// (cgo- and Docker-gated, so not runnable here). cgo_harness is a separate Go
// module (its own go.mod), so this package cannot import its types; it
// decodes the same committed JSON with a local, minimal struct instead.
const compactT3RouteEqualityWitnessManifestPath = "cgo_harness/testdata/compact_t3_oracle_witnesses_v1.json"

type routeEqualityWitnessManifest struct {
	Witnesses []routeEqualityWitness `json:"witnesses"`
}

type routeEqualityWitness struct {
	ID         string `json:"id"`
	Language   string `json:"language"`
	SourceUTF8 string `json:"source_utf8"`
	Expected   struct {
		CHasError          *bool `json:"c_has_error"`
		ProductionHasError *bool `json:"production_has_error"`
		CompactHasError    *bool `json:"compact_has_error"`
	} `json:"expected"`
}

func loadRouteEqualityWitnesses(t *testing.T) map[string]routeEqualityWitness {
	t.Helper()
	raw, err := os.ReadFile(compactT3RouteEqualityWitnessManifestPath)
	if err != nil {
		t.Fatalf("read witness manifest: %v", err)
	}
	var manifest routeEqualityWitnessManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode witness manifest: %v", err)
	}
	byID := make(map[string]routeEqualityWitness, len(manifest.Witnesses))
	for _, w := range manifest.Witnesses {
		byID[w.ID] = w
	}
	return byID
}

// TestCompactRouteHTMLErroneousEndTagByteGapDeclines pins the B1 reference
// witness by its literal bytes, independent of the committed manifest file:
// html "<a></a^>" must decline the compact route because no leaf covers byte
// 6 (the stray '^' inside the malformed close tag), and production must
// report HasError()==true for the same bytes. Before the tiling gate, the
// compact route accepted this input and published document[0:8] clean.
func TestCompactRouteHTMLErroneousEndTagByteGapDeclines(t *testing.T) {
	// This loads the html grammar into the process-wide embedded cache; purge
	// afterward so it does not inflate heap for later whole-process tests
	// (matches admission_scorecard_test.go's convention).
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entry := grammars.DetectLanguageByName("html")
	if entry == nil {
		t.Fatal("html is not registered")
	}
	lang := entry.Language()
	source := []byte("<a></a^>")

	production := gts.NewParser(lang)
	production.SetAdmissionCandidateRoute(false)
	productionTree, err := production.Parse(source)
	if err != nil {
		t.Fatalf("production parse: %v", err)
	}
	defer productionTree.Release()
	if !productionTree.RootNode().HasError() {
		t.Fatal("production HasError=false, want true for <a></a^>")
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
		t.Fatalf("route counters routed=%d fallback=%d, want routed=0 fallback=1 (compact must decline)", routed, fallback)
	}
	reason := gts.AdmissionCandidateLastFallbackReason()
	if !strings.Contains(reason, "accepted-leaf-tiling-gap") {
		t.Fatalf("fallback reason=%q, want it to cite accepted-leaf-tiling-gap", reason)
	}
	if !candidateTree.RootNode().HasError() {
		t.Fatal("production-served (post-fallback) tree HasError=false, want true")
	}
}

// TestCompactRouteDeclinesAdjudicatedFalseCleanWitnesses pulls a focused
// subset (3 html, 3 javascript) from the 20-witness committed manifest (10
// html, 8 javascript, 2 swift). Each entry was, before the B1 tiling gate,
// accepted by the compact route with HasError()==false while production and
// the locked C oracle both report an error
// (compact_t3_oracle_witnesses_v1.json: c_has_error=production_has_error=
// true, compact_has_error=false for all 20). This asserts the fixed
// contract: compact declines specifically at the new tiling gate, the
// caller falls back to production within the same Parse call, and the
// production-served tree's HasError matches the manifest's adjudicated
// verdict.
//
// Scope note: the manifest's 2 swift entries (swift_log_1, swift_log_2) are
// NOT in this list. Root-cause (verified by direct tree inspection):
// swift's witnesses are ambiguous `>>` tokenization (ship-vs-comparison,
// `x >> y>>`) where the compact generic scheduler's accepted derivation
// fully tiles the source -- every byte is covered by a leaf, contiguously,
// with no trivia-excused or non-trivia gap anywhere. Production instead
// treats the dangling trailing `>` `>` as unparseable and wraps it in an
// ERROR node. Both derivations are complete and gapless; they simply
// disagree on which reduction is grammatically valid for the trailing
// operator. That is a shift/reduce conflict-resolution divergence between
// the compact scheduler and production's LR tables, a different mechanism
// from the false-clean coverage gap this tranche closes, so the B1 leaf-
// tiling invariant correctly does not (and must not be made to) reject it.
// See TestCompactRouteSwiftShiftComparisonGapIsNotATilingDefect
// (admission_route_equality_swift_residual_test.go, gts_parsercorephase0-
// tagged -- loading the swift grammar alone retains roughly 20 MB past a
// full drain+purge+GC cycle, a pre-existing characteristic reproduced
// identically on origin/main with none of this tranche's changes present,
// unrelated to either route; keeping it out of the always-on suite avoids
// tripping TestArenaGCRetentionAfterRelease's whole-process heap budget),
// which documents the current, still-open state directly. Filed as an
// out-of-scope finding for a follow-up tranche rather than folded into this
// fix (campaign v7 delegation protocol: scope changes route back through
// the specification, not through an ad hoc widening of one tranche's check).
func TestCompactRouteDeclinesAdjudicatedFalseCleanWitnesses(t *testing.T) {
	// html and javascript are not otherwise loaded by the always-on (untagged)
	// suite; purge the process-wide embedded cache afterward so this test does
	// not inflate heap for later whole-process tests (matches
	// admission_scorecard_test.go's convention).
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	witnesses := loadRouteEqualityWitnesses(t)
	ids := []string{
		"html_min_a", "html_min_html", "html_log_1",
		"js_log_1", "js_log_2", "js_log_3",
	}
	for _, id := range ids {
		id := id
		t.Run(id, func(t *testing.T) {
			witness, ok := witnesses[id]
			if !ok {
				t.Fatalf("witness %q is missing from %s", id, compactT3RouteEqualityWitnessManifestPath)
			}
			if witness.Expected.CHasError == nil || witness.Expected.ProductionHasError == nil || witness.Expected.CompactHasError == nil {
				t.Fatalf("witness %q has an incomplete expected outcome", id)
			}
			if *witness.Expected.CompactHasError {
				t.Fatalf("witness %q manifest compact_has_error=true, want the documented pre-fix false-clean baseline (false)", id)
			}
			if *witness.Expected.CHasError != *witness.Expected.ProductionHasError {
				t.Fatalf("witness %q manifest c_has_error=%t disagrees with production_has_error=%t; adjudication requires the C oracle to side with production",
					id, *witness.Expected.CHasError, *witness.Expected.ProductionHasError)
			}

			entry := grammars.DetectLanguageByName(witness.Language)
			if entry == nil {
				t.Fatalf("witness %q: language %q is not registered", id, witness.Language)
			}
			lang := entry.Language()
			source := []byte(witness.SourceUTF8)

			gts.ResetAdmissionCandidateCountersForTest()
			parser := gts.NewParser(lang)
			parser.SetAdmissionCandidateRoute(true)
			tree, err := parser.Parse(source)
			if err != nil {
				t.Fatalf("witness %q: parse: %v", id, err)
			}
			defer tree.Release()

			routed, fallback := gts.AdmissionCandidateCounters()
			if routed != 0 || fallback != 1 {
				t.Fatalf("witness %q: route counters routed=%d fallback=%d, want routed=0 fallback=1 (compact must decline)", id, routed, fallback)
			}
			reason := gts.AdmissionCandidateLastFallbackReason()
			if !strings.Contains(reason, "accepted-leaf-tiling-gap") {
				t.Fatalf("witness %q: fallback reason=%q, want it to cite accepted-leaf-tiling-gap", id, reason)
			}

			if got := tree.RootNode().HasError(); got != *witness.Expected.ProductionHasError {
				t.Fatalf("witness %q: production-served HasError=%t, want %t (manifest production_has_error, C-adjudicated)",
					id, got, *witness.Expected.ProductionHasError)
			}
		})
	}
}
