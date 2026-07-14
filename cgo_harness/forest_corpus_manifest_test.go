package cgoharness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMaterializeAndLoadForestCorpusManifestAuthenticatesSources(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	corpusRevision := initForestManifestTestRepo(t, root, "bash", map[string]string{
		"small.sh": "echo small\n",
		"large.sh": "echo a larger authenticated source\n",
		"skip.txt": "not selected\n",
	})
	lockPath := filepath.Join(root, "corpus_sources.lock")
	lock := fmt.Sprintf("bash https://example.invalid/bash %s . .sh\n", corpusRevision)
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := MaterializeForestCorpusManifest(ForestCorpusMaterializeOptions{
		GotreesitterRevision: revision,
		CorpusLockPath:       lockPath,
		CorpusRoot:           root,
		Languages:            []string{"bash"},
		Selection: ForestCorpusSelection{
			Order: "largest", MaxFiles: 1,
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "large.sh" || manifest.Files[0].Revision != corpusRevision {
		t.Fatalf("files = %#v", manifest.Files)
	}
	manifestPath := filepath.Join(root, "forest.json")
	if err := WriteForestCorpusManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("manifest mode = %v, want 0644", info.Mode().Perm())
	}
	lockDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(lock)))
	gotManifest, files, err := LoadForestCorpusManifest(manifestPath, root, lockPath, revision, lockDigest)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if gotManifest.GotreesitterRevision != revision || len(files["bash"]) != 1 {
		t.Fatalf("manifest=%#v files=%#v", gotManifest, files)
	}

	if err := os.WriteFile(files["bash"][0], []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadForestCorpusManifest(manifestPath, root, lockPath, revision, lockDigest); err == nil || !strings.Contains(err.Error(), "checkout is dirty") && !strings.Contains(err.Error(), "byte count") && !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("tampered load error = %v", err)
	}
}

