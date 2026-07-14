package cgoharness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	gts "github.com/odvcencio/gotreesitter"
	grm "github.com/odvcencio/gotreesitter/grammars"
)

// TestForestCorpusParity is the correctness gate for promoting a language onto
// the GSS-forest fast path (Parser.tryForestFastPath / builtinForestDefaults).
//
// The runtime dispatch falls back to the production parser whenever the forest
// declines (failure / error node / truncation), so it can never regress those
// cases. The ONE risk it cannot catch is a forest that produces a clean,
// complete, but STRUCTURALLY DIFFERENT tree — a silent divergence. This test
// closes that gap: for every authenticated real-corpus file it parses with the
// production parser and with the forest, and on every clean, complete result
// this conservative gate certifies, it asserts complete Go-tree identity. Any
// divergence fails the test and blocks default-on for that language. Certified
// error-bearing recovery profiles use their separate C-oracle gate.
//
// Languages come from GTS_FOREST_LANGS (comma-separated; default "bash") so a
// candidate (swift, fortran, ...) can be vetted before it is added to the
// runtime allowlist. It also reports dispatch rate and wall speedup, which is
// the "wall" half of "full parity wall and correctness".
//
// Run heavy (real corpus) under Docker per the repo's testing discipline:
//
//	cgo_harness/docker/run_forest_corpus_parity.sh
func TestForestCorpusParity(t *testing.T) {
	// Opt-in: this is a heavy real-corpus gate that currently FAILS by design
	// while the forest is pre-parity (it reports the divergences blocking
	// default-on). Keep it out of the default `go test ./...` run; the Docker
	// runner and manual promotion checks set GTS_FOREST_CORPUS=1.
	if strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS")) == "" {
		t.Skip("set GTS_FOREST_CORPUS=1 to run the forest real-corpus parity gate")
	}
	langs := strings.Split(envOr("GTS_FOREST_LANGS", "bash"), ",")
	loaders := forestLanguageLoaders()
	manifestPath := strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_MANIFEST"))
	resultOut := strings.TrimSpace(os.Getenv("GTS_FOREST_AUDIT_RESULT_OUT"))
	var manifestFiles map[string][]string
	var manifest ForestCorpusManifest
	if manifestPath != "" {
		var err error
		var files map[string][]string
		manifest, files, err = loadForestCorpusManifestLanguages(
			manifestPath,
			strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_ROOT")),
			strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LOCK_PATH")),
			strings.TrimSpace(os.Getenv("GTS_FOREST_GOTREESITTER_REVISION")),
			strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LOCK_SHA256")),
			langs,
		)
		if err != nil {
			t.Fatalf("authenticate forest corpus manifest: %v", err)
		}
		manifestFiles = files
		t.Logf("authenticated forest corpus manifest=%s gotreesitter_revision=%s lock_sha256=%s files=%d",
			manifestPath, manifest.GotreesitterRevision, manifest.CorpusLock.SHA256, len(manifest.Files))
	} else if strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_LEGACY_MANUAL")) == "" {
		t.Fatal("set GTS_FOREST_CORPUS_MANIFEST to an authenticated manifest; set GTS_FOREST_CORPUS_LEGACY_MANUAL=1 only for a non-authoritative manual flat-corpus run")
	}
	if resultOut != "" && (manifestPath == "" || len(langs) != 1) {
		t.Fatal("GTS_FOREST_AUDIT_RESULT_OUT requires one authenticated language")
	}
	var repoRoot string
	if manifestFiles == nil {
		repoRoot = forestRepoRoot(t)
		t.Log("legacy flat forest corpus mode is non-authoritative and cannot certify promotion")
	}

	anyRun := false
	for _, raw := range langs {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		load, ok := loaders[name]
		if !ok {
			t.Errorf("%s: unknown language (not in grammars.AllLanguages)", name)
			continue
		}
		var files []string
		if manifestFiles != nil {
			files = manifestFiles[name]
			if len(files) == 0 {
				t.Errorf("%s: requested language is absent from authenticated forest corpus manifest", name)
				continue
			}
		} else {
			dir := forestCorpusDir(repoRoot, name)
			if dir == "" {
				t.Logf("%s: no corpus_real/%s directory — skipping", name, name)
				continue
			}
			files = forestCorpusFiles(t, dir)
		}
		if len(files) == 0 {
			t.Logf("%s: forest corpus empty — skipping", name)
			continue
		}
		anyRun = true
		lang := load()
		result := runForestLangParity(t, name, lang, files, manifestPath, manifest.CorpusLock.SHA256,
			strings.TrimSpace(os.Getenv("GTS_FOREST_GOTREESITTER_REVISION")), forestManifestMetadata(manifest, name, files))
		if resultOut != "" {
			if err := WriteForestAuditResult(resultOut, result); err != nil {
				t.Fatalf("write forest audit result: %v", err)
			}
		}
	}
	if !anyRun {
		t.Skip("no forest corpus available for requested languages")
	}
}

