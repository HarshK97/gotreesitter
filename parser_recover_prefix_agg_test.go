package gotreesitter

import "testing"

func TestMetadataVersionBumpKeepsRecoveryPrefixAggregate(t *testing.T) {
	lang := &Language{SymbolMetadata: []SymbolMetadata{{}, {Visible: true}}}
	p := &Parser{
		language:  lang,
		cNodeMemo: make(map[*Node]cNodeMemoEntry),
	}
	payload := &Node{symbol: 1, equivVersion: 1, errorRankCache: 3}
	head := &gssNode{entry: newStackEntryNode(1, payload), depth: 1}

	cost, visible := p.cStackPrefixAgg(head)
	if cost != 0 || visible != 1 {
		t.Fatalf("initial aggregate = (%d, %d), want (0, 1)", cost, visible)
	}
	gen := gssPrefixAggGen.Load()
	if head.aggGen != gen || head.aggVisGen != gen {
		t.Fatalf("initial cache generation = (%d, %d), want %d", head.aggGen, head.aggVisGen, gen)
	}

	payload.parseState = 2
	nodeBumpEquivVersionMetadata(payload)
	if payload.equivVersion != 2 {
		t.Fatalf("equiv version = %d, want 2", payload.equivVersion)
	}
	if payload.errorRankCache != 0 {
		t.Fatalf("error-rank cache = %d, want invalidated", payload.errorRankCache)
	}
	if got := gssPrefixAggGen.Load(); got != gen {
		t.Fatalf("metadata bump changed prefix generation from %d to %d", gen, got)
	}
	if cost, visible = p.cStackPrefixAgg(head); cost != 0 || visible != 1 {
		t.Fatalf("post-metadata aggregate = (%d, %d), want (0, 1)", cost, visible)
	}

	payload.setMissing(true)
	nodeBumpEquivVersion(payload)
	if got := gssPrefixAggGen.Load(); got == gen {
		t.Fatalf("recovery-relevant bump left prefix generation at %d", got)
	}
	cost, visible = p.cStackPrefixAgg(head)
	wantCost := uint32(cErrCostPerMissingTree + cErrCostPerRecovery)
	if cost != wantCost || visible != 1 {
		t.Fatalf("post-missing aggregate = (%d, %d), want (%d, 1)", cost, visible, wantCost)
	}
}

func TestSetGSSMainLinkInvalidatesOnlyChangedRecoveryContribution(t *testing.T) {
	prev := &gssNode{depth: 1}
	payload := &Node{symbol: 1, equivVersion: 1}
	head := &gssNode{
		prev:      prev,
		entry:     newStackEntryNode(1, payload),
		depth:     2,
		aggGen:    gssPrefixAggGen.Load(),
		aggVisGen: gssPrefixAggGen.Load(),
	}

	gen := gssPrefixAggGen.Load()
	setGSSMainLink(head, 0, prev, newStackEntryNode(2, payload))
	if got := gssPrefixAggGen.Load(); got != gen {
		t.Fatalf("same contribution changed prefix generation from %d to %d", gen, got)
	}
	compactHead := &gssNode{
		prev:  prev,
		entry: newStackEntryCompactFullLeaf(1, &compactFullLeaf{noTreeNode: noTreeNode{symbol: 1}}),
		depth: 2,
	}
	setGSSMainLink(compactHead, 0, prev, newStackEntryCompactFullLeaf(2, &compactFullLeaf{noTreeNode: noTreeNode{symbol: 2}}))
	if got := gssPrefixAggGen.Load(); got != gen {
		t.Fatalf("zero-contribution compact rewrite changed prefix generation from %d to %d", gen, got)
	}

	otherPayload := &Node{symbol: 1, equivVersion: 1}
	setGSSMainLink(head, 0, prev, newStackEntryNode(2, otherPayload))
	changedPayloadGen := gssPrefixAggGen.Load()
	if changedPayloadGen == gen {
		t.Fatalf("changed payload left prefix generation at %d", changedPayloadGen)
	}

	otherPrev := &gssNode{depth: 1}
	setGSSMainLink(head, 0, otherPrev, newStackEntryNode(2, otherPayload))
	if got := gssPrefixAggGen.Load(); got == changedPayloadGen {
		t.Fatalf("changed predecessor left prefix generation at %d", got)
	}
}

func TestResetGSSPrefixPathClearsAndBoundsRetention(t *testing.T) {
	nodes := []*gssNode{{}, {}, {}, {}, {}}
	path := nodes[:1]
	resetGSSPrefixPath(&path)
	if len(path) != 0 || cap(path) != len(nodes) {
		t.Fatalf("retained path shape = len %d cap %d, want len 0 cap %d", len(path), cap(path), len(nodes))
	}
	for i, node := range path[:cap(path)] {
		if node != nil {
			t.Fatalf("retained path[%d] = %p, want nil", i, node)
		}
	}

	path = make([]*gssNode, 1, maxRetainedGSSPrefixPath+1)
	path[0] = &gssNode{}
	resetGSSPrefixPath(&path)
	if path != nil {
		t.Fatalf("oversized path retained len %d cap %d, want nil", len(path), cap(path))
	}
}

func TestParseFinalizationClearsPrefixPathPointers(t *testing.T) {
	parser := NewParser(buildArithmeticLanguage())
	nodes := []*gssNode{{}, {}, {}}
	parser.cPrefixPath = nodes[:1]

	tree := mustParse(t, parser, []byte("42"))
	tree.Release()

	if len(parser.cPrefixPath) != 0 || cap(parser.cPrefixPath) != len(nodes) {
		t.Fatalf("finalized path shape = len %d cap %d, want len 0 cap %d", len(parser.cPrefixPath), cap(parser.cPrefixPath), len(nodes))
	}
	for i, node := range parser.cPrefixPath[:cap(parser.cPrefixPath)] {
		if node != nil {
			t.Fatalf("finalized path[%d] = %p, want nil", i, node)
		}
	}
}

func TestGLRMergeScratchResetClearsPrefixPathPointers(t *testing.T) {
	nodes := []*gssNode{{}, {}, {}}
	var scratch glrMergeScratch
	scratch.cPrefixPath = nodes[:1]
	scratch.reset()
	if len(scratch.cPrefixPath) != 0 || cap(scratch.cPrefixPath) != len(nodes) {
		t.Fatalf("reset path shape = len %d cap %d, want len 0 cap %d", len(scratch.cPrefixPath), cap(scratch.cPrefixPath), len(nodes))
	}
	for i, node := range scratch.cPrefixPath[:cap(scratch.cPrefixPath)] {
		if node != nil {
			t.Fatalf("reset path[%d] = %p, want nil", i, node)
		}
	}
}

func TestParserScratchBudgetIncludesMergePrefixPathGrowth(t *testing.T) {
	var scratch parserScratch
	scratch.setBudget(1)
	scratch.merge.cPrefixPath = make([]*gssNode, 0, 2)
	if !scratch.budgetExhausted() {
		t.Fatal("budget not exhausted after merge prefix-path growth")
	}
}
