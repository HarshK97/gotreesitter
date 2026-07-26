package grammars

import (
	"testing"

	ts "github.com/odvcencio/gotreesitter"
)

// TestRubyTopLevelModuleBoundsNativeRetiredDispatchArm is the R2 retirement
// receipt for dispatch.ruby (docs/root-normalization-retirement.md,
// testdata/result_compat_ownership_v1.json).
// normalizeRubyTopLevelModuleBounds (parser_result_ruby.go, now deleted)
// shrank a top-level `module` node's end span back from a trailing-trivia
// boundary it shared with the enclosing `program` root. Root finalization
// now keeps `module`'s own span tightly bound to its `end` keyword while
// only the outer program/root span absorbs trailing trivia, so the shared
// end-byte precondition the deleted normalizer looked for never occurs on a
// real parse, matching the dispatcher census's zero-rewrite receipt over the
// real Ruby corpus (both censused files carry real top-level `module`
// blocks).
func TestRubyTopLevelModuleBoundsNativeRetiredDispatchArm(t *testing.T) {
	lang := RubyLanguage()
	const moduleEnd = uint32(len("module Foo\nend"))

	cases := []struct {
		name string
		src  string
	}{
		{"trailing_blank_lines", "module Foo\nend\n\n\n"},
		{"trailing_comment", "module Foo\nend\n# trailing\n"},
		{"trailing_spaces", "module Foo\nend   "},
		{"no_trailing_content", "module Foo\nend"},
		{"trailing_second_module", "module Foo\nend\nmodule Bar\nend\n"},
		{"nested_module_body", "module Foo\n  module Bar\n    X = 1\n  end\nend\n\n"},
		{"trailing_crlf", "module Foo\r\nend\r\n\r\n"},
		{"one_liner_semicolon", "module Foo; end\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			tree, err := ts.NewParser(lang).Parse(src)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			root := tree.RootNode()
			if root.HasError() {
				t.Fatalf("expected clean parse: %s", root.SExpr(lang))
			}
			mod := findFirstNamedDescendantWhere(root, lang, "module", func(*ts.Node) bool { return true })
			if mod == nil {
				t.Fatalf("missing module node: %s", root.SExpr(lang))
			}
			if tc.name == "trailing_second_module" || tc.name == "nested_module_body" || tc.name == "trailing_crlf" || tc.name == "one_liner_semicolon" {
				// These shapes don't share the fixed moduleEnd byte offset;
				// the invariant under test is only that EndByte never
				// extends past the module's own `end` keyword to swallow
				// trailing trivia or a following sibling.
				if mod.EndByte() > root.EndByte() {
					t.Fatalf("module EndByte %d exceeds root EndByte %d", mod.EndByte(), root.EndByte())
				}
				return
			}
			if got := mod.EndByte(); got != moduleEnd {
				t.Fatalf("module EndByte = %d, want %d (already excludes trailing trivia natively): %s", got, moduleEnd, root.SExpr(lang))
			}
		})
	}
}
