package gotreesitter_test

import (
	"bytes"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

func TestGoLanguageFixtureLengthChangeIncrementalMatchesFresh(t *testing.T) {
	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		t.Fatal(err)
	}
	var source []byte
	for _, fixture := range fixtures {
		if fixture.Fixture.ID == "language" {
			source = fixture.Source
			break
		}
	}
	if source == nil {
		t.Fatal("locked language fixture not found")
	}

	const start, oldEnd = 32063, 32068
	oldText := []byte("T any")
	newText := []byte("T comparable")
	if got := source[start:oldEnd]; !bytes.Equal(got, oldText) {
		t.Fatalf("locked edit source = %q, want %q", got, oldText)
	}
	edited := make([]byte, 0, len(source)+len(newText)-len(oldText))
	edited = append(edited, source[:start]...)
	edited = append(edited, newText...)
	edited = append(edited, source[oldEnd:]...)

	lang := grammars.GoLanguage()
	t.Run("forward", func(t *testing.T) {
		oldTree, err := gotreesitter.NewParser(lang).Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		defer oldTree.Release()
		oldTree.Edit(gotreesitter.InputEdit{
			StartByte:   start,
			OldEndByte:  oldEnd,
			NewEndByte:  start + uint32(len(newText)),
			StartPoint:  gotreesitter.Point{Row: 844, Column: 23},
			OldEndPoint: gotreesitter.Point{Row: 844, Column: 28},
			NewEndPoint: gotreesitter.Point{Row: 844, Column: 35},
		})
		incremental, profile, err := gotreesitter.NewParser(lang).ParseIncrementalProfiled(edited, oldTree)
		if err != nil {
			t.Fatal(err)
		}
		defer incremental.Release()
		fresh, err := gotreesitter.NewParser(lang).Parse(edited)
		if err != nil {
			t.Fatal(err)
		}
		defer fresh.Release()
		assertLockedIncrementalTreeMatchesFresh(t, lang, incremental, fresh, "d5af3b86c49049bf95e1ebf5a098a4194a3d17d08b233c5fb9e5bfd90e37c801")
		if profile.AcceptedErrorRetryAttempts != 1 || !profile.AcceptedErrorRetryAdopted ||
			profile.AcceptedErrorRetryMergePerKey != 3 || profile.AcceptedErrorRetryCause != gotreesitter.IncrementalRetryCauseAcceptedErrorBaseMerge {
			t.Fatalf("forward retry profile = %+v", profile)
		}
		rt := incremental.ParseRuntime()
		if rt.IncrementalAcceptedErrorRetryAttempts != 1 || !rt.IncrementalAcceptedErrorRetryAdopted ||
			rt.IncrementalAcceptedErrorRetryMergePerKey != 3 {
			t.Fatalf("forward retry runtime = %+v", rt)
		}
		if profile.MaxStacksSeen != 24 || rt.MaxStacksSeen != 18 {
			t.Fatalf("forward stack attribution profile=%d selected_runtime=%d, want aggregate=24 selected=18", profile.MaxStacksSeen, rt.MaxStacksSeen)
		}
		if profile.TokensConsumed <= rt.TokensConsumed {
			t.Fatalf("forward token attribution profile=%d selected_runtime=%d, want aggregate profile", profile.TokensConsumed, rt.TokensConsumed)
		}
	})

	t.Run("reverse", func(t *testing.T) {
		oldTree, err := gotreesitter.NewParser(lang).Parse(edited)
		if err != nil {
			t.Fatal(err)
		}
		defer oldTree.Release()
		oldTree.Edit(gotreesitter.InputEdit{
			StartByte:   start,
			OldEndByte:  start + uint32(len(newText)),
			NewEndByte:  oldEnd,
			StartPoint:  gotreesitter.Point{Row: 844, Column: 23},
			OldEndPoint: gotreesitter.Point{Row: 844, Column: 35},
			NewEndPoint: gotreesitter.Point{Row: 844, Column: 28},
		})
		incremental, profile, err := gotreesitter.NewParser(lang).ParseIncrementalProfiled(source, oldTree)
		if err != nil {
			t.Fatal(err)
		}
		defer incremental.Release()
		fresh, err := gotreesitter.NewParser(lang).Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		defer fresh.Release()
		assertLockedIncrementalTreeMatchesFresh(t, lang, incremental, fresh, "583df223904fe414c33bba3b474c6557ecdb20e7f47e304b9a09bfcc2da44539")
		if profile.AcceptedErrorRetryAttempts != 1 || !profile.AcceptedErrorRetryAdopted || profile.AcceptedErrorRetryMergePerKey != 3 {
			t.Fatalf("reverse retry profile = %+v", profile)
		}
	})
}

