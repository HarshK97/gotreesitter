//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestParitySwiftAdjacentGenericClosers(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "nested-generic-call", source: "let v = X<Y<Z>>()\n"},
		{name: "right-shift", source: "let v = x >> y\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			runParityCase(t, parityCase{name: "swift", source: test.source}, test.name, source)
		})
	}
}
