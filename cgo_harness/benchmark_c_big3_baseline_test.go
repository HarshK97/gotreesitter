//go:build cgo && treesitter_c_bench_legacy

package cgoharness

import (
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	sitterpython "github.com/smacker/go-tree-sitter/python"
	sitterts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

type cTreeSitterBenchmarkSpec struct {
	language func() *sitter.Language
	source   func(int) []byte
	marker   string
}

func newCTreeSitterParserWithLanguage(tb testing.TB, language func() *sitter.Language) *sitter.Parser {
	tb.Helper()
	parser := sitter.NewParser()
	if parser == nil {
		tb.Fatal("sitter.NewParser returned nil")
		return nil
	}
	parser.SetLanguage(language())
	return parser
}

func benchmarkCTreeSitterParseFull(b *testing.B, spec cTreeSitterBenchmarkSpec) {
	parser := newCTreeSitterParserWithLanguage(b, spec.language)
	defer parser.Close()

	src := spec.source(benchmarkFuncCount(b))

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tree := parseCTree(b, parser, nil, src, "c full")
		requireCompleteCTree(b, tree, src, "c full")
		tree.Close()
	}
}

func benchmarkCTreeSitterParseIncrementalSingleByteEdit(b *testing.B, spec cTreeSitterBenchmarkSpec) {
	parser := newCTreeSitterParserWithLanguage(b, spec.language)
	defer parser.Close()

	src := spec.source(benchmarkFuncCount(b))
	sites := makeBenchmarkEditSites(src, spec.marker)
	if len(sites) == 0 {
		b.Fatalf("could not find edit marker %q", spec.marker)
	}
	site := sites[0]

	tree := parseCTree(b, parser, nil, src, "initial")
	if tree == nil || tree.RootNode() == nil {
		b.Fatal("initial parse returned nil root")
	}
	defer tree.Close()

	edit := sitter.EditInput{
		StartIndex:  uint32(site.offset),
		OldEndIndex: uint32(site.offset + 1),
		NewEndIndex: uint32(site.offset + 1),
		StartPoint:  sitter.Point{Row: site.start.Row, Column: site.start.Column},
		OldEndPoint: sitter.Point{Row: site.end.Row, Column: site.end.Column},
		NewEndPoint: sitter.Point{Row: site.end.Row, Column: site.end.Column},
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		toggleDigitAt(src, site.offset)
		tree.Edit(edit)
		newTree := parseCTree(b, parser, tree, src, "incremental")
		if newTree == nil || newTree.RootNode() == nil {
			b.Fatal("incremental parse returned nil root")
		}
		tree.Close()
		tree = newTree
	}
}

func benchmarkCTreeSitterParseIncrementalNoEdit(b *testing.B, spec cTreeSitterBenchmarkSpec) {
	parser := newCTreeSitterParserWithLanguage(b, spec.language)
	defer parser.Close()

	src := spec.source(benchmarkFuncCount(b))
	tree := parseCTree(b, parser, nil, src, "initial")
	if tree == nil || tree.RootNode() == nil {
		b.Fatal("initial parse returned nil root")
	}
	defer tree.Close()

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		newTree := parseCTree(b, parser, tree, src, "incremental")
		if newTree == nil || newTree.RootNode() == nil {
			b.Fatal("incremental parse returned nil root")
		}
		tree.Close()
		tree = newTree
	}
}

func BenchmarkCTreeSitterTypeScriptParseFull(b *testing.B) {
	benchmarkCTreeSitterParseFull(b, cTreeSitterBenchmarkSpec{
		language: sitterts.GetLanguage,
		source:   makeTypeScriptBenchmarkSource,
		marker:   "const v = ",
	})
}

func BenchmarkCTreeSitterTypeScriptParseIncrementalSingleByteEdit(b *testing.B) {
	benchmarkCTreeSitterParseIncrementalSingleByteEdit(b, cTreeSitterBenchmarkSpec{
		language: sitterts.GetLanguage,
		source:   makeTypeScriptBenchmarkSource,
		marker:   "const v = ",
	})
}

func BenchmarkCTreeSitterTypeScriptParseIncrementalNoEdit(b *testing.B) {
	benchmarkCTreeSitterParseIncrementalNoEdit(b, cTreeSitterBenchmarkSpec{
		language: sitterts.GetLanguage,
		source:   makeTypeScriptBenchmarkSource,
		marker:   "const v = ",
	})
}

func BenchmarkCTreeSitterPythonParseFull(b *testing.B) {
	benchmarkCTreeSitterParseFull(b, cTreeSitterBenchmarkSpec{
		language: sitterpython.GetLanguage,
		source:   makePythonBenchmarkSource,
		marker:   "v = ",
	})
}

func BenchmarkCTreeSitterPythonParseIncrementalSingleByteEdit(b *testing.B) {
	benchmarkCTreeSitterParseIncrementalSingleByteEdit(b, cTreeSitterBenchmarkSpec{
		language: sitterpython.GetLanguage,
		source:   makePythonBenchmarkSource,
		marker:   "v = ",
	})
}

func BenchmarkCTreeSitterPythonParseIncrementalNoEdit(b *testing.B) {
	benchmarkCTreeSitterParseIncrementalNoEdit(b, cTreeSitterBenchmarkSpec{
		language: sitterpython.GetLanguage,
		source:   makePythonBenchmarkSource,
		marker:   "v = ",
	})
}
