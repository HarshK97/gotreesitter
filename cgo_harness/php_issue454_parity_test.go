//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

const issue454PHPSourceSHA256 = "cbf52f81ea212353a3bf04d7c9b37668b5cdfb6cd428c2d0cb3799a8e13ae82f"
const issue454PHPGrammarCommit = "3f2465c217d0a966d41e584b42d75522f2a3149e"
const issue454PHPGrammarBlobSHA256 = "15724627db479c27304b43fa3b5ef7d8d81f85e3b9ce6d8575a847b2dbaa5cd5"
const issue454PHPCArtifactSHA256 = "1daea60ac1ee31227b8e1ed3cbd76b841435fe693e95af65cc61dad447d27891"

func TestParityIssue454PHPWholeTreeFallback(t *testing.T) {
	source := benchfixtures.Issue454PHPSource()
	site := bytes.Index(source, []byte("$x0"))
	if site < 0 {
		t.Fatal("PHP edit marker is absent")
	}
	site++
	edited := make([]byte, 0, len(source)-1)
	edited = append(edited, source[:site]...)
	edited = append(edited, source[site+1:]...)
	if got := fmt.Sprintf("%x", sha256.Sum256(edited)); got != issue454PHPSourceSHA256 {
		t.Fatalf("PHP issue #454 source SHA-256=%s, want %s", got, issue454PHPSourceSHA256)
	}

	goLang := grammars.PhpLanguage()
	goParser := gotreesitter.NewParser(goLang)
	goParser.SetAdmissionCandidateRoute(false)
	goTree, err := goParser.Parse(edited)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGoTree(goTree)
	if goTree.ParseStoppedEarly() {
		t.Fatalf("Go parse stopped early: %s", goTree.ParseRuntime().Summary())
	}

	cLang, err := ParityCLanguage("php")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := COracleIdentity("php")
	if err != nil {
		t.Fatalf("load PHP C-oracle identity: %v", err)
	}
	if identity.GrammarCommit != issue454PHPGrammarCommit {
		t.Fatalf("PHP C-oracle grammar commit=%s, want %s", identity.GrammarCommit, issue454PHPGrammarCommit)
	}
	if identity.GrammarArtifactSHA256 != issue454PHPCArtifactSHA256 {
		t.Fatalf("PHP C-oracle artifact SHA-256=%s, want %s", identity.GrammarArtifactSHA256, issue454PHPCArtifactSHA256)
	}
	blob, err := os.ReadFile("../grammars/grammar_blobs/php.bin")
	if err != nil {
		t.Fatalf("read PHP grammar blob: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(blob)); got != issue454PHPGrammarBlobSHA256 {
		t.Fatalf("PHP grammar blob SHA-256=%s, want %s", got, issue454PHPGrammarBlobSHA256)
	}
	t.Logf("grammar=php grammar_commit=%s grammar_blob_sha256=%s c_runtime=%s@%s c_binding=%s@%s c_artifact_sha256=%s", identity.GrammarCommit, issue454PHPGrammarBlobSHA256, identity.RuntimeVersion, identity.RuntimeCommit, identity.BindingVersion, identity.BindingCommit, identity.GrammarArtifactSHA256)
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(edited, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C reference parser returned nil tree")
	}
	defer cTree.Close()

	var errs []string
	compareNodes(goTree.RootNode(), goLang, cTree.RootNode(), "root", &errs)
	if len(errs) > 0 {
		t.Fatalf("PHP issue #454 production tree differs from the pinned C oracle: %s", errs[0])
	}

	routedBefore, fallbackBefore := gotreesitter.AdmissionCandidateCounters()
	compactParser := gotreesitter.NewParser(goLang)
	compactParser.SetAdmissionCandidateRoute(true)
	compactTree, err := compactParser.Parse(edited)
	if err != nil {
		t.Fatalf("PHP issue #454 compact parse failed: %v", err)
	}
	defer compactTree.Release()
	routedAfter, fallbackAfter := gotreesitter.AdmissionCandidateCounters()
	if routedAfter != routedBefore || fallbackAfter != fallbackBefore+1 {
		t.Fatalf("PHP issue #454 compact counters=%d/%d->%d/%d, want fallback-only", routedBefore, fallbackBefore, routedAfter, fallbackAfter)
	}
	if reason := gotreesitter.AdmissionCandidateLastFallbackReason(); !strings.Contains(reason, "recovery-entered") {
		t.Fatalf("PHP issue #454 compact fallback reason=%q, want recovery-entered", reason)
	}
	if got, want := compactTree.RootNode().SExpr(goLang), goTree.RootNode().SExpr(goLang); got != want {
		t.Fatalf("PHP issue #454 compact fallback changed the production tree:\ncompact: %s\nproduction: %s", got, want)
	}
	var compactErrs []string
	compareNodes(compactTree.RootNode(), goLang, cTree.RootNode(), "root", &compactErrs)
	if len(compactErrs) > 0 {
		t.Fatalf("PHP issue #454 compact fallback differs from the pinned C oracle: %s", compactErrs[0])
	}
	t.Logf("PHP issue #454 compact route source_sha256=%x bytes=%d routed=%d fallback=%d reason=%q", sha256.Sum256(edited), len(edited), routedAfter-routedBefore, fallbackAfter-fallbackBefore, gotreesitter.AdmissionCandidateLastFallbackReason())
	document, err := os.ReadFile("../docs/issue-454-compact-correctness-blocker.md")
	if err != nil {
		t.Fatalf("read issue #454 compact receipt: %v", err)
	}
	documentText := strings.Join(strings.Fields(string(document)), " ")
	for _, marker := range []string{
		"## 2026-08-24 PHP compact fallback guard",
		"Publication base: `af056b2d90e50a8917b9389bf42dfdf75872035e`.",
		"The locked-C artifact SHA-256 is `1daea60ac1ee31227b8e1ed3cbd76b841435fe693e95af65cc61dad447d27891`.",
		"The compact route recorded `routed=0` and `fallback=1`.",
		"This guard does not graduate PHP compact admission.",
	} {
		marker = strings.Join(strings.Fields(marker), " ")
		if !strings.Contains(documentText, marker) {
			t.Fatalf("issue #454 compact receipt is missing marker %q", marker)
		}
	}
}
