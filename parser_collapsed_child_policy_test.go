package gotreesitter

import "testing"

func collapsedChildPolicyTestLanguage(rule collapsedNamedLeafRule) (*Language, Symbol, Symbol) {
	childNamed := rule.languageName == "ruby" || rule.languageName == "kotlin"
	lang := &Language{
		Name:                      rule.languageName,
		TokenCount:                4,
		NativeResultCompatibility: ResultCompatibilityNativeCollapsedChildren,
		SymbolNames:               []string{"EOF", "root", rule.parentName, rule.childName},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "root", Visible: true, Named: true},
			{Name: rule.parentName, Visible: true, Named: true},
			{Name: rule.childName, Visible: true, Named: childNamed},
		},
	}
	return lang, 2, 3
}

func TestCollapsedChildOccurrencePolicyNativelyRetainsAllLedgerRows(t *testing.T) {
	if got, want := len(resultCollapsedNamedLeafRules), 23; got != want {
		t.Fatalf("collapsed-child ledger rows = %d, want %d", got, want)
	}
	for _, rule := range resultCollapsedNamedLeafRules {
		rule := rule
		t.Run(rule.languageName+"/"+rule.parentName, func(t *testing.T) {
			lang, parentSymbol, childSymbol := collapsedChildPolicyTestLanguage(rule)
			parser := NewParser(lang)
			if !parser.retainsCollapsedChildOccurrence(parentSymbol, childSymbol) {
				t.Fatal("compiled occurrence policy omitted ledger pair")
			}
			arena := newNodeArena(arenaClassFull)
			end := uint32(len(rule.childName))
			original := newLeafNodeInArena(arena, childSymbol, symbolIsNamed(lang, childSymbol), 0, end, Point{}, Point{Column: end})

			var parent *Node
			switch rule.languageName {
			case "ruby", "kotlin":
				lang.AliasSequences = [][]Symbol{{parentSymbol}}
				parser = NewParser(lang)
				children, _, _ := parser.buildReduceChildren(
					[]stackEntry{newStackEntryNode(0, original)}, 0, 1, 1, 1, 0, arena,
				)
				if len(children) != 1 {
					t.Fatalf("native alias path children = %d", len(children))
				}
				parent = children[0]
				if parent == original || parent == nil || parent.symbol != parentSymbol || parent.ChildCount() != 1 {
					t.Fatalf("native alias retention = %#v", parent)
				}
				child := parent.Child(0)
				if child == nil || child == original || child.symbol != childSymbol {
					t.Fatalf("retained alias child = %#v, original=%p", child, original)
				}
				if original.parent != nil {
					t.Fatal("alias retention mutated borrowed child parent")
				}
			default:
				action := ParseAction{Type: ParseActionReduce, Symbol: parentSymbol, ChildCount: 1}
				entries := []stackEntry{newStackEntryNode(0, original)}
				if collapsed := parser.collapsibleRawUnarySelfReduction(action, Token{}, arena, entries, 0, 1); collapsed != nil {
					t.Fatalf("native unary retention collapsed to %p", collapsed)
				}
				lang.KeywordCaptureToken = 1
				if collapsed := parser.forestCollapsibleNamedKeywordLeaf(action, Token{}, arena, entries, 0, 1); collapsed != nil {
					t.Fatalf("forest native unary retention collapsed to %p", collapsed)
				}
				parent = newParentNodeInArena(arena, parentSymbol, true, []*Node{original}, nil, 0)
			}

			if parent.ChildCount() != 1 || parent.Child(0).symbol != childSymbol {
				t.Fatalf("native shape parent=%d children=%d child=%v", parent.symbol, parent.ChildCount(), parent.Child(0))
			}
			parser.resetNormalizationStats()
			parser.materializationTiming = &parseMaterializationTiming{}
			parser.runNamedNormalizationPass("collapsed_named_leaf_children", func() bool { return true }, func() normalizationPassCounters {
				return normalizeResultCollapsedNamedLeafChildren(parent, []byte(rule.childName), lang)
			})
			var runtime ParseRuntime
			parser.copyNormalizationStats(&runtime)
			pass, ok := normalizationRuntimePass(runtime, "collapsed_named_leaf_children")
			if !ok || pass.Checked != 1 || pass.Run != 1 || pass.NodesVisited == 0 || pass.NodesRewritten != 0 {
				t.Fatalf("native safety-net pass=%+v found=%t runtime=%+v", pass, ok, runtime)
			}
		})
	}
}

