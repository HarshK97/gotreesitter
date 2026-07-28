package gotreesitter

func normalizeObjcCompatibility(root *Node, source []byte, lang *Language, census materializationSubpassCensus) {
	census.run("dispatch.objc.sizeof-type-identifier-operands", func() {
		normalizeObjcSizeofTypeIdentifierOperands(root, lang)
	})
}

func normalizeObjcSizeofTypeIdentifierOperands(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "objc" {
		return
	}
	sizeofSym, ok1 := symbolByName(lang, "sizeof_expression")
	typeDescriptorSym, ok2 := symbolByName(lang, "type_descriptor")
	typeIdentifierSym, ok3 := symbolByName(lang, "type_identifier")
	identifierSym, ok4 := symbolByName(lang, "identifier")
	parenthesizedSym, ok5 := symbolByName(lang, "parenthesized_expression")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
		return
	}
	identifierNamed := symbolIsNamed(lang, identifierSym)
	parenthesizedNamed := symbolIsNamed(lang, parenthesizedSym)
	valueFieldID, hasValueField := lang.FieldByName("value")
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != sizeofSym || len(n.children) != 4 {
			return
		}
		typeDescriptor := n.children[2]
		if typeDescriptor == nil || typeDescriptor.symbol != typeDescriptorSym || len(typeDescriptor.children) != 1 {
			return
		}
		typeIdent := typeDescriptor.children[0]
		if typeIdent == nil || typeIdent.symbol != typeIdentifierSym {
			return
		}
		ident := newLeafNodeInArena(n.ownerArena, identifierSym, identifierNamed, typeIdent.startByte, typeIdent.endByte, typeIdent.startPoint, typeIdent.endPoint)
		paren := newParentNodeInArena(n.ownerArena, parenthesizedSym, parenthesizedNamed, []*Node{n.children[1], ident, n.children[3]}, nil, 0)
		replaceChildRangeWithSingleNode(n, 1, 4, paren)
		if hasValueField && len(n.children) > 1 {
			ensureNodeFieldStorage(n, len(n.children))
			n.fieldIDs()[1] = valueFieldID
			n.fieldSources()[1] = fieldSourceDirect
		}
	})
}
