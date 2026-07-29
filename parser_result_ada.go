package gotreesitter

import "bytes"

func normalizeAdaCompatibility(root *Node, source []byte, lang *Language) {
	normalizeAdaCompatibilityWithCensus(root, source, lang, materializationSubpassCensus{})
}

func normalizeAdaCompatibilityWithCensus(
	root *Node,
	source []byte,
	lang *Language,
	census materializationSubpassCensus,
) {
	if root == nil || lang == nil || lang.Name != "ada" || len(source) == 0 {
		return
	}
	recordAggSym, recordAggNamed, ok := symbolMeta(lang, "record_aggregate")
	if !ok {
		return
	}
	namedArrayAggSym, namedArrayAggNamed, ok := symbolMeta(lang, "named_array_aggregate")
	if !ok {
		return
	}
	positionalArrayAggSym, positionalArrayAggNamed, ok := symbolMeta(lang, "positional_array_aggregate")
	if !ok {
		return
	}
	recordAssocListSym, recordAssocListNamed, ok := symbolMeta(lang, "record_component_association_list")
	if !ok {
		return
	}
	arrayAssocSym, arrayAssocNamed, ok := symbolMeta(lang, "array_component_association")
	if !ok {
		return
	}
	componentChoiceListSym, componentChoiceListNamed, ok := symbolMeta(lang, "component_choice_list")
	if !ok {
		return
	}
	discreteChoiceListSym, discreteChoiceListNamed, ok := symbolMeta(lang, "discrete_choice_list")
	if !ok {
		return
	}
	discreteChoiceSym, discreteChoiceNamed, ok := symbolMeta(lang, "discrete_choice")
	if !ok {
		return
	}
	indexConstraintSym, indexConstraintNamed, ok := symbolMeta(lang, "index_constraint")
	if !ok {
		return
	}
	discriminantConstraintSym, ok := symbolByName(lang, "discriminant_constraint")
	if !ok {
		return
	}
	discriminantAssociationSym, ok := symbolByName(lang, "discriminant_association")
	if !ok {
		return
	}
	expressionSym, ok := symbolByName(lang, "expression")
	if !ok {
		return
	}
	selectedComponentSym, selectedComponentNamed, ok := symbolMeta(lang, "selected_component")
	if !ok {
		return
	}
	tickSym, tickNamed, ok := symbolMeta(lang, "tick")
	if !ok {
		return
	}
	attributeDesignatorSym, attributeDesignatorNamed, ok := symbolMeta(lang, "attribute_designator")
	if !ok {
		return
	}
	identifierSym, identifierNamed, ok := symbolMeta(lang, "identifier")
	if !ok {
		return
	}
	dotSym, dotNamed, ok := symbolMeta(lang, ".")
	if !ok {
		return
	}
	accessSym, accessNamed, ok := symbolMeta(lang, "access")
	if !ok {
		return
	}

	walkResultTree(root, func(n *Node) {
		if n == nil || int(n.endByte) > len(source) || n.startByte > n.endByte {
			return
		}
		children := resultChildSliceForMutation(n)
		if len(children) != 3 || children[1] == nil {
			return
		}
		mid := children[1]
		text := adaTrimmedNodeText(mid, source)
		switch n.symbol {
		case discriminantConstraintSym:
			census.run("dispatch.ada.constraint-kind-election", func() {
				if mid.symbol == discriminantAssociationSym && bytes.Contains(text, []byte("'")) {
					assocChildren := resultChildSliceForMutation(mid)
					if len(assocChildren) == 0 {
						return
					}
					if len(assocChildren) == 1 && assocChildren[0] != nil && assocChildren[0].symbol == expressionSym {
						if rebuilt := adaAttributeReferenceChildren(n.ownerArena, source, assocChildren[0], selectedComponentSym, selectedComponentNamed, tickSym, tickNamed, attributeDesignatorSym, attributeDesignatorNamed, identifierSym, identifierNamed, dotSym, dotNamed, accessSym, accessNamed); len(rebuilt) > 0 {
							assocChildren = rebuilt
						} else if exprChildren := resultChildSliceForMutation(assocChildren[0]); len(exprChildren) > 0 {
							assocChildren = exprChildren
						}
					}
					out := make([]*Node, 0, len(assocChildren)+2)
					out = append(out, children[0])
					out = append(out, assocChildren...)
					out = append(out, children[2])
					n.symbol = indexConstraintSym
					n.setNamed(indexConstraintNamed)
					replaceNodeChildrenUnfielded(n, cloneNodeSliceIfArena(n.ownerArena, out))
					nodeInitEquivVersion(n)
				}
			})
		case namedArrayAggSym:
			census.run("dispatch.ada.aggregate-kind-election", func() {
				if mid.symbol == arrayAssocSym && bytes.Contains(text, []byte("=>")) && !adaAggregateTextStartsOthers(text) {
					n.symbol = recordAggSym
					n.setNamed(recordAggNamed)
					mid.symbol = recordAssocListSym
					mid.setNamed(recordAssocListNamed)
					normalizeAdaRecordAssociationChoicesWithCensus(mid, componentChoiceListSym, componentChoiceListNamed, discreteChoiceListSym, discreteChoiceSym, expressionSym, census)
					nodeInitEquivVersion(n)
					nodeInitEquivVersion(mid)
				}
			})
		case recordAggSym:
			census.run("dispatch.ada.aggregate-kind-election", func() {
				switch {
				case mid.symbol == recordAssocListSym && bytes.Contains(text, []byte("=>")) && adaAggregateTextStartsOthers(text):
					n.symbol = namedArrayAggSym
					n.setNamed(namedArrayAggNamed)
					mid.symbol = arrayAssocSym
					mid.setNamed(arrayAssocNamed)
					normalizeAdaArrayAssociationChoicesWithCensus(mid, componentChoiceListSym, discreteChoiceListSym, discreteChoiceListNamed, discreteChoiceSym, discreteChoiceNamed, census)
					nodeInitEquivVersion(n)
					nodeInitEquivVersion(mid)
				case mid.symbol == recordAssocListSym && !bytes.Contains(text, []byte("=>")) && bytes.Contains(text, []byte(",")):
					listChildren := resultChildSliceForMutation(mid)
					if len(listChildren) == 0 {
						return
					}
					out := make([]*Node, 0, len(listChildren)+2)
					out = append(out, children[0])
					out = append(out, listChildren...)
					out = append(out, children[2])
					n.symbol = positionalArrayAggSym
					n.setNamed(positionalArrayAggNamed)
					replaceNodeChildrenUnfielded(n, cloneNodeSliceIfArena(n.ownerArena, out))
					nodeInitEquivVersion(n)
				}
			})
		}
	})
}

