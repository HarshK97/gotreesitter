package gotreesitter_test

import (
	"fmt"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestStatelessScannerIncrementalCertification certifies a capability class,
// not a name-based runtime allowlist. Each scanner has a nil payload, empty
// serialization, and a Scan decision derived only from lookahead plus the
// parser-provided valid-symbol set. The fresh oracle still crosses every
// grammar independently so parser ownership remains part of admission.
func TestStatelessScannerIncrementalCertification(t *testing.T) {
	cases := []struct {
		name                 string
		lang                 func() *gts.Language
		line                 func(int) string
		minMacroReusePercent uint64
		minMacroReuseBytes   uint64
	}{
		{name: "cue", lang: grammars.CueLanguage, line: func(i int) string { return fmt.Sprintf("field_%06d: %d\n", i, i) }, minMacroReusePercent: 15},
		{name: "d", lang: grammars.DLanguage, line: func(i int) string { return fmt.Sprintf("int value_%06d = %d;\n", i, i) }, minMacroReusePercent: 15},
		{name: "elixir", lang: grammars.ElixirLanguage, line: func(i int) string { return fmt.Sprintf("value_%06d = %d\n", i, i) }, minMacroReuseBytes: 16},
		{name: "erlang", lang: grammars.ErlangLanguage, line: func(i int) string { return fmt.Sprintf("value_%06d() -> %d.\n", i, i) }, minMacroReusePercent: 25},
		{name: "gleam", lang: grammars.GleamLanguage, line: func(i int) string { return fmt.Sprintf("const value_%06d = \"%d\"\n", i, i) }, minMacroReusePercent: 3},
		{name: "move", lang: grammars.MoveLanguage, line: func(i int) string { return fmt.Sprintf("module 0x1::value_%06d { const VALUE: u64 = %d; }\n", i, i) }, minMacroReusePercent: 5},
		{name: "tcl", lang: grammars.TclLanguage, line: func(i int) string { return fmt.Sprintf("set value_%06d %d\n", i, i) }, minMacroReuseBytes: 32},
		{name: "wgsl", lang: grammars.WgslLanguage, line: func(i int) string { return fmt.Sprintf("/* value */ fn value_%06d() { let x: i32 = %d; }\n", i, i) }, minMacroReusePercent: 45},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plans := []struct {
				name      string
				size      int
				positions []string
				classes   []statelessScannerEditClass
			}{
				{name: "4KiB", size: 4 << 10, positions: []string{"start", "middle", "end"}, classes: []statelessScannerEditClass{statelessScannerInsert, statelessScannerDelete, statelessScannerReplace}},
				{name: "137KiB", size: 137 << 10, positions: []string{"middle"}, classes: []statelessScannerEditClass{statelessScannerReplace}},
			}
			for _, plan := range plans {
				source, sites := makeStatelessScannerCertificationSource(plan.size, tc.line)
				for _, position := range plan.positions {
					var site int
					switch position {
					case "start":
						site = sites[1]
					case "middle":
						site = sites[len(sites)/2]
					case "end":
						site = sites[len(sites)-2]
					}
					for _, class := range plan.classes {
						t.Run(fmt.Sprintf("%s/%s/%s", plan.name, class, position), func(t *testing.T) {
							edited, edit := applyStatelessScannerCertificationEdit(source, site, class)
							profile := runStatelessScannerCertificationSample(t, tc.lang(), source, edited, edit)
							if plan.size >= 137<<10 && profile.ReusedBytes*100 < uint64(len(edited))*tc.minMacroReusePercent {
								t.Fatalf("stateless scanner reused only %d/%d bytes (<%d%%): %+v", profile.ReusedBytes, len(edited), tc.minMacroReusePercent, profile)
							}
							if plan.size >= 137<<10 && profile.ReusedBytes < tc.minMacroReuseBytes {
								t.Fatalf("stateless scanner reused only %d bytes (<%d): %+v", profile.ReusedBytes, tc.minMacroReuseBytes, profile)
							}
						})
					}
				}
			}
		})
	}
}

type statelessScannerEditClass uint8

const (
	statelessScannerInsert statelessScannerEditClass = iota
	statelessScannerDelete
	statelessScannerReplace
)

func (c statelessScannerEditClass) String() string {
	switch c {
	case statelessScannerInsert:
		return "insert"
	case statelessScannerDelete:
		return "delete"
	case statelessScannerReplace:
		return "replace"
	default:
		return "unknown"
	}
}

func makeStatelessScannerCertificationSource(target int, line func(int) string) ([]byte, []int) {
	var source strings.Builder
	source.Grow(target + 128)
	sites := make([]int, 0, target/24)
	for i := 0; source.Len() < target || len(sites) < 4; i++ {
		entry := line(i)
		underscore := strings.IndexByte(entry, '_')
		if underscore < 0 {
			panic("stateless scanner certification line lacks marker")
		}
		marker := underscore + 1
		sites = append(sites, source.Len()+marker)
		source.WriteString(entry)
	}
	return []byte(source.String()), sites
}

func applyStatelessScannerCertificationEdit(source []byte, offset int, class statelessScannerEditClass) ([]byte, gts.InputEdit) {
	oldEnd := offset
	replacement := []byte{'8'}
	switch class {
	case statelessScannerDelete:
		oldEnd = offset + 1
		replacement = nil
	case statelessScannerReplace:
		oldEnd = offset + 1
		replacement = []byte("88")
	}
	edited := make([]byte, 0, len(source)-(oldEnd-offset)+len(replacement))
	edited = append(edited, source[:offset]...)
	edited = append(edited, replacement...)
	edited = append(edited, source[oldEnd:]...)
	return edited, gts.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(oldEnd),
		NewEndByte:  uint32(offset + len(replacement)),
		StartPoint:  pointForOffset(source, offset),
		OldEndPoint: pointForOffset(source, oldEnd),
		NewEndPoint: pointForOffset(edited, offset+len(replacement)),
	}
}

func runStatelessScannerCertificationSample(t *testing.T, lang *gts.Language, source, edited []byte, edit gts.InputEdit) gts.IncrementalParseProfile {
	t.Helper()
	parser := gts.NewParser(lang)
	parser.SetAdmissionCandidateRoute(false)
	oldTree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("initial parse: %v", err)
	}
	defer oldTree.Release()
	oldRoot := requireCompleteParse(t, oldTree, source, lang, "initial stateless-scanner certification")
	if oldRoot.HasError() {
		t.Fatalf("initial fixture produced ERROR: %s", oldRoot.SExpr(lang))
	}
	oldTree.Edit(edit)

	incremental, profile, err := parser.ParseIncrementalProfiled(edited, oldTree)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	defer incremental.Release()
	requireCompleteParse(t, incremental, edited, lang, "incremental stateless-scanner certification")
	if profile.ReuseUnsupported || !profile.OldTreeReuseRoute || profile.ReusedSubtrees == 0 || profile.ReusedBytes == 0 {
		t.Fatalf("stateless scanner did not enter useful reuse: %+v", profile)
	}

	fresh, err := gts.NewParser(lang).Parse(edited)
	if err != nil {
		t.Fatalf("fresh parse: %v", err)
	}
	defer fresh.Release()
	requireCompleteParse(t, fresh, edited, lang, "fresh stateless-scanner certification")
	requireIncrementalDeepTreeMatchesFresh(t, incremental, fresh, lang)
	return profile
}
