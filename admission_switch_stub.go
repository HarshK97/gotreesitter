//go:build !gts_parsercorephase0

package gotreesitter

// tryCompactFullParseRoute is the default-build stub for the compact candidate
// route. The parsercorephase0 engine is only compiled under the
// gts_parsercorephase0 build tag, so in the default build the candidate route
// is unavailable: every eligible full parse fails closed and falls back to
// production. The switch still resolves and its counters still move, so the
// routing seam is testable without the tag.
func (p *Parser) tryCompactFullParseRoute(_ []byte) (*Tree, bool, string) {
	return nil, false, "compact candidate route not compiled in (build with -tags gts_parsercorephase0)"
}
