//go:build cgo && treesitter_c_parity

package cgoharness

// Forest-vs-C oracle gate. Unlike TestForestCorpusParity (forest vs the
// production parser), this compares the GSS-forest fast path DIRECTLY against
// tree-sitter-c, skipping the production leg entirely. That matters for
// languages whose production parse is too slow to use as the parity baseline —
// notably haskell, whose O(n^2) deep-merge blowup makes the production-vs-forest
// gate time out. Here the forest is the only gotreesitter parse we run, so a
// pathologically slow production path can't block vetting the forest.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gts "github.com/odvcencio/gotreesitter"
)

// TestForestVsCOracleParity asserts that, for every real-corpus file the forest
// would DISPATCH (clean, no error node, reaches the last non-whitespace byte),
// the forest tree is deeply identical to the tree-sitter-c oracle: node type,
// byte range, points, named/extra/missing/error flags, all children, and fields.
// Files the forest declines fall back to production at runtime and are NOT
// required to match C here. It also reports dispatch rate and the C-vs-forest
// wall speedup, which is the "is it worth promoting" half of the decision.
//
// The forest tree comes from ParseForestExperimental, which finalizes the root
// through the same finalizeResultRoot -> per-language compatibility pass the
// runtime forest path uses, so it is a faithful stand-in for what promotion
// would return.
//
// Heavy (real corpus + CGo) -> opt-in:
//
//	GTS_FOREST_ORACLE=1 GTS_FOREST_ORACLE_LANGS=haskell \
//	  go test ./cgo_harness -tags treesitter_c_parity -run TestForestVsCOracleParity -v
func TestForestVsCOracleParity(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GTS_FOREST_ORACLE")) == "" {
		t.Skip("set GTS_FOREST_ORACLE=1 to run the forest-vs-C oracle gate")
	}
	setup := loadForestOracleSetup(t)
	anyRun := false
	for _, raw := range setup.langs {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		lang, cLang, files, ok := setup.language(t, name)
		if !ok {
			continue
		}
		anyRun = true
		result := runForestCOracleAudit(t, name, lang, cLang, files, setup)
		if setup.resultOut != "" {
			if err := WriteForestAuditResult(setup.resultOut, result); err != nil {
				t.Fatalf("write forest C-oracle result: %v", err)
			}
		}
	}
	if !anyRun {
		t.Skip("no forest-oracle corpus available for requested languages")
	}
}

type forestOracleSetup struct {
	langs        []string
	loaders      map[string]func() *gts.Language
	manifestPath string
	resultOut    string
	manifest     ForestCorpusManifest
	files        map[string][]string
}

func loadForestOracleSetup(t *testing.T) forestOracleSetup {
	t.Helper()
	setup := forestOracleSetup{
		langs:        strings.Split(envOr("GTS_FOREST_ORACLE_LANGS", "haskell"), ","),
		loaders:      forestLanguageLoaders(),
		manifestPath: strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_MANIFEST")),
		resultOut:    strings.TrimSpace(os.Getenv("GTS_FOREST_AUDIT_RESULT_OUT")),
	}
	var err error
	setup.manifest, setup.files, err = loadForestCorpusManifestLanguages(
		setup.manifestPath,
		strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_ROOT")),
		strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LOCK_PATH")),
		strings.TrimSpace(os.Getenv("GTS_FOREST_GOTREESITTER_REVISION")),
		strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LOCK_SHA256")),
		setup.langs,
	)
	if err != nil {
		t.Fatalf("authenticate forest corpus manifest: %v", err)
	}
	if setup.resultOut != "" && len(setup.langs) != 1 {
		t.Fatal("GTS_FOREST_AUDIT_RESULT_OUT requires one authenticated language")
	}
	return setup
}

func (setup forestOracleSetup) language(t *testing.T, name string) (*gts.Language, *sitter.Language, []string, bool) {
	t.Helper()
	load, ok := setup.loaders[name]
	if !ok {
		t.Errorf("%s: unknown language (not in grammars.AllLanguages)", name)
		return nil, nil, nil, false
	}
	lang := load()
	cLang, err := ParityCLanguage(name)
	if err != nil {
		if reason := parityReferenceSkipReason(err); reason != "" {
			t.Logf("%s: skip — no C reference parser: %s", name, reason)
			return nil, nil, nil, false
		}
		t.Errorf("%s: ParityCLanguage: %v", name, err)
		return nil, nil, nil, false
	}
	files := setup.files[name]
	if len(files) == 0 {
		t.Errorf("%s: requested language absent from authenticated manifest", name)
		return nil, nil, nil, false
	}
	return lang, cLang, files, true
}