type forestProductionAudit struct {
	t                *testing.T
	name             string
	lang             *gts.Language
	metadata         map[string]ForestCorpusManifestFile
	result           ForestAuditResult
	total            int
	dispatched       int
	fellBack         int
	diverged         int
	prodNanos        int64
	forestNanos      int64
	routedNanos      int64
	routedForest     int
	routedFallback   int
	auditFailed      bool
	divergedFiles    []string
	routedDiverged   int
	routedDiffFiles  []string
	fallbackReasons  map[string]int
	verboseFallbacks bool
}

func runForestLangParity(t *testing.T, name string, lang *gts.Language, files []string, manifestPath, lockSHA, revision string, metadata map[string]ForestCorpusManifestFile) ForestAuditResult {
	t.Helper()
	audit := newForestProductionAudit(t, name, lang, manifestPath, lockSHA, revision, metadata)
	gts.ResetPerfCounters()
	for _, filePath := range files {
		audit.evaluateFile(filePath)
	}
	return audit.finish()
}

func newForestProductionAudit(t *testing.T, name string, lang *gts.Language, manifestPath, lockSHA, revision string, metadata map[string]ForestCorpusManifestFile) *forestProductionAudit {
	result := ForestAuditResult{Schema: ForestAuditResultSchema, Mode: "production", Language: name, Status: "pass"}
	if manifestPath != "" {
		var err error
		result, err = NewForestAuditResult("production", revision, manifestPath, lockSHA, name)
		if err != nil {
			t.Fatalf("initialize forest audit result: %v", err)
		}
	}
	return &forestProductionAudit{
		t: t, name: name, lang: lang, metadata: metadata, result: result,
		fallbackReasons:  map[string]int{},
		verboseFallbacks: strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_VERBOSE")) != "",
	}
}

func (audit *forestProductionAudit) evaluateFile(filePath string) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		audit.t.Errorf("%s: read %s: %v", audit.name, filePath, err)
		return
	}
	audit.total++
	meta, ok := audit.metadata[filePath]
	if !ok {
		audit.fellBack++
		audit.t.Errorf("%s: authenticated metadata missing for %s", audit.name, filePath)
		return
	}
	notRun := forestAuditNotRunOutcome(src)
	fileResult := ForestAuditFileResult{
		Path: meta.Path, Bytes: meta.Bytes, SHA256: meta.SHA256,
		Forest: notRun, Peer: notRun, Routed: notRun, RoutedProvenance: forestAuditRouteNotRun,
	}
	defer func() { audit.result.Files = append(audit.result.Files, fileResult) }()

	prodTree := audit.parseProduction(src)
	fileResult.Peer = forestGoTreeOutcome(prodTree, src)
	if prodTree != nil {
		defer prodTree.Release()
	}
	var prodRoot *gts.Node
	if prodTree != nil {
		prodRoot = prodTree.RootNode()
	}

	routedTree, routedNanos := audit.parseRouted(src)
	fileResult.RoutedNanos = routedNanos
	fileResult.Routed = forestGoTreeOutcome(routedTree, src)
	fileResult.RoutedProvenance = forestAuditRouteProductionFallback
	if fileResult.Routed.ForestFastPath {
		fileResult.RoutedProvenance = forestAuditRouteForestFastPath
		audit.routedForest++
	} else {
		audit.routedFallback++
	}
	if routedTree != nil {
		defer routedTree.Release()
	}
	var routedRoot *gts.Node
	if routedTree != nil {
		routedRoot = routedTree.RootNode()
	}
	audit.compareRouted(filePath, routedRoot, prodRoot, &fileResult)

	if prodRoot == nil {
		fileResult.Disposition = "declined"
		fileResult.Decline = "production_no_tree"
		audit.fellBack++
		audit.auditFailed = true
		audit.t.Errorf("%s: production produced no tree for %s", audit.name, filepath.Base(filePath))
		return
	}

	forestTree, forestOK := audit.parseForest(src)
	if forestTree != nil {
		defer forestTree.Release()
	}
	var forestRoot *gts.Node
	if forestTree != nil {
		forestRoot = forestTree.RootNode()
	}
	fileResult.Forest = forestGoTreeOutcome(forestTree, src)
	if audit.forestDeclined(filePath, src, forestOK, forestRoot, prodRoot, fileResult.Forest, &fileResult) {
		return
	}
	audit.dispatched++
	fileResult.Disposition = "accepted"
	audit.compareProduction(filePath, forestRoot, prodRoot, &fileResult)
}

