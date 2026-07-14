//go:build cgo && (treesitter_c_parity || treesitter_c_bench)

package cgoharness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyPinnedRepoAcceptsCleanExactHead(t *testing.T) {
	repoDir, head := newPinnedTestRepo(t)
	if err := verifyPinnedRepo(repoDir, head); err != nil {
		t.Fatalf("verify clean pinned repository: %v", err)
	}
}

func TestVerifyPinnedRepoRejectsWrongHead(t *testing.T) {
	repoDir, pinnedHead := newPinnedTestRepo(t)
	writePinnedTestFile(t, filepath.Join(repoDir, "tracked.txt"), "second\n")
	runPinnedTestGit(t, repoDir, "add", "tracked.txt")
	runPinnedTestGit(t, repoDir, "commit", "-m", "second")

	err := verifyPinnedRepo(repoDir, pinnedHead)
	if err == nil || !strings.Contains(err.Error(), "HEAD mismatch") {
		t.Fatalf("verify wrong HEAD error=%v, want actionable HEAD mismatch", err)
	}
}

func TestVerifyPinnedRepoRejectsDirtyTrackedFile(t *testing.T) {
	repoDir, head := newPinnedTestRepo(t)
	writePinnedTestFile(t, filepath.Join(repoDir, "tracked.txt"), "dirty\n")

	err := verifyPinnedRepo(repoDir, head)
	if err == nil || !strings.Contains(err.Error(), "working tree is not clean") || !strings.Contains(err.Error(), "tracked.txt") {
		t.Fatalf("verify dirty tracked file error=%v, want path and clean-tree guidance", err)
	}
}

func TestVerifyPinnedRepoRejectsUntrackedFile(t *testing.T) {
	repoDir, head := newPinnedTestRepo(t)
	writePinnedTestFile(t, filepath.Join(repoDir, "untracked.txt"), "untracked\n")

	err := verifyPinnedRepo(repoDir, head)
	if err == nil || !strings.Contains(err.Error(), "working tree is not clean") || !strings.Contains(err.Error(), "?? untracked.txt") {
		t.Fatalf("verify untracked file error=%v, want untracked path and clean-tree guidance", err)
	}
}

func TestClonePinnedRepoFromLocalCacheRejectsDirtySource(t *testing.T) {
	cacheDir, head := newPinnedTestRepo(t)
	writePinnedTestFile(t, filepath.Join(cacheDir, "untracked.txt"), "untracked\n")
	dest := filepath.Join(t.TempDir(), "copy")

	err := clonePinnedRepoFromLocalCache(cacheDir, head, dest)
	if err == nil || !strings.Contains(err.Error(), "cached parity repository") || !strings.Contains(err.Error(), "?? untracked.txt") {
		t.Fatalf("clone dirty cached repository error=%v, want cache rejection and untracked path", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dirty cache destination stat error=%v, want destination not created", statErr)
	}
}

func TestBuildParityCRefRejectsDirtyLocalRepo(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "fixture")
	head := initPinnedTestRepo(t, repoDir)
	writePinnedTestFile(t, filepath.Join(repoDir, "untracked.txt"), "untracked\n")
	t.Setenv(parityRepoRootEnv, root)

	_, err := buildParityCRef(t.TempDir(), "", parityLockEntry{
		Name:    "fixture",
		RepoURL: "https://example.invalid/tree-sitter-fixture",
		Commit:  head,
		Subdir:  "src",
	})
	if err == nil || !strings.Contains(err.Error(), "local parity repository") || !strings.Contains(err.Error(), "?? untracked.txt") {
		t.Fatalf("build dirty local repository error=%v, want local-source rejection and untracked path", err)
	}
}

func newPinnedTestRepo(t *testing.T) (string, string) {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), "repo")
	return repoDir, initPinnedTestRepo(t, repoDir)
}

func initPinnedTestRepo(t *testing.T, repoDir string) string {
	t.Helper()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runPinnedTestGit(t, repoDir, "init")
	runPinnedTestGit(t, repoDir, "config", "user.name", "Gotreesitter Test")
	runPinnedTestGit(t, repoDir, "config", "user.email", "gotreesitter@example.invalid")
	writePinnedTestFile(t, filepath.Join(repoDir, "tracked.txt"), "first\n")
	runPinnedTestGit(t, repoDir, "add", "tracked.txt")
	runPinnedTestGit(t, repoDir, "commit", "-m", "first")
	return strings.TrimSpace(runPinnedTestGit(t, repoDir, "rev-parse", "HEAD"))
}

func writePinnedTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPinnedTestGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	gitArgs := append([]string{"-C", repoDir}, args...)
	cmd := exec.Command("git", gitArgs...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(gitArgs, " "), err, out)
	}
	return string(out)
}
