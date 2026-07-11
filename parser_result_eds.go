package gotreesitter

// normalizeEDSCompatibility matches tree-sitter C recovery for EDS entries
// whose empty value is followed by a blank line and a new section header. The
// Go runtime can consume "[Header" as the previous statement's value, merging
// two top-level sections. C keeps a zero-width value and starts a new section.
func normalizeEDSCompatibility(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil || root.Type(lang) != "source_file" || root.ownerArena == nil {
		return
	}
	sectionSym, ok := symbolByName(lang, "section")
	if !ok {
		return
	}
	children := resultChildSliceForMutation(root)
	var out []*Node
	changed := false
	for _, child := range children {
		if child != nil && child.symbol == sectionSym {
			if split, ok := splitEDSMergedSection(child, source, lang); ok {
				out = append(out, split...)
				changed = true
				continue
			}
		}
		out = append(out, child)
	}
	if changed {
		root.children = cloneNodeSliceInArena(root.ownerArena, out)
		root.clearFieldMetadata()
		populateParentNode(root, root.children)
		nodeInitEquivVersion(root)
	}
	if !normalizeEDSTopLevelErrors(root, source, lang) {
		return
	}
	out = resultChildSliceForMutation(root)
	root.children = cloneNodeSliceInArena(root.ownerArena, out)
	root.clearFieldMetadata()
	populateParentNode(root, root.children)
	nodeInitEquivVersion(root)
}

func splitEDSMergedSection(section *Node, source []byte, lang *Language) ([]*Node, bool) {
	children := resultChildSliceForMutation(section)
	if len(children) < 5 || section.ownerArena == nil {
		return nil, false
	}
	statementSym, ok := symbolByName(lang, "statement")
	if !ok {
		return nil, false
	}
	for i := 3; i < len(children); i++ {
		stmt := children[i]
		if stmt == nil || stmt.symbol != statementSym || !edsStatementContainsEmbeddedSectionHeader(stmt, source) {
			continue
		}
		firstStmt, secondHeader, ok := splitEDSEmbeddedHeaderStatement(stmt, source, lang)
		if !ok {
			continue
		}
		firstChildren := cloneNodeSliceInArena(section.ownerArena, append(append([]*Node{}, children[:i]...), firstStmt))
		first := newParentNodeInArena(section.ownerArena, section.symbol, section.isNamed(), firstChildren, nil, section.productionID)
		first.startByte = section.startByte
		first.startPoint = section.startPoint
		first.endByte = firstStmt.endByte
		first.endPoint = firstStmt.endPoint

		secondChildren := append([]*Node{}, secondHeader...)
		secondChildren = append(secondChildren, children[i+1:]...)
		second := newParentNodeInArena(section.ownerArena, section.symbol, section.isNamed(), cloneNodeSliceInArena(section.ownerArena, secondChildren), nil, section.productionID)
		second.startByte = secondChildren[0].startByte
		second.startPoint = secondChildren[0].startPoint
		if last := secondChildren[len(secondChildren)-1]; last != nil {
			second.endByte = last.endByte
			second.endPoint = last.endPoint
		}
		return []*Node{first, second}, true
	}
	return nil, false
}

func edsStatementContainsEmbeddedSectionHeader(stmt *Node, source []byte) bool {
	if int(stmt.endByte) > len(source) || stmt.startByte >= stmt.endByte {
		return false
	}
	text := source[stmt.startByte:stmt.endByte]
	for i := 0; i+2 < len(text); i++ {
		if text[i] == '\n' && text[i+1] == '\n' && text[i+2] == '[' {
			return true
		}
	}
	return false
}

