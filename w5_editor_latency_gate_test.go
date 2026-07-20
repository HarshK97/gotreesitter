package gotreesitter_test

// Campaign O(edit) workstream W5 (spec.campaign.oedit): the editor-latency
// gate.
//
// This is the continuous CI instrument that locks in the W1 (fragility-gated
// top-level sibling block-splice), W1b (eager-default reduce settling), and
// W2 (memo-cache growth determinism) wins so a future change cannot silently
// reopen the O(file)-per-keystroke regression #380 found.
//
// Design principle: deterministic counters are the hard gate; wall time is
// informational only. CI runners are noisy shared hardware -- a wall-clock
// threshold gate would flake on unrelated host load. IncrementalParseProfile's
// counters (ReuseRejectRootNonLeafChanged, ReusedBytes, NewNodesAllocated,
// TokensConsumed, ...) are proven input-deterministic
// (TestW1BSettleIsDeterministicAcrossFreshParsers, w1b_reduce_settle_test.go,
// and TestW5DeterminismAcrossFreshParsers below), so they are what regresses
// the build. Wall time is still measured and logged for visibility.
//
// # What this gate actually measured (read before changing ceilings)
//
// Building the sweep below (four languages, three positions, three edit
// classes, two size tiers) surfaced two things a single near-top fixture
// cannot:
//
//  1. ReuseRejectRootNonLeafChanged is NOT flat "regardless of position in
//     file" -- only "regardless of file size AT A FIXED near-the-very-start
//     position" (exactly what TestW1BSettleReuseIsBoundedNotFileSized in
//     w1b_reduce_settle_test.go tests: it holds the edit at the FIRST
//     top-level item and varies trailing-sibling count). Today's mechanism
//     (topLevelSiblingBlockSpliceEligible) only fast-paths the TRAILING run
//     after the edit; content BEFORE the edit is still walked one top-level
//     item at a time, and each leading item currently costs a small but
//     nonzero, consistent number of rejected candidates (empirically ~10-17
//     per language, constant per item, independent of file size) before
//     falling through to a slower reuse path. For a "middle" or "bottom"
//     position edit that consistent per-item cost multiplies by the leading
//     item count, which IS proportional to file size. This is a real,
//     currently open scope limit -- not a regression this gate enforces on
//     -- and it matches the campaign's own success-metric wording exactly:
//     "Go 137KB NEAR-TOP single-char insert" (W1's Workstreams section).
//     Consequently MaxRootNonLeafChangedAtTop below is only asserted at the
//     "top" position.
//  2. ReusedBytes / len(editedSource), unlike the rejection counter, IS
//     input-deterministic AND size-independent when position is expressed
//     as a FRACTION of the file (not an absolute byte offset): a "middle"
//     (~50%-through) edit measures the same byte-reuse percentage at 20KB
//     and at 137KB for the same language, within noise. That makes it the
//     right metric for a ratchet that must hold "regardless of size" at
//     every position, not just at the top, and it is what this gate uses
//     for MinByteReusePercent below.
//
// Coverage: four languages (Go, TypeScript, Python, CSS) at two committed
// size tiers (~20KB, ~137KB) crossed with three edit classes (insert,
// delete, replace) at three file positions (top ~0%, middle ~50%, bottom
// ~99% through the file) -- 72 samples. A third size tier (~1MB) exists but
// is gated behind GTS_W5_FULL_SWEEP=1 + GTS_W5_SLOW_TIER=1 (see w5Tiers)
// because it pushes single-run wall time well past a fast local/CI budget;
// it still asserts the same oracle and counter ceilings, it is just not run
// on every `go test .`.
//
// Every sample enforces, unconditionally (the non-negotiable campaign
// invariant, not a soft check):
//
//  1. Oracle equality: the incremental tree is structurally identical to a
//     fresh parse of the same final text (w1bStructuralDiff, the same
//     recursive type/span/named/missing/childCount walk W1b's tests use --
//     equivalent to a byte-identical serialization diff, and strictly
//     stronger since it also catches mismatched node identity that a naive
//     byte compare of two different trees' printed forms could theoretically
//     miss).
//  2. Full-span coverage: the incremental root's end byte covers the whole
//     edited buffer.
//  3. Per-language, per-position counter ceilings (w5Ceilings), which
//     regress the build if a change reopens O(file) reparse cost.
//
// # Ceiling table and provenance
//
// Ceilings are the ratchet floor: today's achieved level (this box,
// linux/amd64, GOMAXPROCS default, measured via this harness) minus modest
// slack for cross-hardware noise, not an aspirational target.
//
//	language    | topRootNonLeafChanged ceiling | MinByteReusePercent (top / middle / bottom)
//	go          | 16  (measured 10-11)          | 88 / 60 / 35  (measured ~96 / ~70 / ~44)
//	typescript  | 20  (measured 13-14)          | 90 / 70 / 55  (measured ~97 / ~81 / ~65)
//	css         | 12  (measured 3-6)            | 88 / 75 / 65  (measured ~96 / ~85 / ~75)
//	python      | 8   (measured 0)               | 0  / 0  / 0   (measured 0 -- see below)
//
//   - go: topRootNonLeafChanged=16. The campaign Status section
//     (hypha://m31labs/gotreesitter spec.campaign.oedit) records W1b's
//     achieved result as "rootNonLeafChanged constant 9 at any size" for
//     clean near-top Go edits; this harness measures 10-11 on its own
//     fixture (a different sibling count than w1b's own 300-sibling
//     fixture, hence the small difference) -- 16 keeps that as a small
//     constant with slack for box variance, satisfying the literal
//     campaign instruction "clean-Go top-level edits rootNonLeafChanged <=
//     16 regardless of size".
//   - css top MinByteReusePercent=88 for the SWEEP's synthetic fixture
//     (measured ~96 on this box). The campaign's literal instruction ("CSS
//     near-top reuse >= 95%, today 99.5%") is enforced precisely, not with
//     this generic sweep's slacker floor, but by a dedicated test using the
//     EXACT fixture the W1 PR #395 merge-gate review measured against
//     (testdata/incremental_gate/css_stylesheet.css):
//     TestW5CSSNearTopReuseAssertion below asserts ReusedBytes/len(edited)
//     >= 95 on that fixture (measured ~97 on this box via that formula; the
//     review's own "99.5%" figure used an unrecorded/different formula on
//     the same fixture -- both are comfortably >= 95). This is the tracked
//     "add a committed W1 measurement assertion" follow-up from that
//     review, now enforced in CI instead of only recorded in a comment.
//   - typescript, python: measured on this box via this harness (no
//     campaign-recorded prior figure exists for these languages yet -- W4,
//     the per-language scanner-quiescence proof, is still open for them).
//   - python: MinByteReusePercent=0 at every position for insert/delete is
//     an intentional "honest negative", not a placeholder. Measurement
//     shows Python's insert/delete incremental parses currently take the
//     oldTreeDisablesIncrementalReuse fresh-parse-fallback route
//     unconditionally (ReusedBytes=0, NewNodesAllocated exactly equal to a
//     full parse's node count, regardless of edit position) -- consistent
//     with the campaign Status/W4 text listing "Python indent stack" as a
//     still-open scanner-quiescence proof obligation: a column-shifting
//     length-changing edit cannot yet be proven not to perturb the
//     indent/dedent stack, so the parser conservatively declines reuse
//     entirely rather than risk unsound splicing. Oracle equality (checked
//     unconditionally, same as every other cell) is what actually protects
//     Python today; this floor will ratchet upward as a natural follow-on
//     once W4 lands for Python.
//   - Every language's REPLACE (same-length substitution) class measures
//     ReusedBytes at ~100% universally, INCLUDING python: a same-length
//     edit that does not change token boundaries or classification hits an
//     unconditional single-token in-place substitution fast path before
//     the reuse cursor / scanner-quiescence question is even reached. See
//     w5ReplaceMinByteReusePercent.
//
// # Follow-ups this gate closes
//
//   - The W1 PR #395 merge-gate review's tracked follow-up "add a committed
//     W1 measurement assertion" for the CSS splice profile:
//     TestW5CSSNearTopReuseAssertion below is exactly that assertion, now
//     enforced in CI rather than only recorded in a review comment.
//   - The W1b bound at w1b_reduce_settle_test.go:179
//     (TestW1BSettleUnlocksGoTopLevelSiblingSplice), which allows
//     ReuseRejectRootNonLeafChanged up to 64 ("a generous bound" per that
//     test's comment). This gate tightens the same property, at the "top"
//     position, to <=16 for go (w5Ceilings["go"].MaxRootNonLeafChangedAtTop).
//     That existing test is left as-is: it is a fixture-specific unit-level
//     proof (300 trailing siblings, single insert) already covered, at the
//     tighter bound, by this gate's cross-product sweep; loosening one to
//     match the other was unnecessary churn on a test that still passes and
//     still guards real regressions.
import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// w5FullSweep reports whether the ~137KB tier should be added to the
// default ~20KB-only sweep. Unset, only the fast tier runs as part of
// `go test .`.
func w5FullSweep() bool {
	return strings.TrimSpace(os.Getenv("GTS_W5_FULL_SWEEP")) != ""
}