func TestCleanGoIncrementalDoesNotRunAcceptedErrorRetry(t *testing.T) {
	lang := grammars.GoLanguage()
	original := []byte("package p\n\nfunc f() {\n\tx := 1\n\t_ = x\n}\n")
	edited := []byte("package p\n\nfunc f() {\n\tvalue := 1\n\t_ = x\n}\n")
	oldTree, err := gotreesitter.NewParser(lang).Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	defer oldTree.Release()
	const start = 23
	if original[start] != 'x' {
		t.Fatalf("locked clean edit starts at %q, want x", original[start])
	}
	oldTree.Edit(gotreesitter.InputEdit{
		StartByte:   start,
		OldEndByte:  start + 1,
		NewEndByte:  start + 5,
		StartPoint:  gotreesitter.Point{Row: 3, Column: 1},
		OldEndPoint: gotreesitter.Point{Row: 3, Column: 2},
		NewEndPoint: gotreesitter.Point{Row: 3, Column: 6},
	})
	incremental, profile, err := gotreesitter.NewParser(lang).ParseIncrementalProfiled(edited, oldTree)
	if err != nil {
		t.Fatal(err)
	}
	defer incremental.Release()
	fresh, err := gotreesitter.NewParser(lang).Parse(edited)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()
	if incremental.RootNode().HasError() || fresh.RootNode().HasError() ||
		incremental.RootNode().SExpr(lang) != fresh.RootNode().SExpr(lang) {
		t.Fatalf("clean incremental mismatch incremental=%s fresh=%s", incremental.RootNode().SExpr(lang), fresh.RootNode().SExpr(lang))
	}
	if profile.AcceptedErrorRetryAttempts != 0 || incremental.ParseRuntime().IncrementalAcceptedErrorRetryAttempts != 0 {
		t.Fatalf("clean incremental unexpectedly retried: %+v", profile)
	}
}

func TestMalformedGoIncrementalRunsOneBaseMergeRetryAndKeepsFirstResult(t *testing.T) {
	lang := grammars.GoLanguage()
	original := []byte("package p\nfunc f() {}\n")
	offset := bytes.LastIndexByte(original, '}')
	if offset < 0 {
		t.Fatal("malformed Go fixture missing closing brace")
	}
	edited := append([]byte(nil), original[:offset]...)
	edited = append(edited, original[offset+1:]...)
	oldTree, err := gotreesitter.NewParser(lang).Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	defer oldTree.Release()
	oldTree.Edit(gotreesitter.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(offset + 1),
		NewEndByte:  uint32(offset),
		StartPoint:  pointForOffset(original, offset),
		OldEndPoint: pointForOffset(original, offset+1),
		NewEndPoint: pointForOffset(edited, offset),
	})
	incremental, profile, err := gotreesitter.NewParser(lang).ParseIncrementalProfiled(edited, oldTree)
	if err != nil {
		t.Fatal(err)
	}
	defer incremental.Release()
	fresh, err := gotreesitter.NewParser(lang).Parse(edited)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()
	if !incremental.RootNode().HasError() || !fresh.RootNode().HasError() ||
		incremental.RootNode().SExpr(lang) != fresh.RootNode().SExpr(lang) {
		t.Fatalf("malformed Go incremental/fresh mismatch incremental=%s fresh=%s", incremental.RootNode().SExpr(lang), fresh.RootNode().SExpr(lang))
	}
	incrementalInspection, err := benchfixtures.InspectGoTree(incremental.RootNode(), lang)
	if err != nil {
		t.Fatal(err)
	}
	freshInspection, err := benchfixtures.InspectGoTree(fresh.RootNode(), lang)
	if err != nil {
		t.Fatal(err)
	}
	if incrementalInspection.SHA256 != freshInspection.SHA256 {
		t.Fatalf("malformed Go deep digest incremental=%s fresh=%s", incrementalInspection.SHA256, freshInspection.SHA256)
	}
	rt := incremental.ParseRuntime()
	if profile.AcceptedErrorRetryAttempts != 1 || rt.IncrementalAcceptedErrorRetryAttempts != 1 ||
		profile.AcceptedErrorRetryAdopted || rt.IncrementalAcceptedErrorRetryAdopted ||
		profile.AcceptedErrorRetryMergePerKey != 3 || !profile.OldTreeReuseRoute || !rt.IncrementalOldTreeReuseRoute {
		t.Fatalf("malformed Go retry profile=%+v runtime=%+v", profile, rt)
	}
	incremental.Release()
	if oldTree.RootNode() == nil || oldTree.RootNode().HasError() {
		t.Fatal("releasing retained first incremental result invalidated caller-owned old tree")
	}
}

