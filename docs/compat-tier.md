# The C-faithful compat tier (`parser_result_*.go`)

If you clone this repository and list the root package, the first thing you
see is a wall of `parser_result_<language>*.go` files — currently about 177
files and about 72K lines. This page explains what that tier is, why it
lives where it lives, and why it shrinks over time.

## What it is

Post-parse, C-faithful **result normalization**. Two GLR parsers can both be
"correct" and still select different trees under ambiguity, error recovery,
or extra/trivia attachment. gotreesitter's parity program demands more than
correctness: it demands byte-exact agreement with the tree the C runtime
*selects*, including error-node shapes and recovered spans, verified
against the cgo-backed oracle in `cgo_harness/`.

Where the core engine does not yet reproduce a C selection behavior through
a general mechanism, a bounded normalization pass reshapes the raw parse
result after the fact. Examples: JavaScript ASI and trailing-comment
attachment, statement-keyword shapes shared by JS/TS, Python repair shapes,
recovered-tree normalizations for Doxygen/JSDoc, and COBOL `EXEC CICS` span
trimming.

Each pass is:

- **language-scoped** — `parser_result_<language>*.go`, greppable as a tier;
- **oracle-gated** — every pass exists because a named witness diverged
  from C, and parity fixtures pin it, failing the build if it drifts;
- **internal** — passes run on unexported arena/node internals before the
  tree is returned. They are plumbing, not API. This is also why the tier
  lives in the root package: it needs `nodeArena`, `stackEntry`, and
  friends, and exporting those to move the files into a subpackage would
  trade cosmetic layout for a worse public surface.

## Why it shrinks

The tier is scaffolding for the parity ratchet, not a destination. The
stated engine rule (see the README roadmap) is: **add no public parse
variant and no parser-core language-name switch when a general mechanism
can express the need.** As general mechanisms land — certified
conflict-resolution policies, blob-pinned runtime profiles, non-terminal
alias maps, the global repetition fold — the tier deletes the shims those
mechanisms subsume:

- v0.24.1 removed fourteen retired language-specific repetition/conflict
  dispatch helpers and their dead JavaScript/TypeScript/Java closure.
- v0.25.0 removed the retired `no_alias` reduction-attribution lane end to
  end.
- The Python normalization cluster is queued for the same closure (its
  remaining helpers are referenced only by their own tests).

The honest trajectory is: shim count rises while a language's parity
closes, then falls as the behavior moves into certified engine mechanisms.
The 206/206 exhaustive-parity milestone (v0.23.0) is what makes the
deletions safe — every removal must keep the zero-exemption parity gate
green.

## Reading the tier

| Convention | Meaning |
|---|---|
| `parser_result_<lang>.go` | primary normalization passes for a language |
| `parser_result_<lang>_*_test.go` | witness fixtures pinning each pass |
| `normalize<Lang>Compatibility(...)` | per-language entry point |
| `parser_result.go` | shared dispatch into per-language entry points |

Largest groups today (file count): C# (14), Rust (6), Go (5), Python (4),
JavaScript/TypeScript (4+), Swift (3), Scala (3), PowerShell (3),
Kotlin (3), Haskell (3).
