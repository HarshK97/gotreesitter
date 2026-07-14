# BENCH — canonical performance claims and how to reproduce them

This is the single authoritative page for gotreesitter performance claims.
Every number here is either pinned to a release receipt in
[CHANGELOG.md](CHANGELOG.md) or derived from the ratcheted fleet ledger in
[`cgo_harness/perf_scan`](cgo_harness/perf_scan/). Anything not on this page
is not a claim.

## The one-paragraph story

gotreesitter trades some raw full-parse speed for portability: pure Go, no
cgo, cross-compiles anywhere Go does (including `wasip1`), fully visible to
`go test -race`. Editor-style incremental workloads are where it is fast
outright — a no-edit reparse is nanoseconds and a one-byte edit is
microsecond-scale on the historical control, both zero-allocation. Full parses
are ratcheted against the C runtime language-by-language with explicit
caveats instead of averaged marketing numbers.

## Canonical benchmark status

The generated 500-function Go source is a historical straight-LR control. It
contains no imports, selectors, methods, types, comments, strings, or control
flow and never forks under the current parser. It remains useful for tracking
the incremental fast paths and single-stack regressions, but it is not a
representative full-parse headline.

The former **1.895x C** full-parse headline and its **29% materialization**
decomposition are withdrawn pending the locked real-code replacement below.
The old comparison also used different Go grammar artifacts: gotreesitter used
the project-locked 1,425-state/214-symbol grammar while the C benchmark used a
1,404-state/212-symbol grammar bundled by the old smacker binding.

Historical control results, retained as workload-specific receipts:

| Lane | Benchmark | Historical result |
|---|---|---|
| Full parse (materialized, straight LR) | `BenchmarkGoParseFullDFA` | 10.907 ms on the pinned quiet host |
| One-byte incremental edit | `BenchmarkGoParseIncrementalSingleByteEditDFA` | 649 ns/op, 0 allocs |
| No-edit reparse | `BenchmarkGoParseIncrementalNoEditDFA` | 2.43 ns/op, 0 allocs |

Reproduce the historical control:

```sh
GOMAXPROCS=1 go test . -run '^$' \
  -bench 'BenchmarkGoParseFullDFA|BenchmarkGoParseIncrementalSingleByteEditDFA|BenchmarkGoParseIncrementalNoEditDFA' \
  -benchmem -count=10 -benchtime=750ms
```

`BenchmarkGoParseCoreDFA` is a parser-loop diagnostic (no tree
materialization); its numbers are never quoted as full-parse numbers. See
the benchmark-integrity note below.

### Historical quiet-host receipt

The v0.24.1 audit withdrew the pre-correction full-parse headlines pending a
quiet-host rerun of the corrected public benchmark. First such receipt,
2026-07-12, main @ 04f75d15, Intel Xeon D-2141I @ 2.20 GHz (idle host,
`taskset -c 14`, `GOMAXPROCS=1`, `-count=10 -benchtime=750ms`), medians:

| Lane | ns/op | B/op | allocs/op |
|---|---|---|---|
| `BenchmarkGoParseFullDFA` | 12,245,000 | 1,527 | **9** |
| `BenchmarkGoParseIncrementalSingleByteEditDFA` | 1,976 | 0 | **0** |
| `BenchmarkGoParseIncrementalNoEditDFA` | 9.85 | 0 | **0** |
| `BenchmarkGoParseCoreDFA` (diagnostic) | 8,737,000 | 996 | 6 |

Wall-clock numbers are host-specific (this is a low-clock server part; do not
compare against dev-box history). The allocation counts remain valid for this
fixture. The full-minus-core decomposition is not generalized beyond this
straight-LR control.

### v0.27.0 combined receipt

Same host and pinned core, the historical full parse measured 10.907 ms and
the mismatched-grammar C baseline measured 5.756 ms. Their former 1.895x ratio
is recorded only to explain earlier releases; it is not a current claim.

### Withdrawn same-host C calibration

The old C baselines were run on the same host and workload, but through the
smacker binding and its different Go grammar. Same-host scheduling does not
repair an oracle-identity mismatch. These rows are historical only:

| Lane | pure Go | cgo binding (C) | Go / C |
|---|---|---|---|
| Full parse (materialized) | 12.25 ms | 5.72 ms | **2.14x** |
| One-byte incremental edit | 1.98 µs | 331 µs | **0.006x — 167x faster** |
| No-edit reparse | 9.9 ns | 330 µs | **~33,000x faster** |

`BenchmarkGoParseFull` is also not the registry production route: built-in Go
uses the generated DFA path, while the hand-written Go token source remains an
explicit alternate. Its old 1.70x row is therefore withdrawn as both
oracle-mismatched and mislabeled.

## The one C oracle

Every new "versus C" claim names and fingerprints the same oracle inputs:

- upstream tree-sitter runtime v0.25.1, commit
  `f5afe475deb7c0bae6407fb776c76824f717bb61`;
- `github.com/tree-sitter/go-tree-sitter v0.25.0` wrapper commit
  `adc13ffd8b2c0b01b878fda9f7c422ce0df5fad3` for in-process parity;
- tree-sitter-go commit
  `2346a3ab1bb3857b48b29d779a1ef9799a248cd7`, from
  `grammars/languages.lock`;
- C runtime and grammar compiled with `-O2` into the static publication
  artifact, with compiler identity, flags, and artifact SHA-256 in the receipt.

The in-process cgo parity transport and the static throughput artifact must
consume those same runtime and grammar sources. The former
`treesitter_c_bench`/`treesitter_c_parity` binding split is a harness defect,
not an accepted source of two oracles.

## Canonical real-Go full-parse matrix

