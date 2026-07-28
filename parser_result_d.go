package gotreesitter

func normalizeDCompatibility(root *Node, lang *Language, census materializationSubpassCensus) {
	census.run("dispatch.d.call-expression-property-types", func() {
		normalizeDCallExpressionPropertyTypes(root, lang)
	})
	census.run("dispatch.d.call-expression-simple-type-callees", func() {
		normalizeDCallExpressionSimpleTypeCallees(root, lang)
	})
	census.run("dispatch.d.variable-type-qualifiers", func() {
		normalizeDVariableTypeQualifiers(root, lang)
	})
	census.run("dispatch.d.variable-storage-class-wrappers", func() {
		normalizeDVariableStorageClassWrappers(root, lang)
	})
}

func normalizeDVariableStorageClassWrappers(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "d" {
		return
	}
	storageClassSym, ok := lang.SymbolByName("storage_class")
	if !ok {
		return
	}
	storageClassNamed := symbolIsNamed(lang, storageClassSym)
	walkResultTree(root, func(n *Node) {
		if n.Type(lang) == "variable_declaration" {
			for i, child := range n.children {
				if child == nil || child.Type(lang) != "static" {
					continue
				}
				wrapper := newParentNodeInArena(n.ownerArena, storageClassSym, storageClassNamed, []*Node{child}, nil, 0)
				n.children[i] = wrapper
			}
		}
	})
}

func normalizeDVariableTypeQualifiers(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "d" {
		return
	}
	walkResultTree(root, func(n *Node) {
		if n.Type(lang) == "variable_declaration" && len(n.children) >= 3 {
			for i := 0; i+1 < len(n.children); i++ {
				left := n.children[i]
				right := n.children[i+1]
				if left == nil || right == nil || left.Type(lang) != "storage_class" || right.Type(lang) != "type" {
					continue
				}
				if len(left.children) != 1 || left.children[0] == nil || left.children[0].Type(lang) != "type_ctor" {
					continue
				}
				mergedType := cloneNodeInArena(n.ownerArena, right)
				mergedChildren := make([]*Node, 0, 1+len(right.children))
				mergedChildren = append(mergedChildren, left.children[0])
				mergedChildren = append(mergedChildren, right.children...)
				if n.ownerArena != nil {
					buf := n.ownerArena.allocNodeSlice(len(mergedChildren))
					copy(buf, mergedChildren)
					mergedChildren = buf
				}
				mergedType.children = mergedChildren
				mergedType.startByte = mergedChildren[0].startByte
				mergedType.startPoint = mergedChildren[0].startPoint
				n.children[i+1] = mergedType
				n.children = append(n.children[:i], n.children[i+1:]...)
				break
			}
		}
	})
}

func normalizeDCallExpressionPropertyTypes(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "d" {
		return
	}
	typeSym, ok := lang.SymbolByName("type")
	if !ok {
		return
	}
	typeNamed := symbolIsNamed(lang, typeSym)
	walkResultTree(root, func(n *Node) {
		if n.Type(lang) == "call_expression" && len(n.children) > 0 {
			child := n.children[0]
			if parts, ok := flattenDPropertyTypeChain(child, lang); ok {
				if n.ownerArena != nil {
					buf := n.ownerArena.allocNodeSlice(len(parts))
					copy(buf, parts)
					parts = buf
				}
				n.children[0] = newParentNodeInArena(n.ownerArena, typeSym, typeNamed, parts, nil, 0)
			}
		}
	})
}

func normalizeDCallExpressionSimpleTypeCallees(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "d" {
		return
	}
	walkResultTree(root, func(n *Node) {
		if n.Type(lang) == "call_expression" && len(n.children) > 0 {
			child := n.children[0]
			if child != nil && child.Type(lang) == "type" && len(child.children) == 1 && child.children[0] != nil && child.children[0].Type(lang) == "identifier" {
				n.children[0] = child.children[0]
			}
		}
	})
}
func flattenDPropertyTypeChain(n *Node, lang *Language) ([]*Node, bool) {
	if n == nil || lang == nil {
		return nil, false
	}
	switch n.Type(lang) {
	case "identifier":
		return []*Node{n}, true
	case "property_expression":
		if len(n.children) != 3 || n.children[1] == nil || n.children[2] == nil {
			return nil, false
		}
		rightType := n.children[2].Type(lang)
		if n.children[1].Type(lang) != "." || (rightType != "identifier" && rightType != "template_instance") {
			return nil, false
		}
		left, ok := flattenDPropertyTypeChain(n.children[0], lang)
		if !ok {
			return nil, false
		}
		out := make([]*Node, 0, len(left)+2)
		out = append(out, left...)
		out = append(out, n.children[1], n.children[2])
		return out, true
	default:
		return nil, false
	}
}
