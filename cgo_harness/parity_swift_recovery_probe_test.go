//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestParitySwiftCleanRecoveryProbeControls(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "for-range",
			source: "func f(n: Int) -> Int {\n" +
				"  var total = 0\n" +
				"  for i in 0..<n { total += i }\n" +
				"  return total\n" +
				"}\n",
		},
		{
			name: "for-closed-range",
			source: "func f(n: Int) -> Int {\n" +
				"  var total = 0\n" +
				"  for i in 0...n { total += i }\n" +
				"  return total\n" +
				"}\n",
		},
		{
			name:   "native-ternary",
			source: "func f(x: Int) -> Int { return x > 0 ? 1 : 2 }\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runParityCase(t, parityCase{name: "swift", source: test.source}, test.name, []byte(test.source))
		})
	}
}

func TestSwiftUnsafeWitnessRemainsKnownCStructuralMismatch(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "grammars", "testdata", "swift_corpus", "stdlib_FloatingPointToString.swift"))
	if err != nil {
		t.Fatalf("read Swift unsafe witness: %v", err)
	}
	goTree, goLang, err := parseWithGo(parityCase{name: "swift"}, source, nil)
	if err != nil {
		t.Fatalf("parse Swift witness with Go: %v", err)
	}
	defer releaseGoTree(goTree)

	cLang, err := ParityCLanguage("swift")
	if err != nil {
		t.Fatalf("load locked Swift C parser: %v", err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("set locked Swift C parser language: %v", err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("locked Swift C parser returned no tree")
	}
	defer cTree.Close()

	goRoot := goTree.RootNode()
	cRoot := cTree.RootNode()
	if !goRoot.HasError() || !cRoot.HasError() {
		t.Fatalf("Swift unsafe witness HasError() Go=%v C=%v, want true/true", goRoot.HasError(), cRoot.HasError())
	}
	goInspection, err := benchfixtures.InspectGoTree(goRoot, goLang)
	if err != nil {
		t.Fatalf("inspect Go Swift witness: %v", err)
	}
	cDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		t.Fatalf("inspect C Swift witness: %v", err)
	}
	const wantCDigest = "ab96dddf088487acc700d72af9342c338901504dcf1d32b9644e9f6f6638190d"
	if cDigest != wantCDigest {
		t.Fatalf("locked C Swift witness digest = %s, want %s", cDigest, wantCDigest)
	}
	if goInspection.SHA256 == cDigest {
		t.Fatal("Swift unsafe witness reached C structural parity; ratchet the known-mismatch test")
	}
	if diff := FirstDivergenceDumpV1(goRoot, goLang, cRoot); diff == nil {
		t.Fatal("Swift unsafe witness digest differs without a structural divergence")
	}
	t.Logf("Swift unsafe witness known C mismatch: Go=%s C=%s", goInspection.SHA256, cDigest)
}
