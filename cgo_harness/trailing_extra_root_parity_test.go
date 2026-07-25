//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestCleanTrailingExtraRootParity(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{name: "rst", source: "Title\n=====\n\nbody\n"},
		{name: "comment", source: "hello\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			runParityCase(t, parityCase{name: test.name}, "clean-trailing-extra-root", []byte(test.source))
		})
	}
}
