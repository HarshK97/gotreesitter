# When you need an external scanner — and how to write one in Go

External scanners are the escape hatch tree-sitter grammars use when a
regular lexer cannot recognize a token. The grammar's `externals` array
names the tokens, and hand-written scanner code recognizes them with full
context. In upstream tree-sitter that code is `src/scanner.c`; in
gotreesitter it is a Go type that implements `gotreesitter.ExternalScanner`,
attached to `Language.ExternalScanner`.

This document is in two parts: first, deciding whether you need a scanner
at all (most grammars that think they do, don't); second, the Go porting
contract, including behavioral requirements this project learned the hard
way while chasing byte-exact parity with C tree-sitter.

Companion doc: [authoring-languages.md](authoring-languages.md) for the
grammar → blob → runtime pipeline itself.

## Part 1 — the decision

### You do NOT need a scanner for

- **Keyword versus identifier.** Declare a `word` token (`"word": "identifier"`
  in grammar.json, `g.SetWord("identifier")` in the DSL) and write keywords
  as plain strings. Keyword extraction handles the overlap.
- **Precedence and associativity.** `prec`, `prec.left`, `prec.right`,
  `prec.dynamic` (DSL: `Prec`, `PrecLeft`, `PrecRight`, `PrecDynamic`)
  resolve expression-grammar ambiguity in the tables. Dangling-else,
  cast-vs-call, and operator towers all stay in-grammar.
- **Comments and whitespace.** These are `extras`. gotreesitter compiles a
  whitespace pattern extra into DFA skip transitions and shifts visible
  extras (like `comment`) anywhere.
- **Plain strings and numbers.** `token(seq('"', /[^"]*/, '"'))` and similar
  patterns cover these. A string with a fixed delimiter and escape rules is
  regular.
- **C-style preprocessors.** tree-sitter-c parses `#if`/`#define`/`#include`
  entirely in-grammar: directives are line-oriented rules, and `\\\n`
  splices are part of token patterns. Having a preprocessor is not, by
  itself, a reason to write a scanner — Pawn needed scanner help only for
  two *recovery* cases, covered below.

If your grammar is in one of these buckets, stop here: every scanner you
don't write is corpus parity you don't have to re-verify.

### You DO need a scanner for

Write a scanner for context-sensitive or unbounded-lookahead lexing — cases
where the identity of the next token depends on arbitrary distance or on
state the LR automaton cannot carry:

- **Indentation blocks** (Python, YAML): a stack of indent widths compared
  on each newline. This needs serialized mutable state.
- **Heredocs** (bash, Perl, Ruby): the parser chooses the closing delimiter
  at open time and matches it arbitrarily later.
- **Nested string interpolation** (Elixir, JavaScript templates): string
  content tokens whose validity depends on interpolation depth.
- **Delimiter-matched raw strings** (Rust `r###"..."###`, C# raw strings):
  count the delimiter at open, then match the same count at close.
- **Newline significance / optional semicolons**: Go's automatic-semicolon
  rule and Pawn's "lax mode" (statements may end at the line break) are the
  canonical cases. The terminator token exists only when the *next* line
  does not continue the statement — pure lookahead-past-the-token territory.
- **Contextual token disambiguation**: Pawn's `public OnFoo(<Bar>)` callback
  signature opener `<` versus relational `i < 10` uses the same character
  for a different token, decided by what follows.
- **Zero-width / sentinel tokens**: these tokens consume nothing but tell
  the parser "a block ended here" (implicit end tags in HTML, dedents, the
  terminators below).
- **Bounded recovery for messy constructs**: consume a known-unparseable
  region as one opaque token so the rest of the file parses. Pawn's
  `#define` header recovery works this way.

### Worked case study: Pawn's five externals

pawnkit's tree-sitter-pawn declares exactly five external tokens
(`grammar.js` `externals`), with a deliberately narrow, **stateless**
`scanner.c` — a good first port, because there is no serialization to get
wrong:

| Token | What it does | Why the lexer can't |
|---|---|---|
| `_callback_signature_start` | Recognizes `<` as the opener of a callback signature. Consumes `<`, marks the token end there, then *looks ahead without consuming*: optional whitespace, then `>`, `_`, `%ident`, or an identifier, then `>`, and — when there was content — requires that the line does not immediately end (EOF, `\n`, `;`, or a comment running to end-of-line all reject). | Distinguishing signature `<` from relational `<` needs lookahead through an identifier, whitespace, comments, and the following construct. |
| `_statement_line_terminator` | Pawn's lax mode: a statement ends at a newline. Skips horizontal whitespace, comments, and `\`-newline line splices first; at EOF, terminates; at `\n`, consumes it, marks the end, then keeps scanning forward — if the next non-trivia character is `{` or a continuation character (`(`, `[`, `.`, `,`, `?`, `:`, operators…), it rejects, because the statement continues. | The token's existence depends on the first meaningful character of the *next* line. Classic unbounded lookahead. |
| `_directive_line_terminator` | Ends a preprocessor directive: skip spaces/tabs/CR; EOF terminates; `\n` is consumed and the end marked after it. | Line-oriented terminator shared by all directives; kept external so it composes with the two recovery tokens. |
| `_unsupported_define_header` | Bounded recovery: when a `#define` header has a shape the grammar doesn't model, consume it byte-by-byte up to whitespace, `/`, or end-of-line, marking the end as it goes, and hand the parser one opaque token. | Recovery: without it, an exotic `#define` shatters into a cascade of ERROR nodes. |
| `_unsupported_macro_parameter_list` | Same idea for macro parameter lists: consume a balanced-paren region (tracking nesting depth, and quoted strings with escapes) until the matching close paren or end-of-line, and only succeed when it actually saw something unsupported. | Balanced-delimiter matching with embedded strings is beyond regular lexing; doing it in the parser would poison the grammar with junk rules. |

Note the shape: two disambiguation/terminator tokens driven by lookahead,
one line-terminator, and two recovery tokens. Zero bytes of persistent
state — the C `create`/`serialize` functions are no-ops. Keep your
externals list this honest.

## Part 2 — porting a scanner to Go

### The interface

From `language.go`:

```go
type ExternalScanner interface {
    Create() any
    Destroy(payload any)
    Serialize(payload any, buf []byte) int
    Deserialize(payload any, buf []byte)
    Scan(payload any, lexer *ExternalLexer, validSymbols []bool) bool
}
```

Here is the lifecycle, as the runtime actually drives it
(`parser_dfa_token_source.go`):

- The runtime calls `Create` when it initializes a token source for a
  parse; the returned payload is passed to every other method. Use a
  pointer to a concrete struct; scanners conventionally panic on a wrong
  payload type.
- The runtime calls `Destroy` when it closes or resets the token source.
- `Serialize(payload, buf) int` writes scanner state into `buf` and returns
  the byte count. The runtime hands you a 4096-byte buffer
  (`externalScannerSerializationBufferSize`) — larger than C's 1024 — and
  snapshots state around retries and, when checkpoints are enabled, **after
  external tokens**. Keep `Serialize` allocation-free and cheap.
  `Deserialize` restores from a snapshot, which may be empty; treat an
  empty snapshot as a reset to initial state.
- `Scan` returns true if it recognized a token. On true, the runtime reads
  the result from the lexer (symbol + span). On false, the runtime
  discards position effects (it does not discard state effects — see
  `FailurePreservingExternalScanner` below).

### The dual numbering contract (differs from C!)

Two different number spaces appear in `Scan`, and they are not the same
space — unlike C, where `result_symbol` takes the same enum used to index
`valid_symbols`:

- `validSymbols[i]` is indexed by **external token index**: `i` is the
  position in the grammar's `externals` array, which is also the index
  into `Language.ExternalSymbols` (grammargen preserves this order —
  `registerExternalSymbols` in `grammargen/normalize.go` walks
  `Grammar.Externals` in order).
- `ExternalLexer.SetResultSymbol(sym Symbol)` takes the **language symbol
  ID** — that is, `lang.ExternalSymbols[i]`, not `i`.

Every scanner in this repo therefore keeps two constant sets (see
`grammars/svelte_scanner.go` for the pattern): token indexes for gating,
and symbol IDs for results. Resolve the symbols from the Language at
construction time, rather than hard-coding them.

### The lexer API

From `external_lexer.go`, with C equivalents:

| Go | C | Semantics |
|---|---|---|
| `Lookahead() rune` | `lexer->lookahead` | Current rune; `0` at EOF. |
| `Advance(skip bool)` | `lexer->advance(lexer, skip)` | Consume one rune. `skip=true` moves the token *start* forward (whitespace exclusion) and — exactly like C — does **not** move the token end; `MarkEnd` is the only way to set the end. |
| `MarkEnd()` | `lexer->mark_end(lexer)` | Set token end = current position. |
| `SetResultSymbol(sym Symbol)` | `lexer->result_symbol = ...` | See numbering contract above. |
| `Column() uint32` | `lexer->get_column(lexer)` | 0-based column at the cursor. |
| `HasPreviousBytes(text string) bool` | (no C equivalent) | True if the bytes immediately before the cursor equal `text`; used to guard content tokens when merged parser states expose them too broadly. |
| `AdvanceSpaces(skip bool) int`, `AdvanceUntilNewline(skip bool) int` | (helpers) | Bulk equivalents of repeated `Advance` for ASCII-space runs / to-end-of-line runs. |

You must not reinvent these span rules — they mirror C `ts_lexer` exactly,
and the comments in `external_lexer.go` document why:

- If `Scan` returns true and you never called `MarkEnd`, the end defaults
  to the current cursor, including cursor movement from `Advance(true)`.
- If you `MarkEnd` and then `Advance(true)` past the mark, the token
  becomes **zero-width at the mark**, and the parser re-positions there,
  so it lexes the skipped bytes again on the next call. This is how
  YAML-style and terminator tokens work; it is deliberate, not a bug.

### A faithful Go port of Pawn's scanner (condensed)

```go
package pawn

import gts "github.com/odvcencio/gotreesitter"

// External token indexes: order of grammar.json "externals".
const (
    tokCallbackSignatureStart = iota
    tokStatementLineTerminator
    tokDirectiveLineTerminator
    tokUnsupportedDefineHeader
    tokUnsupportedMacroParameterList
    tokCount
)

// PawnScanner is stateless, like upstream scanner.c.
type PawnScanner struct {
    syms [tokCount]gts.Symbol // external index -> language symbol ID
}

func NewPawnScanner(lang *gts.Language) *PawnScanner {
    s := &PawnScanner{}
    copy(s.syms[:], lang.ExternalSymbols)
    return s
}

func (s *PawnScanner) Create() any                           { return nil }
func (s *PawnScanner) Destroy(any)                           {}
func (s *PawnScanner) Serialize(payload any, buf []byte) int { return 0 }
func (s *PawnScanner) Deserialize(payload any, buf []byte)   {}

// Stateless scanners can safely opt into both fast paths.
func (s *PawnScanner) SupportsIncrementalReuse() bool    { return true }
func (s *PawnScanner) PreservesStateOnScanFailure() bool { return true }

func (s *PawnScanner) Scan(payload any, lx *gts.ExternalLexer, valid []bool) bool {
    if valid[tokCallbackSignatureStart] && scanCallbackSignatureStart(lx) {
        lx.SetResultSymbol(s.syms[tokCallbackSignatureStart])
        return true
    }
    if valid[tokStatementLineTerminator] && scanStatementLineTerminator(lx) {
        lx.SetResultSymbol(s.syms[tokStatementLineTerminator])
        return true
    }
    if valid[tokDirectiveLineTerminator] && scanDirectiveLineTerminator(lx) {
        lx.SetResultSymbol(s.syms[tokDirectiveLineTerminator])
        return true
    }
    // ... the two recovery tokens follow the same shape ...
    return false
}

// scanDirectiveLineTerminator: skip spaces/tabs/CR; EOF terminates; a
// newline is consumed (as skip) and the end marked after it — a zero-width
// terminator, exactly like the C original.
func scanDirectiveLineTerminator(lx *gts.ExternalLexer) bool {
    for lx.Lookahead() == ' ' || lx.Lookahead() == '\t' || lx.Lookahead() == '\r' {
        lx.Advance(true)
    }
    if lx.Lookahead() == 0 { // EOF: see contract (b) below
        lx.MarkEnd()
        return true
    }
    if lx.Lookahead() != '\n' {
        return false
    }
    lx.Advance(true)
    lx.MarkEnd()
    return true
}

// scanCallbackSignatureStart shows the mark-then-look-ahead idiom: the token
// is exactly "<"; everything after the MarkEnd is validation only.
func scanCallbackSignatureStart(lx *gts.ExternalLexer) bool {
    if lx.Lookahead() != '<' {
        return false
    }
    lx.Advance(false)
    lx.MarkEnd()
    // ... whitespace-skip, then accept ">", "_", "%ident", or an identifier,
    // then ">", then reject if the line effectively ends (EOF/'\n'/';'/line
    // comment) — consuming freely, because the mark already froze the span.
    // Port the C control flow 1:1; do not "simplify" the reject conditions.
    ...
    return true
}
```

Gate every branch on `valid[...]`. The runtime computes `validSymbols` from
the grammar tables — for grammargen-built languages, through the
`Language.ExternalLexStates` table (built automatically when the grammar
has externals; it mirrors C's `ts_external_scanner_states`), unioned
across all live GLR stacks when the parse has forked (`SetGLRStates`).
Returning a symbol that is not valid in the current state is
undefined-behavior territory: at best it gets pruned, at worst it triggers
an error cascade.

### Wiring the scanner to the Language

The simplest way, registry-free, is to assign the public field:

```go
lang, err := gts.LoadLanguage(pawnBlob)
...
lang.ExternalScanner = NewPawnScanner(lang)
```

If you distribute through the `grammars` registry
(`grammars.RegisterExternalScanner(name, s)` +
`grammars.RegisterExternalLexStates(name, states)`, then
`grammars.LoadLanguage(name, blob)` / `grammars.AttachLanguageSupport`),
note one caveat verified against `grammars/embedded_loader.go`: for a
language whose name has **no embedded reference blob in this repo**, the
attach path (`AdaptScannerForLanguage`) can bind your scanner only if it
implements

```go
ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner
```

(the `languageBoundExternalScanner` hook). Without it, the adapter tries to
load the in-repo reference blob to remap symbol IDs, and for an
out-of-tree name that lookup panics. Implement the hook (return
`NewPawnScanner(lang)`) or skip the registry and assign the field. If you
need to move a scanner between two Languages with different symbol
numbering, use the public remapper:
`gotreesitter.AdaptExternalScannerByExternalOrder(sourceLang, targetLang)`.

### Optional capability interfaces

- `IncrementalReuseExternalScanner` (`SupportsIncrementalReuse() bool`):
  declare true only if reusing subtrees across edits is safe for your
  state. Stateless scanners: yes. Returning false is an explicit opt-out;
  not implementing the interface is also treated as uncertified and fails
  closed. Python, Mojo, and Starlark currently return false.
- `FailurePreservingExternalScanner` (`PreservesStateOnScanFailure() bool`):
  declare true if `Scan` returning false never mutated the payload — this
  lets the runtime skip defensive state snapshots on the hot path.

### Incremental-reuse certification matrix

This matrix records the production contract for every external scanner in
the built-in registry. It is derived from the registration files under
`grammars/z_subset_scanner_register_*.go` and the runtime gate in
`languageSupportsIncrementalReuse`:

- **certified reuse** means the registered scanner implements
  `IncrementalReuseExternalScanner` and returns true. A changed edit may
  enter old-tree subtree reuse, subject to the normal dirty-span,
  fragility, byte-equality, and scanner-boundary gates.
- **bounded reuse** is Dart's certified route for sources up to 256 KiB.
  Larger Dart sources use the fresh production parse with reason
  `dart_large_external_scanner_unsupported`.
- **fallback (explicit opt-out)** means the scanner implements the
  interface and returns false.
- **fallback (uncertified)** means the scanner does not implement the
  interface. For either fallback status, once the preliminary
  token-invariant leaf fast path declines, `ParseIncremental` ignores the
  old tree and runs the same production full-parse and retry path as
  `Parse`. `ParseIncrementalProfiled` reports `ReuseUnsupported=true`,
  `ReuseUnsupportedReason="external_scanner_unsupported"`, no old-tree
  reuse route, and no reused bytes or subtrees.

The token-invariant leaf exception is intentionally narrow: on a production
tree, a same-length edit that independently reauthenticates the same leaf
token may return before the scanner gate. That is not certification for
general scanner-dependent subtree reuse. Likewise, scanner checkpoints do
not certify a scanner by themselves: CMake and Svelte both opt in and use
checkpoints, while Python, Mojo, and Starlark use checkpoints but explicitly
opt out. Custom `TokenSource` implementations have their own
`IncrementalReuseTokenSource` contract and are outside this registry matrix.
In particular, the registered SQL, HTML, Markdown, and Markdown Inline
scanners are currently uncertified, so changed-token or shape-changing edits
take the production full-parse fallback rather than scanner-dependent reuse.

| Language | Changed-edit contract |
|---|---|
| `agda` | fallback (uncertified) |
| `angular` | fallback (uncertified) |
| `arduino` | fallback (uncertified) |
| `astro` | fallback (uncertified) |
| `awk` | fallback (uncertified) |
| `bash` | fallback (uncertified) |
| `beancount` | fallback (uncertified) |
| `bicep` | fallback (uncertified) |
| `bitbake` | fallback (uncertified) |
| `blade` | fallback (uncertified) |
| `c_sharp` | fallback (uncertified) |
| `caddy` | fallback (uncertified) |
| `cairo` | fallback (uncertified) |
| `cmake` | certified reuse |
| `cobol` | fallback (uncertified) |
| `comment` | fallback (uncertified) |
| `cooklang` | fallback (uncertified) |
| `cpp` | fallback (uncertified) |
| `crystal` | fallback (uncertified) |
| `css` | certified reuse |
| `cuda` | fallback (uncertified) |
| `cue` | fallback (uncertified) |
| `d` | fallback (uncertified) |
| `dart` | bounded reuse (up to 256 KiB) |
| `dhall` | fallback (uncertified) |
| `disassembly` | fallback (uncertified) |
| `djot` | fallback (uncertified) |
| `dockerfile` | fallback (uncertified) |
| `doxygen` | fallback (uncertified) |
| `dtd` | fallback (uncertified) |
| `earthfile` | fallback (uncertified) |
| `editorconfig` | fallback (uncertified) |
| `elixir` | fallback (uncertified) |
| `elm` | fallback (uncertified) |
| `erlang` | fallback (uncertified) |
| `fennel` | fallback (uncertified) |
| `firrtl` | fallback (uncertified) |
| `fish` | fallback (uncertified) |
| `foam` | fallback (uncertified) |
| `fortran` | fallback (uncertified) |
| `fsharp` | fallback (uncertified) |
| `gdscript` | fallback (uncertified) |
| `gitcommit` | fallback (uncertified) |
| `gleam` | fallback (uncertified) |
| `gn` | fallback (uncertified) |
| `go` | certified reuse |
| `godot_resource` | fallback (uncertified) |
| `hack` | fallback (uncertified) |
| `haskell` | fallback (uncertified) |
| `haxe` | fallback (uncertified) |
| `hcl` | fallback (uncertified) |
| `hlsl` | fallback (uncertified) |
| `html` | fallback (uncertified) |
| `janet` | fallback (uncertified) |
| `javascript` | certified reuse |
| `jsdoc` | fallback (uncertified) |
| `jsonnet` | fallback (uncertified) |
| `julia` | fallback (uncertified) |
| `just` | fallback (uncertified) |
| `kconfig` | fallback (uncertified) |
| `kdl` | fallback (uncertified) |
| `kotlin` | fallback (uncertified) |
| `less` | fallback (uncertified) |
| `liquid` | fallback (uncertified) |
| `lua` | fallback (uncertified) |
| `luau` | fallback (uncertified) |
| `markdown` | fallback (uncertified) |
| `markdown_inline` | fallback (uncertified) |
| `matlab` | fallback (uncertified) |
| `mojo` | fallback (explicit opt-out) |
| `move` | fallback (uncertified) |
| `nginx` | fallback (uncertified) |
| `nickel` | fallback (uncertified) |
| `nim` | fallback (uncertified) |
| `nix` | fallback (uncertified) |
| `norg` | fallback (uncertified) |
| `nushell` | fallback (uncertified) |
| `ocaml` | fallback (uncertified) |
| `odin` | fallback (uncertified) |
| `org` | fallback (uncertified) |
| `perl` | fallback (uncertified) |
| `php` | fallback (uncertified) |
| `pkl` | fallback (uncertified) |
| `powershell` | fallback (uncertified) |
| `properties` | fallback (uncertified) |
| `pug` | fallback (uncertified) |
| `purescript` | fallback (uncertified) |
| `python` | fallback (explicit opt-out) |
| `r` | fallback (uncertified) |
| `racket` | fallback (uncertified) |
| `rescript` | fallback (uncertified) |
| `ron` | fallback (uncertified) |
| `rst` | fallback (uncertified) |
| `ruby` | fallback (uncertified) |
| `rust` | fallback (uncertified) |
| `scala` | fallback (uncertified) |
| `scss` | certified reuse |
| `sql` | fallback (uncertified) |
| `squirrel` | fallback (uncertified) |
| `starlark` | fallback (explicit opt-out) |
| `svelte` | certified reuse |
| `swift` | fallback (uncertified) |
| `tablegen` | fallback (uncertified) |
| `tcl` | fallback (uncertified) |
| `teal` | fallback (uncertified) |
| `templ` | fallback (uncertified) |
| `tlaplus` | fallback (uncertified) |
| `toml` | certified reuse |
| `tsx` | certified reuse |
| `typescript` | certified reuse |
| `typst` | fallback (uncertified) |
| `uxntal` | fallback (uncertified) |
| `vhdl` | fallback (uncertified) |
| `vue` | fallback (uncertified) |
| `wgsl` | fallback (uncertified) |
| `wolfram` | fallback (uncertified) |
| `xml` | fallback (uncertified) |
| `yaml` | fallback (uncertified) |
| `yuck` | fallback (uncertified) |

## Hard-learned behavioral contracts

These four contracts came out of this project's C-parity work. They apply
mainly when you implement a **full custom `TokenSource`** (parser_api.go:
`Next() Token`, returning a zero-`Symbol` token at EOF) instead of, or in
addition to, an external scanner — but (a) and (b) also affect scanner
authors.

**(a) Emit extras; never skip silently.** If the grammar declares
whitespace as an extra token, your token source must *emit* it as that
extra symbol, not swallow it. Here is a real incident: a hand-written
token source skipped horizontal whitespace instead of emitting the
grammar's `_whitespace` extra. C shifts whitespace extras, which advances
the parse position, so when error recovery re-lexed, the Go anchor sat one
byte behind C's and the ERROR spans diverged. The fix and its rationale
are preserved as a comment in `grammars/authzed_lexer.go` ("Emit it the
same way instead of silently skipping, so error recovery re-lexes at the
true content byte"). For external scanners, the same rule applies
differently: use `Advance(skip=true)` only for bytes the grammar genuinely
treats as skippable in that context, and remember that skip never moves
the marked end.

**(b) EOF must mirror C: no accept at EOF without a matched token.** At end
of input with nothing matched, return the zero-`Symbol` EOF token at the
EOF position (`lexer.go` does exactly this). Do not promote a partial
match that never reached an accepting state, and do not fabricate a
terminator the grammar didn't ask for. This repo shipped a fix titled
"mirror C tree-sitter behavior for EOF without accept," because getting it
wrong flips end-of-file reductions and changes the last node of every
tree. For external scanners: `Lookahead() == 0` means EOF, and returning
true there is correct only for genuine zero-width EOF-terminator tokens
(Pawn's terminators, dedents), with `MarkEnd` placed deliberately.

**(c) Error-mode lexing.** C's `ts_parser__lex` re-lexes at the recovery
frontier with the most permissive lex mode — `LexModes[0]`, the
ERROR_STATE mode — and the faithful C recovery port expects the same:
after `SetParserState(0)`, tokens should carry error-mode identity. The
built-in DFA token source honors this. The runtime discovers the
capability through the `errorModeLexingTokenSource` interface in
`parser_api.go` (`lexesErrorModeAtErrorState() bool`), but state this
honestly: that method name is unexported, so **an out-of-tree token source
cannot currently declare the capability**, even if it implements the
behavior (Go's unexported interface methods are package-scoped). Here is
what happens instead (`parser_recover_c.go`,
`cRecoverCustomSourceEligibleFor`): if your source supports `SkipToByte`,
the grammar has usable lex tables, **and it has no external
scanner/symbols**, the engine substitutes its own DFA in error mode and
resyncs your source afterward. Otherwise recovery decisions can diverge
from C on error-bearing inputs. Until the marker is exported, implement
`SetParserState(0)` to mean most-permissive lexing anyway (it is the
correct behavior), support `SkipToByte`, and test error inputs against
the C oracle rather than assuming they behave correctly.

**(d) Parser-state plumbing for TokenSource implementers.** The parser
feeds context through optional, structurally matched methods (all
exported names, so out-of-tree types satisfy them):

- `SetParserState(state StateID)` — the runtime calls this before lexing
  each token with the primary live stack's state; state selects the lex
  mode and, for scanners, the `ExternalLexStates` row. State 0 is the
  error state (see (c)).
- `SetGLRStates(states []StateID)` — when multiple GLR stacks are live,
  this carries the full set of stack-top states. Compute external-token
  validity as the **union** across them — this is exactly what the
  built-in source does — then let the parser prune. The set clears (or
  narrows to a single state) when the fork collapses.
- `SkipToByte(offset uint32) Token` / `SkipToByteWithPoint(offset uint32,
  pt Point) Token` — jump to a byte offset and return the first token at
  or after it. Incremental subtree reuse requires this method
  (`IncrementalReuseTokenSource`, `SupportsIncrementalReuse() bool`), and
  recovery resync uses it too. It must be deterministic: skipping to
  offset N and then calling `Next` repeatedly must yield the same stream
  as `Next`-ing from the start past N.
- If the parse uses `Parser.SetIncludedRanges`, an included-range filter
  wraps your source and forwards `SetParserState`/`SetGLRStates`/
  `SkipToByte`/error-mode queries to you (`included_ranges.go`).
  Implement the methods on the base source, and the wrapper composes for
  free.

## Before you ship a scanner port

- [ ] Run your grammar's full corpus through both the C parser and the Go
      port, and compare S-expressions **byte-exact** — not "no errors,"
      exact. tree-sitter-pawn keeps its corpus under `test/corpus/`; treat
      that as the oracle.
- [ ] Test EOF edges specifically: a file ending exactly at your token,
      ending in whitespace, ending mid-construct, an empty file, and a
      file with only a BOM.
- [ ] Test **error inputs**, not just clean ones. Recovery is where (a),
      (b), and (c) show up, and it is the least-tested path in every port.
- [ ] If the scanner holds state: confirm `Serialize` → `Deserialize` →
      `Serialize` reaches a fixed point, and confirm the state fits 4096
      bytes at your worst nesting depth.
- [ ] If any token can be zero-width: confirm the parser makes progress on
      adversarial inputs (the runtime caps consecutive zero-width tokens,
      but hitting the cap means your validity gating is wrong).
- [ ] Gate every `Scan` branch on `validSymbols`; never return a symbol
      whose index was not valid.
