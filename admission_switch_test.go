package gotreesitter_test

import (
	"bytes"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// These tests exercise the Phase-3 admission switch through the public API. They
// are build-agnostic: they assert routing EVENTS (a full parse either serves the
// candidate route or falls back), not which engine served the parse, so they
// hold in both the default and the gts_parsercorephase0 builds. The digest and
// engine-identity proofs live in the tagged internal tests.

func admissionRoutingEvents(t *testing.T) uint64 {
	t.Helper()
	routed, fallback := gts.AdmissionCandidateCounters()
	return routed + fallback
}

// newAdmissionDFAParser returns a parser for the first candidate DFA-backed
// language whose smoke sample parses to a clean, full-span tree in production.
// The DFA Parse path is the one the admission switch can route.
func newAdmissionDFAParser(t *testing.T) (*gts.Parser, []byte) {
	t.Helper()
	for _, name := range []string{"go", "lua", "toml", "ini", "json5", "css"} {
		entry := grammars.DetectLanguageByName(name)
		if entry == nil {
			continue
		}
		lang := entry.Language()
		if grammars.EvaluateParseSupport(*entry, lang).Backend != grammars.ParseBackendDFA {
			continue
		}
		source := []byte(grammars.ParseSmokeSample(name))
		probe := gts.NewParser(lang)
		probe.SetAdmissionCandidateRoute(false)
		tree, err := probe.Parse(source)
		if err != nil || tree == nil || tree.RootNode() == nil {
			continue
		}
		clean := tree.RootNode().EndByte() == uint32(len(source)) && !tree.RootNode().HasError()
		tree.Release()
		if !clean {
			continue
		}
		return gts.NewParser(lang), source
	}
	t.Skip("no clean DFA-backed language available for the routing test")
	return nil, nil
}

func requireCleanFullTree(t *testing.T, tree *gts.Tree, source []byte, label string) {
	t.Helper()
	if tree == nil || tree.RootNode() == nil {
		t.Fatalf("%s: nil tree/root", label)
	}
	root := tree.RootNode()
	if root.EndByte() != uint32(len(source)) {
		t.Fatalf("%s: truncated root end=%d source=%d", label, root.EndByte(), len(source))
	}
	if root.HasError() {
		t.Fatalf("%s: tree has error nodes", label)
	}
}

// TestAdmissionSwitchDefaultLeavesParseOnProduction proves the shipped default
// (off) routes no parse through the candidate and moves no counter.
func TestAdmissionSwitchDefaultLeavesParseOnProduction(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(false)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	requireCleanFullTree(t, tree, source, "default-off")
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("default-off moved a routing counter: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchPerParserOnRoutesFreshParse proves a per-Parser override
// makes a fresh DFA Parse consult the candidate route exactly once.
func TestAdmissionSwitchPerParserOnRoutesFreshParse(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(false)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	parser.SetAdmissionCandidateRoute(true)
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	requireCleanFullTree(t, tree, source, "per-parser-on")
	if got := admissionRoutingEvents(t); got != before+1 {
		t.Fatalf("per-parser-on expected one routing event: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchPerParserOffOverridesGlobalOn proves precedence: a per-
// Parser off wins over a global-on default and consults no candidate route.
func TestAdmissionSwitchPerParserOffOverridesGlobalOn(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	parser.SetAdmissionCandidateRoute(false)
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	requireCleanFullTree(t, tree, source, "per-parser-off")
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("per-parser-off must override global-on and move no counter: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchGlobalOnRoutesFreshParse proves the global default alone
// (no per-Parser override) routes a fresh DFA Parse.
func TestAdmissionSwitchGlobalOnRoutesFreshParse(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	requireCleanFullTree(t, tree, source, "global-on")
	if got := admissionRoutingEvents(t); got != before+1 {
		t.Fatalf("global-on expected one routing event: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchParseIncrementalNeverConsultsCandidate proves that even
// with the switch forced on, ParseIncremental never consults the candidate
// route: it is a reuse-consuming path barred from compact trees.
func TestAdmissionSwitchParseIncrementalNeverConsultsCandidate(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	oldTree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("fresh parse: %v", err)
	}
	defer oldTree.Release()
	before := admissionRoutingEvents(t)

	edited := append(append([]byte(nil), source...), ' ')
	eof := admissionEOFPoint(source)
	edit := gts.InputEdit{
		StartByte:   uint32(len(source)),
		OldEndByte:  uint32(len(source)),
		NewEndByte:  uint32(len(source) + 1),
		StartPoint:  eof,
		OldEndPoint: eof,
		NewEndPoint: gts.Point{Row: eof.Row, Column: eof.Column + 1},
	}
	oldTree.Edit(edit)
	newTree, err := parser.ParseIncremental(edited, oldTree)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	if newTree != nil && newTree != oldTree {
		defer newTree.Release()
	}
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("ParseIncremental consulted the candidate route: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchCustomTokenSourceNeverConsultsCandidate proves that a
// caller-supplied token source is never eligible: the seam declines before any
// candidate attempt, so no counter moves.
func TestAdmissionSwitchCustomTokenSourceNeverConsultsCandidate(t *testing.T) {
	// Find any registered token-source-backed language (json is one).
	var entry *grammars.LangEntry
	for _, name := range []string{"json", "go", "typescript", "tsx", "javascript"} {
		e := grammars.DetectLanguageByName(name)
		if e != nil && e.TokenSourceFactory != nil {
			entry = e
			break
		}
	}
	if entry == nil {
		t.Skip("no token-source-backed language registered for the custom source test")
	}
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	lang := entry.Language()
	source := []byte(grammars.ParseSmokeSample(entry.Name))
	parser := gts.NewParser(lang)
	parser.SetAdmissionCandidateRoute(true)
	before := admissionRoutingEvents(t)
	tree, err := parser.ParseWithTokenSource(source, entry.TokenSourceFactory(source, lang))
	if err != nil {
		t.Fatalf("token-source parse: %v", err)
	}
	defer tree.Release()
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("custom token source consulted the candidate route: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchDeclinesWhenIncludedRangesSet proves the candidate route
// declines when included ranges are configured: the compact runner lexes the
// whole source and cannot honor ranges, so the parse stays on production.
func TestAdmissionSwitchDeclinesWhenIncludedRangesSet(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	parser.SetIncludedRanges([]gts.Range{{StartByte: 0, EndByte: uint32(len(source)), EndPoint: admissionEOFPoint(source)}})
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("included ranges must keep the parse on production: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchDeclinesWhenTimeoutSet proves the candidate route declines
// when a caller set a timeout: the compact scheduler does not poll deadlines, so
// the parse stays on production which honors it.
func TestAdmissionSwitchDeclinesWhenTimeoutSet(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	parser.SetTimeoutMicros(1_000_000)
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("a timeout must keep the parse on production: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchDeclinesWhenCancellationFlagSet proves the candidate route
// declines when a caller set a cancellation flag.
func TestAdmissionSwitchDeclinesWhenCancellationFlagSet(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	var flag uint32
	parser.SetCancellationFlag(&flag)
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("a cancellation flag must keep the parse on production: %d -> %d", before, got)
	}
}

// TestAdmissionSwitchInternalSubParsersPinnedToProduction proves that recovery,
// snippet, and injection sub-parsers are born pinned to the production route:
// even with the global default forced on, they are ineligible to route a
// compact fragment into recovery splicing or an injection subtree.
func TestAdmissionSwitchInternalSubParsersPinnedToProduction(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true) // global ON, the future-flip state

	entry := grammars.DetectLanguageByName("go")
	if entry == nil {
		t.Skip("go grammar not registered")
	}
	lang := entry.Language()

	snippet := gts.AcquireSnippetParserForTest(lang)
	defer gts.ReleaseSnippetParserForTest(snippet)
	if !gts.ParserPinnedToProductionForTest(snippet) {
		t.Fatal("recovery/snippet parser is not pinned to production")
	}
	if gts.ParserAdmissionEligibleForTest(snippet) {
		t.Fatal("recovery/snippet parser is eligible to route under global ON")
	}

	child := gts.InjectionChildParserForTest(lang)
	if !gts.ParserPinnedToProductionForTest(child) {
		t.Fatal("injection child parser is not pinned to production")
	}
	if gts.ParserAdmissionEligibleForTest(child) {
		t.Fatal("injection child parser is eligible to route under global ON")
	}
}

// TestAdmissionSwitchPooledParserScrubsRouteState proves ParserPool.applyDefaults
// clears the per-Parser override so a pooled parser follows the default again.
func TestAdmissionSwitchPooledParserScrubsRouteState(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(false)

	entry := grammars.DetectLanguageByName("go")
	if entry == nil {
		t.Skip("go grammar not registered")
	}
	pool := gts.NewParserPool(entry.Language())
	// A checked-out parser forced off, then returned, must not leak that override.
	p1 := gts.ParserPoolCheckoutForTest(pool)
	p1.SetAdmissionCandidateRoute(false)
	gts.ParserPoolReleaseForTest(pool, p1)

	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()
	p2 := gts.ParserPoolCheckoutForTest(pool)
	defer gts.ParserPoolReleaseForTest(pool, p2)
	if !gts.ParserAdmissionEligibleForTest(p2) {
		t.Fatal("pooled parser did not scrub its route override; a forced-off override leaked across checkouts")
	}
}

// TestAdmissionSwitchEnvVarContract proves GTS_ADMISSION_CANDIDATE seeds the
// process-wide default: init calls this same parser at package load.
//
// Phase-3 admission made the compact route the default, so an unset or
// unrecognized value resolves ON; only an explicit off value forces the
// production route (the escape hatch).
func TestAdmissionSwitchEnvVarContract(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"1", true}, {"true", true}, {"on", true}, {"yes", true}, {"YES", true},
		{"", true}, {"nonsense", true},
		{"0", false}, {"false", false}, {"off", false}, {"no", false}, {"NO", false},
	} {
		t.Setenv("GTS_ADMISSION_CANDIDATE", tc.value)
		if got := gts.AdmissionCandidateEnvEnabledForTest(); got != tc.want {
			t.Fatalf("GTS_ADMISSION_CANDIDATE=%q enabled=%v want %v", tc.value, got, tc.want)
		}
	}
}

// TestAdmissionSwitchDeclinesWhenLoggerAttached proves the candidate route
// preserves callback fidelity: with a logger attached and the switch on, the
// parse stays on production (no routing event) so every logger callback fires.
func TestAdmissionSwitchDeclinesWhenLoggerAttached(t *testing.T) {
	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)
	gts.ResetAdmissionCandidateCountersForTest()

	parser, source := newAdmissionDFAParser(t)
	parser.SetLogger(func(gts.ParserLogType, string) {})
	before := admissionRoutingEvents(t)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer tree.Release()
	requireCleanFullTree(t, tree, source, "logger-attached")
	if got := admissionRoutingEvents(t); got != before {
		t.Fatalf("an attached logger must keep the parse on production: %d -> %d", before, got)
	}
}

// TestAdmissionCandidateMemoryBudgetContractPreserved proves the Phase-3 size
// gate keeps the automatic large-input memory budget contract intact even with
// the candidate route on by default.
//
// The compact scheduler does not poll the automatic memory budget, so the
// switch declines every input at or above the source-length floor where the
// production route arms that budget (parseRuntimeMemoryMinSourceBytes, 64 KiB).
// Such inputs stay on production and honor ParseStopMemoryBudget. Inputs below
// the floor, where no budget arms on either route, still route freely.
func TestAdmissionCandidateMemoryBudgetContractPreserved(t *testing.T) {
	// Mirror parseRuntimeMemoryMinSourceBytes (parser_memory_budget_runtime.go).
	const minBudgetSourceBytes = 64 * 1024

	restore := gts.AdmissionCandidateRouteDefault()
	defer gts.SetAdmissionCandidateRouteDefault(restore)
	gts.SetAdmissionCandidateRouteDefault(true)

	lang := grammars.GoLanguage()

	// Positive control: a small clean source below the floor routes.
	small := []byte(grammars.ParseSmokeSample("go"))
	if len(small) == 0 || len(small) >= minBudgetSourceBytes {
		t.Skipf("go smoke sample unsuitable for the control (%d bytes)", len(small))
	}
	gts.ResetAdmissionCandidateCountersForTest()
	smallParser := gts.NewParser(lang)
	smallTree, err := smallParser.Parse(small)
	if err != nil {
		t.Fatalf("small parse: %v", err)
	}
	smallTree.Release()
	if routed, _ := gts.AdmissionCandidateCounters(); routed == 0 {
		t.Fatalf("small clean source below the floor did not route to the candidate; routed=%d", routed)
	}

	// A clean source at or above the floor must NOT route: the size gate keeps
	// it on production even with the switch on.
	var big bytes.Buffer
	big.WriteString("package p\n\nfunc f() {\n")
	for big.Len() < minBudgetSourceBytes+(1<<15) {
		big.WriteString("\t_ = 1\n")
	}
	big.WriteString("}\n")
	if big.Len() < minBudgetSourceBytes {
		t.Fatalf("large control source too small: %d", big.Len())
	}
	gts.ResetAdmissionCandidateCountersForTest()
	bigParser := gts.NewParser(lang)
	bigTree, err := bigParser.Parse(big.Bytes())
	if err != nil {
		t.Fatalf("large parse: %v", err)
	}
	bigTree.Release()
	if routed, _ := gts.AdmissionCandidateCounters(); routed != 0 {
		t.Fatalf("source of %d bytes (>= %d floor) routed to the candidate; the memory-budget size gate did not decline it (routed=%d)",
			big.Len(), minBudgetSourceBytes, routed)
	}

	// With a low budget, a pathological large source on production honors
	// ParseStopMemoryBudget -- the exact contract the size gate preserves by
	// routing every budget-eligible input to production.
	t.Setenv("GOT_PARSE_MEMORY_BUDGET_MB", "1")
	gts.ResetParseEnvConfigCacheForTests()
	defer gts.ResetParseEnvConfigCacheForTests()

	var huge bytes.Buffer
	huge.WriteString("package p\nfunc f() {\n")
	for i := 0; i < 20000; i++ {
		huge.WriteString("var x = 1\n")
	}
	huge.WriteString("}\n")
	gts.ResetAdmissionCandidateCountersForTest()
	hugeParser := gts.NewParser(lang)
	hugeTree, err := hugeParser.Parse(huge.Bytes())
	if err != nil {
		t.Fatalf("huge parse: %v", err)
	}
	defer hugeTree.Release()
	if routed, _ := gts.AdmissionCandidateCounters(); routed != 0 {
		t.Fatalf("budgeted large source routed to the candidate; routed=%d", routed)
	}
	if got := hugeTree.ParseStopReason(); got != gts.ParseStopMemoryBudget {
		t.Fatalf("ParseStopReason() = %q, want %q (production must honor the budget the candidate cannot poll)",
			got, gts.ParseStopMemoryBudget)
	}
}

// TestAdmissionCandidateGoTypeConversionKnownDivergence records a KNOWN, TRACKED
// correctness divergence the Phase-3 admission flip surfaced. On the ambiguous
// Go construct `Foo[int](a)` (type conversion versus a call of an index
// expression) the compact candidate route resolves the GLR conflict to
// call_expression(index_expression), while the production engine and the
// tree-sitter-go C oracle resolve it to type_conversion_expression(generic_type).
//
// This divergence is NOT covered by the 206-language admission scorecard
// fixtures. It is a BLOCKER for unconditional Go admission and must be resolved
// by the engine-swap review: the compact scheduler must reproduce the production
// conflict resolution or fail closed on this conflict class. When that lands,
// the candidate tree matches production and this test fails loudly, directing
// the maintainer to route TestParseGoNewMakeGenericInstantiationUntouched again
// and remove this tracker.
func TestAdmissionCandidateGoTypeConversionKnownDivergence(t *testing.T) {
	src := "package p\n\n" +
		"type Foo[T any] struct {\n\tV T\n}\n\n" +
		"func f() {\n" +
		"\ta := Foo[int]{}\n" +
		"\tb := Foo[int](a)\n" +
		"\t_ = a\n" +
		"\t_ = b\n" +
		"}\n"
	lang := grammars.GoLanguage()

	prod := gts.NewParser(lang)
	prod.SetAdmissionCandidateRoute(false)
	prodTree, err := prod.Parse([]byte(src))
	if err != nil {
		t.Fatalf("production parse: %v", err)
	}
	defer prodTree.Release()
	prodSExpr := prodTree.RootNode().SExpr(lang)

	gts.ResetAdmissionCandidateCountersForTest()
	cand := gts.NewParser(lang)
	cand.SetAdmissionCandidateRoute(true)
	candTree, err := cand.Parse([]byte(src))
	if err != nil {
		t.Fatalf("candidate parse: %v", err)
	}
	defer candTree.Release()
	routed, _ := gts.AdmissionCandidateCounters()
	candSExpr := candTree.RootNode().SExpr(lang)

	if routed == 0 {
		t.Skip("candidate route declined this Go input; the known divergence no longer reproduces via routing")
	}
	if !strings.Contains(prodSExpr, "type_conversion_expression") {
		t.Fatalf("production reference lost type_conversion_expression; the C-oracle witness changed: %s", prodSExpr)
	}
	if prodSExpr == candSExpr {
		t.Fatalf("KNOWN divergence resolved: the candidate now matches production for Foo[int](a). "+
			"Re-route TestParseGoNewMakeGenericInstantiationUntouched and remove this tracker.\n%s", candSExpr)
	}
	t.Logf("KNOWN candidate-route divergence (BLOCKER for unconditional Go admission):\n  production: %s\n  candidate:  %s",
		prodSExpr, candSExpr)
}

// admissionEOFPoint returns the row/column point at the end of source.
func admissionEOFPoint(source []byte) gts.Point {
	var row, col uint32
	for _, b := range source {
		if b == '\n' {
			row++
			col = 0
			continue
		}
		col++
	}
	return gts.Point{Row: row, Column: col}
}
