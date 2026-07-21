package gotreesitter_test

// Adversarial byte-sweep differential for the campaign post-admission-frontier
// T2a leading-run block-splice (spec.campaign.post-admission-frontier). For
// every insert / delete / replace edit at every byte position of an adversarial
// multi-item source, the incremental parse must be IDENTICAL to a fresh parse of
// the same edited text, at every site where the fresh parse is genuinely clean
// (full-span, zero IsError/IsMissing).
//
// Identity is checked with oeditSerialize (the hardened #418 serializer: every
// node's type, [startByte-endByte], named-vs-anonymous, missing, error, walking
// ALL children including anonymous), NOT SExpr -- the leading splice reuses whole
// top-level items across the edit, exactly the span/anonymous-child state SExpr
// hides.
//
// Fixtures target the leading-splice boundary cases the design must survive:
// comments between top-level items, extras before the first item, edits into
// item 0, an untyped-parameter-list function (Go's tracked ambiguity construct)
// sitting in the spliced leading run, and (for css) the production route the
// splice runs on.
//
// The primary check is a DIFFERENTIAL, the same shape as the #418 range-limited
// sweep: for the same Parser, the leading-splice-ON tree must be byte-identical
// (oeditSerialize) to the leading-splice-OFF tree (SetDisableLeadingRunSplice).
// Because both sides share every OTHER reuse path, a pre-existing incremental
// divergence appears on BOTH and cancels; only a divergence the leading splice
// ITSELF causes survives. This is what lets adversarial fixtures with their own
// pre-existing reuse quirks (css, the untyped-parameter construct) be swept
// without an allowlist. A secondary check adds oracle strength: where the
// leading-OFF tree already equals fresh, the leading-ON tree must too.
//
// Coverage spans the leading-enabled languages (go, production-route css), the
// reuse-declining one (python), AND the barred family (typescript, tsx,
// javascript) -- for the barred family the differential must be a strict no-op
// (leading OFF == leading ON, since the splice never engages), which the sweep
// confirms directly.

import (
	"fmt"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type leadingSweepCase struct {
	name string
	lang *gts.Language
	src  []byte
}

func leadingSweepCases() []leadingSweepCase {
	goLang := grammars.GoLanguage()

	// Comments between items + a leading file comment + a block comment.
	goComments := []byte("package p\n\n" +
		"// leading file comment\n" +
		"func A0(a int) int {\n\treturn a\n}\n\n" +
		"// between A0 and A1\n" +
		"func A1(b int) int {\n\treturn b * 2\n}\n\n" +
		"/* block between A1 and A2 */\n" +
		"func A2(c int) int {\n\tif c > 0 {\n\t\treturn c\n\t}\n\treturn 0\n}\n\n" +
		"func A3(d int) int {\n\treturn d\n}\n")

	// An untyped-parameter-list function (tracked ambiguity) as a LEADING item,
	// plus later clean functions to edit so the untyped-param function splices.
	goUntyped := []byte("package p\n\n" +
		"func h(a, b int) int {\n\treturn a\n}\n\n" +
		"func C0(x int) int {\n\treturn x\n}\n\n" +
		"func C1(x int) int {\n\treturn x + 1\n}\n\n" +
		"func C2(x int) int {\n\treturn x + 2\n}\n")

	// Mixed top-level declaration forms with an import/const/var run ahead of a
	// function, so the leading run crosses several distinct top-level symbols.
	goDecls := []byte("package p\n\n" +
		"import \"fmt\"\n\n" +
		"const A = 1\n\n" +
		"var x = 2\n\n" +
		"type T struct {\n\tF int\n}\n\n" +
		"func F() { fmt.Println(x) }\n\n" +
		"func G() int { return A }\n")

	// CSS on the production route: comments between rules + a leading comment.
	cssComments := []byte("/* header comment */\n" +
		".a {\n  color: red;\n  margin: 0;\n}\n\n" +
		"/* between a and b */\n" +
		".b {\n  padding: 1px;\n}\n\n" +
		"#c {\n  display: block;\n  width: 10px;\n}\n\n" +
		".d {\n  color: blue;\n}\n")

	// Python: comments between defs + a leading module comment. Python declines
	// reuse today, so incremental == fresh is the trivial invariant here, and the
	// sweep confirms the shared leading-splice plumbing stays neutral for it.
	pyComments := []byte("# module comment\n" +
		"def f0(a):\n    return a\n\n" +
		"# between f0 and f1\n" +
		"def f1(a):\n    v = a + 1\n    return v\n\n" +
		"def f2(a):\n    return a * 2\n")

	// TypeScript / TSX: comments between items. The family is barred from the
	// leading splice, so the differential must be a strict no-op here.
	tsComments := []byte("// module header\n" +
		"export function f0(a: number): number {\n  return a;\n}\n\n" +
		"// between f0 and f1\n" +
		"export function f1(a: number): number {\n  const v = a + 1;\n  return v;\n}\n\n" +
		"interface Shape {\n  width: number;\n  height: number;\n}\n")

	jsComments := []byte("// module header\n" +
		"function f0(a) {\n  return a;\n}\n\n" +
		"// between f0 and f1\n" +
		"function f1(a) {\n  const v = a + 1;\n  return v;\n}\n\n" +
		"const g = (x) => x * 2;\n")

	return []leadingSweepCase{
		{"go-comments", goLang, goComments},
		{"go-untyped-leading", goLang, goUntyped},
		{"go-decls", goLang, goDecls},
		{"css-comments", grammars.CssLanguage(), cssComments},
		{"python-comments", grammars.PythonLanguage(), pyComments},
		{"typescript-comments", grammars.TypescriptLanguage(), tsComments},
		{"tsx-comments", grammars.TsxLanguage(), tsComments},
		{"javascript-comments", grammars.JavascriptLanguage(), jsComments},
	}
}

func TestLeadingSpliceByteSweep(t *testing.T) {
	for _, tc := range leadingSweepCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runLeadingSweep(t, tc)
		})
	}
}

