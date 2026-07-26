package gotreesitter

import (
	"testing"
)

func TestNormalizeMakeConditionalConsequenceFieldsExtendsAcrossLeadingTabs(t *testing.T) {
	lang := &Language{
		Name:        "make",
		SymbolNames: []string{"EOF", "conditional", "ifneq_directive", "\t", "recipe_line", "else_directive", "endif"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "conditional", Visible: true, Named: true},
			{Name: "ifneq_directive", Visible: true, Named: true},
			{Name: "\t", Visible: true, Named: false},
			{Name: "recipe_line", Visible: true, Named: true},
			{Name: "else_directive", Visible: true, Named: true},
			{Name: "endif", Visible: true, Named: false},
		},
		FieldNames: []string{"", "consequence"},
	}

	arena := newNodeArena(arenaClassFull)
	directive := newLeafNodeInArena(arena, 2, true, 0, 5, Point{}, Point{Column: 5})
	tab := newLeafNodeInArena(arena, 3, false, 5, 6, Point{Column: 5}, Point{Column: 6})
	recipe := newLeafNodeInArena(arena, 4, true, 6, 12, Point{Column: 6}, Point{Column: 12})
	elseDir := newLeafNodeInArena(arena, 5, true, 12, 16, Point{Column: 12}, Point{Column: 16})
	endif := newLeafNodeInArena(arena, 6, false, 16, 21, Point{Column: 16}, Point{Column: 21})
	root := newParentNodeInArena(arena, 1, true, []*Node{directive, tab, recipe, elseDir, endif}, []FieldID{0, 0, 1, 1, 0}, 0)
	root.setFieldSources([]uint8{0, 0, fieldSourceDirect, fieldSourceDirect, 0})

	normalizeMakeConditionalConsequenceFields(root, lang)

	if got, want := root.fieldIDs()[1], FieldID(1); got != want {
		t.Fatalf("tab field = %d, want %d", got, want)
	}
	if got, want := fieldSourceAt(root.fieldSources(), 1), uint8(fieldSourceDirect); got != want {
		t.Fatalf("tab field source = %d, want %d", got, want)
	}
	if got := root.fieldIDs()[4]; got != 0 {
		t.Fatalf("endif field = %d, want 0", got)
	}
}

