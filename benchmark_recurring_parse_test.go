package gotreesitter_test

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func requireRecurringBenchmarkTree(b *testing.B, tree *gotreesitter.Tree, err error, wantError bool) {
	b.Helper()
	if err != nil {
		b.Fatalf("parse error: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		b.Fatal("parse returned nil tree or root")
	}
	if got := tree.RootNode().HasError(); got != wantError {
		b.Fatalf("root.HasError() = %v, want %v", got, wantError)
	}
}

// BenchmarkKDLParseRecurringTinyClean isolates the fixed per-Parse floor on a
// reused parser. The input is intentionally much smaller than the parser's
// retained scratch caches, making recurring reset work visible.
func BenchmarkKDLParseRecurringTinyClean(b *testing.B) {
	benchmarkRecurringTinyClean(b, grammars.KdlLanguage(), []byte("node\n"))
}

// BenchmarkJavaParseRecurringTinyCleanDFA isolates the built-in DFA route. The
// fleet scanner uses Java's registered TokenSourceFactory instead; keep this
// variant explicit so it cannot be mistaken for the fleet witness below.
func BenchmarkJavaParseRecurringTinyCleanDFA(b *testing.B) {
	benchmarkRecurringTinyClean(b, grammars.JavaLanguage(), []byte("class A {}\n"))
}

// BenchmarkJavaParseRecurringTinyTokenSourceFactory matches perf_scan's Java
// route: construct the registered custom token source inside the timed
// operation, then parse it on a reused Parser.
func BenchmarkJavaParseRecurringTinyTokenSourceFactory(b *testing.B) {
	lang := grammars.JavaLanguage()
	parser := gotreesitter.NewParser(lang)
	src := []byte("class A {}\n")

	warm := grammars.NewJavaTokenSourceOrEOF(src, lang)
	warmTree, err := parser.ParseWithTokenSource(src, warm)
	requireRecurringBenchmarkTree(b, warmTree, err, false)
	warmTree.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := grammars.NewJavaTokenSourceOrEOF(src, lang)
		tree, err := parser.ParseWithTokenSource(src, ts)
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
		tree.Release()
	}
}

// BenchmarkJavaTokenSourceConstructRecurring isolates the per-call factory
// floor, including immutable token-symbol binding and cached lexer-table
// attachment, without parser work.
func BenchmarkJavaTokenSourceConstructRecurring(b *testing.B) {
	lang := grammars.JavaLanguage()
	src := []byte("class A {}\n")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts, err := grammars.NewJavaTokenSource(src, lang)
		if err != nil {
			b.Fatalf("token source: %v", err)
		}
		_ = ts
	}
}

// BenchmarkCSharpParseRecurringTinyClean is a second clean fleet-tail witness
// with substantially different grammar and conflict behavior from Java.
func BenchmarkCSharpParseRecurringTinyClean(b *testing.B) {
	benchmarkRecurringTinyClean(b, grammars.CSharpLanguage(), []byte("class A {}\n"))
}

// BenchmarkCSSParseRecurringTinyClean covers another clean/parity-passing row
// from the post-refresh fleet tail at negligible additional benchmark cost.
func BenchmarkCSSParseRecurringTinyClean(b *testing.B) {
	benchmarkRecurringTinyClean(b, grammars.CssLanguage(), []byte("a{color:red}\n"))
}

func benchmarkRecurringTinyClean(b *testing.B, lang *gotreesitter.Language, src []byte) {
	b.Helper()
	parser := gotreesitter.NewParser(lang)

	warm, err := parser.Parse(src)
	requireRecurringBenchmarkTree(b, warm, err, false)
	warm.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := parser.Parse(src)
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
		tree.Release()
	}
}

// BenchmarkKDLParseRecurringErrorCleanAlternate exercises an error parse that
// grows recovery scratch followed by a tiny clean parse on the same Parser.
// Each operation is one error+clean pair so reset costs cannot hide behind a
// sub-benchmark boundary or a fresh parser.
func BenchmarkKDLParseRecurringErrorCleanAlternate(b *testing.B) {
	lang := grammars.KdlLanguage()
	parser := gotreesitter.NewParser(lang)
	errorSrc := makeKDLRecoveryGarbageSource(4, 8)
	cleanSrc := []byte("node\n")

	errorTree, err := parser.Parse(errorSrc)
	requireRecurringBenchmarkTree(b, errorTree, err, true)
	errorTree.Release()
	cleanTree, err := parser.Parse(cleanSrc)
	requireRecurringBenchmarkTree(b, cleanTree, err, false)
	cleanTree.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(errorSrc) + len(cleanSrc)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		errorTree, err := parser.Parse(errorSrc)
		if err != nil {
			b.Fatalf("error parse: %v", err)
		}
		errorTree.Release()

		cleanTree, err := parser.Parse(cleanSrc)
		if err != nil {
			b.Fatalf("clean parse: %v", err)
		}
		cleanTree.Release()
	}
}

// BenchmarkJSONParseRecurringTinyTokenSourceFactory measures the recurring
// floor when callers use the public factory route instead of Parser.Parse.
func BenchmarkJSONParseRecurringTinyTokenSourceFactory(b *testing.B) {
	lang := grammars.JsonLanguage()
	parser := gotreesitter.NewParser(lang)
	src := []byte("{}")
	factory := func(source []byte) (gotreesitter.TokenSource, error) {
		return grammars.NewJSONTokenSource(source, lang)
	}

	warm, err := parser.ParseWithTokenSourceFactory(src, factory)
	requireRecurringBenchmarkTree(b, warm, err, false)
	warm.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := parser.ParseWithTokenSourceFactory(src, factory)
		if err != nil {
			b.Fatalf("parse error: %v", err)
		}
		tree.Release()
	}
}
