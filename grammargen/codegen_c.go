package grammargen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/gotreesitter"
)

// GenerateC compiles a Grammar to a standard tree-sitter parser.c string.
// The output is compatible with tree-sitter's C runtime ABI 14/15 features
// that grammargen currently emits.
func GenerateC(g *Grammar) (string, error) {
	lang, err := GenerateLanguage(g)
	if err != nil {
		return "", fmt.Errorf("generate language: %w", err)
	}
	return EmitC(g.Name, lang)
}

// EmitC emits a parser.c string from a compiled Language struct.
func EmitC(name string, lang *gotreesitter.Language) (string, error) {
	if err := validateCSupertypeSurface(lang); err != nil {
		return "", err
	}
	parseActionOffsets, err := cParseActionOffsets(lang)
	if err != nil {
		return "", err
	}
	var b strings.Builder

	emitHeader(&b, name, lang)
	emitSymbolEnum(&b, lang)
	emitFieldEnum(&b, lang)
	emitSymbolNames(&b, lang)
	emitSymbolMetadata(&b, lang)
	emitFieldNames(&b, lang)
	emitFieldMaps(&b, lang)
	emitAliasSequences(&b, lang)
	emitParseActions(&b, lang)
	emitParseTable(&b, lang, parseActionOffsets)
	emitSmallParseTable(&b, lang, parseActionOffsets)
	emitLexModes(&b, lang)
	emitReservedWords(&b, lang)
	emitSupertypes(&b, lang)
	emitLexFunction(&b, "ts_lex", lang.LexStates, lang)
	if len(lang.KeywordLexStates) > 0 {
		emitLexFunction(&b, "ts_lex_keywords", lang.KeywordLexStates, lang)
	}
	if len(lang.ExternalSymbols) > 0 {
		emitExternalScanner(&b, lang)
	}
	emitLanguageExport(&b, name, lang)

	return b.String(), nil
}

func cParseActionOffsets(lang *gotreesitter.Language) ([]uint16, error) {
	offsets := make([]uint16, len(lang.ParseActions))
	offset := 0
	for i, entry := range lang.ParseActions {
		if offset > int(^uint16(0)) {
			return nil, fmt.Errorf("emit C: flattened parse actions exceed uint16 at group %d", i)
		}
		offsets[i] = uint16(offset)
		offset += 1 + len(entry.Actions)
	}
	return offsets, nil
}

func cParseTableValue(lang *gotreesitter.Language, actionOffsets []uint16, symbol int, value uint16) uint16 {
	if symbol >= int(lang.TokenCount) || value == 0 {
		return value
	}
	if int(value) >= len(actionOffsets) {
		return value
	}
	return actionOffsets[value]
}

func validateCSupertypeSurface(lang *gotreesitter.Language) error {
	if lang == nil {
		return fmt.Errorf("emit C: nil language")
	}
	hasSymbols := len(lang.SupertypeSymbols) > 0
	hasSlices := len(lang.SupertypeMapSlices) > 0
	hasEntries := len(lang.SupertypeMapEntries) > 0
	if !hasSymbols && !hasSlices && !hasEntries {
		return nil
	}
	if lang.LanguageVersion < 15 {
		return fmt.Errorf("emit C: supertype map requires ABI 15, got %d", lang.LanguageVersion)
	}
	if !hasSymbols || !hasSlices || !hasEntries {
		return fmt.Errorf("emit C: incomplete ABI-15 supertype surface")
	}
	if len(lang.SupertypeMapSlices) < int(lang.SymbolCount) {
		return fmt.Errorf("emit C: supertype map has %d slices for %d symbols", len(lang.SupertypeMapSlices), lang.SymbolCount)
	}
	for _, supertype := range lang.SupertypeSymbols {
		if int(supertype) >= len(lang.SymbolNames) || int(supertype) >= len(lang.SymbolMetadata) {
			return fmt.Errorf("emit C: supertype symbol %d outside symbol tables", supertype)
		}
		if !lang.SymbolMetadata[supertype].Supertype {
			return fmt.Errorf("emit C: symbol %d is in the supertype table without supertype metadata", supertype)
		}
		slice := lang.SupertypeMapSlices[supertype]
		end := int(slice[0]) + int(slice[1])
		if slice[1] == 0 || end > len(lang.SupertypeMapEntries) {
			return fmt.Errorf("emit C: invalid supertype slice for symbol %d: index=%d length=%d entries=%d", supertype, slice[0], slice[1], len(lang.SupertypeMapEntries))
		}
	}
	for _, subtype := range lang.SupertypeMapEntries {
		if int(subtype) >= len(lang.SymbolNames) {
			return fmt.Errorf("emit C: subtype symbol %d outside symbol names", subtype)
		}
	}
	return nil
}

