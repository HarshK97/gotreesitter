package gotreesitter

import "testing"

func newCueTestLanguage() *Language {
	return &Language{
		Name: "cue",
		SymbolNames: []string{
			"EOF", "source_file", "field", "value", "identifier", "number", "float",
		},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "field", Visible: true, Named: true},
			{Name: "value", Visible: true, Named: true},
			{Name: "identifier", Visible: true, Named: true},
			{Name: "number", Visible: true, Named: true},
			{Name: "float", Visible: true, Named: true},
		},
	}
}

const (
	cueTestSymSourceFile = 1
	cueTestSymField      = 2
	cueTestSymValue      = 3
	cueTestSymIdentifier = 4
	cueTestSymNumber     = 5
)

func buildCueValueTree(arena *nodeArena, start, end uint32) (*Node, *Node) {
	value := newLeafNodeInArena(arena, cueTestSymValue, true, start, end, Point{Column: start}, Point{Column: end})
	field := newParentNodeInArena(arena, cueTestSymField, true, []*Node{value}, nil, 0)
	root := newParentNodeInArena(arena, cueTestSymSourceFile, true, []*Node{field}, nil, 0)
	return root, value
}

// The hidden `_alias_expr` (aliased to `value`) collapses onto a lone leaf
// token child: `x: sub` yields value with no children where C has
// value(identifier). Rebuild the leaf child from the source text.
func TestNormalizeCueValueIdentifierChildRestored(t *testing.T) {
	lang := newCueTestLanguage()
	source := []byte("x: sub")
	arena := newNodeArena(arenaClassFull)
	root, value := buildCueValueTree(arena, 3, 6)

	normalizeCueCompatibility(root, source, lang)

	if got, want := value.ChildCount(), 1; got != want {
		t.Fatalf("value child count = %d, want %d", got, want)
	}
	child := value.Child(0)
	if got, want := child.Type(lang), "identifier"; got != want {
		t.Fatalf("child type = %q, want %q", got, want)
	}
	if child.StartByte() != 3 || child.EndByte() != 6 {
		t.Fatalf("child span = [%d:%d], want [3:6]", child.StartByte(), child.EndByte())
	}
	if !child.IsNamed() {
		t.Fatalf("identifier child should be named")
	}
}

func TestNormalizeCueValueHashIdentifierChildRestored(t *testing.T) {
	lang := newCueTestLanguage()
	source := []byte("x: #Def")
	arena := newNodeArena(arenaClassFull)
	root, value := buildCueValueTree(arena, 3, 7)

	normalizeCueCompatibility(root, source, lang)

	if got, want := value.ChildCount(), 1; got != want {
		t.Fatalf("value child count = %d, want %d", got, want)
	}
	if got, want := value.Child(0).Type(lang), "identifier"; got != want {
		t.Fatalf("child type = %q, want %q", got, want)
	}
}

func TestNormalizeCueValueNumberChildRestored(t *testing.T) {
	lang := newCueTestLanguage()
	source := []byte("module: 123")
	arena := newNodeArena(arenaClassFull)
	root, value := buildCueValueTree(arena, 8, 11)

	normalizeCueCompatibility(root, source, lang)

	if got, want := value.ChildCount(), 1; got != want {
		t.Fatalf("value child count = %d, want %d", got, want)
	}
	if got, want := value.Child(0).Type(lang), "number"; got != want {
		t.Fatalf("child type = %q, want %q", got, want)
	}
}

func TestNormalizeCueValueFloatChildRestored(t *testing.T) {
	lang := newCueTestLanguage()
	source := []byte("x: 1.5")
	arena := newNodeArena(arenaClassFull)
	root, value := buildCueValueTree(arena, 3, 6)

	normalizeCueCompatibility(root, source, lang)

	if got, want := value.ChildCount(), 1; got != want {
		t.Fatalf("value child count = %d, want %d", got, want)
	}
	if got, want := value.Child(0).Type(lang), "float"; got != want {
		t.Fatalf("child type = %q, want %q", got, want)
	}
}

// Keyword literals (true/false/null) are distinct rules in C, not
// identifiers; an empty value over them is not the witnessed collapse —
// leave untouched rather than synthesize the wrong child.
func TestNormalizeCueValueKeywordLiteralUntouched(t *testing.T) {
	lang := newCueTestLanguage()
	source := []byte("x: null")
	arena := newNodeArena(arenaClassFull)
	root, value := buildCueValueTree(arena, 3, 7)

	normalizeCueCompatibility(root, source, lang)

	if got, want := value.ChildCount(), 0; got != want {
		t.Fatalf("value child count = %d, want %d", got, want)
	}
}

// Values that already have children are correct — never touch them.
func TestNormalizeCueValueWithChildrenUntouched(t *testing.T) {
	lang := newCueTestLanguage()
	source := []byte("x: sub")
	arena := newNodeArena(arenaClassFull)
	inner := newLeafNodeInArena(arena, cueTestSymIdentifier, true, 3, 6, Point{Column: 3}, Point{Column: 6})
	value := newParentNodeInArena(arena, cueTestSymValue, true, []*Node{inner}, nil, 0)
	root := newParentNodeInArena(arena, cueTestSymSourceFile, true, []*Node{value}, nil, 0)

	normalizeCueCompatibility(root, source, lang)

	if got, want := value.ChildCount(), 1; got != want {
		t.Fatalf("value child count = %d, want %d", got, want)
	}
	if value.Child(0) != inner {
		t.Fatalf("existing child was replaced")
	}
}

func TestCueClassifyValueLeafText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"sub", "identifier"},
		{"Sub", "identifier"},
		{"_private", "identifier"},
		{"#Def", "identifier"},
		{"_#hidden", "identifier"},
		{"$x", "identifier"},
		{"a1_b", "identifier"},
		{"123", "number"},
		{"0", "number"},
		{"1_000", "number"},
		{"0x1F", "number"},
		{"0b1010", "number"},
		{"0o17", "number"},
		{"-5", "number"},
		{"+5", "number"},
		{"1.5", "float"},
		{"1.", "float"},
		{".5", "float"},
		{"-1.5", "float"},
		{"1e9", "float"},
		{"1.5e-3", "float"},
		{"true", ""},
		{"false", ""},
		{"null", ""},
		{"_", ""},
		{"_|_", ""},
		{"", ""},
		{"a.b", ""},
		{"1x", ""},
		{"08", "number"}, // plain decimal_digits branch admits leading zero

	}
	for _, c := range cases {
		if got := cueClassifyValueLeafText([]byte(c.in)); got != c.want {
			t.Errorf("cueClassifyValueLeafText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
