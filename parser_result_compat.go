package gotreesitter

type resultCompatibilityContext struct {
	root      *Node
	source    []byte
	parser    *Parser
	lang      *Language
	stopCheck parseStopCheck
	// incrementalRanges confines range-capable result normalizers to the
	// reparsed spans of an incremental parse (campaign O(edit),
	// spec.campaign.oedit). It is nil on every fresh parse and on any
	// incremental parse without reuse, which restores the full-tree walk. Only
	// languages proven node-local for range-limiting consume it; every other
	// language ignores it and keeps its full walk (fail-closed).
	incrementalRanges []Range
}

type resultCompatibilityResult struct {
	stopReason                     ParseStopReason
	iniMypyEnableErrorContinuation bool
	iniContinuationStart           uint32
	iniContinuationEnd             uint32
	errorSummary                   resultErrorSummary
}

// normalizeResultCompatibility applies narrow post-build tree rewrites that
// keep gotreesitter output aligned with C tree-sitter and existing recovery
// expectations for grammars with known normalization gaps.
func normalizeResultCompatibility(root *Node, source []byte, p *Parser, incrementalRanges []Range) resultCompatibilityResult {
	var lang *Language
	if p != nil {
		lang = p.language
	}
	if root == nil || lang == nil {
		return resultCompatibilityResult{}
	}
	ctx := resultCompatibilityContext{
		root:              root,
		source:            source,
		parser:            p,
		lang:              lang,
		stopCheck:         p.activeParseStopCheck(),
		incrementalRanges: incrementalRanges,
	}
	if reason := ctx.stopReason(); parseStopReasonIsActive(reason) {
		return resultCompatibilityResult{stopReason: reason}
	}
	result := runLanguageResultCompatibility(ctx)
	// resultMaterializationShouldStop (not parseStopReasonIsActive) here: Go's
	// normalizer (the only one that can produce it — see
	// normalizeGoReturnedTreeCompatibility) may now report ParseStopMemoryBudget,
	// which parseStopReasonIsActive deliberately excludes (many callers rely on
	// its narrower Timeout/Cancelled-only semantics). Without this, a
	// budget-stopped Go result would still fall through into the generic
	// trailing-trivia/terminal-leaf passes below.
	if resultMaterializationShouldStop(result.stopReason) {
		return result
	}
	if reason := ctx.stopReason(); parseStopReasonIsActive(reason) {
		result.stopReason = reason
		return result
	}
	normalizeRootTrailingExtraTriviaCompatibility(root, source, lang)
	var terminalReason ParseStopReason
	_, terminalReason, result.errorSummary = normalizeResultTerminalLeafNodesWithAliasTargetsAndStopAndErrorSummary(root, lang, p.visibleAliasTargetSymbol, ctx.stopCheck)
	if parseStopReasonIsActive(terminalReason) {
		result.stopReason = terminalReason
		return result
	}
	p.runNamedNormalizationPass("collapsed_named_leaf_children", func() bool { return true }, func() normalizationPassCounters {
		return normalizeResultCollapsedNamedLeafChildren(root, source, lang)
	})
	result.stopReason = ctx.stopReason()
	return result
}

func (ctx resultCompatibilityContext) stopReason() ParseStopReason {
	if ctx.stopCheck == nil {
		return ParseStopNone
	}
	reason := ctx.stopCheck()
	if reason == "" {
		return ParseStopNone
	}
	return reason
}

func normalizeRootTrailingExtraTriviaCompatibility(root *Node, source []byte, lang *Language) {
	if root == nil || root.hasError() {
		return
	}
	trimTrailingExtraTriviaRoot(root, source, lang)
}

