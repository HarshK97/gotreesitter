package grammars

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func TestCleanTrailingExtraOwnershipDoesNotNeedResultCompatibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		lang   *gotreesitter.Language
		source string
	}{
		{name: "rst", lang: RstLanguage(), source: "Title\n=====\n\nbody\n"},
		{name: "comment", lang: CommentLanguage(), source: "hello\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			production, err := gotreesitter.NewParser(test.lang).Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			defer production.Release()
			native, err := gotreesitter.NewParser(test.lang).ParseNoResultCompatibilityBenchmarkOnly(source)
			if err != nil {
				t.Fatal(err)
			}
			defer native.Release()
			productionRoot := production.RootNode()
			nativeRoot := native.RootNode()
			if got, want := nativeRoot.SExpr(test.lang), productionRoot.SExpr(test.lang); got != want {
				t.Fatalf("compatibility-free tree = %s, want production %s", got, want)
			}
			if nativeRoot.EndByte() != uint32(len(source)) {
				t.Fatalf("compatibility-free root end = %d, want %d", nativeRoot.EndByte(), len(source))
			}
			if count := nativeRoot.ChildCount(); count == 0 || nativeRoot.Child(count-1).IsExtra() {
				t.Fatalf("compatibility-free root retained trailing extra: %s", nativeRoot.SExpr(test.lang))
			}
		})
	}
}
