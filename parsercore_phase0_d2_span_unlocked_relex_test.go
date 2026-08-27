//go:build gts_parsercorephase0

package gotreesitter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// D2-1 slice: span-unlocked per-header relex with fail-closed ragged-end
// decline. relexTokenForState used to reject any per-header relex whose
// span (StartByte and EndByte) did not exactly match the shared election's
// span. This slice unlocks EndByte: a same-start relex may now return a
// wider or narrower token than the shared election, and dispatchPassActive
// declines that shape fail-closed instead of shifting it (see
// diagnosticParserCoreRaggedRelexDeclineDetail's doc comment).
//
// bashRaggedEndWitnessSource ("A(1%") is a real, minimal witness found by
// targeted fuzzing of FuzzAdmissionRouteEquality's own bash lane (not
// hand-crafted): at byte 2, the shared election is a 1-byte token (symbol
// 160, span 2..3); the no-action header's own state (7338) relexes the
// same start byte to a 2-byte token (symbol 1, span 2..4). Reproduced
// deterministically via RelexTokenForStateForTest and
// RunStateDependentRelexSchedulerForTest below.
var bashRaggedEndWitnessSource = []byte("A(1%")

// TestRelexTokenForStateSpanUnlockedFindsRaggedEndOnRealBashWitness pins
// the probe's own contract (Phase 1 item 2) against the real witness above.
// Enabled (the zero-value default), the probe returns the wider token: it
// starts at the same byte as the shared election but ends two bytes later,
// with a different symbol -- exactly the "may differ in Symbol and/or
// EndByte/EndPoint" contract. Disabled
// (DisablePerHeaderSpanUnlockedRelex), the probe restores the pre-D2-1
// span-locked reject: an EndByte mismatch declines the same way a scan
// failure does, regardless of Symbol.
func TestRelexTokenForStateSpanUnlockedFindsRaggedEndOnRealBashWitness(t *testing.T) {
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entry := grammars.DetectLanguageByName("bash")
	if entry == nil {
		t.Fatal("bash is absent from the language registry")
	}
	lang := entry.Language()

	shared := gts.Token{
		Symbol:                   160,
		Text:                     "1",
		StartByte:                2,
		EndByte:                  3,
		StartPoint:               gts.Point{Column: 2},
		EndPoint:                 gts.Point{Column: 3},
		ExternalScannerToken:     true,
		ExternalScannerStartByte: 2,
	}
	const raggedState = gts.StateID(7338)

	relexed, ok := gts.RelexTokenForStateForTest(
		lang, bashRaggedEndWitnessSource, 1, gts.DiagnosticParserCorePrefixOptions{}, raggedState, shared,
	)
	if !ok {
		t.Fatalf("span-unlocked probe declined the real witness; want a wider token")
	}
	if relexed.StartByte != shared.StartByte {
		t.Fatalf("relexed StartByte = %d, want %d (same start as the shared election)", relexed.StartByte, shared.StartByte)
	}
	if relexed.EndByte != 4 || relexed.Symbol != 1 {
		t.Fatalf("relexed token = %+v, want a wider token (symbol=1, end=4)", relexed)
	}

	relexedDisabled, okDisabled := gts.RelexTokenForStateForTest(
		lang, bashRaggedEndWitnessSource, 1,
		gts.DiagnosticParserCorePrefixOptions{DisablePerHeaderSpanUnlockedRelex: true},
		raggedState, shared,
	)
	if okDisabled {
		t.Fatalf("disabled probe accepted a ragged-end relex = %+v, want span-locked decline", relexedDisabled)
	}
	if relexedDisabled != shared {
		t.Fatalf("disabled probe token = %+v, want the unchanged shared token %+v", relexedDisabled, shared)
	}
}

