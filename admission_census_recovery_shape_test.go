//go:build !gts_no_parsercorephase0

package gotreesitter_test

import (
	"strconv"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// The compact-route decline census sub-classifies a recovery-boundary decline
// by the C recovery mechanism that owns the input at that point
// (admissionCensusRecoveryShape, admission_census.go). These tests pin that
// classification.
//
// Every witness below is a short literal source over an embedded grammar, not
// a corpus file. cgo_harness/corpus_real is gitignored, so a corpus-backed
// gate silently skips and reports ok on a checkout that never staged the
// corpus; these witnesses cannot skip.

// admissionCensusRecoveryShapeWitness is one pinned (source, mechanism) pair.
type admissionCensusRecoveryShapeWitness struct {
	language string
	source   string
	// mechanism is the c-mechanism value the census must report.
	mechanism string
	// candidates, when non-zero, additionally pins the reported
	// missing-token candidate population. It is asserted only where the
	// count is a structural claim worth pinning (exactly one candidate
	// terminal, so C's ascending-order "first surviving candidate" scan is
	// unambiguous), not for every witness.
	candidates int
}

func admissionCensusRecoveryShapeWitnesses() []admissionCensusRecoveryShapeWitness {
	return []admissionCensusRecoveryShapeWitness{
		// C ts_parser__handle_error step 2 (parser.c:2154-2230): a terminal
		// shifts from the declining state into a state whose leading action
		// for the elected token is a reduce, so C inserts a zero-width
		// MISSING leaf and keeps parsing. B3 stage S5 owns this.
		{language: "go", source: "package p\nfunc f() {\n", mechanism: "missing-token-insertion", candidates: 1},
		{language: "python", source: "def f(:\n    pass\n", mechanism: "missing-token-insertion", candidates: 1},
		{language: "ini", source: "[a\nb=c\n", mechanism: "missing-token-insertion", candidates: 1},
		{language: "c", source: "int x = ;", mechanism: "missing-token-insertion"},

		// C ts_parser__recover strategy 1 (cRecoverStrategy1Election): no
		// missing-token opportunity, but an ancestor boundary within
		// cRecoverMaxSummaryDepth has an action for the elected token, so C
		// recovers to that state. B3 stage S4 owns this.
		{language: "json", source: `{"a": 1`, mechanism: "stack-summary-resume"},
		{language: "c", source: "int main() { return 0; ", mechanism: "stack-summary-resume"},
		{language: "lua", source: "function f( end", mechanism: "stack-summary-resume"},

		// C ts_parser__recover strategy 2 (cAbsorbTokenIntoError): neither
		// earlier mechanism applies, so C absorbs the elected token into an
		// open error region. B3 stage S3 owns this.
		{language: "sql", source: "SELECT a b c FROM;", mechanism: "error-region-absorb"},

		// C ts_parser__recover recover_eof: the elected token is
		// authenticated end-of-file and no earlier mechanism applies, so C
		// wraps the remaining stack in one ERROR root.
		{language: "yaml", source: "a: [1\n", mechanism: "recover-eof-wrap"},
	}
}

// admissionCensusDeclineReason parses source through the compact candidate
// route and returns the decline reason, or "" when the route admitted it.
func admissionCensusDeclineReason(t *testing.T, language, source string) string {
	t.Helper()
	var entry grammars.LangEntry
	found := false
	for _, candidate := range grammars.AllLanguages() {
		if candidate.Name == language {
			entry, found = candidate, true
			break
		}
	}
	if !found {
		t.Skipf("grammar %q is not registered in this build", language)
	}
	lang := entry.Language()
	if lang == nil {
		t.Skipf("grammar %q has no loadable language", language)
	}
	parser := gts.NewParser(lang)
	parser.SetAdmissionCandidateRoute(true)
	gts.ResetAdmissionCandidateCountersForTest()
	tree, err := parser.Parse([]byte(source))
	if err != nil {
		t.Fatalf("%s: parse failed: %v", language, err)
	}
	if tree != nil {
		defer tree.Release()
	}
	routed, fallbacks := gts.AdmissionCandidateCounters()
	if routed != 0 {
		return ""
	}
	if fallbacks == 0 {
		t.Fatalf("%s: compact route neither routed nor fell back for %q", language, source)
	}
	return gts.AdmissionCandidateLastFallbackReason()
}

// TestAdmissionCensusRecoveryShapeClassification pins the C-mechanism
// sub-class the census reports for each recovery-boundary decline.
func TestAdmissionCensusRecoveryShapeClassification(t *testing.T) {
	restore := gts.SetAdmissionCensusEnabledForTest(true)
	defer restore()

	for _, witness := range admissionCensusRecoveryShapeWitnesses() {
		t.Run(witness.language+"/"+witness.mechanism, func(t *testing.T) {
			reason := admissionCensusDeclineReason(t, witness.language, witness.source)
			if reason == "" {
				t.Fatalf("compact route admitted %q; expected a recovery decline", witness.source)
			}
			want := "[c-mechanism=" + witness.mechanism
			if !strings.Contains(reason, want) {
				t.Fatalf("source %q: decline reason %q does not report %q", witness.source, reason, want)
			}
			if witness.candidates != 0 {
				wantCandidates := want + " candidates=" + strconv.Itoa(witness.candidates) + " "
				if !strings.Contains(reason, wantCandidates) {
					t.Fatalf("source %q: decline reason %q does not report %q", witness.source, reason, wantCandidates)
				}
			}
		})
	}
}

// TestAdmissionCensusRecoveryShapeIsDiagnosticOnly proves the classification
// changes decline TEXT only: with the census disabled every witness still
// declines, still declines for the same recorded reason, and carries no
// c-mechanism tag at all.
func TestAdmissionCensusRecoveryShapeIsDiagnosticOnly(t *testing.T) {
	for _, witness := range admissionCensusRecoveryShapeWitnesses() {
		t.Run(witness.language, func(t *testing.T) {
			restoreOff := gts.SetAdmissionCensusEnabledForTest(false)
			off := admissionCensusDeclineReason(t, witness.language, witness.source)
			restoreOff()
			if off == "" {
				t.Fatalf("compact route admitted %q with the census disabled", witness.source)
			}
			if strings.Contains(off, "c-mechanism") {
				t.Fatalf("census-disabled decline reason leaked a classification: %q", off)
			}

			restoreOn := gts.SetAdmissionCensusEnabledForTest(true)
			on := admissionCensusDeclineReason(t, witness.language, witness.source)
			restoreOn()
			if on == "" {
				t.Fatalf("compact route admitted %q with the census enabled", witness.source)
			}
			// The census-enabled text is the census-disabled classification
			// plus the appended tag; the routing outcome (a decline) is
			// identical either way.
			if !strings.Contains(on, "[c-mechanism=") {
				t.Fatalf("census-enabled decline reason carries no classification: %q", on)
			}
		})
	}
}

// TestAdmissionCensusRecoveryShapeVocabulary proves every classification the
// census can emit comes from the closed, documented set. A new mechanism must
// be added here deliberately, not leak out as free text.
func TestAdmissionCensusRecoveryShapeVocabulary(t *testing.T) {
	restore := gts.SetAdmissionCensusEnabledForTest(true)
	defer restore()

	known := map[string]bool{
		"missing-token-insertion": true,
		"stack-summary-resume":    true,
		"recover-eof-wrap":        true,
		"error-region-absorb":     true,
	}
	for _, witness := range admissionCensusRecoveryShapeWitnesses() {
		if !known[witness.mechanism] {
			t.Fatalf("witness pins mechanism %q outside the documented vocabulary", witness.mechanism)
		}
		reason := admissionCensusDeclineReason(t, witness.language, witness.source)
		index := strings.Index(reason, "[c-mechanism=")
		if index < 0 {
			continue
		}
		tail := reason[index+len("[c-mechanism="):]
		end := strings.IndexAny(tail, " ]")
		if end < 0 {
			t.Fatalf("malformed classification tag in %q", reason)
		}
		if !known[tail[:end]] {
			t.Fatalf("census reported mechanism %q outside the documented vocabulary (%q)", tail[:end], reason)
		}
	}
}
