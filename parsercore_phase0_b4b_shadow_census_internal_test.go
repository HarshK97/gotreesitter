//go:build gts_parsercorephase0

package gotreesitter

// B4b stage 1 shadow census runner, canonical-fixtures half
// (spec.b4b-alternative-set.v1 section 7, "Agreement census on ... the six
// certified languages' real corpora"; the four canonical Go fixtures are the
// available real-corpus-scale sources in this tree). Opt-in and diagnostic-
// only, reusing the same canonical fixture loader and admission path as
// TestDiagnosticParserCoreCanonicalAdmissions. The smoke-corpus half lives in
// parsercore_phase0_b4b_shadow_census_test.go (package gotreesitter_test).
//
// Run with:
//
//	GTS_B4B_SHADOW_CENSUS_REPORT=1 go test -tags gts_parsercorephase0 \
//	  -run TestDiagnosticParserCoreB4bShadowCensusCanonicalReport -v .

import (
	"os"
	"testing"

	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

func TestDiagnosticParserCoreB4bShadowCensusCanonicalReport(t *testing.T) {
	if os.Getenv("GTS_B4B_SHADOW_CENSUS_REPORT") != "1" {
		t.Skip("set GTS_B4B_SHADOW_CENSUS_REPORT=1 to run the B4b stage 1 shadow census over the canonical fixtures")
	}
	// The census comparison switch and the recording switch are two
	// independent cached reads of the same GTS_B4B_SHADOW_CENSUS
	// environment variable (recording lives in parsercorephase0, which
	// cannot depend back on this package's copy of the flag); a real
	// process setting the env var enables both together, so the ForTest
	// override here must enable both explicitly.
	restore := SetDiagnosticParserCoreShadowCensusEnabledForTest(true)
	defer restore()
	restoreRecording := core.SetAlternativeSetRecordingEnabledForTest(true)
	defer restoreRecording()

	var grandTotal DiagnosticParserCoreShadowCensusTotals
	var allFalsifiers []DiagnosticParserCoreShadowCensusFalsifier
	for _, row := range diagnosticParserCoreCanonicalAdmissions {
		row := row
		DiagnosticParserCoreShadowCensusResetForTest()
		fixture := loadDiagnosticParserCoreCanonicalFixture(t, row.id)
		requireDiagnosticParserCoreCanonicalFixtureIdentity(t, fixture, row)
		_, routeErr := DiagnosticParseParserCorePrefix(
			parserCoreWarmGoScanner, fixture.Source,
			DiagnosticParserCorePrefixOptions{
				ReceiptMode: DiagnosticParserCoreReceiptSummary,
				MaxTokens:   300000, MaxDispatches: 600000,
				Limits: diagnosticParserCoreCanonicalLimits(),
			},
		)
		if routeErr != nil {
			t.Fatalf("canonical fixture %s: compact admission declined: %v", row.id, routeErr)
		}
		total := DiagnosticParserCoreShadowCensusSnapshotForTest()
		grandTotal.Agree += total.Agree
		grandTotal.OldProvedNewUnproved += total.OldProvedNewUnproved
		grandTotal.NewProvedOldUnproved += total.NewProvedOldUnproved
		grandTotal.NeitherProved += total.NeitherProved
		allFalsifiers = append(allFalsifiers, DiagnosticParserCoreShadowCensusFalsifiersForTest()...)
		t.Logf("%-16s agree=%-6d old-proved/new-unproved=%-6d new-proved/old-unproved=%-6d neither=%-6d",
			row.id, total.Agree, total.OldProvedNewUnproved, total.NewProvedOldUnproved, total.NeitherProved)
	}
	t.Logf("--- canonical totals: agree=%d old-proved/new-unproved=%d new-proved/old-unproved=%d neither=%d falsifiers=%d ---",
		grandTotal.Agree, grandTotal.OldProvedNewUnproved, grandTotal.NewProvedOldUnproved, grandTotal.NeitherProved, len(allFalsifiers))

	if len(allFalsifiers) > 0 {
		for index, falsifier := range allFalsifiers {
			t.Logf("FALSIFIER[%d]: %s", index, falsifier.Detail)
		}
		t.Fatalf("design falsifier: %d old-proved/new-unproved drop(s) on the canonical fixtures", len(allFalsifiers))
	}
}
