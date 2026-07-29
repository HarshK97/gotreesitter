//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestDartInheritedFieldProjectionLockedCParity(t *testing.T) {
	runParityCase(
		t,
		parityCase{name: "dart"},
		"switch_expression_body",
		[]byte("int f(Object value) => switch (value) { int n => n, _ => 0 };\n"),
	)
}

func TestElixirInheritedFieldProjectionLockedCParity(t *testing.T) {
	runParityCase(
		t,
		parityCase{name: "elixir"},
		"nested_call_target",
		[]byte("def unquote(f)(x), do: nil\n"),
	)
}
