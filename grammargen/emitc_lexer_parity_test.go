package grammargen

// C-runtime parity tests for grammargen's EmitC lexer emission.
//
// These compile the emitted parser.c against a real tree-sitter C runtime
// (github.com/tree-sitter/go-tree-sitter v0.25.0 — the runtime gotreesitter's
// own C-oracle parity work verifies against), parse an input with the C
// runtime, and byte-compare its named-node S-expression against the pure-Go
// parse of the SAME generated tables. The pure-Go parser is the oracle: any
// divergence here is a bug in EmitC, not in the grammar or tables.
//
// They target three EmitC defects the goala C-parity harness isolated:
//
//  1a. ts_lex failed to tokenize an anonymous keyword string that overlaps the
//      identifier character class: `let x` parsed as (ERROR) under C because
//      the whitespace skip-state accepted ts_builtin_sym_end (symbol 0 = EOF),
//      so the parser believed input ended right after `let`.
//  1b. ts_lex infinite-looped at true end-of-input for tokens whose DFA has a
//      transition range covering codepoint 0 (negated classes like [^"], `.`):
//      at EOF lookahead is 0, the range matched, and ADVANCE spun in place.
//  2.  Two distinct same-text anonymous tokens (plain `"` and
//      token.immediate('"')) both emitted as anon_sym_DQUOTE — a C
//      "redeclaration of enumerator".
//
// The harness skips gracefully when a C compiler or the runtime module is
// unavailable, so `go test ./grammargen/...` stays green in minimal
// environments.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gotreesitter"
)

const emitcWalkerMainC = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <tree_sitter/api.h>

const TSLanguage *LANG_FN(void);

// walk prints the S-expression of named nodes only, matching gotreesitter's
// Node.SExpr format exactly: "(" type ( " " child )* ")".
static void walk(TSNode n, FILE *out) {
  if (!ts_node_is_named(n)) return;
  fputc('(', out);
  fputs(ts_node_type(n), out);
  uint32_t c = ts_node_named_child_count(n);
  for (uint32_t i = 0; i < c; i++) {
    fputc(' ', out);
    walk(ts_node_named_child(n, i), out);
  }
  fputc(')', out);
}

