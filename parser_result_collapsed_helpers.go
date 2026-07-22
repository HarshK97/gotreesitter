package gotreesitter

// normalizeCollapsedNamedLeafChildren restores collapsed single-anonymous-child
// nodes. When a named node (parentName) wraps a single anonymous token
// (childName) and the collapse logic strips the child, this function
// reconstructs the child so the tree matches C tree-sitter output.
func normalizeCollapsedNamedLeafChildren(root *Node, lang *Language, parentName, childName string) {
	normalizeCollapsedNamedLeafChildrenWithStats(root, lang, parentName, childName)
}

func normalizeCollapsedNamedLeafChildrenBySource(root *Node, source []byte, lang *Language, parentName string, childNames ...string) {
	normalizeCollapsedNamedLeafChildrenBySourceWithStats(root, source, lang, parentName, childNames...)
}

type collapsedNamedLeafRule struct {
	languageName string
	parentName   string
	childName    string
	// childNamed is part of the raw occurrence identity. Ruby and Kotlin keep
	// named grammar children; the keyword-wrapper rows keep anonymous tokens.
	// Ownership admission must not fall back across this boundary because a
	// generated alias may expose the same display name with different metadata.
	childNamed bool
	// bySource selects the source-verified matcher
	// (normalizeCollapsedNamedLeafChildrenBySource) instead of the plain
	// structural matcher (normalizeCollapsedNamedLeafChildren). Use this when
	// the parent's collapsed span must be confirmed to actually read as
	// childName before the child is attached (e.g. the parent symbol is
	// shared with unrelated collapsed shapes, or the rule was ported from a
	// language-specific adapter that verified source text).
	bySource bool
}

var resultCollapsedNamedLeafRules = []collapsedNamedLeafRule{
	// Ruby collapses bare_string/bare_symbol -> string_content unary wrappers
	// into a leaf; C keeps the single named string_content child over the same
	// span. Only fires when Go's node has zero children (i.e. no escapes /
	// interpolation, which would already materialize children in both trees).
	{languageName: "ruby", parentName: "bare_string", childName: "string_content", childNamed: true},
	{languageName: "ruby", parentName: "bare_symbol", childName: "string_content", childNamed: true},
	// Apex wraps its contextual / DML / trigger-event keywords as a named node
	// over the anonymous keyword token (`keyword -> 'keyword'`). Go collapses
	// these single-token wrappers to a leaf; C keeps the anonymous token child.
	// Verified exhaustively against the apex corpus: every collapsed instance's
	// C child is the same-named anonymous token over the identical span.
	{languageName: "apex", parentName: "after_delete", childName: "after_delete"},
	{languageName: "apex", parentName: "after_insert", childName: "after_insert"},
	{languageName: "apex", parentName: "after_undelete", childName: "after_undelete"},
	{languageName: "apex", parentName: "after_update", childName: "after_update"},
	{languageName: "apex", parentName: "before_delete", childName: "before_delete"},
	{languageName: "apex", parentName: "before_insert", childName: "before_insert"},
	{languageName: "apex", parentName: "before_update", childName: "before_update"},
	{languageName: "apex", parentName: "delete", childName: "delete"},
	{languageName: "apex", parentName: "insert", childName: "insert"},
	{languageName: "apex", parentName: "super", childName: "super"},
	{languageName: "apex", parentName: "system", childName: "system"},
	{languageName: "apex", parentName: "undelete", childName: "undelete"},
	{languageName: "apex", parentName: "upsert", childName: "upsert"},
	{languageName: "apex", parentName: "user", childName: "user"},
	// Kotlin's `identifier` is sep1 of `simple_identifier` by "."; a
	// single-element identifier (e.g. `import benchmarks.*`) collapses to a
	// leaf in Go but C always materializes the simple_identifier child.
	{languageName: "kotlin", parentName: "identifier", childName: "simple_identifier", childNamed: true},
	// Hack's true/false/null literals wrap the identically-named anonymous
	// keyword token (`true -> 'true'`, etc.); Go collapses the lone child.
	{languageName: "hack", parentName: "true", childName: "true", bySource: true},
	{languageName: "hack", parentName: "false", childName: "false", bySource: true},
	{languageName: "hack", parentName: "null", childName: "null", bySource: true},
	// Dart's `super`/`this` wrap the identically-named anonymous keyword
	// token; Go collapses the lone child, C keeps it.
	{languageName: "dart", parentName: "super", childName: "super", bySource: true},
	{languageName: "dart", parentName: "this", childName: "this", bySource: true},
	// Elixir's `nil` wraps the identically-named anonymous keyword token.
	{languageName: "elixir", parentName: "nil", childName: "nil", bySource: true},
}

