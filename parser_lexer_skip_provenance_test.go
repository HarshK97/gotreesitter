package gotreesitter

import "testing"

func TestRealTokenAttachmentGapHonorsLexerSkipProvenanceWithIncludedRange(t *testing.T) {
	source := []byte("a\n * b")
	stack := newGLRStack(1)
	stack.byteOffset = 1
	tok := Token{
		Symbol:                  1,
		StartByte:               5,
		EndByte:                 6,
		StartPoint:              Point{Row: 1, Column: 3},
		EndPoint:                Point{Row: 1, Column: 4},
		lexerSkippedPrefix:      true,
		lexerSkippedPrefixStart: 1,
	}
	parser := &Parser{included: []Range{{StartByte: 1, EndByte: uint32(len(source))}}}
	if !parser.guardRealTokenAttachmentGap(source, &stack, tok, "included-range") {
		t.Fatal("guardRealTokenAttachmentGap = false, want true for lexer skip provenance")
	}
	if stack.dead {
		t.Fatal("stack.dead = true, want false for lexer skip provenance")
	}

	badStack := newGLRStack(1)
	badStack.byteOffset = 1
	tok.lexerSkippedPrefixStart = 0
	if (&Parser{}).guardRealTokenAttachmentGap(source, &badStack, tok, "included-range") {
		t.Fatal("guardRealTokenAttachmentGap = true, want false for mismatched skip provenance")
	}
	if !badStack.dead {
		t.Fatal("badStack.dead = false, want true for mismatched skip provenance")
	}
}
