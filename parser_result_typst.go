package gotreesitter

func normalizeTypstCompatibility(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "typst" {
		return
	}
	if len(source) > 0 && root.endByte != uint32(len(source)) {
		return
	}
	if root.hasError() {
		return
	}
	contentSym, ok := symbolByName(lang, "content")
	if !ok {
		return
	}
	itemSym, ok := symbolByName(lang, "item")
	if !ok {
		return
	}
	normalizeTypstNestedItems(root, contentSym, itemSym)
}

func normalizeTypstNestedItems(node *Node, contentSym, itemSym Symbol) {
	if node == nil {
		return
	}
	for i := 0; i < resultChildCount(node); i++ {
		normalizeTypstNestedItems(resultChildAt(node, i), contentSym, itemSym)
	}
	if node.symbol != contentSym || resultChildCount(node) < 2 {
		return
	}
	typstMergeNestedItemSiblings(node, itemSym)
}

type typstItemStackEntry struct {
	node   *Node
	column uint32
}

func typstMergeNestedItemSiblings(content *Node, itemSym Symbol) {
	children := resultChildSliceForMutation(content)
	if len(children) < 2 {
		return
	}
	startByte := content.startByte
	endByte := content.endByte
	startPoint := content.startPoint
	endPoint := content.endPoint

	out := make([]*Node, 0, len(children))
	stack := make([]typstItemStackEntry, 0, 4)
	changed := false
	for _, child := range children {
		if child == nil || child.symbol != itemSym {
			out = append(out, child)
			stack = stack[:0]
			continue
		}

		col := child.startPoint.Column
		for len(stack) > 0 && stack[len(stack)-1].column >= col {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			out = append(out, child)
			stack = append(stack, typstItemStackEntry{node: child, column: col})
			continue
		}

		parent := stack[len(stack)-1].node
		typstAppendNestedItem(parent, child)
		for i := range stack {
			typstExtendItemSpan(stack[i].node, child)
		}
		stack = append(stack, typstItemStackEntry{node: child, column: col})
		changed = true
	}
	if !changed {
		return
	}
	replaceNodeChildrenUnfielded(content, cloneNodeSliceInArena(content.ownerArena, out))
	content.startByte = startByte
	content.endByte = endByte
	content.startPoint = startPoint
	content.endPoint = endPoint
}

func typstAppendNestedItem(parent, child *Node) {
	if parent == nil || child == nil {
		return
	}
	children := resultChildSliceForMutation(parent)
	children = append(append([]*Node{}, children...), child)
	replaceNodeChildrenUnfielded(parent, cloneNodeSliceInArena(parent.ownerArena, children))
}

func typstExtendItemSpan(parent, child *Node) {
	if parent == nil || child == nil || child.endByte <= parent.endByte {
		return
	}
	parent.endByte = child.endByte
	parent.endPoint = child.endPoint
}
