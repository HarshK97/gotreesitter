package gotreesitter

// collapsedChildSymbolPair is one exact public-wrapper/raw-child identity that
// C tree-sitter retains as a unary node. The policy is compiled once per Parser
// only after the exact SHA-pinned built-in runtime profile authenticates it.
type collapsedChildSymbolPair struct {
	parent Symbol
	child  Symbol
}

func collapsedChildPairKey(parent, child Symbol) uint32 {
	return uint32(parent)<<16 | uint32(child)
}

func compileCollapsedChildOccurrencePolicy(lang *Language) ([]collapsedChildSymbolPair, map[uint32]struct{}) {
	if lang == nil || lang.NativeResultCompatibility&ResultCompatibilityNativeCollapsedChildren == 0 {
		return nil, nil
	}
	pairs := make([]collapsedChildSymbolPair, 0, 4)
	seen := make(map[uint32]struct{})
	for _, rule := range resultCollapsedNamedLeafRules {
		if rule.languageName != lang.Name {
			continue
		}
		parent, ok := lang.symbolByNameAndNamed(rule.parentName, true)
		if !ok {
			parent, ok = symbolByName(lang, rule.parentName)
		}
		if !ok {
			continue
		}
		child, ok := lang.symbolByNameAndNamed(rule.childName, false)
		if !ok {
			child, ok = symbolByName(lang, rule.childName)
		}
		if !ok {
			continue
		}
		key := collapsedChildPairKey(parent, child)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		pairs = append(pairs, collapsedChildSymbolPair{parent: parent, child: child})
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return pairs, seen
}

// retainsCollapsedChildOccurrence is the single native keep/wrap decision.
// Its authenticated (parent, raw-child) domain is strictly narrower than the
// legacy safety net, which may repair a collapsed parent leaf (and, for some
// rows, verify source text) after the raw child identity has been lost.
func (p *Parser) retainsCollapsedChildOccurrence(parent, rawChild Symbol) bool {
	if p == nil || len(p.collapsedChildOccurrenceSet) == 0 {
		return false
	}
	_, ok := p.collapsedChildOccurrenceSet[collapsedChildPairKey(parent, rawChild)]
	return ok
}
