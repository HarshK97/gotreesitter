package grammargen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemapParityCorpusPath(t *testing.T) {
	cases := []struct{ path, root, want string }{
		{"/tmp/grammar_parity/json/src/grammar.json", "", "/tmp/grammar_parity/json/src/grammar.json"},
		{"/tmp/grammar_parity/json/src/grammar.json", "/tmp/grammar_parity", "/tmp/grammar_parity/json/src/grammar.json"},
		{"/tmp/grammar_parity/json/src/grammar.json", "/durable/corpora", "/durable/corpora/json/src/grammar.json"},
		{"/tmp/grammar_parity/php/php/src/grammar.json", "/durable/corpora", "/durable/corpora/php/php/src/grammar.json"},
		{"/elsewhere/grammar.json", "/durable/corpora", "/elsewhere/grammar.json"},
		{"", "/durable/corpora", ""},
	}
	for _, c := range cases {
		if got := remapParityCorpusPath(c.path, c.root); got != c.want {
			t.Errorf("remapParityCorpusPath(%q, %q) = %q, want %q", c.path, c.root, got, c.want)
		}
	}
}

func TestParityGrammarRepoRootCustomRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "json", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := importParityGrammar{
		name:     "json",
		jsonPath: "/tmp/grammar_parity/json/src/grammar.json",
	}
	got := parityGrammarRepoRoot(g, root)
	want := filepath.Join(root, "json")
	if got != want {
		t.Errorf("parityGrammarRepoRoot custom root = %q, want %q", got, want)
	}
}