type forestCOracleAudit struct {
	t               *testing.T
	name            string
	lang            *gts.Language
	cLang           *sitter.Language
	metadata        map[string]ForestCorpusManifestFile
	result          ForestAuditResult
	total           int
	dispatched      int
	fellBack        int
	diverged        int
	cNanos          int64
	forestNanos     int64
	divergedFiles   []string
	fallbackReasons map[string]int
}

func runForestCOracleAudit(t *testing.T, name string, lang *gts.Language, cLang *sitter.Language, files []string, setup forestOracleSetup) ForestAuditResult {
	t.Helper()
	result, err := NewForestAuditResult("c_oracle", setup.manifest.GotreesitterRevision, setup.manifestPath, setup.manifest.CorpusLock.SHA256, name)
	if err != nil {
		t.Fatalf("initialize forest C-oracle result: %v", err)
	}
	audit := &forestCOracleAudit{
		t: t, name: name, lang: lang, cLang: cLang, result: result,
		metadata:        forestManifestMetadata(setup.manifest, name, files),
		fallbackReasons: map[string]int{},
	}
	for _, filePath := range files {
		audit.evaluateFile(filePath)
	}
	return audit.finish()
}

func (audit *forestCOracleAudit) evaluateFile(filePath string) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		audit.t.Errorf("%s: read %s: %v", audit.name, filePath, err)
		return
	}
	audit.total++
	meta, ok := audit.metadata[filePath]
	if !ok {
		audit.t.Errorf("%s: authenticated metadata missing for %s", audit.name, filePath)
		return
	}
	notRun := forestAuditNotRunOutcome(src)
	fileResult := ForestAuditFileResult{
		Path: meta.Path, Bytes: meta.Bytes, SHA256: meta.SHA256,
		Forest: notRun, Peer: notRun, Routed: notRun, RoutedProvenance: forestAuditRouteNotRun,
	}
	defer func() { audit.result.Files = append(audit.result.Files, fileResult) }()

	forestTree, forestOK, timedOut, elapsed := parseForestWithBudget(audit.lang, src, forestOracleBudget())
	audit.forestNanos += elapsed
	if timedOut {
		audit.recordDecline("timeout", &fileResult)
		return
	}
	if forestTree != nil {
		defer forestTree.Release()
	}
	var forestRoot *gts.Node
	if forestTree != nil {
		forestRoot = forestTree.RootNode()
	}
	fileResult.Forest = forestGoTreeOutcome(forestTree, src)
	if audit.forestDeclined(src, forestOK, forestRoot, fileResult.Forest, &fileResult) {
		return
	}
	audit.dispatched++
	fileResult.Disposition = "accepted"
	audit.compareCOracle(filePath, src, forestRoot, &fileResult)
}

type forestBudgetParseResult struct {
	tree *gts.Tree
	ok   bool
}

func parseForestWithBudget(lang *gts.Language, src []byte, budget time.Duration) (*gts.Tree, bool, bool, int64) {
	results := make(chan forestBudgetParseResult, 1)
	parser := gts.NewParser(lang)
	start := time.Now()
	go func() {
		tree, ok := parser.ParseForestExperimental(src)
		results <- forestBudgetParseResult{tree: tree, ok: ok}
	}()
	select {
	case result := <-results:
		return result.tree, result.ok, false, time.Since(start).Nanoseconds()
	case <-time.After(budget):
		// The parse goroutine intentionally survives until process exit. Its
		// eventual tree cannot be touched without racing the active parser.
		return nil, false, true, time.Since(start).Nanoseconds()
	}
}

