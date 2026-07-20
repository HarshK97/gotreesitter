package gotreesitter

import "testing"

func TestNormalizeCrystalBraceContainerStarts(t *testing.T) {
	lang := &Language{
		Name:        "crystal",
		SymbolNames: []string{"EOF", "source_file", "hash", "tuple", "named_tuple", "{", "}", "integer"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "hash", Visible: true, Named: true},
			{Name: "tuple", Visible: true, Named: true},
			{Name: "named_tuple", Visible: true, Named: true},
			{Name: "{", Visible: true, Named: false},
			{Name: "}", Visible: true, Named: false},
			{Name: "integer", Visible: true, Named: true},
		},
	}
	source := []byte("x = {1}")

	for _, tc := range []struct {
		name string
		sym  Symbol
	}{
		{name: "hash", sym: 2},
		{name: "named_tuple", sym: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arena := newNodeArena(arenaClassFull)
			open := newLeafNodeInArena(arena, 5, false, 4, 5, Point{Column: 4}, Point{Column: 5})
			value := newLeafNodeInArena(arena, 7, true, 5, 6, Point{Column: 5}, Point{Column: 6})
			close := newLeafNodeInArena(arena, 6, false, 6, 7, Point{Column: 6}, Point{Column: 7})
			container := newParentNodeInArena(arena, tc.sym, true, []*Node{open, value, close}, nil, 0)
			container.startByte = 4
			container.startPoint = Point{Column: 4}
			container.endByte = 7
			container.endPoint = Point{Column: 7}

			normalizeCrystalCompatibility(container, source, lang)

			if got, want := container.StartByte(), uint32(5); got != want {
				t.Fatalf("container.StartByte = %d, want %d", got, want)
			}
			if got, want := container.StartPoint(), (Point{Column: 5}); got != want {
				t.Fatalf("container.StartPoint = %+v, want %+v", got, want)
			}
			if got, want := open.StartByte(), uint32(5); got != want {
				t.Fatalf("open.StartByte = %d, want %d", got, want)
			}
			if got, want := open.EndByte(), uint32(5); got != want {
				t.Fatalf("open.EndByte = %d, want %d", got, want)
			}
			if got, want := open.StartPoint(), (Point{Column: 5}); got != want {
				t.Fatalf("open.StartPoint = %+v, want %+v", got, want)
			}
			if got, want := value.StartByte(), uint32(5); got != want {
				t.Fatalf("value.StartByte = %d, want %d", got, want)
			}
		})
	}

	t.Run("tuple", func(t *testing.T) {
		arena := newNodeArena(arenaClassFull)
		open := newLeafNodeInArena(arena, 5, false, 4, 5, Point{Column: 4}, Point{Column: 5})
		value := newLeafNodeInArena(arena, 7, true, 5, 6, Point{Column: 5}, Point{Column: 6})
		close := newLeafNodeInArena(arena, 6, false, 6, 7, Point{Column: 6}, Point{Column: 7})
		tuple := newParentNodeInArena(arena, 3, true, []*Node{open, value, close}, nil, 0)
		tuple.startByte = 4
		tuple.startPoint = Point{Column: 4}
		tuple.endByte = 7
		tuple.endPoint = Point{Column: 7}

		normalizeCrystalCompatibility(tuple, source, lang)

		if got, want := tuple.StartByte(), uint32(4); got != want {
			t.Fatalf("tuple.StartByte = %d, want %d", got, want)
		}
		if got, want := open.StartByte(), uint32(4); got != want {
			t.Fatalf("tuple open.StartByte = %d, want %d", got, want)
		}
	})
}

func TestNormalizeResultCompatibilityDispatchesCrystal(t *testing.T) {
	lang := &Language{
		Name:        "crystal",
		SymbolNames: []string{"EOF", "source_file", "hash", "{", "}"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "hash", Visible: true, Named: true},
			{Name: "{", Visible: true, Named: false},
			{Name: "}", Visible: true, Named: false},
		},
	}
	source := []byte("x = {}")
	arena := newNodeArena(arenaClassFull)
	open := newLeafNodeInArena(arena, 3, false, 4, 5, Point{Column: 4}, Point{Column: 5})
	close := newLeafNodeInArena(arena, 4, false, 5, 6, Point{Column: 5}, Point{Column: 6})
	hash := newParentNodeInArena(arena, 2, true, []*Node{open, close}, nil, 0)
	hash.startByte = 4
	hash.startPoint = Point{Column: 4}
	hash.endByte = 6
	hash.endPoint = Point{Column: 6}
	root := newParentNodeInArena(arena, 1, true, []*Node{hash}, nil, 0)

	normalizeResultCompatibility(root, source, &Parser{language: lang}, nil)

	if got, want := hash.StartByte(), uint32(5); got != want {
		t.Fatalf("hash.StartByte = %d, want %d", got, want)
	}
	if got, want := open.StartByte(), uint32(5); got != want {
		t.Fatalf("open.StartByte = %d, want %d", got, want)
	}
}

func TestParserNormalizeReturnedTreeDispatchesCrystalCompatibility(t *testing.T) {
	lang := &Language{
		Name:        "crystal",
		SymbolNames: []string{"EOF", "source_file", "hash", "{", "}"},
		SymbolMetadata: []SymbolMetadata{
			{Name: "EOF"},
			{Name: "source_file", Visible: true, Named: true},
			{Name: "hash", Visible: true, Named: true},
			{Name: "{", Visible: true, Named: false},
			{Name: "}", Visible: true, Named: false},
		},
	}
	source := []byte("x = {}")
	arena := newNodeArena(arenaClassFull)
	open := newLeafNodeInArena(arena, 3, false, 4, 5, Point{Column: 4}, Point{Column: 5})
	close := newLeafNodeInArena(arena, 4, false, 5, 6, Point{Column: 5}, Point{Column: 6})
	hash := newParentNodeInArena(arena, 2, true, []*Node{open, close}, nil, 0)
	hash.startByte = 4
	hash.startPoint = Point{Column: 4}
	hash.endByte = 6
	hash.endPoint = Point{Column: 6}
	root := newParentNodeInArena(arena, 1, true, []*Node{hash}, nil, 0)
	parser := &Parser{language: lang}

	if reason := parser.normalizeReturnedTree(root, source, nil); reason != ParseStopNone {
		t.Fatalf("normalizeReturnedTree stop reason = %q, want %q", reason, ParseStopNone)
	}
	if got, want := hash.StartByte(), uint32(5); got != want {
		t.Fatalf("hash.StartByte = %d, want %d", got, want)
	}
	if got, want := open.StartByte(), uint32(5); got != want {
		t.Fatalf("open.StartByte = %d, want %d", got, want)
	}
}
