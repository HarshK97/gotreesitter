package gotreesitter

// normalizeResultTerminalLeafNodesWithStopAndErrorSummary removes redundant
// terminal payload children that can be introduced by generalized
// reduction/materialization paths. C tree-sitter exposes terminal and
// terminal-alias symbols as leaves, so a visible leaf-like parent with one
// visible anonymous terminal child and no semantic decorations should keep
// the parent's symbol/flags and adopt the child's token span.
func normalizeResultTerminalLeafNodesWithStopAndErrorSummary(root *Node, lang *Language, stopCheck parseStopCheck) (normalizationPassCounters, ParseStopReason, resultErrorSummary) {
	var counters normalizationPassCounters
	if root == nil || lang == nil {
		return counters, ParseStopNone, resultErrorSummaryUnknown
	}
	if root.hasError() {
		return counters, ParseStopNone, resultErrorSummaryPresent
	}
	errorSummary := resultErrorSummaryClean
	if root.IsError() {
		errorSummary = resultErrorSummaryPresent
	}
	poller := parseStopPoller{check: stopCheck}
	if reason := poller.pollNow(); parseStopReasonIsActive(reason) {
		if errorSummary != resultErrorSummaryPresent {
			errorSummary = resultErrorSummaryUnknown
		}
		return counters, reason, errorSummary
	}
	aliasTargets := resultVisibleAliasTargetSet(lang)
	var stopReason ParseStopReason
	walkResultTreeUntil(root, func(n *Node) bool {
		if reason := poller.poll(); parseStopReasonIsActive(reason) {
			stopReason = reason
			return false
		}
		counters.nodesVisited++
		if n.IsError() || n.hasError() {
			errorSummary = resultErrorSummaryPresent
		}
		if !resultCanCollapseTerminalLeafNode(n, lang, aliasTargets) {
			return true
		}
		child := resultChildAt(n, 0)
		if !resultCanCollapseTerminalLeafChild(child, lang, aliasTargets) {
			return true
		}
		if (!resultSymbolIsVisibleTerminal(lang, n.symbol) || resultSymbolIsVisibleAliasTarget(lang, n.symbol, aliasTargets)) && !resultSymbolNamesEqual(lang, n.symbol, child.symbol) {
			// n only qualified via the alias-target set, which just records
			// "this symbol is SOME production's alias somewhere in the
			// grammar" -- it is not scoped to this specific reduction. A
			// same-named anonymous child (e.g. an aliased "]" wrapping a "]"
			// token) is the genuine inlined-terminal case C tree-sitter
			// collapses to a bare leaf. A DIFFERENT-named child (e.g. ruby's
			// splat_parameter wrapping a bare "*" token) is a real, distinct
			// production whose child C tree-sitter keeps as a visible node --
			// collapsing it here would silently drop a real AST child.
			//
			// Grammars can also reuse a genuine visible-terminal symbol ID as
			// an alias target elsewhere (e.g. norg's "_word" alias reuses a
			// symbol ID that also denotes a real terminal). In that case
			// resultSymbolIsVisibleTerminal(n.symbol) alone is true and used
			// to short-circuit this guard, even though -- for this specific
			// reduction -- n is standing in for the alias, not the terminal.
			// Treat "also a visible alias target" the same as "not a visible
			// terminal": require matching names before collapsing so a
			// distinct-named child under a reused ID is preserved too.
			return true
		}
		n.startByte = child.startByte
		n.endByte = child.endByte
		n.startPoint = child.startPoint
		n.endPoint = child.endPoint
		n.children = nil
		n.clearFieldMetadata()
		if n.ownerArena != nil {
			n.ownerArena.clearFinalChildRefs(n)
		}
		counters.nodesRewritten++
		return true
	})
	if parseStopReasonIsActive(stopReason) {
		if errorSummary != resultErrorSummaryPresent {
			errorSummary = resultErrorSummaryUnknown
		}
		return counters, stopReason, errorSummary
	}
	stopReason = poller.pollNow()
	if parseStopReasonIsActive(stopReason) && errorSummary != resultErrorSummaryPresent {
		errorSummary = resultErrorSummaryUnknown
	}
	return counters, stopReason, errorSummary
}

func resultCanCollapseTerminalLeafNode(n *Node, lang *Language, aliasTargets []bool) bool {
	if n == nil || lang == nil || n.isExtra() || n.isMissing() || n.hasError() {
		return false
	}
	if !resultSymbolIsVisibleTerminalLeafLike(lang, n.symbol, aliasTargets) {
		return false
	}
	if resultChildCount(n) != 1 {
		return false
	}
	return !resultNodeHasChildFields(n)
}

func resultCanCollapseTerminalLeafChild(child *Node, lang *Language, aliasTargets []bool) bool {
	if child == nil || lang == nil || child.isNamed() || child.isExtra() || child.isMissing() || child.hasError() {
		return false
	}
	return resultSymbolIsVisibleTerminalLeafLike(lang, child.symbol, aliasTargets)
}

func resultSymbolIsVisibleTerminalLeafLike(lang *Language, sym Symbol, aliasTargets []bool) bool {
	return resultSymbolIsVisibleTerminal(lang, sym) || resultSymbolIsVisibleAliasTarget(lang, sym, aliasTargets)
}

func resultSymbolIsVisibleTerminal(lang *Language, sym Symbol) bool {
	return lang != nil && uint32(sym) < lang.TokenCount && symbolIsVisible(lang, sym)
}

func resultSymbolIsVisibleAliasTarget(lang *Language, sym Symbol, aliasTargets []bool) bool {
	if lang == nil || int(sym) >= len(aliasTargets) {
		return false
	}
	return aliasTargets[sym]
}

func resultVisibleAliasTargetSet(lang *Language) []bool {
	if lang == nil || len(lang.AliasSequences) == 0 {
		return nil
	}
	aliasTargets := make([]bool, len(lang.SymbolNames))
	for _, seq := range lang.AliasSequences {
		for _, alias := range seq {
			if int(alias) < len(aliasTargets) && symbolIsVisible(lang, alias) {
				aliasTargets[alias] = true
			}
		}
	}
	return aliasTargets
}

// resultSymbolNamesEqual mirrors Parser.sameSymbolName for callers (like this
// file's normalization pass) that only have a *Language, not a *Parser.
func resultSymbolNamesEqual(lang *Language, a, b Symbol) bool {
	if lang == nil {
		return false
	}
	meta := lang.SymbolMetadata
	if int(a) < len(meta) && int(b) < len(meta) {
		an, bn := meta[a].Name, meta[b].Name
		if an != "" && bn != "" {
			return an == bn
		}
	}
	names := lang.SymbolNames
	if int(a) >= len(names) || int(b) >= len(names) {
		return false
	}
	return names[a] == names[b]
}

func resultNodeHasChildFields(n *Node) bool {
	if n == nil {
		return false
	}
	for _, fid := range n.fieldIDs() {
		if fid != 0 {
			return true
		}
	}
	for _, source := range n.fieldSources() {
		if source != fieldSourceNone {
			return true
		}
	}
	return false
}
