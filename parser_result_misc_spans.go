package gotreesitter

func normalizePascalTopLevelProgramEnd(root *Node, source []byte, lang *Language) {
	shrinkFirstTopLevelChildEndToLastNonTrivia(root, source, lang, "pascal", "root", "program", false)
}

func normalizePascalTrailingExtraTrivia(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "pascal" || root.Type(lang) != "root" {
		return
	}
	trimTrailingExtraTriviaRoot(root, source, lang)
}

func shrinkFirstTopLevelChildEndToLastNonTrivia(root *Node, source []byte, lang *Language, languageName, rootType, childType string, requireSingleChild bool) bool {
	if root == nil || lang == nil || lang.Name != languageName || root.Type(lang) != rootType || resultChildCount(root) == 0 {
		return false
	}
	if requireSingleChild && resultChildCount(root) != 1 {
		return false
	}
	child := resultChildAt(root, 0)
	if child == nil || child.IsExtra() || child.Type(lang) != childType {
		return false
	}
	end := lastNonTriviaByteEnd(source)
	if end == 0 || end >= child.endByte {
		return false
	}
	child.endByte = end
	child.endPoint = advancePointByBytes(Point{}, source[:end])
	return true
}

func bytesAreCooklangStepTail(b []byte) bool {
	sawPunctuation := false
	for _, c := range b {
		switch c {
		case '.', '!', '?':
			if sawPunctuation {
				return false
			}
			sawPunctuation = true
		case ' ', '\t', '\n', '\r':
		default:
			return false
		}
	}
	return sawPunctuation
}

func normalizeCooklangTrailingStepTail(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "cooklang" || len(source) == 0 {
		return
	}
	if root.Type(lang) != "recipe" || resultChildCount(root) != 1 {
		return
	}
	step := resultChildAt(root, 0)
	if step == nil || step.Type(lang) != "step" || step.endByte >= uint32(len(source)) {
		return
	}
	tail := source[step.endByte:]
	if !bytesAreCooklangStepTail(tail) {
		return
	}
	stepEnd := step.endByte
	for i := int(step.endByte); i < len(source); i++ {
		switch source[i] {
		case '.', '!', '?':
			stepEnd = uint32(i + 1)
		}
	}
	if stepEnd > step.endByte {
		extendNodeEndTo(step, stepEnd, source)
	}
	if root.endByte < uint32(len(source)) {
		extendNodeEndTo(root, uint32(len(source)), source)
	}
}

func normalizeTopLevelTrailingLineBreakSpan(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || len(source) == 0 {
		return
	}
	if resultChildCount(root) != 1 {
		return
	}
	child := resultChildAt(root, 0)
	if child == nil || child.endByte >= root.endByte || root.endByte > uint32(len(source)) {
		return
	}
	gap := source[child.endByte:root.endByte]
	if !bytesAreTrivia(gap) || !bytesContainLineBreak(gap) {
		return
	}
	extendNodeEndTo(child, root.endByte, source)
}

func normalizeJustTopLevelTrailingLineBreakSpans(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "just" || len(source) == 0 || root.Type(lang) != "source_file" {
		return
	}
	normalizeJustTrailingLineBreakSpansInNode(root, root, source, lang)
}

func normalizeJustTrailingLineBreakSpansInNode(root, parent *Node, source []byte, lang *Language) {
	if parent == nil {
		return
	}
	count := resultChildCount(parent)
	for i := 0; i < count; i++ {
		child := resultChildAt(parent, i)
		normalizeJustTrailingLineBreakSpan(root, parent, child, i, count, source, lang)
		normalizeJustTrailingLineBreakSpansInNode(root, child, source, lang)
	}
}

func normalizeJustTrailingLineBreakSpan(root, parent, child *Node, idx, count int, source []byte, lang *Language) {
	if child == nil || child.endByte >= uint32(len(source)) {
		return
	}
	switch child.Type(lang) {
	case "setting", "assignment", "export", "attribute", "recipe":
	case "ERROR":
		if parent != root {
			return
		}
	default:
		return
	}
	limit := parent.endByte
	if idx+1 < count {
		next := resultChildAt(parent, idx+1)
		if next != nil && next.startByte >= child.endByte {
			limit = next.startByte
		}
	}
	if limit <= child.endByte || limit > uint32(len(source)) {
		return
	}
	gap := source[child.endByte:limit]
	if !bytesAreTrivia(gap) || !bytesContainLineBreak(gap) {
		return
	}
	end := child.endByte
	for end < limit {
		switch source[end] {
		case '\n':
			end++
			if end > child.endByte {
				extendNodeEndTo(child, end, source)
			}
			return
		case '\r':
			end++
			if end < limit && source[end] == '\n' {
				end++
			}
			if end > child.endByte {
				extendNodeEndTo(child, end, source)
			}
			return
		case ' ', '\t':
			end++
		default:
			return
		}
	}
}

func lineBreakEndAt(source []byte, start, limit uint32) uint32 {
	if start >= limit || start >= uint32(len(source)) {
		return 0
	}
	switch source[start] {
	case '\n':
		return start + 1
	case '\r':
		if start+1 < limit && start+1 < uint32(len(source)) && source[start+1] == '\n' {
			return start + 2
		}
		return start + 1
	default:
		return 0
	}
}

// extendChildLineBreakBeforeNextSibling walks every parentType node and, for
// each childType child that stops short of its next sibling, extends that
// child's end to swallow an immediately following line break (formerly
// normalizeFortranStatementLineBreaks, generalized for the shared
// trailingSpanRule table in parser_result_trailing_span_rules.go).
func extendChildLineBreakBeforeNextSibling(root *Node, source []byte, lang *Language, parentType, childType string) {
	if root == nil || lang == nil || len(source) == 0 {
		return
	}
	walkResultTreeBounded(root, func(n *Node) {
		if n.Type(lang) != parentType {
			return
		}
		childCount := resultChildCount(n)
		for i := 0; i+1 < childCount; i++ {
			cur := resultChildAt(n, i)
			next := resultChildAt(n, i+1)
			if cur == nil || next == nil || cur.endByte >= next.startByte {
				continue
			}
			if cur.Type(lang) != childType {
				continue
			}
			if end := lineBreakEndAt(source, cur.endByte, next.startByte); end > cur.endByte {
				extendNodeEndTo(cur, end, source)
			}
		}
	})
}

func normalizeNginxAttributeLineBreaks(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "nginx" || len(source) == 0 {
		return
	}
	walkResultTreeBounded(root, func(n *Node) {
		if n.Type(lang) == "attribute" {
			if end := lineBreakEndAt(source, n.endByte, uint32(len(source))); end > n.endByte {
				extendNodeEndTo(n, end, source)
			}
		}
	})
}

func normalizeRootEOFNewlineSpan(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || len(source) == 0 || root.endByte >= uint32(len(source)) {
		return
	}
	switch {
	case lang.Name == "go" && root.Type(lang) == "source_file":
	case lang.Name == "scala" && root.Type(lang) == "compilation_unit":
	default:
		return
	}
	gap := source[root.endByte:]
	if !bytesAreTrivia(gap) || !bytesContainLineBreak(gap) {
		return
	}
	extendNodeEndTo(root, uint32(len(source)), source)
}
