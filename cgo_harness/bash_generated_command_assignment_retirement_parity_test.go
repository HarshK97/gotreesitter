//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestBashGeneratedCommandAssignmentRetirementCOracleParity pins the
// generated-command assignment witness against the locked Bash C oracle.
// Both the raw and production routes must match the oracle exactly.
func TestBashGeneratedCommandAssignmentRetirementCOracleParity(t *testing.T) {
	entry, ok := parityEntriesByName["bash"]
	if !ok {
		t.Fatal("missing Bash grammar entry")
	}
	language := entry.Language()
	cLanguage, err := ParityCLanguage("bash")
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("zipname=npm-$(node ../cli.js -v).zip")

	rawParser := gotreesitter.NewParser(language)
	rawParser.SetAdmissionCandidateRoute(false)
	rawTree, err := rawParser.ParseNoResultCompatibilityBenchmarkOnly(source)
	if err != nil {
		t.Fatalf("raw parse: %v", err)
	}
	defer rawTree.Release()

	productionParser := gotreesitter.NewParser(language)
	productionParser.SetAdmissionCandidateRoute(false)
	productionTree, err := productionParser.Parse(source)
	if err != nil {
		t.Fatalf("production parse: %v", err)
	}
	defer productionTree.Release()

	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLanguage); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C oracle returned a nil tree")
	}
	defer cTree.Close()

	cDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		t.Fatalf("inspect C deep tree: %v", err)
	}
	rawInspection, err := benchfixtures.InspectGoTree(rawTree.RootNode(), language)
	if err != nil {
		t.Fatalf("inspect raw Go deep tree: %v", err)
	}
	productionInspection, err := benchfixtures.InspectGoTree(productionTree.RootNode(), language)
	if err != nil {
		t.Fatalf("inspect production Go deep tree: %v", err)
	}

	rawDiff := FirstDivergenceDumpV1(rawTree.RootNode(), language, cTree.RootNode())
	if rawDiff != nil {
		t.Fatalf(
			"raw tree diverges from the C oracle: first_difference=%+v raw_digest=%s c_digest=%s",
			rawDiff,
			rawInspection.SHA256,
			cDigest,
		)
	}
	if flagDiff := firstLockedCTreeFlagDivergence(rawTree.RootNode(), language, cTree.RootNode(), "/root"); flagDiff != nil {
		t.Fatalf("raw tree has a missing or error flag divergence: %v", flagDiff)
	}
	if rawInspection.SHA256 != cDigest {
		t.Fatalf("raw deep digest Go=%s C=%s", rawInspection.SHA256, cDigest)
	}
	t.Logf("raw route matches locked C exactly: deep_digest=%s", cDigest)

	assertLockedCTreeExact(t, "Bash generated-command assignment production route", productionTree, language, cTree)
	if productionInspection.SHA256 != cDigest {
		t.Fatalf("production deep digest Go=%s C=%s", productionInspection.SHA256, cDigest)
	}
}
