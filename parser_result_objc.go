package gotreesitter

func normalizeObjcCompatibility(root *Node, source []byte, lang *Language) {
	normalizeObjcMethodTypeIdentifiers(root, lang)
	normalizeObjcSizeofTypeIdentifierOperands(root, lang)
	normalizeObjcAtStringLiterals(root, lang)
	normalizeObjcStructSizedTypeSpecifiers(root, lang)
	normalizeObjcEncodeTypeIdentifiers(root, lang)
	normalizeObjcFunctionPointerDeclarationsAsExpressions(root, lang)
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

func normalizeObjcAtStringLiterals(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "objc" {
		return
	}
	atExpressionSym, ok1 := symbolByName(lang, "at_expression")
	stringLiteralSym, ok2 := symbolByName(lang, "string_literal")
	concatenatedStringSym, hasConcatenatedString := symbolByName(lang, "concatenated_string")
	if !ok1 || !ok2 {
		return
	}
	stringLiteralNamed := symbolIsNamed(lang, stringLiteralSym)
	concatenatedStringNamed := false
	if hasConcatenatedString {
		concatenatedStringNamed = symbolIsNamed(lang, concatenatedStringSym)
	}
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != atExpressionSym || resultChildCount(n) != 2 {
			return
		}
		at := resultChildAt(n, 0)
		inner := resultChildAt(n, 1)
		if at == nil || inner == nil {
			return
		}
		switch inner.symbol {
		case stringLiteralSym:
			innerChildren := resultChildSliceForMutation(inner)
			if len(innerChildren) == 0 {
				return
			}
			children := make([]*Node, 0, len(innerChildren)+1)
			children = append(children, at)
			children = append(children, innerChildren...)
			children = cloneNodeSliceIfArena(n.ownerArena, children)
			n.symbol = stringLiteralSym
			n.setNamed(stringLiteralNamed)
			replaceNodeChildrenUnfielded(n, children)
			n.productionID = 0
		case concatenatedStringSym:
			if !hasConcatenatedString || resultChildCount(inner) == 0 {
				return
			}
			concatChildren := resultChildSliceForMutation(inner)
			first := concatChildren[0]
			if first == nil || first.symbol != stringLiteralSym {
				return
			}
			firstChildren := resultChildSliceForMutation(first)
			if len(firstChildren) == 0 {
				return
			}
			rebuiltFirstChildren := make([]*Node, 0, len(firstChildren)+1)
			rebuiltFirstChildren = append(rebuiltFirstChildren, at)
			rebuiltFirstChildren = append(rebuiltFirstChildren, firstChildren...)
			first.symbol = stringLiteralSym
			first.setNamed(stringLiteralNamed)
			first.startByte = at.startByte
			first.startPoint = at.startPoint
			replaceNodeChildrenUnfielded(first, cloneNodeSliceIfArena(first.ownerArena, rebuiltFirstChildren))
			first.productionID = 0
			children := cloneNodeSliceIfArena(n.ownerArena, concatChildren)
			n.symbol = concatenatedStringSym
			n.setNamed(concatenatedStringNamed)
			n.startByte = at.startByte
			n.startPoint = at.startPoint
			replaceNodeChildrenUnfielded(n, children)
			n.productionID = 0
		default:
			return
		}
	})
}

func normalizeObjcStructSizedTypeSpecifiers(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "objc" {
		return
	}
	structDeclarationSym, ok1 := symbolByName(lang, "struct_declaration")
	sizedTypeSpecifierSym, ok2 := symbolByName(lang, "sized_type_specifier")
	if !ok1 || !ok2 {
		return
	}
	sizedTypeSpecifierNamed := symbolIsNamed(lang, sizedTypeSpecifierSym)
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != structDeclarationSym || resultChildCount(n) < 4 {
			return
		}
		children := resultChildSliceForMutation(n)
		for i := 0; i+1 < len(children); i++ {
			left := children[i]
			right := children[i+1]
			if left == nil || right == nil || left.symbol != sizedTypeSpecifierSym || right.symbol != sizedTypeSpecifierSym {
				continue
			}
			if resultChildCount(left) != 1 || resultChildCount(right) != 1 {
				continue
			}
			mergedChildren := []*Node{resultChildAt(left, 0), resultChildAt(right, 0)}
			merged := newParentNodeInArena(n.ownerArena, sizedTypeSpecifierSym, sizedTypeSpecifierNamed, cloneNodeSliceIfArena(n.ownerArena, mergedChildren), nil, 0)
			merged.startByte = left.startByte
			merged.endByte = right.endByte
			merged.startPoint = left.startPoint
			merged.endPoint = right.endPoint
			replaceChildRangeWithSingleNode(n, i, i+2, merged)
			return
		}
	})
}

