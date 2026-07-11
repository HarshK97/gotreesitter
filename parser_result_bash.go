package gotreesitter

func normalizeBashProgramVariableAssignments(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "bash" || root.Type(lang) != "program" || len(root.children) == 0 {
		return
	}
	normalizeBashVariableAssignmentsInNode(root, lang)
}

func normalizeBashVariableAssignmentsInNode(node *Node, lang *Language) {
	if node == nil || lang == nil || len(node.children) == 0 {
		return
	}
	for _, child := range node.children {
		if child != nil {
			normalizeBashVariableAssignmentsInNode(child, lang)
		}
	}
	out := make([]*Node, 0, len(node.children))
	changed := false
	for _, child := range node.children {
		if child == nil {
			continue
		}
		if child.Type(lang) == "variable_assignments" && bashAllVariableAssignments(child, lang) && bashShouldSplitVariableAssignments(node.Type(lang)) {
			out = append(out, child.children...)
			changed = true
			continue
		}
		out = append(out, child)
	}
	if !changed {
		assignBashIfConditionField(node, lang)
		return
	}
	if node.ownerArena != nil {
		buf := node.ownerArena.allocNodeSlice(len(out))
		copy(buf, out)
		out = buf
	}
	node.children = out
	node.clearFieldMetadata()
	assignBashIfConditionField(node, lang)
}

func normalizeBashGeneratedCommandAssignments(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || !lang.GeneratedByGrammargen || lang.Name != "bash" || len(source) == 0 {
		return
	}
	ctx, ok := newBashGeneratedAssignmentContext(lang)
	if !ok {
		return
	}
	normalizeBashGeneratedCommandAssignmentsInNode(root, source, lang, ctx)
}

func normalizeBashCommandNameArguments(root *Node, lang *Language) {
	if root == nil || lang == nil || lang.Name != "bash" || root.IsError() || root.hasError() {
		return
	}
	commandSym, ok := symbolByName(lang, "command")
	if !ok {
		return
	}
	commandNameSym, ok := symbolByName(lang, "command_name")
	if !ok {
		return
	}
	concatenationSym, ok := symbolByName(lang, "concatenation")
	if !ok {
		return
	}
	normalizeBashCommandNameArgumentsInNode(root, commandSym, commandNameSym, concatenationSym)
}

func normalizeBashCommandNameArgumentsInNode(node *Node, commandSym, commandNameSym, concatenationSym Symbol) {
	if node == nil || node.IsError() || node.hasError() {
		return
	}
	for _, child := range node.children {
		normalizeBashCommandNameArgumentsInNode(child, commandSym, commandNameSym, concatenationSym)
	}
	mergeBashCommandNameArguments(node, commandSym, commandNameSym, concatenationSym)
}

func mergeBashCommandNameArguments(node *Node, commandSym, commandNameSym, concatenationSym Symbol) bool {
	if node == nil || node.symbol != commandSym || len(node.children) < 2 || node.IsError() || node.hasError() {
		return false
	}
	commandName := node.children[0]
	if commandName == nil || commandName.symbol != commandNameSym || len(commandName.children) == 0 || commandName.IsError() || commandName.hasError() {
		return false
	}
	if len(commandName.children) == 1 && commandName.children[0] != nil && commandName.children[0].symbol == concatenationSym {
		return false
	}

	arena := node.ownerArena
	mergeEnd := 1
	lastEnd := commandName.endByte
	for mergeEnd < len(node.children) {
		child := node.children[mergeEnd]
		if child == nil || child.IsError() || child.hasError() || child.startByte != lastEnd {
			break
		}
		lastEnd = child.endByte
		mergeEnd++
	}
	if mergeEnd == 1 {
		return false
	}

	parts := make([]*Node, 0, len(commandName.children)+mergeEnd-1)
	parts = append(parts, commandName.children...)
	parts = append(parts, node.children[1:mergeEnd]...)
	concat := newParentNodeInArena(arena, concatenationSym, true, cloneNodeSliceInArena(arena, parts), nil, 0)

	commandName.endByte = concat.endByte
	commandName.endPoint = concat.endPoint
	replaceNodeChildrenUnfielded(commandName, []*Node{concat})

	commandChildren := make([]*Node, 0, len(node.children)-mergeEnd+1)
	commandChildren = append(commandChildren, commandName)
	commandChildren = append(commandChildren, node.children[mergeEnd:]...)
	if arena != nil {
		commandChildren = cloneNodeSliceInArena(arena, commandChildren)
	}
	replaceNodeChildrenUnfielded(node, commandChildren)
	return true
}