func runLeadingSweep(t *testing.T, tc leadingSweepCase) {
	t.Helper()
	baseP := gts.NewParser(tc.lang)
	baseline, err := baseP.Parse(tc.src)
	if err != nil || baseline == nil || baseline.RootNode() == nil {
		t.Fatalf("baseline parse: %v", err)
	}
	defer baseline.Release()
	if e, m := incrGateTreeStats(baseline.RootNode()); e != 0 || m != 0 {
		t.Fatalf("baseline source must be clean (err=%d miss=%d)", e, m)
	}

	freshP := gts.NewParser(tc.lang)
	incrP := gts.NewParser(tc.lang)
	clean, checked, oracle := 0, 0, 0
	for i := 1; i < len(tc.src)-1; i++ {
		for _, cls := range []string{"insert", "delete", "replace"} {
			edited, edit := incrGateBuildEdit(tc.src, i, cls)

			fresh, err := freshP.Parse(edited)
			if err != nil || fresh == nil || fresh.RootNode() == nil {
				if fresh != nil {
					fresh.Release()
				}
				continue
			}
			if int(fresh.RootNode().EndByte()) != len(edited) {
				fresh.Release()
				continue
			}
			if e, m := incrGateTreeStats(fresh.RootNode()); e != 0 || m != 0 {
				fresh.Release()
				continue
			}
			clean++

			// Leading-splice ON (the change under test).
			incrP.SetDisableLeadingRunSplice(false)
			oldOn := baseline.Copy()
			oldOn.Edit(edit)
			on, err := incrP.ParseIncremental(edited, oldOn)
			if err != nil || on == nil || on.RootNode() == nil {
				t.Fatalf("%s pos=%d class=%s: leading-ON incremental parse failed while fresh was clean", tc.name, i, cls)
			}
			onS := oeditSerialize(on.RootNode(), tc.lang)

			// Leading-splice OFF (the pre-change behavior).
			incrP.SetDisableLeadingRunSplice(true)
			oldOff := baseline.Copy()
			oldOff.Edit(edit)
			off, err := incrP.ParseIncremental(edited, oldOff)
			if err != nil || off == nil || off.RootNode() == nil {
				t.Fatalf("%s pos=%d class=%s: leading-OFF incremental parse failed", tc.name, i, cls)
			}
			offS := oeditSerialize(off.RootNode(), tc.lang)
			incrP.SetDisableLeadingRunSplice(false)
			checked++

			// Primary isolation invariant: the leading splice must be a no-op vs
			// leaving it off. Any difference here is a divergence THIS change
			// caused (a pre-existing reuse quirk appears on both sides and
			// cancels).
			if onS != offS {
				t.Fatalf("%s pos=%d class=%s: leading-ON != leading-OFF (the leading splice changed the tree)\n  off=%s\n  on =%s",
					tc.name, i, cls, oeditTrunc(offS), oeditTrunc(onS))
			}

			// Secondary oracle: where leading-OFF already equals fresh, leading-ON
			// must too (pre-existing off-vs-fresh divergences are out of scope --
			// tracked by TestIncrementalInvariantGate).
			freshS := oeditSerialize(fresh.RootNode(), tc.lang)
			if offS == freshS {
				oracle++
				if onS != freshS {
					t.Fatalf("%s pos=%d class=%s: leading-ON != fresh while leading-OFF == fresh\n  fresh=%s\n  on   =%s",
						tc.name, i, cls, oeditTrunc(freshS), oeditTrunc(onS))
				}
			}
			oldOn.Release()
			oldOff.Release()
			fresh.Release()
			on.Release()
			off.Release()
		}
	}
	t.Logf("%s: freshCleanSites=%d checked=%d oracleMatched=%d", tc.name, clean, checked, oracle)
	if checked == 0 {
		t.Fatalf("%s: no freshly-clean sites checked -- sweep did not exercise the invariant", tc.name)
	}
}

