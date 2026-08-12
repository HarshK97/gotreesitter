package gotreesitter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// hyprlangTranscriptFixture builds the same known-firing witness as
// TestNormalizeHyprlangBooleanAssignmentValues (parser_result_hyprlang_test.go):
// a hand-built hyprlang tree with one boolean-valued assignment
// ("resize_on_border = true") and one non-boolean assignment
// ("name = myBezier"). dispatch.hyprlang (runLanguageResultCompatibility's
// "hyprlang" case) retags the first assignment's string value node to a
// boolean node with one anonymous child, and leaves the second alone. It is
// named as the recommended first SA-T cohort proof in the normalization
// backlog plan, so it doubles as this file's dispatcher-arm witness.
func hyprlangTranscriptFixture() (lang *Language, source []byte, root, boolValue *Node) {
	lang = &Language{
		Name: "hyprlang",
		SymbolNames: []string{
			"EOF", "configuration", "assignment", "name", "=", "string", "boolean",
			"true", "false", "on", "off", "yes", "no",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "configuration", Visible: true, Named: true},
			{Name: "assignment", Visible: true, Named: true},
			{Name: "name", Visible: true, Named: true},
			{Name: "=", Visible: true, Named: false},
			{Name: "string", Visible: true, Named: true},
			{Name: "boolean", Visible: true, Named: true},
			{Name: "true", Visible: true, Named: false},
			{Name: "false", Visible: true, Named: false},
			{Name: "on", Visible: true, Named: false},
			{Name: "off", Visible: true, Named: false},
			{Name: "yes", Visible: true, Named: false},
			{Name: "no", Visible: true, Named: false},
		},
	}
	source = []byte("resize_on_border = true\nname = myBezier\n")
	arena := newNodeArena(arenaClassFull)
	boolName := newLeafNodeInArena(arena, 3, true, 0, 16, Point{}, Point{Column: 16})
	boolEquals := newLeafNodeInArena(arena, 4, false, 17, 18, Point{Column: 17}, Point{Column: 18})
	boolValue = newLeafNodeInArena(arena, 5, true, 18, 23, Point{Column: 18}, Point{Column: 23})
	boolAssignment := newParentNodeInArena(arena, 2, true, []*Node{boolName, boolEquals, boolValue}, nil, 0)
	stringName := newLeafNodeInArena(arena, 3, true, 24, 28, Point{Row: 1}, Point{Row: 1, Column: 4})
	stringEquals := newLeafNodeInArena(arena, 4, false, 29, 30, Point{Row: 1, Column: 5}, Point{Row: 1, Column: 6})
	stringValue := newLeafNodeInArena(arena, 5, true, 30, 39, Point{Row: 1, Column: 6}, Point{Row: 1, Column: 15})
	stringAssignment := newParentNodeInArena(arena, 2, true, []*Node{stringName, stringEquals, stringValue}, nil, 0)
	root = newParentNodeInArena(arena, 1, true, []*Node{boolAssignment, stringAssignment}, nil, 0)
	return lang, source, root, boolValue
}