func runLanguageResultCompatibility(ctx resultCompatibilityContext) resultCompatibilityResult {
	if isCobolLanguage(ctx.lang) {
		normalizeCobolCompatibility(ctx.root, ctx.source, ctx.lang)
		return resultCompatibilityResult{stopReason: ctx.stopReason()}
	}

	switch ctx.lang.Name {
	case "ada":
		normalizeAdaCompatibility(ctx.root, ctx.source, ctx.lang)
	case "angular":
		normalizeAngularCompatibility(ctx.root, ctx.source, ctx.lang)
	case "apex":
		normalizeApexCompatibility(ctx.root, ctx.source, ctx.lang)
	case "authzed":
		normalizeAuthzedCompatibility(ctx.root, ctx.source, ctx.lang)
	case "awk":
		normalizeAwkCompatibility(ctx.root, ctx.source, ctx.lang)
	case "bibtex":
		normalizeBibtexCompatibility(ctx.root, ctx.source, ctx.lang)
	case "bash":
		normalizeBashProgramVariableAssignments(ctx.root, ctx.lang)
		normalizeBashGeneratedCommandAssignments(ctx.root, ctx.source, ctx.lang)
		normalizeBashCommandNameArguments(ctx.root, ctx.lang)
	case "bitbake":
		normalizeBitbakeCompatibility(ctx.root, ctx.source, ctx.lang)
	case "chatito":
		normalizeChatitoCompatibility(ctx.root, ctx.source, ctx.lang)
	case "arduino":
		normalizeArduinoBuiltinPrimitiveTypes(ctx.root, ctx.source, ctx.lang)
	case "c", "cpp":
		normalizeCCompatibilityWithParser(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "c_sharp":
		normalizeCSharpCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "caddy", "comment", "fortran", "nim", "pug", "rst":
		normalizeResultTrailingSpanCompatibility(ctx.root, ctx.source, ctx.lang)
	case "cooklang":
		normalizeCooklangCompatibility(ctx.root, ctx.source, ctx.lang)
	case "corn":
		normalizeCornCompatibility(ctx.root, ctx.source, ctx.lang)
	case "crystal":
		normalizeCrystalCompatibility(ctx.root, ctx.source, ctx.lang)
	case "cpon":
		normalizeCPONCompatibility(ctx.root, ctx.source, ctx.lang)
	case "cue":
		normalizeCueCompatibility(ctx.root, ctx.source, ctx.lang)
	case "d":
		normalizeDCompatibility(ctx.root, ctx.source, ctx.lang)
	case "dart":
		normalizeDartCompatibility(ctx.root, ctx.source, ctx.lang)
	case "doxygen":
		normalizeDoxygenCompatibility(ctx.root, ctx.source, ctx.lang)
	case "jsdoc":
		normalizeJsdocCompatibility(ctx.root, ctx.source, ctx.lang)
	case "dtd":
		normalizeDTDCompatibility(ctx.root, ctx.source, ctx.lang)
	case "elixir":
		normalizeElixirCompatibility(ctx.root, ctx.source, ctx.lang)
	case "enforce":
		normalizeEnforceCompatibility(ctx.root, ctx.source, ctx.lang)
	case "ebnf":
		normalizeEBNFCompatibility(ctx.root, ctx.source, ctx.lang)
	case "eds":
		normalizeEDSCompatibility(ctx.root, ctx.source, ctx.lang)
	case "erlang":
		normalizeErlangSourceFileForms(ctx.root, ctx.lang)
	case "fsharp":
		normalizeFSharpCompatibility(ctx.root, ctx.source, ctx.lang)
	case "forth":
		normalizeForthCompatibility(ctx.root, ctx.source, ctx.lang)
	case "fidl":
		normalizeFIDLCompatibility(ctx.root, ctx.source, ctx.lang)
	case "go":
		return resultCompatibilityResult{stopReason: normalizeGoReturnedTreeCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang, ctx.incrementalRanges)}
	case "gitcommit":
		normalizeGitcommitCompatibility(ctx.root, ctx.source, ctx.lang)
	case "haskell":
		normalizeHaskellCompatibility(ctx.root, ctx.source, ctx.lang)
	case "hcl":
		normalizeHCLConfigFileRoot(ctx.root, ctx.source, ctx.lang)
	case "html":
		normalizeHTMLCompatibility(ctx.root, ctx.source, ctx.lang)
	case "http":
		normalizeHTTPCompatibility(ctx.root, ctx.source, ctx.lang)
	case "hurl":
		normalizeHurlCompatibility(ctx.root, ctx.lang)
	case "hlsl":
		normalizeHLSLCompatibility(ctx.root, ctx.source, ctx.lang)
	case "hyprlang":
		normalizeHyprlangCompatibility(ctx.root, ctx.source, ctx.lang)
	case "ini":
		return normalizeIniCompatibility(ctx.root, ctx.source, ctx.lang)
	case "javascript":
		return resultCompatibilityResult{stopReason: normalizeJavaScriptCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)}
	case "julia":
		normalizeJuliaCompatibility(ctx.root, ctx.source, ctx.lang)
	case "just":
		normalizeJustTopLevelTrailingLineBreakSpans(ctx.root, ctx.source, ctx.lang)
	case "ledger":
		normalizeLedgerCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "linkerscript":
		normalizeLinkerscriptCompatibility(ctx.root, ctx.source, ctx.lang)
	case "kotlin":
		normalizeKotlinCompatibility(ctx.root, ctx.source, ctx.lang)
	case "lua":
		normalizeLuaChunkLocalDeclarationFields(ctx.root, ctx.source, ctx.lang)
	case "luau":
		normalizeLuauCompatibility(ctx.root, ctx.source, ctx.lang)
	case "make":
		normalizeMakeConditionalConsequenceFields(ctx.root, ctx.lang)
	case "objc":
		normalizeObjcCompatibility(ctx.root, ctx.source, ctx.lang)
	case "nginx":
		normalizeNginxAttributeLineBreaks(ctx.root, ctx.source, ctx.lang)
	case "ninja":
		normalizeNinjaCompatibility(ctx.root, ctx.source, ctx.lang)
	case "ocaml":
		normalizeOCamlCompatibility(ctx.root, ctx.source, ctx.lang)
	case "pascal":
		normalizePascalTopLevelProgramEnd(ctx.root, ctx.source, ctx.lang)
		normalizePascalTrailingExtraTrivia(ctx.root, ctx.source, ctx.lang)
	case "perl":
		normalizePerlCompatibility(ctx.root, ctx.source, ctx.lang)
	case "php":
		normalizePHPCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "powershell":
		normalizePowerShellProgramShape(ctx.root, ctx.source, ctx.lang)
		normalizePowerShellErrorProgramRoot(ctx.root, ctx.lang)
		normalizePowerShellAssignmentOperatorTokens(ctx.root, ctx.source, ctx.lang)
		normalizePowerShellPathCommandNameVariables(ctx.root, ctx.source, ctx.lang)
		normalizePowerShellEnumStatementKeywordSpans(ctx.root, ctx.source, ctx.lang)
	case "ql":
		normalizeQLCompatibility(ctx.root, ctx.source, ctx.lang)
	case "r":
		normalizeRCompatibility(ctx.root, ctx.source, ctx.lang)
	case "python":
		normalizePythonCompatibilityWithParser(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "rescript":
		normalizeRescriptCompatibility(ctx.root, ctx.lang)
	case "robot":
		normalizeRobotCompatibility(ctx.root, ctx.source, ctx.lang)
	case "rust":
		normalizeRustCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "ruby":
		normalizeRubyTopLevelModuleBounds(ctx.root, ctx.source, ctx.lang)
	case "scala":
		normalizeScalaCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "scheme":
		normalizeSchemeCompatibility(ctx.root, ctx.source, ctx.lang)
	case "solidity":
		normalizeSolidityMemberObjectWrappers(ctx.root, ctx.lang)
		normalizeSolidityCallExpressionAliases(ctx.root, ctx.lang)
	case "sql":
		normalizeSQLRecoveredSelectRoot(ctx.root, ctx.lang)
		normalizeSQLTrailingSelectListError(ctx.root, ctx.lang)
		if ctx.parser != nil && !ctx.parser.skipRecoveryReparse {
			normalizeSQLRecoveredTopLevelSelectStatements(ctx.root, ctx.source, ctx.parser, ctx.lang)
		}
		normalizeSQLSelectClauseBodyIntoFields(ctx.root, ctx.lang)
	case "squirrel":
		normalizeSquirrelCompatibility(ctx.root, ctx.source, ctx.lang)
	case "swift":
		normalizeSwiftCompatibility(ctx.root, ctx.source, ctx.parser, ctx.lang)
	case "templ":
		normalizeTemplCompatibility(ctx.root, ctx.source, ctx.lang)
	case "wgsl":
		normalizeWGSLCompatibility(ctx.root, ctx.lang)
	case "wolfram":
		normalizeWolframCompatibility(ctx.root, ctx.source, ctx.lang)
	case "tsx", "typescript":
		return resultCompatibilityResult{stopReason: normalizeTypeScriptTreeCompatibilityWithParser(ctx.root, ctx.source, ctx.parser, ctx.lang)}
	case "typst":
		normalizeTypstCompatibility(ctx.root, ctx.source, ctx.lang)
	case "yaml":
		normalizeYAMLRecoveredRoot(ctx.root, ctx.source, ctx.lang)
	case "zig":
		normalizeZigEmptyInitListFields(ctx.root, ctx.lang)
	}
	return resultCompatibilityResult{stopReason: ctx.stopReason()}
}