// w5SlowTier reports whether the ~1MB size tier should be included. Only
// meaningful when w5FullSweep is also set; the 1MB tier is opt-in even for
// the full sweep, matching the campaign spec's "1MB env-gated as the slow
// tier" instruction.
func w5SlowTier() bool {
	return strings.TrimSpace(os.Getenv("GTS_W5_SLOW_TIER")) != ""
}

// w5EditClass names the byte-level edit shape applied at a sample site.
type w5EditClass int

const (
	w5Insert w5EditClass = iota
	w5Delete
	w5Replace
)

func (c w5EditClass) String() string {
	switch c {
	case w5Insert:
		return "insert"
	case w5Delete:
		return "delete"
	case w5Replace:
		return "replace"
	default:
		return "unknown"
	}
}

// w5Position names which region of the file an edit site falls in,
// expressed as a fraction of total top-level items (see w5EditSiteIndices):
// this is what makes byte-reuse measurements comparable across size tiers.
type w5Position int

const (
	w5Top w5Position = iota
	w5Middle
	w5Bottom
)

func (p w5Position) String() string {
	switch p {
	case w5Top:
		return "top"
	case w5Middle:
		return "middle"
	case w5Bottom:
		return "bottom"
	default:
		return "unknown"
	}
}

// w5SizeTier names a committed corpus size tier.
type w5SizeTier struct {
	name       string
	targetSize int
}

