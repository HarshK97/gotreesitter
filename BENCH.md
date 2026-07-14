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
decomposition are withdrawn in favor of the locked real-code replacement
below. The old comparison also used different Go grammar artifacts:
gotreesitter used the project-locked 1,425-state/214-symbol grammar while the C
benchmark used a 1,404-state/212-symbol grammar bundled by the old smacker
binding.

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

Produce the complete publication receipt from a clean worktree on a quiet
Docker-capable host:

```sh
bash cgo_harness/pure_c/run_canonical_go_full_parse.sh --core <idle-cpu>
```

The driver authenticates and materializes the four snapshots, admits exact Go,
cgo, and static-C trees, builds the locked `-O2` oracle, and collects ten
process-isolated samples per backend and fixture in five Go-C-C-Go cycles. It
fails closed on dirty source, parser or Go runtime overrides, noisy-host
admission, identity drift, and incomplete receipts. Shortened or skipped gates
require `--diagnostic` and are labeled `NONPUBLICATION_DIAGNOSTIC`.

### First locked-oracle publication receipt

The first complete publication receipt was collected on 2026-07-14 from
`3ffad7778199a17270efe6791d09036242667233` on the pinned quiet host, using
core 7 and Go 1.22.2. All four Go, cgo, and static-C deep trees matched before
timing. Medians from ten process-isolated samples per backend and fixture:

| Fixture | Go median | static C median | Go / C | Go max RSS | C max RSS |
|---|---:|---:|---:|---:|---:|
| `query_compile.go` | 31.525 ms | 5.469 ms | **5.764366x** | 67,560 KiB | 2,816 KiB |
| `rewrite.go` | 5.556 ms | 1.197 ms | **4.639849x** | 56,344 KiB | 2,304 KiB |
| `language.go` | 30.106 ms | 5.809 ms | **5.182694x** | 69,136 KiB | 2,816 KiB |
| `grammargen/lr.go` | 376.938 ms | 57.867 ms | **6.513909x** | 192,560 KiB | 9,216 KiB |

The canonical equal-fixture geomean is **5.481673x C**. The fixed-suite sum of
medians is **6.313799x C**; it is reported separately because the largest file
dominates aggregate wall time. This is the baseline for future full-parse
claims, not a retrospective adjustment to the withdrawn straight-LR ratio.

Receipt identities:

- manifest SHA-256:
  `a850d60ec93c2ff3a49b3ccf9266f32421c04a87c5cb5ce807f2f4fe3f70c1cb`;
- report SHA-256:
  `36fded16ad1ffa34eab8d4f3e61d0a634bcc30e70237101825f73394d797c1b8`;
- complete receipt bundle SHA-256:
  `aecb7f76e3df4832877f4526280849257826f3d84f2f104379a57748f4f6f310`;
- static C artifact SHA-256:
  `dfbed45811491be8d81e32b293ed5577222445dae47b67d876cedae09679a871`.

### Diagnostic workload-regime receipt

Before the static publication artifact was built, a strict-admitted quiet run
used the exact locked Go grammar through the existing dynamic parity loader.
On `a5df0aa5`, ten 750 ms samples measured the synthetic at 1.0437x locked C
and the nearly size-matched `query_compile.go` at 2.8890x. The synthetic had one
stack and no forks or merges; the real file reached 12 stacks, 1,765 forks,
5,216 merges, and constructed 31,847 nodes for 7,524 selected. This proves the
workload-regime defect, not the final C ratio. Report SHA-256:
`c6de42e12724f72393162a0a50ecb8247f97312eaaff6cb5b093746b1206b4ab`.

### Authenticated work-count diagnostic harness

Wall time alone cannot distinguish excess GLR work from higher cost per event.
The diagnostic-only `gts-work-count/v2` contract therefore runs one fresh
public `query_compile.go` parse invocation through a tagged diagnostic Go test
binary and a fully static C binary built from the same locked runtime and
grammar as the publication oracle. The tagged parse is diagnostic, not a
production-path claim. It is not a timing lane, does not modify the production
static timing artifact, and emits no elapsed time. Every internal Go retry or
recovery reparse triggered by that single public invocation remains inside the
counter window.

Admission happens before instrumentation: a separate ordinary untagged Go
binary and the unmodified static C oracle must both reproduce the frozen
`gts-deep-tree-v1` digest. The tagged Go child independently authenticates the
fixture hash, grammar commit/blob, GLR workload regime, full clean span, and
deep tree before reporting counters. The C child must reproduce the same tree.
The complete unmodified-C cold build and admission runs in a dedicated,
wall-bounded process group, so repository acquisition, compiler/linker,
identity, `nm`, and `readelf` descendants are killed together on timeout.

The Go schema attributes counter deltas to every `parseInternal` attempt at
entry, after cap resolution, and during finalization. A logical retry rung is
reported separately from the resolved `retry_pass` mode. The aggregate must
equal the sum of attempt counters plus the explicitly reported outside-attempt
residual. On the frozen canonical fixture, `accept_actions=3` means three GLR
accept actions inside one `initial_full` attempt; it does not mean three parse
attempts. A content-addressed valid-Go retry witness pins the two-attempt
`initial_full` accepted-error to `initial_merge` accepted-clean sequence, and a
separate content-addressed straight-LR control pins the one-stack case.

`accept_actions` is a terminal-event counter, not a convergence counter. C can
accept once and then emit multiple packed roots during `pop_all`, while Go can
apply multiple accept actions inside one attempt. Contract v2 therefore
reserves, but does not yet implement, a bounded convergence-frontier record:
the first 256 events plus the first rejection for each reason, keyed by
attempt, lookahead, phase, head state/byte/status/error/scanner identity, with
before/after links and packed paths, cumulative counters, and separate
`accept_actions` and `packed_roots_emitted`. No current receipt makes a
convergence claim from the accept-action row.