func splitEDSEmbeddedHeaderStatement(stmt *Node, source []byte, lang *Language) (*Node, []*Node, bool) {
	children := resultChildSliceForMutation(stmt)
	if len(children) != 3 || stmt.ownerArena == nil {
		return nil, nil, false
	}
	key, eq, value := children[0], children[1], children[2]
	if key == nil || eq == nil || value == nil || eq.endByte > value.startByte || int(value.endByte) > len(source) {
		return nil, nil, false
	}
	open := value.startByte - 1
	close := value.endByte
	if int(open) >= len(source) || int(close) >= len(source) || source[open] != '[' || source[close] != ']' {
		return nil, nil, false
	}
	valueSym, ok := symbolByName(lang, "value")
	if !ok || value.symbol != valueSym {
		return nil, nil, false
	}
	sectionNameSym, ok := symbolByName(lang, "section_name")
	if !ok {
		return nil, nil, false
	}
	openSym, ok := symbolByName(lang, "[")
	if !ok {
		return nil, nil, false
	}
	closeSym, ok := symbolByName(lang, "]")
	if !ok {
		return nil, nil, false
	}

	emptyValue := newLeafNodeInArena(stmt.ownerArena, valueSym, value.isNamed(), eq.endByte, eq.endByte, eq.endPoint, eq.endPoint)
	firstStmt := newParentNodeInArena(stmt.ownerArena, stmt.symbol, stmt.isNamed(), cloneNodeSliceInArena(stmt.ownerArena, []*Node{key, eq, emptyValue}), nil, stmt.productionID)
	firstStmt.startByte = stmt.startByte
	firstStmt.startPoint = stmt.startPoint
	firstStmt.endByte = eq.endByte
	firstStmt.endPoint = eq.endPoint

	openPoint := advancePointByBytes(Point{}, source[:open])
	closePoint := advancePointByBytes(Point{}, source[:close])
	afterClose := close + 1
	afterClosePoint := advancePointByBytes(closePoint, source[close:afterClose])
	openNode := newLeafNodeInArena(stmt.ownerArena, openSym, symbolIsNamed(lang, openSym), open, open+1, openPoint, advancePointByBytes(openPoint, source[open:open+1]))
	nameNode := newLeafNodeInArena(stmt.ownerArena, sectionNameSym, symbolIsNamed(lang, sectionNameSym), value.startByte, value.endByte, value.startPoint, value.endPoint)
	closeNode := newLeafNodeInArena(stmt.ownerArena, closeSym, symbolIsNamed(lang, closeSym), close, afterClose, closePoint, afterClosePoint)
	return firstStmt, []*Node{openNode, nameNode, closeNode}, true
}

func normalizeEDSTopLevelErrors(root *Node, source []byte, lang *Language) bool {
	children := resultChildSliceForMutation(root)
	if len(children) == 0 || root.ownerArena == nil {
		return false
	}
	eqSym, ok := symbolByName(lang, "=")
	if !ok {
		return false
	}
	changed := false
	for _, child := range children {
		if child == nil || !child.IsError() || resultChildCount(child) != 0 {
			continue
		}
		if normalizeEDSErrorLine(child, source, eqSym) {
			changed = true
		}
	}
	return changed
}

func normalizeEDSErrorLine(errNode *Node, source []byte, eqSym Symbol) bool {
	if errNode.ownerArena == nil || int(errNode.startByte) > len(source) || int(errNode.endByte) > len(source) {
		return false
	}
	start := int(errNode.startByte)
	for start > 0 && source[start-1] != '\n' && source[start-1] != '\r' {
		start--
	}
	end := int(errNode.endByte)
	for end < len(source) && source[end] != '\n' && source[end] != '\r' {
		end++
	}
	if start == end || !edsSpanContainsEquals(source[start:end]) {
		return false
	}
	errNode.startByte = uint32(start)
	errNode.endByte = uint32(end)
	errNode.startPoint = advancePointByBytes(Point{}, source[:start])
	errNode.endPoint = advancePointByBytes(errNode.startPoint, source[start:end])
	errNode.setExtra(true)
	errNode.children = buildEDSErrorLineChildren(errNode.ownerArena, source, start, end, eqSym)
	errNode.clearFieldMetadata()
	populateParentNode(errNode, errNode.children)
	nodeInitEquivVersion(errNode)
	return true
}

func edsSpanContainsEquals(span []byte) bool {
	for _, b := range span {
		if b == '=' {
			return true
		}
	}
	return false
}

func buildEDSErrorLineChildren(arena *nodeArena, source []byte, start, end int, eqSym Symbol) []*Node {
	var out []*Node
	segStart := start
	for i := start; i < end; i++ {
		if source[i] != '=' {
			continue
		}
		if segStart < i {
			out = append(out, newEDSErrorLeaf(arena, source, segStart, i))
		}
		eqStart := uint32(i)
		eqPoint := advancePointByBytes(Point{}, source[:i])
		out = append(out, newLeafNodeInArena(arena, eqSym, false, eqStart, eqStart+1, eqPoint, advancePointByBytes(eqPoint, source[i:i+1])))
		segStart = i + 1
		if segStart < end && source[segStart] == '\r' {
			segStart++
			if segStart < end && source[segStart] == '\n' {
				segStart++
			}
		} else if segStart < end && source[segStart] == '\n' {
			segStart++
		}
	}
	if segStart < end {
		out = append(out, newEDSErrorLeaf(arena, source, segStart, end))
	}
	return cloneNodeSliceInArena(arena, out)
}

func newEDSErrorLeaf(arena *nodeArena, source []byte, start, end int) *Node {
	startPoint := advancePointByBytes(Point{}, source[:start])
	return newLeafNodeInArena(arena, errorSymbol, true, uint32(start), uint32(end), startPoint, advancePointByBytes(startPoint, source[start:end]))
}
