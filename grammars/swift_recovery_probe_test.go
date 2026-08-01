package grammars

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

func TestSwiftCleanRecoveryProbeMatchesLegacyTree(t *testing.T) {
	lang := SwiftLanguage()
	cases := []struct {
		name      string
		source    string
		wantProbe bool
	}{
		{
			name: "for-range",
			source: "func f(n: Int) -> Int {\n" +
				"  var total = 0\n" +
				"  for i in 0..<n { total += i }\n" +
				"  return total\n" +
				"}\n",
			wantProbe: true,
		},
		{
			name: "for-closed-range",
			source: "func f(n: Int) -> Int {\n" +
				"  var total = 0\n" +
				"  for i in 0...n { total += i }\n" +
				"  return total\n" +
				"}\n",
			wantProbe: true,
		},
		{
			name:      "native-ternary",
			source:    "func f(x: Int) -> Int { return x > 0 ? 1 : 2 }\n",
			wantProbe: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legacy, _ := parseSwiftRecoveryProbeRoute(t, lang, []byte(tc.source), "legacy")
			defer legacy.Release()
			initial, runtime := parseSwiftRecoveryProbeRoute(t, lang, []byte(tc.source), "")
			defer initial.Release()

			swiftRequireSameRecoveryProbeTree(t, lang, legacy.RootNode(), initial.RootNode(), "root")
			if root := initial.RootNode(); root == nil || root.HasError() {
				t.Fatalf("initial-only route returned an error tree: %v", root)
			}
			if got, want := initial.RootNode().EndByte(), uint32(len(tc.source)); got != want {
				t.Fatalf("initial-only root end = %d, want %d", got, want)
			}
			if tc.wantProbe {
				if got, want := runtime.RecoveryProbeInitialAttempts, uint64(1); got != want {
					t.Fatalf("initial probes = %d, want %d", got, want)
				}
				if got, want := runtime.RecoveryProbeInitialAccepted, uint64(1); got != want {
					t.Fatalf("accepted initial probes = %d, want %d", got, want)
				}
			}
			if got := runtime.RecoveryProbeLegacyFallbacks; got != 0 {
				t.Fatalf("legacy fallbacks = %d, want 0", got)
			}
			if got := runtime.RecoveryProbeInitialRetryPasses; got != 0 {
				t.Fatalf("initial retry passes = %d, want 0", got)
			}
			if got := runtime.RecoveryProbeLegacyRetryPasses; got != 0 {
				t.Fatalf("legacy retry passes = %d, want 0", got)
			}
		})
	}
}

func TestSwiftUnsafeWitnessKeepsCurrentGoTreeAndSkipsRecoveryProbe(t *testing.T) {
	lang := SwiftLanguage()
	source, err := os.ReadFile(filepath.Join("testdata", "swift_corpus", "stdlib_FloatingPointToString.swift"))
	if err != nil {
		t.Fatalf("read Swift witness: %v", err)
	}
	t.Setenv("GTS_DISPATCHER_CENSUS", "1")

	legacy, _ := parseSwiftRecoveryProbeRoute(t, lang, source, "legacy")
	defer legacy.Release()
	initial, runtime := parseSwiftRecoveryProbeRoute(t, lang, source, "")
	defer initial.Release()

	legacyRoot := legacy.RootNode()
	initialRoot := initial.RootNode()
	swiftRequireSameRecoveryProbeTree(t, lang, legacyRoot, initialRoot, "root")
	if initialRoot == nil || !initialRoot.HasError() {
		t.Fatal("Swift unsafe witness no longer has its known error tree")
	}
	if got, want := initialRoot.EndByte(), uint32(len(source)); got != want {
		t.Fatalf("Swift unsafe witness root end = %d, want %d", got, want)
	}
	inspection, err := benchfixtures.InspectGoTree(initialRoot, lang)
	if err != nil {
		t.Fatalf("inspect Swift unsafe witness: %v", err)
	}
	const wantDigest = "ec51c633a3f99515cc0cd1c0cff435a44ddc7db8e83705977d28f78bdfb0fc0e"
	if inspection.SHA256 != wantDigest {
		t.Fatalf("Swift unsafe witness digest = %s, want %s", inspection.SHA256, wantDigest)
	}
	t.Logf("Swift unsafe witness gts-deep-tree-v1 digest: %s", inspection.SHA256)

	if got := runtime.RecoveryProbeInitialAttempts; got != 0 {
		t.Fatalf("initial probes = %d, want 0", got)
	}
	if got := runtime.RecoveryProbeInitialAccepted; got != 0 {
		t.Fatalf("accepted initial probes = %d, want 0", got)
	}
	if got := runtime.RecoveryProbeLegacyFallbacks; got != 0 {
		t.Fatalf("legacy fallbacks = %d, want 0", got)
	}
	if got := runtime.RecoveryProbeInitialRetryPasses; got != 0 {
		t.Fatalf("initial retry passes = %d, want 0", got)
	}
	if got := runtime.RecoveryProbeLegacyRetryPasses; got != 0 {
		t.Fatalf("legacy retry passes = %d, want 0", got)
	}
	if got := runtime.SwiftLegacyRecoverySubparseAttempts; got != 0 {
		t.Fatalf("Swift legacy recovery parses = %d, want 0", got)
	}
	if got := runtime.SwiftLegacyRecoveryRetryPasses; got != 0 {
		t.Fatalf("Swift legacy retry passes = %d, want 0", got)
	}

	for _, name := range []string{
		"dispatch.swift.conditions",
		"dispatch.swift.ternary",
		"dispatch.swift.top-level",
		"dispatch.swift.control",
	} {
		pass, ok := swiftRecoveryProbePass(runtime, name)
		if !ok {
			t.Fatalf("missing census pass %q", name)
		}
		if pass.Checked != 1 || pass.Run != 1 {
			t.Fatalf("census pass %q checked=%d run=%d, want 1/1", name, pass.Checked, pass.Run)
		}
		if pass.NodesVisited == 0 {
			t.Fatalf("census pass %q visited no nodes", name)
		}
		if pass.NodesRewritten != 0 {
			t.Fatalf("census pass %q rewrote %d nodes, want 0", name, pass.NodesRewritten)
		}
	}
}

