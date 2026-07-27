package parsercorephase0

// MaterializationReplayChildren is a borrowed compact-child view for one
// parse-state replay traversal. It remains valid until the Core is reset.
// Callers must not retain it past that boundary.
type MaterializationReplayChildren struct {
	ids []SubtreeID
}

// Len returns the number of compact children.
func (c MaterializationReplayChildren) Len() int {
	return len(c.ids)
}

// At returns one compact child without copying the child arena.
func (c MaterializationReplayChildren) At(index int) (SubtreeID, bool) {
	if index < 0 || index >= len(c.ids) {
		return 0, false
	}
	return c.ids[index], true
}

// MaterializationReplayView exposes only the compact fields that parse-state
// replay consumes.
type MaterializationReplayView struct {
	Symbol   Symbol
	Children MaterializationReplayChildren
	Extra    bool
	Terminal bool
}

// MaterializationReplayView returns one borrowed parse-state replay view.
func (c *Core) MaterializationReplayView(id SubtreeID) (MaterializationReplayView, error) {
	record, err := c.subtree(id)
	if err != nil {
		return MaterializationReplayView{}, err
	}
	return MaterializationReplayView{
		Symbol: record.symbol,
		Children: MaterializationReplayChildren{
			ids: c.children[record.firstChild : record.firstChild+record.childCount],
		},
		Extra:    record.extra,
		Terminal: record.terminal,
	}, nil
}