func TestMaterializeForestCorpusManifestUsesCanonicalBasenamesWithoutRegistryExtensions(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	corpusRevision := initForestManifestTestRepo(t, root, "gomod", map[string]string{
		"go.mod":          "module example.invalid/test\n",
		"nested/go.mod":   "module example.invalid/nested\n",
		"go.sum":          "not selected\n",
		"test/corpus.txt": "not selected\n",
	})
	lockPath := filepath.Join(root, "corpus_sources.lock")
	lock := fmt.Sprintf("gomod https://example.invalid/gomod %s .\n", corpusRevision)
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := MaterializeForestCorpusManifest(ForestCorpusMaterializeOptions{
		GotreesitterRevision: revision,
		CorpusLockPath:       lockPath,
		CorpusRoot:           root,
		Languages:            []string{"gomod"},
		Selection:            ForestCorpusSelection{Order: "path"},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].Path != "go.mod" || manifest.Files[1].Path != "nested/go.mod" {
		t.Fatalf("files = %#v", manifest.Files)
	}
}

func TestLoadForestCorpusManifestRejectsRevisionAndCheckoutDrift(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	corpusRevision := initForestManifestTestRepo(t, root, "go", map[string]string{"input.go": "package p\n"})
	lockPath := filepath.Join(root, "corpus_sources.lock")
	lock := fmt.Sprintf("go https://example.invalid/go %s . .go\n", corpusRevision)
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := MaterializeForestCorpusManifest(ForestCorpusMaterializeOptions{
		GotreesitterRevision: revision, CorpusLockPath: lockPath, CorpusRoot: root,
		Languages: []string{"go"}, Selection: ForestCorpusSelection{Order: "path"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := WriteForestCorpusManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	lockDigest := manifest.CorpusLock.SHA256
	if _, _, err := LoadForestCorpusManifest(manifestPath, root, lockPath, strings.Repeat("b", 40), lockDigest); err == nil || !strings.Contains(err.Error(), "executing") {
		t.Fatalf("revision mismatch error = %v", err)
	}

	runForestManifestGit(t, filepath.Join(root, "go"), "commit", "--allow-empty", "-m", "drift")
	if _, _, err := LoadForestCorpusManifest(manifestPath, root, lockPath, revision, lockDigest); err == nil || !strings.Contains(err.Error(), "checkout revision") {
		t.Fatalf("checkout drift error = %v", err)
	}
}

func TestLoadForestCorpusManifestLanguageShardAuthenticatesOnlySelection(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	goRevision := initForestManifestTestRepo(t, root, "go", map[string]string{
		"input.go": "package p\n",
	})
	rustRevision := initForestManifestTestRepo(t, root, "rust", map[string]string{
		"input.rs": "fn main() {}\n",
	})
	lockPath := filepath.Join(root, "corpus_sources.lock")
	lock := fmt.Sprintf("go https://example.invalid/go %s . .go\nrust https://example.invalid/rust %s . .rs\n", goRevision, rustRevision)
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := MaterializeForestCorpusManifest(ForestCorpusMaterializeOptions{
		GotreesitterRevision: revision, CorpusLockPath: lockPath, CorpusRoot: root,
		Languages: []string{"go", "rust"}, Selection: ForestCorpusSelection{Order: "path"},
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := WriteForestCorpusManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}

	rustPath := filepath.Join(root, "rust", "input.rs")
	if err := os.WriteFile(rustPath, []byte("fn changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, files, err := loadForestCorpusManifestLanguages(
		manifestPath, root, lockPath, revision, manifest.CorpusLock.SHA256, []string{"go"},
	)
	if err != nil {
		t.Fatalf("load selected Go shard: %v", err)
	}
	if len(files) != 1 || len(files["go"]) != 1 || files["rust"] != nil {
		t.Fatalf("selected files = %#v", files)
	}
	if _, _, err := loadForestCorpusManifestLanguages(
		manifestPath, root, lockPath, revision, manifest.CorpusLock.SHA256, []string{"rust"},
	); err == nil || !strings.Contains(err.Error(), "checkout is dirty") {
		t.Fatalf("selected tampered Rust load error = %v", err)
	}
	if _, _, err := LoadForestCorpusManifest(
		manifestPath, root, lockPath, revision, manifest.CorpusLock.SHA256,
	); err == nil || !strings.Contains(err.Error(), "checkout is dirty") {
		t.Fatalf("full-manifest tampered Rust load error = %v", err)
	}
}

func TestMaterializeAndLoadForestCorpusManifestAllowsLockedGeneratedSources(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 40)
	corpusRevision := initForestManifestTestRepo(t, root, "comment", map[string]string{
		"tracked.txt": "tracked source identity\n",
	})
	generatedPath := filepath.Join(root, "comment", ".gts-extracted", "comment", "input.c")
	if err := os.MkdirAll(filepath.Dir(generatedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	generatedSource := []byte("// generated corpus source\n")
	if err := os.WriteFile(generatedPath, generatedSource, 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "corpus_sources.lock")
	lock := fmt.Sprintf("comment https://example.invalid/comment %s .gts-extracted/comment .c\n", corpusRevision)
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := MaterializeForestCorpusManifest(ForestCorpusMaterializeOptions{
		GotreesitterRevision: revision, CorpusLockPath: lockPath, CorpusRoot: root,
		Languages: []string{"comment"}, Selection: ForestCorpusSelection{Order: "path"},
	})
	if err != nil {
		t.Fatalf("materialize generated corpus: %v", err)
	}
	wantPath := ".gts-extracted/comment/input.c"
	if len(manifest.Files) != 1 || manifest.Files[0].Path != wantPath {
		t.Fatalf("files = %#v, want %s", manifest.Files, wantPath)
	}
	manifestPath := filepath.Join(root, "manifest.json")
	if err := WriteForestCorpusManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	_, files, err := LoadForestCorpusManifest(manifestPath, root, lockPath, revision, manifest.CorpusLock.SHA256)
	if err != nil {
		t.Fatalf("load generated corpus: %v", err)
	}
	if len(files["comment"]) != 1 || files["comment"][0] != generatedPath {
		t.Fatalf("files = %#v, want %s", files, generatedPath)
	}
	checkout := filepath.Join(root, "comment")
	runForestManifestGit(t, checkout, "remote", "set-url", "origin", "https://example.invalid/wrong")
	if _, _, err := LoadForestCorpusManifest(manifestPath, root, lockPath, revision, manifest.CorpusLock.SHA256); err == nil ||
		!strings.Contains(err.Error(), "checkout origin") {
		t.Fatalf("origin drift load error = %v", err)
	}
	runForestManifestGit(t, checkout, "remote", "set-url", "origin", "https://example.invalid/comment")

	tamperedSource := append([]byte(nil), generatedSource...)
	tamperedSource[0] = '!'
	if err := os.WriteFile(generatedPath, tamperedSource, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadForestCorpusManifest(manifestPath, root, lockPath, revision, manifest.CorpusLock.SHA256); err == nil ||
		!strings.Contains(err.Error(), "sha256") {
		t.Fatalf("tampered generated load error = %v", err)
	}
}

func TestVerifyForestCorpusCheckoutRejectsDirtOutsideExactGeneratedSubdir(t *testing.T) {
	tests := map[string]struct {
		subdir string
		mutate func(t *testing.T, checkout string)
	}{
		"untracked elsewhere": {
			subdir: ".gts-extracted/comment",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, "outside.c", "outside\n")
			},
		},
		"sibling prefix": {
			subdir: ".gts-extracted/comment",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, ".gts-extracted/commentary/input.c", "sibling\n")
			},
		},
		"broader generated directory": {
			subdir: ".gts-extracted",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, ".gts-extracted/comment/input.c", "broad\n")
			},
		},
		"other language directory": {
			subdir: ".gts-extracted/other",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, ".gts-extracted/other/input.c", "other\n")
			},
		},
		"traversal declaration": {
			subdir: ".gts-extracted/comment/../comment",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, ".gts-extracted/comment/input.c", "traversal\n")
			},
		},
		"tracked modification": {
			subdir: ".gts-extracted/comment",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, "tracked.txt", "modified\n")
			},
		},
		"tracked deletion": {
			subdir: ".gts-extracted/comment",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				if err := os.Remove(filepath.Join(checkout, "tracked.txt")); err != nil {
					t.Fatal(err)
				}
			},
		},
		"staged generated source": {
			subdir: ".gts-extracted/comment",
			mutate: func(t *testing.T, checkout string) {
				t.Helper()
				writeForestManifestTestFile(t, checkout, ".gts-extracted/comment/input.c", "staged\n")
				runForestManifestGit(t, checkout, "add", ".gts-extracted/comment/input.c")
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			revision := initForestManifestTestRepo(t, root, "comment", map[string]string{
				"tracked.txt": "tracked\n",
			})
			checkout := filepath.Join(root, "comment")
			test.mutate(t, checkout)
			entry := forestCorpusLockEntry{
				Language: "comment", Repository: "https://example.invalid/comment",
				Revision: revision, Subdir: test.subdir,
			}
			if err := verifyForestCorpusCheckout(root, entry); err == nil || !strings.Contains(err.Error(), "checkout is dirty") {
				t.Fatalf("verify error = %v", err)
			}
		})
	}
}

