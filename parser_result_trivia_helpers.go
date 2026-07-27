package gotreesitter

func bytesAreTrivia(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			return false
		}
	}
	return true
}

// firstNonTriviaByteStart returns the first non-whitespace byte. It skips an
// optional UTF-8 byte order mark. It returns zero for an all-whitespace source.
func firstNonTriviaByteStart(source []byte) uint32 {
	start := 0
	if len(source) >= len(utf8BOM) &&
		source[0] == utf8BOM[0] && source[1] == utf8BOM[1] && source[2] == utf8BOM[2] {
		start = len(utf8BOM)
	}
	for index := start; index < len(source); index++ {
		switch source[index] {
		case ' ', '\t', '\n', '\r', '\f':
			continue
		default:
			return uint32(index)
		}
	}
	return 0
}

// bytesAreInterTokenTrivia is bytesAreTrivia plus line-continuation backslashes
// (`\` immediately before a newline). The forest's reduce-coverage rejection
// uses it to decide whether a hole BETWEEN two reduced children is real
// (a dropped statement/token → invalid) or just inter-token whitespace the
// lexer skips. A bare `\<newline>` only appears between tokens where the
// language treats it as a continuation (bash/python/C/…), so accepting it here
// is safe; a real `\` token is never immediately followed by a newline.
func bytesAreInterTokenTrivia(b []byte) bool {
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case ' ', '\t', '\n', '\r', '\f':
			continue
		case '\\':
			if i+1 < len(b) && (b[i+1] == '\n' || b[i+1] == '\r') {
				continue // line-continuation backslash; the newline is whitespace
			}
			return false
		default:
			return false
		}
	}
	return true
}

func lastNonTriviaByteEnd(source []byte) uint32 {
	for i := len(source); i > 0; i-- {
		switch source[i-1] {
		case ' ', '\t', '\n', '\r', '\f':
			continue
		default:
			return uint32(i)
		}
	}
	return 0
}

// trimTrailingExtraTriviaRoot drops a trailing extra child that is pure
// whitespace AND hidden (SymbolMetadata.Visible == false) from the root,
// widening the root's own span to still cover it instead. It targets grammars
// whose extras rule captures raw whitespace as its own (unnamed, invisible)
// token: tree-sitter C never surfaces that as a distinct trailing child, only
// as span coverage. A VISIBLE extra (e.g. an anonymous-but-shown punctuation
// token used as a stand-in "extra" in a synthetic fixture, or any grammar that
// deliberately gives its trivia a visible node type) is intentionally shown in
// the tree, so it is left alone even when its bytes happen to be whitespace —
// only the hidden case is the "never really a node" pattern this normalizes.
func trimTrailingExtraTriviaRoot(root *Node, source []byte, lang *Language) {
	view := resultMutableChildrenForMutation(root)
	childCount := view.Len()
	if root == nil || childCount == 0 || len(source) == 0 {
		return
	}
	if view.hasFinalChildRefs() {
		last, ok := view.Entry(childCount - 1)
		if !ok || !stackEntryNodeIsExtra(last) || stackEntryNodeChildCount(last) != 0 {
			return
		}
		if symbolIsVisible(lang, stackEntryNodeSymbol(last)) {
			return
		}
		start := stackEntryNodeStartByte(last)
		end := stackEntryNodeEndByte(last)
		if start >= end || end != root.endByte || int(end) > len(source) {
			return
		}
		if !bytesAreTrivia(source[start:end]) {
			return
		}
		view.FilterFinalRefs(func(i int, entry stackEntry) bool {
			return i+1 < childCount
		})
		return
	}
	last := resultChildAt(root, childCount-1)
	if last == nil || !last.IsExtra() || resultChildCount(last) != 0 {
		return
	}
	if symbolIsVisible(lang, last.symbol) {
		return
	}
	if last.startByte >= last.endByte || last.endByte != root.endByte || int(last.endByte) > len(source) {
		return
	}
	if !bytesAreTrivia(source[last.startByte:last.endByte]) {
		return
	}
	children := resultChildSliceForMutation(root)
	root.children = cloneNodeSliceInArena(root.ownerArena, children[:len(children)-1])
	fieldIDs := root.fieldIDs()
	fieldSources := root.fieldSources()
	metadataChanged := false
	if len(fieldIDs) > len(root.children) {
		fieldIDs = fieldIDs[:len(root.children)]
		metadataChanged = true
	}
	if len(fieldSources) > len(root.children) {
		fieldSources = fieldSources[:len(root.children)]
		metadataChanged = true
	}
	if metadataChanged {
		root.setFieldMetadata(fieldIDs, fieldSources)
	}
	populateParentNode(root, root.children)
}