// TestLeadingSpliceBarredLanguagesDoNotSplice proves the JS/TS/TSX bar
// (leadingSpliceLanguageBarred): a middle edit that WOULD present a large leading
// run splices ZERO leading top-level items for the barred family, so their
// incremental behavior is byte-identical to before this change. The counter it
// checks is the leading run's block-splice steps at a middle edit; for a barred
// language the trailing run may still splice, so this asserts the reject counter
// still SCALES (stays large / O(file)) at a middle edit -- the signature of "the
// leading run was NOT block-spliced".
func TestLeadingSpliceBarredLanguagesDoNotSplice(t *testing.T) {
	build := func(fmtStr string, n int) []byte {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, fmtStr, i, i)
		}
		return []byte(b.String())
	}
	cases := []struct {
		name string
		lang *gts.Language
		src  []byte
		mark string
	}{
		{"typescript", grammars.TypescriptLanguage(),
			build("export function f%d(a: number): number {\n  const v = a + %d;\n  return v;\n}\n\n", 200), "f100("},
		{"tsx", grammars.TsxLanguage(),
			build("export function f%d(a: number): number {\n  const v = a + %d;\n  return v;\n}\n\n", 200), "f100("},
		{"javascript", grammars.JavascriptLanguage(),
			build("function f%d(a) {\n  const v = a + %d;\n  return v;\n}\n\n", 200), "f100("},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mi := strings.Index(string(tc.src), tc.mark)
			if mi < 0 {
				t.Fatalf("marker %q not found", tc.mark)
			}
			off := mi + len(tc.mark) - 2 // a digit inside the index literal
			fresh, incr, prof, edited := leadingParseAt(t, tc.lang, tc.src, off, "insert")
			_ = fresh
			if int(incr.EndByte()) != len(edited) {
				t.Fatalf("incremental root span %d does not cover edited buffer %d", incr.EndByte(), len(edited))
			}
			// The barred family did NOT block-splice the leading run: with a
			// middle edit and thousands of leading items, the reject counter must
			// still be large (O(leading items)), the pre-T2a signature. A leading
			// splice would have collapsed it to a small constant.
			if prof.ReuseRejectRootNonLeafChanged < 1000 {
				t.Fatalf("%s: reject counter %d is small -- the leading run was block-spliced, but this language is barred",
					tc.name, prof.ReuseRejectRootNonLeafChanged)
			}
		})
	}
}