func BenchmarkSwiftCleanFullSourceRecoveryProbe(b *testing.B) {
	lang := SwiftLanguage()
	source := []byte("func f(n: Int) -> Int {\n" +
		"  var total = 0\n" +
		"  for i in 0..<n { total += i }\n" +
		"  return total\n" +
		"}\n")

	for _, route := range []struct {
		name string
		mode string
	}{
		{name: "legacy-retry", mode: "legacy"},
		{name: "initial-only", mode: ""},
	} {
		b.Run(route.name, func(b *testing.B) {
			b.Setenv("GOT_SWIFT_CLEAN_RECOVERY_PROBE_MODE", route.mode)
			parser := gotreesitter.NewParser(lang)
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tree, err := parser.Parse(source)
				if err != nil {
					b.Fatalf("parse: %v", err)
				}
				if root := tree.RootNode(); root == nil || root.HasError() {
					tree.Release()
					b.Fatal("recovery probe benchmark returned an error tree")
				}
				tree.Release()
			}
		})
	}
}

func parseSwiftRecoveryProbeRoute(t *testing.T, lang *gotreesitter.Language, source []byte, mode string) (*gotreesitter.Tree, gotreesitter.ParseRuntime) {
	t.Helper()
	t.Setenv("GOT_SWIFT_CLEAN_RECOVERY_PROBE_MODE", mode)
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse Swift recovery probe route %q: %v", mode, err)
	}
	if tree == nil || tree.RootNode() == nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatalf("Swift recovery probe route %q returned no tree/root", mode)
	}
	return tree, tree.ParseRuntime()
}

func swiftRecoveryProbePass(runtime gotreesitter.ParseRuntime, name string) (gotreesitter.NormalizationPassRuntime, bool) {
	if runtime.NormalizationPasses == nil {
		return gotreesitter.NormalizationPassRuntime{}, false
	}
	for _, pass := range *runtime.NormalizationPasses {
		if pass.Name == name {
			return pass, true
		}
	}
	return gotreesitter.NormalizationPassRuntime{}, false
}

func swiftRequireSameRecoveryProbeTree(t *testing.T, lang *gotreesitter.Language, want, got *gotreesitter.Node, path string) {
	t.Helper()
	if want == nil || got == nil {
		if want != got {
			t.Fatalf("%s node presence differs", path)
		}
		return
	}
	if want.Type(lang) != got.Type(lang) ||
		want.StartByte() != got.StartByte() ||
		want.EndByte() != got.EndByte() ||
		want.StartPoint() != got.StartPoint() ||
		want.EndPoint() != got.EndPoint() ||
		want.IsNamed() != got.IsNamed() ||
		want.IsExtra() != got.IsExtra() ||
		want.IsMissing() != got.IsMissing() ||
		want.IsError() != got.IsError() ||
		want.HasError() != got.HasError() ||
		want.ChildCount() != got.ChildCount() {
		t.Fatalf("%s node differs: legacy=%s[%d:%d] initial=%s[%d:%d]", path, want.Type(lang), want.StartByte(), want.EndByte(), got.Type(lang), got.StartByte(), got.EndByte())
	}
	for i := 0; i < want.ChildCount(); i++ {
		if want.FieldNameForChild(i, lang) != got.FieldNameForChild(i, lang) {
			t.Fatalf("%s child %d field differs: legacy=%q initial=%q", path, i, want.FieldNameForChild(i, lang), got.FieldNameForChild(i, lang))
		}
		swiftRequireSameRecoveryProbeTree(t, lang, want.Child(i), got.Child(i), path+"/child")
	}
}
