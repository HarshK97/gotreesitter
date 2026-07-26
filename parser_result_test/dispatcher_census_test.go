package parserresult_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// This file implements the R2 "census and remove inert language passes" step
// of docs/root-normalization-retirement.md for the per-language
// result-normalization dispatcher (runLanguageResultCompatibility's switch in
// parser_result_compat.go), which is registered as "dispatcher_arm" /
// "dispatcher_predicate" entries in testdata/result_compat_ownership_v1.json.
//
// KNOWN COVERAGE CHOICE. The generic.terminal-leaf census
// (parser_result_terminal_leaf_test.go, TestGenericTerminalLeafCensusMultiLanguage)
// documented that loading grammars with the root package's raw LoadLanguage
// attaches no external scanner (the root package cannot import
// gotreesitter/grammars without an import cycle), so many grammars parsed to
// an error root and their census was vacuous. This package
// (parser_result_test, package parserresult_test) is an external test package
// that CAN import gotreesitter/grammars, so this census uses the same
// grammars.LangEntry.Language() + grammars.AuditParseSupport() production
// loading path every real parser caller uses (see helpers_test.go's
// parseByLanguageName), closing that gap rather than reporting it as a limit.

// dispatcherOwnershipEntry mirrors the fields of
// testdata/result_compat_ownership_v1.json's entries this census needs. It is
// intentionally a minimal projection, not a full schema type, so this census
// stays a read-only consumer of the mechanically checked registry.
type dispatcherOwnershipEntry struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Languages []string `json:"languages"`
}

type dispatcherOwnershipRegistry struct {
	Entries []dispatcherOwnershipEntry `json:"entries"`
}

// dispatcherArmLanguages returns every language name the ownership registry
// lists under a "dispatcher_arm" or "dispatcher_predicate" entry, and maps
// each such language to a display arm ID (a language can only ever match one
// arm, per runLanguageResultCompatibility's switch, so this is a 1:1 map).
// Languages absent from this set have no dispatcher arm at all: the switch in
// parser_result_compat.go has no case for them, so they are out of scope for
// this census, not a coverage failure.
func dispatcherArmLanguages(t *testing.T) map[string]string {
	t.Helper()
	raw, readErr := os.ReadFile(filepath.Join("..", "testdata", "result_compat_ownership_v1.json"))
	if readErr != nil {
		t.Fatalf("read result_compat_ownership_v1.json: %v", readErr)
	}
	var reg dispatcherOwnershipRegistry
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatalf("parse result_compat_ownership_v1.json: %v", err)
	}
	langToArm := map[string]string{}
	for _, e := range reg.Entries {
		if e.Kind != "dispatcher_arm" && e.Kind != "dispatcher_predicate" {
			continue
		}
		for _, lang := range e.Languages {
			name := strings.ToLower(lang)
			if previous, exists := langToArm[name]; exists && previous != e.ID {
				t.Fatalf("ownership registry assigns language %q to both %q and %q", name, previous, e.ID)
			}
			langToArm[name] = e.ID
		}
	}
	if len(langToArm) == 0 {
		t.Fatal("ownership registry listed zero dispatcher_arm/dispatcher_predicate languages: the registry read is broken, not that there are no arms")
	}
	return langToArm
}

// dispatcherLangCensus accumulates the per-language receipt this test
// reports: how many corpus files were parsed, whether the registered arm
// actually ran (checked/run counts from (*Parser).recordNormalizationMetric),
// and the fingerprint-diff visited/rewritten totals.
type dispatcherLangCensus struct {
	armID      string
	files      int
	checked    uint64
	run        uint64
	visited    uint64
	rewritten  uint64
	rewriteIn  map[string]uint64 // file -> rewritten, only when > 0
	errorRoots int
}