// TestDiagnosticParserCoreRaggedRelexDeclineDetailCarriesWiderTokenSpan pins
// the decline detail's exact format (Phase 1 item 4: receipts must record
// the wider token's own symbol and span) against the same real witness's
// exact values.
func TestDiagnosticParserCoreRaggedRelexDeclineDetailCarriesWiderTokenSpan(t *testing.T) {
	shared := gts.Token{Symbol: 160, StartByte: 2, EndByte: 3}
	relexed := gts.Token{Symbol: 1, StartByte: 2, EndByte: 4}

	detail := gts.DiagnosticParserCoreRaggedRelexDeclineDetailFormatForTest(relexed, shared)
	wantPrefix := gts.DiagnosticParserCoreRaggedRelexDeclineDetailForTest()
	if !strings.HasPrefix(detail, wantPrefix) {
		t.Fatalf("decline detail = %q, want prefix %q", detail, wantPrefix)
	}
	for _, want := range []string{"symbol=1", "span=2..4", "span=2..3"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("decline detail = %q, want it to contain %q", detail, want)
		}
	}
}

// TestCompactSchedulerNeverShiftsRaggedEndRelex is the scheduler-level
// fail-closed proof (Phase 2 item 1a) for the real witness above: across
// the whole parse, the compact scheduler never adopts the wider relexed
// token (symbol 1, ending at byte 4) at byte 2 -- every no-action drop
// recorded for that position keeps the shared election's own narrow token
// (symbol 160, ending at byte 3). This is the observable half of the
// contract TestRelexTokenForStateSpanUnlockedFindsRaggedEndOnRealBashWitness
// pins directly: the probe finds the wider token, but dispatchPassActive
// never lets a header shift onto it.
//
// This witness's own no-action head is drop-eligible every time it
// recurs (a sibling header keeps advancing the same pass), so the
// scheduler's own terminal Stop for this particular witness is the
// ordinary genuinely-empty-row boundary, not the ragged-end decline's own
// detail -- diagnosticParserCoreRaggedRelexDeclineDetail's own classifier
// branch is unreachable unless every live header in a pass is
// simultaneously ragged (never drop-eligible then, since dropping every
// head would leave none). No corpus file or 210+ seconds of targeted live
// fuzzing (FuzzAdmissionRouteEquality) produced that all-ragged shape; see
// the D2-1 report. The two tests above instead pin the decline path's own
// exact contract and detail format directly.
func TestCompactSchedulerNeverShiftsRaggedEndRelex(t *testing.T) {
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entry := grammars.DetectLanguageByName("bash")
	if entry == nil {
		t.Fatal("bash is absent from the language registry")
	}
	lang := entry.Language()

	receipt, err := gts.RunStateDependentRelexSchedulerForTest(lang, bashRaggedEndWitnessSource)
	if err != nil {
		t.Fatalf("compact scheduler: %v", err)
	}
	if receipt.Acceptance != nil {
		t.Fatal("compact scheduler unexpectedly accepted the ragged-end witness")
	}
	if len(receipt.NoActionDrops) == 0 {
		t.Fatal("compact scheduler recorded no no-action drops for the ragged-end witness")
	}
	for _, drop := range receipt.NoActionDrops {
		if drop.Token.StartByte != 2 {
			continue
		}
		if drop.Token.EndByte != 3 || drop.Token.Symbol != 160 {
			t.Fatalf("no-action drop at byte 2 = %+v, want the shared election's own narrow token (symbol=160, end=3), never the wider relexed one", drop.Token)
		}
	}
}