func adaAttributeReferenceChildren(arena *nodeArena, source []byte, expr *Node, selectedComponentSym Symbol, selectedComponentNamed bool, tickSym Symbol, tickNamed bool, attributeDesignatorSym Symbol, attributeDesignatorNamed bool, identifierSym Symbol, identifierNamed bool, dotSym Symbol, dotNamed bool, accessSym Symbol, accessNamed bool) []*Node {
	if expr == nil || expr.startByte >= expr.endByte || int(expr.endByte) > len(source) {
		return nil
	}
	body := source[expr.startByte:expr.endByte]
	tickOffset := bytes.LastIndexByte(body, '\'')
	if tickOffset <= 0 || tickOffset+1 >= len(body) {
		return nil
	}
	tickStart := expr.startByte + uint32(tickOffset)
	tickEnd := tickStart + 1
	selected := adaSelectedComponentNode(arena, source, expr.startByte, tickStart, selectedComponentSym, selectedComponentNamed, identifierSym, identifierNamed, dotSym, dotNamed)
	if selected == nil {
		return nil
	}
	tick := newLeafNodeInArena(arena, tickSym, tickNamed, tickStart, tickEnd, selected.endPoint, advancePointByBytes(selected.endPoint, body[tickOffset:tickOffset+1]))
	attrIdent := newLeafNodeInArena(arena, accessSym, accessNamed, tickEnd, expr.endByte, tick.endPoint, expr.endPoint)
	attr := newParentNodeInArena(arena, attributeDesignatorSym, attributeDesignatorNamed, []*Node{attrIdent}, nil, 0)
	return cloneNodeSliceIfArena(arena, []*Node{selected, tick, attr})
}

func adaSelectedComponentNode(arena *nodeArena, source []byte, start, end uint32, selectedComponentSym Symbol, selectedComponentNamed bool, identifierSym Symbol, identifierNamed bool, dotSym Symbol, dotNamed bool) *Node {
	if start >= end || int(end) > len(source) {
		return nil
	}
	body := source[start:end]
	dotOffset := bytes.LastIndexByte(body, '.')
	if dotOffset <= 0 || dotOffset+1 >= len(body) {
		startPoint := advancePointByBytes(Point{}, source[:start])
		endPoint := advancePointByBytes(startPoint, body)
		return newLeafNodeInArena(arena, identifierSym, identifierNamed, start, end, startPoint, endPoint)
	}
	dotStart := start + uint32(dotOffset)
	left := adaSelectedComponentNode(arena, source, start, dotStart, selectedComponentSym, selectedComponentNamed, identifierSym, identifierNamed, dotSym, dotNamed)
	if left == nil {
		return nil
	}
	dot := newLeafNodeInArena(arena, dotSym, dotNamed, dotStart, dotStart+1, left.endPoint, advancePointByBytes(left.endPoint, []byte{'.'}))
	rightStart := dotStart + 1
	rightPoint := dot.endPoint
	right := newLeafNodeInArena(arena, identifierSym, identifierNamed, rightStart, end, rightPoint, advancePointByBytes(rightPoint, source[rightStart:end]))
	return newParentNodeInArena(arena, selectedComponentSym, selectedComponentNamed, []*Node{left, dot, right}, nil, 0)
}