func normalizationRuntimePass(runtime ParseRuntime, name string) (NormalizationPassRuntime, bool) {
	if runtime.NormalizationPasses == nil {
		return NormalizationPassRuntime{}, false
	}
	for _, pass := range *runtime.NormalizationPasses {
		if pass.Name == name {
			return pass, true
		}
	}
	return NormalizationPassRuntime{}, false
}

func TestCollapsedChildOccurrencePolicyNegativeControls(t *testing.T) {
	t.Run("go-same-name-still-collapses", func(t *testing.T) {
		lang := &Language{
			Name: "go", TokenCount: 4,
			SymbolNames: []string{"EOF", "root", "true", "true"},
			SymbolMetadata: []SymbolMetadata{
				{Name: "EOF"}, {Name: "root", Visible: true, Named: true},
				{Name: "true", Visible: true, Named: true}, {Name: "true", Visible: true},
			},
		}
		parser := NewParser(lang)
		arena := newNodeArena(arenaClassFull)
		child := newLeafNodeInArena(arena, 3, false, 0, 4, Point{}, Point{Column: 4})
		action := ParseAction{Type: ParseActionReduce, Symbol: 2, ChildCount: 1}
		if collapsed := parser.collapsibleRawUnarySelfReduction(action, Token{}, arena, []stackEntry{newStackEntryNode(0, child)}, 0, 1); collapsed == nil {
			t.Fatal("unlisted Go same-name token stopped collapsing")
		}
	})

	t.Run("fielded-unary-remains-fielded", func(t *testing.T) {
		rule := collapsedNamedLeafRule{languageName: "apex", parentName: "super", childName: "super"}
		lang, parentSymbol, childSymbol := collapsedChildPolicyTestLanguage(rule)
		lang.FieldMapSlices = [][2]uint16{{0, 1}}
		lang.FieldMapEntries = []FieldMapEntry{{FieldID: 1, ChildIndex: 0}}
		parser := NewParser(lang)
		arena := newNodeArena(arenaClassFull)
		child := newLeafNodeInArena(arena, childSymbol, false, 0, 5, Point{}, Point{Column: 5})
		action := ParseAction{Type: ParseActionReduce, Symbol: parentSymbol, ChildCount: 1}
		if collapsed := parser.collapsibleRawUnarySelfReduction(action, Token{}, arena, []stackEntry{newStackEntryNode(0, child)}, 0, 1); collapsed != nil {
			t.Fatal("fielded unary occurrence collapsed")
		}
		parent := newParentNodeInArena(arena, parentSymbol, true, []*Node{child}, []FieldID{1}, 0)
		if parent.fieldIDs()[0] != 1 {
			t.Fatal("field metadata was not preserved")
		}
	})

	t.Run("inherited-field-is-not-promoted-to-direct", func(t *testing.T) {
		rule := collapsedNamedLeafRule{languageName: "ruby", parentName: "bare_string", childName: "string_content"}
		lang, parentSymbol, childSymbol := collapsedChildPolicyTestLanguage(rule)
		lang.FieldMapSlices = [][2]uint16{{0, 1}}
		lang.FieldMapEntries = []FieldMapEntry{{FieldID: 1, ChildIndex: 0, Inherited: true}}
		lang.AliasSequences = [][]Symbol{{parentSymbol}}
		parser := NewParser(lang)
		arena := newNodeArena(arenaClassFull)
		original := newLeafNodeInArena(arena, childSymbol, true, 0, 1, Point{}, Point{Column: 1})
		children, fields, sources := parser.buildReduceChildren(
			[]stackEntry{newStackEntryNode(0, original)}, 0, 1, 1, 1, 0, arena,
		)
		if len(children) != 1 || children[0] == original || children[0].symbol != parentSymbol || children[0].ChildCount() != 1 {
			t.Fatalf("retained inherited-field child = %#v", children)
		}
		if len(fields) != 1 || fields[0] != 0 || fieldSourceAt(sources, 0) != fieldSourceNone {
			t.Fatalf("inherited alias field was promoted: field=%v source=%v", fields, sources)
		}
		if original.parent != nil {
			t.Fatal("retained inherited-field alias mutated borrowed child")
		}
	})

	for _, test := range []struct {
		name  string
		mark  func(*Node)
		check func(*Node) bool
	}{
		{name: "missing", mark: func(n *Node) { n.setMissing(true) }, check: func(n *Node) bool { return n.isMissing() }},
		{name: "error", mark: func(n *Node) { n.setHasError(true) }, check: func(n *Node) bool { return n.hasError() }},
	} {
		t.Run("flagged-alias-fails-closed-"+test.name, func(t *testing.T) {
			rule := collapsedNamedLeafRule{languageName: "ruby", parentName: "bare_string", childName: "string_content"}
			lang, parentSymbol, childSymbol := collapsedChildPolicyTestLanguage(rule)
			lang.AliasSequences = [][]Symbol{{parentSymbol}}
			parser := NewParser(lang)
			arena := newNodeArena(arenaClassFull)
			original := newLeafNodeInArena(arena, childSymbol, true, 0, 1, Point{}, Point{Column: 1})
			test.mark(original)
			children, _, _ := parser.buildReduceChildren(
				[]stackEntry{newStackEntryNode(0, original)}, 0, 1, 1, 1, 0, arena,
			)
			if len(children) != 1 || children[0].symbol != parentSymbol || children[0].ChildCount() != 0 || !test.check(children[0]) {
				t.Fatalf("flagged alias did not fail closed: %#v", children)
			}
			if original.parent != nil {
				t.Fatal("borrowed flagged child was mutated")
			}
		})
	}

	t.Run("extra-skips-aliasing-on-live-child-build-path", func(t *testing.T) {
		rule := collapsedNamedLeafRule{languageName: "ruby", parentName: "bare_string", childName: "string_content"}
		lang, parentSymbol, childSymbol := collapsedChildPolicyTestLanguage(rule)
		lang.AliasSequences = [][]Symbol{{parentSymbol}}
		parser := NewParser(lang)
		arena := newNodeArena(arenaClassFull)
		original := newLeafNodeInArena(arena, childSymbol, true, 0, 1, Point{}, Point{Column: 1})
		original.setExtra(true)
		children, _, _ := parser.buildReduceChildren(
			[]stackEntry{newStackEntryNode(0, original)}, 0, 1, 1, 1, 0, arena,
		)
		if len(children) != 1 || children[0] != original || children[0].symbol != childSymbol || !children[0].isExtra() {
			t.Fatalf("extra alias path = %#v", children)
		}
	})

	t.Run("same-name-custom-artifacts-stay-on-safety-net", func(t *testing.T) {
		for _, rule := range []collapsedNamedLeafRule{
			{languageName: "apex", parentName: "super", childName: "super"},
			{languageName: "ruby", parentName: "bare_string", childName: "string_content"},
		} {
			lang, parentSymbol, childSymbol := collapsedChildPolicyTestLanguage(rule)
			lang.NativeResultCompatibility = 0
			parser := NewParser(lang)
			if parser.retainsCollapsedChildOccurrence(parentSymbol, childSymbol) {
				t.Fatalf("same-name custom %s artifact compiled native policy", rule.languageName)
			}
			arena := newNodeArena(arenaClassFull)
			leaf := newLeafNodeInArena(arena, childSymbol, symbolIsNamed(lang, childSymbol), 0, 5, Point{}, Point{Column: 5})
			collapsed := aliasedNodeInArena(arena, lang, leaf, parentSymbol)
			if collapsed.ChildCount() != 0 {
				t.Fatal("legacy custom path unexpectedly retained child")
			}
			stats := normalizeCollapsedNamedLeafChildrenWithStats(collapsed, lang, rule.parentName, rule.childName)
			if stats.nodesRewritten != 1 || collapsed.ChildCount() != 1 || collapsed.Child(0).symbol != childSymbol {
				t.Fatalf("custom safety-net stats=%+v shape=%s", stats, collapsed.SExpr(lang))
			}
		}
	})
}