// jsonLines splits raw JSON-lines content into its non-empty lines.
func jsonLines(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// TestDispatcherArmTranscriptRecordsKnownFiringWitness runs dispatch.hyprlang
// (a known-firing arm: it always retags a boolean-valued assignment) through
// the real dispatcher switch with GTS_DISPATCHER_TRANSCRIPT=1 and asserts a
// transcript record appears in the JSON-lines output with the shape the
// cohort-proof instrument promises: language, arm, a node path into the
// changed subtree, and a compact node-type/child-count effect.
func TestDispatcherArmTranscriptRecordsKnownFiringWitness(t *testing.T) {
	resetDispatcherTranscriptSummary()
	t.Setenv("GTS_DISPATCHER_CENSUS", "")
	t.Setenv("GTS_DISPATCHER_TRANSCRIPT", "1")
	outPath := filepath.Join(t.TempDir(), "transcript.jsonl")
	t.Setenv("GTS_DISPATCHER_TRANSCRIPT_OUT", outPath)
	resetDispatcherTranscriptEnvCacheForTest()
	t.Cleanup(resetDispatcherTranscriptEnvCacheForTest)

	lang, source, root, boolValue := hyprlangTranscriptFixture()
	ctx := resultCompatibilityContext{root: root, source: source, lang: lang}

	if result := runLanguageResultCompatibility(ctx); result.stopReason != ParseStopNone {
		t.Fatalf("dispatcher returned stop reason %v, want none", result.stopReason)
	}

	// The witness fired exactly as it does with no instrumentation at all
	// (TestNormalizeHyprlangBooleanAssignmentValues): the transcript never
	// alters normalization, it only observes it.
	if got, want := boolValue.Type(lang), "boolean"; got != want {
		t.Fatalf("bool value type = %q, want %q", got, want)
	}
	if got, want := boolValue.ChildCount(), 1; got != want {
		t.Fatalf("bool value child count = %d, want %d", got, want)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read transcript output: %v", err)
	}
	lines := jsonLines(t, raw)
	if len(lines) != 1 {
		t.Fatalf("transcript recorded %d lines for dispatch.hyprlang's single case body, want 1: %s", len(lines), raw)
	}

	var rec dispatcherTranscriptRecord
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("unmarshal transcript line %s: %v", lines[0], err)
	}

	if rec.Language != "hyprlang" {
		t.Errorf("record language = %q, want %q", rec.Language, "hyprlang")
	}
	if rec.Arm != "dispatch.hyprlang" {
		t.Errorf("record arm = %q, want %q", rec.Arm, "dispatch.hyprlang")
	}
	if want := []int{0, 2}; !slices.Equal(rec.NodePath, want) {
		t.Errorf("record node_path = %v, want %v (root -> first assignment -> its value child)", rec.NodePath, want)
	}
	if rec.Effect.NodeType == nil {
		t.Fatal("record effect has no node_type change, want string -> boolean")
	}
	if rec.Effect.NodeType.Before != "string" || rec.Effect.NodeType.After != "boolean" {
		t.Errorf("record node_type = %+v, want {Before:string After:boolean}", rec.Effect.NodeType)
	}
	if rec.Effect.ChildCountDelta != 1 {
		t.Errorf("record child_count_delta = %d, want 1 (the synthesized boolean child)", rec.Effect.ChildCountDelta)
	}
	if rec.Effect.Span != nil {
		t.Errorf("record span = %+v, want nil: the retagged node's byte range does not move", rec.Effect.Span)
	}
	if rec.Effect.Error != nil || rec.Effect.Missing != nil {
		t.Errorf("record error=%+v missing=%+v, want both nil: this witness carries no recovery flags", rec.Effect.Error, rec.Effect.Missing)
	}
	if rec.Effect.Inserted || rec.Effect.Removed {
		t.Errorf("record inserted=%v removed=%v, want both false: the changed node still exists on both sides", rec.Effect.Inserted, rec.Effect.Removed)
	}

	summary := dispatcherTranscriptSummary()
	if summary["dispatch.hyprlang"] != 1 {
		t.Errorf("summary count map[dispatch.hyprlang] = %d, want 1: %v", summary["dispatch.hyprlang"], summary)
	}
}

// TestDispatcherArmTranscriptDisabledRecordsNothing runs the identical
// witness with GTS_DISPATCHER_TRANSCRIPT unset and asserts: no output file
// is created, the summary count map stays empty, and the normalizer's
// result is byte-for-byte the same as the enabled run above — the
// instrument is diagnostic-only and never changes behavior.
func TestDispatcherArmTranscriptDisabledRecordsNothing(t *testing.T) {
	resetDispatcherTranscriptSummary()
	t.Setenv("GTS_DISPATCHER_CENSUS", "")
	t.Setenv("GTS_DISPATCHER_TRANSCRIPT", "")
	outPath := filepath.Join(t.TempDir(), "transcript.jsonl")
	t.Setenv("GTS_DISPATCHER_TRANSCRIPT_OUT", outPath)
	resetDispatcherTranscriptEnvCacheForTest()
	t.Cleanup(resetDispatcherTranscriptEnvCacheForTest)

	lang, source, root, boolValue := hyprlangTranscriptFixture()
	ctx := resultCompatibilityContext{root: root, source: source, lang: lang}

	if result := runLanguageResultCompatibility(ctx); result.stopReason != ParseStopNone {
		t.Fatalf("dispatcher returned stop reason %v, want none", result.stopReason)
	}

	if got, want := boolValue.Type(lang), "boolean"; got != want {
		t.Fatalf("bool value type = %q, want %q (same result as with the flag on)", got, want)
	}
	if got, want := boolValue.ChildCount(), 1; got != want {
		t.Fatalf("bool value child count = %d, want %d (same result as with the flag on)", got, want)
	}
	if child := boolValue.Child(0); child == nil || child.Type(lang) != "true" || child.IsNamed() {
		t.Fatalf("bool child = %#v, want anonymous true (same result as with the flag on)", child)
	}

	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("transcript output file exists with the flag off (stat err = %v); the instrument must write nothing when disabled", err)
	}
	if summary := dispatcherTranscriptSummary(); len(summary) != 0 {
		t.Fatalf("summary count map is non-empty with the flag off: %v", summary)
	}
}