func (audit *forestCOracleAudit) forestDeclined(src []byte, ok bool, root *gts.Node, outcome ForestAuditOutcome, fileResult *ForestAuditFileResult) bool {
	if ok && root != nil && outcome.Accepted && outcome.FullSpan && !root.HasError() {
		return false
	}
	reason := forestFallbackReason(ok, root, src)
	if ok && root != nil && (!outcome.Accepted || !outcome.FullSpan) {
		reason = "invalid_outcome"
	}
	audit.recordDecline(reason, fileResult)
	return true
}

func (audit *forestCOracleAudit) recordDecline(reason string, fileResult *ForestAuditFileResult) {
	audit.fellBack++
	audit.fallbackReasons[reason]++
	fileResult.Disposition = "declined"
	fileResult.Decline = reason
}

func (audit *forestCOracleAudit) compareCOracle(filePath string, src []byte, forestRoot *gts.Node, fileResult *ForestAuditFileResult) {
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(audit.cLang); err != nil {
		audit.recordDivergence(filePath, "C SetLanguage: "+err.Error(), fileResult)
		audit.t.Errorf("%s: C SetLanguage: %v", audit.name, err)
		return
	}
	start := time.Now()
	cTree := cParser.Parse(src, nil)
	audit.cNanos += time.Since(start).Nanoseconds()
	if cTree == nil || cTree.RootNode() == nil {
		fileResult.Peer = forestCTreeOutcome(cTree, src)
		fileResult.Disposition = "diverged"
		fileResult.Diff = "C oracle produced no tree"
		audit.diverged++
		audit.t.Errorf("%s: C produced no tree for %s", audit.name, filepath.Base(filePath))
		return
	}
	defer cTree.Close()
	fileResult.Peer = forestCTreeOutcome(cTree, src)
	if !fileResult.Peer.Accepted || !fileResult.Peer.FullSpan {
		audit.recordDivergence(filePath, "C oracle outcome not accepted/full-span", fileResult)
		return
	}
	if diff := completeForestCIdentityDiff(forestRoot, audit.lang, cTree.RootNode()); diff != "" {
		audit.recordDivergence(filePath, diff, fileResult)
		audit.t.Logf("%s: %s forest!=C: %s", audit.name, filepath.Base(filePath), diff)
	}
}

func (audit *forestCOracleAudit) recordDivergence(filePath, diff string, fileResult *ForestAuditFileResult) {
	audit.diverged++
	fileResult.Disposition = "diverged"
	fileResult.Diff = diff
	audit.divergedFiles = append(audit.divergedFiles, filepath.Base(filePath)+": "+diff)
}

func (audit *forestCOracleAudit) finish() ForestAuditResult {
	speedup := 0.0
	if audit.forestNanos > 0 {
		speedup = float64(audit.cNanos) / float64(audit.forestNanos)
	}
	audit.t.Logf("%-8s files=%d dispatched=%d fellback=%d diverged=%d | c=%.1fms forest=%.1fms forest_vs_c=%.2fx",
		audit.name, audit.total, audit.dispatched, audit.fellBack, audit.diverged,
		float64(audit.cNanos)/1e6, float64(audit.forestNanos)/1e6, speedup)
	if audit.fellBack > 0 {
		audit.t.Logf("%-8s fallback reasons: %s", audit.name, formatFallbackReasons(audit.fallbackReasons))
	}
	if audit.diverged > 0 {
		audit.t.Errorf("%s: %d/%d dispatched files DIVERGED from the C oracle (blocks forest promotion): %s",
			audit.name, audit.diverged, audit.dispatched, strings.Join(audit.divergedFiles, ", "))
	}
	audit.result.FilesTotal = audit.total
	audit.result.FilesAccepted = audit.dispatched
	audit.result.FilesDeclined = audit.fellBack
	audit.result.FilesDiverged = audit.diverged
	audit.result.ForestNanos = audit.forestNanos
	audit.result.PeerNanos = audit.cNanos
	if audit.diverged > 0 || audit.dispatched == 0 {
		audit.result.Status = "fail"
	}
	sort.Slice(audit.result.Files, func(i, j int) bool { return audit.result.Files[i].Path < audit.result.Files[j].Path })
	return audit.result
}

// forestOracleBudget is the per-file wall budget for a single forest parse.
// Overridable via GTS_FOREST_ORACLE_BUDGET (a Go duration, e.g. "30s").
func forestOracleBudget() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GTS_FOREST_ORACLE_BUDGET")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Second
}

