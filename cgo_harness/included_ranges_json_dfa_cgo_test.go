//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestIncludedRangesJSONInternalDFAParity checks one token across a source gap.
func TestIncludedRangesJSONInternalDFAParity(t *testing.T) {
	source := []byte("xx{\"abZZcd\":1}yy")
	spans := [][2]int{{2, 6}, {8, 14}}

	goLanguage := grammars.JsonLanguage()
	if goLanguage == nil {
		t.Fatal("JSON language is unavailable")
	}
	if goLanguage.ExternalScanner != nil {
		t.Fatal("JSON unexpectedly uses an external scanner")
	}
	cLanguage, err := COracleLanguage("json")
	if err != nil {
		t.Fatal(err)
	}

	cRanges := make([]sitter.Range, 0, len(spans))
	goRanges := make([]gotreesitter.Range, 0, len(spans))
	for _, span := range spans {
		sr, sc := includedRangesPointAt(source, span[0])
		er, ec := includedRangesPointAt(source, span[1])
		cRanges = append(cRanges, sitter.Range{
			StartByte:  uint(span[0]),
			EndByte:    uint(span[1]),
			StartPoint: sitter.Point{Row: sr, Column: sc},
			EndPoint:   sitter.Point{Row: er, Column: ec},
		})
		goRanges = append(goRanges, gotreesitter.Range{
			StartByte:  uint32(span[0]),
			EndByte:    uint32(span[1]),
			StartPoint: gotreesitter.Point{Row: uint32(sr), Column: uint32(sc)},
			EndPoint:   gotreesitter.Point{Row: uint32(er), Column: uint32(ec)},
		})
	}

	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLanguage); err != nil {
		t.Fatal(err)
	}
	if err := cParser.SetIncludedRanges(cRanges); err != nil {
		t.Fatal(err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("C parser returned no tree")
	}
	defer cTree.Close()

	goParser := gotreesitter.NewParser(goLanguage)
	goParser.SetIncludedRanges(goRanges)
	goTree, err := goParser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if goTree == nil || goTree.RootNode() == nil {
		t.Fatal("Go parser returned no tree")
	}
	defer goTree.Release()

	if diff := FirstDivergenceDumpV1(goTree.RootNode(), goLanguage, cTree.RootNode()); diff != nil {
		t.Fatalf("first divergence = %+v, want exact JSON parity", diff)
	}
	goInspection, err := benchfixtures.InspectGoTree(goTree.RootNode(), goLanguage)
	if err != nil {
		t.Fatal(err)
	}
	cDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		t.Fatal(err)
	}
	if goInspection.SHA256 != cDigest {
		t.Fatalf("Go digest = %s, C digest = %s", goInspection.SHA256, cDigest)
	}
	if root := goTree.RootNode(); root.StartByte() != 2 || root.EndByte() != 14 || root.HasError() {
		t.Fatalf("Go root = %s [%d,%d) error=%t, want document [2,14) without error",
			root.Type(goLanguage), root.StartByte(), root.EndByte(), root.HasError())
	}
}