func TestParseForestCorpusLockRejectsGeneratedSubdirTraversal(t *testing.T) {
	revision := strings.Repeat("a", 40)
	lock := fmt.Sprintf("comment https://example.invalid/comment %s .gts-extracted/comment/../comment .c\n", revision)
	if _, err := parseForestCorpusLock("corpus_sources.lock", []byte(lock)); err == nil || !strings.Contains(err.Error(), "invalid corpus subdir path") {
		t.Fatalf("parse error = %v", err)
	}
}

func TestReduceForestAuditResultsIsDeterministicAndResumable(t *testing.T) {
	dir := t.TempDir()
	manifest := ForestCorpusManifest{
		Schema: ForestCorpusManifestSchema, GotreesitterRevision: strings.Repeat("a", 40),
		CorpusLock: ForestCorpusManifestLock{Path: "corpus.lock", SHA256: strings.Repeat("b", 64)},
		Selection:  ForestCorpusSelection{Order: "largest", MaxFiles: 8},
		Files: []ForestCorpusManifestFile{
			{Language: "go", Repository: "repo", Revision: strings.Repeat("c", 40), Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64)},
			{Language: "rust", Repository: "repo", Revision: strings.Repeat("e", 40), Path: "a.rs", Bytes: 1, SHA256: strings.Repeat("f", 64)},
		},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := WriteForestCorpusManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	result, err := NewForestAuditResult("production", manifest.GotreesitterRevision, manifestPath, manifest.CorpusLock.SHA256, "go")
	if err != nil {
		t.Fatal(err)
	}
	result.Files = []ForestAuditFileResult{{
		Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64), Disposition: "accepted",
		Forest: forestAuditTestAcceptedOutcome(1, true), Peer: forestAuditTestAcceptedOutcome(1, false),
		Routed: forestAuditTestAcceptedOutcome(1, true), RoutedProvenance: forestAuditRouteForestFastPath, RoutedNanos: 1,
	}}
	result.FilesTotal, result.FilesAccepted = 1, 1
	result.FilesRoutedForest = 1
	result.PeerNanos, result.RoutedNanos, result.RoutedImproved = 2, 1, true
	if err := WriteForestAuditResult(filepath.Join(dir, "results", "production", "go.json"), result); err != nil {
		t.Fatal(err)
	}
	bad := result
	bad.Files = append([]ForestAuditFileResult(nil), result.Files...)
	bad.Files[0].Bytes++
	bad.Files[0].Forest = forestAuditTestAcceptedOutcome(2, true)
	bad.Files[0].Peer = forestAuditTestAcceptedOutcome(2, false)
	bad.Files[0].Routed = forestAuditTestAcceptedOutcome(2, true)
	badRoot := filepath.Join(dir, "bad-results")
	if err := WriteForestAuditResult(filepath.Join(badRoot, "production", "go.json"), bad); err != nil {
		t.Fatal(err)
	}
	if _, err := ReduceForestAuditResults(manifestPath, badRoot); err == nil || !strings.Contains(err.Error(), "identity does not match manifest") {
		t.Fatalf("mismatched result identity error = %v", err)
	}
	report, err := ReduceForestAuditResults(manifestPath, filepath.Join(dir, "results"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "incomplete" || report.LanguagesComplete != 0 || len(report.Languages) != 2 {
		t.Fatalf("partial report = %#v", report)
	}

	for _, mode := range []string{"c_oracle"} {
		peer, err := NewForestAuditResult(mode, manifest.GotreesitterRevision, manifestPath, manifest.CorpusLock.SHA256, "go")
		if err != nil {
			t.Fatal(err)
		}
		peer.Files = []ForestAuditFileResult{{
			Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64), Disposition: "accepted",
			Forest: forestAuditTestAcceptedOutcome(1, true), Peer: forestAuditTestAcceptedOutcome(1, false),
			Routed: forestAuditTestNotRunOutcome(1), RoutedProvenance: forestAuditRouteNotRun,
		}}
		peer.FilesTotal, peer.FilesAccepted = 1, 1
		if err := WriteForestAuditResult(filepath.Join(dir, "results", mode, "go.json"), peer); err != nil {
			t.Fatal(err)
		}
	}
	report, err = ReduceForestAuditResults(manifestPath, filepath.Join(dir, "results"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "incomplete" || report.LanguagesComplete != 0 || report.Languages[0].Language != "go" || report.Languages[0].Status != "incomplete" ||
		!report.Languages[0].ScreeningEligible || report.Languages[0].PromotionEligible ||
		report.Languages[0].CorrectnessRelation != forestAuditRelationProductionIdentity ||
		report.Languages[0].ConfirmationStatus != forestAuditConfirmationRequired ||
		report.Languages[0].RoutedSpeedup != 2 || report.Languages[0].ScreenRoutedSpeedup != 2 ||
		!reflect.DeepEqual(report.Languages[0].PromotionBlockers, []string{forestAuditConfirmationRequired}) {
		t.Fatalf("resumed report = %#v", report)
	}
	data, err := json.Marshal(report)
	if err != nil || len(data) == 0 {
		t.Fatalf("marshal report: bytes=%d err=%v", len(data), err)
	}

	verifyForestAuditConfirmationLayout(t, manifestPath, filepath.Join(dir, "results"), result)
}

type forestAuditConfirmationLayoutFixture struct {
	runConfigSHA  string
	runConfigPath string
	runConfig     ForestAuditRunConfig
	finalPath     string
	finalTrial    ForestAuditConfirmationTrial
	indexPath     string
	cohortSHA     string
}

func verifyForestAuditConfirmationLayout(t *testing.T, manifestPath, resultsRoot string, screen ForestAuditResult) {
	t.Helper()
	fixture := writeForestAuditTestConfirmationChain(t, resultsRoot, screen)
	report, err := ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, fixture.indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Languages[0].PromotionEligible || report.Languages[0].ConfirmationStatus != forestAuditConfirmationConfirmed ||
		report.Status != "incomplete" || report.LanguagesComplete != 1 || len(report.Languages[0].PromotionBlockers) != 0 ||
		report.ConfirmationIndexSHA256+".json" != filepath.Base(fixture.indexPath) ||
		report.Languages[0].ConfirmationCohortSHA256 != fixture.cohortSHA {
		t.Fatalf("confirmed report = %#v", report)
	}
	verifyForestAuditCLIConfirmationFlow(t, manifestPath, resultsRoot, fixture)
	verifyForestAuditConfirmationTamperRejection(t, manifestPath, resultsRoot, fixture)
}

func verifyForestAuditCLIConfirmationFlow(t *testing.T, manifestPath, resultsRoot string, fixture forestAuditConfirmationLayoutFixture) {
	t.Helper()
	runCLI := func(args ...string) {
		t.Helper()
		command := exec.Command("go", append([]string{"run", "./cmd/forest_audit"}, args...)...)
		command.Dir = "."
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("forest_audit %v: %v\n%s", args, err, output)
		}
	}
	runCLI("publish-confirmation", "--results-root", resultsRoot, "--run-config", fixture.runConfigPath, "--trial", fixture.finalPath)
	runCLI("publish-result", "--results-root", resultsRoot, "--result", filepath.Join(resultsRoot, "production", "go.json"))
	reportPath := filepath.Join(t.TempDir(), "report.json")
	runCLI("reduce", "--manifest", manifestPath, "--results-root", resultsRoot, "--confirmation-index", fixture.indexPath, "--out", reportPath)
	runCLI("index-confirmations", "--report", reportPath, "--results-root", resultsRoot, "--cohorts", "go="+fixture.cohortSHA)
	planPath := filepath.Join(resultsRoot, "confirmation", "plan.json")
	runCLI("plan-confirmations", "--report", reportPath, "--out", planPath)
	secondReportPath := filepath.Join(t.TempDir(), "report.json")
	runCLI("reduce", "--manifest", manifestPath, "--results-root", resultsRoot, "--confirmation-index", fixture.indexPath, "--out", secondReportPath)
}

func writeForestAuditTestConfirmationChain(t *testing.T, resultsRoot string, screen ForestAuditResult) forestAuditConfirmationLayoutFixture {
	t.Helper()
	runConfigSHA, stagedRunConfigPath, runConfig := writeForestAuditTestRunConfig(t)
	var previousTrialSHA string
	var finalTrialPath, finalIndexPath, finalCohortSHA string
	var finalTrial ForestAuditConfirmationTrial
	for _, spec := range []struct {
		id, order   string
		repeats     int
		peerNanos   int64
		routedNanos int64
	}{
		{id: forestAuditConfirmationPairA, order: ForestAuditOrderProductionFirst, repeats: 1, peerNanos: 1_030_000_000, routedNanos: 1_000_000_000},
		{id: forestAuditConfirmationPairB, order: ForestAuditOrderRoutedFirst, repeats: 1, peerNanos: 1_020_000_000, routedNanos: 1_000_000_000},
		{id: forestAuditConfirmationABBAA1, order: ForestAuditOrderProductionFirst, repeats: 2, peerNanos: 2_400_000_000, routedNanos: 2_000_000_000},
		{id: forestAuditConfirmationABBAB1, order: ForestAuditOrderRoutedFirst, repeats: 2, peerNanos: 2_300_000_000, routedNanos: 2_000_000_000},
		{id: forestAuditConfirmationABBAB2, order: ForestAuditOrderRoutedFirst, repeats: 2, peerNanos: 2_200_000_000, routedNanos: 2_000_000_000},
		{id: forestAuditConfirmationABBAA2, order: ForestAuditOrderProductionFirst, repeats: 2, peerNanos: 2_300_000_000, routedNanos: 2_000_000_000},
	} {
		confirmedResult := screen
		confirmedResult.Files = append([]ForestAuditFileResult(nil), screen.Files...)
		confirmedResult.PeerNanos = spec.peerNanos
		confirmedResult.RoutedNanos = spec.routedNanos
		confirmedResult.RoutedImproved = true
		confirmedResult.Files[0].RoutedNanos = spec.routedNanos
		trial, err := NewForestAuditConfirmationTrial(spec.id, runConfigSHA, previousTrialSHA, ForestAuditRunPlan{
			ExecutionOrder: spec.order, RepeatCount: spec.repeats,
		}, confirmedResult)
		if err != nil {
			t.Fatal(err)
		}
		trialPath := filepath.Join(t.TempDir(), spec.id+".json")
		if err := WriteForestAuditConfirmationTrial(trialPath, trial); err != nil {
			t.Fatal(err)
		}
		stored, err := StoreForestAuditConfirmation(resultsRoot, stagedRunConfigPath, trialPath)
		if err != nil {
			t.Fatal(err)
		}
		previousTrialSHA = stored.TrialSHA256
		finalTrialPath = filepath.Join(resultsRoot, "confirmation", "trials", stored.TrialSHA256+".json")
		finalIndexPath, finalCohortSHA, finalTrial = stored.IndexPath, stored.CohortSHA256, trial
	}
	return forestAuditConfirmationLayoutFixture{
		runConfigSHA: runConfigSHA, runConfigPath: filepath.Join(resultsRoot, "confirmation", "run-configs", runConfigSHA+".json"), runConfig: runConfig,
		finalPath: finalTrialPath, finalTrial: finalTrial, indexPath: finalIndexPath, cohortSHA: finalCohortSHA,
	}
}

func verifyForestAuditConfirmationTamperRejection(t *testing.T, manifestPath, resultsRoot string, fixture forestAuditConfirmationLayoutFixture) {
	t.Helper()
	if err := os.Remove(fixture.runConfigPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, fixture.indexPath); err == nil || !strings.Contains(err.Error(), "read run config") {
		t.Fatalf("missing run config error = %v", err)
	}
	if err := writeForestAuditJSON(fixture.runConfigPath, fixture.runConfig); err != nil {
		t.Fatal(err)
	}
	forgedConfig := fixture.runConfig
	forgedConfig.HostLabel = "forged-host"
	if err := writeForestAuditJSON(fixture.runConfigPath, forgedConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, fixture.indexPath); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("forged run config error = %v", err)
	}
	if err := writeForestAuditJSON(fixture.runConfigPath, fixture.runConfig); err != nil {
		t.Fatal(err)
	}
	fixture.finalTrial.Result.PeerNanos += 500_000_000
	fixture.finalTrial.Result.Files[0].RoutedNanos = fixture.finalTrial.Result.RoutedNanos
	if err := writeForestAuditJSON(fixture.finalPath, fixture.finalTrial); err != nil {
		t.Fatal(err)
	}
	if _, err := ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, fixture.indexPath); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("tampered head timing error = %v", err)
	}
	fixture.finalTrial.Result.PeerNanos -= 500_000_000
	if err := writeForestAuditJSON(fixture.finalPath, fixture.finalTrial); err != nil {
		t.Fatal(err)
	}
	var index ForestAuditConfirmationIndex
	if err := readForestAuditJSON(fixture.indexPath, &index); err != nil {
		t.Fatal(err)
	}
	index.Cohorts[0].CohortSHA256 = strings.Repeat("f", 64)
	if err := writeForestAuditJSON(fixture.indexPath, index); err != nil {
		t.Fatal(err)
	}
	if _, err := ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, fixture.indexPath); err == nil || !strings.Contains(err.Error(), "not named by content hash") {
		t.Fatalf("forged confirmation index error = %v", err)
	}
}

