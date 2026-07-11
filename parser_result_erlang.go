package gotreesitter

func normalizeErlangSourceFileForms(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "erlang" || root.Type(lang) != "source_file" {
		return
	}
	normalizeErlangReplacementClauses(root, lang)
	formsOnlyID, ok := lang.FieldByName("forms_only")
	if !ok || !erlangSourceFileLooksLikeForms(root, lang) {
		return
	}
	view := resultMutableChildrenForMutation(root)
	ensureNodeFieldStorage(root, view.Len())
	fieldIDs := root.fieldIDs()
	fieldSources := root.fieldSources()
	for i := 0; i < view.Len(); i++ {
		entry, ok := view.Entry(i)
		if !ok || stackEntryNodeIsExtra(entry) {
			continue
		}
		fieldIDs[i] = formsOnlyID
		fieldSources[i] = fieldSourceDirect
		if stackEntryNodeChildCount(entry) > 0 {
			normalizeErlangTopLevelFormBounds(view.Child(i))
		}
	}
}

func erlangSourceFileLooksLikeForms(root *Node, lang *Language) bool {
	sawForm := false
	view := resultMutableChildrenForMutation(root)
	for i := 0; i < view.Len(); i++ {
		entry, ok := view.Entry(i)
		if !ok || stackEntryNodeIsExtra(entry) {
			continue
		}
		if !erlangIsTopLevelFormType(symbolTypeName(lang, stackEntryNodeSymbol(entry))) {
			return false
		}
		sawForm = true
	}
	return sawForm
}

func erlangIsTopLevelFormType(typ string) bool {
	switch typ {
	case "module_attribute",
		"behaviour_attribute",
		"export_attribute",
		"import_attribute",
		"export_type_attribute",
		"optional_callbacks_attribute",
		"compile_options_attribute",
		"feature_attribute",
		"file_attribute",
		"deprecated_attribute",
		"record_decl",
		"type_alias",
		"nominal",
		"opaque",
		"spec",
		"callback",
		"wild_attribute",
		"fun_decl",
		"pp_include",
		"pp_include_lib",
		"pp_undef",
		"pp_ifdef",
		"pp_ifndef",
		"pp_else",
		"pp_endif",
		"pp_if",
		"pp_elif",
		"pp_define",
		"ssr_definition",
		"shebang":
		return true
	default:
		return false
	}
}

func normalizeErlangTopLevelFormBounds(node *Node) {
	if node == nil || resultChildCount(node) == 0 {
		return
	}
	var first, last *Node
	for i := 0; i < resultChildCount(node); i++ {
		child := resultChildAt(node, i)
		if child == nil || child.IsExtra() {
			continue
		}
		if first == nil {
			first = child
		}
		last = child
	}
	if first == nil || last == nil {
		return
	}
	node.startByte = first.startByte
	node.startPoint = first.startPoint
	node.endByte = last.endByte
	node.endPoint = last.endPoint
}

func normalizeErlangReplacementClauses(root *Node, lang *Language) {
	replacementCRClausesSym, ok := symbolByName(lang, "replacement_cr_clauses")
	if !ok {
		return
	}
	replacementFunctionClausesSym, ok := symbolByName(lang, "replacement_function_clauses")
	if !ok {
		return
	}
	crClauseSym, ok := symbolByName(lang, "cr_clause")
	if !ok {
		return
	}
	functionClauseSym, ok := symbolByName(lang, "function_clause")
	if !ok {
		return
	}
	walkResultTree(root, func(n *Node) {
		if n == nil || n.symbol != replacementCRClausesSym {
			return
		}
		n.symbol = replacementFunctionClausesSym
		for i := 0; i < resultChildCount(n); i++ {
			child := resultChildAt(n, i)
			if child != nil && child.symbol == crClauseSym {
				child.symbol = functionClauseSym
				normalizeErlangReplacementFunctionClauseHead(child, lang)
			}
		}
	})
}

func normalizeErlangReplacementFunctionClauseHead(clause *Node, lang *Language) {
	if clause == nil || resultChildCount(clause) == 0 {
		return
	}
	call := resultChildAt(clause, 0)
	if call == nil || call.Type(lang) != "call" || resultChildCount(call) != 2 {
		return
	}
	name := resultChildAt(call, 0)
	args := resultChildAt(call, 1)
	if name == nil || args == nil || name.Type(lang) != "var" || args.Type(lang) != "expr_args" {
		return
	}
	if call.startByte != name.startByte || call.endByte != args.endByte {
		return
	}
	clauseChildren := resultChildSliceForMutation(clause)
	if len(clauseChildren) == 0 || clauseChildren[0] == nil {
		return
	}
	rewritten := make([]*Node, 0, len(clauseChildren)+1)
	rewritten = append(rewritten, name, args)
	rewritten = append(rewritten, clauseChildren[1:]...)
	replaceNodeChildrenUnfielded(clause, cloneNodeSliceInArena(clause.ownerArena, rewritten))
}
