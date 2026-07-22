package gotreesitter

func (p *Parser) beginParseOperationBudget() func() {
	return p.enterParseBudget()
}

func (p *Parser) parseStopReasonNow() ParseStopReason {
	return p.activeParseStopReason()
}

func parseStopReasonIsTerminal(reason ParseStopReason) bool {
	return parseStopReasonIsActive(reason) || reason == ParseStopInvariantViolation
}