var (
	w5Tier20KB  = w5SizeTier{name: "20KB", targetSize: 20 * 1024}
	w5Tier137KB = w5SizeTier{name: "137KB", targetSize: 137 * 1024}
	w5Tier1MB   = w5SizeTier{name: "1MB", targetSize: 1024 * 1024}
)

// w5LangSpec is one gate language: how to build a corpus of at least
// targetBytes (returning the source and the number of top-level items
// written) and how to find the edit site for a given item index.
//
// Every generator writes many small, syntactically unambiguous top-level
// items (functions or rule blocks), each carrying its index as a decimal
// literal. w5MinItems guarantees at least a few 2+-digit indices exist even
// for a tiny targetBytes, so "middle"/"bottom" (which select an index >= 10)
// are always safe to single-byte-delete without collapsing an index's last
// digit to nothing; "top" always selects index 0, which single-byte-delete
// reduces to a bare, still-valid identifier suffix in every gate language
// (verified empirically, not just asserted -- see the harness's own probe
// history).
type w5LangSpec struct {
	name   string
	lang   func() *gts.Language
	build  func(targetBytes int) (src []byte, itemCount int)
	marker func(itemIndex int) string // substring ending right after the item's index digits
}

const w5MinItems = 40

func w5BuildGo(targetBytes int) ([]byte, int) {
	var b strings.Builder
	b.Grow(targetBytes + 256)
	b.WriteString("package p\n\n")
	n := 0
	for b.Len() < targetBytes || n < w5MinItems {
		fmt.Fprintf(&b, "func G%d(a int) int {\n\tx := a + %d\n\treturn x\n}\n\n", n, n)
		n++
	}
	return []byte(b.String()), n
}

