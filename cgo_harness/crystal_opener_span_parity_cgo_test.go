//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestCrystalBraceOpenerLockedCParity(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "inline_hash", source: "x = {1 => 2}\n"},
		{name: "multiline_hash", source: "x = {\n  1 => 2,\n}\n"},
		{name: "inline_named_tuple", source: "x = {a: 1}\n"},
		{name: "multiline_named_tuple", source: "x = {\n  a: 1,\n}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runParityCase(
				t,
				parityCase{name: "crystal"},
				test.name,
				[]byte(test.source),
			)
		})
	}
}