// TestDispatcherArmCensusProbeIsWiredForMake is the production-wiring
// positive control the R2 dispatcher-arm census requires
// (docs/root-normalization-retirement.md; see
// parser_result_test/dispatcher_census_test.go for the census itself). It
// reuses the exact fixture from
// TestNormalizeMakeConditionalConsequenceFieldsExtendsAcrossLeadingTabs
// above, which independently proves normalizeMakeConditionalConsequenceFields
// rewrites root.fieldIDs()[1] from 0 to 1 on this input, but drives it
// through the real production entry point (normalizeResultCompatibility) and
// the real "make" switch case instead of calling the normalizer directly.
// This proves the census counters observe what production actually runs, so
// a zero-rewrite census result for some OTHER language is a claim about that
// language, not evidence the probe never fires.
func TestDispatcherArmCensusProbeIsWiredForMake(t *testing.T) {
	t.Setenv("GTS_DISPATCHER_CENSUS", "1")

	lang := &Language{
		Name:        "make",
		SymbolNames: []string{"EOF", "conditional", "ifneq_directive", "\t", "recipe_line", "else_directive", "endif"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "conditional", Visible: true, Named: true},
			{Name: "ifneq_directive", Visible: true, Named: true},
			{Name: "\t", Visible: true, Named: false},
			{Name: "recipe_line", Visible: true, Named: true},
			{Name: "else_directive", Visible: true, Named: true},
			{Name: "endif", Visible: true, Named: false},
		},
		FieldNames: []string{"", "consequence"},
	}

	arena := newNodeArena(arenaClassFull)
	directive := newLeafNodeInArena(arena, 2, true, 0, 5, Point{}, Point{Column: 5})
	tab := newLeafNodeInArena(arena, 3, false, 5, 6, Point{Column: 5}, Point{Column: 6})
	recipe := newLeafNodeInArena(arena, 4, true, 6, 12, Point{Column: 6}, Point{Column: 12})
	elseDir := newLeafNodeInArena(arena, 5, true, 12, 16, Point{Column: 12}, Point{Column: 16})
	endif := newLeafNodeInArena(arena, 6, false, 16, 21, Point{Column: 16}, Point{Column: 21})
	root := newParentNodeInArena(arena, 1, true, []*Node{directive, tab, recipe, elseDir, endif}, []FieldID{0, 0, 1, 1, 0}, 0)
	root.setFieldSources([]uint8{0, 0, fieldSourceDirect, fieldSourceDirect, 0})

	source := make([]byte, 21)
	parser := &Parser{language: lang}

	normalizeResultCompatibility(root, source, parser, nil)

	var pass *normalizationNamedPassStats
	for i := range parser.normalizationStats.namedPasses {
		if parser.normalizationStats.namedPasses[i].name == "dispatch.make" {
			pass = &parser.normalizationStats.namedPasses[i]
		}
	}
	if pass == nil {
		t.Fatal("dispatch.make census was never recorded: the probe is not wired to the production dispatcher, so any zero-rewrite census result for another language would be vacuous")
	}
	if pass.checked == 0 || pass.run == 0 {
		t.Fatalf("dispatch.make census checked=%d run=%d, want both > 0", pass.checked, pass.run)
	}
	if pass.nodesRewritten == 0 {
		t.Fatal("dispatch.make census recorded zero rewrites on an input known to mutate a field (see TestNormalizeMakeConditionalConsequenceFieldsExtendsAcrossLeadingTabs): the fingerprint probe did not observe a change it should have caught")
	}
	t.Logf("probe caught a real production rewrite: visited=%d rewritten=%d", pass.nodesVisited, pass.nodesRewritten)
}

