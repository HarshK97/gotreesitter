package gotreesitter

func normalizeHaskellCompatibility(root *Node, source []byte, lang *Language) {
	normalizeHaskellImportsSpan(root, source, lang)
	normalizeHaskellDeclarationsSpan(root, source, lang)
}

func normalizeHaskellImportsSpan(root *Node, source []byte, lang *Language) {
	childCount := resultChildCount(root)
	if root == nil || childCount < 2 || len(source) == 0 || lang == nil || lang.Name != "haskell" {
		return
	}
	for i := 0; i+1 < childCount; i++ {
		left := resultChildAt(root, i)
		right := resultChildAt(root, i+1)
		if left == nil || right == nil {
			continue
		}
		if left.Type(lang) != "imports" {
			continue
		}
		if left.endByte >= right.startByte {
			continue
		}
		if left.endByte > uint32(len(source)) || right.startByte > uint32(len(source)) {
			continue
		}
		gap := source[left.endByte:right.startByte]
		if !bytesAreTrivia(gap) {
			continue
		}
		left.endByte = right.startByte
		left.endPoint = advancePointByBytes(left.endPoint, gap)
	}
}

func normalizeHaskellDeclarationsSpan(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "haskell" || len(source) == 0 {
		return
	}
	for i := 0; i < resultChildCount(root); i++ {
		child := resultChildAt(root, i)
		if child == nil || child.Type(lang) != "declarations" {
			continue
		}
		if child.endByte >= root.endByte || root.endByte > uint32(len(source)) {
			continue
		}
		gap := source[child.endByte:root.endByte]
		if !bytesAreTrivia(gap) {
			continue
		}
		extendNodeEndTo(child, root.endByte, source)
	}
}
