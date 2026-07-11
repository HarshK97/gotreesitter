package gotreesitter

import "strings"

func normalizeHyprlangCompatibility(root *Node, source []byte, lang *Language) {
	normalizeHyprlangBooleanAssignmentValues(root, source, lang)
}

func normalizeHyprlangBooleanAssignmentValues(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || lang.Name != "hyprlang" || len(source) == 0 {
		return
	}
	assignmentSym, ok := lang.symbolByNameAndNamed("assignment", true)
	if !ok {
		return
	}
	stringSym, ok := lang.symbolByNameAndNamed("string", true)
	if !ok {
		return
	}
	booleanSym, ok := lang.symbolByNameAndNamed("boolean", true)
	if !ok {
		return
	}
	equalsSym, ok := lang.symbolByNameAndNamed("=", false)
	if !ok {
		return
	}
	booleanChildren := make(map[string]Symbol, 6)
	for _, name := range []string{"true", "false", "on", "off", "yes", "no"} {
		if sym, ok := lang.symbolByNameAndNamed(name, false); ok {
			booleanChildren[name] = sym
		}
	}
	if len(booleanChildren) == 0 {
		return
	}
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != assignmentSym {
			return
		}
		for i := 1; i < resultChildCount(n); i++ {
			prev := resultChildAt(n, i-1)
			child := resultChildAt(n, i)
			if prev == nil || child == nil || prev.symbol != equalsSym || child.symbol != stringSym || resultChildCount(child) != 0 {
				continue
			}
			if child.startByte > child.endByte || int(child.endByte) > len(source) {
				continue
			}
			lit := strings.TrimSpace(string(source[child.startByte:child.endByte]))
			childSym, ok := booleanChildren[lit]
			if !ok {
				continue
			}
			leaf := newLeafNodeInArena(child.ownerArena, childSym, false, child.startByte, child.endByte, child.startPoint, child.endPoint)
			leaf.parent = child
			leaf.childIndex = 0
			child.symbol = booleanSym
			child.setNamed(true)
			child.children = cloneNodeSliceInArena(child.ownerArena, []*Node{leaf})
			child.clearFieldMetadata()
			if child.ownerArena != nil {
				child.ownerArena.clearFinalChildRefs(child)
			}
		}
	})
}
