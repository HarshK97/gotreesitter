package cgoharness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	}}
	result.FilesTotal, result.FilesAccepted = 1, 1
	if err := WriteForestAuditResult(filepath.Join(dir, "results", "production", "go.json"), result); err != nil {
		t.Fatal(err)
	}
	bad := result
	bad.Files = append([]ForestAuditFileResult(nil), result.Files...)
	bad.Files[0].Bytes++
	bad.Files[0].Forest = forestAuditTestAcceptedOutcome(2, true)
	bad.Files[0].Peer = forestAuditTestAcceptedOutcome(2, false)
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
	if report.LanguagesComplete != 1 || report.Languages[0].Language != "go" || report.Languages[0].Status != "pass" {
		t.Fatalf("resumed report = %#v", report)
	}
	data, err := json.Marshal(report)
	if err != nil || len(data) == 0 {
		t.Fatalf("marshal report: bytes=%d err=%v", len(data), err)
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
		Files: []ForestAuditFileResult{{
			Path: "a.go", Bytes: 1, SHA256: strings.Repeat("d", 64), Disposition: "accepted",
			Forest: forestAuditTestAcceptedOutcome(1, true), Peer: forestAuditTestAcceptedOutcome(1, false),
		}},
	}
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
	if err := validateForestAuditResult(diverged); err != nil {
		t.Fatalf("diverged result may encode coherent peer failure: %v", err)
	}
}

func forestAuditTestAcceptedOutcome(sourceLen uint32, forest bool) ForestAuditOutcome {
	return ForestAuditOutcome{
		Present: true, Accepted: true, FullSpan: true, StopReason: "accepted",
		SourceLen: sourceLen, ExpectedEOF: sourceLen, RootEndByte: sourceLen,
		LastTokenEOF: forest, ForestFastPath: forest,
	}
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

func runForestManifestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
