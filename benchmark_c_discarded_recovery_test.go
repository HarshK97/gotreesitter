package gotreesitter_test

import (
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestCDiscardedRecoveryBranchRestoresCleanCostMode(t *testing.T) {
	source := makeCInitializerSource(40)
	parser := gotreesitter.NewParser(grammars.CLanguage())
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse C initializer: %v", err)
	}
	if tree == nil {
		t.Fatal("C initializer parse returned a nil tree")
	}
	t.Cleanup(tree.Release)

	requireCleanCInitializerTree(t, source, tree)
	if got := tree.ParseRuntime().MaxStacksSeen; got > 8 {
		t.Fatalf("peak stack count = %d, want at most 8 after the discarded branch", got)
	}
}

// BenchmarkCInitializerAfterDiscardedRecoveryBranch measures a clean C parse
// that creates and then discards a transient recovery branch.
func BenchmarkCInitializerAfterDiscardedRecoveryBranch(b *testing.B) {
	source := makeCInitializerSource(40)
	parser := gotreesitter.NewParser(grammars.CLanguage())

	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := parser.Parse(source)
		if err != nil {
			b.Fatalf("parse C initializer: %v", err)
		}
		requireCleanCInitializerTree(b, source, tree)
		tree.Release()
	}
}

func makeCInitializerSource(rows int) []byte {
	const prefix = "typedef struct { unsigned long x[4]; } scalar_t; static const scalar_t values[] = {"
	return []byte(prefix + strings.Repeat("{{1,2,3,4}},", rows) + "};")
}

func requireCleanCInitializerTree(tb testing.TB, source []byte, tree *gotreesitter.Tree) {
	tb.Helper()
	if tree == nil || tree.RootNode() == nil {
		tb.Fatal("C initializer parse returned a nil tree")
	}
	root := tree.RootNode()
	if root.StartByte() != 0 || root.EndByte() != uint32(len(source)) {
		tb.Fatalf("root span = %d:%d, want 0:%d", root.StartByte(), root.EndByte(), len(source))
	}
	if root.HasError() {
		tb.Fatal("C initializer parse returned an error tree")
	}
	runtime := tree.ParseRuntime()
	if got, want := runtime.StopReason, gotreesitter.ParseStopAccepted; got != want {
		tb.Fatalf("stop reason = %q, want %q", got, want)
	}
	if runtime.Truncated {
		tb.Fatal("C initializer parse returned a truncated tree")
	}
}
