//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestParityJavaScriptStandaloneBlockBeforeSimpleAssignment(t *testing.T) {
	src := []byte("{a}b=c")
	runParityCase(t, parityCase{name: "javascript", source: string(src)}, "issue111", src)
}

func TestParityJavaScriptAutomaticSemicolonBeforeTrailingComment(t *testing.T) {
	src := []byte("function f() {\n  if (!size || !pre) { i--; break; } // counter\n  var curr = 1;\n}\n")
	runParityCase(t, parityCase{name: "javascript", source: string(src)}, "javascript-asi-before-trailing-comment", src)
}

func TestParityJavaScriptAdjacentBlockCommentsDoNotConsumeFollowingToken(t *testing.T) {
	src := []byte("if (x) {} /* first *//* second */ else {}\n")
	runParityCase(t, parityCase{name: "javascript", source: string(src)}, "javascript-adjacent-block-comments-before-else", src)
}