// TestDispatcherTranscriptRequiresExactOne locks in GTS_DISPATCHER_TRANSCRIPT's
// gating contract: only the exact string "1" turns the instrument on,
// matching GTS_DISPATCHER_CENSUS's existing contract
// (TestDispatcherCensusRequiresExactOne).
func TestDispatcherTranscriptRequiresExactOne(t *testing.T) {
	t.Cleanup(resetDispatcherTranscriptEnvCacheForTest)
	for _, value := range []string{"", "0", "false", "2"} {
		t.Setenv("GTS_DISPATCHER_TRANSCRIPT", value)
		// dispatcherTranscriptEnabled caches its read (sync.Once); reset
		// before each value so this loop still exercises a fresh read every
		// iteration instead of pinning the first iteration's result.
		resetDispatcherTranscriptEnvCacheForTest()
		if dispatcherTranscriptEnabled() {
			t.Fatalf("GTS_DISPATCHER_TRANSCRIPT=%q enabled the transcript", value)
		}
	}
	t.Setenv("GTS_DISPATCHER_TRANSCRIPT", "1")
	resetDispatcherTranscriptEnvCacheForTest()
	if !dispatcherTranscriptEnabled() {
		t.Fatal("GTS_DISPATCHER_TRANSCRIPT=1 did not enable the transcript")
	}
}

// TestCaptureDispatcherTranscriptSnapshotPaths pins the path convention
// (root-relative child-index chain, root itself is []) against a small hand
// built tree, independent of any real dispatcher arm.
func TestCaptureDispatcherTranscriptSnapshotPaths(t *testing.T) {
	arena := newNodeArena(arenaClassFull)
	leaf1 := newLeafNodeInArena(arena, 10, true, 0, 3, Point{Column: 0}, Point{Column: 3})
	leaf2 := newLeafNodeInArena(arena, 11, true, 3, 6, Point{Column: 3}, Point{Column: 6})
	root := newParentNodeInArena(arena, 1, true, []*Node{leaf1, leaf2}, nil, 0)

	snap := captureDispatcherTranscriptSnapshot(root)
	if len(snap.signatures) != 3 || len(snap.paths) != 3 {
		t.Fatalf("snapshot visited %d signatures / %d paths, want 3/3 (root + 2 leaves)", len(snap.signatures), len(snap.paths))
	}
	want := [][]int{{}, {0}, {1}}
	for i, wantPath := range want {
		if !slices.Equal(snap.paths[i], wantPath) {
			t.Errorf("paths[%d] = %v, want %v", i, snap.paths[i], wantPath)
		}
	}
}