int main(int argc, char **argv) {
  if (argc < 2) { fprintf(stderr, "usage: walker <file>\n"); return 2; }
  FILE *f = fopen(argv[1], "rb");
  if (!f) { fprintf(stderr, "open %s\n", argv[1]); return 2; }
  fseek(f, 0, SEEK_END); long sz = ftell(f); fseek(f, 0, SEEK_SET);
  char *buf = malloc(sz > 0 ? sz : 1);
  size_t got = fread(buf, 1, sz, f); fclose(f);
  TSParser *p = ts_parser_new();
  if (!ts_parser_set_language(p, LANG_FN())) { fprintf(stderr, "set_language\n"); return 3; }
  TSTree *t = ts_parser_parse_string(p, NULL, buf, (uint32_t)got);
  TSNode root = ts_tree_root_node(t);
  walk(root, stdout);
  fputc('\n', stdout);
  ts_tree_delete(t); ts_parser_delete(p); free(buf);
  return 0;
}
`

// cWalker compiles the language's EmitC output against the v0.25 runtime and
// returns a function that parses an input string and yields the C runtime's
// named-node S-expression. It skips the test when cc or the runtime module is
// unavailable, and fails (rather than hangs) when the C parser loops.
func cWalker(t *testing.T, name string, lang *gotreesitter.Language) func(t *testing.T, input string) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}",
		"github.com/tree-sitter/go-tree-sitter@v0.25.0").CombinedOutput()
	runtimeDir := strings.TrimSpace(string(out))
	if err != nil || runtimeDir == "" {
		t.Skipf("tree-sitter C runtime unavailable: %v\n%s", err, out)
	}

	parserC, err := EmitC(name, lang)
	if err != nil {
		t.Fatalf("EmitC: %v", err)
	}

	tmp := t.TempDir()
	incDir := filepath.Join(tmp, "include", "tree_sitter")
	if err := os.MkdirAll(incDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parserH, err := os.ReadFile(filepath.Join(runtimeDir, "src", "parser.h"))
	if err != nil {
		t.Fatalf("read runtime parser.h: %v", err)
	}
	mustWrite(t, filepath.Join(incDir, "parser.h"), parserH)
	parserPath := filepath.Join(tmp, "parser.c")
	mustWrite(t, parserPath, []byte(parserC))
	mainC := strings.ReplaceAll(emitcWalkerMainC, "LANG_FN", "tree_sitter_"+name)
	mainPath := filepath.Join(tmp, "walker.c")
	mustWrite(t, mainPath, []byte(mainC))

	compile, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	objParser := filepath.Join(tmp, "parser.o")
	objLib := filepath.Join(tmp, "lib.o")
	objMain := filepath.Join(tmp, "walker.o")
	walker := filepath.Join(tmp, "walker")
	steps := [][]string{
		{"-std=c11", "-O0", "-D_DEFAULT_SOURCE",
			"-I" + filepath.Join(tmp, "include"), "-c", parserPath, "-o", objParser},
		{"-std=c11", "-O0", "-D_DEFAULT_SOURCE",
			"-I" + filepath.Join(runtimeDir, "include"), "-I" + filepath.Join(runtimeDir, "src"),
			"-c", filepath.Join(runtimeDir, "src", "lib.c"), "-o", objLib},
		{"-std=c11", "-O0", "-I" + filepath.Join(runtimeDir, "include"),
			"-c", mainPath, "-o", objMain},
		{objParser, objLib, objMain, "-pthread", "-o", walker},
	}
	for i, args := range steps {
		cmdOut, err := exec.CommandContext(compile, cc, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("compile step %d failed: %v\n%s", i, err, cmdOut)
		}
	}

	return func(t *testing.T, input string) string {
		t.Helper()
		srcPath := filepath.Join(t.TempDir(), "input")
		mustWrite(t, srcPath, []byte(input))
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		runOut, err := exec.CommandContext(ctx, walker, srcPath).Output()
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("C parser hung (>15s) on %q — EmitC lexer EOF loop", input)
		}
		if err != nil {
			t.Fatalf("run C walker on %q: %v", input, err)
		}
		return strings.TrimRight(string(runOut), "\n")
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func goSExpr(t *testing.T, lang *gotreesitter.Language, input string) string {
	t.Helper()
	tree, err := gotreesitter.NewParser(lang).Parse([]byte(input))
	if err != nil {
		t.Fatalf("pure-Go parse %q: %v", input, err)
	}
	return tree.RootNode().SExpr(lang)
}

// TestEmitCKeywordOverIdentifier is the regression for defect 1a: an anonymous
// keyword token (`let`) that overlaps the identifier class, plus 1b: EOF that
// does not end in an extra. Every input must byte-match pure-Go, and no input
// may hang (the walker has a hard timeout).
func TestEmitCKeywordOverIdentifier(t *testing.T) {
	g := NewGrammar("emitc_let")
	g.Define("source_file", Repeat(Sym("statement")))
	g.Define("statement", Seq(Str("let"), Sym("identifier")))
	g.Define("identifier", Pat(`[a-z]+`))
	g.SetExtras(Pat(`\s`))

	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage: %v", err)
	}
	parseC := cWalker(t, "emitc_let", lang)

	inputs := []string{
		"let x",   // 1a: keyword over identifier, EOF not an extra
		"let x\n", // trailing extra
		"let x let y",
		"let x\nlet y\n",
		"let letters", // identifier that has the keyword as a prefix
	}
	for _, in := range inputs {
		in := in
		t.Run(strings.ReplaceAll(in, "\n", "_"), func(t *testing.T) {
			want := goSExpr(t, lang, in)
			got := parseC(t, in)
			if got != want {
				t.Fatalf("C/Go divergence for %q\n C: %s\nGo: %s", in, got, want)
			}
			if strings.Contains(got, "ERROR") {
				t.Fatalf("unexpected ERROR for %q: %s", in, got)
			}
		})
	}
}

// TestEmitCNegatedClassEOFLoop is the focused regression for defect 1b: a token
// whose DFA has a transition range that includes codepoint 0 (a negated
// character class). At true EOF lookahead is 0; the pre-fix emission ADVANCEd
// in place forever. The walker's timeout turns a regression into a failure.
func TestEmitCNegatedClassEOFLoop(t *testing.T) {
	g := NewGrammar("emitc_str")
	g.Define("source_file", Repeat(Sym("string")))
	g.Define("string", Seq(Str(`"`), Repeat(Pat(`[^"]`)), Str(`"`)))
	g.SetExtras(Pat(`\s`))

	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage: %v", err)
	}
	parseC := cWalker(t, "emitc_str", lang)

	inputs := []string{
		`"hello"`, // complete string, EOF not an extra
		`"hello world"`,
		`"a" "b"`,
		`"unterminated`, // negated-class run hits true EOF — must not hang
	}
	for _, in := range inputs {
		in := in
		t.Run(in, func(t *testing.T) {
			want := goSExpr(t, lang, in)
			got := parseC(t, in)
			if got != want {
				t.Fatalf("C/Go divergence for %q\n C: %s\nGo: %s", in, got, want)
			}
		})
	}
}

