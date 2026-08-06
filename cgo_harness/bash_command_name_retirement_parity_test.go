//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestBashCommandNameRetirementCOracleParity(t *testing.T) {
	entry, ok := parityEntriesByName["bash"]
	if !ok {
		t.Fatal("missing Bash grammar entry")
	}
	language := entry.Language()
	cLanguage, err := ParityCLanguage("bash")
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("${0%/*}/check-unsafe-assertions.sh")

	goParser := gotreesitter.NewParser(language)
	goParser.SetAdmissionCandidateRoute(false)
	goTree, err := goParser.ParseNoResultCompatibilityBenchmarkOnly(source)
	if err != nil {
		t.Fatal(err)
	}
	defer goTree.Release()

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

	var divergences []string
	compareNodes(
		goTree.RootNode(),
		language,
		cTree.RootNode(),
		"root",
		&divergences,
	)
	if len(divergences) != 0 {
		t.Fatalf("C-oracle divergences:\n%s", strings.Join(divergences, "\n"))
	}
}
