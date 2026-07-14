//go:build cgo && (treesitter_c_parity || treesitter_c_bench)

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

	sitter "github.com/tree-sitter/go-tree-sitter"
)

const lockedGoGrammarCommit = "2346a3ab1bb3857b48b29d779a1ef9799a248cd7"

var canonicalGoOracleFixtures = []struct {
	path   string
	sha256 string
	deep   string
}{
	{path: "../query_compile.go", sha256: "b788ee19b0075f0b9b567a9f93ea657e715bc8a6a40a99d3ca5c761404e71894", deep: "0c4f2288bcd473cbb3332aed4e5514320e71ce2b36f70e705192f1c94da7d316"},
	{path: "../rewrite.go", sha256: "74c0705f8729670559492fb5460a01b2a1a2a109928e1aeb52736e485e8ff097", deep: "2ea60dfb7e3267acd2d3b674b44fcca8d049d3ecb5510119b6d62b5c1b8ad61a"},
	{path: "../language.go", sha256: "009aa9fd5352c712f3839670c7df8a9b00ae878ee20dc88131a438b2d5edfd9a", deep: "08c379c72bf24b5bf8a38f45b2076b43add92013df35c8fb507cfce9ce9ce6cd"},
	{path: "../grammargen/lr.go", sha256: "a7e4a1a64b25a60aea36183b9d6d53dcd9240942cdb10e67a3cf9e6ce30f95b2", deep: "f7f59f98c4c052e545e6b936a303c73fa42c06386fea202923e61c264c3a7eee"},
}

func TestCOracleContractPreflight(t *testing.T) {
	identity, err := COracleIdentity("go")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Contract != COracleContractVersion {
		t.Fatalf("contract=%q want %q", identity.Contract, COracleContractVersion)
	}
	if identity.BindingModule != COracleBindingModule || identity.BindingVersion != COracleBindingVersion {
		t.Fatalf("binding=%s@%s want %s@%s", identity.BindingModule, identity.BindingVersion, COracleBindingModule, COracleBindingVersion)
	}
	if identity.RuntimeCommit != COracleRuntimeCommit {
		t.Fatalf("runtime commit=%s want %s", identity.RuntimeCommit, COracleRuntimeCommit)
	}
	if identity.GrammarCommit != lockedGoGrammarCommit {
		t.Fatalf("Go grammar commit=%s want %s", identity.GrammarCommit, lockedGoGrammarCommit)
	}
	if !strings.Contains(identity.GrammarCompileFlags, "-O2") {
		t.Fatalf("grammar flags=%q do not include -O2", identity.GrammarCompileFlags)
	}
	if identity.RuntimeLinkage != "static_cgo_test_binary" || identity.GrammarLinkage != "shared_dlopen" {
		t.Fatalf("unexpected cgo transport linkage: runtime=%s grammar=%s", identity.RuntimeLinkage, identity.GrammarLinkage)
	}
	if len(identity.GrammarArtifactSHA256) != 64 {
		t.Fatalf("grammar artifact sha256=%q", identity.GrammarArtifactSHA256)
	}

	versionOut, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", COracleBindingModule).Output()
	if err != nil {
		t.Fatalf("resolve C oracle binding module: %v", err)
	}
	if version := strings.TrimSpace(string(versionOut)); version != COracleBindingVersion {
		t.Fatalf("resolved binding version=%s want %s", version, COracleBindingVersion)
	}

	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("c_oracle_identity=%s", encoded)

	parser := sitter.NewParser()
	defer parser.Close()
	language, err := COracleLanguage("go")
	if err != nil {
		t.Fatal(err)
	}
	if err := parser.SetLanguage(language); err != nil {
		t.Fatal(err)
	}
	source := makeGoBenchmarkSource(500)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("C oracle preflight parse returned nil")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() || root.EndByte() != uint(len(source)) {
		t.Fatalf("C oracle preflight parse incomplete: root=%v", root)
	}
	digest, err := COracleDeepDigest(tree)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("c_oracle_deep_digest_format=gts-deep-tree-v1 c_oracle_deep_sha256=%s", digest)
}

func TestCOracleStaticDeepParity(t *testing.T) {
	language, err := COracleLanguage("go")
	if err != nil {
		t.Fatal(err)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		t.Fatal(err)
	}

	for _, fixture := range canonicalGoOracleFixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			source, err := os.ReadFile(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			sourceSHA := fmt.Sprintf("%x", sha256.Sum256(source))
			if sourceSHA != fixture.sha256 {
				t.Fatalf("source sha256=%s want %s", sourceSHA, fixture.sha256)
			}

			tree := parser.Parse(source, nil)
			if tree == nil {
				t.Fatal("cgo C oracle returned nil tree")
			}
			root := tree.RootNode()
			if root == nil || root.HasError() || root.EndByte() != uint(len(source)) {
				tree.Close()
				t.Fatalf("cgo C oracle incomplete: root=%v", root)
			}
			digest, err := COracleDeepDigest(tree)
			tree.Close()
			if err != nil {
				t.Fatal(err)
			}
			if digest != fixture.deep {
				t.Fatalf("cgo C oracle deep sha256=%s want %s", digest, fixture.deep)
			}

			absPath, err := filepath.Abs(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "pure_c/run_go_benchmark.sh", "500", "1", "1", absPath)
			cmd.Env = append(os.Environ(), "GTS_C_ORACLE_EXPECTED_DEEP_SHA256="+digest)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("static C oracle: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "oracle_deep_sha256="+digest+"\n") {
				t.Fatalf("static C oracle did not report expected digest %s\n%s", digest, out)
			}
			t.Logf("source_sha256=%s deep_sha256=%s", sourceSHA, digest)
		})
	}
}
