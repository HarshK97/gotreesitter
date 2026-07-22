package gotreesitter

// trailingSpanRuleKind selects which shared primitive a trailingSpanRule row
// applies. Every kind is generic (parameterized by the row's fields); no
// per-language logic lives outside this table -- the same spirit as
// collapsedChildOccurrenceRules (parser_collapsed_child_policy.go) for the
// collapsed-named-leaf-child compat class.
type trailingSpanRuleKind int

const (
	// Extends the root's single child to cover a trailing trivia gap
	// containing a line break (normalizeTopLevelTrailingLineBreakSpan).
	trailingSpanExtendSoleChildLineBreak trailingSpanRuleKind = iota
	// Drops the root's final invisible extra-trivia child, widening the
	// root's own span to cover it (trimTrailingExtraTriviaRoot). Fires even
	// on error trees, unlike the shared post-switch pass in
	// normalizeResultCompatibility, which only trims clean trees. rootType,
	// if set, additionally gates on the root node's type.
	trailingSpanTrimRootExtraTrivia
	// Shrinks the root's first child's end back to the source's last
	// non-trivia byte when its span over-reaches into trailing whitespace
	// (shrinkFirstTopLevelChildEndToLastNonTrivia). Requires rootType and
	// childType; requireSingleChild additionally gates on a single root child.
	trailingSpanShrinkFirstChildToLastNonTrivia
	// Extends each childType child of a parentType node to cover an
	// immediately following line break before its next sibling
	// (extendChildLineBreakBeforeNextSibling). Requires parentType, childType.
	trailingSpanExtendStatementSiblingLineBreak
)

// trailingSpanRule is one row of the shared trailing-trivia/span compat
// pass: languageName selects the row, kind selects the primitive, and the
// remaining fields parameterize it. Replaces the former per-language wrapper
// functions normalizeNimTopLevelCallEnd, normalizeCommentTrailingExtraTrivia,
// normalizeRSTTopLevelSectionEnd, and normalizeFortranStatementLineBreaks.
type trailingSpanRule struct {
	languageName       string
	kind               trailingSpanRuleKind
	rootType           string // trim (optional gate) / shrink
	parentType         string // sibling
	childType          string // shrink / sibling
	requireSingleChild bool   // shrink
}

var trailingSpanRules = []trailingSpanRule{
	// Caddy/pug: sole top-level construct absorbs a trailing line-break gap.
	{languageName: "caddy", kind: trailingSpanExtendSoleChildLineBreak},
	{languageName: "pug", kind: trailingSpanExtendSoleChildLineBreak},
	// Fortran: a program_statement absorbs the line break before its next
	// program sibling, then the top-level construct absorbs the trailing gap.
	{languageName: "fortran", kind: trailingSpanExtendStatementSiblingLineBreak, parentType: "program", childType: "program_statement"},
	{languageName: "fortran", kind: trailingSpanExtendSoleChildLineBreak},
	// Comment/RST: trim a trailing invisible extra-trivia child at the root,
	// unconditionally (including on error trees).
	{languageName: "comment", kind: trailingSpanTrimRootExtraTrivia, rootType: "source"},
	{languageName: "rst", kind: trailingSpanTrimRootExtraTrivia},
	// Nim/RST: shrink a top-level construct's end back off trailing
	// whitespace that C tree-sitter does not attach.
	{languageName: "nim", kind: trailingSpanShrinkFirstChildToLastNonTrivia, rootType: "source_file", childType: "call", requireSingleChild: true},
	{languageName: "rst", kind: trailingSpanShrinkFirstChildToLastNonTrivia, rootType: "document", childType: "section"},
}

// normalizeResultTrailingSpanCompatibility is the single parameterized pass
// covering the trailing-span compat shims for caddy, comment, fortran, nim,
// pug, and rst: it applies every trailingSpanRules row matching lang, in
// table order (fortran's statement pass must run before its sole-child pass;
// rst's trim must run before its shrink).
func normalizeResultTrailingSpanCompatibility(root *Node, source []byte, lang *Language) {
	if root == nil || lang == nil {
		return
	}
	for _, rule := range trailingSpanRules {
		if rule.languageName != lang.Name {
			continue
		}
		switch rule.kind {
		case trailingSpanExtendSoleChildLineBreak:
			normalizeTopLevelTrailingLineBreakSpan(root, source, lang)
		case trailingSpanTrimRootExtraTrivia:
			if rule.rootType != "" && root.Type(lang) != rule.rootType {
				continue
			}
			trimTrailingExtraTriviaRoot(root, source, lang)
		case trailingSpanShrinkFirstChildToLastNonTrivia:
			shrinkFirstTopLevelChildEndToLastNonTrivia(root, source, lang, rule.languageName, rule.rootType, rule.childType, rule.requireSingleChild)
		case trailingSpanExtendStatementSiblingLineBreak:
			extendChildLineBreakBeforeNextSibling(root, source, lang, rule.parentType, rule.childType)
		}
	}
}
