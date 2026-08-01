package gotreesitter

import (
	"bytes"
	"os"
)

// normalizeSwiftCompatibility recovers the leading control-keyword token that
// grammargen's reduce path drops from `control_transfer_statement` nodes for
// the `return <expr>` case: existing children are present but the keyword
// leaf is missing as the first child and the span starts at the result
// expression (`return 42`). The bare-keyword case (`return` with no result
// expression) no longer needs a post-hoc patch here:
// shouldKeepVisibleAnonymousTokenChild keeps different-named
// single-token-wrapper anonymous children unconditionally, so the reduce
// engine now preserves that keyword child natively (see
// TestSwiftBareControlTransferKeywordChild in grammars/).
func normalizeSwiftCompatibility(root *Node, source []byte, p *Parser, lang *Language) {
	normalizeSwiftCompatibilityWithCensus(root, source, p, lang, materializationSubpassCensus{})
}

func normalizeSwiftCompatibilityWithCensus(root *Node, source []byte, p *Parser, lang *Language, census materializationSubpassCensus) {
	if root == nil || lang == nil || lang.Name != "swift" {
		return
	}
	census.run("dispatch.swift.conditions", func() {
		normalizeSwiftRecoveredTrailingClosureConditions(root, source, p, lang)
	})
	census.run("dispatch.swift.ternary", func() {
		normalizeSwiftRecoveredTernaryExpressions(root, source, p, lang)
	})
	census.run("dispatch.swift.top-level", func() {
		normalizeSwiftRecoveredTopLevelDeclarations(root, source, p, lang)
	})
	// `return <expr>` case: existing children present but the keyword leaf is
	// missing as the first child and the span starts at the result expression.
	census.run("dispatch.swift.control", func() {
		prependSwiftControlTransferKeyword(root, source, lang)
	})
}

const swiftCleanRecoveryProbeModeEnv = "GOT_SWIFT_CLEAN_RECOVERY_PROBE_MODE"

// parseSwiftCleanFullSourceRecovery runs the initial-only probe for the two
// whole-source Swift recoveries with a clean, full-span gate. Set the mode to
// "legacy" only to compare the old route in a test or a diagnostic run.
func parseSwiftCleanFullSourceRecovery(p *Parser, source []byte, lang *Language) (*Tree, error) {
	if p == nil || lang == nil {
		return nil, ErrNoLanguage
	}
	if os.Getenv(swiftCleanRecoveryProbeModeEnv) == "legacy" {
		return p.parseForRecovery(source)
	}
	return p.parseForRecoveryInitialOnlyOrLegacy(source, func(tree *Tree) bool {
		return swiftCleanFullSourceRecoveryAccepted(tree, source, lang)
	})
}

func swiftCleanFullSourceRecoveryAccepted(tree *Tree, source []byte, lang *Language) bool {
	if tree == nil || lang == nil {
		return false
	}
	root := tree.RootNode()
	return root != nil && !root.HasError() && root.Type(lang) == "source_file" && root.endByte == uint32(len(source))
}

func normalizeSwiftRecoveredTopLevelDeclarations(root *Node, source []byte, p *Parser, lang *Language) {
	if root == nil || p == nil || p.skipRecoveryReparse || lang == nil || root.ownerArena == nil || len(source) == 0 {
		return
	}
	if root.Type(lang) != "source_file" || len(root.children) == 0 {
		return
	}
	recoveredChildren := make([]*Node, 0, len(root.children))
	changed := false
	for i, child := range root.children {
		if child == nil {
			continue
		}
		if !child.HasError() {
			recoveredChildren = append(recoveredChildren, child)
			continue
		}
		if !swiftTopLevelRecoveryCanRepairChild(child) {
			recoveredChildren = append(recoveredChildren, child)
			continue
		}
		start := child.startByte
		end := uint32(len(source))
		if i+1 < len(root.children) && root.children[i+1] != nil && root.children[i+1].startByte > start {
			end = root.children[i+1].startByte
		}
		if recovered, ok := swiftRecoverTopLevelDeclarationFromRange(source, start, end, p, lang, root.ownerArena); ok {
			recoveredChildren = append(recoveredChildren, recovered)
			changed = true
			continue
		}
		recoveredChildren = append(recoveredChildren, child)
	}
	if !changed {
		return
	}
	root.children = cloneNodeSliceIfArena(root.ownerArena, recoveredChildren)
	populateParentNode(root, root.children)
	if !swiftAnyChildHasError(root) {
		root.setHasError(false)
	}
	if len(root.children) > 0 {
		last := root.children[len(root.children)-1]
		if last != nil && last.endByte > root.endByte {
			root.endByte = last.endByte
			root.endPoint = last.endPoint
		}
	}
}