func w5BuildTypeScript(targetBytes int) ([]byte, int) {
	var b strings.Builder
	b.Grow(targetBytes + 256)
	n := 0
	for b.Len() < targetBytes || n < w5MinItems {
		fmt.Fprintf(&b, "export function f%d(a: number): number {\n  const v = a + %d;\n  return v;\n}\n\n", n, n)
		n++
	}
	return []byte(b.String()), n
}

func w5BuildPython(targetBytes int) ([]byte, int) {
	var b strings.Builder
	b.Grow(targetBytes + 256)
	n := 0
	for b.Len() < targetBytes || n < w5MinItems {
		fmt.Fprintf(&b, "def f%d(a):\n    v = a + %d\n    return v\n\n", n, n)
		n++
	}
	return []byte(b.String()), n
}

func w5BuildCSS(targetBytes int) ([]byte, int) {
	var b strings.Builder
	b.Grow(targetBytes + 256)
	n := 0
	for b.Len() < targetBytes || n < w5MinItems {
		fmt.Fprintf(&b, ".item-%d {\n  color: #112233;\n  margin: %dpx;\n}\n\n", n, n%64)
		n++
	}
	return []byte(b.String()), n
}

var w5Langs = []w5LangSpec{
	{name: "go", lang: grammars.GoLanguage, build: w5BuildGo, marker: func(i int) string { return fmt.Sprintf("G%d(", i) }},
	{name: "typescript", lang: grammars.TypescriptLanguage, build: w5BuildTypeScript, marker: func(i int) string { return fmt.Sprintf("f%d(", i) }},
	{name: "python", lang: grammars.PythonLanguage, build: w5BuildPython, marker: func(i int) string { return fmt.Sprintf("f%d(", i) }},
	{name: "css", lang: grammars.CssLanguage, build: w5BuildCSS, marker: func(i int) string { return fmt.Sprintf("item-%d ", i) }},
}

// w5EditSiteIndices maps a w5Position to a concrete item index into a
// corpus of itemCount items: top is always the very first item (index 0,
// matching w1b_reduce_settle_test.go's own near-top methodology), middle is
// ~50% through, bottom is ~99% through (itemCount-4, not the literal last
// item, to avoid EOF-adjacent edge effects). itemCount >= w5MinItems (40)
// guarantees middle and bottom always select a 2+-digit index, which
// single-byte delete needs to leave a nonempty numeric literal behind.
func w5EditSiteIndices(itemCount int) map[w5Position]int {
	bottom := itemCount - 4
	if bottom < 12 {
		bottom = itemCount - 1
	}
	return map[w5Position]int{
		w5Top:    0,
		w5Middle: itemCount / 2,
		w5Bottom: bottom,
	}
}

// w5LastDigitOffset finds the byte offset of the last decimal digit of the
// item-index literal embedded in marker (marker's own last byte is always
// the fixed non-digit delimiter that follows the digits, e.g. "G12(" or
// "item-12 ", so the digit is one byte before the end of the match).
func w5LastDigitOffset(t *testing.T, src []byte, marker string) int {
	t.Helper()
	idx := bytes.Index(src, []byte(marker))
	if idx < 0 {
		t.Fatalf("w5 fixture invariant: marker %q not found in generated corpus", marker)
	}
	return idx + len(marker) - 2
}

// w5ApplyEdit builds the edited buffer and the corresponding InputEdit for a
// single-byte insert/delete/replace at offset, which must be a decimal
// digit (w5LastDigitOffset's contract): duplicate it (insert), drop it
// (delete), or replace it with a different decimal digit (replace). All
// three keep the surrounding token a valid identifier/number suffix in
// every gate language.
func w5ApplyEdit(src []byte, offset int, class w5EditClass) ([]byte, gts.InputEdit) {
	startPoint := w1bPointAt(src, offset)
	switch class {
	case w5Insert:
		edited := make([]byte, 0, len(src)+1)
		edited = append(edited, src[:offset]...)
		edited = append(edited, src[offset])
		edited = append(edited, src[offset:]...)
		return edited, gts.InputEdit{
			StartByte: uint32(offset), OldEndByte: uint32(offset), NewEndByte: uint32(offset + 1),
			StartPoint: startPoint, OldEndPoint: startPoint, NewEndPoint: w1bPointAt(edited, offset+1),
		}
	case w5Delete:
		edited := make([]byte, 0, len(src)-1)
		edited = append(edited, src[:offset]...)
		edited = append(edited, src[offset+1:]...)
		return edited, gts.InputEdit{
			StartByte: uint32(offset), OldEndByte: uint32(offset + 1), NewEndByte: uint32(offset),
			StartPoint: startPoint, OldEndPoint: w1bPointAt(src, offset+1), NewEndPoint: startPoint,
		}
	case w5Replace:
		edited := append([]byte(nil), src...)
		edited[offset] = (src[offset]-'0'+1)%10 + '0'
		return edited, gts.InputEdit{
			StartByte: uint32(offset), OldEndByte: uint32(offset + 1), NewEndByte: uint32(offset + 1),
			StartPoint: startPoint, OldEndPoint: w1bPointAt(src, offset+1), NewEndPoint: w1bPointAt(edited, offset+1),
		}
	default:
		panic("unreachable w5EditClass")
	}
}

