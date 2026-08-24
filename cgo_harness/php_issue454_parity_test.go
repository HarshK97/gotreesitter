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

const (
	issue454PHPProductionDeepDigest = "4456730ce6919a623dd6db2e6ae7f11933aeb454c7e337b7da5c08a8d9ba267c"
	issue454PHPCompactDeepDigest    = "4456730ce6919a623dd6db2e6ae7f11933aeb454c7e337b7da5c08a8d9ba267c"
	issue454PHPLockedCDeepDigest    = "1516308c38163089778464ad171875308c559af11af7c8c03ee17ae4eacd23c6"
	issue454PHPRootHasError         = true
)

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
	productionInspection, err := benchfixtures.InspectGoTree(goTree.RootNode(), goLang)
	if err != nil {
		t.Fatal(err)
	}
	cDeepDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		t.Fatal(err)
	}
	if productionInspection.SHA256 != issue454PHPProductionDeepDigest {
		t.Fatalf("PHP issue #454 production gts-deep-tree-v1 digest=%s, want %s", productionInspection.SHA256, issue454PHPProductionDeepDigest)
	}
	if cDeepDigest != issue454PHPLockedCDeepDigest {
		t.Fatalf("PHP issue #454 locked-C gts-deep-tree-v1 digest=%s, want %s", cDeepDigest, issue454PHPLockedCDeepDigest)
	}
	if got := goTree.RootNode().HasError(); got != issue454PHPRootHasError {
		t.Fatalf("PHP issue #454 production root HasError=%t, want %t", got, issue454PHPRootHasError)
	}
	if got := cTree.RootNode().HasError(); got != issue454PHPRootHasError {
		t.Fatalf("PHP issue #454 locked-C root HasError=%t, want %t", got, issue454PHPRootHasError)
	}
	if productionInspection.SHA256 == cDeepDigest {
		t.Fatal("PHP issue #454 production deep digest unexpectedly matches locked C; review the NO-GO receipt before changing route policy")
	}

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
	compactInspection, err := benchfixtures.InspectGoTree(compactTree.RootNode(), goLang)
	if err != nil {
		t.Fatal(err)
	}
	if compactInspection.SHA256 != issue454PHPCompactDeepDigest {
		t.Fatalf("PHP issue #454 compact gts-deep-tree-v1 digest=%s, want %s", compactInspection.SHA256, issue454PHPCompactDeepDigest)
	}
	if compactInspection.SHA256 != productionInspection.SHA256 {
		t.Fatalf("PHP issue #454 compact and production deep digests differ: compact=%s production=%s", compactInspection.SHA256, productionInspection.SHA256)
	}
	if got := compactTree.RootNode().HasError(); got != issue454PHPRootHasError {
		t.Fatalf("PHP issue #454 compact root HasError=%t, want %t", got, issue454PHPRootHasError)
	}
	if compactInspection.SHA256 == cDeepDigest {
		t.Fatal("PHP issue #454 compact deep digest unexpectedly matches locked C; review the NO-GO receipt before changing route policy")
	}
	t.Logf("deep_digest format=%s production=%s compact=%s locked_c=%s production_root_has_error=%t compact_root_has_error=%t locked_c_root_has_error=%t exact_locked_c=false fields=type+named,field,byte+point,extra+missing+error+has_error,child_order", benchfixtures.DeepTreeDigestVersion, productionInspection.SHA256, compactInspection.SHA256, cDeepDigest, goTree.RootNode().HasError(), compactTree.RootNode().HasError(), cTree.RootNode().HasError())
	t.Logf("PHP issue #454 compact route source_sha256=%x bytes=%d routed=%d fallback=%d reason=%q", sha256.Sum256(edited), len(edited), routedAfter-routedBefore, fallbackAfter-fallbackBefore, gotreesitter.AdmissionCandidateLastFallbackReason())
	document, err := os.ReadFile("../docs/issue-454-compact-correctness-blocker.md")
	if err != nil {
		t.Fatalf("read issue #454 compact receipt: %v", err)
	}
	documentText := strings.Join(strings.Fields(string(document)), " ")
	for _, marker := range []string{
		"## 2026-08-24 PHP compact fallback guard",
		"Publication base: `c25686c882affd7408e5ef4a7d65e92cc8391fab`.",
		"The locked-C artifact SHA-256 is `1daea60ac1ee31227b8e1ed3cbd76b841435fe693e95af65cc61dad447d27891`.",
		issue454PHPProductionDeepDigest,
		issue454PHPCompactDeepDigest,
		issue454PHPLockedCDeepDigest,
		"The `gts-deep-tree-v1` stream covers type and named identity, incoming fields, byte and point spans, and child order. It also covers extra and missing flags, error flags, and the `HasError` flag.",
		"The production and compact deep digests differ from the locked-C digest.",
		"All three roots report `HasError=true`.",
		"The compact route recorded `routed=0` and `fallback=1`.",
		"This guard does not graduate PHP compact admission.",
	} {
		marker = strings.Join(strings.Fields(marker), " ")
		if !strings.Contains(documentText, marker) {
			t.Fatalf("issue #454 compact receipt is missing marker %q", marker)
		}
	}
}
