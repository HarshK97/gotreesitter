//go:build cgo && treesitter_c_parity

package cgoharness

import "testing"

func TestTerminalLeafNativeParity(t *testing.T) {
	source := []byte(`package p

type Number interface {
	~int | ~string
}

func F[T Number](value T) {
	_ = value
	_ = true
}
`)
	runParityCase(t, parityCase{name: "go"}, "terminal-leaf-native", source)
}