func TestNormalizeIniSectionStartsSnapToFirstChild(t *testing.T) {
	lang := &Language{
		Name:        "ini",
		SymbolNames: []string{"EOF", "section", "section_name", "setting"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "section", Visible: true, Named: true},
			{Name: "section_name", Visible: true, Named: true},
			{Name: "setting", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	sectionName := newLeafNodeInArena(arena, 2, true, 48, 69, Point{Row: 1}, Point{Row: 1, Column: 21})
	setting := newLeafNodeInArena(arena, 3, true, 70, 80, Point{Row: 2}, Point{Row: 2, Column: 10})
	section := newParentNodeInArena(arena, 1, true, []*Node{sectionName, setting}, nil, 0)
	section.startByte = 0
	section.startPoint = Point{}

	normalizeIniSectionStarts(section, lang)

	if got, want := section.startByte, uint32(48); got != want {
		t.Fatalf("section.startByte = %d, want %d", got, want)
	}
	if got, want := section.startPoint, sectionName.startPoint; got != want {
		t.Fatalf("section.startPoint = %#v, want %#v", got, want)
	}
}

func TestNormalizeIniDocumentBlanksDropsRootBlank(t *testing.T) {
	lang := &Language{
		Name:        "ini",
		SymbolNames: []string{"EOF", "document", "comment", "_blank", "section"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "document", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
			{Name: "_blank", Visible: true, Named: true},
			{Name: "section", Visible: true, Named: true},
		},
	}

	arena := newNodeArena(arenaClassFull)
	comment := newLeafNodeInArena(arena, 2, true, 108, 149, Point{}, Point{Column: 41})
	blank := newLeafNodeInArena(arena, 3, true, 149, 150, Point{Column: 41}, Point{Row: 1})
	section := newLeafNodeInArena(arena, 4, true, 150, 600, Point{Row: 1}, Point{Row: 20})
	root := newParentNodeInArena(arena, 1, true, []*Node{comment, blank, section}, nil, 0)
	root.endByte = 600
	root.endPoint = Point{Row: 20}

	normalizeIniCompatibility(root, nil, lang)

	if got, want := root.ChildCount(), 2; got != want {
		t.Fatalf("document child count = %d, want %d", got, want)
	}
	if got, want := root.Child(0).Type(lang), "comment"; got != want {
		t.Fatalf("child[0] type = %q, want %q", got, want)
	}
	if got, want := root.Child(1).Type(lang), "section"; got != want {
		t.Fatalf("child[1] type = %q, want %q", got, want)
	}
	if got, want := root.Child(1).childIndex, int32(1); got != want {
		t.Fatalf("section childIndex = %d, want %d", got, want)
	}
}

func TestNormalizeIniMypyBuildContinuationListMatchesCErrorRoot(t *testing.T) {
	lang := testIniMypyLanguage()
	source := []byte("[mypy]\n\n# Please, when adding new files here, also add them to:\n# .github/workflows/mypy.yml\nfiles =\n    Tools/build/check_extension_modules.py,\n    Tools/build/check_warnings.py,\n\npretty = True\npython_version = 3.10\nstrict = True\nextra_checks = True\nenable_error_code = ignore-without-code,redundant-expr\nwarn_unreachable = True\n")
	arena := newNodeArena(arenaClassFull)
	sectionName := testIniLeaf(arena, lang, "section_name", source, 0, 7)
	comment1 := testIniLeaf(arena, lang, "comment", source, 8, 64)
	comment1.setExtra(true)
	comment2 := testIniLeaf(arena, lang, "comment", source, 64, 93)
	comment2.setExtra(true)
	filesSetting := testIniLeaf(arena, lang, "setting", source, 93, 101)
	acceptedContinuation := testIniLeaf(arena, lang, "setting", source, 105, 178)
	section := newParentNodeInArena(arena, testIniSymbol(lang, "section"), true, []*Node{sectionName, comment1, comment2, filesSetting, acceptedContinuation}, nil, 0)
	section.endByte = uint32(len(source))
	section.endPoint = advancePointByBytes(Point{}, source)
	root := newParentNodeInArena(arena, testIniSymbol(lang, "document"), true, []*Node{section}, nil, 0)
	root.endByte = uint32(len(source))
	root.endPoint = advancePointByBytes(Point{}, source)

	normalizeIniCompatibility(root, source, lang)

	if got := root.Type(lang); got != "ERROR" {
		t.Fatalf("root type = %q, want ERROR", got)
	}
	if !root.HasError() {
		t.Fatal("root HasError = false, want true")
	}
	if got := root.Child(0).Type(lang); got != "section_name" {
		t.Fatalf("child[0] = %q, want section_name", got)
	}
	if got := root.Child(4).Type(lang); got != "setting_name" {
		t.Fatalf("first continuation child = %q, want setting_name", got)
	}
	if got := root.Child(5).Type(lang); got != "ERROR" {
		t.Fatalf("second continuation child = %q, want ERROR", got)
	}
}

func TestNormalizeIniMypyContinuationErrorBuildsDocumentRoot(t *testing.T) {
	lang := testIniMypyLanguage()
	source := []byte("[mypy]\nfiles = Tools/check-c-api-docs/\npretty = True\n\n# We need `_colorize` import:\nmypy_path = $MYPY_CONFIG_FILE_DIR/../../Misc/mypy\n\n# Make sure Python can still be built\n# using Python 3.13 for `PYTHON_FOR_REGEN`...\npython_version = 3.13\n\n# ...And be strict:\nstrict = True\nextra_checks = True\nenable_error_code = \n    ignore-without-code,\n    redundant-expr,\n    truthy-bool,\n    possibly-undefined,\n")
	arena := newNodeArena(arenaClassFull)
	section := newParentNodeInArena(arena, testIniSymbol(lang, "section"), true, []*Node{
		testIniLeaf(arena, lang, "section_name", source, 0, 6),
		testIniLeaf(arena, lang, "setting", source, 7, 38),
		testIniLeaf(arena, lang, "setting", source, 39, 52),
		testIniLeaf(arena, lang, "comment", source, 54, 83),
		testIniLeaf(arena, lang, "setting", source, 84, 133),
		testIniLeaf(arena, lang, "comment", source, 135, 172),
		testIniLeaf(arena, lang, "comment", source, 173, 218),
		testIniLeaf(arena, lang, "setting", source, 219, 240),
		testIniLeaf(arena, lang, "comment", source, 242, 261),
		testIniLeaf(arena, lang, "setting", source, 262, 275),
		testIniLeaf(arena, lang, "setting", source, 276, 295),
		testIniLeaf(arena, lang, "setting", source, 296, 316),
	}, nil, 0)
	section.endByte = 316
	section.endPoint = advancePointByBytes(Point{}, source[:316])
	firstName := testIniLeaf(arena, lang, "setting_name", source, 321, 341)
	firstErr := newParentNodeInArena(arena, errorSymbol, true, []*Node{firstName}, nil, 0)
	firstErr.setExtra(true)
	firstErr.setHasError(true)
	firstErr.startByte = 321
	firstErr.endByte = 341
	firstErr.startPoint = advancePointByBytes(Point{}, source[:321])
	firstErr.endPoint = advancePointByBytes(Point{}, source[:341])
	blank := testIniLeaf(arena, lang, "_blank", source, 341, 342)
	blank.setExtra(true)
	root := newParentNodeInArena(arena, errorSymbol, true, []*Node{section, firstErr, blank}, nil, 0)
	root.setHasError(true)
	root.endByte = uint32(len(source))
	root.endPoint = advancePointByBytes(Point{}, source)

	normalizeIniCompatibility(root, source, lang)

	if got := root.Type(lang); got != "document" {
		t.Fatalf("root type = %q, want document", got)
	}
	if got := root.ChildCount(); got != 2 {
		t.Fatalf("root child count = %d, want 2", got)
	}
	if got := root.Child(0).EndByte(); got != 317 {
		t.Fatalf("section end = %d, want 317", got)
	}
	if got := root.Child(0).Child(11).EndByte(); got != 317 {
		t.Fatalf("last setting end = %d, want 317", got)
	}
	errNode := root.Child(1)
	if got := errNode.EndByte(); got != 402 {
		t.Fatalf("error end = %d, want 402", got)
	}
	if got := errNode.ChildCount(); got != 4 {
		t.Fatalf("error child count = %d, want 4", got)
	}
}

func TestNormalizeIniMypyFlatDocumentContinuationDropsSectionBlanks(t *testing.T) {
	lang := testIniMypyLanguage()
	source := []byte("[mypy]\nfiles = Tools/check-c-api-docs/\npretty = True\n\n# We need `_colorize` import:\nmypy_path = $MYPY_CONFIG_FILE_DIR/../../Misc/mypy\n\n# Make sure Python can still be built\n# using Python 3.13 for `PYTHON_FOR_REGEN`...\npython_version = 3.13\n\n# ...And be strict:\nstrict = True\nextra_checks = True\nenable_error_code = \n    ignore-without-code,\n    redundant-expr,\n    truthy-bool,\n    possibly-undefined,\n")
	arena := newNodeArena(arenaClassFull)
	root := newParentNodeInArena(arena, testIniSymbol(lang, "document"), true, []*Node{
		testIniLeaf(arena, lang, "section_name", source, 0, 7),
		testIniLeaf(arena, lang, "setting", source, 7, 39),
		testIniLeaf(arena, lang, "setting", source, 39, 54),
		testIniLeaf(arena, lang, "_blank", source, 53, 54),
		testIniLeaf(arena, lang, "comment", source, 54, 84),
		testIniLeaf(arena, lang, "setting", source, 84, 135),
		testIniLeaf(arena, lang, "_blank", source, 134, 135),
		testIniLeaf(arena, lang, "comment", source, 135, 173),
		testIniLeaf(arena, lang, "comment", source, 173, 219),
		testIniLeaf(arena, lang, "setting", source, 219, 242),
		testIniLeaf(arena, lang, "_blank", source, 241, 242),
		testIniLeaf(arena, lang, "comment", source, 242, 262),
		testIniLeaf(arena, lang, "setting", source, 262, 276),
		testIniLeaf(arena, lang, "setting", source, 276, 296),
		testIniLeaf(arena, lang, "setting", source, 296, 317),
		testIniLeaf(arena, lang, "setting_name", source, 321, 341),
	}, nil, 0)
	root.endByte = 402
	root.endPoint = advancePointByBytes(Point{}, source[:402])

	normalizeIniCompatibility(root, source, lang)

	if got := root.Type(lang); got != "document" {
		t.Fatalf("root type = %q, want document", got)
	}
	if got := root.ChildCount(); got != 2 {
		t.Fatalf("root child count = %d, want 2", got)
	}
	section := root.Child(0)
	if got := section.ChildCount(); got != 12 {
		t.Fatalf("section child count = %d, want 12", got)
	}
	for _, idx := range []int{2, 4, 7} {
		if got := section.Child(idx).Type(lang); got == "_blank" {
			t.Fatalf("section child[%d] = _blank, want C-compatible visible child", idx)
		}
	}
	if got := section.Child(2).EndByte(); got != 53 {
		t.Fatalf("pretty setting end = %d, want 53", got)
	}
	if got := section.Child(4).EndByte(); got != 134 {
		t.Fatalf("mypy_path setting end = %d, want 134", got)
	}
	if got := section.Child(7).EndByte(); got != 241 {
		t.Fatalf("python_version setting end = %d, want 241", got)
	}
	if got := section.EndByte(); got != 317 {
		t.Fatalf("section end = %d, want 317", got)
	}
	if got := root.EndByte(); got != uint32(len(source)) {
		t.Fatalf("root end = %d, want %d", got, len(source))
	}
	if !iniDeferredCompatibilityAccepted(root, source, lang, resultCompatibilityResult{
		iniMypyEnableErrorContinuation: true,
		iniContinuationStart:           321,
		iniContinuationEnd:             402,
	}) {
		t.Fatal("iniDeferredCompatibilityAccepted = false, want true")
	}
}

func TestNormalizeIniMypyContinuationLinesStopAtBlockBoundary(t *testing.T) {
	source := []byte("enable_error_code = \n    ignore-without-code,\n    redundant-expr,\n\n    should-not-continue,\n")
	first := iniFirstContinuationContentStart(source, []byte("enable_error_code = \n"))
	lines := iniContinuationLinesFrom(source, first)
	if got := len(lines); got != 2 {
		t.Fatalf("blank-terminated continuation lines = %d, want 2", got)
	}

	source = []byte("enable_error_code = \n    ignore-without-code,\n    # stop here\n    should-not-continue,\n")
	first = iniFirstContinuationContentStart(source, []byte("enable_error_code = \n"))
	lines = iniContinuationLinesFrom(source, first)
	if got := len(lines); got != 1 {
		t.Fatalf("comment-terminated continuation lines = %d, want 1", got)
	}

	source = []byte("enable_error_code = \n    ignore-without-code,\nnext = setting\n    should-not-continue,\n")
	first = iniFirstContinuationContentStart(source, []byte("enable_error_code = \n"))
	lines = iniContinuationLinesFrom(source, first)
	if got := len(lines); got != 1 {
		t.Fatalf("setting-terminated continuation lines = %d, want 1", got)
	}
}

func TestNormalizeIniDeferredAcceptanceRequiresAppliedContinuationRepair(t *testing.T) {
	lang := testIniMypyLanguage()
	source := []byte("[mypy]\nenable_error_code = \n    ignore-without-code,\n")
	arena := newNodeArena(arenaClassFull)
	section := newParentNodeInArena(arena, testIniSymbol(lang, "section"), true, []*Node{
		testIniLeaf(arena, lang, "section_name", source, 0, 7),
		testIniLeaf(arena, lang, "setting", source, 7, 28),
	}, nil, 0)
	section.endByte = iniMypyEnableErrorMarkerEnd(source)
	section.endPoint = advancePointByBytes(Point{}, source[:section.endByte])
	errChild := testIniLeaf(arena, lang, "setting_name", source, 32, 52)
	errNode := newParentNodeInArena(arena, errorSymbol, true, []*Node{errChild}, nil, 0)
	errNode.setHasError(true)
	errNode.startByte = 32
	errNode.startPoint = advancePointByBytes(Point{}, source[:32])
	errNode.endByte = 52
	errNode.endPoint = advancePointByBytes(Point{}, source[:52])
	root := newParentNodeInArena(arena, testIniSymbol(lang, "document"), true, []*Node{section, errNode}, nil, 0)
	root.setHasError(true)
	root.endByte = uint32(len(source))
	root.endPoint = advancePointByBytes(Point{}, source)
	tree := &Tree{
		root:     root,
		source:   source,
		language: lang,
		parseRuntime: ParseRuntime{
			StopReason:  ParseStopNoStacksAlive,
			RootEndByte: root.endByte,
		},
	}

	tree.finishDeferredResultCompatibility(resultCompatibilityResult{})
	if got := tree.ParseStopReason(); got != ParseStopNoStacksAlive {
		t.Fatalf("stop reason without repair result = %q, want %q", got, ParseStopNoStacksAlive)
	}

	tree.parseRuntime.StopReason = ParseStopNoStacksAlive
	tree.finishDeferredResultCompatibility(resultCompatibilityResult{
		iniMypyEnableErrorContinuation: true,
		iniContinuationStart:           32,
		iniContinuationEnd:             51,
	})
	if got := tree.ParseStopReason(); got != ParseStopNoStacksAlive {
		t.Fatalf("stop reason with wrong repair span = %q, want %q", got, ParseStopNoStacksAlive)
	}
}

func TestParserReturnedTreeNormalizationPreservesDeferredIniCompatibility(t *testing.T) {
	lang := testIniMypyLanguage()
	source := []byte("[mypy]\nfiles = Tools/check-c-api-docs/\npretty = True\n\n# We need `_colorize` import:\nmypy_path = $MYPY_CONFIG_FILE_DIR/../../Misc/mypy\n\n# Make sure Python can still be built\n# using Python 3.13 for `PYTHON_FOR_REGEN`...\npython_version = 3.13\n\n# ...And be strict:\nstrict = True\nextra_checks = True\nenable_error_code = \n    ignore-without-code,\n    redundant-expr,\n    truthy-bool,\n    possibly-undefined,\n")
	arena := newNodeArena(arenaClassFull)
	root := newParentNodeInArena(arena, testIniSymbol(lang, "document"), true, []*Node{
		testIniLeaf(arena, lang, "section_name", source, 0, 7),
		testIniLeaf(arena, lang, "setting", source, 7, 39),
		testIniLeaf(arena, lang, "setting", source, 39, 54),
		testIniLeaf(arena, lang, "_blank", source, 53, 54),
		testIniLeaf(arena, lang, "comment", source, 54, 84),
		testIniLeaf(arena, lang, "setting", source, 84, 135),
		testIniLeaf(arena, lang, "_blank", source, 134, 135),
		testIniLeaf(arena, lang, "comment", source, 135, 173),
		testIniLeaf(arena, lang, "comment", source, 173, 219),
		testIniLeaf(arena, lang, "setting", source, 219, 242),
		testIniLeaf(arena, lang, "_blank", source, 241, 242),
		testIniLeaf(arena, lang, "comment", source, 242, 262),
		testIniLeaf(arena, lang, "setting", source, 262, 276),
		testIniLeaf(arena, lang, "setting", source, 276, 296),
		testIniLeaf(arena, lang, "setting", source, 296, 317),
		testIniLeaf(arena, lang, "setting_name", source, 321, 341),
	}, nil, 0)
	root.endByte = 402
	root.endPoint = advancePointByBytes(Point{}, source[:402])
	tree := &Tree{
		root:     root,
		source:   source,
		language: lang,
		parseRuntime: ParseRuntime{
			StopReason:  ParseStopNoStacksAlive,
			RootEndByte: root.endByte,
		},
	}
	tree.deferResultCompatibility()

	parser := &Parser{language: lang}
	parser.normalizeReturnedTreeForParse(tree, source)
	if !tree.hasDeferredResultCompatibility() {
		t.Fatal("deferred compatibility finalizer missing after parser-return normalization")
	}
	// ParseStopReason is a public read of state that deferred INI compatibility
	// may mutate, so it must join the same one-shot finalization boundary as
	// RootNode instead of racing a concurrent first tree read.
	if got := tree.ParseStopReason(); got != ParseStopAccepted {
		t.Fatalf("stop reason after deferred accessor finalization = %q, want %q", got, ParseStopAccepted)
	}

	if root := tree.RootNode(); root == nil {
		t.Fatal("RootNode() = nil")
	}
	if got := tree.ParseStopReason(); got != ParseStopAccepted {
		t.Fatalf("stop reason after deferred compatibility = %q, want %q", got, ParseStopAccepted)
	}
}

func TestNormalizeIniMypyContinuationGuardRequiresContinuationPattern(t *testing.T) {
	lang := testIniMypyLanguage()
	source := []byte("[mypy]\nenable_error_code = ignore-without-code\n")
	arena := newNodeArena(arenaClassFull)
	setting := testIniLeaf(arena, lang, "setting", source, 7, uint32(len(source)))
	section := newParentNodeInArena(arena, testIniSymbol(lang, "section"), true, []*Node{setting}, nil, 0)
	root := newParentNodeInArena(arena, testIniSymbol(lang, "document"), true, []*Node{section}, nil, 0)

	normalizeIniCompatibility(root, source, lang)

	if got := root.Type(lang); got != "document" {
		t.Fatalf("root type = %q, want unchanged document", got)
	}
	if got := root.ChildCount(); got != 1 {
		t.Fatalf("root child count = %d, want unchanged 1", got)
	}
	if iniDeferredCompatibilityAccepted(root, source, lang, resultCompatibilityResult{
		iniMypyEnableErrorContinuation: true,
	}) {
		t.Fatal("iniDeferredCompatibilityAccepted = true, want false")
	}
}

func testIniMypyLanguage() *Language {
	return &Language{
		Name:        "ini",
		SymbolNames: []string{"EOF", "document", "section", "section_name", "setting", "setting_name", "setting_value", "comment", "text", "_blank", "="},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "document", Visible: true, Named: true},
			{Name: "section", Visible: true, Named: true},
			{Name: "section_name", Visible: true, Named: true},
			{Name: "setting", Visible: true, Named: true},
			{Name: "setting_name", Visible: true, Named: true},
			{Name: "setting_value", Visible: true, Named: true},
			{Name: "comment", Visible: true, Named: true},
			{Name: "text", Visible: true, Named: true},
			{Name: "_blank", Visible: true, Named: true},
			{Name: "=", Visible: true, Named: false},
		},
	}
}

