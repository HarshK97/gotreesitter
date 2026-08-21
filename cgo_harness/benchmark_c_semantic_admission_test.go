//go:build cgo && treesitter_c_bench

package cgoharness

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestGeneratedGoBenchmarkSemanticAdmission proves the generated benchmark
// source against fresh and incremental Go and C trees in both edit directions.
func TestGeneratedGoBenchmarkSemanticAdmission(t *testing.T) {
	goLang := grammars.GoLanguage()
	goParser := gotreesitter.NewParser(goLang)
	cParser := newOracleCTreeSitterParser(t)
	defer cParser.Close()

	source := makeGoBenchmarkSource(500)
	sites := makeGoBenchmarkEditSites(source)
	if len(sites) == 0 {
		t.Fatal("generated benchmark source has no edit site")
	}
	site := sites[0]
	edited := append([]byte(nil), source...)
	toggleDigitAt(edited, site.offset)

	directions := []struct {
		name string
		from []byte
		to   []byte
	}{
		{name: "forward", from: source, to: edited},
		{name: "reverse", from: edited, to: source},
	}
	for _, direction := range directions {
		direction := direction
		t.Run(direction.name, func(t *testing.T) {
			goFresh, err := goParser.Parse(direction.to)
			if err != nil {
				t.Fatalf("fresh Go parse: %v", err)
			}
			defer goFresh.Release()
			cFresh := parseOracleCTree(t, cParser, nil, direction.to, "fresh C")
			defer cFresh.Close()
			assertGeneratedSemanticPair(t, "fresh", direction.to, goFresh, cFresh, goLang)

			goOld, err := goParser.Parse(direction.from)
			if err != nil {
				t.Fatalf("old Go parse: %v", err)
			}
			goOld.Edit(generatedGoBenchmarkEdit(direction.from, direction.to, site))
			goIncremental, err := goParser.ParseIncremental(direction.to, goOld)
			if err != nil {
				t.Fatalf("incremental Go parse: %v", err)
			}
			if goIncremental != goOld {
				goOld.Release()
			}
			defer goIncremental.Release()
			cOld := parseOracleCTree(t, cParser, nil, direction.from, "old C")
			cEdit := generatedCTreeSitterEdit(direction.from, direction.to, site)
			cOld.Edit(&cEdit)
			cIncremental := parseOracleCTree(t, cParser, cOld, direction.to, "incremental C")
			cOld.Close()
			defer cIncremental.Close()

			assertGeneratedSemanticPair(t, "incremental", direction.to, goIncremental, cIncremental, goLang)
			assertGeneratedTreeEquivalent(t, "Go incremental/fresh Go", goIncremental, goFresh, goLang)
			assertGeneratedCTreeEquivalent(t, "C incremental/fresh C", cIncremental, cFresh)
		})
	}
}

func generatedGoBenchmarkEdit(from, to []byte, site benchmarkEditSite) gotreesitter.InputEdit {
	return gotreesitter.InputEdit{
		StartByte:   uint32(site.offset),
		OldEndByte:  uint32(site.offset + 1),
		NewEndByte:  uint32(site.offset + 1),
		StartPoint:  pointAtOffset(from, site.offset),
		OldEndPoint: pointAtOffset(from, site.offset+1),
		NewEndPoint: pointAtOffset(to, site.offset+1),
	}
}

func generatedCTreeSitterEdit(from, to []byte, site benchmarkEditSite) sitter.InputEdit {
	start := pointAtOffset(from, site.offset)
	oldEnd := pointAtOffset(from, site.offset+1)
	newEnd := pointAtOffset(to, site.offset+1)
	return sitter.InputEdit{
		StartByte:      uint(site.offset),
		OldEndByte:     uint(site.offset + 1),
		NewEndByte:     uint(site.offset + 1),
		StartPosition:  sitter.Point{Row: uint(start.Row), Column: uint(start.Column)},
		OldEndPosition: sitter.Point{Row: uint(oldEnd.Row), Column: uint(oldEnd.Column)},
		NewEndPosition: sitter.Point{Row: uint(newEnd.Row), Column: uint(newEnd.Column)},
	}
}

