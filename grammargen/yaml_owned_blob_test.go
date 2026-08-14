package grammargen

import (
	"bytes"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestYAMLOwnedGrammarReproducesEmbeddedBlob(t *testing.T) {
	grammar := YAMLGrammar()
	if !grammar.PreferPreciseExternalLexStates {
		t.Fatal("YAML grammar must preserve precise external lexer states")
	}

	lang, blob, err := GenerateLanguageAndBlob(grammar)
	if err != nil {
		t.Fatalf("generate owned YAML grammar: %v", err)
	}
	if !lang.GeneratedByGrammargen {
		t.Fatal("generated YAML language lacks grammargen provenance")
	}
	if got := len(lang.ExternalSymbols); got != 113 {
		t.Fatalf("external symbol count = %d, want 113", got)
	}
	if got := len(lang.ExternalLexStates); got < 100 {
		t.Fatalf("external lexer state count = %d, want at least 100", got)
	}

	embedded := grammars.BlobByName("yaml")
	if !bytes.Equal(blob, embedded) {
		t.Fatalf("owned YAML blob differs from the embedded artifact")
	}
}
