package gotreesitter

func normalizeMakeConditionalConsequenceFields(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "make" {
		return
	}
	consequenceID, ok := lang.FieldByName("consequence")
	if !ok {
		return
	}
	walkResultTree(root, func(n *Node) {
		switch n.Type(lang) {
		case "conditional", "elsif_directive", "else_directive":
			ensureNodeFieldStorage(n, len(n.children))
			fieldIDs := n.fieldIDs()
			fieldSources := n.fieldSources()
			start, end := -1, -1
			for i := 0; i < len(n.children); i++ {
				if fieldIDs[i] != consequenceID {
					continue
				}
				if start < 0 {
					start = i
				}
				end = i
			}
			if start >= 0 && end >= start {
				for start > 0 {
					prev := n.children[start-1]
					if prev == nil || prev.isNamed() || prev.isExtra() || prev.Type(lang) != "\t" {
						break
					}
					start--
				}
				for i := start; i <= end; i++ {
					if n.children[i] == nil {
						continue
					}
					fieldIDs[i] = consequenceID
					if len(fieldSources) == len(n.children) {
						fieldSources[i] = fieldSourceDirect
					}
				}
			}
		}
	})
}
