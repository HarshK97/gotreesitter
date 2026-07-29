package gotreesitter

func normalizeApexClassLiteralAccess(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "apex" {
		return
	}
	fieldAccessSym, fieldAccessNamed, ok := symbolMeta(lang, "field_access")
	if !ok {
		return
	}
	identifierSym, identifierNamed, ok := symbolMeta(lang, "identifier")
	if !ok {
		return
	}
	walkResultTree(root, func(n *Node) {
		if n == nil || n.Type(lang) != "class_literal" || resultChildCount(n) != 3 {
			return
		}
		left := resultChildAt(n, 0)
		dot := resultChildAt(n, 1)
		right := resultChildAt(n, 2)
		if left == nil || dot == nil || right == nil ||
			left.Type(lang) != "type_identifier" ||
			dot.Type(lang) != "." ||
			right.Type(lang) != "class" ||
			!apexNodeTextEquals(source, right, "class") {
			return
		}
		retagResultRoot(n, fieldAccessSym, fieldAccessNamed)
		retagResultRoot(left, identifierSym, identifierNamed)
		retagResultRoot(right, identifierSym, identifierNamed)
	})
}

func apexNodeTextEquals(source []byte, n *Node, want string) bool {
	return n != nil &&
		n.startByte <= n.endByte &&
		int(n.endByte) <= len(source) &&
		string(source[n.startByte:n.endByte]) == want
}