func emitHeader(b *strings.Builder, name string, lang *gotreesitter.Language) {
	fmt.Fprintf(b, "#include <tree_sitter/parser.h>\n\n")
	fmt.Fprintf(b, "#if defined(__GNUC__) || defined(__clang__)\n")
	fmt.Fprintf(b, "#pragma GCC diagnostic push\n")
	fmt.Fprintf(b, "#pragma GCC diagnostic ignored \"-Wmissing-field-initializers\"\n")
	fmt.Fprintf(b, "#endif\n\n")

	maxAliasLen := 0
	for _, row := range lang.AliasSequences {
		if len(row) > maxAliasLen {
			maxAliasLen = len(row)
		}
	}

	fmt.Fprintf(b, "#define LANGUAGE_VERSION %d\n", lang.LanguageVersion)
	fmt.Fprintf(b, "#define STATE_COUNT %d\n", lang.StateCount)
	fmt.Fprintf(b, "#define LARGE_STATE_COUNT %d\n", lang.LargeStateCount)
	fmt.Fprintf(b, "#define SYMBOL_COUNT %d\n", lang.SymbolCount)
	fmt.Fprintf(b, "#define ALIAS_COUNT 0\n")
	fmt.Fprintf(b, "#define TOKEN_COUNT %d\n", lang.TokenCount)
	fmt.Fprintf(b, "#define EXTERNAL_TOKEN_COUNT %d\n", lang.ExternalTokenCount)
	fmt.Fprintf(b, "#define FIELD_COUNT %d\n", lang.FieldCount)
	if len(lang.SupertypeSymbols) > 0 {
		fmt.Fprintf(b, "#define SUPERTYPE_COUNT %d\n", len(lang.SupertypeSymbols))
	}
	if lang.MaxReservedWordSetSize > 0 {
		fmt.Fprintf(b, "#define MAX_RESERVED_WORD_SET_SIZE %d\n", lang.MaxReservedWordSetSize)
	}
	fmt.Fprintf(b, "#define MAX_ALIAS_SEQUENCE_LENGTH %d\n", maxAliasLen)
	fmt.Fprintf(b, "#define PRODUCTION_ID_COUNT %d\n\n", lang.ProductionIDCount)
}