An authoritative `gts-work-count-receipt/v3` requires a clean Git checkout and
records `HEAD`, `HEAD^{tree}`, and `git_clean=true`. Both Go binaries compile
from one sealed content-addressed Git source snapshot. C compiles from private
snapshots of the runtime, grammar, patch, and driver. Ambient `GOT_*`,
`GOFLAGS`, `GOAMD64`, `GOEXPERIMENT`, and `GODEBUG` values are removed and the
chosen build/runtime environments are serialized. Build, patch, link, and
child process groups are wall-bounded. A stale receipt is removed before work
starts, and a successful receipt is published atomically in its destination
directory.

Directly comparable counters are limited to applied shift/reduce/recover
actions, reduction pop requests and emitted paths/payloads, and the selected
tree census. Accept actions are reported as terminal evidence but require the
future packed-root/convergence record for interpretation. Table
lookups, lexer calls, stack versions, merges, graph links, and transient
payload construction remain explicitly labeled representation-specific
proxies.

Run the focused Docker gate from a linked worktree by mounting its absolute
Git common directory and selecting the worktree-specific Git directory inside
the container:

```sh
git_common=$(git rev-parse --path-format=absolute --git-common-dir)
git_dir_name=$(basename "$(git rev-parse --path-format=absolute --git-dir)")
bash cgo_harness/docker/run_parity_in_docker.sh \
  --label work-count-query-compile --memory 8g --cpus 2 --timeout 12m \
  --mount "$git_common:/git-common:ro" -- \
  "export GIT_DIR=/git-common/worktrees/$git_dir_name GIT_WORK_TREE=/workspace; \
   cd /workspace/cgo_harness && env \
   GTS_WORK_COUNT_ORACLE=1 \
   GTS_WORK_COUNT_RECEIPT=/workspace/harness_out/work_count/query_compile.json \
   go test . -tags 'treesitter_c_parity treesitter_c_perfscan' \
   -run '^TestAuthenticatedWorkCountOracle$|^TestWorkCountSanitizedEnvDropsParserAndGoOverrides$|^TestWorkCountRunCapturedKillsDescendantProcessGroup$|^TestStaticCLanguageSymbol' \
   -count=1 -parallel 1 -timeout 12m -v"
```

The checkout must expose usable Git metadata inside the container. Dirty
source fails closed. `GTS_WORK_COUNT_ALLOW_DIRTY=1` exists only for focused
pre-commit harness tests; its receipt records `authoritative=false` and cannot
enter the evidence ledger.
The checked-in semantic contract is
[`cgo_harness/work_count/contract_v2.json`](cgo_harness/work_count/contract_v2.json).
`construction_surplus` means representation-specific leaf plus parent
constructions minus selected nodes; it must never be relabeled as a count of
discarded nodes.

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

Fleet report reduction can preserve a terminal shard only when it carries the
closed status `no_static_c_oracle`, `no_corpus`, or `no_corpus_files` and no
measurement or oracle payload. These rows remain fatal closure findings and
are omitted from the combined oracle-language manifest. Certification still
rejects them; both modes reject missing, generic, contradictory, or mixed
identity evidence.

### Forest-routing screen and confirmation

The authenticated forest audit separates discovery from promotion. A
`forest-audit-result-v2` production shard is a directional screen: it can show
that the routed parser was faster on that run, but it cannot by itself promote
a language. Promotion requires fresh, isolated confirmation trials with the
same hashed host fingerprint, image, and resource configuration. Confirmation
uses `--cpus 1` and one numeric `--cpuset-cpus`; `--host-label` is an operator
label and is recorded separately from the automatically hashed host
fingerprint:

1. run one complete production-first trial (`pair-a`);
2. run one complete routed-first trial (`pair-b`);
3. pool the two orders only when both preserve exact production identity;
4. confirm only when the pooled routed speedup is at least 1.05x, each timed
   side lasts at least 750 ms, and order speedups stay within 10%;
5. escalate short, marginal, sign-changing, or order-sensitive results to an
   A-B-B-A sequence, increasing complete-corpus repetitions up to 20 for the
   duration floor.

The runner writes into an attempt directory, mounts the source tree read-only,
and publishes only after a successful container exit and a same-HEAD clean
worktree postcheck. Trial, run-config, and cohort receipts are immutable and
content-addressed. The reducer admits a cohort only through an explicitly
selected, content-addressed confirmation index; an unindexed or failed attempt
cannot complete a trial. `plan-confirmations` output is a planning artifact,
not evidence, and should live outside the results bundle (the reducer also
ignores its schema if it is copied there).

The reducer emits a win-only confirmation plan, keeps a screen win `incomplete`,
and records screening eligibility separately from promotion eligibility. Trees
are released after every repetition, including error, decline, nil-tree, and
divergence paths, and authoritative shards run exactly one language per
container. Every routed path must have accepted coverage in the locked C
shard. The locked C oracle remains the correctness authority: if forest and
routed results appear to correct a production-parser divergence, the reducer
records `oracle_correction_review_required` for direct three-way review instead
of auto-promoting or forcing a second four-runtime benchmark lane.

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
   core, ten process-isolated samples per backend and fixture in five paired
   Go-C-C-Go cycles, at least 750 ms of internal timing, and Go `benchmem`.
   Contended-box measurements are smoke evidence only.

## Multi-workload tracking

```sh
go run ./cmd/benchmatrix --count 10   # bench_out/matrix.{json,md} + raw logs
```

The default matrix includes a bounded, warmed language-family full-parse
group reported in MB/s, plus the Go/editor lanes above.
