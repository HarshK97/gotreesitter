//go:build !grammar_subset

package grammars

import (
	"fmt"
	"testing"

	"github.com/odvcencio/gotreesitter"
)

func TestCollapsedChildLedgerRealLanguagesNeedNoSafetyNetRewrite(t *testing.T) {
	tests := []struct {
		name           string
		lang           func() *gotreesitter.Language
		src            string
		compactDecline bool
		compactSrc     string
		compactWant    map[string]struct {
			child string
			named bool
			count int
		}
		forestEligible bool
		want           map[string]struct {
			child string
			named bool
			count int
		}
	}{
		{name: "ruby", lang: RubyLanguage, src: "%w(foo)\n%i(bar)\n", forestEligible: true, want: map[string]struct {
			child string
			named bool
			count int
		}{"bare_string": {child: "string_content", named: true, count: 1}, "bare_symbol": {child: "string_content", named: true, count: 1}}},
		{name: "kotlin", lang: KotlinLanguage, src: "package a\nimport d.*\n", forestEligible: true, want: map[string]struct {
			child string
			named bool
			count int
		}{"identifier": {child: "simple_identifier", named: true, count: 2}}},
		{name: "hack", lang: HackLanguage, src: "<?hh function f(): void { $a = true; $b = false; $c = null; }\n", compactDecline: true, want: map[string]struct {
			child string
			named bool
			count int
		}{"true": {child: "true", count: 1}, "false": {child: "false", count: 1}, "null": {child: "null", count: 1}}},
		{name: "dart", lang: DartLanguage, src: "class A { void f(){ this.f(); } } class B extends A { B(): super(); }\n", forestEligible: true, want: map[string]struct {
			child string
			named bool
			count int
		}{"this": {child: "this", count: 1}, "super": {child: "super", count: 1}}},
		{name: "elixir", lang: ElixirLanguage, src: "nil\n", forestEligible: true, want: map[string]struct {
			child string
			named bool
			count int
		}{"nil": {child: "nil", count: 1}}},
		{name: "apex", lang: ApexLanguage, forestEligible: true,
			compactSrc: "trigger T on Account (before insert, after delete) { System.debug(UserInfo.getUserId()); }\n",
			compactWant: map[string]struct {
				child string
				named bool
				count int
			}{"before_insert": {child: "before_insert", count: 1}, "after_delete": {child: "after_delete", count: 1}},
			src: `trigger T on Account (before insert, before update, before delete, after insert, after update, after delete, after undelete) { System.debug(UserInfo.getUserId()); }
public class C { public C(){ super(); } void f(Account a, User u) { insert a; delete a; undelete a; upsert a; insert as user a; update as system a; System.runAs(u) { } } }
`, want: map[string]struct {
				child string
				named bool
				count int
			}{
				"after_delete": {child: "after_delete", count: 1}, "after_insert": {child: "after_insert", count: 1},
				"after_undelete": {child: "after_undelete", count: 1}, "after_update": {child: "after_update", count: 1},
				"before_delete": {child: "before_delete", count: 1}, "before_insert": {child: "before_insert", count: 1},
				"before_update": {child: "before_update", count: 1}, "delete": {child: "delete", count: 1},
				"insert": {child: "insert", count: 2}, "super": {child: "super", count: 1},
				"system": {child: "system", count: 1}, "undelete": {child: "undelete", count: 1},
				"upsert": {child: "upsert", count: 1}, "user": {child: "user", count: 1},
			}},
	}
	seenPairs := make(map[string]struct{}, 23)
	for _, test := range tests {
		lang := test.lang()
		if lang.NativeResultCompatibility&gotreesitter.ResultCompatibilityNativeCollapsedChildren == 0 {
			t.Fatalf("%s exact artifact omitted collapsed-child capability", test.name)
		}
		for parentName, want := range test.want {
			parent, parentOK := collapsedChildRegressionSymbol(lang, parentName, true)
			child, childOK := collapsedChildRegressionSymbol(lang, want.child, want.named)
			if !parentOK || !childOK {
				t.Fatalf("%s route coverage could not resolve %s/%s named=%t", test.name, parentName, want.child, want.named)
			}
			key := fmt.Sprintf("%s:%d/%d", test.name, parent, child)
			if _, duplicate := seenPairs[key]; duplicate {
				t.Fatalf("duplicate collapsed-child route pair %s", key)
			}
			seenPairs[key] = struct{}{}
		}
	}
	if got := len(seenPairs); got != 23 {
		t.Fatalf("exact-artifact collapsed-child route rows=%d, want 23", got)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lang := test.lang()
			production := gotreesitter.NewParser(lang)
			production.SetAdmissionCandidateRoute(false)
			tree, err := production.Parse([]byte(test.src))
			if err != nil {
				t.Fatal(err)
			}
			assertCollapsedChildRouteTree(t, "production", tree, lang, test.want)
			if tree.ParseRuntime().ForestFastPath {
				t.Fatal("production-pinned parse reported forest route")
			}
			tree.Release()

			routedBefore, fallbackBefore := gotreesitter.AdmissionCandidateCounters()
			compact := gotreesitter.NewParser(lang)
			compact.SetAdmissionCandidateRoute(true)
			compactSrc, compactWant := test.compactSrc, test.compactWant
			if compactSrc == "" {
				compactSrc, compactWant = test.src, test.want
			}
			tree, err = compact.Parse([]byte(compactSrc))
			if err != nil {
				t.Fatal(err)
			}
			routedAfter, fallbackAfter := gotreesitter.AdmissionCandidateCounters()
			if test.compactDecline {
				assertCollapsedChildRouteTree(t, "compact-fallback", tree, lang, compactWant)
				if routedAfter != routedBefore || fallbackAfter != fallbackBefore+1 {
					t.Fatalf("compact decline counters routed=%d/%d fallback=%d/%d", routedBefore, routedAfter, fallbackBefore, fallbackAfter)
				}
			} else {
				assertCollapsedChildRouteTree(t, "compact-routed", tree, lang, compactWant)
				if routedAfter != routedBefore+1 || fallbackAfter != fallbackBefore {
					t.Fatalf("compact route counters routed=%d/%d fallback=%d/%d", routedBefore, routedAfter, fallbackBefore, fallbackAfter)
				}
			}
			tree.Release()

			if test.forestEligible {
				forest := gotreesitter.NewParser(lang)
				tree, ok := forest.ParseForestExperimental([]byte(test.src))
				if !ok || tree == nil {
					offset, symbol, reason, _ := forest.ForestDeclineInfo()
					t.Fatalf("eligible forest route declined at %d symbol=%d reason=%s", offset, symbol, reason)
				}
				assertCollapsedChildRouteTree(t, "forest", tree, lang, test.want)
				if !tree.ParseRuntime().ForestFastPath {
					t.Fatal("forest parse did not report forest route")
				}
				tree.Release()
			}
		})
	}
}