func testIniSymbol(lang *Language, name string) Symbol {
	sym, ok := symbolByName(lang, name)
	if !ok {
		panic(name)
	}
	return sym
}

func testIniLeaf(arena *nodeArena, lang *Language, name string, source []byte, start, end uint32) *Node {
	sym := testIniSymbol(lang, name)
	return newLeafNodeInArena(arena, sym, symbolIsNamed(lang, sym), start, end, advancePointByBytes(Point{}, source[:start]), advancePointByBytes(Point{}, source[:end]))
}

func TestNormalizeZigEmptyInitListFieldConstantCleared(t *testing.T) {
	lang := &Language{
		Name:        "zig",
		SymbolNames: []string{"EOF", "SuffixExpr", ".", "InitList", "{", "}"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "SuffixExpr", Visible: true, Named: true},
			{Name: ".", Visible: true, Named: false},
			{Name: "InitList", Visible: true, Named: true},
			{Name: "{", Visible: true, Named: false},
			{Name: "}", Visible: true, Named: false},
		},
		FieldNames: []string{"", "field_constant"},
	}

	arena := newNodeArena(arenaClassFull)
	dot := newLeafNodeInArena(arena, 2, false, 0, 1, Point{}, Point{Column: 1})
	open := newLeafNodeInArena(arena, 4, false, 1, 2, Point{Column: 1}, Point{Column: 2})
	close := newLeafNodeInArena(arena, 5, false, 2, 3, Point{Column: 2}, Point{Column: 3})
	initList := newParentNodeInArena(arena, 3, true, []*Node{open, close}, nil, 0)
	parent := newParentNodeInArena(arena, 1, true, []*Node{dot, initList}, []FieldID{0, 1}, 0)
	parent.setFieldSources([]uint8{0, fieldSourceDirect})

	normalizeZigEmptyInitListFields(parent, lang)

	if got := parent.fieldIDs()[1]; got != 0 {
		t.Fatalf("fieldIDs[1] = %d, want 0", got)
	}
	if got := fieldSourceAt(parent.fieldSources(), 1); got != 0 {
		t.Fatalf("fieldSources[1] = %d, want 0", got)
	}
}

