//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestParitySwiftHiddenFieldProjectionDeepDigest keeps the full structural
// contract for field projection through hidden Swift grammar symbols. The
// deep digest includes field names and byte and point ranges.
func TestParitySwiftHiddenFieldProjectionDeepDigest(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantError bool
	}{
		{
			name:   "clean-parameter",
			source: "func f(_ value: Int) {}\n",
		},
		{
			name:   "clean-associatedtype",
			source: "protocol P {\n  associatedtype Stride: SignedNumeric\n}\n",
		},
		{
			name:      "issue-577-lifetime-attribute",
			source:    "struct S {\n  @lifetime(copy value)\n  public init(_ value: Int) {}\n}\n",
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(test.source)
			goTree, goLang, err := parseWithGo(parityCase{name: "swift", source: test.source}, source, nil)
			if err != nil {
				t.Fatalf("parse Swift with Go: %v", err)
			}
			defer releaseGoTree(goTree)

			cLang, err := ParityCLanguage("swift")
			if err != nil {
				t.Fatalf("load locked Swift C parser: %v", err)
			}
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				t.Fatalf("set locked Swift C parser language: %v", err)
			}
			cTree := cParser.Parse(source, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatal("locked Swift C parser returned no tree")
			}
			defer cTree.Close()

			goRoot := goTree.RootNode()
			cRoot := cTree.RootNode()
			if got, want := goRoot.HasError(), cRoot.HasError(); got != want {
				t.Fatalf("HasError() Go=%v C=%v", got, want)
			}
			if got := cRoot.HasError(); got != test.wantError {
				t.Fatalf("locked C HasError()=%v, want %v", got, test.wantError)
			}

			goInspection, err := benchfixtures.InspectGoTree(goRoot, goLang)
			if err != nil {
				t.Fatalf("inspect Go gts-deep-tree-v1 tree: %v", err)
			}
			cDigest, err := COracleDeepDigest(cTree)
			if err != nil {
				t.Fatalf("inspect locked C gts-deep-tree-v1 tree: %v", err)
			}
			if goInspection.SHA256 == cDigest {
				return
			}

			diff := FirstDivergenceDumpV1(goRoot, goLang, cRoot)
			t.Fatalf("gts-deep-tree-v1 digest Go=%s C=%s first_difference=%+v", goInspection.SHA256, cDigest, diff)
		})
	}
}

// TestParitySwiftAssociatedtypeFieldProjectionAfterRecovery checks the #578
// field label. A separate repair must correct its ERROR range.
func TestParitySwiftAssociatedtypeFieldProjectionAfterRecovery(t *testing.T) {
	const sourceText = "protocol P {\n  associatedtype Stride: SignedNumeric, Comparable\n}\n"
	source := []byte(sourceText)
	typeStart := strings.Index(sourceText, "SignedNumeric")
	if typeStart < 0 {
		t.Fatal("find SignedNumeric in Swift source")
	}

	goTree, goLang, err := parseWithGo(parityCase{name: "swift", source: sourceText}, source, nil)
	if err != nil {
		t.Fatalf("parse Swift with Go: %v", err)
	}
	defer releaseGoTree(goTree)

	cLang, err := ParityCLanguage("swift")
	if err != nil {
		t.Fatalf("load locked Swift C parser: %v", err)
	}
	cParser := sitter.NewParser()
	defer cParser.Close()
	if err := cParser.SetLanguage(cLang); err != nil {
		t.Fatalf("set locked Swift C parser language: %v", err)
	}
	cTree := cParser.Parse(source, nil)
	if cTree == nil || cTree.RootNode() == nil {
		t.Fatal("locked Swift C parser returned no tree")
	}
	defer cTree.Close()

	goRoot := goTree.RootNode()
	cRoot := cTree.RootNode()
	if got, want := goRoot.HasError(), cRoot.HasError(); got != want {
		t.Fatalf("HasError() Go=%v C=%v", got, want)
	}
	if !cRoot.HasError() {
		t.Fatal("locked Swift C parser did not report the expected recovery error")
	}

	goField, ok := swiftGoIncomingFieldAt(goRoot, goLang, "user_type", uint32(typeStart))
	if !ok {
		t.Fatal("Go Swift tree has no user_type for SignedNumeric")
	}
	cField, ok := swiftCIncomingFieldAt(cRoot, "user_type", uint32(typeStart))
	if !ok {
		t.Fatal("locked Swift C tree has no user_type for SignedNumeric")
	}
	if got, want := goField, "name"; got != want {
		t.Fatalf("Go incoming field for SignedNumeric = %q, want %q", got, want)
	}
	if got, want := cField, "name"; got != want {
		t.Fatalf("locked C incoming field for SignedNumeric = %q, want %q", got, want)
	}
	if goField != cField {
		t.Fatalf("incoming field for SignedNumeric Go=%q C=%q", goField, cField)
	}
}

func swiftGoIncomingFieldAt(node *gotreesitter.Node, lang *gotreesitter.Language, typ string, startByte uint32) (string, bool) {
	if node == nil {
		return "", false
	}
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		if child.Type(lang) == typ && child.StartByte() == startByte {
			return node.FieldNameForChild(i, lang), true
		}
		if field, ok := swiftGoIncomingFieldAt(child, lang, typ, startByte); ok {
			return field, true
		}
	}
	return "", false
}

func swiftCIncomingFieldAt(node *sitter.Node, typ string, startByte uint32) (string, bool) {
	if node == nil {
		return "", false
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child == nil {
			continue
		}
		if child.Kind() == typ && uint32(child.StartByte()) == startByte {
			return node.FieldNameForChild(uint32(i)), true
		}
		if field, ok := swiftCIncomingFieldAt(child, typ, startByte); ok {
			return field, true
		}
	}
	return "", false
}
