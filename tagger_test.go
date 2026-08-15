package gotreesitter

import (
	"errors"
	"testing"
	"time"
)

func TestTaggerTimeoutOptionConfiguresParser(t *testing.T) {
	tagger, err := NewTagger(queryTestLanguage(), "", WithTaggerTimeoutMicros(54_321))
	if err != nil {
		t.Fatal(err)
	}
	if got := tagger.parser.TimeoutMicros(); got != 54_321 {
		t.Fatalf("timeout = %d, want 54321", got)
	}
}

func TestTagIncrementalStrictReportsTimeout(t *testing.T) {
	tagger, err := NewTagger(buildArithmeticLanguage(), "",
		WithTaggerTimeoutMicros(100),
		WithTaggerTokenSourceFactory(func(source []byte) TokenSource {
			return &slowArithmeticTokenSource{
				delay: 2 * time.Millisecond,
				tokens: []Token{
					{Symbol: 1, StartByte: 0, EndByte: 1},
					{Symbol: 0, StartByte: 1, EndByte: 1},
				},
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	tags, tree, err := tagger.TagIncrementalStrict([]byte("1"), nil)
	if tree == nil {
		t.Fatal("strict tagger returned nil partial tree")
	}
	defer tree.Release()
	if !errors.Is(err, ErrParseStoppedEarly) {
		t.Fatalf("error = %v, want ErrParseStoppedEarly", err)
	}
	if got := tree.ParseStopReason(); got != ParseStopTimeout {
		t.Fatalf("stop reason = %q, want %q", got, ParseStopTimeout)
	}
	if tags != nil {
		t.Fatalf("tags = %#v, want nil", tags)
	}
}

func TestTagStrictReportsTimeout(t *testing.T) {
	tagger, err := NewTagger(buildArithmeticLanguage(), "",
		WithTaggerTimeoutMicros(100),
		WithTaggerTokenSourceFactory(func(source []byte) TokenSource {
			return &slowArithmeticTokenSource{
				delay: 2 * time.Millisecond,
				tokens: []Token{
					{Symbol: 1, StartByte: 0, EndByte: 1},
					{Symbol: 0, StartByte: 1, EndByte: 1},
				},
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	tags, err := tagger.TagStrict([]byte("1"))
	var stopped *ParseStoppedEarlyError
	if !errors.As(err, &stopped) {
		t.Fatalf("error = %v, want ParseStoppedEarlyError", err)
	}
	if stopped.Reason != ParseStopTimeout {
		t.Fatalf("stop reason = %q, want %q", stopped.Reason, ParseStopTimeout)
	}
	if tags != nil {
		t.Fatalf("tags = %#v, want nil", tags)
	}
}

func TestTaggerBasic(t *testing.T) {
	lang := queryTestLanguage()

	tagger, err := NewTagger(lang, `
(function_declaration name: (identifier) @name) @definition.function
`)
	if err != nil {
		t.Fatalf("NewTagger error: %v", err)
	}

	tree := buildSimpleTree(lang)
	tags := tagger.TagTree(tree)

	if len(tags) == 0 {
		t.Fatal("expected tags, got none")
	}

	found := false
	for _, tag := range tags {
		if tag.Kind == "definition.function" {
			found = true
			if tag.Name == "" {
				t.Error("definition.function tag has empty Name")
			}
		}
	}
	if !found {
		t.Errorf("no definition.function tag found in %+v", tags)
	}
}

func TestTaggerEmptySource(t *testing.T) {
	lang := queryTestLanguage()
	tagger, err := NewTagger(lang, `(function_declaration) @definition.function`)
	if err != nil {
		t.Fatalf("NewTagger error: %v", err)
	}

	tags := tagger.Tag(nil)
	if tags != nil {
		t.Errorf("expected nil for nil source, got %+v", tags)
	}

	tags = tagger.Tag([]byte{})
	if tags != nil {
		t.Errorf("expected nil for empty source, got %+v", tags)
	}
}

func TestTaggerInvalidQuery(t *testing.T) {
	lang := queryTestLanguage()
	_, err := NewTagger(lang, `(nonexistent_node) @name @definition.function`)
	if err == nil {
		t.Fatal("expected error for invalid query")
	}
}

func TestTaggerWithTokenSourceFactory(t *testing.T) {
	lang := queryTestLanguage()
	factoryCalled := false
	factory := func(source []byte) TokenSource {
		factoryCalled = true
		return &eofTokenSource{pos: uint32(len(source))}
	}

	tagger, err := NewTagger(lang, `(function_declaration) @definition.function`,
		WithTaggerTokenSourceFactory(factory))
	if err != nil {
		t.Fatalf("NewTagger error: %v", err)
	}

	tagger.Tag([]byte("func main() { 42 }"))
	if !factoryCalled {
		t.Error("expected token source factory to be called")
	}
}

func TestTaggerTagTree(t *testing.T) {
	lang := queryTestLanguage()
	tagger, err := NewTagger(lang, `
(function_declaration) @definition.function
`)
	if err != nil {
		t.Fatalf("NewTagger error: %v", err)
	}

	tree := buildSimpleTree(lang)
	tags := tagger.TagTree(tree)

	if len(tags) == 0 {
		t.Fatal("expected tags from TagTree, got none")
	}
}

func TestTaggerIncremental(t *testing.T) {
	lang := queryTestLanguage()
	tagger, err := NewTagger(lang, `(function_declaration) @definition.function`)
	if err != nil {
		t.Fatalf("NewTagger error: %v", err)
	}

	tags, tree := tagger.TagIncremental([]byte("func main() { 42 }"), nil)
	_ = tags
	if tree == nil {
		t.Fatal("TagIncremental returned nil tree")
	}
}