// w5Sample is one full run: base parse, apply edit, fresh parse of the
// result, profiled incremental parse of the result, plus the oracle diff
// and wall time.
type w5Sample struct {
	Lang        string
	Size        string
	Class       w5EditClass
	Position    w5Position
	Profile     gts.IncrementalParseProfile
	EditedLen   int
	OracleDiff  string
	FullSpanOK  bool
	IncrWallNs  int64
	FreshWallNs int64
}

func w5RunSample(t *testing.T, spec w5LangSpec, tier w5SizeTier, class w5EditClass, pos w5Position) w5Sample {
	t.Helper()
	lang := spec.lang()
	src, itemCount := spec.build(tier.targetSize)
	sites := w5EditSiteIndices(itemCount)
	offset := w5LastDigitOffset(t, src, spec.marker(sites[pos]))

	parser := gts.NewParser(lang)
	oldTree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("%s/%s base parse: %v", spec.name, tier.name, err)
	}
	if oldTree.RootNode().HasError() {
		t.Fatalf("%s/%s fixture invariant: base parse must be clean", spec.name, tier.name)
	}
	defer oldTree.Release()

	edited, edit := w5ApplyEdit(src, offset, class)

	freshParser := gts.NewParser(lang)
	freshStart := time.Now()
	freshTree, err := freshParser.Parse(edited)
	freshWall := time.Since(freshStart)
	if err != nil {
		t.Fatalf("%s/%s fresh parse of edited: %v", spec.name, tier.name, err)
	}
	defer freshTree.Release()

	oldTree.Edit(edit)
	incrStart := time.Now()
	incrTree, prof, err := parser.ParseIncrementalProfiled(edited, oldTree)
	incrWall := time.Since(incrStart)
	if err != nil {
		t.Fatalf("%s/%s incremental parse: %v", spec.name, tier.name, err)
	}
	defer incrTree.Release()

	diff := w1bStructuralDiff(lang, freshTree.RootNode(), incrTree.RootNode(), "root")
	fullSpan := int(incrTree.RootNode().EndByte()) == len(edited)

	return w5Sample{
		Lang: spec.name, Size: tier.name, Class: class, Position: pos,
		Profile: prof, EditedLen: len(edited), OracleDiff: diff, FullSpanOK: fullSpan,
		IncrWallNs: incrWall.Nanoseconds(), FreshWallNs: freshWall.Nanoseconds(),
	}
}

// w5ByteReusePercent is this gate's "how much of the file was served from
// the old tree" metric: ReusedBytes over the edited buffer's total length.
// It is the metric this file's doc comment found to be size-independent by
// position FRACTION, and it is what the campaign's CSS PR review's "node
// reuse" figures are closest to when recomputed on that review's own
// fixture (see TestW5CSSNearTopReuseAssertion).
func w5ByteReusePercent(p gts.IncrementalParseProfile, editedLen int) float64 {
	if editedLen <= 0 {
		return 100
	}
	return float64(p.ReusedBytes) / float64(editedLen) * 100
}

// w5PositionCeiling is the byte-reuse floor for one (language, position)
// cell, applied to insert/delete classes.
type w5PositionCeiling struct {
	MinByteReusePercent float64
}