type bashGeneratedAssignmentContext struct {
	commandSym            Symbol
	commandNameSym        Symbol
	concatenationSym      Symbol
	variableAssignmentSym Symbol
	variableNameSym       Symbol
	nameFieldID           FieldID
	valueFieldID          FieldID
}

func newBashGeneratedAssignmentContext(lang *Language) (bashGeneratedAssignmentContext, bool) {
	var ctx bashGeneratedAssignmentContext
	var ok bool
	if ctx.commandSym, ok = symbolByName(lang, "command"); !ok {
		return ctx, false
	}
	if ctx.commandNameSym, ok = symbolByName(lang, "command_name"); !ok {
		return ctx, false
	}
	if ctx.concatenationSym, ok = symbolByName(lang, "concatenation"); !ok {
		return ctx, false
	}
	if ctx.variableAssignmentSym, ok = symbolByName(lang, "variable_assignment"); !ok {
		return ctx, false
	}
	if ctx.variableNameSym, ok = symbolByName(lang, "variable_name"); !ok {
		return ctx, false
	}
	ctx.nameFieldID, _ = lang.FieldByName("name")
	ctx.valueFieldID, _ = lang.FieldByName("value")
	return ctx, true
}

func normalizeBashGeneratedCommandAssignmentsInNode(node *Node, source []byte, lang *Language, ctx bashGeneratedAssignmentContext) {
	if node == nil {
		return
	}
	for _, child := range node.children {
		normalizeBashGeneratedCommandAssignmentsInNode(child, source, lang, ctx)
	}
	rewriteBashGeneratedCommandAssignment(node, source, lang, ctx)
}

func rewriteBashGeneratedCommandAssignment(node *Node, source []byte, lang *Language, ctx bashGeneratedAssignmentContext) bool {
	if node == nil || node.symbol != ctx.commandSym || len(node.children) != 1 {
		return false
	}
	commandName := node.children[0]
	if commandName == nil || commandName.symbol != ctx.commandNameSym {
		return false
	}
	parts := bashGeneratedAssignmentParts(commandName, ctx)
	if len(parts) == 0 {
		return false
	}
	first := parts[0]
	if first == nil || int(first.startByte) >= len(source) || first.endByte > uint32(len(source)) {
		return false
	}
	nameEnd, valueStart, ok := bashSimpleAssignmentSplit(source, first.startByte, first.endByte)
	if !ok || valueStart > commandName.endByte {
		return false
	}

	arena := node.ownerArena
	name := newLeafNodeInArena(arena, ctx.variableNameSym, true, first.startByte, nameEnd, first.startPoint, advancePointByBytes(first.startPoint, source[first.startByte:nameEnd]))
	valueChildren := make([]*Node, 0, len(parts))
	if valueStart < first.endByte {
		valueStartPoint := advancePointByBytes(first.startPoint, source[first.startByte:valueStart])
		valueChildren = append(valueChildren, newLeafNodeInArena(arena, first.symbol, first.isNamed(), valueStart, first.endByte, valueStartPoint, first.endPoint))
	}
	valueChildren = append(valueChildren, parts[1:]...)
	if len(valueChildren) == 0 {
		return false
	}

	value := valueChildren[0]
	if len(valueChildren) > 1 {
		value = newParentNodeInArena(arena, ctx.concatenationSym, true, cloneNodeSliceInArena(arena, valueChildren), nil, 0)
	}

	children := []*Node{name, value}
	if arena != nil {
		children = cloneNodeSliceInArena(arena, children)
	}
	node.symbol = ctx.variableAssignmentSym
	node.setNamed(true)
	node.children = children
	node.setFieldMetadata(bashGeneratedAssignmentFieldIDs(arena, ctx), bashGeneratedAssignmentFieldSources(ctx))
	node.setHasError(false)
	populateParentNode(node, children)
	return true
}