// parseRealCorpusFile uses the production language loader and backend. It
// disables the admission candidate so the receipt measures the production
// parser lane.
func parseRealCorpusFile(entry grammars.LangEntry, backend grammars.ParseBackend, lang *gotreesitter.Language, src []byte) (*gotreesitter.Tree, error) {
	parser := gotreesitter.NewParser(lang)
	parser.SetAdmissionCandidateRoute(false)
	switch backend {
	case grammars.ParseBackendTokenSource:
		if entry.TokenSourceFactory == nil {
			return nil, fmt.Errorf("token source backend without factory")
		}
		return parser.ParseWithTokenSource(src, entry.TokenSourceFactory(src, lang))
	case grammars.ParseBackendDFA, grammars.ParseBackendDFAPartial:
		return parser.Parse(src)
	default:
		return nil, fmt.Errorf("unsupported backend %q", backend)
	}
}

// TestDispatcherArmCensusOverRealCorpus is the R2 census itself. It parses
// every file in cgo_harness/corpus_real under GTS_DISPATCHER_CENSUS=1 and
// reports, per language with a registered dispatcher arm, whether the arm
// ever rewrote the tree.
//
// This is a receipt, not a gate: it does not assert zero rewrites, and it
// does not delete anything. Turning an INERT receipt into a deletion needs
// the production, compact, forest, incremental, and C-oracle route parity
// the retirement plan requires (docs/root-normalization-retirement.md, R2).
func TestDispatcherArmCensusOverRealCorpus(t *testing.T) {
	t.Setenv("GTS_DISPATCHER_CENSUS", "1")

	const corpusRoot = "../cgo_harness/corpus_real"
	dirEntries, err := os.ReadDir(corpusRoot)
	if err != nil {
		t.Skipf("real corpus unavailable: %v", err)
	}

	armLanguages := dispatcherArmLanguages(t)

	langEntries := map[string]grammars.LangEntry{}
	for _, e := range grammars.AllLanguages() {
		langEntries[e.Name] = e
	}
	backends := map[string]grammars.ParseBackend{}
	for _, report := range grammars.AuditParseSupport() {
		backends[report.Name] = report.Backend
	}

	results := map[string]*dispatcherLangCensus{}
	uncoveredByLanguage := map[string]string{}
	var notApplicable []string // corpus dir has no registered dispatcher arm at all
	var noLangEntry []string
	var parseFailed []string

	for _, dirEntry := range dirEntries {
		if !dirEntry.IsDir() {
			continue
		}
		name := dirEntry.Name()

		armID, hasArm := armLanguages[name]

		langEntry, ok := langEntries[name]
		if !ok {
			noLangEntry = append(noLangEntry, name)
			continue
		}
		backend, ok := backends[name]
		if !ok {
			noLangEntry = append(noLangEntry, name)
			continue
		}

		files, err := os.ReadDir(filepath.Join(corpusRoot, name))
		if err != nil {
			continue
		}

		lang := langEntry.Language()
		census := &dispatcherLangCensus{armID: armID, rewriteIn: map[string]uint64{}}
		filesParsed := 0
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			src, err := os.ReadFile(filepath.Join(corpusRoot, name, f.Name()))
			if err != nil || len(src) == 0 {
				continue
			}
			tree, err := parseRealCorpusFile(langEntry, backend, lang, src)
			if err != nil {
				parseFailed = append(parseFailed, name+"/"+f.Name()+": "+err.Error())
				continue
			}
			if tree == nil || tree.RootNode() == nil {
				parseFailed = append(parseFailed, name+"/"+f.Name()+": nil tree/root")
				if tree != nil {
					tree.Release()
				}
				continue
			}
			filesParsed++
			if tree.RootNode().HasError() {
				census.errorRoots++
			}
			rt := tree.ParseRuntime()
			tree.Release()
			if rt.NormalizationPasses == nil {
				continue
			}
			for _, pass := range *rt.NormalizationPasses {
				if pass.Name != armID {
					continue
				}
				census.checked += pass.Checked
				census.run += pass.Run
				census.visited += pass.NodesVisited
				census.rewritten += pass.NodesRewritten
				if pass.NodesRewritten > 0 {
					census.rewriteIn[f.Name()] = pass.NodesRewritten
				}
			}
		}
		census.files = filesParsed
		if filesParsed == 0 {
			if hasArm {
				uncoveredByLanguage[name] = fmt.Sprintf("%s(files=0,errorRoots=0,arm=%s,reason=no-parsed-files)", name, armID)
			}
			continue
		}

		if !hasArm {
			notApplicable = append(notApplicable, fmt.Sprintf("%s(files=%d)", name, filesParsed))
			continue
		}
		if census.run == 0 {
			uncoveredByLanguage[name] = fmt.Sprintf("%s(files=%d,errorRoots=%d,arm=%s,reason=no-pass-record)", name, filesParsed, census.errorRoots, armID)
			continue
		}
		results[name] = census
	}

	for name, armID := range armLanguages {
		if _, covered := results[name]; covered {
			continue
		}
		if _, recorded := uncoveredByLanguage[name]; recorded {
			continue
		}
		uncoveredByLanguage[name] = fmt.Sprintf("%s(files=0,errorRoots=0,arm=%s,reason=no-corpus-directory)", name, armID)
	}

	var inert, active []string
	for name, c := range results {
		if c.rewritten > 0 {
			active = append(active, name)
		} else {
			inert = append(inert, name)
		}
	}
	uncovered := make([]string, 0, len(uncoveredByLanguage))
	for _, detail := range uncoveredByLanguage {
		uncovered = append(uncovered, detail)
	}
	sort.Strings(inert)
	sort.Strings(active)
	sort.Strings(uncovered)
	sort.Strings(notApplicable)
	sort.Strings(noLangEntry)
	sort.Strings(parseFailed)

	t.Logf("dispatcher-arm census: languages_censused=%d inert=%d active=%d uncovered=%d not_applicable=%d",
		len(results), len(inert), len(active), len(uncovered), len(notApplicable))

	t.Logf("INERT (ran, changed nothing) [%d]:", len(inert))
	for _, name := range inert {
		c := results[name]
		t.Logf("  %-14s arm=%-28s files=%d checked=%d run=%d visited=%d rewritten=0",
			name, c.armID, c.files, c.checked, c.run, c.visited)
	}

	t.Logf("ACTIVE (changed something) [%d]:", len(active))
	for _, name := range active {
		c := results[name]
		t.Logf("  %-14s arm=%-28s files=%d checked=%d run=%d visited=%d rewritten=%d",
			name, c.armID, c.files, c.checked, c.run, c.visited, c.rewritten)
		rewriteFiles := make([]string, 0, len(c.rewriteIn))
		for file := range c.rewriteIn {
			rewriteFiles = append(rewriteFiles, file)
		}
		sort.Strings(rewriteFiles)
		for i, file := range rewriteFiles {
			if i >= 5 {
				break
			}
			n := c.rewriteIn[file]
			t.Logf("      rewrote %d position(s) in %s", n, file)
		}
	}

	if len(uncovered) > 0 {
		t.Logf("UNCOVERED (registered arm, probe never observed a pass record) [%d]: %s", len(uncovered), strings.Join(uncovered, " "))
	}
	if len(notApplicable) > 0 {
		t.Logf("NOT APPLICABLE (no dispatcher arm registered for this language) [%d]: %s", len(notApplicable), strings.Join(notApplicable, " "))
	}
	if len(noLangEntry) > 0 {
		t.Logf("no grammars.LangEntry/backend found [%d]: %s", len(noLangEntry), strings.Join(noLangEntry, " "))
	}
	if len(parseFailed) > 0 {
		t.Logf("parse errors [%d]: %s", len(parseFailed), strings.Join(parseFailed, " "))
	}

	if len(results) == 0 {
		t.Fatal("dispatcher-arm census covered zero languages: the result is vacuous")
	}
}

