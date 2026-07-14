package gotreesitter_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

// BenchmarkGoParseWarmRealDFA measures warm full-parse lifecycle cost on
// authenticated snapshots of human-authored Go source. Fixture decompression,
// grammar loading, parser construction, arena-pool draining, and one explicit
// warm-up parse all happen outside the timed region. Each timed operation owns
// and releases exactly one fully validated tree.
func BenchmarkGoParseWarmRealDFA(b *testing.B) {
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		b.Fatal(err)
	}
	statsEnabled := strings.TrimSpace(os.Getenv("GOT_STATS")) != ""
	if statsEnabled {
		gotreesitter.EnableRuntimeAudit(true)
		defer gotreesitter.EnableRuntimeAudit(false)
	}

	for _, fixture := range fixtures {
		fixture := fixture
		b.Run(fixture.Fixture.ID, func(b *testing.B) {
			benchmarkWarmRealGoDFA(b, fixture)
		})
	}
}

func benchmarkWarmRealGoDFA(b *testing.B, fixture benchfixtures.LoadedFixture) {
	b.Helper()
	if err := fixture.Fixture.VerifySource(fixture.Source); err != nil {
		b.Fatal(err)
	}

	gotreesitter.DrainArenaPools()
	lang := grammars.GoLanguage()
	parser := gotreesitter.NewParser(lang)
	warmTree, err := parser.Parse(fixture.Source)
	if err != nil {
		releaseBenchmarkTree(warmTree)
		b.Fatalf("%s warm parse: %v", fixture.Fixture.ID, err)
	}
	if err := validateRealGoBenchmarkTree(warmTree, fixture.Source, lang); err != nil {
		releaseBenchmarkTree(warmTree)
		b.Fatalf("%s warm parse: %v", fixture.Fixture.ID, err)
	}
	warmDigest, err := benchfixtures.DigestGoTree(warmTree.RootNode(), lang)
	if err != nil {
		releaseBenchmarkTree(warmTree)
		b.Fatalf("%s warm parse digest: %v", fixture.Fixture.ID, err)
	}
	if err := fixture.Fixture.VerifyDeepTreeDigest(warmDigest); err != nil {
		releaseBenchmarkTree(warmTree)
		b.Fatalf("%s warm parse digest: %v", fixture.Fixture.ID, err)
	}
	warmTree.Release()

	b.ReportAllocs()
	b.SetBytes(int64(len(fixture.Source)))
	b.ResetTimer()

	var lastRuntime gotreesitter.ParseRuntime
	for i := 0; i < b.N; i++ {
		tree, err := parser.Parse(fixture.Source)
		if err != nil {
			releaseBenchmarkTree(tree)
			b.Fatalf("%s timed parse: %v", fixture.Fixture.ID, err)
		}
		if err := validateRealGoBenchmarkTree(tree, fixture.Source, lang); err != nil {
			releaseBenchmarkTree(tree)
			b.Fatalf("%s timed parse: %v", fixture.Fixture.ID, err)
		}
		if i == b.N-1 {
			lastRuntime = tree.ParseRuntime()
		}
		tree.Release()
	}
	b.StopTimer()
	reportRealGoRuntime(b, lastRuntime)
}

func validateRealGoBenchmarkTree(tree *gotreesitter.Tree, source []byte, lang *gotreesitter.Language) error {
	if tree == nil {
		return fmt.Errorf("parse returned nil tree")
	}
	root := tree.RootNode()
	if root == nil {
		return fmt.Errorf("parse returned nil root")
	}
	if got, want := root.StartByte(), uint32(0); got != want {
		return fmt.Errorf("root.StartByte=%d want=%d", got, want)
	}
	if got, want := root.EndByte(), uint32(len(source)); got != want {
		return fmt.Errorf("root.EndByte=%d want=%d (%s)", got, want, tree.ParseRuntime().Summary())
	}
	if root.HasError() {
		return fmt.Errorf("root %q has errors (%s)", root.Type(lang), tree.ParseRuntime().Summary())
	}
	if tree.ParseStoppedEarly() {
		return fmt.Errorf("parse stopped early (%s)", tree.ParseRuntime().Summary())
	}
	return nil
}

func releaseBenchmarkTree(tree *gotreesitter.Tree) {
	if tree != nil {
		tree.Release()
	}
}

func reportRealGoRuntime(b *testing.B, rt gotreesitter.ParseRuntime) {
	b.Helper()
	b.ReportMetric(float64(rt.MaxStacksSeen), "max_stacks")
	b.ReportMetric(float64(rt.PeakStackDepth), "peak_depth")
	b.ReportMetric(float64(rt.TokensConsumed), "tokens/op")
	b.ReportMetric(float64(rt.MultiStackIterations), "multi_iters/op")
	b.ReportMetric(float64(rt.MultiStackTokens), "multi_tokens/op")
	b.ReportMetric(float64(rt.NodesAllocated), "nodes/op")
	b.ReportMetric(float64(rt.ArenaBytesAllocated), "arena_B/op")
	b.ReportMetric(float64(rt.NormalizationPassesRun), "normalization_runs/op")
	if rt.ForestFastPath {
		b.ReportMetric(1, "forest_fast_path")
	} else {
		b.ReportMetric(0, "forest_fast_path")
	}
	constructed := rt.LeafNodesConstructed + rt.ParentNodesConstructed
	if rt.FinalNodes > 0 && constructed > 0 {
		b.ReportMetric(float64(constructed)/float64(rt.FinalNodes), "constructed/final")
	}
}

func TestGoFullParseBenchmarkFixturesParseClean(t *testing.T) {
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		t.Fatal(err)
	}
	lang := grammars.GoLanguage()
	parser := gotreesitter.NewParser(lang)
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Fixture.ID, func(t *testing.T) {
			tree, err := parser.Parse(fixture.Source)
			if err != nil {
				releaseBenchmarkTree(tree)
				t.Fatal(err)
			}
			defer tree.Release()
			if err := validateRealGoBenchmarkTree(tree, fixture.Source, lang); err != nil {
				t.Fatal(err)
			}
			digest, err := benchfixtures.DigestGoTree(tree.RootNode(), lang)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.Fixture.VerifyDeepTreeDigest(digest); err != nil {
				t.Fatal(err)
			}
		})
	}
}