func writeForestAuditTestRunConfig(t *testing.T) (string, string, ForestAuditRunConfig) {
	t.Helper()
	config := ForestAuditRunConfig{
		Schema: ForestAuditRunConfigSchema, HostLabel: "quiet-box", HostFingerprint: strings.Repeat("b", 64), ImageID: "sha256:" + strings.Repeat("a", 64),
		Memory: "4g", CPUs: "1", CPUSetCPUs: "7", PIDs: "4096", GoMemLimit: "3GiB", GOMAXPROCS: 1, TestTimeout: "10m",
	}
	tempPath := filepath.Join(t.TempDir(), "run-config.json")
	if err := writeForestAuditJSON(tempPath, config); err != nil {
		t.Fatal(err)
	}
	digest, err := forestFileSHA256(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	return digest, tempPath, config
}

func TestReduceForestAuditResultsClassifiesCFirstNoCoverage(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		decline              string
		wantRowStatus        string
		wantClassification   string
		wantReportStatus     string
		wantComplete         int
		wantNoCoverage       int
		wantPromotionBlocker []string
	}{
		{
			name: "completed semantic declines", decline: "parse_failed",
			wantRowStatus: "pass", wantClassification: forestAuditClassNoForestCoverage, wantReportStatus: "pass",
			wantComplete: 1, wantNoCoverage: 1,
			wantPromotionBlocker: []string{forestAuditClassNoForestCoverage},
		},
		{
			name: "timeout is ambiguous", decline: "timeout",
			wantRowStatus: "incomplete", wantReportStatus: "incomplete",
			wantPromotionBlocker: []string{"missing_production", "c_oracle_timeout", "c_oracle_failed"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := ForestCorpusManifest{
				Schema: ForestCorpusManifestSchema, GotreesitterRevision: strings.Repeat("a", 40),
				CorpusLock: ForestCorpusManifestLock{Path: "corpus.lock", SHA256: strings.Repeat("b", 64)},
				Selection:  ForestCorpusSelection{Order: "largest", MaxFiles: 8},
				Files: []ForestCorpusManifestFile{{
					Language: "go", Repository: "repo", Revision: strings.Repeat("c", 40),
					Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64),
				}},
			}
			manifestPath := filepath.Join(dir, "manifest.json")
			if err := WriteForestCorpusManifest(manifestPath, manifest); err != nil {
				t.Fatal(err)
			}
			result, err := NewForestAuditResult("c_oracle", manifest.GotreesitterRevision, manifestPath, manifest.CorpusLock.SHA256, "go")
			if err != nil {
				t.Fatal(err)
			}
			result.Status = "fail"
			result.FilesTotal, result.FilesDeclined = 1, 1
			notRun := forestAuditTestNotRunOutcome(1)
			result.Files = []ForestAuditFileResult{{
				Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64),
				Forest: notRun, Peer: notRun, Routed: notRun,
				RoutedProvenance: forestAuditRouteNotRun,
				Disposition:      "declined", Decline: tc.decline,
			}}
			if err := WriteForestAuditResult(filepath.Join(dir, "results", "c_oracle", "go.json"), result); err != nil {
				t.Fatal(err)
			}
			// A resumed result directory may still contain the previous reduced
			// report. Schema evolution must not make that artifact look like a
			// malformed per-language result shard.
			if err := os.WriteFile(filepath.Join(dir, "results", "report-v2.json"), []byte(`{"schema":"forest-audit-report-v2"}`), 0o644); err != nil {
				t.Fatal(err)
			}

			report, err := ReduceForestAuditResults(manifestPath, filepath.Join(dir, "results"))
			if err != nil {
				t.Fatal(err)
			}
			if report.Status != tc.wantReportStatus || report.LanguagesComplete != tc.wantComplete ||
				report.LanguagesNoCoverage != tc.wantNoCoverage || len(report.Languages) != 1 {
				t.Fatalf("report = %#v", report)
			}
			row := report.Languages[0]
			if row.Status != tc.wantRowStatus || row.Classification != tc.wantClassification || row.PromotionEligible ||
				!reflect.DeepEqual(row.PromotionBlockers, tc.wantPromotionBlocker) {
				t.Fatalf("row = %#v", row)
			}
		})
	}
}