func normalizeObjcEncodeTypeIdentifiers(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "objc" {
		return
	}
	encodeExpressionSym, ok1 := symbolByName(lang, "encode_expression")
	typeNameSym, ok2 := symbolByName(lang, "type_name")
	identifierSym, ok3 := symbolByName(lang, "identifier")
	typeIdentifierSym, ok4 := symbolByName(lang, "type_identifier")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return
	}
	typeIdentifierNamed := symbolIsNamed(lang, typeIdentifierSym)
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != encodeExpressionSym {
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

func normalizeObjcFunctionPointerDeclarationsAsExpressions(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "objc" {
		return
	}
	declarationSym, ok1 := symbolByName(lang, "declaration")
	expressionStatementSym, ok2 := symbolByName(lang, "expression_statement")
	assignmentExpressionSym, ok3 := symbolByName(lang, "assignment_expression")
	initDeclaratorSym, ok4 := symbolByName(lang, "init_declarator")
	functionDeclaratorSym, ok5 := symbolByName(lang, "function_declarator")
	typeIdentifierSym, ok6 := symbolByName(lang, "type_identifier")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return
	}
	expressionStatementNamed := symbolIsNamed(lang, expressionStatementSym)
	assignmentExpressionNamed := symbolIsNamed(lang, assignmentExpressionSym)
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != declarationSym || resultChildCount(n) != 3 {
			return
		}
		typeNode := resultChildAt(n, 0)
		declarator := resultChildAt(n, 1)
		semi := resultChildAt(n, 2)
		if typeNode == nil || typeNode.symbol != typeIdentifierSym || declarator == nil || semi == nil {
			return
		}
		var functionDeclarator, eq, value *Node
		if declarator.symbol == functionDeclaratorSym {
			functionDeclarator = declarator
		} else if declarator.symbol == initDeclaratorSym && resultChildCount(declarator) == 3 {
			functionDeclarator = resultChildAt(declarator, 0)
			eq = resultChildAt(declarator, 1)
			value = resultChildAt(declarator, 2)
			if functionDeclarator == nil || functionDeclarator.symbol != functionDeclaratorSym || eq == nil || value == nil {
				return
			}
		} else {
			return
		}
		call := objcBuildFunctionPointerExpressionCall(n.ownerArena, lang, typeNode, functionDeclarator)
		if call == nil {
			return
		}
		expr := call
		if eq != nil {
			expr = newParentNodeInArena(n.ownerArena, assignmentExpressionSym, assignmentExpressionNamed, cloneNodeSliceIfArena(n.ownerArena, []*Node{call, eq, value}), nil, 0)
		}
		n.symbol = expressionStatementSym
		n.setNamed(expressionStatementNamed)
		replaceNodeChildrenUnfielded(n, cloneNodeSliceIfArena(n.ownerArena, []*Node{expr, semi}))
		n.productionID = 0
	})
}

