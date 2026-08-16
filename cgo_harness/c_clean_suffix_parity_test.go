//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"
)

func TestCInitializerAfterTransientRecoveryMatchesC(t *testing.T) {
	var source strings.Builder
	source.WriteString("static const element_t table[] = {\n")
	for range 40 {
		source.WriteString("  {{ 0x1, 0x2, 0x3, 0x4, }},\n")
	}
	source.WriteString("};\n")

	runParityCase(t, parityCase{name: "c"}, "initializer-after-transient-recovery", []byte(source.String()))
}