The replacement headline uses immutable snapshots of clean, human-authored Go
that exercise genuine GLR forking:

| Fixture | Bytes | SHA-256 |
|---|---:|---|
| `rewrite.go` | 5,116 | `74c0705f8729670559492fb5460a01b2a1a2a109928e1aeb52736e485e8ff097` |
| `query_compile.go` | 20,168 | `b788ee19b0075f0b9b567a9f93ea657e715bc8a6a40a99d3ca5c761404e71894` |
| `language.go` | 41,387 | `009aa9fd5352c712f3839670c7df8a9b00ae878ee20dc88131a438b2d5edfd9a` |
| `grammargen/lr.go` | 235,626 | `a7e4a1a64b25a60aea36183b9d6d53dcd9240942cdb10e67a3cf9e6ce30f95b2` |

These reach 12-18 live stacks, thousands to tens of thousands of multi-stack
iterations, and constructed-to-selected-node ratios of 3.65-4.47 on the
admitting revision. Generated code is reported separately and never blended
into the human-code headline. A pinned external-project fixture will be added
to check repository self-reference.

### Diagnostic workload-regime receipt

Before the static publication artifact was built, a strict-admitted quiet run
used the exact locked Go grammar through the existing dynamic parity loader.
On `a5df0aa5`, ten 750 ms samples measured the synthetic at 1.0437x locked C
and the nearly size-matched `query_compile.go` at 2.8890x. The synthetic had one
stack and no forks or merges; the real file reached 12 stacks, 1,765 forks,
5,216 merges, and constructed 31,847 nodes for 7,524 selected. This proves the
workload-regime defect, not the final C ratio. Report SHA-256:
`c6de42e12724f72393162a0a50ecb8247f97312eaaff6cb5b093746b1206b4ab`.

## Go-vs-C fleet scoreboard (full parse, real corpora)

Source of truth: [`cgo_harness/perf_scan/perf_ratio_budgets.json`](cgo_harness/perf_scan/perf_ratio_budgets.json)
and the wave-3 ledger. 204 of 206 languages carry ratcheted budgets
(D and F# are held out pending memory RCA; every exclusion is named in the
[known-gap ledger](cgo_harness/perf_scan/wave3_sweep_status.md)).

Distribution of observed full-parse ratios (Go time / C time, largest-file
basis, as of the 2026-07-11 ledger):

| Bucket | Languages |
|---|---|
| ≤ 1x (Go at or faster than C) | 10 |
| 1–2x | 64 |
| 2–3x | 29 |
| > 3x | 101 |

Median observed ratio: **~3x**. The long tail (up to ~650x on `uxntal`) is
dominated by small-file DSL grammars where C finishes in microseconds and
the ratio is mostly fixed per-parse overhead, plus a small set of named
GLR-ambiguity cliffs. Ratios are budgets with caveats, not endorsements:
`green_with_caveat` rows record exactly what was measured and what was not.

Named large-file witnesses (tracked, not hidden): JavaScript
`poppler.js` (3.4 MB — exact parity inside a hard 2 GiB container at
1,708,712 KiB max RSS; full parse 3.50x C), TypeScript
`webworker.generated.d.ts`, Groovy `pleac11_15.groovy`, and the
generated-table class (Go `opGen.go` / `rewriteAMD64.go` and in-repo
witnesses).

## Memory receipts (the v0.24 → v0.26.1 campaign)

On the exact Poppler witness under a hard 2 GiB container:

- Retained post-GC heap: 862,803,056 → **409,862,040 bytes (−52.5%)**
  after final-tree arena compaction (v0.26.1).
- Node header: **144 → 104 bytes** via arena-backed field-metadata
  sidecars (v0.26.0).
- Bounded raw-shape reclamation: **−192 MiB** retained (v0.26.0).

Each claim shipped with exact Go/C S-expression and deep parity on the same
witness; memory wins that break the selected tree do not merge.

## Methodology (why these numbers can be trusted)

1. **Correctness and performance are separate gates.** Before timing, every
   fixture must be clean and full-span and must preserve exact symbol, byte and
   point ranges, named/extra/missing flags, field ownership, and child order
   against the locked C oracle. See `cgo_harness/`.
2. **Ratchets, not snapshots.** Perf budgets only tighten
   (`cmd/benchgate`, `perf_scan` hard zero-cliff gate); parity exemptions
   only shrink (currently zero).
3. **Benchmark identity fails closed.** Fixture bytes, runtime commit, grammar
   commit, compiler, flags, and C artifact hash are recorded. A mismatch aborts
   admission instead of silently producing another ratio.
4. **Lifecycle and warm state are symmetric.** Each backend receives an
   untimed warm parse; the timed lane includes parse, first root validation,
   and tree release/close. Cold construction/loading is measured separately.
5. **Benchmark integrity is audited.** A 2026-07-11 audit found the then-
   canonical full-parse benchmark silently ran a no-tree diagnostic path;
   the affected headline numbers (1.54 ms / 7 allocs and successors) were
   withdrawn. A 2026-07-14 audit then found that the replacement workload never
   forked and that its C lane used a different grammar. Those claims are also
   withdrawn rather than patched around.
6. **Quiet-host discipline.** Publication runs use `GOMAXPROCS=1`, a pinned
   core, paired ABBA samples, `-count=10`, `-benchtime=750ms`, and `-benchmem`.
   Contended-box measurements are smoke evidence only.

## Multi-workload tracking

```sh
go run ./cmd/benchmatrix --count 10   # bench_out/matrix.{json,md} + raw logs
```

The default matrix includes a bounded, warmed language-family full-parse
group reported in MB/s, plus the Go/editor lanes above.