func objcBuildFunctionPointerExpressionCall(arena *nodeArena, lang *Language, typeNode, functionDeclarator *Node) *Node {
	if arena == nil || lang == nil || typeNode == nil || functionDeclarator == nil || resultChildCount(functionDeclarator) != 2 {
		return nil
	}
	callExpressionSym, ok1 := symbolByName(lang, "call_expression")
	argumentListSym, ok2 := symbolByName(lang, "argument_list")
	pointerExpressionSym, ok3 := symbolByName(lang, "pointer_expression")
	identifierSym, ok4 := symbolByName(lang, "identifier")
	parenthesizedDeclaratorSym, ok5 := symbolByName(lang, "parenthesized_declarator")
	pointerDeclaratorSym, ok6 := symbolByName(lang, "pointer_declarator")
	parameterListSym, ok7 := symbolByName(lang, "parameter_list")
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
		return nil
	}
	parenthesized := resultChildAt(functionDeclarator, 0)
	parameters := resultChildAt(functionDeclarator, 1)
	if parenthesized == nil || parenthesized.symbol != parenthesizedDeclaratorSym || resultChildCount(parenthesized) != 3 {
		return nil
	}
	if parameters == nil || parameters.symbol != parameterListSym {
		return nil
	}
	pointerDeclarator := resultChildAt(parenthesized, 1)
	if pointerDeclarator == nil || pointerDeclarator.symbol != pointerDeclaratorSym || resultChildCount(pointerDeclarator) != 2 {
		return nil
	}
	callExpressionNamed := symbolIsNamed(lang, callExpressionSym)
	argumentListNamed := symbolIsNamed(lang, argumentListSym)
	pointerExpressionNamed := symbolIsNamed(lang, pointerExpressionSym)
	identifierNamed := symbolIsNamed(lang, identifierSym)

	callee := newLeafNodeInArena(arena, identifierSym, identifierNamed, typeNode.startByte, typeNode.endByte, typeNode.startPoint, typeNode.endPoint)
	pointerExpression := newParentNodeInArena(arena, pointerExpressionSym, pointerExpressionNamed, cloneNodeSliceIfArena(arena, resultChildSliceForMutation(pointerDeclarator)), nil, 0)
	innerArguments := newParentNodeInArena(arena, argumentListSym, argumentListNamed, cloneNodeSliceIfArena(arena, []*Node{
		resultChildAt(parenthesized, 0),
		pointerExpression,
		resultChildAt(parenthesized, 2),
	}), nil, 0)
	innerCall := newParentNodeInArena(arena, callExpressionSym, callExpressionNamed, cloneNodeSliceIfArena(arena, []*Node{callee, innerArguments}), nil, 0)
	outerArguments := objcBuildExpressionArgumentListFromParameterList(arena, lang, parameters)
	if outerArguments == nil {
		return nil
	}
	return newParentNodeInArena(arena, callExpressionSym, callExpressionNamed, cloneNodeSliceIfArena(arena, []*Node{innerCall, outerArguments}), nil, 0)
}

func objcBuildExpressionArgumentListFromParameterList(arena *nodeArena, lang *Language, parameters *Node) *Node {
	if arena == nil || lang == nil || parameters == nil {
		return nil
	}
	argumentListSym, ok1 := symbolByName(lang, "argument_list")
	parameterDeclarationSym, ok2 := symbolByName(lang, "parameter_declaration")
	identifierSym, ok3 := symbolByName(lang, "identifier")
	if !ok1 || !ok2 || !ok3 {
		return nil
	}
	argumentListNamed := symbolIsNamed(lang, argumentListSym)
	identifierNamed := symbolIsNamed(lang, identifierSym)
	children := make([]*Node, 0, resultChildCount(parameters))
	for i := 0; i < resultChildCount(parameters); i++ {
		child := resultChildAt(parameters, i)
		if child == nil {
			return nil
		}
		if child.symbol != parameterDeclarationSym {
			children = append(children, child)
			continue
		}
		if resultChildCount(child) != 1 {
			return nil
		}
		arg := objcIdentifierForExpressionParameter(arena, lang, resultChildAt(child, 0), identifierSym, identifierNamed)
		if arg == nil {
			return nil
		}
		children = append(children, arg)
	}
	return newParentNodeInArena(arena, argumentListSym, argumentListNamed, cloneNodeSliceIfArena(arena, children), nil, 0)
}

func objcIdentifierForExpressionParameter(arena *nodeArena, lang *Language, n *Node, identifierSym Symbol, identifierNamed bool) *Node {
	if arena == nil || lang == nil || n == nil {
		return nil
	}
	if resultChildCount(n) == 0 {
		return newLeafNodeInArena(arena, identifierSym, identifierNamed, n.startByte, n.endByte, n.startPoint, n.endPoint)
	}
	if resultChildCount(n) == 1 {
		return objcIdentifierForExpressionParameter(arena, lang, resultChildAt(n, 0), identifierSym, identifierNamed)
	}
	return nil
}
