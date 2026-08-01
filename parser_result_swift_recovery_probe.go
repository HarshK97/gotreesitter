package gotreesitter

// swiftConditionRecoveryCanReachCleanTree declines a whole-source recovery
// when a terminal diagnostic occurs before every proposed control header.
// Later parentheses cannot repair that earlier source region.
func swiftConditionRecoveryCanReachCleanTree(root *Node, source []byte, inserts []swiftParenInsert) bool {
	if root == nil || !root.HasError() {
		return true
	}
	firstDiagnostic, foundDiagnostic := swiftFirstTerminalDiagnosticStart(root)
	if !foundDiagnostic {
		return false
	}
	firstHeader, foundHeader := swiftFirstConditionRecoveryHeader(source, inserts)
	return foundHeader && firstDiagnostic >= firstHeader
}

func swiftFirstConditionRecoveryHeader(source []byte, inserts []swiftParenInsert) (uint32, bool) {
	var first uint32
	found := false
	for _, insert := range inserts {
		if insert.ch != '(' {
			continue
		}
		header, ok := swiftLastControlHeaderBefore(source, insert.pos)
		if !ok {
			continue
		}
		if !found || header < first {
			first = header
			found = true
		}
	}
	return first, found
}

func swiftLastControlHeaderBefore(source []byte, end uint32) (uint32, bool) {
	if end > uint32(len(source)) {
		end = uint32(len(source))
	}
	var last uint32
	found := false
	for i := uint32(0); i < end; {
		if next, code := swiftSkipStringOrComment(source, i); !code {
			i = next
			continue
		}
		if !isSwiftWordByte(source[i]) || (i > 0 && isSwiftWordByte(source[i-1])) {
			i++
			continue
		}
		j := i + 1
		for j < end && isSwiftWordByte(source[j]) {
			j++
		}
		switch string(source[i:j]) {
		case "if", "while", "for":
			last = i
			found = true
		}
		i = j
	}
	return last, found
}

// swiftTernaryRecoveryCanReachCleanTree declines a whole-source recovery
// when a terminal diagnostic is outside every blanked ternary tail.
func swiftTernaryRecoveryCanReachCleanTree(root *Node, blankRanges [][2]uint32) bool {
	if root == nil || len(blankRanges) == 0 {
		return false
	}
	found := false
	allowed := true
	swiftVisitTerminalDiagnostics(root, func(n *Node) {
		found = true
		if !swiftTerminalDiagnosticOverlapsRanges(n, blankRanges) {
			allowed = false
		}
	})
	return found && allowed
}

// swiftTopLevelRecoveryCanRepairChild keeps the declaration recovery scoped to
// one terminal diagnostic. A multi-error child has no single recovery target.
func swiftTopLevelRecoveryCanRepairChild(child *Node) bool {
	if child == nil || !child.HasError() {
		return false
	}
	terminalDiagnostics := 0
	swiftVisitTerminalDiagnostics(child, func(*Node) {
		terminalDiagnostics++
	})
	return terminalDiagnostics == 1
}

func swiftTerminalDiagnosticOverlapsRanges(n *Node, ranges [][2]uint32) bool {
	if n == nil {
		return false
	}
	for _, span := range ranges {
		if n.startByte == n.endByte {
			if n.startByte >= span[0] && n.startByte <= span[1] {
				return true
			}
			continue
		}
		if n.startByte < span[1] && span[0] < n.endByte {
			return true
		}
	}
	return false
}

func swiftFirstTerminalDiagnosticStart(root *Node) (uint32, bool) {
	var first uint32
	found := false
	swiftVisitTerminalDiagnostics(root, func(n *Node) {
		if !found || n.startByte < first {
			first = n.startByte
			found = true
		}
	})
	return first, found
}

func swiftVisitTerminalDiagnostics(root *Node, visit func(*Node)) bool {
	if root == nil {
		return false
	}
	childHasDiagnostic := false
	for i := 0; i < resultChildCount(root); i++ {
		if swiftVisitTerminalDiagnostics(resultChildAt(root, i), visit) {
			childHasDiagnostic = true
		}
	}
	selfDiagnostic := root.IsError() || root.IsMissing()
	if selfDiagnostic && !childHasDiagnostic && visit != nil {
		visit(root)
	}
	return selfDiagnostic || childHasDiagnostic
}
