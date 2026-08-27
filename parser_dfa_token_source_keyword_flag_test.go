package gotreesitter

import "testing"

// keywordAdoptionLanguage builds a minimal language with a word-shaped main
// lexer (any run of lowercase letters) and a keyword lexer that recognizes
// only the literal "if". Symbols:
//
//	0: EOF
//	1: identifier (terminal, named) — keyword capture token
//	2: if (terminal, anonymous) — keyword matched by the keyword DFA
//
// lookupActionIndex is left nil so promoteKeyword's context-aware gating
// (parser_dfa_token_source.go ~4588-4622) is skipped and every full-length
// keyword match adopts unconditionally, matching this fixture's single,
// context-free parser state.
func keywordAdoptionLanguage() *Language {
	return &Language{
		Name:                "keyword_adoption_test",
		SymbolCount:         3,
		TokenCount:          2,
		KeywordCaptureToken: 1, // identifier
		SymbolNames:         []string{"end", "identifier", "if"},
		LexStates: []LexState{
			// state 0: start of a word — dispatch any lowercase letter
			{Default: -1, EOF: -1, Transitions: []LexTransition{
				{Lo: 'a', Hi: 'z', NextState: 1},
			}},
			// state 1: accept identifier, loop on further lowercase letters
			{AcceptToken: 1, Default: -1, EOF: -1, Transitions: []LexTransition{
				{Lo: 'a', Hi: 'z', NextState: 1},
			}},
		},
		LexModes: []LexMode{{LexState: 0}},
		KeywordLexStates: []LexState{
			// kw state 0: start — dispatch 'i'
			{AcceptToken: 0, Default: -1, EOF: -1, Transitions: []LexTransition{
				{Lo: 'i', Hi: 'i', NextState: 1},
			}},
			// kw state 1: saw 'i' — dispatch 'f'
			{AcceptToken: 0, Default: -1, EOF: -1, Transitions: []LexTransition{
				{Lo: 'f', Hi: 'f', NextState: 2},
			}},
			// kw state 2: saw "if" — accept the keyword (symbol 2)
			{AcceptToken: 2, Default: -1, EOF: -1},
		},
	}
}

// TestPromoteKeywordSetsIsKeywordOnAdoption drives the token source's real
// Next() entry point over a source that is exactly the keyword-lexer's
// literal ("if"). The main lexer produces the keyword-capture (identifier)
// token; the keyword re-lex fully consumes the same span, so the token is
// adopted: its symbol becomes the "if" keyword and IsKeyword records the
// adoption, mirroring C's Subtree.is_keyword (subtree.h:131).
func TestPromoteKeywordSetsIsKeywordOnAdoption(t *testing.T) {
	lang := keywordAdoptionLanguage()
	source := []byte("if")
	ts := acquireDFATokenSource(NewLexer(lang.LexStates, source), lang, nil, nil, nil, nil)
	defer ts.Close()

	tok := ts.Next()
	if tok.Symbol != 2 {
		t.Fatalf("token symbol = %d, want 2 (if keyword — adopted)", tok.Symbol)
	}
	if !tok.IsKeyword {
		t.Fatal("adopted keyword token: IsKeyword = false, want true")
	}
}

// TestPromoteKeywordLeavesIsKeywordFalseForNonKeywordWord drives Next() over
// a word-shaped span the keyword lexer cannot fully match: "ifx" shares the
// keyword's "if" prefix, but the keyword DFA only accepts the exact literal
// "if" (parser_dfa_token_source.go's lexKeywordSource requires the keyword to
// consume the entire captured span). The token stays an identifier and is
// never adopted, so IsKeyword must be false.
func TestPromoteKeywordLeavesIsKeywordFalseForNonKeywordWord(t *testing.T) {
	lang := keywordAdoptionLanguage()
	source := []byte("ifx")
	ts := acquireDFATokenSource(NewLexer(lang.LexStates, source), lang, nil, nil, nil, nil)
	defer ts.Close()

	tok := ts.Next()
	if tok.Symbol != 1 {
		t.Fatalf("token symbol = %d, want 1 (identifier — not adopted)", tok.Symbol)
	}
	if tok.IsKeyword {
		t.Fatal("non-keyword word token: IsKeyword = true, want false")
	}
}

// TestPromoteKeywordIsKeywordFalseWithoutKeywordCapture drives Next() over
// the keyword's own literal spelling ("if") in a language that has no
// keyword-capture token at all. promoteKeyword must return unmodified
// (parser_dfa_token_source.go:4500-4502), so no token from this language is
// ever adopted and IsKeyword stays false.
func TestPromoteKeywordIsKeywordFalseWithoutKeywordCapture(t *testing.T) {
	lang := keywordAdoptionLanguage()
	lang.KeywordCaptureToken = 0
	lang.KeywordLexStates = nil
	source := []byte("if")
	ts := acquireDFATokenSource(NewLexer(lang.LexStates, source), lang, nil, nil, nil, nil)
	defer ts.Close()

	tok := ts.Next()
	if tok.Symbol != 1 {
		t.Fatalf("token symbol = %d, want 1 (identifier — no keyword capture)", tok.Symbol)
	}
	if tok.IsKeyword {
		t.Fatal("language without keyword capture: IsKeyword = true, want false")
	}
}