func forestCTreeOutcome(tree *sitter.Tree, source []byte) ForestAuditOutcome {
	outcome := ForestAuditOutcome{
		SourceLen: uint32(len(source)), ExpectedEOF: uint32(len(source)), StopReason: "no_tree",
	}
	if tree == nil || tree.RootNode() == nil {
		return outcome
	}
	root := tree.RootNode()
	outcome.Present = true
	outcome.RootEndByte = uint32(root.EndByte())
	outcome.RootHasError = root.HasError()
	outcome.FullSpan = outcome.RootEndByte >= lastNonWSByte(source)
	outcome.Truncated = !outcome.FullSpan
	switch {
	case !outcome.FullSpan:
		outcome.StopReason = "truncated"
	case outcome.RootHasError:
		outcome.StopReason = "root_error"
	default:
		outcome.StopReason = "accepted"
		outcome.Accepted = true
	}
	return outcome
}

func completeForestCIdentityDiff(goNode *gts.Node, goLang *gts.Language, cNode *sitter.Node) string {
	path := make([]int, 0, 32)
	var walk func(*gts.Node, *sitter.Node) string
	walk = func(goNode *gts.Node, cNode *sitter.Node) string {
		location := forestIdentityPath(path)
		if goNode == nil || cNode == nil {
			if goNode == nil && cNode == nil {
				return ""
			}
			return fmt.Sprintf("%s presence go=%t c=%t", location, goNode != nil, cNode != nil)
		}
		if goNode.Type(goLang) != cNode.Kind() {
			return fmt.Sprintf("%s type go=%q c=%q", location, goNode.Type(goLang), cNode.Kind())
		}
		if goNode.StartByte() != uint32(cNode.StartByte()) || goNode.EndByte() != uint32(cNode.EndByte()) {
			return fmt.Sprintf("%s range go=%d:%d c=%d:%d", location, goNode.StartByte(), goNode.EndByte(), cNode.StartByte(), cNode.EndByte())
		}
		goStart, goEnd := goNode.StartPoint(), goNode.EndPoint()
		cStart, cEnd := cNode.StartPosition(), cNode.EndPosition()
		if goStart.Row != uint32(cStart.Row) || goStart.Column != uint32(cStart.Column) ||
			goEnd.Row != uint32(cEnd.Row) || goEnd.Column != uint32(cEnd.Column) {
			return fmt.Sprintf("%s points go=%v:%v c=%v:%v", location, goStart, goEnd, cStart, cEnd)
		}
		if goNode.IsNamed() != cNode.IsNamed() {
			return fmt.Sprintf("%s named go=%t c=%t", location, goNode.IsNamed(), cNode.IsNamed())
		}
		if goNode.IsExtra() != cNode.IsExtra() {
			return fmt.Sprintf("%s extra go=%t c=%t", location, goNode.IsExtra(), cNode.IsExtra())
		}
		if goNode.IsMissing() != cNode.IsMissing() {
			return fmt.Sprintf("%s missing go=%t c=%t", location, goNode.IsMissing(), cNode.IsMissing())
		}
		if goNode.HasError() != cNode.HasError() {
			return fmt.Sprintf("%s has_error go=%t c=%t", location, goNode.HasError(), cNode.HasError())
		}
		goChildren, cChildren := goNode.ChildCount(), int(cNode.ChildCount())
		if goChildren != cChildren {
			return fmt.Sprintf("%s child_count go=%d c=%d", location, goChildren, cChildren)
		}
		for i := 0; i < goChildren; i++ {
			path = append(path, i)
			goField, cField := goNode.FieldNameForChild(i, goLang), cNode.FieldNameForChild(uint32(i))
			if goField != cField {
				diff := fmt.Sprintf("%s field go=%q c=%q", forestIdentityPath(path), goField, cField)
				path = path[:len(path)-1]
				return diff
			}
			if diff := walk(goNode.Child(i), cNode.Child(uint(i))); diff != "" {
				path = path[:len(path)-1]
				return diff
			}
			path = path[:len(path)-1]
		}
		return ""
	}
	return walk(goNode, cNode)
}
