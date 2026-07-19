//go:build !grammar_subset || grammar_subset_fsharp

package grammars

import (
	"reflect"
	"strings"
	"testing"
	_ "unsafe"

	"github.com/odvcencio/gotreesitter"
)

func TestFsharpKeywordDedentFallbackIgnoresEmptyIndentStack(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "then", src: "then"},
		{name: "and", src: "and "},
		{name: "with", src: "with "},
		{name: "else", src: "else"},
		{name: "elif", src: "elif"},
		{name: "end", src: "end"},
	}

	scanner := FsharpExternalScanner{}
	valid := make([]bool, fsTokErrorSentinel+1)
	valid[fsTokDedent] = true
	initialIndents := [][]uint16{nil, []uint16{0}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, initial := range initialIndents {
				want := append([]uint16(nil), initial...)
				if initial == nil {
					want = nil
				}
				state := &fsState{indents: initial}
				lexer := newFsharpExternalLexer([]byte(tt.src), 0, 0, 0)
				if scanner.Scan(state, lexer, valid) {
					t.Fatalf("Scan() emitted DEDENT with indent stack %#v", initial)
				}
				if tok, ok := fsharpExternalLexerToken(lexer); ok {
					t.Fatalf("Scan() returned false but produced token %+v", tok)
				}
				if !reflect.DeepEqual(state.indents, want) {
					t.Fatalf("indents = %#v, want %#v", state.indents, want)
				}
			}
		})
	}
}

// TestFsharpScannerSerializeDeserializeRoundTripLargeIndent guards against
// checkpoint truncation of 16-bit indentation levels to a single byte.
// indents/preprocessorIndents are []uint16 because F# indentation is a
// column count that can exceed 255 (deeply nested code, wide alignment,
// generated sources). A checkpoint round-trip through Serialize/Deserialize
// must reproduce every level exactly, not just its low 8 bits.
func TestFsharpScannerSerializeDeserializeRoundTripLargeIndent(t *testing.T) {
	scanner := FsharpExternalScanner{}
	original := &fsState{
		indents:             []uint16{0, 4, 256, 260, 1000, 65535},
		preprocessorIndents: []uint16{255, 300, 512},
	}

	buf := make([]byte, 4096)
	n := scanner.Serialize(original, buf)
	if n <= 0 {
		t.Fatalf("Serialize() returned n=%d, want > 0", n)
	}

	restored := &fsState{}
	scanner.Deserialize(restored, buf[:n])

	if !reflect.DeepEqual(restored.indents, original.indents) {
		t.Fatalf("indents after checkpoint round-trip = %#v, want %#v (levels above 255 must not be truncated to a byte)", restored.indents, original.indents)
	}
	if !reflect.DeepEqual(restored.preprocessorIndents, original.preprocessorIndents) {
		t.Fatalf("preprocessorIndents after checkpoint round-trip = %#v, want %#v", restored.preprocessorIndents, original.preprocessorIndents)
	}
}

// TestFsharpScannerCheckpointRoundTripPreservesDeepIndentDedent exercises the
// same truncation bug end-to-end: a scanner state checkpointed with an
// indent level above 255, restored, and then driven through Scan must still
// compare the *real* indent level (not its truncated low byte) when deciding
// whether to emit DEDENT. Before the fix, indents=[0,300] serialized and
// deserialized as [0,44] (300 truncated mod 256), so a line indented to
// column 100 would incorrectly look more indented than the (corrupted)
// current level and no DEDENT would be produced.
func TestFsharpScannerCheckpointRoundTripPreservesDeepIndentDedent(t *testing.T) {
	scanner := FsharpExternalScanner{}

	original := &fsState{indents: []uint16{0, 300}}
	buf := make([]byte, 4096)
	n := scanner.Serialize(original, buf)
	if n <= 0 {
		t.Fatalf("Serialize() returned n=%d, want > 0", n)
	}

	restored := &fsState{}
	scanner.Deserialize(restored, buf[:n])
	if !reflect.DeepEqual(restored.indents, original.indents) {
		t.Fatalf("restored indents = %#v, want %#v", restored.indents, original.indents)
	}

	valid := make([]bool, fsTokErrorSentinel+1)
	valid[fsTokNewline] = true
	valid[fsTokDedent] = true

	// A newline followed by 100 columns of indentation: less than the
	// restored level of 300, so a DEDENT must be emitted.
	src := "\n" + strings.Repeat(" ", 100) + "x"
	lexer := newFsharpExternalLexer([]byte(src), 0, 0, 0)
	if !scanner.Scan(restored, lexer, valid) {
		t.Fatalf("Scan() returned false; expected DEDENT when unwinding from a restored indent level of 300 to a line indented 100")
	}
	tok, ok := fsharpExternalLexerToken(lexer)
	if !ok {
		t.Fatalf("Scan() reported success but produced no token")
	}
	if tok.Symbol != fsSymDedent {
		t.Fatalf("Scan() emitted symbol %d, want DEDENT (%d) — indicates the restored indent level was corrupted by checkpoint truncation", tok.Symbol, fsSymDedent)
	}
	wantIndents := []uint16{0}
	if !reflect.DeepEqual(restored.indents, wantIndents) {
		t.Fatalf("indents after DEDENT = %#v, want %#v", restored.indents, wantIndents)
	}
}

//go:linkname newFsharpExternalLexer github.com/odvcencio/gotreesitter.newExternalLexer
func newFsharpExternalLexer(source []byte, pos int, row, col uint32) *gotreesitter.ExternalLexer

//go:linkname fsharpExternalLexerToken github.com/odvcencio/gotreesitter.(*ExternalLexer).token
func fsharpExternalLexerToken(*gotreesitter.ExternalLexer) (gotreesitter.Token, bool)