func TestReduceForestAuditResultsRejectsNonExecutableManifest(t *testing.T) {
	manifest := ForestCorpusManifest{
		Schema: ForestCorpusManifestSchema, GotreesitterRevision: strings.Repeat("a", 40),
		CorpusLock: ForestCorpusManifestLock{Path: "corpus.lock", SHA256: strings.Repeat("b", 64)},
		Selection:  ForestCorpusSelection{Order: "largest"},
		Files: []ForestCorpusManifestFile{{
			Language: "go", Repository: "repo", Revision: strings.Repeat("c", 40),
			Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64),
		}},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"unknown":true}`)...)
	trailing := append(append([]byte(nil), encoded...), []byte("\n{}")...)
	for name, data := range map[string][]byte{"unknown": unknown, "trailing": trailing} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			manifestPath := filepath.Join(dir, "manifest.json")
			if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := ReduceForestAuditResults(manifestPath, filepath.Join(dir, "results")); err == nil {
				t.Fatal("reducer accepted a manifest the execution decoder rejects")
			}
		})
	}
}

func TestValidateForestAuditResultRejectsIncoherentOutcomes(t *testing.T) {
	valid := ForestAuditResult{
		Schema: ForestAuditResultSchema, Mode: "production", GotreesitterRevision: strings.Repeat("a", 40),
		CorpusManifestSHA256: strings.Repeat("b", 64), CorpusLockSHA256: strings.Repeat("c", 64),
		Language: "go", Status: "pass", FilesTotal: 1, FilesAccepted: 1,
		PeerNanos: 2, RoutedNanos: 1, RoutedImproved: true,
		Files: []ForestAuditFileResult{{
			Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64), Disposition: "accepted",
			Forest: forestAuditTestAcceptedOutcome(1, true), Peer: forestAuditTestAcceptedOutcome(1, false),
			Routed: forestAuditTestAcceptedOutcome(1, false), RoutedProvenance: forestAuditRouteProductionFallback, RoutedNanos: 1,
		}},
	}
	valid.FilesRoutedFallback = 1
	clone := func() ForestAuditResult {
		result := valid
		result.Files = append([]ForestAuditFileResult(nil), valid.Files...)
		return result
	}
	tests := map[string]func(*ForestAuditResult){
		"forest_without_eof": func(result *ForestAuditResult) { result.Files[0].Forest.LastTokenEOF = false },
		"peer_not_accepted":  func(result *ForestAuditResult) { result.Files[0].Peer.Accepted = false },
		"source_mismatch":    func(result *ForestAuditResult) { result.Files[0].Forest.SourceLen = 0 },
		"full_and_truncated": func(result *ForestAuditResult) { result.Files[0].Peer.Truncated = true },
		"routed_bad_provenance": func(result *ForestAuditResult) {
			result.Files[0].RoutedProvenance = forestAuditRouteForestFastPath
		},
		"routed_aggregate_mismatch": func(result *ForestAuditResult) {
			result.FilesRoutedFallback = 0
		},
		"routed_not_run": func(result *ForestAuditResult) {
			result.Status = "fail"
			result.FilesRoutedFallback = 0
			result.RoutedNanos, result.Files[0].RoutedNanos, result.RoutedImproved = 0, 0, false
			result.Files[0].Routed = forestAuditTestNotRunOutcome(1)
			result.Files[0].RoutedProvenance = forestAuditRouteNotRun
		},
		"declined_accepted": func(result *ForestAuditResult) {
			result.Status = "fail"
			result.FilesAccepted, result.FilesDeclined = 0, 1
			result.Files[0].Disposition = "declined"
			result.Files[0].Decline = "test"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			result := clone()
			mutate(&result)
			if err := validateForestAuditResult(result); err == nil {
				t.Fatal("accepted incoherent forest audit result")
			}
		})
	}

	diverged := clone()
	diverged.Status = "fail"
	diverged.FilesDiverged = 1
	diverged.Files[0].Disposition = "diverged"
	diverged.Files[0].Diff = "peer failed"
	diverged.Files[0].Peer = ForestAuditOutcome{StopReason: "no_tree", SourceLen: 1, ExpectedEOF: 1}
	diverged.FilesRoutedDiverged = 1
	diverged.Files[0].RoutedDiff = "production tree missing"
	if err := validateForestAuditResult(diverged); err != nil {
		t.Fatalf("diverged result may encode coherent peer failure: %v", err)
	}

	routedForest := clone()
	routedForest.FilesRoutedFallback = 0
	routedForest.FilesRoutedForest = 1
	routedForest.Files[0].Routed = forestAuditTestAcceptedOutcome(1, true)
	routedForest.Files[0].RoutedProvenance = forestAuditRouteForestFastPath
	if err := validateForestAuditResult(routedForest); err != nil {
		t.Fatalf("forest-fast-path routed evidence should validate: %v", err)
	}

	routedDiverged := clone()
	routedDiverged.Status = "fail"
	routedDiverged.FilesRoutedFallback = 0
	routedDiverged.FilesRoutedForest = 1
	routedDiverged.FilesRoutedDiverged = 1
	routedDiverged.Files[0].Routed = forestAuditTestAcceptedOutcome(1, true)
	routedDiverged.Files[0].RoutedProvenance = forestAuditRouteForestFastPath
	routedDiverged.Files[0].RoutedDiff = "routed shape mismatch"
	if err := validateForestAuditResult(routedDiverged); err != nil {
		t.Fatalf("routed divergence should remain admissible evidence: %v", err)
	}
	blockers := forestPromotionBlockers(&routedDiverged, &ForestAuditResult{Status: "pass"})
	if len(blockers) != 1 || blockers[0] != "routed_divergence" {
		t.Fatalf("routed divergence blockers = %v", blockers)
	}

	routedSlower := clone()
	routedSlower.FilesRoutedFallback = 0
	routedSlower.FilesRoutedForest = 1
	routedSlower.Files[0].Routed = forestAuditTestAcceptedOutcome(1, true)
	routedSlower.Files[0].RoutedProvenance = forestAuditRouteForestFastPath
	routedSlower.RoutedNanos, routedSlower.Files[0].RoutedNanos, routedSlower.RoutedImproved = 2, 2, false
	if err := validateForestAuditResult(routedSlower); err != nil {
		t.Fatalf("slower routed result should pass correctness validation: %v", err)
	}
	blockers = forestPromotionBlockers(&routedSlower, &ForestAuditResult{Status: "pass"})
	if len(blockers) != 1 || blockers[0] != "routed_not_faster" {
		t.Fatalf("slower routed blockers = %v", blockers)
	}
}

func forestAuditTestAcceptedOutcome(sourceLen uint32, forest bool) ForestAuditOutcome {
	return ForestAuditOutcome{
		Present: true, Accepted: true, FullSpan: true, StopReason: "accepted",
		SourceLen: sourceLen, ExpectedEOF: sourceLen, RootEndByte: sourceLen,
		LastTokenEOF: true, ForestFastPath: forest,
	}
}

func forestAuditTestNotRunOutcome(sourceLen uint32) ForestAuditOutcome {
	return ForestAuditOutcome{StopReason: "not_run", SourceLen: sourceLen, ExpectedEOF: sourceLen}
}

func initForestManifestTestRepo(t *testing.T, root, language string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, language)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runForestManifestGit(t, dir, "init", "-q")
	runForestManifestGit(t, dir, "config", "user.email", "forest@example.invalid")
	runForestManifestGit(t, dir, "config", "user.name", "Forest Test")
	runForestManifestGit(t, dir, "remote", "add", "origin", "https://example.invalid/"+language)
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runForestManifestGit(t, dir, "add", ".")
	runForestManifestGit(t, dir, "commit", "-q", "-m", "fixture")
	out := runForestManifestGit(t, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func writeForestManifestTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runForestManifestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