func TestNormalizeZigDottedInitListFieldConstantCleared(t *testing.T) {
	lang := &Language{
		Name:        "zig",
		SymbolNames: []string{"EOF", "SuffixExpr", ".", "InitList", "{", "STRINGLITERALSINGLE", "}"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF", Visible: false, Named: false},
			{Name: "SuffixExpr", Visible: true, Named: true},
			{Name: ".", Visible: true, Named: false},
			{Name: "InitList", Visible: true, Named: true},
			{Name: "{", Visible: true, Named: false},
			{Name: "STRINGLITERALSINGLE", Visible: true, Named: true},
			{Name: "}", Visible: true, Named: false},
		},
		FieldNames: []string{"", "field_constant"},
	}

	arena := newNodeArena(arenaClassFull)
	dot := newLeafNodeInArena(arena, 2, false, 0, 1, Point{}, Point{Column: 1})
	open := newLeafNodeInArena(arena, 4, false, 1, 2, Point{Column: 1}, Point{Column: 2})
	value := newLeafNodeInArena(arena, 5, true, 2, 6, Point{Column: 2}, Point{Column: 6})
	close := newLeafNodeInArena(arena, 6, false, 6, 7, Point{Column: 6}, Point{Column: 7})
	initList := newParentNodeInArena(arena, 3, true, []*Node{open, value, close}, nil, 0)
	parent := newParentNodeInArena(arena, 1, true, []*Node{dot, initList}, []FieldID{0, 1}, 0)
	parent.setFieldSources([]uint8{0, fieldSourceDirect})

	normalizeZigEmptyInitListFields(parent, lang)

	if got := parent.fieldIDs()[1]; got != 0 {
		t.Fatalf("fieldIDs[1] = %d, want 0", got)
	}
	if got := fieldSourceAt(parent.fieldSources(), 1); got != 0 {
		t.Fatalf("fieldSources[1] = %d, want 0", got)
	}
}