func assertGeneratedSemanticPair(tb testing.TB, phase string, source []byte, goTree *gotreesitter.Tree, cTree *sitter.Tree, goLang *gotreesitter.Language) {
	tb.Helper()
	goRoot := goTree.RootNode()
	cRoot := cTree.RootNode()
	if goRoot == nil || cRoot == nil {
		tb.Fatalf("%s root is nil", phase)
	}
	if got, want := goRoot.StartByte(), uint32(0); got != want {
		tb.Fatalf("%s Go root start=%d want=%d", phase, got, want)
	}
	if got, want := goRoot.EndByte(), uint32(len(source)); got != want {
		tb.Fatalf("%s Go root end=%d want=%d", phase, got, want)
	}
	if got, want := cRoot.StartByte(), uint(0); got != want {
		tb.Fatalf("%s C root start=%d want=%d", phase, got, want)
	}
	if got, want := cRoot.EndByte(), uint(len(source)); got != want {
		tb.Fatalf("%s C root end=%d want=%d", phase, got, want)
	}
	if goRoot.HasError() != cRoot.HasError() {
		tb.Fatalf("%s root error mismatch: Go=%v C=%v", phase, goRoot.HasError(), cRoot.HasError())
	}
	goInspection, err := benchfixtures.InspectGoTree(goRoot, goLang)
	if err != nil {
		tb.Fatalf("%s Go deep digest: %v", phase, err)
	}
	cDigest, err := COracleDeepDigest(cTree)
	if err != nil {
		tb.Fatalf("%s C deep digest: %v", phase, err)
	}
	if goInspection.SHA256 != cDigest {
		tb.Fatalf("%s deep digest mismatch: Go=%s C=%s", phase, goInspection.SHA256, cDigest)
	}
	goSExpr := generatedGoNamedSExpr(goRoot, goLang)
	cSExpr := generatedCNamedSExpr(cRoot)
	if goSExpr != cSExpr {
		tb.Fatalf("%s named S-expression mismatch\nGo: %s\nC: %s", phase, goSExpr, cSExpr)
	}
}

func assertGeneratedTreeEquivalent(tb testing.TB, phase string, got, want *gotreesitter.Tree, lang *gotreesitter.Language) {
	tb.Helper()
	if got.RootNode().StartByte() != want.RootNode().StartByte() || got.RootNode().EndByte() != want.RootNode().EndByte() || got.RootNode().HasError() != want.RootNode().HasError() {
		tb.Fatalf("%s root semantic mismatch", phase)
	}
	gotDigest, err := benchfixtures.InspectGoTree(got.RootNode(), lang)
	if err != nil {
		tb.Fatalf("%s got digest: %v", phase, err)
	}
	wantDigest, err := benchfixtures.InspectGoTree(want.RootNode(), lang)
	if err != nil {
		tb.Fatalf("%s want digest: %v", phase, err)
	}
	if gotDigest.SHA256 != wantDigest.SHA256 {
		tb.Fatalf("%s deep digest mismatch: got=%s want=%s", phase, gotDigest.SHA256, wantDigest.SHA256)
	}
	if generatedGoNamedSExpr(got.RootNode(), lang) != generatedGoNamedSExpr(want.RootNode(), lang) {
		tb.Fatalf("%s named S-expression mismatch", phase)
	}
}

func assertGeneratedCTreeEquivalent(tb testing.TB, phase string, got, want *sitter.Tree) {
	tb.Helper()
	if got.RootNode().StartByte() != want.RootNode().StartByte() || got.RootNode().EndByte() != want.RootNode().EndByte() || got.RootNode().HasError() != want.RootNode().HasError() {
		tb.Fatalf("%s root semantic mismatch", phase)
	}
	gotDigest, err := COracleDeepDigest(got)
	if err != nil {
		tb.Fatalf("%s got digest: %v", phase, err)
	}
	wantDigest, err := COracleDeepDigest(want)
	if err != nil {
		tb.Fatalf("%s want digest: %v", phase, err)
	}
	if gotDigest != wantDigest {
		tb.Fatalf("%s deep digest mismatch: got=%s want=%s", phase, gotDigest, wantDigest)
	}
	if generatedCNamedSExpr(got.RootNode()) != generatedCNamedSExpr(want.RootNode()) {
		tb.Fatalf("%s named S-expression mismatch", phase)
	}
}

func generatedGoNamedSExpr(node *gotreesitter.Node, lang *gotreesitter.Language) string {
	var b strings.Builder
	var visit func(*gotreesitter.Node)
	visit = func(current *gotreesitter.Node) {
		if current == nil {
			return
		}
		b.WriteByte('(')
		b.WriteString(current.Type(lang))
		for i := 0; i < current.ChildCount(); i++ {
			child := current.Child(i)
			if child != nil && child.IsNamed() {
				b.WriteByte(' ')
				visit(child)
			}
		}
		b.WriteByte(')')
	}
	visit(node)
	return b.String()
}

func generatedCNamedSExpr(node *sitter.Node) string {
	var b strings.Builder
	var visit func(*sitter.Node)
	visit = func(current *sitter.Node) {
		if current == nil || !current.IsNamed() {
			return
		}
		b.WriteByte('(')
		b.WriteString(current.Kind())
		for i := uint(0); i < current.ChildCount(); i++ {
			child := current.Child(i)
			if child != nil && child.IsNamed() {
				b.WriteByte(' ')
				visit(child)
			}
		}
		b.WriteByte(')')
	}
	visit(node)
	return b.String()
}
