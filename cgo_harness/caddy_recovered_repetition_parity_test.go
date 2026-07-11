//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParityCaddyRecoveredStringLiteralRepetition(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "testdata", "caddy", "security-header.Caddyfile"))
	if err != nil {
		t.Fatalf("read Caddy witness: %v", err)
	}
	runParityCase(t, parityCase{name: "caddy"}, "recovered-string-literal-repetition", source)
}
