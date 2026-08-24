//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

func TestSQLC26akGeneratedCreateDomainAs(t *testing.T) {
	var grammar grammargenCGOGrammar
	for _, candidate := range grammargenCGOGrammars {
		if candidate.name == "sql" {
			grammar = candidate
			break
		}
	}
	if grammar.name == "" {
		t.Fatal("missing sql grammargen CGO config")
	}

	gram, err := importGrammargenSource(grammar)
	if err != nil {
		t.Skipf("import unavailable: %v", err)
	}
	genLang, err := grammargenGenerate(gram, 90*time.Second)
	if err != nil {
		t.Fatalf("generate grammar: %v", err)
	}
	refLang := grammar.blobFunc()
	adaptGrammargenCGOExternalScanner("sql", refLang, genLang)
	grammarHash, ok := genLang.GrammarBlobSHA256()
	if !ok {
		t.Fatal("generated SQL language has no authenticated grammar identity")
	}
	provider, ok := genLang.ExternalScanner.(gotreesitter.ExternalScannerCheckpointIdentityProvider)
	if !ok {
		t.Fatal("generated SQL scanner has no checkpoint identity provider")
	}
	wantScanner := decodeSQLIdentity(t, "7e493677411a501e6d8592c6b9cc158e21a1bfed44c72ca914e2d81e4e34861d")
	wantGrammar := decodeSQLIdentity(t, "4ffb2a6d09e2000126f10101db9028d28e0752ac3e4f83e401f045c3b028ca7c")
	identity, ok := provider.CheckpointIdentity()
	if !ok || !bytes.Equal(identity.Scanner, wantScanner) || !bytes.Equal(identity.Grammar, wantGrammar) || !bytes.Equal(identity.Grammar, grammarHash[:]) {
		t.Fatalf("generated SQL identity changed: ok=%t scanner=%x grammar=%x blob=%x", ok, identity.Scanner, identity.Grammar, grammarHash)
	}

	cLang, err := ParityCLanguage("sql")
	if err != nil {
		t.Skipf("C parser unavailable: %v", err)
	}
	cParser := sitter.NewParser()
	t.Cleanup(cParser.Close)
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("C SetLanguage: %v", err)
	}

	source := []byte("CREATE DOMAIN test AS text;")
	sourceHash := sha256.Sum256(source)
	if got, want := hex.EncodeToString(sourceHash[:]), "0cad93f1b70ffa192008df866587c05b206ee404cedd5a0025f542c39c6a504b"; got != want {
		t.Fatalf("source SHA-256=%s, want %s", got, want)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C parser returned no tree")
	}
	t.Cleanup(cTree.Close)
	if cTree.RootNode().HasError() {
		t.Fatalf("locked C witness has an error: %s", dumpCTree(cTree.RootNode(), 0))
	}
	cDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		t.Fatalf("locked C digest: %v", err)
	}

	genTree, err := gotreesitter.NewParser(genLang).Parse(source)
	if err != nil {
		t.Fatalf("generated SQL parse: %v", err)
	}
	t.Cleanup(genTree.Release)
	genInspection, err := benchfixtures.InspectGoTree(genTree.RootNode(), genLang)
	if err != nil {
		t.Fatalf("generated SQL digest: %v", err)
	}
	var genVsCErrs []string
	compareNodes(genTree.RootNode(), genLang, cTree.RootNode(), "root", &genVsCErrs)
	if len(genVsCErrs) == 0 {
		t.Fatal("generated SQL CREATE DOMAIN AS witness unexpectedly matches locked C")
	}
	if genVsCErrs[0] != `root[0][1]: ChildCount go=1 c=0 (goType="CREATE_DOMAIN" cType="CREATE_DOMAIN" goBytes=[7-13] cBytes=[7-13])` {
		t.Fatalf("generated SQL CREATE DOMAIN AS first divergence=%q", genVsCErrs[0])
	}
	if genInspection.SHA256 != "67366d68529459613040eb60c5eef47b371fd3091dbe3283c82d7bcff287ea9c" || genTree.RootNode().ChildCount() != 2 || genTree.RootNode().HasError() {
		t.Fatalf("generated SQL CREATE DOMAIN AS receipt changed: digest=%s children=%d error=%t", genInspection.SHA256, genTree.RootNode().ChildCount(), genTree.RootNode().HasError())
	}

	blobTree, err := gotreesitter.NewParser(refLang).Parse(source)
	if err != nil {
		t.Fatalf("checked-in SQL parse: %v", err)
	}
	t.Cleanup(blobTree.Release)
	blobInspection, err := benchfixtures.InspectGoTree(blobTree.RootNode(), refLang)
	if err != nil {
		t.Fatalf("checked-in SQL digest: %v", err)
	}
	var blobVsCErrs []string
	compareNodes(blobTree.RootNode(), refLang, cTree.RootNode(), "root", &blobVsCErrs)
	if len(blobVsCErrs) != 0 || blobTree.RootNode().HasError() || blobInspection.SHA256 != cDigest || blobInspection.SHA256 != "d0daa4279f00eb8c7278992cff6a870c98bf3deee2292b28f997adbe2f434916" {
		t.Fatalf("checked-in SQL differs from locked C: errors=%v has_error=%t blob_digest=%s c_digest=%s", blobVsCErrs, blobTree.RootNode().HasError(), blobInspection.SHA256, cDigest)
	}

	t.Logf("scanner_identity=%x generated_blob_identity=%x source_sha256=%s bytes=%d generated_digest=%s checked_in_digest=%s locked_c_digest=%s generated_children=%d checked_in_children=%d generated_error=%t checked_in_error=%t", identity.Scanner, identity.Grammar, hex.EncodeToString(sourceHash[:]), len(source), genInspection.SHA256, blobInspection.SHA256, cDigest, genTree.RootNode().ChildCount(), blobTree.RootNode().ChildCount(), genTree.RootNode().HasError(), blobTree.RootNode().HasError())
	t.Logf("generated_vs_locked_c=%s", genVsCErrs[0])
}
