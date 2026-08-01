//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// swiftForInRangeLoopCountSource mirrors
// grammars.swiftForInRangeLoopCountSource: a single function containing count
// copies of the #123 for…in trailing-closure-ambiguity trigger
// (`for i in 0..<10 { }`). See that function's doc comment for why the loop
// count matters: it is the removed terminal-diagnostic-count pre-gate's own
// trigger variable.
func swiftForInRangeLoopCountSource(count int) string {
	var b strings.Builder
	b.WriteString("func manyLoops() {\n")
	for i := 0; i < count; i++ {
		b.WriteString("    for i in 0..<10 { }\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// TestParitySwiftForInRangeLoopCountAgreesWithCOracle is the C-parity
// counterpart to grammars.TestSwiftForInRangeLoopCountParityWitness: for 9,
// 10, and 20 copies of the same for…in-range loop, gotreesitter's Go output
// must agree, node for node, with the locked C tree-sitter-swift oracle. The
// removed terminal-diagnostic-count pre-gates (mechanism 2) made Go return an
// ERROR tree at 10 and 20 loops while the C oracle stayed clean — this test
// is the direct proof that the fixed accept gate (swiftRecoveryProbeMatches
// LegacyRoute, parser_result_swift.go) no longer diverges from C at any of
// these counts.
func TestParitySwiftForInRangeLoopCountAgreesWithCOracle(t *testing.T) {
	for _, count := range []int{9, 10, 20} {
		source := swiftForInRangeLoopCountSource(count)
		t.Run(fmt.Sprintf("loops=%d", count), func(t *testing.T) {
			runParityCase(t, parityCase{name: "swift", source: source}, fmt.Sprintf("loops=%d", count), []byte(source))
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
	// Pin both sides, not just the C oracle's: this witness's whole point is a
	// known, tracked structural mismatch (#576, the `unsafe` expression-prefix
	// keyword), and an unpinned Go digest could silently drift to a different
	// (still-mismatching) shape without this test noticing — exactly the kind
	// of silent change the initial-only recovery probe (parser_result_swift.go)
	// must never cause. This Go digest matches
	// grammars.TestSwiftUnsafeWitnessKeepsCurrentGoTreeAcrossRecoveryProbe's
	// pinned digest for the same file.
	const wantGoDigest = "ec51c633a3f99515cc0cd1c0cff435a44ddc7db8e83705977d28f78bdfb0fc0e"
	if goInspection.SHA256 != wantGoDigest {
		t.Fatalf("Go Swift witness digest = %s, want %s", goInspection.SHA256, wantGoDigest)
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