// TestEmitCDuplicateAnonToken is the regression for defect 2: two distinct
// anonymous tokens with identical text must emit distinct C enumerators
// (anon_sym_DQUOTE / anon_sym_DQUOTE2) so the parser.c compiles.
func TestEmitCDuplicateAnonToken(t *testing.T) {
	g := NewGrammar("emitc_dupquote")
	// A plain `"` and an immediate `"` are distinct tokens with identical text.
	g.Define("source_file", Repeat(Sym("item")))
	g.Define("item", Seq(Str(`"`), Optional(Sym("inner"))))
	g.Define("inner", Seq(ImmToken(Str(`"`)), Sym("word")))
	g.Define("word", Pat(`[a-z]+`))
	g.SetExtras(Pat(`\s`))

	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage: %v", err)
	}

	code, err := EmitC("emitc_dupquote", lang)
	if err != nil {
		t.Fatalf("EmitC: %v", err)
	}
	// If two distinct same-text DQUOTE tokens exist, the second must be
	// disambiguated rather than redeclaring the first enumerator.
	if strings.Contains(code, "anon_sym_DQUOTE = ") {
		enumBlock := code[strings.Index(code, "enum ts_symbol_identifiers"):]
		enumBlock = enumBlock[:strings.Index(enumBlock, "};")]
		if strings.Count(enumBlock, "anon_sym_DQUOTE") >= 2 && !strings.Contains(enumBlock, "anon_sym_DQUOTE2 = ") {
			t.Fatalf("duplicate anon token not disambiguated:\n%s", enumBlock)
		}
	}

	// The decisive test: the C compiles without a
	// "redeclaration of enumerator" error.
	_ = cWalker(t, "emitc_dupquote", lang)
}

// TestEmitCAliasSequenceStride guards the alias-sequence stride defect: the
// tree-sitter runtime indexes ts_alias_sequences as a flat
// [PRODUCTION_ID_COUNT][MAX_ALIAS_SEQUENCE_LENGTH] array by structural child
// index for every production with a non-zero production_id. If
// MAX_ALIAS_SEQUENCE_LENGTH is sized to the longest alias row rather than the
// longest production, a long production's index runs past its own row into a
// later production's aliased entries and renders an unrelated child under a
// spurious alias. This grammar pairs a short aliased production with a long
// unaliased one; MAX_ALIAS_SEQUENCE_LENGTH must cover the long production, and
// the C parse must byte-match pure-Go.
func TestEmitCAliasSequenceStride(t *testing.T) {
	g := NewGrammar("emitc_stride")
	g.Define("source_file", Repeat(Choice(Sym("long_rule"), Sym("aliased"))))
	// A long production (7 structural children) with a field, so it carries a
	// non-zero production_id whose alias row is all zeros.
	g.Define("long_rule", Seq(
		Str("k"),
		Field("a", Sym("identifier")),
		Field("b", Sym("identifier")),
		Field("c", Sym("identifier")),
		Field("d", Sym("identifier")),
		Field("e", Sym("identifier")),
		Str(";"),
	))
	// A short production whose child 1 is aliased.
	g.Define("aliased", Seq(Str("`"), Alias(Sym("identifier"), "aliased_content", true), Str("`")))
	g.Define("identifier", Pat(`[a-z]+`))
	g.SetExtras(Pat(`\s`))

	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage: %v", err)
	}

	code, err := EmitC("emitc_stride", lang)
	if err != nil {
		t.Fatalf("EmitC: %v", err)
	}
	// The declared stride must cover the longest production, not the longest
	// alias row.
	stride := headerDefineInt(t, code, "MAX_ALIAS_SEQUENCE_LENGTH")
	maxChild := maxReduceChildCount(code)
	if stride < maxChild {
		t.Fatalf("MAX_ALIAS_SEQUENCE_LENGTH=%d < max reduce child_count=%d: long productions read past their alias row", stride, maxChild)
	}

	parseC := cWalker(t, "emitc_stride", lang)
	for _, in := range []string{"k a b c d e ;", "`x`", "k a b c d e ; `y` k p q r s t ;"} {
		in := in
		t.Run(strings.ReplaceAll(in, " ", "_"), func(t *testing.T) {
			want := goSExpr(t, lang, in)
			got := parseC(t, in)
			if got != want {
				t.Fatalf("C/Go divergence for %q\n C: %s\nGo: %s", in, got, want)
			}
		})
	}
}

func headerDefineInt(t *testing.T, code, name string) int {
	t.Helper()
	marker := "#define " + name + " "
	i := strings.Index(code, marker)
	if i < 0 {
		t.Fatalf("missing #define %s", name)
	}
	line := code[i+len(marker):]
	line = line[:strings.IndexByte(line, '\n')]
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("bad #define %s value %q: %v", name, line, err)
	}
	return n
}

// maxReduceChildCount scans emitted REDUCE(...) macros for the largest
// child-count argument (the second macro argument).
func maxReduceChildCount(code string) int {
	max := 0
	for i := 0; ; {
		j := strings.Index(code[i:], "REDUCE(")
		if j < 0 {
			break
		}
		i += j + len("REDUCE(")
		rest := code[i:]
		close := strings.IndexByte(rest, ')')
		if close < 0 {
			break
		}
		args := strings.Split(rest[:close], ",")
		if len(args) >= 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(args[1])); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}
