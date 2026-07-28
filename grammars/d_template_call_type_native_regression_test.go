//go:build !grammar_subset

package grammars

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const dTemplateCallTypeSource = "void f() { foo!int(); }"

func TestDTemplateCallTypeNeedsNoResultCompatibility(t *testing.T) {
	source := []byte(dTemplateCallTypeSource)
	language := DLanguage()
	tree, err := gotreesitter.NewParser(language).
		ParseNoResultCompatibilityBenchmarkOnly(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)

	assertDTemplateCallType(t, "compatibility-free", tree, language)
}

func TestDTemplateCallTypeRoutes(t *testing.T) {
	baseSource := []byte(dTemplateCallTypeSource)
	source := append(append([]byte(nil), baseSource...), '\n')
	language := DLanguage()

	productionParser := gotreesitter.NewParser(language)
	productionParser.SetAdmissionCandidateRoute(false)
	production, err := productionParser.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(production.Release)

	forestParser := gotreesitter.NewParser(language)
	forest, ok := forestParser.ParseForestExperimental(source)
	if !ok || forest == nil {
		offset, symbol, reason, _ := forestParser.ForestDeclineInfo()
		t.Fatalf("forest declined at %d symbol=%d reason=%s", offset, symbol, reason)
	}
	t.Cleanup(forest.Release)
	if !forest.ParseRuntime().ForestFastPath {
		t.Fatal("forest parse did not report the forest route")
	}

	incrementalParser := gotreesitter.NewParser(language)
	incrementalParser.SetAdmissionCandidateRoute(false)
	oldTree, err := incrementalParser.Parse(baseSource)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(oldTree.Release)
	endPoint := retiredDispatchPointAtByte(baseSource, len(baseSource))
	oldTree.Edit(gotreesitter.InputEdit{
		StartByte:   uint32(len(baseSource)),
		OldEndByte:  uint32(len(baseSource)),
		NewEndByte:  uint32(len(source)),
		StartPoint:  endPoint,
		OldEndPoint: endPoint,
		NewEndPoint: gotreesitter.Point{Row: endPoint.Row + 1},
	})
	incremental, profile, err := incrementalParser.ParseIncrementalProfiled(source, oldTree)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(incremental.Release)
	if profile.ReuseUnsupported {
		if profile.ReuseUnsupportedReason == "" {
			t.Fatal("incremental reuse declined without a reason")
		}
	} else if !profile.OldTreeReuseRoute || profile.ReusedSubtrees == 0 || profile.ReusedBytes == 0 {
		t.Fatalf("incremental route did not reuse the old tree: %+v", profile)
	}

	want := production.RootNode().SExpr(language)
	for route, tree := range map[string]*gotreesitter.Tree{
		"production":  production,
		"forest":      forest,
		"incremental": incremental,
	} {
		assertDTemplateCallType(t, route, tree, language)
		if got := tree.RootNode().SExpr(language); got != want {
			t.Fatalf("%s tree differs from production\ngot:  %s\nwant: %s", route, got, want)
		}
	}
}

func assertDTemplateCallType(
	t *testing.T,
	route string,
	tree *gotreesitter.Tree,
	language *gotreesitter.Language,
) {
	t.Helper()
	root := tree.RootNode()
	if root == nil || root.HasError() {
		t.Fatalf("%s root = %v, want a clean tree", route, root)
	}
	call := findFirstNamedDescendantWhere(
		root,
		language,
		"call_expression",
		func(*gotreesitter.Node) bool { return true },
	)
	if call == nil {
		t.Fatalf("%s has no call_expression: %s", route, root.SExpr(language))
	}
	if got, want := call.ChildCount(), 2; got != want {
		t.Fatalf("%s call child count = %d, want %d: %s", route, got, want, call.SExpr(language))
	}
	callType := call.Child(0)
	if callType == nil || callType.Type(language) != "type" {
		t.Fatalf("%s call child 0 = %v, want type: %s", route, callType, call.SExpr(language))
	}
	if got, want := callType.NamedChildCount(), 1; got != want {
		t.Fatalf("%s type named child count = %d, want %d: %s", route, got, want, callType.SExpr(language))
	}
	template := callType.NamedChild(0)
	if template == nil || template.Type(language) != "template_instance" {
		t.Fatalf("%s type child = %v, want template_instance: %s", route, template, callType.SExpr(language))
	}
}
