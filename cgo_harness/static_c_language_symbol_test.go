//go:build cgo && treesitter_c_parity && treesitter_c_perfscan

package cgoharness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticCLanguageSymbolAcceptsVoidAndEmptyParameterLists(t *testing.T) {
	for _, tc := range []struct {
		name   string
		entry  parityLockEntry
		source string
		want   string
	}{
		{
			name:   "void",
			entry:  parityLockEntry{Name: "go", RepoURL: "https://github.com/tree-sitter/tree-sitter-go"},
			source: "const TSLanguage *tree_sitter_go(void) { return 0; }\n",
			want:   "tree_sitter_go",
		},
		{
			name:   "empty_with_export_macro",
			entry:  parityLockEntry{Name: "scss", RepoURL: "https://github.com/serenadeai/tree-sitter-scss"},
			source: "TS_PUBLIC const TSLanguage *tree_sitter_scss() { return 0; }\n",
			want:   "tree_sitter_scss",
		},
		{
			name:   "whitespace",
			entry:  parityLockEntry{Name: "scss", RepoURL: "https://github.com/serenadeai/tree-sitter-scss"},
			source: "TS_PUBLIC const TSLanguage * tree_sitter_scss(  void  ) { return 0; }\n",
			want:   "tree_sitter_scss",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "parser.c")
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := staticCLanguageSymbol(tc.entry, path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("symbol=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestStaticCLanguageSymbolRejectsParameterizedNearMisses(t *testing.T) {
	entry := parityLockEntry{Name: "scss", RepoURL: "https://github.com/serenadeai/tree-sitter-scss"}
	for _, params := range []string{"int state", "void *payload", "void, int"} {
		t.Run(strings.NewReplacer(" ", "_", "*", "ptr", ",", "comma").Replace(params), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "parser.c")
			source := "TS_PUBLIC const TSLanguage *tree_sitter_scss(" + params + ") { return 0; }\n"
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := staticCLanguageSymbol(entry, path); err == nil {
				t.Fatalf("near-miss parameter list admitted as %q", got)
			}
		})
	}
}

func TestStaticCLanguageSymbolRejectsNonDefinitions(t *testing.T) {
	entry := parityLockEntry{Name: "scss", RepoURL: "https://github.com/serenadeai/tree-sitter-scss"}
	for _, tc := range []struct {
		name   string
		source string
	}{
		{name: "prototype", source: "TS_PUBLIC const TSLanguage *tree_sitter_scss();\n"},
		{name: "void_prototype", source: "const TSLanguage *tree_sitter_scss(void);\n"},
		{name: "line_comment", source: "// const TSLanguage *tree_sitter_scss() { return 0; }\n"},
		{name: "block_comment", source: "/* const TSLanguage *tree_sitter_scss(void) { return 0; } */\n"},
		{name: "string", source: "const char *s = \"const TSLanguage *tree_sitter_scss() {\";\n"},
		{name: "character_noise", source: "char c = '}'; /* tree_sitter_scss */\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "parser.c")
			if err := os.WriteFile(path, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := staticCLanguageSymbol(entry, path); err == nil {
				t.Fatalf("non-definition admitted as %q", got)
			}
		})
	}
}

func TestStaticCLanguageSymbolIgnoresFalseDefinitionBeforeRealOne(t *testing.T) {
	entry := parityLockEntry{Name: "scss", RepoURL: "https://github.com/serenadeai/tree-sitter-scss"}
	path := filepath.Join(t.TempDir(), "parser.c")
	source := "/* const TSLanguage *tree_sitter_fake() { } */\n" +
		"TS_PUBLIC const TSLanguage *tree_sitter_scss() { return 0; }\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := staticCLanguageSymbol(entry, path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "tree_sitter_scss" {
		t.Fatalf("symbol=%q want tree_sitter_scss", got)
	}
}