func normalizeAdaRecordAssociationChoices(assoc *Node, componentChoiceListSym Symbol, componentChoiceListNamed bool, discreteChoiceListSym, discreteChoiceSym, expressionSym Symbol) {
	normalizeAdaRecordAssociationChoicesWithCensus(
		assoc,
		componentChoiceListSym,
		componentChoiceListNamed,
		discreteChoiceListSym,
		discreteChoiceSym,
		expressionSym,
		materializationSubpassCensus{},
	)
}

func normalizeAdaRecordAssociationChoicesWithCensus(
	assoc *Node,
	componentChoiceListSym Symbol,
	componentChoiceListNamed bool,
	discreteChoiceListSym Symbol,
	discreteChoiceSym Symbol,
	expressionSym Symbol,
	census materializationSubpassCensus,
) {
	walkResultTree(assoc, func(n *Node) {
		if n == nil || n.symbol != discreteChoiceListSym {
			return
		}
		census.run("dispatch.ada.association-choice-materialization", func() {
			n.symbol = componentChoiceListSym
			n.setNamed(componentChoiceListNamed)
			children := resultChildSliceForMutation(n)
			if len(children) == 1 && children[0] != nil && children[0].symbol == discreteChoiceSym {
				innerChildren := resultChildSliceForMutation(children[0])
				if len(innerChildren) == 1 && innerChildren[0] != nil {
					replaceNodeChildrenUnfielded(n, cloneNodeSliceIfArena(n.ownerArena, []*Node{innerChildren[0]}))
				}
			}
			adaCollapseSingleChildChoiceWrappers(n, expressionSym)
			nodeInitEquivVersion(n)
		})
	})
}

func adaCollapseSingleChildChoiceWrappers(choiceList *Node, firstWrapperSym Symbol) {
	children := resultChildSliceForMutation(choiceList)
	if len(children) != 1 || children[0] == nil || children[0].symbol != firstWrapperSym {
		return
	}
	for {
		children = resultChildSliceForMutation(choiceList)
		if len(children) != 1 || children[0] == nil {
			return
		}
		wrapper := children[0]
		innerChildren := resultChildSliceForMutation(wrapper)
		if len(innerChildren) != 1 || innerChildren[0] == nil {
			return
		}
		inner := innerChildren[0]
		if wrapper.startByte != inner.startByte || wrapper.endByte != inner.endByte {
			return
		}
		replaceNodeChildrenUnfielded(choiceList, cloneNodeSliceIfArena(choiceList.ownerArena, []*Node{inner}))
	}
}

func normalizeAdaArrayAssociationChoices(assoc *Node, componentChoiceListSym, discreteChoiceListSym Symbol, discreteChoiceListNamed bool, discreteChoiceSym Symbol, discreteChoiceNamed bool) {
	normalizeAdaArrayAssociationChoicesWithCensus(
		assoc,
		componentChoiceListSym,
		discreteChoiceListSym,
		discreteChoiceListNamed,
		discreteChoiceSym,
		discreteChoiceNamed,
		materializationSubpassCensus{},
	)
}

func normalizeAdaArrayAssociationChoicesWithCensus(
	assoc *Node,
	componentChoiceListSym Symbol,
	discreteChoiceListSym Symbol,
	discreteChoiceListNamed bool,
	discreteChoiceSym Symbol,
	discreteChoiceNamed bool,
	census materializationSubpassCensus,
) {
	walkResultTree(assoc, func(n *Node) {
		if n == nil || n.symbol != componentChoiceListSym {
			return
		}
		census.run("dispatch.ada.association-choice-materialization", func() {
			n.symbol = discreteChoiceListSym
			n.setNamed(discreteChoiceListNamed)
			children := resultChildSliceForMutation(n)
			if len(children) == 1 && children[0] != nil && children[0].symbol != discreteChoiceSym {
				wrapper := newParentNodeInArena(n.ownerArena, discreteChoiceSym, discreteChoiceNamed, []*Node{children[0]}, nil, 0)
				replaceNodeChildrenUnfielded(n, cloneNodeSliceIfArena(n.ownerArena, []*Node{wrapper}))
			}
			nodeInitEquivVersion(n)
		})
	})
}

func adaTrimmedNodeText(n *Node, source []byte) []byte {
	if n == nil || n.startByte > n.endByte || int(n.endByte) > len(source) {
		return nil
	}
	return bytes.TrimSpace(source[n.startByte:n.endByte])
}

func adaAggregateTextStartsOthers(text []byte) bool {
	if len(text) < len("others") || !bytes.EqualFold(text[:len("others")], []byte("others")) {
		return false
	}
	if len(text) == len("others") {
		return true
	}
	switch text[len("others")] {
	case ' ', '\t', '\r', '\n', '=':
		return true
	default:
		return false
	}
}