func normalizeResultCollapsedNamedLeafChildren(root *Node, source []byte, lang *Language) normalizationPassCounters {
	var total normalizationPassCounters
	if root == nil || lang == nil {
		return total
	}
	for _, rule := range resultCollapsedNamedLeafRules {
		if rule.languageName != lang.Name {
			continue
		}
		if rule.bySource {
			stats := normalizeCollapsedNamedLeafChildrenBySourceAndNamednessWithStats(root, source, lang, rule.parentName, rule.childNamed, rule.childName)
			total.nodesVisited += stats.nodesVisited
			total.nodesRewritten += stats.nodesRewritten
			continue
		}
		stats := normalizeCollapsedNamedLeafChildrenAndNamednessWithStats(root, lang, rule.parentName, rule.childName, rule.childNamed)
		total.nodesVisited += stats.nodesVisited
		total.nodesRewritten += stats.nodesRewritten
	}
	return total
}

func normalizeCollapsedNamedLeafChildrenWithStats(root *Node, lang *Language, parentName, childName string) normalizationPassCounters {
	return normalizeCollapsedNamedLeafChildrenAndNamednessWithStats(root, lang, parentName, childName, false)
}

func normalizeCollapsedNamedLeafChildrenAndNamednessWithStats(root *Node, lang *Language, parentName, childName string, childNamed bool) normalizationPassCounters {
	var counters normalizationPassCounters
	if root == nil || lang == nil {
		return counters
	}
	parentSym, ok := lang.symbolByNameAndNamed(parentName, true)
	if !ok {
		return counters
	}
	childSym, childOk := lang.symbolByNameAndNamed(childName, childNamed)
	if !childOk {
		return counters
	}
	if lang.Name == "rust" {
		walkResultTreeSidecarFirst(root, func(n *Node) {
			counters.nodesVisited++
			childCount := resultChildCount(n)
			if n.symbol == parentSym && childCount == 0 {
				child := newLeafNodeInArena(n.ownerArena, childSym, childNamed, n.startByte, n.endByte, n.startPoint, n.endPoint)
				child.parent = n
				child.childIndex = 0
				n.children = cloneNodeSliceInArena(n.ownerArena, []*Node{child})
				counters.nodesRewritten++
			}
		})
		return counters
	}
	walkResultTree(root, func(n *Node) {
		counters.nodesVisited++
		childCount := resultChildCount(n)
		if n.symbol == parentSym && childCount == 0 {
			child := newLeafNodeInArena(n.ownerArena, childSym, childNamed, n.startByte, n.endByte, n.startPoint, n.endPoint)
			child.parent = n
			child.childIndex = 0
			n.children = cloneNodeSliceInArena(n.ownerArena, []*Node{child})
			counters.nodesRewritten++
		}
	})
	return counters
}

func normalizeCollapsedNamedLeafChildrenBySourceWithStats(root *Node, source []byte, lang *Language, parentName string, childNames ...string) normalizationPassCounters {
	return normalizeCollapsedNamedLeafChildrenBySourceAndNamednessWithStats(root, source, lang, parentName, false, childNames...)
}

func normalizeCollapsedNamedLeafChildrenBySourceAndNamednessWithStats(root *Node, source []byte, lang *Language, parentName string, expectedChildNamed bool, childNames ...string) normalizationPassCounters {
	var counters normalizationPassCounters
	if root == nil || lang == nil || len(source) == 0 || len(childNames) == 0 {
		return counters
	}
	parentSym, ok := lang.symbolByNameAndNamed(parentName, true)
	if !ok {
		return counters
	}
	childSyms := make(map[string]Symbol, len(childNames))
	childNamed := make(map[Symbol]bool, len(childNames))
	for _, childName := range childNames {
		childSym, ok := lang.symbolByNameAndNamed(childName, expectedChildNamed)
		if !ok {
			continue
		}
		childSyms[childName] = childSym
		childNamed[childSym] = symbolIsNamed(lang, childSym)
	}
	if len(childSyms) == 0 {
		return counters
	}
	if lang.Name == "rust" {
		walkResultTreeSidecarFirst(root, func(n *Node) {
			counters.nodesVisited++
			childCount := resultChildCount(n)
			if n.symbol != parentSym || childCount != 0 || int(n.startByte) > len(source) || int(n.endByte) > len(source) || n.startByte > n.endByte {
				return
			}
			childSym, ok := childSyms[string(source[n.startByte:n.endByte])]
			if !ok {
				return
			}
			child := newLeafNodeInArena(n.ownerArena, childSym, childNamed[childSym], n.startByte, n.endByte, n.startPoint, n.endPoint)
			child.parent = n
			child.childIndex = 0
			n.children = cloneNodeSliceInArena(n.ownerArena, []*Node{child})
			counters.nodesRewritten++
		})
		return counters
	}
	walkResultTree(root, func(n *Node) {
		counters.nodesVisited++
		childCount := resultChildCount(n)
		if n.symbol != parentSym || childCount != 0 || int(n.startByte) > len(source) || int(n.endByte) > len(source) || n.startByte > n.endByte {
			return
		}
		childSym, ok := childSyms[string(source[n.startByte:n.endByte])]
		if !ok {
			return
		}
		child := newLeafNodeInArena(n.ownerArena, childSym, childNamed[childSym], n.startByte, n.endByte, n.startPoint, n.endPoint)
		child.parent = n
		child.childIndex = 0
		n.children = cloneNodeSliceInArena(n.ownerArena, []*Node{child})
		counters.nodesRewritten++
	})
	return counters
}
