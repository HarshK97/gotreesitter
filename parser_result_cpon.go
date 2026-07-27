package gotreesitter

func normalizeCPONCompatibility(root *Node, source []byte, lang *Language) {
	normalizeCPONNullLeafChildren(root, source, lang)
}

func normalizeCPONNullLeafChildren(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "cpon" || len(source) == 0 {
		return
	}
	walkResultTree(root, func(n *Node) {
		if n == nil || n.Type(lang) != "null" || resultChildCount(n) != 1 {
			return
		}
		if n.startByte > n.endByte || int(n.endByte) > len(source) || string(source[n.startByte:n.endByte]) != "null" {
			return
		}
		child := resultChildAt(n, 0)
		if child == nil || child.Type(lang) != "null" || child.startByte != n.startByte || child.endByte != n.endByte {
			return
		}
		n.children = nil
		n.clearFieldMetadata()
		if n.ownerArena != nil {
			n.ownerArena.clearFinalChildRefs(n)
		}
	})
}