func (audit *forestProductionAudit) parseProduction(src []byte) *gts.Tree {
	gts.SetGLRForestEnabled(false)
	defer gts.SetGLRForestEnabled(true)
	start := time.Now()
	tree, _ := gts.NewParser(audit.lang).Parse(src)
	audit.prodNanos += time.Since(start).Nanoseconds()
	return tree
}

func (audit *forestProductionAudit) parseRouted(src []byte) (*gts.Tree, int64) {
	previous := audit.lang.WantsForest
	audit.lang.WantsForest = true
	defer func() { audit.lang.WantsForest = previous }()
	gts.SetGLRForestEnabled(true)
	start := time.Now()
	tree, _ := gts.NewParser(audit.lang).Parse(src)
	elapsed := time.Since(start).Nanoseconds()
	audit.routedNanos += elapsed
	return tree, elapsed
}

func (audit *forestProductionAudit) parseForest(src []byte) (*gts.Tree, bool) {
	start := time.Now()
	tree, ok := gts.NewParser(audit.lang).ParseForestExperimental(src)
	audit.forestNanos += time.Since(start).Nanoseconds()
	return tree, ok
}

func (audit *forestProductionAudit) forestDeclined(filePath string, src []byte, ok bool, root, productionRoot *gts.Node, outcome ForestAuditOutcome, fileResult *ForestAuditFileResult) bool {
	if ok && root != nil && outcome.Accepted && outcome.FullSpan && !root.HasError() {
		return false
	}
	audit.fellBack++
	reason := forestFallbackReason(ok, root, src)
	if ok && root != nil && (!outcome.Accepted || !outcome.FullSpan) {
		reason = "invalid_outcome"
	}
	audit.fallbackReasons[reason]++
	fileResult.Disposition = "declined"
	fileResult.Decline = reason
	if audit.verboseFallbacks {
		audit.logFallback(filePath, src, ok, root, productionRoot, reason)
	}
	return true
}

func (audit *forestProductionAudit) logFallback(filePath string, src []byte, ok bool, root, productionRoot *gts.Node, reason string) {
	endByte := uint32(0)
	hasError := false
	if root != nil {
		endByte = root.EndByte()
		hasError = root.HasError()
	}
	audit.t.Logf("%s: forest fallback %s reason=%s ok=%v root=%v hasError=%v end=%d lastNonWS=%d size=%d",
		audit.name, filepath.Base(filePath), reason, ok, root != nil, hasError, endByte, lastNonWSByte(src), len(src))
	audit.t.Logf("%s: production fallback peer %s hasError=%v end=%d",
		audit.name, filepath.Base(filePath), productionRoot.HasError(), productionRoot.EndByte())
}