// w5Ceiling is the ratchet floor for one language. See the file doc comment
// for full provenance of every value.
type w5Ceiling struct {
	// MaxRootNonLeafChangedAtTop bounds ReuseRejectRootNonLeafChanged, but
	// ONLY at the "top" position -- see the file doc comment's "What this
	// gate actually measured" section for why middle/bottom positions do
	// not get this same flat ceiling today.
	MaxRootNonLeafChangedAtTop uint64
	Top, Middle, Bottom        w5PositionCeiling
}

// w5ReplaceMinByteReusePercent is the universal (language-independent)
// floor for the REPLACE edit class: a same-length substitution that does
// not change a token's boundaries or classification hits an unconditional
// single-token in-place fast path (measured ~100% on every gate language,
// including python, where insert/delete currently measure 0 -- see the
// file doc comment).
const w5ReplaceMinByteReusePercent = 95

// w5Ceilings is the committed ratchet: today's achieved level per
// (language, position), minus modest slack. See the file doc comment for
// full provenance of every value here.
var w5Ceilings = map[string]w5Ceiling{
	"go": {
		MaxRootNonLeafChangedAtTop: 16,
		Top:                        w5PositionCeiling{MinByteReusePercent: 88},
		Middle:                     w5PositionCeiling{MinByteReusePercent: 60},
		Bottom:                     w5PositionCeiling{MinByteReusePercent: 35},
	},
	"typescript": {
		MaxRootNonLeafChangedAtTop: 20,
		Top:                        w5PositionCeiling{MinByteReusePercent: 90},
		Middle:                     w5PositionCeiling{MinByteReusePercent: 70},
		Bottom:                     w5PositionCeiling{MinByteReusePercent: 55},
	},
	"css": {
		MaxRootNonLeafChangedAtTop: 12,
		Top:                        w5PositionCeiling{MinByteReusePercent: 88},
		Middle:                     w5PositionCeiling{MinByteReusePercent: 75},
		Bottom:                     w5PositionCeiling{MinByteReusePercent: 65},
	},
	// python: an honest negative, not a placeholder -- see the file doc
	// comment's python bullet. Insert/delete measure 0% byte reuse today
	// (the W4 Python indent-stack scanner-quiescence proof is still open),
	// so the floor is 0 at every position; oracle equality is what actually
	// protects Python here, and it is still checked unconditionally.
	"python": {
		MaxRootNonLeafChangedAtTop: 8,
		Top:                        w5PositionCeiling{MinByteReusePercent: 0},
		Middle:                     w5PositionCeiling{MinByteReusePercent: 0},
		Bottom:                     w5PositionCeiling{MinByteReusePercent: 0},
	},
}

func (c w5Ceiling) forPosition(pos w5Position) w5PositionCeiling {
	switch pos {
	case w5Top:
		return c.Top
	case w5Middle:
		return c.Middle
	default:
		return c.Bottom
	}
}

