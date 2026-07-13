package gotreesitter

func normalizeRubyTopLevelModuleBounds(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "ruby" || root.Type(lang) != "program" || len(source) == 0 {
		return
	}
	end := lastNonTriviaByteEnd(source)
	for i := 0; i < resultChildCount(root); i++ {
		child := resultChildAt(root, i)
		if child == nil || child.IsExtra() || child.Type(lang) != "module" {
			continue
		}
		firstChild := resultChildAt(child, 0)
		if firstChild != nil && child.startByte < firstChild.startByte {
			child.startByte = firstChild.startByte
			child.startPoint = firstChild.startPoint
		}
		if child.endByte == root.endByte && end > child.startByte && end < child.endByte {
			child.endByte = end
			child.endPoint = advancePointByBytes(Point{}, source[:end])
		}
	}
}