func bashGeneratedAssignmentParts(commandName *Node, ctx bashGeneratedAssignmentContext) []*Node {
	if commandName == nil {
		return nil
	}
	if len(commandName.children) != 1 {
		return nil
	}
	child := commandName.children[0]
	if child == nil {
		return nil
	}
	if child.symbol == ctx.concatenationSym {
		return child.children
	}
	return []*Node{child}
}

func bashSimpleAssignmentSplit(source []byte, startByte, endByte uint32) (uint32, uint32, bool) {
	if startByte >= endByte || endByte > uint32(len(source)) {
		return 0, 0, false
	}
	start := int(startByte)
	end := int(endByte)
	if !bashAssignmentNameStart(source[start]) {
		return 0, 0, false
	}
	for i := start + 1; i < end; i++ {
		c := source[i]
		if c == '=' {
			nameEnd := i
			if i > start && source[i-1] == '+' {
				nameEnd = i - 1
			}
			if nameEnd <= start {
				return 0, 0, false
			}
			return uint32(nameEnd), uint32(i + 1), true
		}
		if c == '+' && i+1 < end && source[i+1] == '=' {
			continue
		}
		if !bashAssignmentNameContinue(c) {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

func bashAssignmentNameStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func bashAssignmentNameContinue(c byte) bool {
	return bashAssignmentNameStart(c) || (c >= '0' && c <= '9')
}

func bashGeneratedAssignmentFieldIDs(arena *nodeArena, ctx bashGeneratedAssignmentContext) []FieldID {
	if ctx.nameFieldID == 0 && ctx.valueFieldID == 0 {
		return nil
	}
	fields := []FieldID{ctx.nameFieldID, ctx.valueFieldID}
	if arena == nil {
		return fields
	}
	out := arena.allocFieldIDSlice(len(fields))
	copy(out, fields)
	return out
}

func bashGeneratedAssignmentFieldSources(ctx bashGeneratedAssignmentContext) []uint8 {
	if ctx.nameFieldID == 0 && ctx.valueFieldID == 0 {
		return nil
	}
	return []uint8{fieldSourceDirect, fieldSourceDirect}
}

func bashAllVariableAssignments(node *Node, lang *Language) bool {
	if node == nil || lang == nil || len(node.children) < 2 {
		return false
	}
	for _, child := range node.children {
		if child == nil || child.Type(lang) != "variable_assignment" {
			return false
		}
	}
	return true
}

func bashShouldSplitVariableAssignments(parentType string) bool {
	switch parentType {
	case "command", "redirected_statement", "declaration_command", "unset_command":
		return false
	default:
		return true
	}
}

func assignBashIfConditionField(node *Node, lang *Language) {
	if node == nil || lang == nil || node.Type(lang) != "if_statement" || len(node.children) <= 1 {
		return
	}
	fid, ok := lang.FieldByName("condition")
	if !ok {
		return
	}
	ensureNodeFieldStorage(node, len(node.children))
	thenIndex := -1
	for i, child := range node.children {
		if child != nil && child.Type(lang) == "then" {
			thenIndex = i
			break
		}
	}
	if thenIndex < 0 {
		thenIndex = len(node.children)
	}
	fieldIDs := node.fieldIDs()
	fieldSources := node.fieldSources()
	for i := 1; i < thenIndex; i++ {
		if node.children[i] == nil {
			continue
		}
		fieldIDs[i] = fid
		fieldSources[i] = fieldSourceDirect
	}
}
