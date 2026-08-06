//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestParitySwiftOptionalBindingActionSelection(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "if-let", source: "if let x = y {\n}\n"},
		{name: "if-var", source: "if var x = y {\n}\n"},
		{name: "trailing-closure", source: "let value = transform(y) { item in item }\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			runParityCase(t, parityCase{name: "swift", source: test.source}, test.name, source)
		})
	}
}