func (audit *forestProductionAudit) compareProduction(filePath string, forestRoot, productionRoot *gts.Node, fileResult *ForestAuditFileResult) {
	if !fileResult.Peer.Accepted || !fileResult.Peer.FullSpan {
		fileResult.Diff = "production outcome not accepted/full-span"
	} else if diff := completeGoTreeIdentityDiff(forestRoot, productionRoot, audit.lang); diff != "" {
		fileResult.Diff = diff
	} else {
		return
	}
	audit.diverged++
	fileResult.Disposition = "diverged"
	audit.divergedFiles = append(audit.divergedFiles, filepath.Base(filePath)+": "+fileResult.Diff)
}

func (audit *forestProductionAudit) compareRouted(filePath string, routedRoot, productionRoot *gts.Node, fileResult *ForestAuditFileResult) {
	if productionRoot == nil {
		fileResult.RoutedDiff = "production tree missing"
	} else if routedRoot == nil {
		fileResult.RoutedDiff = "routed tree missing"
	} else if diff := completeGoTreeIdentityDiff(routedRoot, productionRoot, audit.lang); diff != "" {
		fileResult.RoutedDiff = diff
	} else {
		return
	}
	audit.routedDiverged++
	audit.routedDiffFiles = append(audit.routedDiffFiles, filepath.Base(filePath)+": "+fileResult.RoutedDiff)
}

func (audit *forestProductionAudit) finish() ForestAuditResult {
	speedup := 0.0
	if audit.forestNanos > 0 {
		speedup = float64(audit.prodNanos) / float64(audit.forestNanos)
	}
	routedSpeedup := 0.0
	if audit.routedNanos > 0 {
		routedSpeedup = float64(audit.prodNanos) / float64(audit.routedNanos)
	}
	audit.t.Logf("%-8s files=%d dispatched=%d fellback=%d diverged=%d routed_forest=%d routed_fallback=%d routed_diverged=%d | prod=%.1fms forest=%.1fms raw_speedup=%.1fx routed=%.1fms routed_speedup=%.2fx",
		audit.name, audit.total, audit.dispatched, audit.fellBack, audit.diverged, audit.routedForest, audit.routedFallback, audit.routedDiverged,
		float64(audit.prodNanos)/1e6, float64(audit.forestNanos)/1e6, speedup,
		float64(audit.routedNanos)/1e6, routedSpeedup)
	if audit.fellBack > 0 {
		audit.t.Logf("%-8s fallback reasons: %s", audit.name, formatFallbackReasons(audit.fallbackReasons))
	}
	if perf := gts.PerfCountersSnapshot(); perf.ForestReduceCalls > 0 || perf.ForestCoalesceCalls > 0 {
		audit.logPerfCounters(perf)
	}
	if audit.diverged > 0 {
		sort.Strings(audit.divergedFiles)
		audit.t.Errorf("%s: %d/%d dispatched files DIVERGED from production (blocks forest default-on): %s",
			audit.name, audit.diverged, audit.dispatched, strings.Join(audit.divergedFiles, ", "))
	}
	if audit.routedDiverged > 0 {
		sort.Strings(audit.routedDiffFiles)
		audit.t.Errorf("%s: %d/%d routed files DIVERGED from production (blocks forest promotion): %s",
			audit.name, audit.routedDiverged, audit.total, strings.Join(audit.routedDiffFiles, ", "))
	}
	audit.result.FilesTotal = audit.total
	audit.result.FilesAccepted = audit.dispatched
	audit.result.FilesDeclined = audit.fellBack
	audit.result.FilesDiverged = audit.diverged
	audit.result.FilesRoutedDiverged = audit.routedDiverged
	audit.result.FilesRoutedForest = audit.routedForest
	audit.result.FilesRoutedFallback = audit.routedFallback
	audit.result.ForestNanos = audit.forestNanos
	audit.result.PeerNanos = audit.prodNanos
	audit.result.RoutedNanos = audit.routedNanos
	audit.result.RoutedImproved = audit.prodNanos > 0 && audit.routedNanos < audit.prodNanos
	if audit.auditFailed || audit.diverged > 0 || audit.routedDiverged > 0 || audit.dispatched == 0 {
		audit.result.Status = "fail"
	}
	sort.Slice(audit.result.Files, func(i, j int) bool { return audit.result.Files[i].Path < audit.result.Files[j].Path })
	return audit.result
}