func emitSymbolEnum(b *strings.Builder, lang *gotreesitter.Language) {
	fmt.Fprintf(b, "enum ts_symbol_identifiers {\n")
	for i, name := range lang.SymbolNames {
		if i == 0 {
			continue // ts_builtin_sym_end is implicit
		}
		cname := symbolToCName(name, i, lang)
		fmt.Fprintf(b, "  %s = %d,\n", cname, i)
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitFieldEnum(b *strings.Builder, lang *gotreesitter.Language) {
	if lang.FieldCount <= 1 {
		return
	}
	fmt.Fprintf(b, "enum ts_field_identifiers {\n")
	for i, name := range lang.FieldNames {
		if i == 0 || name == "" {
			continue
		}
		fmt.Fprintf(b, "  field_%s = %d,\n", name, i)
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitSymbolNames(b *strings.Builder, lang *gotreesitter.Language) {
	fmt.Fprintf(b, "static const char * const ts_symbol_names[] = {\n")
	for i, name := range lang.SymbolNames {
		cname := symbolToCName(name, i, lang)
		fmt.Fprintf(b, "  [%s] = %q,\n", cname, name)
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitSymbolMetadata(b *strings.Builder, lang *gotreesitter.Language) {
	fmt.Fprintf(b, "static const TSSymbolMetadata ts_symbol_metadata[] = {\n")
	for i, meta := range lang.SymbolMetadata {
		cname := symbolToCName(lang.SymbolNames[i], i, lang)
		fmt.Fprintf(b, "  [%s] = {\n", cname)
		fmt.Fprintf(b, "    .visible = %s,\n", boolStr(meta.Visible))
		fmt.Fprintf(b, "    .named = %s,\n", boolStr(meta.Named))
		if meta.Supertype {
			fmt.Fprintf(b, "    .supertype = true,\n")
		}
		fmt.Fprintf(b, "  },\n")
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitFieldNames(b *strings.Builder, lang *gotreesitter.Language) {
	if lang.FieldCount <= 1 {
		return
	}
	fmt.Fprintf(b, "static const char * const ts_field_names[] = {\n")
	for i, name := range lang.FieldNames {
		if name == "" {
			fmt.Fprintf(b, "  [%d] = NULL,\n", i)
		} else {
			fmt.Fprintf(b, "  [%d] = %q,\n", i, name)
		}
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitFieldMaps(b *strings.Builder, lang *gotreesitter.Language) {
	if len(lang.FieldMapEntries) == 0 {
		return
	}

	fmt.Fprintf(b, "static const TSFieldMapSlice ts_field_map_slices[PRODUCTION_ID_COUNT] = {\n")
	for i, slice := range lang.FieldMapSlices {
		if slice[0] != 0 || slice[1] != 0 {
			fmt.Fprintf(b, "  [%d] = {.index = %d, .length = %d},\n", i, slice[0], slice[1])
		}
	}
	fmt.Fprintf(b, "};\n\n")

	fmt.Fprintf(b, "static const TSFieldMapEntry ts_field_map_entries[] = {\n")
	for i, entry := range lang.FieldMapEntries {
		inherited := boolStr(entry.Inherited)
		fmt.Fprintf(b, "  [%d] = {.field_id = %d, .child_index = %d, .inherited = %s},\n",
			i, entry.FieldID, entry.ChildIndex, inherited)
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitAliasSequences(b *strings.Builder, lang *gotreesitter.Language) {
	if len(lang.AliasSequences) == 0 {
		return
	}

	maxLen := 0
	for _, row := range lang.AliasSequences {
		if len(row) > maxLen {
			maxLen = len(row)
		}
	}
	if maxLen == 0 {
		return
	}

	fmt.Fprintf(b, "static const TSSymbol ts_alias_sequences[PRODUCTION_ID_COUNT][MAX_ALIAS_SEQUENCE_LENGTH] = {\n")
	for i, row := range lang.AliasSequences {
		if len(row) == 0 {
			continue
		}
		hasNonZero := false
		for _, sym := range row {
			if sym != 0 {
				hasNonZero = true
				break
			}
		}
		if !hasNonZero {
			continue
		}
		fmt.Fprintf(b, "  [%d] = {\n", i)
		for j, sym := range row {
			if sym != 0 {
				cname := symbolToCName(lang.SymbolNames[sym], int(sym), lang)
				fmt.Fprintf(b, "    [%d] = %s,\n", j, cname)
			}
		}
		fmt.Fprintf(b, "  },\n")
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitParseActions(b *strings.Builder, lang *gotreesitter.Language) {
	fmt.Fprintf(b, "static const TSParseActionEntry ts_parse_actions[] = {\n")
	idx := 0
	for _, entry := range lang.ParseActions {
		fmt.Fprintf(b, "  [%d] = {.entry = {.count = %d, .reusable = %s}},",
			idx, len(entry.Actions), boolStr(entry.Reusable))
		idx++
		for _, action := range entry.Actions {
			switch action.Type {
			case gotreesitter.ParseActionShift:
				if action.Extra {
					fmt.Fprintf(b, " SHIFT_EXTRA(),")
				} else if action.Repetition {
					fmt.Fprintf(b, " SHIFT_REPEAT(%d),", action.State)
				} else {
					fmt.Fprintf(b, " SHIFT(%d),", action.State)
				}
			case gotreesitter.ParseActionReduce:
				cname := symbolToCName(lang.SymbolNames[action.Symbol], int(action.Symbol), lang)
				fmt.Fprintf(b, " REDUCE(%s, %d, %d, %d),",
					cname, action.ChildCount, action.DynamicPrecedence, action.ProductionID)
			case gotreesitter.ParseActionAccept:
				fmt.Fprintf(b, " ACCEPT_INPUT(),")
			case gotreesitter.ParseActionRecover:
				fmt.Fprintf(b, " RECOVER(),")
			}
			idx++
		}
		fmt.Fprintf(b, "\n")
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitParseTable(b *strings.Builder, lang *gotreesitter.Language, actionOffsets []uint16) {
	if lang.LargeStateCount == 0 || len(lang.ParseTable) == 0 {
		return
	}
	fmt.Fprintf(b, "static const uint16_t ts_parse_table[LARGE_STATE_COUNT][SYMBOL_COUNT] = {\n")
	for i, row := range lang.ParseTable {
		if i >= int(lang.LargeStateCount) {
			break
		}
		fmt.Fprintf(b, "  [%d] = {\n", i)
		for j, val := range row {
			if val == 0 {
				continue
			}
			cname := symbolToCName(lang.SymbolNames[j], j, lang)
			fmt.Fprintf(b, "    [%s] = %d,\n", cname, cParseTableValue(lang, actionOffsets, j, val))
		}
		fmt.Fprintf(b, "  },\n")
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitSmallParseTable(b *strings.Builder, lang *gotreesitter.Language, actionOffsets []uint16) {
	if len(lang.SmallParseTable) == 0 {
		return
	}
	type smallState struct {
		offset uint32
		data   []uint16
	}
	states := make([]smallState, len(lang.SmallParseTableMap))
	for state, sourceOffset := range lang.SmallParseTableMap {
		pos := int(sourceOffset)
		if pos >= len(lang.SmallParseTable) {
			continue
		}
		groupCount := int(lang.SmallParseTable[pos])
		pos++
		groups := make(map[uint16][]uint16)
		for group := 0; group < groupCount && pos+1 < len(lang.SmallParseTable); group++ {
			value := lang.SmallParseTable[pos]
			count := int(lang.SmallParseTable[pos+1])
			pos += 2
			for i := 0; i < count && pos < len(lang.SmallParseTable); i++ {
				symbol := lang.SmallParseTable[pos]
				pos++
				mapped := cParseTableValue(lang, actionOffsets, int(symbol), value)
				groups[mapped] = append(groups[mapped], symbol)
			}
		}
		values := make([]int, 0, len(groups))
		for value := range groups {
			values = append(values, int(value))
		}
		sort.Ints(values)
		data := []uint16{uint16(len(values))}
		for _, rawValue := range values {
			value := uint16(rawValue)
			syms := groups[value]
			sort.Slice(syms, func(i, j int) bool { return syms[i] < syms[j] })
			data = append(data, value, uint16(len(syms)))
			data = append(data, syms...)
		}
		states[state].data = data
	}
	fmt.Fprintf(b, "static const uint16_t ts_small_parse_table[] = {\n")
	offset := uint32(0)
	for i := range states {
		states[i].offset = offset
		for _, val := range states[i].data {
			fmt.Fprintf(b, "  /* %d */ %d,\n", offset, val)
			offset++
		}
	}
	fmt.Fprintf(b, "};\n\n")

	fmt.Fprintf(b, "static const uint32_t ts_small_parse_table_map[] = {\n")
	for i, state := range states {
		fmt.Fprintf(b, "  [SMALL_STATE(%d)] = %d,\n", int(lang.LargeStateCount)+i, state.offset)
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitLexModes(b *strings.Builder, lang *gotreesitter.Language) {
	modeType := "TSLexMode"
	if lang.LanguageVersion >= 15 {
		modeType = "TSLexerMode"
	}
	fmt.Fprintf(b, "static const %s ts_lex_modes[STATE_COUNT] = {\n", modeType)
	for i, mode := range lang.LexModes {
		parts := []string{fmt.Sprintf(".lex_state = %d", mode.LexState)}
		if mode.ExternalLexState > 0 {
			parts = append(parts, fmt.Sprintf(".external_lex_state = %d", mode.ExternalLexState))
		}
		if lang.LanguageVersion >= 15 && mode.ReservedWordSetID > 0 {
			parts = append(parts, fmt.Sprintf(".reserved_word_set_id = %d", mode.ReservedWordSetID))
		}
		fmt.Fprintf(b, "  [%d] = {%s},\n", i, strings.Join(parts, ", "))
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitReservedWords(b *strings.Builder, lang *gotreesitter.Language) {
	if lang.MaxReservedWordSetSize == 0 || len(lang.ReservedWords) == 0 {
		return
	}

	stride := int(lang.MaxReservedWordSetSize)
	rowCount := (len(lang.ReservedWords) + stride - 1) / stride
	fmt.Fprintf(b, "static const TSSymbol ts_reserved_words[%d][MAX_RESERVED_WORD_SET_SIZE] = {\n", rowCount)
	for row := 0; row < rowCount; row++ {
		fmt.Fprintf(b, "  [%d] = {\n", row)
		start := row * stride
		end := start + stride
		if end > len(lang.ReservedWords) {
			end = len(lang.ReservedWords)
		}
		for _, sym := range lang.ReservedWords[start:end] {
			if sym == 0 {
				break
			}
			cname := symbolToCName(lang.SymbolNames[sym], int(sym), lang)
			fmt.Fprintf(b, "    %s,\n", cname)
		}
		fmt.Fprintf(b, "  },\n")
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitSupertypes(b *strings.Builder, lang *gotreesitter.Language) {
	if len(lang.SupertypeSymbols) == 0 || len(lang.SupertypeMapEntries) == 0 {
		return
	}

	fmt.Fprintf(b, "static const TSSymbol ts_supertype_symbols[SUPERTYPE_COUNT] = {\n")
	for i, sym := range lang.SupertypeSymbols {
		if int(sym) >= len(lang.SymbolNames) {
			continue
		}
		fmt.Fprintf(b, "  [%d] = %s,\n", i, symbolToCName(lang.SymbolNames[sym], int(sym), lang))
	}
	fmt.Fprintf(b, "};\n\n")

	fmt.Fprintf(b, "static const TSMapSlice ts_supertype_map_slices[SYMBOL_COUNT] = {\n")
	for sym, slice := range lang.SupertypeMapSlices {
		if slice == [2]uint16{} || sym >= len(lang.SymbolNames) {
			continue
		}
		fmt.Fprintf(b, "  [%s] = {.index = %d, .length = %d},\n",
			symbolToCName(lang.SymbolNames[sym], sym, lang), slice[0], slice[1])
	}
	fmt.Fprintf(b, "};\n\n")

	fmt.Fprintf(b, "static const TSSymbol ts_supertype_map_entries[] = {\n")
	for _, sym := range lang.SupertypeMapEntries {
		if int(sym) >= len(lang.SymbolNames) {
			continue
		}
		fmt.Fprintf(b, "  %s,\n", symbolToCName(lang.SymbolNames[sym], int(sym), lang))
	}
	fmt.Fprintf(b, "};\n\n")
}

func emitLexFunction(b *strings.Builder, funcName string, states []gotreesitter.LexState, lang *gotreesitter.Language) {
	fmt.Fprintf(b, "static bool %s(TSLexer *lexer, TSStateId state) {\n", funcName)
	fmt.Fprintf(b, "  START_LEXER();\n")
	fmt.Fprintf(b, "  eof = lexer->eof(lexer);\n")
	fmt.Fprintf(b, "  switch (state) {\n")

	for i, st := range states {
		fmt.Fprintf(b, "    case %d:\n", i)

		// Accept token.
		if st.AcceptToken > 0 {
			cname := symbolToCName(lang.SymbolNames[st.AcceptToken], int(st.AcceptToken), lang)
			fmt.Fprintf(b, "      ACCEPT_TOKEN(%s);\n", cname)
		}
		if st.Skip {
			fmt.Fprintf(b, "      ACCEPT_TOKEN(ts_builtin_sym_end); /* skip */\n")
		}

		// EOF transition.
		if st.EOF >= 0 {
			fmt.Fprintf(b, "      if (eof) ADVANCE(%d);\n", st.EOF)
		}

		// Character transitions.
		for _, tr := range st.Transitions {
			cond := charCondition(tr.Lo, tr.Hi)
			action := "ADVANCE"
			if tr.Skip {
				action = "SKIP"
			}
			fmt.Fprintf(b, "      if (%s) %s(%d);\n", cond, action, tr.NextState)
		}

		// Default transition.
		if st.Default >= 0 {
			fmt.Fprintf(b, "      ADVANCE(%d);\n", st.Default)
		}

		fmt.Fprintf(b, "      END_STATE();\n")
	}

	fmt.Fprintf(b, "    default:\n")
	fmt.Fprintf(b, "      return false;\n")
	fmt.Fprintf(b, "  }\n")
	fmt.Fprintf(b, "}\n\n")
}

func emitExternalScanner(b *strings.Builder, lang *gotreesitter.Language) {
	// External scanner symbol map.
	fmt.Fprintf(b, "static const uint16_t ts_external_scanner_symbol_map[EXTERNAL_TOKEN_COUNT] = {\n")
	for i, sym := range lang.ExternalSymbols {
		cname := symbolToCName(lang.SymbolNames[sym], int(sym), lang)
		fmt.Fprintf(b, "  [%d] = %s,\n", i, cname)
	}
	fmt.Fprintf(b, "};\n\n")

	// External scanner states (validity table).
	if len(lang.ExternalLexStates) > 0 {
		fmt.Fprintf(b, "static const bool ts_external_scanner_states[%d][EXTERNAL_TOKEN_COUNT] = {\n",
			len(lang.ExternalLexStates))
		for i, row := range lang.ExternalLexStates {
			fmt.Fprintf(b, "  [%d] = {", i)
			for j, valid := range row {
				if j > 0 {
					fmt.Fprintf(b, ", ")
				}
				fmt.Fprintf(b, "%s", boolStr(valid))
			}
			fmt.Fprintf(b, "},\n")
		}
		fmt.Fprintf(b, "};\n\n")
	}
}

func emitLanguageExport(b *strings.Builder, name string, lang *gotreesitter.Language) {
	funcName := "tree_sitter_" + name

	fmt.Fprintf(b, "const TSLanguage *%s(void) {\n", funcName)
	fmt.Fprintf(b, "  static const TSLanguage language = {\n")
	fmt.Fprintf(b, "    .abi_version = LANGUAGE_VERSION,\n")
	fmt.Fprintf(b, "    .symbol_count = SYMBOL_COUNT,\n")
	fmt.Fprintf(b, "    .alias_count = ALIAS_COUNT,\n")
	fmt.Fprintf(b, "    .token_count = TOKEN_COUNT,\n")
	fmt.Fprintf(b, "    .external_token_count = EXTERNAL_TOKEN_COUNT,\n")
	fmt.Fprintf(b, "    .state_count = STATE_COUNT,\n")
	fmt.Fprintf(b, "    .large_state_count = LARGE_STATE_COUNT,\n")
	fmt.Fprintf(b, "    .production_id_count = PRODUCTION_ID_COUNT,\n")
	fmt.Fprintf(b, "    .field_count = FIELD_COUNT,\n")
	fmt.Fprintf(b, "    .max_alias_sequence_length = MAX_ALIAS_SEQUENCE_LENGTH,\n")
	fmt.Fprintf(b, "    .parse_table = &ts_parse_table[0][0],\n")

	if len(lang.SmallParseTable) > 0 {
		fmt.Fprintf(b, "    .small_parse_table = ts_small_parse_table,\n")
		fmt.Fprintf(b, "    .small_parse_table_map = ts_small_parse_table_map,\n")
	}

	fmt.Fprintf(b, "    .parse_actions = ts_parse_actions,\n")
	fmt.Fprintf(b, "    .symbol_names = ts_symbol_names,\n")
	fmt.Fprintf(b, "    .symbol_metadata = ts_symbol_metadata,\n")

	if len(lang.FieldNames) > 1 {
		fmt.Fprintf(b, "    .field_names = ts_field_names,\n")
		fmt.Fprintf(b, "    .field_map_slices = ts_field_map_slices,\n")
		fmt.Fprintf(b, "    .field_map_entries = ts_field_map_entries,\n")
	}

	if len(lang.AliasSequences) > 0 {
		fmt.Fprintf(b, "    .alias_sequences = &ts_alias_sequences[0][0],\n")
	}

	fmt.Fprintf(b, "    .lex_modes = ts_lex_modes,\n")
	fmt.Fprintf(b, "    .lex_fn = ts_lex,\n")

	if len(lang.KeywordLexStates) > 0 {
		fmt.Fprintf(b, "    .keyword_lex_fn = ts_lex_keywords,\n")
		fmt.Fprintf(b, "    .keyword_capture_token = %d,\n", lang.KeywordCaptureToken)
	}

	if len(lang.ExternalSymbols) > 0 {
		fmt.Fprintf(b, "    .external_scanner = {\n")
		if len(lang.ExternalLexStates) > 0 {
			fmt.Fprintf(b, "      .states = ts_external_scanner_states,\n")
		}
		fmt.Fprintf(b, "      .symbol_map = ts_external_scanner_symbol_map,\n")
		fmt.Fprintf(b, "    },\n")
	}

	if len(lang.PrimaryStateIDs) > 0 {
		fmt.Fprintf(b, "    .primary_state_ids = ts_primary_state_ids,\n")
	}
	if len(lang.ReservedWords) > 0 && lang.MaxReservedWordSetSize > 0 {
		fmt.Fprintf(b, "    .reserved_words = &ts_reserved_words[0][0],\n")
		fmt.Fprintf(b, "    .max_reserved_word_set_size = %d,\n", lang.MaxReservedWordSetSize)
	}
	if lang.LanguageVersion >= 15 {
		fmt.Fprintf(b, "    .name = %q,\n", name)
		if len(lang.SupertypeSymbols) > 0 && len(lang.SupertypeMapEntries) > 0 {
			fmt.Fprintf(b, "    .supertype_count = SUPERTYPE_COUNT,\n")
			fmt.Fprintf(b, "    .supertype_symbols = ts_supertype_symbols,\n")
			fmt.Fprintf(b, "    .supertype_map_slices = ts_supertype_map_slices,\n")
			fmt.Fprintf(b, "    .supertype_map_entries = ts_supertype_map_entries,\n")
		}
		fmt.Fprintf(b, "    .metadata = {\n")
		fmt.Fprintf(b, "      .major_version = %d,\n", lang.Metadata.MajorVersion)
		fmt.Fprintf(b, "      .minor_version = %d,\n", lang.Metadata.MinorVersion)
		fmt.Fprintf(b, "      .patch_version = %d,\n", lang.Metadata.PatchVersion)
		fmt.Fprintf(b, "    },\n")
	}

	fmt.Fprintf(b, "  };\n")
	fmt.Fprintf(b, "  return &language;\n")
	fmt.Fprintf(b, "}\n\n")

	fmt.Fprintf(b, "#if defined(__GNUC__) || defined(__clang__)\n")
	fmt.Fprintf(b, "#pragma GCC diagnostic pop\n")
	fmt.Fprintf(b, "#endif\n")
}

// symbolToCName converts a symbol name to a C identifier.
func symbolToCName(name string, id int, lang *gotreesitter.Language) string {
	if id == 0 {
		return "ts_builtin_sym_end"
	}

	// Check if it's a named symbol (nonterminal or named token).
	if id < len(lang.SymbolMetadata) && lang.SymbolMetadata[id].Named {
		return "sym_" + sanitizeCIdent(name)
	}

	// Anonymous terminal.
	return "anon_sym_" + sanitizeCIdent(name)
}

// sanitizeCIdent converts a string to a valid C identifier component.
func sanitizeCIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		case r == '{':
			b.WriteString("LBRACE")
		case r == '}':
			b.WriteString("RBRACE")
		case r == '[':
			b.WriteString("LBRACK")
		case r == ']':
			b.WriteString("RBRACK")
		case r == '(':
			b.WriteString("LPAREN")
		case r == ')':
			b.WriteString("RPAREN")
		case r == '<':
			b.WriteString("LT")
		case r == '>':
			b.WriteString("GT")
		case r == '+':
			b.WriteString("PLUS")
		case r == '-':
			b.WriteString("DASH")
		case r == '*':
			b.WriteString("STAR")
		case r == '/':
			b.WriteString("SLASH")
		case r == '=':
			b.WriteString("EQ")
		case r == '!':
			b.WriteString("BANG")
		case r == '&':
			b.WriteString("AMP")
		case r == '|':
			b.WriteString("PIPE")
		case r == '^':
			b.WriteString("CARET")
		case r == '~':
			b.WriteString("TILDE")
		case r == '.':
			b.WriteString("DOT")
		case r == ',':
			b.WriteString("COMMA")
		case r == ';':
			b.WriteString("SEMI")
		case r == ':':
			b.WriteString("COLON")
		case r == '"':
			b.WriteString("DQUOTE")
		case r == '\'':
			b.WriteString("SQUOTE")
		case r == '\\':
			b.WriteString("BSLASH")
		case r == '#':
			b.WriteString("POUND")
		case r == '@':
			b.WriteString("AT")
		case r == '?':
			b.WriteString("QMARK")
		case r == '%':
			b.WriteString("PERCENT")
		case r == ' ':
			b.WriteString("_")
		default:
			fmt.Fprintf(&b, "U%04X", r)
		}
	}
	result := b.String()
	if result == "" {
		return fmt.Sprintf("_sym_%d", 0)
	}
	// C identifiers can't start with a digit.
	if result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}
	return result
}

// charCondition generates a C condition for a character range.
func charCondition(lo, hi rune) string {
	if lo == hi {
		return fmt.Sprintf("lookahead == %s", charLiteral(lo))
	}
	return fmt.Sprintf("(%s <= lookahead && lookahead <= %s)", charLiteral(lo), charLiteral(hi))
}

// charLiteral formats a rune as a C character literal.
func charLiteral(r rune) string {
	switch r {
	case '\n':
		return "'\\n'"
	case '\r':
		return "'\\r'"
	case '\t':
		return "'\\t'"
	case '\\':
		return "'\\\\'"
	case '\'':
		return "'\\''"
	case 0:
		return "0"
	default:
		if r >= 0x20 && r < 0x7f {
			return fmt.Sprintf("'%c'", r)
		}
		return fmt.Sprintf("%d", r)
	}
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