type trackedDispatcherCensusFixture struct {
	Language       string `json:"language"`
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ArmID          string `json:"arm_id"`
	Checked        uint64 `json:"checked"`
	Run            uint64 `json:"run"`
	NodesVisited   uint64 `json:"nodes_visited"`
	NodesRewritten uint64 `json:"nodes_rewritten"`
	ErrorRoot      bool   `json:"error_root"`
}

type trackedDispatcherCensusReceipt struct {
	Schema     string                           `json:"schema"`
	Limitation string                           `json:"limitation"`
	Fixtures   []trackedDispatcherCensusFixture `json:"fixtures"`
}

// TestDispatcherArmCensusTrackedReceipt validates a small tracked corpus on
// every run. The full authenticated corpus remains an external release gate.
func TestDispatcherArmCensusTrackedReceipt(t *testing.T) {
	t.Setenv("GTS_DISPATCHER_CENSUS", "1")

	raw, err := os.ReadFile(filepath.Join("..", "testdata", "dispatcher_census_tracked_v1.json"))
	if err != nil {
		t.Fatalf("read tracked dispatcher census receipt: %v", err)
	}
	var receipt trackedDispatcherCensusReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatalf("parse tracked dispatcher census receipt: %v", err)
	}
	if receipt.Schema != "gts-dispatcher-census-tracked/v1" {
		t.Fatalf("tracked dispatcher census schema = %q", receipt.Schema)
	}
	if strings.TrimSpace(receipt.Limitation) == "" || len(receipt.Fixtures) == 0 {
		t.Fatal("tracked dispatcher census receipt lacks its limitation or fixtures")
	}

	armLanguages := dispatcherArmLanguages(t)
	langEntries := map[string]grammars.LangEntry{}
	for _, entry := range grammars.AllLanguages() {
		langEntries[entry.Name] = entry
	}
	backends := map[string]grammars.ParseBackend{}
	for _, report := range grammars.AuditParseSupport() {
		backends[report.Name] = report.Backend
	}

	previousKey := ""
	for _, fixture := range receipt.Fixtures {
		key := fixture.Language + "\x00" + fixture.Path
		if key <= previousKey {
			t.Fatalf("tracked dispatcher census fixtures are not strictly sorted at %q", key)
		}
		previousKey = key
		if fixture.Checked == 0 || fixture.Run == 0 || fixture.NodesVisited == 0 {
			t.Fatalf("%s has a vacuous tracked census receipt", fixture.Path)
		}
		armID, ok := armLanguages[fixture.Language]
		if !ok || armID != fixture.ArmID {
			t.Fatalf("%s arm = %q, want %q", fixture.Language, armID, fixture.ArmID)
		}
		entry, ok := langEntries[fixture.Language]
		if !ok {
			t.Fatalf("%s has no grammar entry", fixture.Language)
		}
		backend, ok := backends[fixture.Language]
		if !ok {
			t.Fatalf("%s has no parse backend", fixture.Language)
		}
		src, err := os.ReadFile(filepath.Join("..", filepath.FromSlash(fixture.Path)))
		if err != nil {
			t.Fatalf("read %s: %v", fixture.Path, err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(src)); got != fixture.SHA256 {
			t.Fatalf("%s SHA-256 = %s, want %s", fixture.Path, got, fixture.SHA256)
		}

		tree, err := parseRealCorpusFile(entry, backend, entry.Language(), src)
		if err != nil {
			t.Fatalf("%s parse: %v", fixture.Path, err)
		}
		if tree == nil || tree.RootNode() == nil {
			if tree != nil {
				tree.Release()
			}
			t.Fatalf("%s returned a nil tree or root", fixture.Path)
		}
		errorRoot := tree.RootNode().HasError()
		rt := tree.ParseRuntime()
		tree.Release()

		var checked, run, visited, rewritten uint64
		if rt.NormalizationPasses != nil {
			for _, pass := range *rt.NormalizationPasses {
				if pass.Name != fixture.ArmID {
					continue
				}
				checked += pass.Checked
				run += pass.Run
				visited += pass.NodesVisited
				rewritten += pass.NodesRewritten
			}
		}
		if checked != fixture.Checked || run != fixture.Run ||
			visited != fixture.NodesVisited || rewritten != fixture.NodesRewritten ||
			errorRoot != fixture.ErrorRoot {
			t.Errorf("%s census = checked:%d run:%d visited:%d rewritten:%d error_root:%t, want checked:%d run:%d visited:%d rewritten:%d error_root:%t",
				fixture.Path, checked, run, visited, rewritten, errorRoot,
				fixture.Checked, fixture.Run, fixture.NodesVisited, fixture.NodesRewritten, fixture.ErrorRoot)
		}
	}
}
