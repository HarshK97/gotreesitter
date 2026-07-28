package gotreesitter

func normalizeObjcCompatibility(root *Node, source []byte, lang *Language, census materializationSubpassCensus) {
	census.run("dispatch.objc.method-type-identifiers", func() {
		normalizeObjcMethodTypeIdentifiers(root, lang)
	})
	census.run("dispatch.objc.sizeof-type-identifier-operands", func() {
		normalizeObjcSizeofTypeIdentifierOperands(root, lang)
	})
}

func normalizeObjcMethodTypeIdentifiers(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "objc" {
		return
	}
	methodTypeSym, ok1 := symbolByName(lang, "method_type")
	typeNameSym, ok2 := symbolByName(lang, "type_name")
	identifierSym, ok3 := symbolByName(lang, "identifier")
	typeIdentifierSym, ok4 := symbolByName(lang, "type_identifier")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return
	}
	typeIdentifierNamed := symbolIsNamed(lang, typeIdentifierSym)
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != methodTypeSym {
			return
		}
		for i := 0; i < resultChildCount(n); i++ {
			typeName := resultChildAt(n, i)
			if typeName == nil || typeName.symbol != typeNameSym {
				continue
			}
			for j := 0; j < resultChildCount(typeName); j++ {
				child := resultChildAt(typeName, j)
				if child != nil && child.symbol == identifierSym {
					child.symbol = typeIdentifierSym
					child.setNamed(typeIdentifierNamed)
				}
			}
		}
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
