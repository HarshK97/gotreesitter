package gotreesitter

type parseOperationBudgetState struct {
	endBudget func()
}

func (p *Parser) beginParseOperationBudget() parseOperationBudgetState {
	if p == nil {
		return parseOperationBudgetState{}
	}
	if p.cNodeMemoOperationDepth == 0 {
		p.cNodeMemoOperationPeakTier = RecoveryNodeMemoTierNone
		if cold := p.forestDeclineMemo; cold != nil {
			cold.cNodeMemoCollisions = 0
		}
	}
	p.cNodeMemoOperationDepth++
	state := parseOperationBudgetState{}
	if p.needsParseBudget() {
		state.endBudget = p.enterParseBudget()
	}
	return state
}

func (p *Parser) endParseOperationBudget(state parseOperationBudgetState) {
	if state.endBudget != nil {
		state.endBudget()
	}
	if p == nil {
		return
	}
	p.cNodeMemoOperationDepth--
	if p.cNodeMemoOperationDepth == 0 {
		p.finishCNodeMemoParse()
	}
}

func (p *Parser) parseStopReasonNow() ParseStopReason {
	return p.activeParseStopReason()
}

func parseStopReasonIsTerminal(reason ParseStopReason) bool {
	return parseStopReasonIsActive(reason) || reason == ParseStopInvariantViolation
}