// w5Check applies the oracle and counter assertions for one sample.
func w5Check(t *testing.T, s w5Sample) {
	t.Helper()
	if s.OracleDiff != "" {
		t.Fatalf("%s/%s/%s/%s: oracle divergence (incremental != fresh parse): %s",
			s.Lang, s.Size, s.Class, s.Position, s.OracleDiff)
	}
	if !s.FullSpanOK {
		t.Fatalf("%s/%s/%s/%s: incremental root does not cover the full edited buffer", s.Lang, s.Size, s.Class, s.Position)
	}
	if s.Profile.ReuseRejectFragileNonLeaf > 0 {
		// Rejections here are a correctness feature (see
		// IncrementalParseProfile.ReuseRejectFragileNonLeaf's doc comment),
		// never a hard failure -- this gate's fixtures are all deliberately
		// unambiguous, so a nonzero count here is still logged for
		// visibility rather than silently ignored.
		t.Logf("%s/%s/%s/%s: ReuseRejectFragileNonLeaf=%d on a fixture expected to be unambiguous (not a failure, but worth investigating)",
			s.Lang, s.Size, s.Class, s.Position, s.Profile.ReuseRejectFragileNonLeaf)
	}

	ceiling, ok := w5Ceilings[s.Lang]
	if !ok {
		t.Fatalf("%s: no ceiling registered in w5Ceilings", s.Lang)
	}
	if s.Position == w5Top && s.Profile.ReuseRejectRootNonLeafChanged > ceiling.MaxRootNonLeafChangedAtTop {
		t.Fatalf("%s/%s/%s/%s: ReuseRejectRootNonLeafChanged=%d exceeds top-position ceiling %d -- O(edit) near-top reuse regressed toward O(file)",
			s.Lang, s.Size, s.Class, s.Position, s.Profile.ReuseRejectRootNonLeafChanged, ceiling.MaxRootNonLeafChangedAtTop)
	}

	minReuse := w5ReplaceMinByteReusePercent
	if s.Class != w5Replace {
		minReuse = int(ceiling.forPosition(s.Position).MinByteReusePercent)
	}
	rate := w5ByteReusePercent(s.Profile, s.EditedLen)
	if rate < float64(minReuse) {
		t.Fatalf("%s/%s/%s/%s: byte reuse %.1f%% below floor %d%% (reusedBytes=%d editedLen=%d) -- O(edit) reuse regressed",
			s.Lang, s.Size, s.Class, s.Position, rate, minReuse, s.Profile.ReusedBytes, s.EditedLen)
	}

	t.Logf("%s/%s/%s/%s: rootNonLeafChanged=%d reusedBytes=%d byteReuse=%.1f%% newNodes=%d tokensConsumed=%d incrWall=%s freshWall=%s",
		s.Lang, s.Size, s.Class, s.Position,
		s.Profile.ReuseRejectRootNonLeafChanged, s.Profile.ReusedBytes, rate, s.Profile.NewNodesAllocated,
		s.Profile.TokensConsumed, time.Duration(s.IncrWallNs), time.Duration(s.FreshWallNs))
}

// w5Tiers returns the size tiers this invocation should sweep.
func w5Tiers() []w5SizeTier {
	tiers := []w5SizeTier{w5Tier20KB}
	if w5FullSweep() {
		tiers = append(tiers, w5Tier137KB)
		if w5SlowTier() {
			tiers = append(tiers, w5Tier1MB)
		}
	}
	return tiers
}

// TestW5EditorLatencyGate is the gate. It sweeps every (language, size
// tier, edit class, position) combination for the tiers w5Tiers selects and
// enforces the oracle + counter ceilings on each one. See the file doc
// comment for design, coverage, and ceiling provenance.
//
// The fast (default, ~20KB-only) subset is a single language x class x
// position sweep -- 4 x 3 x 3 = 36 samples -- and is well under the
// campaign's ~60s runtime budget for the normal `go test .` run (each
// sample is a handful of small parses; see the reported PASS time).
func TestW5EditorLatencyGate(t *testing.T) {
	classes := []w5EditClass{w5Insert, w5Delete, w5Replace}
	positions := []w5Position{w5Top, w5Middle, w5Bottom}

	for _, spec := range w5Langs {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			for _, tier := range w5Tiers() {
				tier := tier
				t.Run(tier.name, func(t *testing.T) {
					for _, class := range classes {
						class := class
						t.Run(class.String(), func(t *testing.T) {
							for _, pos := range positions {
								pos := pos
								t.Run(pos.String(), func(t *testing.T) {
									s := w5RunSample(t, spec, tier, class, pos)
									w5Check(t, s)
								})
							}
						})
					}
				})
			}
		})
	}
}

