//go:build gts_parsercorephase0

package gotreesitter_test

import (
	"fmt"
	"os"
	"sort"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

// TestAdmissionCandidateScorecard206 is the admission scorecard's missing half:
// it drives the compact candidate route across all 206 registered languages
// (one smoke fixture each) through the public Parse API and records, per
// language, whether the compact route served a byte-exact tree, declined and
// fell back, or diverged. The compact route had only ever been validated on the
// four canonical Go fixtures; this run reports how far it reaches.
//
// Scope: the per-language fixtures are trivial smoke snippets, so this is the
// breadth ratchet -- which grammars the compact route accepts at all -- rather
// than corpus-scale fidelity proof. Frozen canonical and representative-depth
// digests provide the deeper companion gate.
//
// It is a scorecard, not a gate: it never fails on a fallback (a fallback is the
// fail-closed, correct behavior for an unsupported grammar). It fails only on a
// DIVERGE — the compact route accepted a clean tree that disagrees with
// production — because a silent wrong tree is the one outcome the admission
// contract forbids. Set GTS_ADMISSION_SCORECARD_STRICT=1 to also fail if any
// DIVERGE is observed even when only logging is wanted otherwise.
//
// Statuses:
//
//   - PASS     candidate routed and its tree digest equals production's;
//   - DIVERGE  candidate routed but its tree digest differs from production;
//   - FALLBACK candidate declined; production served the parse (fail-closed);
//   - SKIP     language is not routable via the DFA Parse path (token source);
//   - ERROR    production itself failed or a panic was recovered.
const (
	scorecardPass     = "PASS"
	scorecardDiverge  = "DIVERGE"
	scorecardFallback = "FALLBACK"
	scorecardSkip     = "SKIP"
	scorecardError    = "ERROR"
)

type scorecardRow struct {
	name    string
	backend string
	status  string
	detail  string
}

func TestAdmissionCandidateScorecard206(t *testing.T) {
	// This scorecard loads every registered grammar, which inflates process heap
	// enough to disturb the whole-process TestArenaGCRetentionAfterRelease gate.
	// It is a deliberate diagnostic, so gate it behind an explicit opt-in and
	// keep the routine suite unaffected. Run it with:
	//   GTS_ADMISSION_SCORECARD=1 go test -tags gts_parsercorephase0 \
	//     -run TestAdmissionCandidateScorecard206 -v .
	if os.Getenv("GTS_ADMISSION_SCORECARD") != "1" {
		t.Skip("set GTS_ADMISSION_SCORECARD=1 to run the 206-language admission scorecard")
	}
	// Purge the embedded grammar cache afterward so it does not inflate process
	// heap for later suite tests.
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entries := grammars.AllLanguages()
	rows := make([]scorecardRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, runAdmissionScorecardLanguage(entry))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	counts := map[string]int{}
	var divergences []scorecardRow
	for _, row := range rows {
		counts[row.status]++
		if row.status == scorecardDiverge {
			divergences = append(divergences, row)
		}
	}

	t.Logf("=== Phase-3 admission candidate scorecard (%d languages) ===", len(rows))
	for _, row := range rows {
		t.Logf("%-9s %-16s %-6s %s", row.status, row.name, row.backend, row.detail)
	}
	t.Logf("--- summary: PASS=%d DIVERGE=%d FALLBACK=%d SKIP=%d ERROR=%d total=%d ---",
		counts[scorecardPass], counts[scorecardDiverge], counts[scorecardFallback],
		counts[scorecardSkip], counts[scorecardError], len(rows))

	if len(divergences) > 0 {
		for _, row := range divergences {
			t.Logf("DIVERGENCE FINDING: %s (%s) %s", row.name, row.backend, row.detail)
		}
		if os.Getenv("GTS_ADMISSION_SCORECARD_STRICT") == "1" {
			t.Fatalf("%d language(s) diverged through the compact route", len(divergences))
		}
	}

	if os.Getenv("GTS_ADMISSION_SCORECARD_RATCHET") == "1" {
		// Frozen at the generalized clean-tail admission epoch. Improvements are
		// welcome (more PASS, fewer FALLBACK); silent correctness failures,
		// registry drift, or surrendered route coverage require an explicit
		// review and ratchet update.
		const (
			wantTotal   = 206
			minPass     = 166
			maxFallback = 35
			wantSkip    = 5
		)
		if len(rows) != wantTotal || counts[scorecardPass] < minPass ||
			counts[scorecardFallback] > maxFallback || counts[scorecardSkip] != wantSkip ||
			counts[scorecardDiverge] != 0 || counts[scorecardError] != 0 {
			t.Fatalf("admission breadth ratchet failed: PASS=%d (min %d) DIVERGE=%d FALLBACK=%d (max %d) SKIP=%d (want %d) ERROR=%d total=%d (want %d)",
				counts[scorecardPass], minPass, counts[scorecardDiverge], counts[scorecardFallback], maxFallback,
				counts[scorecardSkip], wantSkip, counts[scorecardError], len(rows), wantTotal)
		}
	}
}

func runAdmissionScorecardLanguage(entry grammars.LangEntry) (row scorecardRow) {
	row = scorecardRow{name: entry.Name, status: scorecardError}
	defer func() {
		if r := recover(); r != nil {
			row.status = scorecardError
			row.detail = fmt.Sprintf("panic: %v", r)
		}
	}()

	lang := entry.Language()
	if lang == nil {
		row.detail = "nil language"
		return row
	}
	support := grammars.EvaluateParseSupport(entry, lang)
	row.backend = string(support.Backend)

	// Only the fresh DFA Parse path is routable through the compact candidate.
	if support.Backend != grammars.ParseBackendDFA {
		row.status = scorecardSkip
		row.detail = "not DFA-routable: " + support.Reason
		return row
	}

	source := []byte(grammars.ParseSmokeSample(entry.Name))

	production := gts.NewParser(lang)
	production.SetAdmissionCandidateRoute(false)
	productionTree, err := production.Parse(source)
	if err != nil || productionTree == nil || productionTree.RootNode() == nil {
		row.detail = fmt.Sprintf("production parse failed: %v", err)
		return row
	}
	defer productionTree.Release()
	productionInspection, err := benchfixtures.InspectGoTree(productionTree.RootNode(), lang)
	if err != nil {
		row.detail = "production digest failed: " + err.Error()
		return row
	}

	candidate := gts.NewParser(lang)
	candidate.SetAdmissionCandidateRoute(true)
	gts.ResetAdmissionCandidateCountersForTest()
	candidateTree, err := candidate.Parse(source)
	if err != nil || candidateTree == nil {
		row.detail = fmt.Sprintf("candidate parse failed: %v", err)
		return row
	}
	defer candidateTree.Release()
	routed, _ := gts.AdmissionCandidateCounters()

	if routed == 0 {
		row.status = scorecardFallback
		row.detail = gts.AdmissionCandidateLastFallbackReason()
		return row
	}
	if candidateTree.RootNode() == nil {
		row.detail = "candidate routed but produced a nil root"
		return row
	}
	candidateInspection, err := benchfixtures.InspectGoTree(candidateTree.RootNode(), lang)
	if err != nil {
		row.detail = "candidate digest failed: " + err.Error()
		return row
	}
	if candidateInspection.SHA256 == productionInspection.SHA256 {
		row.status = scorecardPass
		row.detail = "digest " + candidateInspection.SHA256[:12]
		return row
	}
	row.status = scorecardDiverge
	row.detail = fmt.Sprintf("candidate=%s production=%s", candidateInspection.SHA256[:12], productionInspection.SHA256[:12])
	return row
}
