package gotreesitter

import "bytes"

func normalizeNinjaCompatibility(root *Node, source []byte, lang *Language) {
	normalizeNinjaRecoveredMetadataManifest(root, source, lang)
}

func normalizeNinjaRecoveredMetadataManifest(root *Node, source []byte, lang *Language) bool {
	if root == nil || lang == nil || lang.Name != "ninja" || root.ownerArena == nil || root.Type(lang) != "manifest" || resultChildCount(root) != 6 {
		return false
	}
	children := resultChildSliceForMutation(root)
	if len(children) != 6 {
		return false
	}
	for _, child := range children {
		if child == nil || child.Type(lang) != "ERROR" || child.startByte > child.endByte || int(child.endByte) > len(source) {
			return false
		}
	}
	if !ninjaNodeTextHasPrefix(children[0], source, "Description:") ||
		!ninjaNodeTextHasPrefix(children[1], source, "Version:") ||
		!ninjaNodeTextContains(children[1], source, "\nURL:") ||
		!ninjaNodeTextHasPrefix(children[2], source, "Copyright:") ||
		!ninjaNodeTextHasPrefix(children[3], source, "SPDX-License-Identifier:") ||
		!ninjaNodeTextHasPrefix(children[4], source, "Local changes:\n") ||
		!ninjaNodeTextHasPrefix(children[5], source, "- ") {
		return false
	}
	if !ninjaWhitespaceOnlyGap(source, children[0].endByte, children[1].startByte) ||
		!ninjaWhitespaceOnlyGap(source, children[1].endByte, children[2].startByte) ||
		!ninjaWhitespaceOnlyGap(source, children[2].endByte, children[3].startByte) ||
		!ninjaWhitespaceOnlyGap(source, children[3].endByte, children[4].startByte) ||
		!ninjaWhitespaceOnlyGap(source, children[4].endByte, children[5].startByte) {
		return false
	}

	identifierSym, ok := symbolByName(lang, "identifier")
	if !ok {
		return false
	}
	colonSym, ok := symbolByName(lang, ":")
	if !ok {
		return false
	}

	first := ninjaRecoveredErrorNode(root.ownerArena, source, children[0].startByte, children[1].endByte, identifierSym, colonSym)
	last := ninjaRecoveredErrorNode(root.ownerArena, source, children[3].startByte, root.endByte, identifierSym, colonSym)
	if first == nil || last == nil {
		return false
	}
	next := []*Node{first, children[2], last}
	root.children = cloneNodeSliceInArena(root.ownerArena, next)
	root.clearFieldMetadata()
	root.setHasError(true)
	populateParentNode(root, root.children)
	nodeInitEquivVersion(root)
	return true
}

func ninjaRecoveredErrorNode(arena *nodeArena, source []byte, start, end uint32, identifierSym, colonSym Symbol) *Node {
	if arena == nil || start >= end || int(end) > len(source) {
		return nil
	}
	children := ninjaRecoveredErrorTokenChildren(arena, source, start, end, identifierSym, colonSym)
	if len(children) == 0 {
		return nil
	}
	n := newParentNodeInArena(arena, errorSymbol, true, cloneNodeSliceInArena(arena, children), nil, 0)
	n.startByte = start
	n.endByte = end
	n.startPoint = advancePointByBytes(Point{}, source[:start])
	n.endPoint = advancePointByBytes(Point{}, source[:end])
	n.setHasError(true)
	n.setExtra(true)
	return n
}

func ninjaRecoveredErrorTokenChildren(arena *nodeArena, source []byte, start, end uint32, identifierSym, colonSym Symbol) []*Node {
	var out []*Node
	for pos := start; pos < end; {
		if int(pos) >= len(source) {
			break
		}
		if source[pos] == ':' {
			out = append(out, ninjaRecoveredTokenLeaf(arena, source, colonSym, false, pos, pos+1))
			pos++
			continue
		}
		if !ninjaMetadataIdentifierByte(source[pos]) {
			pos++
			continue
		}
		tokenStart := pos
		for pos < end && int(pos) < len(source) && ninjaMetadataIdentifierByte(source[pos]) {
			pos++
		}
		if pos-tokenStart < 2 {
			continue
		}
		out = append(out, ninjaRecoveredTokenLeaf(arena, source, identifierSym, true, tokenStart, pos))
	}
	return out
}

func ninjaRecoveredTokenLeaf(arena *nodeArena, source []byte, sym Symbol, named bool, start, end uint32) *Node {
	startPoint := advancePointByBytes(Point{}, source[:start])
	endPoint := advancePointByBytes(startPoint, source[start:end])
	return newLeafNodeInArena(arena, sym, named, start, end, startPoint, endPoint)
}

func ninjaMetadataIdentifierByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '.' || b == '-'
}

func ninjaNodeTextHasPrefix(n *Node, source []byte, prefix string) bool {
	if n == nil || int(n.endByte) > len(source) || n.startByte > n.endByte {
		return false
	}
	return bytes.HasPrefix(source[n.startByte:n.endByte], []byte(prefix))
}

func ninjaNodeTextContains(n *Node, source []byte, needle string) bool {
	if n == nil || int(n.endByte) > len(source) || n.startByte > n.endByte {
		return false
	}
	return bytes.Contains(source[n.startByte:n.endByte], []byte(needle))
}

func ninjaWhitespaceOnlyGap(source []byte, start, end uint32) bool {
	if start > end || int(end) > len(source) {
		return false
	}
	if start == end {
		return true
	}
	for _, b := range source[start:end] {
		if b != '\n' && b != '\r' && b != ' ' && b != '\t' {
			return false
		}
	}
	return true
}