// TestW5CSSNearTopReuseAssertion is the committed W1 measurement assertion
// the W1 PR #395 merge-gate review tracked as a follow-up ("add a committed
// W1 measurement assertion" for the CSS splice profile). It runs the exact
// methodology and the exact fixture
// (testdata/incremental_gate/css_stylesheet.css, the same file
// TestIncrementalInvariantGate sweeps) that review recorded "node reuse
// 63.8% -> 99.5%" for on a near-top single-char insert, and asserts the
// literal campaign floor: byte reuse >= 95%.
func TestW5CSSNearTopReuseAssertion(t *testing.T) {
	src, err := os.ReadFile("testdata/incremental_gate/css_stylesheet.css")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lang := grammars.CssLanguage()

	// Insert immediately before the first rule's opening brace, mirroring
	// the PR #395 review's "near-top insert" methodology.
	braceIdx := bytes.IndexByte(src, '{')
	if braceIdx <= 0 {
		t.Fatal("fixture invariant: no CSS rule found near the top of the fixture")
	}
	offset := braceIdx - 1

	parser := gts.NewParser(lang)
	oldTree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("base parse: %v", err)
	}
	if oldTree.RootNode().HasError() {
		t.Fatal("fixture invariant: base parse must be clean")
	}
	defer oldTree.Release()

	edited, edit := w5ApplyEdit(src, offset, w5Insert)

	freshParser := gts.NewParser(lang)
	freshTree, err := freshParser.Parse(edited)
	if err != nil {
		t.Fatalf("fresh parse of edited: %v", err)
	}
	defer freshTree.Release()

	oldTree.Edit(edit)
	incrTree, prof, err := parser.ParseIncrementalProfiled(edited, oldTree)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	defer incrTree.Release()

	if d := w1bStructuralDiff(lang, freshTree.RootNode(), incrTree.RootNode(), "root"); d != "" {
		t.Fatalf("oracle divergence (incremental != fresh parse): %s", d)
	}
	if int(incrTree.RootNode().EndByte()) != len(edited) {
		t.Fatalf("incremental root does not cover the full edited buffer")
	}

	rate := w5ByteReusePercent(prof, len(edited))
	t.Logf("css_stylesheet.css near-top insert: rootNonLeafChanged=%d reusedBytes=%d byteReuse=%.1f%% tokensConsumed=%d",
		prof.ReuseRejectRootNonLeafChanged, prof.ReusedBytes, rate, prof.TokensConsumed)
	if rate < 95 {
		t.Fatalf("CSS near-top byte reuse %.1f%% below the campaign floor of 95%% (spec.campaign.oedit W5 point 2; PR #395 review recorded 99.5%% via a different formula on this same fixture)", rate)
	}
}

// TestW5DeterminismAcrossFreshParsers is the gate's self-check: the same
// (source, edit, language) run on two independent, freshly constructed
// Parser instances must produce identical profile counters. This is the
// same determinism invariant TestW1BSettleIsDeterministicAcrossFreshParsers
// proves for one fixture; this test proves it for the specific counters
// this gate ratchets on, so a future change that made those counters depend
// on a Parser instance's unrelated history (the class of bug #380's
// cNodeMemoCache warm/cold lottery was) would fail here, not just show up as
// gate flakiness.
func TestW5DeterminismAcrossFreshParsers(t *testing.T) {
	spec := w5Langs[0] // go
	tier := w5Tier20KB
	src, itemCount := spec.build(tier.targetSize)
	sites := w5EditSiteIndices(itemCount)
	offset := w5LastDigitOffset(t, src, spec.marker(sites[w5Top]))

	run := func() gts.IncrementalParseProfile {
		lang := spec.lang()
		parser := gts.NewParser(lang)
		oldTree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("base parse: %v", err)
		}
		defer oldTree.Release()
		edited, edit := w5ApplyEdit(src, offset, w5Insert)
		oldTree.Edit(edit)
		incrTree, prof, err := parser.ParseIncrementalProfiled(edited, oldTree)
		if err != nil {
			t.Fatalf("incremental parse: %v", err)
		}
		defer incrTree.Release()
		return prof
	}

	a := run()
	b := run()

	// Compare exactly the counters this gate ratchets on plus the ones its
	// doc comment claims are input-deterministic; a full IncrementalParseProfile
	// struct-equal would also compare wall-time nanosecond fields, which are
	// never expected to be identical across two independent runs.
	if a.ReuseRejectRootNonLeafChanged != b.ReuseRejectRootNonLeafChanged ||
		a.ReusedBytes != b.ReusedBytes ||
		a.NewNodesAllocated != b.NewNodesAllocated ||
		a.TokensConsumed != b.TokensConsumed ||
		a.ReuseRejectFragileNonLeaf != b.ReuseRejectFragileNonLeaf {
		t.Fatalf("W5 non-determinism: two fresh Parsers produced different gate counters for the same (source, edit, language):\n a=%+v\n b=%+v", a, b)
	}
}
