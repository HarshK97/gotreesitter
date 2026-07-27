package grammars

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func TestFieldProjectionNativeRetiredDispatchArms(t *testing.T) {
	tests := fieldProjectionRetirementCases()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			tree, err := gotreesitter.NewParser(test.language).ParseNoResultCompatibilityBenchmarkOnly(test.source)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(tree.Release)
			test.assert(t, tree.RootNode(), test.language)
		})
	}
}

func TestFieldProjectionRetiredDispatchArmRoutes(t *testing.T) {
	tests := fieldProjectionRetirementCases()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			for _, receipt := range retiredDispatchRouteReceipts(t, test.language, test.source) {
				t.Run(receipt.name, func(t *testing.T) {
					test.assert(t, receipt.tree.RootNode(), test.language)
					if receipt.name == "incremental" {
						profile := receipt.incrementalProfile
						if test.reuseUnsupportedReason != "" {
							if !profile.ReuseUnsupported || profile.ReuseUnsupportedReason != test.reuseUnsupportedReason {
								t.Fatalf("incremental reuse status = %+v", profile)
							}
						} else if !profile.OldTreeReuseRoute || profile.ReusedSubtrees == 0 || profile.ReusedBytes == 0 {
							t.Fatalf("incremental route did not reuse the old tree: %+v", profile)
						}
					}
				})
			}
		})
	}
}

type fieldProjectionRetirementCase struct {
	name     string
	source   []byte
	language *gotreesitter.Language
	assert   func(*testing.T, *gotreesitter.Node, *gotreesitter.Language)
	// reuseUnsupportedReason records a locked grammar limitation.
	reuseUnsupportedReason string
}

func fieldProjectionRetirementCases() []fieldProjectionRetirementCase {
	return []fieldProjectionRetirementCase{
		{
			name:                   "lua_local_declarations",
			source:                 []byte("local function foo() end\nlocal x = 1"),
			language:               LuaLanguage(),
			assert:                 assertLuaLocalDeclarationFields,
			reuseUnsupportedReason: "external_scanner_unsupported",
		},
		{
			name:     "make_conditional_consequence",
			source:   []byte("ifneq (a,b)\n\t@echo x\nelse\n\t@echo y\nendif"),
			language: MakeLanguage(),
			assert:   assertMakeConsequenceFields,
		},
		{
			name:     "zig_initializer_list",
			source:   []byte("const x = .{};\nconst y = .{\"x\"};"),
			language: ZigLanguage(),
			assert:   assertZigInitializerListFields,
		},
	}
}

func assertLuaLocalDeclarationFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	if root == nil || root.Type(lang) != "chunk" || root.ChildCount() != 2 {
		t.Fatalf("unexpected Lua root: %v", root)
	}
	for index := 0; index < root.ChildCount(); index++ {
		if got := root.FieldNameForChild(index, lang); got != "local_declaration" {
			t.Fatalf("Lua child %d field = %q, want local_declaration", index, got)
		}
	}
}

func assertMakeConsequenceFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	conditional := findFirstNamedDescendantWhere(root, lang, "conditional", func(*gotreesitter.Node) bool { return true })
	if conditional == nil || conditional.ChildCount() < 4 {
		t.Fatalf("missing Make conditional: %s", root.SExpr(lang))
	}
	assertFieldName(t, conditional, lang, 1, "\t", "consequence")
	assertFieldName(t, conditional, lang, 2, "recipe_line", "consequence")

	elseDirective := conditional.Child(3)
	if elseDirective == nil || elseDirective.Type(lang) != "else_directive" || elseDirective.ChildCount() < 3 {
		t.Fatalf("missing Make else directive: %s", root.SExpr(lang))
	}
	assertFieldName(t, elseDirective, lang, 1, "\t", "consequence")
	assertFieldName(t, elseDirective, lang, 2, "recipe_line", "consequence")
}

func assertZigInitializerListFields(t *testing.T, root *gotreesitter.Node, lang *gotreesitter.Language) {
	t.Helper()
	count := 0
	var visit func(*gotreesitter.Node)
	visit = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		if node.Type(lang) == "anonymous_struct_initializer" {
			count++
			assertFieldName(t, node, lang, 1, "initializer_list", "")
		}
		for index := 0; index < node.ChildCount(); index++ {
			visit(node.Child(index))
		}
	}
	visit(root)
	if count != 2 {
		t.Fatalf("Zig initializer count = %d, want 2: %s", count, root.SExpr(lang))
	}
}

func assertFieldName(
	t *testing.T,
	parent *gotreesitter.Node,
	lang *gotreesitter.Language,
	index int,
	wantType string,
	wantField string,
) {
	t.Helper()
	child := parent.Child(index)
	if child == nil || child.Type(lang) != wantType {
		t.Fatalf("child %d type = %v, want %s", index, child, wantType)
	}
	if got := parent.FieldNameForChild(index, lang); got != wantField {
		t.Fatalf("child %d field = %q, want %q", index, got, wantField)
	}
}
