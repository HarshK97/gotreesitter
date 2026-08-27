package gotreesitter

// This file opens the D2 first-leaf token-cache lane
// (spec.derivation-set-equivalence.v1, section 3) with a literal port of
// C's ts_parser__can_reuse_first_leaf (parser.c:472-505 at the locked C
// revision f5afe475). The predicate decides whether a token lexed under one
// parse state's lexer mode is reusable under another state without
// re-lexing. Nothing consumes it yet; the per-epoch token cell and the
// scheduler election land in later tranches of the same lane.

// tokenReuseLeaf carries the leaf facts C reads from a Subtree. A bare
// token cell sets LeafState and ParseState equal; a reused subtree keeps
// them distinct (the first leaf's lex state against the root's parse
// state, per ts_subtree_leaf_parse_state and ts_subtree_parse_state).
type tokenReuseLeaf struct {
	Symbol     Symbol
	LeafState  StateID
	ParseState StateID
	SizeBytes  uint32
	IsKeyword  bool
}

// lexerModeTriple is C's TSLexerMode (parser.h:89-93): exactly the three
// fields the C reuse test compares with memcmp. Go's LexMode carries
// additional derived fields (AfterWhitespaceLexState, LexStateID, and
// friends) that are functions of the same state row; the literal port
// compares only the C triple.
type lexerModeTriple struct {
	lexState          uint16
	externalLexState  uint16
	reservedWordSetID uint16
}

// nonTerminalExtraLexState mirrors C's (uint16_t)-1 sentinel: at the end
// of a non-terminal extra the lexer returns no token, so nothing may be
// reused there (parser.c:483-487).
const nonTerminalExtraLexState = ^uint16(0)

func lexerModeTripleForState(lang *Language, state StateID) (lexerModeTriple, bool) {
	if lang == nil || int(state) >= len(lang.LexModes) {
		return lexerModeTriple{}, false
	}
	mode := lang.LexModes[state]
	return lexerModeTriple{
		lexState:          mode.LexState,
		externalLexState:  mode.ExternalLexState,
		reservedWordSetID: mode.ReservedWordSetID,
	}, true
}

// canReuseFirstLeaf ports ts_parser__can_reuse_first_leaf clause for
// clause; the rule order and every short-circuit match the C source. The
// only addition is a fail-closed false when either state has no lexer-mode
// row, a case C's complete tables cannot present.
func canReuseFirstLeaf(lang *Language, state StateID, leaf tokenReuseLeaf, entry ParseActionEntry) bool {
	currentMode, ok := lexerModeTripleForState(lang, state)
	if !ok {
		return false
	}
	leafMode, ok := lexerModeTripleForState(lang, leaf.LeafState)
	if !ok {
		return false
	}

	// parser.c:487 -- never reuse at the end of a non-terminal extra.
	if currentMode.lexState == nonTerminalExtraLexState {
		return false
	}

	// parser.c:490-497 -- a token created under the same lookahead set is
	// reusable, unless it is the keyword-capture token and either was
	// adopted as a keyword or came from a different parse state.
	if len(entry.Actions) > 0 && leafMode == currentMode &&
		(leaf.Symbol != lang.KeywordCaptureToken ||
			(!leaf.IsKeyword && leaf.ParseState == state)) {
		return true
	}

	// parser.c:500 -- zero-width tokens other than end-of-file never cross
	// into a different lookahead set.
	if leaf.SizeBytes == 0 && leaf.Symbol != 0 {
		return false
	}

	// parser.c:504 -- otherwise reuse requires a lex context with no
	// external scanner and the table entry's reusable bit.
	return currentMode.externalLexState == 0 && entry.Reusable
}
