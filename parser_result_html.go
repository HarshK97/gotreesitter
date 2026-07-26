package gotreesitter

// normalizeHTMLRecoveredNestedCustomTagRanges is the surviving half of the
// former dispatch.html arm (R2, docs/root-normalization-retirement.md). Its
// sibling normalizeHTMLRecoveredNestedCustomTags (the ERROR-root ->
// wrapped-element reconstruction for deeply nested unclosed custom tags) was
// retired: the reduce engine no longer produces the flat unwrapped
// start_tag-sibling ERROR shape it targeted (confirmed by real-corpus census
// and direct adversarial probing of mismatched/auto-closing nested custom-tag
// HTML — see testdata/result_compat_ownership_v1.json's dispatch.html entry).
//
// This function stays live: it is also called directly from
// normalizeReturnedTree's post-finalization second pass (parser_api.go),
// which is the separate fixpoint.returned-tree-second-pass mechanism R1 of
// the retirement plan owns, not R2.
func normalizeHTMLRecoveredNestedCustomTagRanges(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "html" || len(source) == 0 {
		return
	}
	walkResultTreePostorder(root, func(node *Node) {
		childCount := resultChildCount(node)
		if node.Type(lang) != "element" || childCount < 2 {
			return
		}
		for i := 0; i+1 < childCount; i++ {
			left := resultChildAt(node, i)
			right := resultChildAt(node, i+1)
			if left == nil || right == nil || left.Type(lang) != "element" || right.Type(lang) != "end_tag" || resultChildCount(right) == 0 {
				continue
			}
			closeTok := resultChildAt(right, 0)
			if closeTok == nil || closeTok.Type(lang) != "</" || left.endByte >= closeTok.startByte || closeTok.startByte > uint32(len(source)) {
				continue
			}
			if !bytesAreTrivia(source[left.endByte:closeTok.startByte]) {
				continue
			}
			htmlExtendLeadingElementChain(left, closeTok.startByte, closeTok.startPoint, lang)
		}
	})
}

func htmlExtendLeadingElementChain(node *Node, endByte uint32, endPoint Point, lang *Language) {
	// Validate the complete chain before changing any range. Doing this once
	// also keeps deeply recovered HTML chains linear rather than re-walking the
	// remaining suffix for every element.
	if !htmlElementIsUnclosedChain(node, lang) {
		return
	}
	for cur := node; cur != nil && lang != nil && cur.Type(lang) == "element"; {
		// Only recovered chains of unclosed start tags should grow to the
		// enclosing close token. Closed or self-closing elements and elements
		// ending in concrete content already have an authoritative range.
		cur.endByte = endByte
		cur.endPoint = endPoint
		child := resultChildAt(cur, 1)
		if resultChildCount(cur) < 2 || child == nil || child.Type(lang) != "element" {
			return
		}
		cur = child
	}
}

func htmlElementIsUnclosedChain(node *Node, lang *Language) bool {
	for cur := node; cur != nil && lang != nil && cur.Type(lang) == "element"; {
		childCount := resultChildCount(cur)
		if childCount == 0 {
			return false
		}
		last := resultChildAt(cur, childCount-1)
		if last == nil {
			return false
		}
		switch last.Type(lang) {
		case "start_tag":
			return true
		case "element":
			cur = last
		default:
			return false
		}
	}
	return false
}
