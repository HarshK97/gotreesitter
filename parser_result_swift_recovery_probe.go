package gotreesitter

// swiftCleanRecoveryProbeMaxTerminalDiagnostics limits a full-source probe to
// a small, recoverable diagnostic set. A larger set has no single target.
const swiftCleanRecoveryProbeMaxTerminalDiagnostics = 8

// swiftConditionRecoveryCanReachCleanTree declines a whole-source recovery
// when the raw tree has many terminal diagnostics. A small set can result
// from one trailing-closure collapse before the affected control header.
func swiftConditionRecoveryCanReachCleanTree(root *Node) bool {
	if root == nil {
		return false
	}
	if !root.HasError() {
		return true
	}
	terminalDiagnostics := swiftTerminalDiagnosticCount(root)
	return terminalDiagnostics > 0 && terminalDiagnostics <= swiftCleanRecoveryProbeMaxTerminalDiagnostics
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
	return swiftTerminalDiagnosticCount(child) == 1
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

func swiftTerminalDiagnosticCount(root *Node) int {
	count := 0
	swiftVisitTerminalDiagnostics(root, func(*Node) {
		count++
	})
	return count
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
