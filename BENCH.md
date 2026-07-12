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
sub-microsecond on the canonical workload, both zero-allocation. Full parses
are ratcheted against the C runtime language-by-language with explicit
caveats instead of averaged marketing numbers.

## Canonical microbenchmark trio (generated 500-function Go file)

| Lane | Benchmark | Pinned result | Receipt |
|---|---|---|---|
| Full parse (materialized) | `BenchmarkGoParseFullDFA` | 7.813 ms → **6.750 ms**, 100 → **30 allocs/op** | v0.26.0 |
| One-byte incremental edit | `BenchmarkGoParseIncrementalSingleByteEditDFA` | **649 ns/op**, 0 allocs | README history |
| No-edit reparse | `BenchmarkGoParseIncrementalNoEditDFA` | **2.43 ns/op**, 0 allocs | README history |

Reproduce:

```sh
GOMAXPROCS=1 go test . -run '^$' \
  -bench 'BenchmarkGoParseFullDFA|BenchmarkGoParseIncrementalSingleByteEditDFA|BenchmarkGoParseIncrementalNoEditDFA' \
  -benchmem -count=10 -benchtime=750ms
```

`BenchmarkGoParseCoreDFA` is a parser-loop diagnostic (no tree
materialization); its numbers are never quoted as full-parse numbers. See
the benchmark-integrity note below.

### Pinned quiet-host receipt (corrected benchmark)

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

Wall-clock numbers are host-specific (this is a low-clock server part; do
not compare against dev-box history). The hardware-independent figures are
the allocation counts — 9 allocs/op for a fully materialized parse, zero for
both incremental lanes — and the materialization attribution: full minus
core ≈ 3.5 ms ≈ 29% of full-parse time on this host.

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

1. **Correctness and performance are separate gates.** An optimization must
   preserve the exact selected full-span tree (byte-exact S-expression and
   deep parity against the cgo-backed C oracle) before its timing or memory
   result is considered. See `cgo_harness/`.
2. **Ratchets, not snapshots.** Perf budgets only tighten
   (`cmd/benchgate`, `perf_scan` hard zero-cliff gate); parity exemptions
   only shrink (currently zero).
3. **Benchmark integrity is audited.** A 2026-07-11 audit found the then-
   canonical full-parse benchmark silently ran a no-tree diagnostic path;
   the affected headline numbers (1.54 ms / 7 allocs and successors) were
   **withdrawn**, the benchmark was corrected to the public `Parser.Parse`
   path, and headlines wait for pinned quiet-host reruns (v0.24.1).
4. **Quiet-host discipline.** Contended-box measurements are recorded as
   smoke evidence only; ratchets move on quiet, reproducible, one-language
   runs.

## Multi-workload tracking

```sh
go run ./cmd/benchmatrix --count 10   # bench_out/matrix.{json,md} + raw logs
```

The default matrix includes a bounded, warmed language-family full-parse
group reported in MB/s, plus the Go/editor lanes above.