func collapsedChildRegressionSymbol(lang *gotreesitter.Language, name string, named bool) (gotreesitter.Symbol, bool) {
	for index, symbolName := range lang.SymbolNames {
		if symbolName == name && index < len(lang.SymbolMetadata) && lang.SymbolMetadata[index].Named == named {
			return gotreesitter.Symbol(index), true
		}
	}
	return 0, false
}

func assertCollapsedChildRouteTree(t *testing.T, route string, tree *gotreesitter.Tree, lang *gotreesitter.Language, wants map[string]struct {
	child string
	named bool
	count int
}) {
	t.Helper()
	root := tree.RootNode()
	if root == nil || root.HasError() {
		t.Fatalf("%s unclean fixture: %v", route, root)
	}
	if runtime := tree.ParseRuntime(); route == "compact-routed" || route == "forest" {
		if runtime.NormalizationPassesChecked != 0 || runtime.NormalizationPassesRun != 0 || runtime.NormalizationNodesVisited != 0 || runtime.NormalizationNodesRewritten != 0 {
			t.Fatalf("%s normalization checked/run/visited/rewritten=%d/%d/%d/%d", route, runtime.NormalizationPassesChecked, runtime.NormalizationPassesRun, runtime.NormalizationNodesVisited, runtime.NormalizationNodesRewritten)
		}
	} else if runtime.NormalizationPassesChecked == 0 || runtime.NormalizationPassesRun == 0 || runtime.NormalizationNodesVisited == 0 || runtime.NormalizationNodesRewritten != 0 {
		t.Fatalf("%s normalization checked/run/visited/rewritten=%d/%d/%d/%d", route, runtime.NormalizationPassesChecked, runtime.NormalizationPassesRun, runtime.NormalizationNodesVisited, runtime.NormalizationNodesRewritten)
	}
	for parentType, want := range wants {
		parents := collapsedChildRegressionNodes(root, lang, parentType)
		if len(parents) != want.count {
			t.Fatalf("%s %s count = %d, want %d; tree=%s", route, parentType, len(parents), want.count, root.SExpr(lang))
		}
		for _, parent := range parents {
			if parent.ChildCount() != 1 {
				t.Fatalf("%s %s children = %d, want 1", route, parentType, parent.ChildCount())
			}
			child := parent.Child(0)
			if child == nil || child.Type(lang) != want.child || child.IsNamed() != want.named {
				t.Fatalf("%s %s child = %v, want %s named=%t", route, parentType, child, want.child, want.named)
			}
		}
	}
}

func collapsedChildRegressionNodes(root *gotreesitter.Node, lang *gotreesitter.Language, typ string) []*gotreesitter.Node {
	var nodes []*gotreesitter.Node
	var walk func(*gotreesitter.Node)
	walk = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if node.Type(lang) == typ && node.IsNamed() {
			nodes = append(nodes, node)
		}
		for index := 0; index < node.ChildCount(); index++ {
			walk(node.Child(index))
		}
	}
	walk(root)
	return nodes
}