func (audit *forestProductionAudit) logPerfCounters(perf gts.PerfCounters) {
	audit.t.Logf("%-8s forest perf: reduce calls=%d zero=%d linear=%d dfs=%d dfs_links=%d dfs_multilink=%d dfs_extra=%d dfs_visits=%d dfs_path_entries=%d max_path=%d max_child=%d goto_hit=%d goto_miss=%d coalesce calls=%d new=%d append=%d dedup=%d cap_drop=%d cap_replace=%d precap_drop=%d",
		audit.name,
		perf.ForestReduceCalls,
		perf.ForestReduceZero,
		perf.ForestReduceLinearNoExtras,
		perf.ForestReduceDFS,
		perf.ForestReduceDFSLinks,
		perf.ForestReduceDFSMultiLinkSteps,
		perf.ForestReduceDFSExtraLinks,
		perf.ForestReduceDFSVisits,
		perf.ForestReduceDFSPathEntries,
		perf.ForestReduceMaxPathLen,
		perf.ForestReduceMaxChildCount,
		perf.ForestReduceGotoHits,
		perf.ForestReduceGotoMisses,
		perf.ForestCoalesceCalls,
		perf.ForestCoalesceNewNodes,
		perf.ForestCoalesceLinkAppends,
		perf.ForestCoalesceDedupHits,
		perf.ForestCoalesceCapDrops,
		perf.ForestCoalesceCapReplacements,
		perf.ForestCoalescePreCapDrops)
}

func forestFallbackReason(ok bool, root *gts.Node, src []byte) string {
	switch {
	case !ok:
		return "parse_failed"
	case root == nil:
		return "nil_root"
	case root.HasError():
		return "has_error"
	case root.EndByte() < lastNonWSByte(src):
		return "truncated"
	default:
		return "unknown"
	}
}

func formatFallbackReasons(reasons map[string]int) string {
	if len(reasons) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, reasons[k]))
	}
	return strings.Join(parts, ",")
}

func forestLanguageLoaders() map[string]func() *gts.Language {
	out := map[string]func() *gts.Language{}
	for _, e := range grm.AllLanguages() {
		out[e.Name] = e.Language
	}
	return out
}

func forestRepoRoot(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("GTS_REPO_ROOT")); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// Tests run from cgo_harness/; corpus_real lives there or at repo root.
	for _, cand := range []string{wd, filepath.Dir(wd)} {
		if _, err := os.Stat(filepath.Join(cand, "cgo_harness", "corpus_real")); err == nil {
			return cand
		}
		if _, err := os.Stat(filepath.Join(cand, "corpus_real")); err == nil {
			return cand
		}
	}
	return wd
}

func forestCorpusDir(repoRoot, lang string) string {
	for _, cand := range []string{
		filepath.Join(repoRoot, "cgo_harness", "corpus_real", lang),
		filepath.Join(repoRoot, "corpus_real", lang),
	} {
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand
		}
	}
	return ""
}

func forestCorpusFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files
}

func forestManifestMetadata(manifest ForestCorpusManifest, language string, files []string) map[string]ForestCorpusManifestFile {
	metadata := make(map[string]ForestCorpusManifestFile, len(files))
	root := strings.TrimSpace(os.Getenv("GTS_FOREST_CORPUS_ROOT"))
	for _, file := range manifest.Files {
		if file.Language == language {
			metadata[filepath.Join(root, language, filepath.FromSlash(file.Path))] = file
		}
	}
	if len(manifest.Files) != 0 {
		return metadata
	}
	for _, filePath := range files {
		info, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		digest, _ := forestFileSHA256(filePath)
		metadata[filePath] = ForestCorpusManifestFile{
			Language: language, Path: filepath.Base(filePath), Bytes: info.Size(), SHA256: digest,
		}
	}
	return metadata
}