// TestFirstDivergentTranscriptPath exercises the shapes a divergence can
// take: a same-length interior mismatch, a real child insertion (which, for
// this signature shape, always surfaces as an interior mismatch on the
// inserted node's parent), the trailing-only-length fallback branch in
// isolation, and no divergence at all.
func TestFirstDivergentTranscriptPath(t *testing.T) {
	build := func() (root, leaf1, leaf2 *Node) {
		arena := newNodeArena(arenaClassFull)
		leaf1 = newLeafNodeInArena(arena, 10, true, 0, 3, Point{Column: 0}, Point{Column: 3})
		leaf2 = newLeafNodeInArena(arena, 11, true, 3, 6, Point{Column: 3}, Point{Column: 6})
		root = newParentNodeInArena(arena, 1, true, []*Node{leaf1, leaf2}, nil, 0)
		return root, leaf1, leaf2
	}

	t.Run("interior mismatch", func(t *testing.T) {
		root, _, leaf2 := build()
		before := captureDispatcherTranscriptSnapshot(root)
		leaf2.symbol = 99
		after := captureDispatcherTranscriptSnapshot(root)

		path, b, a, found := firstDivergentTranscriptPath(before, after)
		if !found {
			t.Fatal("expected a divergence")
		}
		if !slices.Equal(path, []int{1}) {
			t.Errorf("path = %v, want [1]", path)
		}
		if b == nil || a == nil {
			t.Fatalf("expected both sides present, got before=%v after=%v", b, a)
		}
		if b.symbol != 11 || a.symbol != 99 {
			t.Errorf("signatures = before %d after %d, want 11 -> 99", b.symbol, a.symbol)
		}
	})

	t.Run("child inserted under root", func(t *testing.T) {
		// Appending a child always changes its parent's own childCount, and
		// the parent (here, root) appears earlier in preorder than the new
		// child, so the interior-mismatch scan (above) finds root itself,
		// not a trailing-only divergence. This is the shape every real
		// dispatcher-arm insertion takes: an ancestor's signature always
		// changes at or before the inserted node's position.
		root, _, _ := build()
		before := captureDispatcherTranscriptSnapshot(root)
		extra := newLeafNodeInArena(root.ownerArena, 12, true, 6, 9, Point{Column: 6}, Point{Column: 9})
		root.children = append(root.children, extra)
		after := captureDispatcherTranscriptSnapshot(root)

		path, b, a, found := firstDivergentTranscriptPath(before, after)
		if !found {
			t.Fatal("expected a divergence")
		}
		if !slices.Equal(path, []int{}) {
			t.Errorf("path = %v, want [] (root itself, whose child_count changed)", path)
		}
		if b == nil || a == nil {
			t.Fatalf("expected both sides present, got before=%v after=%v", b, a)
		}
		if b.childCount != 2 || a.childCount != 3 {
			t.Errorf("root child counts = before %d after %d, want 2 -> 3", b.childCount, a.childCount)
		}
	})

	t.Run("trailing-only length divergence", func(t *testing.T) {
		// firstDivergentTranscriptPath's trailing-only branch: every
		// position in the common prefix compares equal, and only the
		// longer snapshot's own length exposes the change. Built directly
		// (rather than from a tree mutation) because, for this signature
		// shape, an ordinary insertion is always caught earlier as an
		// interior mismatch on the inserted node's parent (see the
		// preceding subtest) — this subtest instead pins the fallback
		// branch's own contract.
		before := dispatcherTranscriptSnapshot{
			signatures: []dispatcherNodeSignature{{symbol: 1, childCount: 1}, {symbol: 10}},
			paths:      [][]int{{}, {0}},
		}
		after := dispatcherTranscriptSnapshot{
			signatures: []dispatcherNodeSignature{{symbol: 1, childCount: 1}, {symbol: 10}, {symbol: 12}},
			paths:      [][]int{{}, {0}, {0, 0}},
		}

		path, b, a, found := firstDivergentTranscriptPath(before, after)
		if !found {
			t.Fatal("expected a divergence")
		}
		if !slices.Equal(path, []int{0, 0}) {
			t.Errorf("path = %v, want [0 0]", path)
		}
		if b != nil {
			t.Errorf("before signature = %+v, want nil (pure insertion)", b)
		}
		if a == nil || a.symbol != 12 {
			t.Errorf("after signature = %v, want symbol 12", a)
		}

		// Symmetric removal: after is the shorter snapshot this time.
		path, b, a, found = firstDivergentTranscriptPath(after, before)
		if !found {
			t.Fatal("expected a divergence")
		}
		if !slices.Equal(path, []int{0, 0}) {
			t.Errorf("path = %v, want [0 0]", path)
		}
		if a != nil {
			t.Errorf("after signature = %+v, want nil (pure removal)", a)
		}
		if b == nil || b.symbol != 12 {
			t.Errorf("before signature = %v, want symbol 12", b)
		}
	})

	t.Run("no divergence", func(t *testing.T) {
		root, _, _ := build()
		before := captureDispatcherTranscriptSnapshot(root)
		after := captureDispatcherTranscriptSnapshot(root)
		if _, _, _, found := firstDivergentTranscriptPath(before, after); found {
			t.Fatal("expected no divergence for two snapshots of the same unmodified tree")
		}
	})
}