// TestDiagnosticParserCoreSameSpanRelexStillShiftsRealBashInstallWitness is
// the D2-1 Phase 2 item 1b anchor: a same-span, different-symbol relex must
// keep today's behavior exactly (it still shifts via the cell token, same
// as before this slice). The designated scala W2 anchor
// (TestDiagnosticParserCoreStateDependentRelexKeepsExactSpanBranch,
// parsercore_phase0_state_relex_test.go) is broken pre-existing on this
// branch, independent of D2-1 (confirmed against origin/main 8fbb9538 by
// git stash: both its raw scheduler call and its certified admission-
// scorecard call already fail there, for an unrelated G18/D6 alternative-
// set convergence reason -- see the D2-1 report). This test substitutes a
// real, unmodified grammar fixture instead: tree-sitter-bash's own
// examples/install.sh (committed at
// cgo_harness/corpus_real/bash/medium__install.sh), confirmed by direct
// code-path instrumentation during evidence gathering to exercise a
// successful same-span relex mid-parse.
//
// NoActionDrops is the discriminating signal, verified directly: forcing
// diagnosticParserCoreSameSpanRelex to always decline changes this exact
// fixture's NoActionDrops from 16 to 17 (one more header falls back to a
// genuine no-action drop instead of shifting via the relexed symbol), while
// every other Stop field (boundary, detail, byte offset) stays identical
// either way -- so NoActionDrops is the only field this regression would
// move, and pinning it here catches a future regression the terminal Stop
// alone would silently hide.
//
// Gated behind GTS_ADMISSION_REAL_CORPUS=1 like every other
// cgo_harness/corpus_real consumer in this package
// (admission_real_corpus_matrix_test.go): that directory is git-ignored and
// no CI job provisions it (docs/ci-gate-coverage.md), so this test must
// skip cleanly, not fail, when it is absent.
func TestDiagnosticParserCoreSameSpanRelexStillShiftsRealBashInstallWitness(t *testing.T) {
	if os.Getenv("GTS_ADMISSION_REAL_CORPUS") != "1" {
		t.Skip("set GTS_ADMISSION_REAL_CORPUS=1 to run this cgo_harness/corpus_real-backed witness")
	}
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entry := grammars.DetectLanguageByName("bash")
	if entry == nil {
		t.Fatal("bash is absent from the language registry")
	}
	lang := entry.Language()
	source, err := os.ReadFile(filepath.Join("cgo_harness", "corpus_real", "bash", "medium__install.sh"))
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := gts.RunStateDependentRelexSchedulerForTest(lang, source)
	if err != nil {
		t.Fatalf("compact scheduler: %v", err)
	}
	if got, want := receipt.Stop.Work.NoActionDrops, uint64(16); got != want {
		t.Fatalf("NoActionDrops = %d, want %d (a same-span relex regressed for tree-sitter-bash's examples/install.sh)", got, want)
	}
}

// TestRunStateDependentRelexSchedulerDisableOptionMatchesLegacyBehavior is
// the D2-1 Phase 2 item 1c anchor: DisablePerHeaderSpanUnlockedRelex
// restores the pre-D2-1 span-locked probe exactly for the real ragged-end
// witness (a same-span, different-symbol relex is unaffected by the
// option; see TestDiagnosticParserCoreSameSpanRelexStillShiftsRealBashInstallWitness).
func TestRunStateDependentRelexSchedulerDisableOptionMatchesLegacyBehavior(t *testing.T) {
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entry := grammars.DetectLanguageByName("bash")
	if entry == nil {
		t.Fatal("bash is absent from the language registry")
	}
	lang := entry.Language()

	enabled, err := gts.RunStateDependentRelexSchedulerForTest(lang, bashRaggedEndWitnessSource)
	if err != nil {
		t.Fatalf("enabled: compact scheduler: %v", err)
	}
	disabled, err := gts.RunStateDependentRelexSchedulerWithSpanUnlockedRelexDisabledForTest(lang, bashRaggedEndWitnessSource)
	if err != nil {
		t.Fatalf("disabled: compact scheduler: %v", err)
	}

	if enabled.Acceptance != nil || disabled.Acceptance != nil {
		t.Fatal("compact scheduler unexpectedly accepted the ragged-end witness")
	}
	// Both routes decline for the same reason here: the disable option only
	// removes the ragged-end probe's own new acceptance (relexTokenForState
	// never returns a wider token at all when disabled), so this witness's
	// drop-eligible no-action head takes the same ordinary path either way.
	if enabled.Stop.Boundary != disabled.Stop.Boundary || enabled.Stop.Detail != disabled.Stop.Detail {
		t.Fatalf("enabled stop = %+v, disabled stop = %+v; want identical legacy behavior for this witness",
			enabled.Stop, disabled.Stop)
	}
}