func forestGoTreeOutcome(tree *gts.Tree, source []byte) ForestAuditOutcome {
	outcome := ForestAuditOutcome{SourceLen: uint32(len(source)), ExpectedEOF: uint32(len(source))}
	if tree == nil || tree.RootNode() == nil {
		outcome.StopReason = string(gts.ParseStopNone)
		return outcome
	}
	root := tree.RootNode()
	runtime := tree.ParseRuntime()
	outcome.Present = true
	outcome.StopReason = string(runtime.StopReason)
	outcome.SourceLen = runtime.SourceLen
	outcome.ExpectedEOF = runtime.ExpectedEOFByte
	outcome.RootEndByte = root.EndByte()
	outcome.RootHasError = root.HasError()
	outcome.Truncated = runtime.Truncated
	outcome.StoppedEarly = tree.ParseStoppedEarly()
	outcome.LastTokenEOF = runtime.LastTokenWasEOF
	outcome.ForestFastPath = runtime.ForestFastPath
	outcome.FullSpan = runtime.SourceLen == uint32(len(source)) &&
		runtime.ExpectedEOFByte == uint32(len(source)) && runtime.RootEndByte == root.EndByte() &&
		root.EndByte() >= lastNonWSByte(source) && !runtime.Truncated
	outcome.Accepted = runtime.StopReason == gts.ParseStopAccepted && !outcome.StoppedEarly && outcome.FullSpan && runtime.LastTokenWasEOF && !outcome.RootHasError
	return outcome
}

func forestAuditNotRunOutcome(source []byte) ForestAuditOutcome {
	sourceLen := uint32(len(source))
	return ForestAuditOutcome{StopReason: "not_run", SourceLen: sourceLen, ExpectedEOF: sourceLen}
}

// completeGoTreeIdentityDiff compares every node and every parent-child field
// edge directly. It returns only the first bounded diagnostic instead of
// materializing whole-tree strings on macro files.
func completeGoTreeIdentityDiff(got, want *gts.Node, lang *gts.Language) string {
	path := make([]int, 0, 32)
	var walk func(*gts.Node, *gts.Node) string
	walk = func(got, want *gts.Node) string {
		if got == nil || want == nil {
			if got == want {
				return ""
			}
			return fmt.Sprintf("%s presence got=%t want=%t", forestIdentityPath(path), got != nil, want != nil)
		}
		if gotType, wantType := got.Type(lang), want.Type(lang); gotType != wantType {
			return fmt.Sprintf("%s type got=%q want=%q", forestIdentityPath(path), gotType, wantType)
		}
		if got.StartByte() != want.StartByte() || got.EndByte() != want.EndByte() {
			return fmt.Sprintf("%s range got=%d:%d want=%d:%d", forestIdentityPath(path), got.StartByte(), got.EndByte(), want.StartByte(), want.EndByte())
		}
		if got.StartPoint() != want.StartPoint() || got.EndPoint() != want.EndPoint() {
			return fmt.Sprintf("%s points got=%v:%v want=%v:%v", forestIdentityPath(path), got.StartPoint(), got.EndPoint(), want.StartPoint(), want.EndPoint())
		}
		if got.IsNamed() != want.IsNamed() {
			return fmt.Sprintf("%s named got=%t want=%t", forestIdentityPath(path), got.IsNamed(), want.IsNamed())
		}
		if got.IsExtra() != want.IsExtra() {
			return fmt.Sprintf("%s extra got=%t want=%t", forestIdentityPath(path), got.IsExtra(), want.IsExtra())
		}
		if got.IsMissing() != want.IsMissing() {
			return fmt.Sprintf("%s missing got=%t want=%t", forestIdentityPath(path), got.IsMissing(), want.IsMissing())
		}
		gotChildren, wantChildren := got.ChildCount(), want.ChildCount()
		if gotChildren != wantChildren {
			return fmt.Sprintf("%s child_count got=%d want=%d", forestIdentityPath(path), gotChildren, wantChildren)
		}
		for i := 0; i < gotChildren; i++ {
			gotField, wantField := got.FieldNameForChild(i, lang), want.FieldNameForChild(i, lang)
			if gotField != wantField {
				path = append(path, i)
				diff := fmt.Sprintf("%s field got=%q want=%q", forestIdentityPath(path), gotField, wantField)
				path = path[:len(path)-1]
				return diff
			}
			path = append(path, i)
			if diff := walk(got.Child(i), want.Child(i)); diff != "" {
				path = path[:len(path)-1]
				return diff
			}
			path = path[:len(path)-1]
		}
		return ""
	}
	return walk(got, want)
}