func swiftRecoverTopLevelDeclarationFromRange(source []byte, start, end uint32, p *Parser, lang *Language, arena *nodeArena) (*Node, bool) {
	if p == nil || lang == nil || arena == nil || start >= end || int(end) > len(source) {
		return nil, false
	}
	start, end = swiftTrimSpaceBounds(source, start, end)
	if start >= end {
		return nil, false
	}
	tree, err := p.parseForRecovery(source[start:end])
	p.recordSwiftLegacyRecoverySubparse(p.recoveryParserRetryPasses())
	if err != nil || tree == nil || tree.RootNode() == nil {
		if tree != nil {
			tree.Release()
		}
		return nil, false
	}
	defer tree.Release()
	startPoint := advancePointByBytes(Point{}, source[:start])
	offsetRoot := tree.RootNodeWithOffset(start, startPoint)
	if offsetRoot == nil || offsetRoot.HasError() {
		return nil, false
	}
	for _, child := range offsetRoot.children {
		if child == nil || child.IsExtra() || child.HasError() {
			continue
		}
		return cloneTreeNodesIntoArena(child, arena), true
	}
	return nil, false
}

func swiftTrimSpaceBounds(source []byte, start, end uint32) (uint32, uint32) {
	for start < end && int(start) < len(source) {
		switch source[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto trimRight
		}
	}
trimRight:
	for end > start && int(end) <= len(source) {
		switch source[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return start, end
		}
	}
	return start, end
}

func swiftAnyChildHasError(root *Node) bool {
	if root == nil {
		return false
	}
	for _, child := range root.children {
		if child != nil && child.HasError() {
			return true
		}
	}
	return false
}

func prependSwiftControlTransferKeyword(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || len(source) == 0 {
		return
	}
	ctsSym, ok := lang.symbolByNameAndNamed("control_transfer_statement", true)
	if !ok {
		ctsSym, ok = symbolByName(lang, "control_transfer_statement")
		if !ok {
			return
		}
	}
	keywordSyms := map[string]Symbol{}
	for _, kw := range []string{"return", "continue", "break", "yield"} {
		s, ok := lang.symbolByNameAndNamed(kw, false)
		if !ok {
			s, ok = symbolByName(lang, kw)
			if !ok {
				continue
			}
		}
		keywordSyms[kw] = s
	}
	if len(keywordSyms) == 0 {
		return
	}
	// Walk top-down with an explicit ancestor stack so we can extend ancestor
	// spans (parent links aren't wired yet at this point in result building).
	var path []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		path = append(path, n)
		defer func() { path = path[:len(path)-1] }()
		if n.symbol == ctsSym && len(n.children) > 0 {
			first := n.children[0]
			isKeywordChild := false
			if first != nil {
				if _, ok := keywordSyms[first.Type(lang)]; ok {
					isKeywordChild = true
				}
			}
			if !isKeywordChild {
				if kw, kwEnd, ok := findSwiftLeadingControlKeyword(source, n.startByte, keywordSyms); ok {
					sym := keywordSyms[kw]
					keywordEnd := n.startByte - uint32(kwEnd-len(kw))
					keywordStart := keywordEnd - uint32(len(kw))
					leaf := newLeafNodeInArena(
						n.ownerArena, sym, false,
						keywordStart, keywordEnd,
						n.startPoint, n.startPoint,
					)
					leaf.parent = n
					leaf.childIndex = 0
					newChildren := make([]*Node, 0, len(n.children)+1)
					newChildren = append(newChildren, leaf)
					for i, c := range n.children {
						if c != nil {
							c.childIndex = int32(i + 1)
						}
						newChildren = append(newChildren, c)
					}
					n.children = cloneNodeSliceInArena(n.ownerArena, newChildren)
					// Extend n and its ancestors that share the old start.
					oldStart := n.startByte
					n.startByte = keywordStart
					for i := len(path) - 2; i >= 0; i-- {
						p := path[i]
						if p == nil || p.startByte != oldStart {
							break
						}
						p.startByte = keywordStart
					}
				}
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
}

// findSwiftLeadingControlKeyword scans source backwards from rhsStart, skipping
// horizontal whitespace, to find one of the swift control keywords. Returns
// the matched keyword string and the offset (in bytes) from the rhsStart to
// where the keyword ends (i.e. how many bytes of whitespace were skipped + the
// keyword length).
func findSwiftLeadingControlKeyword(source []byte, rhsStart uint32, keywordSyms map[string]Symbol) (string, int, bool) {
	if int(rhsStart) > len(source) {
		return "", 0, false
	}
	pos := int(rhsStart)
	// Skip trailing whitespace right before rhsStart.
	for pos > 0 {
		c := source[pos-1]
		if c != ' ' && c != '\t' {
			break
		}
		pos--
	}
	for _, kw := range []string{"return", "continue", "break", "yield"} {
		if _, ok := keywordSyms[kw]; !ok {
			continue
		}
		if pos < len(kw) {
			continue
		}
		if !bytes.Equal(source[pos-len(kw):pos], []byte(kw)) {
			continue
		}
		// Ensure the byte before kw is a word boundary.
		if pos-len(kw) > 0 {
			prev := source[pos-len(kw)-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' {
				continue
			}
		}
		// Return how far rhsStart is from the END of the keyword.
		return kw, int(rhsStart) - (pos - len(kw)), true
	}
	return "", 0, false
}