func TestMalformedRustUnsupportedReuseDoesNotRunBaseMergeRetry(t *testing.T) {
	lang := grammars.RustLanguage()
	original := []byte("fn main() { let x = 1; }\n")
	offset := bytes.LastIndexByte(original, '}')
	if offset < 0 {
		t.Fatal("malformed Rust fixture missing closing brace")
	}
	edited := append([]byte(nil), original[:offset]...)
	edited = append(edited, original[offset+1:]...)
	oldTree, err := gotreesitter.NewParser(lang).Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	defer oldTree.Release()
	oldTree.Edit(gotreesitter.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(offset + 1),
		NewEndByte:  uint32(offset),
		StartPoint:  pointForOffset(original, offset),
		OldEndPoint: pointForOffset(original, offset+1),
		NewEndPoint: pointForOffset(edited, offset),
	})
	incremental, profile, err := gotreesitter.NewParser(lang).ParseIncrementalProfiled(edited, oldTree)
	if err != nil {
		t.Fatal(err)
	}
	defer incremental.Release()
	fresh, err := gotreesitter.NewParser(lang).Parse(edited)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()
	if incremental.RootNode().SExpr(lang) != fresh.RootNode().SExpr(lang) ||
		incremental.RootNode().HasError() != fresh.RootNode().HasError() {
		t.Fatalf("malformed Rust incremental/fresh mismatch incremental=%s fresh=%s", incremental.RootNode().SExpr(lang), fresh.RootNode().SExpr(lang))
	}
	rt := incremental.ParseRuntime()
	if !profile.ReuseUnsupported || profile.OldTreeReuseRoute || rt.IncrementalOldTreeReuseRoute ||
		profile.AcceptedErrorRetryAttempts != 0 || rt.IncrementalAcceptedErrorRetryAttempts != 0 {
		t.Fatalf("malformed Rust unsupported route profile=%+v runtime=%+v", profile, rt)
	}
}

func assertLockedIncrementalTreeMatchesFresh(t *testing.T, lang *gotreesitter.Language, incremental, fresh *gotreesitter.Tree, wantDigest string) {
	t.Helper()
	if incremental == nil || incremental.RootNode() == nil || fresh == nil || fresh.RootNode() == nil {
		t.Fatal("nil incremental or fresh tree")
	}
	if incremental.RootNode().HasError() || fresh.RootNode().HasError() {
		t.Fatalf("HasError incremental=%v fresh=%v", incremental.RootNode().HasError(), fresh.RootNode().HasError())
	}
	if incremental.RootNode().ChildCount() != 195 || fresh.RootNode().ChildCount() != 195 {
		t.Fatalf("root child count incremental=%d fresh=%d want=195", incremental.RootNode().ChildCount(), fresh.RootNode().ChildCount())
	}
	incrementalInspection, err := benchfixtures.InspectGoTree(incremental.RootNode(), lang)
	if err != nil {
		t.Fatal(err)
	}
	freshInspection, err := benchfixtures.InspectGoTree(fresh.RootNode(), lang)
	if err != nil {
		t.Fatal(err)
	}
	if incrementalInspection.SHA256 != freshInspection.SHA256 || freshInspection.SHA256 != wantDigest {
		t.Fatalf("deep digest incremental=%s fresh=%s want=%s", incrementalInspection.SHA256, freshInspection.SHA256, wantDigest)
	}
}

func TestBig3SyntheticIncrementalParsesStayComplete(t *testing.T) {
	cases := []struct {
		name   string
		lang   func() *gotreesitter.Language
		source func(int) []byte
		marker string
	}{
		{
			name:   "typescript",
			lang:   grammars.TypescriptLanguage,
			source: makeTypeScriptBenchmarkSource,
			marker: "const v = ",
		},
		{
			name:   "python",
			lang:   grammars.PythonLanguage,
			source: makePythonBenchmarkSource,
			marker: "v = ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lang := tc.lang()
			parser := gotreesitter.NewParser(lang)
			src := tc.source(128)
			sites := makeBenchmarkEditSites(src, tc.marker)
			if len(sites) == 0 {
				t.Fatalf("missing edit sites for marker %q", tc.marker)
			}
			site := sites[0]

			oldTree, err := parser.Parse(src)
			if err != nil {
				t.Fatalf("initial parse failed: %v", err)
			}
			requireCompleteParse(t, oldTree, src, lang, "initial")
			if oldTree.RootNode().HasError() {
				t.Fatal("initial parse produced error root")
			}

			next := append([]byte(nil), src...)
			toggleDigitAt(next, site.offset)
			oldTree.Edit(gotreesitter.InputEdit{
				StartByte:   uint32(site.offset),
				OldEndByte:  uint32(site.offset + 1),
				NewEndByte:  uint32(site.offset + 1),
				StartPoint:  site.start,
				OldEndPoint: site.end,
				NewEndPoint: site.end,
			})

			newTree, err := parser.ParseIncremental(next, oldTree)
			if err != nil {
				t.Fatalf("incremental parse failed: %v", err)
			}
			requireCompleteParse(t, newTree, next, lang, "incremental")
			if newTree.RootNode().HasError() {
				t.Fatal("incremental parse produced error root")
			}
		})
	}
}