func forestIdentityPath(path []int) string {
	if len(path) == 0 {
		return "root"
	}
	var b strings.Builder
	b.WriteString("root")
	const maxShown = 12
	shown := path
	if len(path) > maxShown {
		shown = path[:8]
	}
	for _, child := range shown {
		fmt.Fprintf(&b, "/%d", child)
	}
	if len(path) > maxShown {
		b.WriteString("/.../")
		for i, child := range path[len(path)-4:] {
			if i > 0 {
				b.WriteByte('/')
			}
			fmt.Fprint(&b, child)
		}
	}
	return b.String()
}

func TestCompleteGoTreeIdentityIncludesAnonymousChildrenAndFields(t *testing.T) {
	lang := grm.JsonLanguage()
	left, err := gts.NewParser(lang).Parse([]byte(`{"key":1}`))
	if err != nil || left == nil || left.RootNode() == nil {
		t.Fatalf("parse left JSON fixture: tree_nil=%t err=%v", left == nil, err)
	}
	defer left.Release()
	right, err := gts.NewParser(lang).Parse([]byte(`{"key":1}`))
	if err != nil || right == nil || right.RootNode() == nil {
		t.Fatalf("parse right JSON fixture: tree_nil=%t err=%v", right == nil, err)
	}
	defer right.Release()
	if diff := completeGoTreeIdentityDiff(left.RootNode(), right.RootNode(), lang); diff != "" {
		t.Fatalf("identical trees differ: %s", diff)
	}

	missingClose, err := gts.NewParser(lang).Parse([]byte(`{"key":1`))
	if err != nil || missingClose == nil || missingClose.RootNode() == nil {
		t.Fatalf("parse incomplete JSON fixture: tree_nil=%t err=%v", missingClose == nil, err)
	}
	defer missingClose.Release()
	if diff := completeGoTreeIdentityDiff(left.RootNode(), missingClose.RootNode(), lang); diff == "" {
		t.Fatal("complete identity ignored anonymous/missing closing syntax")
	}
}

func TestForestProductionAuditRoutedParseUsesAndRestoresLanguageOptIn(t *testing.T) {
	gts.SetGLRForestEnabled(true)
	defer gts.SetGLRForestEnabled(true)
	lang := grm.JsonLanguage()
	previous := lang.WantsForest
	lang.WantsForest = false
	defer func() { lang.WantsForest = previous }()

	for _, tc := range []struct {
		name       string
		source     string
		wantForest bool
	}{
		{name: "accepted", source: `{"key":1}`, wantForest: true},
		{name: "declined", source: `{"key":1`, wantForest: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			audit := &forestProductionAudit{t: t, name: "json", lang: lang}
			production := audit.parseProduction([]byte(tc.source))
			if production == nil || production.RootNode() == nil {
				t.Fatal("production parse returned no tree")
			}
			defer production.Release()
			routed, _ := audit.parseRouted([]byte(tc.source))
			if routed == nil || routed.RootNode() == nil {
				t.Fatal("routed parse returned no tree")
			}
			defer routed.Release()
			if lang.WantsForest {
				t.Fatal("routed parse did not restore Language.WantsForest")
			}
			if got := routed.ParseRuntime().ForestFastPath; got != tc.wantForest {
				t.Fatalf("routed ForestFastPath = %t, want %t", got, tc.wantForest)
			}
			file := ForestAuditFileResult{
				Peer:   forestGoTreeOutcome(production, []byte(tc.source)),
				Routed: forestGoTreeOutcome(routed, []byte(tc.source)),
			}
			audit.compareRouted(tc.name, routed.RootNode(), production.RootNode(), &file)
			if file.RoutedDiff != "" {
				t.Fatalf("routed parse differs from production: %s", file.RoutedDiff)
			}
		})
	}
}

func lastNonWSByte(src []byte) uint32 {
	end := len(src)
	for end > 0 {
		switch src[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
			continue
		}
		break
	}
	return uint32(end)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
