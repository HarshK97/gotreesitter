# Tier classification scan (staged release gate)

Parity-vs-C is the correctness gate: a grammar is **CLEAN** when every
measured real-corpus file parses byte-identical (type/span/child-count) to
the tree-sitter C oracle; anything below 100% is incorrect-parse. The scan
makes **uncharacterized incorrect parse** the transitory tier we drive to
zero: every non-clean grammar must carry a named, assessed sub-tier in
`tier_classification.tsv`, and the committed ratchet (`clean_grammars.txt`)
makes clean→incorrect regressions release-blocking.

Tier publication and the zero-IV release block are still staged. This scan is
not currently required for every release, and the committed `tiers.md` and
`tiers.json` remain explicitly unreleased until a fresh full-corpus run is
intentionally published. Do not relabel those artifacts from an older run.

Data model: `tier_classification.tsv` is the current scan classification. A
previously-clean grammar that regresses in the current scan should have an
assessed `IV-*` row there. `clean_grammars.txt` is the historical parity-clean
ratchet/floor; it reports that current `IV-*` row as a regression, but does not
force the current classification row back to `CLEAN`.

Hygiene rules: each grammar may appear at most once in `tier_classification.tsv`,
and a current `CLEAN` row must also be present in `clean_grammars.txt`. Newly
clean grammars should first advance the ratchet before they publish as
tier-eligible.

## Tier taxonomy

One tier scale for the whole program (canonical: `tier-ratchet.md`, moved to
gotreesitter-specs (external); the taxonomy table below is the in-repo summary).
**Parity vs C is the hard gate; performance is the sub-rank.** A grammar that
is not byte-clean against the C oracle is **tier IV, full stop** — a fast
wrong parser is worthless. Tiers I–III are reserved for parity-clean grammars,
ranked by performance:

| tier | meaning | rule |
| --- | --- | --- |
| `I` | parity-clean, fast | ≤1.5× C full-parse, cold ≤5ms, blob ≤150KB |
| `II` | parity-clean, ok | ≤8× C full-parse, cold ≤20ms |
| `III` | parity-clean, poor | >8× C, or cold >20ms, or blob >400KB |
| `IV` | **not parity-clean** (any divergence, truncation, or unmeasured parity) | fix parity — see sub-causes below |

### Tier-IV sub-causes (`tier_classification.tsv`)

Every tier-IV grammar carries a named, assessed sub-cause:

| sub-cause | meaning | fix recipe |
| --- | --- | --- |
| `IV-recovery` | both parsers see errors, but C contains damage locally where Go fragments / roots ERROR | faithful C error-cost version competition (see `recovery-cost-competition.md`, moved to gotreesitter-specs (external)) |
| `IV-shape` | tree-shape divergence without error nodes | per-grammar diagnosis (`TestFirstDiffDiag`) |
| `IV-scanner` | Go external-scanner port diverges from C (over/under-permissive, token boundaries) | re-port `grammars/<g>_scanner.go` from pinned upstream `src/scanner.c` |
| `IV-version` | most corpus files error in BOTH parsers — corpus uses syntax newer than the embedded grammar | bump the embedded grammar version |
| `IV-stackcap` | divergence/truncation clears or shrinks at `GOT_GLR_MAX_STACKS=2` | add an `effectiveFullParseInitialMaxStacks` cap entry |
| `IV-extmap` | zero/few files measured under the current extension set | add a curated source-extension mapping |
| `IV-perf` | cannot measure within timeout (O(N²) or pathological file) | profile/fix before parity (overlaps the perf push) |
| `IV-unknown` | diagnosed but does not fit a single bucket | deeper single-file diagnosis |
| `IV-unassessed` | **the state we keep empty** — an incorrect or unmeasured parse nobody has triaged | run the diagnosis workflow / `TestFirstDiffDiag` |

A `?` suffix (e.g. `IV-recovery?`) marks a *preliminary* classification
inferred from the measure signature (parity%, errTree, trunc) rather than a
per-file diagnosis — these get confirmed when the full diagnosis workflow
re-runs. The scan fails (exit 1) if any current non-clean grammar in
`tier_iv.txt` or `unmeasured.txt` is missing, `IV-unassessed`, or has a stale
non-IV row such as `CLEAN`, so the uncharacterized count is enforced at zero
from the raw scan artifacts.

The deep-diagnosis evidence and proposed fixes for the first wave live in
`../../.campaign_state/diagnosis_classified.json`.

## Run for a tier publication

A release that intentionally refreshes the tier board runs the full scan:

```sh
GTS_CORPUS_DIR=/path/to/gotreesitter-corpora/corpus_sources \
  cgo_harness/docker/run_tier_scan.sh
```

- Exit 0: no previously-clean grammar regressed. The summary lists any
  NEWLY CLEAN grammars — advance `clean_grammars.txt` in the same release
  PR so the ratchet only ever grows.
- Exit 1: a grammar in `clean_grammars.txt` fell below 100% parity. A release
  using this scan as a gate must not ship until it is restored (fix the
  engine/grammar or revert the regressing change).

The corpus (~33GB of real source repos) is not vendored; the scan runs on a
host that has it (developer machine or a self-hosted runner). Hosted CI
covers the smoke/canary parity gates in `.github/workflows/ci.yml`; this scan
is the full-breadth gate when tier publication is enabled, not a standing
requirement for every release.

## Diagnostic subset and restart runs

A publication run defaults to a full scan over every grammar in `exts.tsv`.
For restartable diagnostics, narrow the scan explicitly:

```sh
GTS_CORPUS_DIR=/path/to/gotreesitter-corpora/corpus_sources \
GTS_TIER_SCAN_SKIP_TIER_PUBLISH=1 \
GTS_TIER_SCAN_N=1 \
GTS_TIER_SCAN_ROUNDS=1 \
GTS_TIER_SCAN_TIMEOUT=60 \
GTS_TIER_SCAN_LANGS='json toml' \
  cgo_harness/docker/run_tier_scan.sh \
  cgo_harness/harness_out/tier_scan/smoke-json-toml
```

Subset controls:

| env var | effect |
| --- | --- |
| `GTS_TIER_SCAN_LANGS` | Optional grammar allowlist. Accepts comma-separated, whitespace-separated, or mixed names, for example `json,toml gitcommit`. |
| `GTS_TIER_SCAN_ROUNDS` | Timing parse rounds per measured file. Defaults to `3`; use `1` for fast diagnostic smoke runs. |
| `GTS_TIER_SCAN_ALL_FILES=1` | Selects every eligible file after deterministic sorting instead of the first `GTS_TIER_SCAN_N` files. Per-grammar manifests and aggregate manifest entries record `all_files` and `eligible_total` for auditability. |
| `GTS_TIER_SCAN_START_AFTER` | Skips `exts.tsv` order until after the named grammar. Use this with the previous run's `active_grammar.txt` or `visited_grammars.txt` when restarting a broad scan. |
| `GTS_TIER_SCAN_LIMIT` | Stops after N selected grammars, after the allowlist and start-after filters. |
| `GTS_TIER_SCAN_ISOLATE_FILES=1` | Opt-in hard per-file frame isolation. The scan uses the deterministic `manifest-<grammar>-<corpus>.json` selection, runs one timeout-bounded measure child per file with `REPRO_FILE`, ingests frames immediately after each child exits, and aggregates the per-file rows into the existing grammar-level `MEASURE-DTIER` line. |
| `GTS_TIER_SCAN_SKIP_TIER_PUBLISH=1` | Skips the final `cgo_harness/tier_scan/tiers.{md,json}` regeneration. Use this for diagnostics and smoke runs. |

When a scan is narrowed, ratchet regression checks are scoped to the visited
grammars so an allowlist does not report every unvisited clean grammar as a
regression. Full scans preserve the clean-grammar ratchet behavior.

Recommended restart workflow:

1. Start a broad scan in a dedicated output directory under
   `cgo_harness/harness_out/tier_scan/`.
2. Watch `progress.log`, `status.tsv`, and `active_grammar.txt`.
3. If the scan stops or is interrupted, inspect `visited_grammars.txt` and
   restart with `GTS_TIER_SCAN_START_AFTER=<last-visited-grammar>` into a new
   output directory.
4. Keep `GTS_TIER_SCAN_SKIP_TIER_PUBLISH=1` until the final publication scan that
   intentionally refreshes `cgo_harness/tier_scan/tiers.{md,json}`.

## Parallel control plane

For broad diagnostic scans, split the selected grammar list into shard-local
workers and keep parent-level telemetry in one output directory:

```sh
GTS_CORPUS_DIR=/path/to/gotreesitter-corpora/corpus_sources \
GTS_TIER_SCAN_N=1 \
GTS_TIER_SCAN_ROUNDS=1 \
GTS_TIER_SCAN_TIMEOUT=120 \
GTS_TIER_SCAN_ISOLATE_FILES=1 \
GTS_TIER_SCAN_PARALLELISM=8 \
GTS_TIER_SCAN_SHARDS=32 \
  cgo_harness/docker/run_tier_scan_parallel.sh \
  cgo_harness/harness_out/tier_scan_parallel/scan-206-smoke
```

The parent directory owns the control-plane artifacts:

| file | contents |
| --- | --- |
| `manifest.json` | Planned shard workers, language lists, parent env/command summary, and per-worker artifact paths. |
| `events.jsonl` | Append-only parent lifecycle stream. Each worker gets a `START` row with shard, worker dir, languages, PID, command/env summary, and an `END` or `FAIL` row with `rc`. |
| `active_worker.txt` | Live TSV snapshot of currently running workers, including each worker PID, shard, worker dir, `active_grammar.txt`, `progress.log`, `status.tsv`, `summary.json`, and the last active grammar row when present. |
| `active_worker.json` | JSON form of the active snapshot, including pending/running/complete worker counts and per-worker artifact paths. |
| `status.tsv` | Parent lifecycle TSV: timestamp, event, shard, pid, rc, worker dir, detail. |
| `progress.log` | Human-readable parent lifecycle log. |
| `workers/<shard>/...` | Ordinary `run_tier_scan.sh` artifacts for that shard. |
| `merged/...` | Reducer output from `merge_runs.py` after worker completion. |

Stall check workflow:

1. Read `active_worker.json` or `active_worker.txt` in the parent directory to
   identify live shard PIDs and worker artifact paths.
2. For each running shard, inspect the referenced `workers/<shard>/active_grammar.txt`
   and `workers/<shard>/progress.log` to find the currently active
   grammar/corpus/frame and the last heartbeat.
3. Compare parent `events.jsonl` `START` rows against terminal `END`/`FAIL`
   rows. Any open shard is a worker that has not produced a terminal parent
   lifecycle event.
4. For a live partial reducer view, run:

```sh
python3 cgo_harness/tier_scan/merge_runs.py --allow-incomplete --no-summarize \
  cgo_harness/harness_out/tier_scan_parallel/<run>/merged-snapshot \
  cgo_harness/harness_out/tier_scan_parallel/<run>/workers/shard-*
```

When parent events are present, the snapshot `summary.json` includes
`merge.parent_lifecycle` with parent start/terminal/open counts, open shard
details, and the parent active-worker snapshot.

## Parallel wringer control plane

Use the parallel wringer wrapper when the goal is one independent
single-grammar wringer per grammar, with a parent anti-stall surface and a
compact reducer view:

```sh
GTS_CORPUS_DIR=/path/to/gotreesitter-corpora/corpus_sources \
GTS_WRINGER_PARALLEL_DRY_RUN=0 \
GTS_WRINGER_PARALLELISM=4 \
GTS_WRINGER_N=10 \
GTS_WRINGER_TIMEOUT=90 \
  cgo_harness/docker/run_grammar_integrity_wringer_parallel.sh \
  --langs 'json go' \
  cgo_harness/harness_out/grammar_integrity_wringer_parallel/json-go
```

The default is dry-run (`GTS_WRINGER_PARALLEL_DRY_RUN=1`), which writes the
parent manifest and active snapshots without launching parser work. Selection
uses `exts.tsv` order and accepts `--langs`, `GTS_WRINGER_START_AFTER`, and
`GTS_WRINGER_LIMIT`.

Parent artifacts:

| file | contents |
| --- | --- |
| `manifest.json` | Selected grammar workers, command/env summary, and compact per-worker artifact paths. |
| `events.jsonl` | Parent lifecycle rows for worker `START`, `END`, `FAIL`, and `ABORT`, including grammar, worker dir, PID, rc, command/env summary, and artifact paths. |
| `active_worker.txt` | TSV snapshot for every pending/running/complete grammar with worker PID and key compact artifact paths. |
| `active_worker.json` | JSON anti-stall snapshot with pending/running/complete/failed counts, last `wringer_active.json` when present, `wringer_summary.json`, `frame_matrix.jsonl`, plan, frames, and baseline status/progress paths. |
| `status.tsv` | Parent lifecycle TSV: timestamp, event, grammar, pid, rc, worker dir, detail. |
| `progress.log` | Human-readable parent lifecycle log. |
| `workers/<grammar>/...` | Ordinary single-grammar wringer artifacts for that grammar. |
| `merged/...` | Compact reducer output from `merge_wringer_runs.py` unless `--no-merge` is set. |

The reducer reads only compact wringer artifacts:

```sh
python3 cgo_harness/tier_scan/merge_wringer_runs.py \
  cgo_harness/harness_out/grammar_integrity_wringer_parallel/<run>/merged-snapshot \
  cgo_harness/harness_out/grammar_integrity_wringer_parallel/<run>/workers/*
```

Add `--allow-incomplete` for a live snapshot while workers are still missing
final `wringer_summary.json` files. Reducer output includes `summary.json`,
source-annotated `frame_matrix.jsonl` and `wringer_plan.jsonl`, compact
`wringer_frames.jsonl` and `wringer_events.jsonl`, and `residual_frames.jsonl`
for every baseline frame whose status is not `match`.

Wringer summaries classify each frame into a conservative failure family using
only existing telemetry: baseline terminal status, comparison diagnostics,
runtime stop hints, root error flags, timeout/progress evidence, and completed
variant outcomes. `frame_matrix.jsonl` carries per-frame `family` and
`family_reasons`; `wringer_summary.json` carries aggregate `family_counts` and
per-family `family_frames`; `wringer_summary.md` renders the same counts for quick
triage. The labels are investigation routing hints, not parser behavior or
parity-gate inputs. Current labels are `accepted_shape_materialization`,
`recovery_error_shape`, `truncation_frontier_loss`, `timeout_fanout_perf`,
`scanner_token_accounting_or_unknown_scanner`, `version_or_corpus`, and
`clean`.

## Telemetry artifacts

Each run writes compact progress and restart state into the output directory:

| file | contents |
| --- | --- |
| `progress.log` | Human-readable event stream. |
| `status.tsv` | Machine-readable event stream: timestamp, event, grammar, corpus kind, detail. |
| `events.jsonl` | Append-only JSON event stream for scan, worker, heartbeat, and parsed per-frame lifecycle events. |
| `frames.jsonl` | Parsed per-file/per-phase frame telemetry derived from `MEASURE-PROGRESS`, with terminal timeout/fail rows when a child exits without `MEASURE-DTIER`. |
| `manifest.json` | Aggregate run manifest listing each measured grammar/corpus and its per-grammar manifest file. |
| `manifest-<grammar>-<corpus>.json` | Deterministic selected-file manifest: stable path order, size, sha256, corpus root/kind, extensions, rounds, and parser/oracle identifiers. |
| `active_grammar.txt` | Last active grammar/corpus kind plus measurement detail; becomes `idle` after the scan loop completes. |
| `visited_grammars.txt` | Grammars selected and visited by this run. |
| `resume.env` | Shell-readable resume state with the last completed grammar, active/checkpoint artifact paths, and exact `GTS_TIER_SCAN_START_AFTER=...` hint when available. |
| `summary.json` | Final counts for clean, tier IV, unmeasured, visited grammars, timeout/fail/heartbeat events, final active state, and scan exit status. |
| `diagnostic_summary.json` | Derived diagnostic report from `summarize_scan.py`: MEASURE-DTIER signature buckets, generalized diagnostic families, slow non-clean measured grammars, unmeasured evidence, stale classification hints, and separate parity sample-size notes for diagnostic scans whose `GTS_TIER_SCAN_N` differs from the TSV baseline. Diagnostic families use scan telemetry only and include labels such as `timeout_fanout_perf`, `truncation_budget_frontier`, `recovery_error_cost`, `accepted_shape_materialization`, `scanner_token_accounting`, `unclear_needs_diagnostic`, `clean`, and `clean_but_slow_perf`; runtime evidence includes parsed `stopReason`, `rootErr`, `tokens=0` accepted divergence counts, `maxStacks`, structured `comparisonDiagnostic` first-diff/root-shape/error evidence, and last progress for unmeasured rows when present. |
| `diagnostic_summary.md` | Markdown rendering of `diagnostic_summary.json` for quick scan review. |

## Wave 4 external-lex-state election ledger

`external_lex_elections.{json,md}` is the Wave 4 control-plane ledger for
all 206 grammars. It derives its grammar universe from `exts.tsv`, external
scanner support from the grammar scanner registries, precise ExternalLexStates
coverage from generated/staged lex-state tables, and current parity status
from `tier_classification.tsv`.

Regenerate after scanner or ExternalLexStates changes:

```bash
python3 cgo_harness/tier_scan/gen_external_lex_elections.py
```

Check committed outputs without rewriting them:

```bash
python3 cgo_harness/tier_scan/gen_external_lex_elections.py --check
```

The ledger statuses are intentionally stricter than the tier table: a grammar
can be parity-clean and still be `blocked_missing_precise_els` if it has a Go
external scanner but no precise ExternalLexStates table registered or staged
yet. That means Wave 4 has not certified its external-scanner C-recovery
election path, even if ordinary corpus parity is currently clean.

During a grammar measurement, the scan emits `HEARTBEAT` rows to both
`progress.log` and `status.tsv` every `GTS_TIER_SCAN_HEARTBEAT` seconds
(default `60`). Set `GTS_TIER_SCAN_HEARTBEAT=0` to disable them. Heartbeat
details include elapsed seconds, the raw per-grammar log path, and the raw log
byte count so a long single-grammar stall is visible before timeout. Heartbeats
also include the supervisor PID, worker PID, and progress log age. Timeout and
failure rows carry the last observed `MEASURE-PROGRESS` frame so an interrupted
grammar has an explicit stale/incomplete location.

When `TestMeasureDtierVsC` finds a Go-vs-C structural divergence, it always
emits a shell-quote-safe progress row with `phase=comparison_diag` and
`result=diverge`, independent of `REPRO_SIGNATURES`. This row carries the
first-diff kind and path, Go/C node type/span/child-count, Go/C root
type/span/child-count/error flags, Go/C `ERROR` and missing-node counts,
`goStop`, and the Go parse runtime summary. When Go is structurally
different but has no `ERROR`/missing nodes while the C oracle does, the row also
carries `oracle=go_clean_c_error` and the terminal `MEASURE-DTIER` line
increments `oracleBetter`; this remains a non-parity result, but it is separated
from ordinary Go parser failures for triage. The older `DIVERGE-SIG` line
remains gated by `REPRO_SIGNATURES=1`; tools should prefer the structured
`MEASURE-PROGRESS phase=comparison_diag` row for machine summaries.

With `GTS_TIER_SCAN_ISOLATE_FILES=1`, a grammar is not measured by one long
child process. The harness first writes the deterministic manifest, then runs
`TestMeasureDtierVsC` once per manifest file using `REPRO_FILE=<path>`. Raw
logs are named `measure-<grammar>-<corpus>-frame-0001.log`,
`measure-<grammar>-<corpus>-frame-0002.log`, and so on. Each child has its
own `GTS_TIER_SCAN_TIMEOUT`, and `frames.jsonl` is updated immediately after
the child exits. A timeout or failed child writes a terminal timeout/fail frame
with the last observed phase/file/path evidence and is counted fail-closed in
the grammar aggregate `files` count and parity denominator, so the grammar
classifies as tier IV rather than appearing clean. In isolated mode, only a
selected frame that reports `parityMatch=1/1` counts as matching; selected
frames that time out, fail, panic, or produce a zero-dispatch `MEASURE-DTIER`
row count as non-matching frames. Successful per-file rows include exact
`goNS` and `cNS` timing fields; the aggregate sums those fields for `aggRatio`
and uses the per-file ratios for `medianRatio`. If the deterministic manifest
selects no files, isolated mode emits no aggregate measure line for that corpus,
so curated fallback and zero-files/unmeasured handling remain the same as the
default scan path.

When reusing an output directory, `run_tier_scan.sh` clears prior
`measure-*.log` files and diagnostic summaries before the new scan starts.
The diagnostic summarizer derives measured grammar rows from the current
`tier_scan.txt`; raw logs are used only as evidence for current measured or
unmeasured entries. Exact TSV parity-difference hints are emitted only when the
current parity denominator matches the TSV denominator.

You can regenerate the derived diagnostic report without rerunning the scan:

```sh
python3 cgo_harness/tier_scan/summarize_scan.py \
  cgo_harness/harness_out/tier_scan/<run-dir>
```

## Single-grammar wringer

For deterministic per-frame integrity triage, run one grammar through the
wringer. It composes the isolated tier scan, records every selected file, then
replays suspicious files with controlled stack, node, forest, merge,
materialization, and recovery variants plus bounded `TestFirstDiffDiag`
diagnostics:

```sh
GTS_WRINGER_N=1 \
GTS_WRINGER_ROUNDS=1 \
GTS_WRINGER_TIMEOUT=60 \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-smoke \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-smoke"
```

`run_parity_in_docker.sh` passes outer `GTS_WRINGER_*` and
`GTS_TIER_SCAN_*` controls into the container. Corpus paths are different:
mount the host corpus with `--mount`, then set `GTS_CORPUS_DIR` to the
container path such as `/workspace/corpus_sources` in the custom command.

Parser-loop progress is opt-in so normal tier scans stay quiet. Enable it for
wringer runs with `GTS_WRINGER_PARSE_PROGRESS=1`; the wrapper translates that
to `GOT_PARSE_PROGRESS=1` for the delegated baseline tier scan and variant
measurement children. Set `GTS_WRINGER_PARSE_PROGRESS_INTERVAL_MS` to pass a
non-default `GOT_PARSE_PROGRESS_INTERVAL_MS`.

Recommended command to put one grammar through the wringer before any full
206-grammar scan. This is the closed-world every-frame integrity contract:
baseline selection, every configured variant, every first-diff action,
parser-loop progress, heartbeat, and strict post-summary assertions all run for
one deterministic grammar sample:

```sh
bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-progress \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources \
   GTS_WRINGER_MODE=full-integrity \
   GTS_WRINGER_N=1 \
   GTS_WRINGER_ROUNDS=1 \
   GTS_WRINGER_TIMEOUT=30 \
   GTS_WRINGER_HEARTBEAT=2 \
   GTS_WRINGER_PARSE_PROGRESS_INTERVAL_MS=100 \
   cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-progress"
```

`GTS_WRINGER_MODE=full-integrity` defaults variant replay to every selected
frame, first-diff replay to every selected frame, parser-loop progress to on,
and post-summary assertions to `closed,plan,telemetry,artifacts`. Set
`GTS_WRINGER_ASSERTS=0` only for artifact surgery. Existing
`GTS_WRINGER_FULL=1` and `GTS_WRINGER_PROFILE=full` remain compatible
full/every-frame replay presets.
These full replay presets do not change baseline file selection. Set
`GTS_WRINGER_ALL_FILES=1` when the baseline should include every eligible
corpus file instead of the first `GTS_WRINGER_N` files.

For high residual-rate grammars, keep the inventory broad but cap diagnostic
fanout: use `GTS_WRINGER_ALL_FILES=1 GTS_WRINGER_MAX_SUSPICIOUS=5` so the
baseline classifies every eligible file while default suspicious-only variants
and first-diff diagnostics sample the first five suspicious frames. Leave the
cap unset for tiny or focused grammars where full-integrity replay over every
selected/suspicious frame is tractable.

After it starts, use the control surface first. It answers which
grammar/frame/action is active, how many selected frames and planned actions
exist, how many actions completed or remain incomplete by stage, whether any
wringer child or baseline frame lifecycle is open/stale, and the exact files to
inspect next:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-json-progress \
  --print-control-status

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-json-progress \
  --emit-control-json | jq .
```

Use the assertion bundle as the machine gate for the same contract:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-json-progress \
  --assert-closed \
  --assert-plan-exact \
  --assert-telemetry-complete \
  --assert-artifacts-sane
```

Poll live state while it runs:

```sh
cat cgo_harness/harness_out/wringer-json-progress/wringer_active.json
tail -n 20 cgo_harness/harness_out/wringer-json-progress/wringer_events.jsonl
tail -n 20 cgo_harness/harness_out/wringer-json-progress/baseline/status.tsv
tail -n 20 cgo_harness/harness_out/wringer-json-progress/baseline/frames.jsonl
```

The wringer writes `wringer_manifest.json`, `wringer_active.txt`,
`wringer_active.json`,
`wringer_events.jsonl`, `frame_catalog.jsonl`, `frame_matrix.jsonl`,
`wringer_plan.jsonl`, `wringer_plan.json`,
`wringer_frames.jsonl`,
`wringer_summary.json`, `wringer_summary.md`, and `commands.log` in its output
directory. `wringer_active.txt` and `wringer_active.json` carry the same JSON
payload and are updated before the delegated baseline scan
and before each variant and first-diff child with the active stage, ordinal,
variant/mode, path, log, timeout, and timestamp. For the baseline stage, the
active row points at the baseline progress log and records the delegated
`run_tier_scan.sh` command; the baseline's own `active_grammar.txt`,
`progress.log`, and `status.tsv` remain under `baseline/`. It is set to `idle`
only after a normal wringer exit; abnormal exits preserve the last known active
stage/frame with `failed` or `aborted` state and append a terminal event for an
in-flight child if needed. `wringer_events.jsonl` is an append-only live event
stream with immediate `START` rows and terminal `END`, `TIMEOUT`, or `FAIL`
rows around the delegated baseline scan and each variant/first-diff child. If
the baseline scan exits nonzero but still emits usable baseline artifacts, the
wringer records the terminal baseline event with its `rc`, appends a
`CONTINUE` baseline event, sets `baseline_exit_status` in the manifest, and
continues with `infra_status=0`.
While a delegated baseline, variant, or first-diff child is alive, the wringer
also emits `HEARTBEAT` rows every `GTS_WRINGER_HEARTBEAT` seconds (default
`15`; set `0` to disable). The delegated baseline passes the same value through
as `GTS_TIER_SCAN_HEARTBEAT`, so `baseline/active_grammar.txt`,
`baseline/progress.log`, and `baseline/status.tsv` continue to update during
the baseline scan. Wringer heartbeat rows carry elapsed seconds, child PID,
log byte count, and log/progress age when the log exists. `START`, terminal
`END`/`FAIL`/`TIMEOUT`, and run metadata include epoch/ISO timing fields plus
`duration_s` or `elapsed_s` for stall diagnosis.
`frame_catalog.jsonl` is the stable
per-selected-frame index: exactly one JSON record per selected baseline file,
with ordinal, size/hash, baseline log, suspicious reasons, terminal baseline
state, parsed runtime hints, and replay commands for that exact frame.
`frame_matrix.jsonl` is the stable per-frame control matrix: exactly one JSON
record per selected frame with ordinal/index/selected_total, grammar/corpus,
path/base/size/sha256, baseline terminal status and reasons, replay plan,
variant actions keyed by configured variant, first-diff action, and stage
completion flags/counts. Every baseline, variant, and first-diff action carries
`planned`, `completed`, `status`, and, when not planned, `reason`. Status values
are `not_planned`, `not_run`, `end`, `fail`, `timeout`, `match`, and
`nonmatch`: `not_planned` means the manifest plan excluded the action,
`not_run` means it was planned but no terminal evidence exists, `end` means a
non-parity diagnostic completed with rc=0, `fail` means a nonzero or malformed
terminal result, `timeout` means the timeout wrapper fired, and
`match`/`nonmatch` are baseline or variant parity outcomes. Reason values include
`stage_disabled`, `selector`, `no_suspicion`, `max_suspicious`, and
`max_diag_files`.
`stage_completion` is true for a stage when all actions planned for that frame
completed; stages with no planned action for that frame are not incomplete.
`wringer_plan.jsonl` is the flattened closed-world experiment plan derived from
the current manifest, frame catalog, frame matrix, configured variant list, and
observed metadata. It has one deterministic action row for the baseline action,
each configured variant action, and the first-diff action for every selected
frame. Rows include `action_id`, `planned`, `completed`, `status`, optional
`reason`, `stage`, `mode`/`variant`, ordinal, grammar/corpus, path, sha256,
log path, timeout, replay commands, event lifecycle, started/finished times,
duration/elapsed seconds, heartbeat count, last heartbeat, and a
`frame_matrix_key` for mapping back to `frame_matrix.jsonl`. Baseline plan rows
now use the tier-scan per-frame status stream as first-class telemetry with
`event_scope=baseline_frame` when `baseline/status.tsv` has frame lifecycle
rows. Those rows carry frame `START`, `HEARTBEAT`, and terminal
`END`/`FAIL`/`TIMEOUT` evidence, including ordinal/total, file, size, sha256,
log path, elapsed seconds, worker PID, log byte count, progress age, and last
`MEASURE-PROGRESS` detail when available. With parser progress enabled they
also carry the latest `PARSE-PROGRESS` fields as `parser_phase`,
`parser_elapsed_ms`, `parser_iter`, `parser_tokens`, `parser_stacks`,
`parser_live_stacks`, `parser_max_stacks`, `parser_node_count`,
`parser_token_start`, and `parser_token_end`; timeout frames therefore show
the parser loop position instead of stopping at only
`MEASURE-PROGRESS phase=go_parse_start`. Older artifacts without frame status
rows still fall back to the delegated baseline event scope. `wringer_plan.json`
is the compact summary with planned/completed counts, aggregate
timing/heartbeat telemetry, baseline-frame telemetry counts, and any unexpected
observed variant or first-diff metadata. Unexpected observed evidence is also
emitted in `wringer_plan.jsonl` as `planned=false`, `completed=true`,
`reason=unplanned_observed_evidence`.
Summary regeneration refreshes both plan artifacts. When a baseline or variant
measurement log contains `phase=comparison_diag`, `frame_catalog.jsonl`,
`frame_matrix.jsonl`, and `wringer_plan.jsonl` preserve it as
`comparison_diagnostic` evidence with `count`, `diffs`, `rootPairs`, and the
first rows of normalized first-diff/root-shape/error fields.
`wringer_frames.jsonl` remains the event-style telemetry stream. Set
`GTS_WRINGER_MODE=full-integrity` for the strict full frame-control preset:
variant scope defaults to `all`, first-diff defaults to every selected frame
(`all`), parser progress defaults on, and plan assertions fail closed when any
selected ordinal lacks a planned baseline action, configured variant action, or
first-diff action. `GTS_WRINGER_FULL=1` and `GTS_WRINGER_PROFILE=full` remain
compatible full/every-frame replay aliases: they default variant scope and
first-diff frames the same way, but do not imply parser progress. Baseline
selection stays unfiltered unless `GTS_WRINGER_BASELINE_FRAMES` is explicitly
set. Explicit user selectors still win: `GTS_WRINGER_VARIANT_SCOPE`,
`GTS_WRINGER_VARIANT_FRAMES`, `GTS_WRINGER_FIRSTDIFF_FRAMES`, or fallback
`GTS_WRINGER_FRAMES` override the preset for that replay stage. In
`full-integrity` mode, such overrides are visible to `--assert-plan-exact`; if
they leave a selected frame/action unplanned, the assertion reports the missing
ordinal and stage. Without the preset, set
`GTS_WRINGER_VARIANT_SCOPE=all` to replay every selected file instead of only
suspicious files, or override `GTS_WRINGER_VARIANTS` with a space-separated
subset of the supported variants:

| variant | measurement child env |
| --- | --- |
| `stack2` | `GOT_GLR_MAX_STACKS=2` |
| `stack8` | `GOT_GLR_MAX_STACKS=8` |
| `stack48` | `GOT_GLR_MAX_STACKS=48` |
| `node3` | `GOT_PARSE_NODE_LIMIT_SCALE=3` |
| `forest_off` | `GOT_GLR_FOREST=0` |
| `forest` | `REPRO_FOREST=1` |
| `merge1` | `GOT_GLR_MAX_MERGE_PER_KEY=1` |
| `merge24` | `GOT_GLR_MAX_MERGE_PER_KEY=24` |
| `faithful` | `GOT_FAITHFUL_CONDENSE=1` |
| `pre_mat` | `GOT_GLR_V2_PRE_MATERIALIZATION_DIAG=1` |
| `mat_off` | `GOT_GLR_V2_PENDING_PARENTS=0`, `GOT_GLR_V2_FINAL_CHILD_REFS=0`, `GOT_GLR_V2_COMPACT_FULL_LEAVES=0` |
| `crecovery_all` | `GOT_C_RECOVERY=all` |
| `crecovery_off` | `GOT_C_RECOVERY=0` |

Variant env only applies to the measurement child. First-diff diagnostics keep
their own env; set `GTS_WRINGER_GLR_TRACE=1` to add `REPRO_GLR_TRACE=1`, and
set `GTS_WRINGER_DEBUG_DFA=1` for scanner/token proof runs that need
`REPRO_DEBUG_DFA=1`. Equivalence audit is still available through
`cgo_harness/cmd/equiv_audit_oneshot` and the parse gap tooling, not as a
wringer variant yet.

Frame ordinals are 1-based positions in the deterministic first-N manifest for
that grammar/corpus, not positions in a filtered replay file. Selector specs are
comma-separated: selectors accept single ordinals, lists, ranges, or mixed forms
such as `1`, `1,4,7`, `3-6`, and `1,3-5`. They also accept stable identity selectors:
`sha256:<prefix>` matches a selected file's SHA-256 prefix, `base:<filename>`
matches the exact basename, and `path:<substring>` matches a deterministic
substring of the selected absolute corpus path. Identity selector values may
contain spaces when the whole env var or CLI argument is quoted; commas remain
selector separators and cannot appear inside an identity value. Invalid ranges,
zero/negative ordinals, empty selectors, and selectors that match no current
frame fail before the replay stage starts and print the available
ordinal/base/SHA/path identities. The baseline stage validates selectors in `run_tier_scan.sh`;
variant and first-diff replays validate against the emitted selected-frame JSONL
derived from `frame_catalog.jsonl`. Without an explicit first-diff selector,
first-diff still defaults to suspicious frames only.

To list the deterministic frame catalog for one grammar before spending parser
time on variants or first-diff, run plan-only mode. It writes the normal
manifest/control artifacts, `frame_catalog.jsonl`, `frame_matrix.jsonl`, and
`wringer_plan.jsonl`, but skips parser measure frames and replay children:

```sh
GTS_WRINGER_PLAN_ONLY=1 \
GTS_WRINGER_N=10 \
GTS_WRINGER_ROUNDS=1 \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-plan \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-plan"
```

Then inspect the concise per-frame control view and choose an exact frame by
ordinal, basename, SHA prefix, or path substring:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-json-plan \
  --print-frame-control

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-json-plan \
  --frames 'sha256:8d91f2' \
  --emit-replay-commands all
```

After choosing frames, run a real baseline or targeted replay with the same
selectors. For example, this reuses the planned output directory, executes only
the exact basename as a real isolated baseline frame, then runs variants plus
summary assertions:

```sh
GTS_WRINGER_STAGES=baseline,variants,summary \
GTS_WRINGER_BASELINE_FRAMES='base:package.json' \
GTS_WRINGER_VARIANT_FRAMES='base:package.json' \
GTS_WRINGER_VARIANTS='stack2 node3' \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-package-replay \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-plan"
```

For a full grammar wring, keep the defaults and choose a deterministic sample
size:

```sh
GTS_WRINGER_MODE=full-integrity \
GTS_WRINGER_N=10 \
GTS_WRINGER_ROUNDS=1 \
GTS_WRINGER_TIMEOUT=60 \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-rst-full \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh rst cgo_harness/harness_out/wringer-rst-full"
```

To run one grammar through every eligible corpus frame, use the parallel
control plane with a single worker. This is intentionally heavy: it selects all
eligible files, then runs every selected frame through baseline, variants,
first-diff, parser progress, heartbeat telemetry, and strict assertions.

```sh
GTS_WRINGER_PARALLEL_DRY_RUN=0 \
GTS_WRINGER_PARALLELISM=1 \
GTS_WRINGER_ALL_FILES=1 \
GTS_WRINGER_MODE=full-integrity \
GTS_WRINGER_ROUNDS=1 \
GTS_WRINGER_TIMEOUT=120 \
GTS_WRINGER_HEARTBEAT=10 \
GTS_WRINGER_PARSE_PROGRESS_INTERVAL_MS=250 \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-rst-all-files \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources \
   cgo_harness/docker/run_grammar_integrity_wringer_parallel.sh \
   --langs 'rst' --parallelism 1 \
   cgo_harness/harness_out/grammar_integrity_wringer_parallel/rst-all-files"
```

To inspect an existing run without rewriting summaries:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --emit-frame-catalog-jsonl | sed -n '1,2p'
```

For a compact live status view that combines the wringer active row with the
delegated baseline active row, wringer START-vs-terminal lifecycle balance, and
baseline frame START-vs-terminal balance:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --print-status
```

For a concise control-plane summary from existing artifacts, including the
active grammar/frame/action, selected frame ordinals, planned/completed/
incomplete counts by stage and overall, open/stale wringer and baseline frame
lifecycle evidence, incomplete planned actions without terminal proof,
unexpected actions, exact inspection paths, and replay-command helper path:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --print-control-status
```

For one script-friendly row per selected frame, including ordinal, basename,
SHA prefix, suspicious reasons, baseline status, planned/completed variants,
first-diff status, and replay-command availability:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --print-frame-control
```

For machine polling, emit a single JSON object with the same control contract:
wringer and delegated baseline active rows, selected frames, per-stage and
overall action counts, incomplete planned actions, open/stale lifecycle
evidence, baseline frame lifecycle balance, unexpected evidence, artifacts, and
stall-inspection paths:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --emit-control-json
```

The older active-only JSON surface remains available:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --emit-active-json
```

To inspect the per-frame matrix directly:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --emit-frame-matrix-jsonl | sed -n '1,2p'
```

To inspect the flattened closed-world plan directly:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --emit-plan-jsonl | sed -n '1,2p'
```

To inspect one frame or selector without parsing the whole run, pass
`--frames`. The same selector grammar is accepted as the wringer environment
selectors, including `1`, `1,4,7`, `3-6`, `1,3-5`, `sha256:<prefix>`,
`base:<filename>`, `path:<substring>`, `all`, and `*`; selector entries are
comma-separated and unavailable identities fail with a clear message.

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --frames 4 \
  --emit-frame-matrix-jsonl

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --frames 4 \
  --emit-replay-commands all
```

For status checks suitable for scripts, use assertions. `--assert-closed`
returns nonzero when either `wringer_events.jsonl` has `START` rows without
terminal `END`, `FAIL`, or `TIMEOUT` rows, or delegated baseline frame
lifecycle in `baseline/status.tsv` remains open. `--assert-planned-complete`
returns nonzero when any planned baseline, variant, or first-diff action in the
matrix is still incomplete. `--assert-plan-exact` is stricter: it includes the
same incomplete planned-action check, proves every selected frame has exactly
one baseline plan row, fails when variant or first-diff metadata exists for an
action the current manifest selectors and configured variant list did not plan,
and rejects lifecycle evidence for actions outside the matrix. Use it when a
rerun must prove observed evidence exactly matches the closed-world plan, not
just that planned work eventually completed. `--assert-telemetry-complete`
fails when the delegated baseline action, any baseline frame action with
frame-level telemetry, or any planned variant/first-diff action selected by
`--frames` lacks START, terminal, or duration evidence.
`--assert-artifacts-sane` fails when required JSON/JSONL artifacts are missing
or corrupt, the selected frame catalog is empty for a normal run, the plan has
zero planned actions, a planned action lacks terminal evidence, baseline frame
completion is only progress evidence, or lifecycle evidence appears outside the
matrix.
For new artifacts with baseline frame status rows, completed planned baseline
actions must have frame-level start, terminal, and duration telemetry; older
artifacts without frame status keep using delegated baseline telemetry.
The wrapper runs `closed,plan,telemetry,artifacts` after the final summary by
default whenever the summary stage is enabled; set `GTS_WRINGER_ASSERTS=0` only
for artifact surgery. Combine assertions with `--frames` to check a single
frame or selector.

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --assert-closed \
  --assert-planned-complete

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --frames 4 \
  --assert-planned-complete

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --assert-plan-exact

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --frames 4 \
  --assert-telemetry-complete

python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --assert-plan-exact \
  --assert-telemetry-complete \
  --assert-artifacts-sane
```

To replay from an existing baseline without selecting or measuring the baseline
again, point the wringer at the same output directory and set
`GTS_WRINGER_REUSE_BASELINE=1`. The baseline directory must already contain
`baseline/manifest.json` and `baseline/summary.json`; reuse does not delete the
baseline artifacts.

To measure one baseline frame surgically, set `GTS_WRINGER_BASELINE_FRAMES`.
The filter is applied after deterministic first-N selection, so a run with
`GTS_WRINGER_N=10` and `GTS_WRINGER_BASELINE_FRAMES=4` executes only the fourth
selected file while preserving `ordinal=4`, `index=4`, `total=10`, and the
baseline log name `measure-<grammar>-<corpus>-frame-0004.log`.

```sh
GTS_WRINGER_N=10 \
GTS_WRINGER_BASELINE_FRAMES=4 \
GTS_WRINGER_STAGES=baseline,summary \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-baseline-frame4 \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-frame4"
```

Then reuse that baseline for variants or first-diff on the same preserved
ordinal:

```sh
GTS_WRINGER_REUSE_BASELINE=1 \
GTS_WRINGER_STAGES=variants,summary \
GTS_WRINGER_VARIANTS='stack2 node3' \
GTS_WRINGER_VARIANT_FRAMES=4 \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-replay \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-frame4"
```

Each catalog row also carries a `replay_plan.baseline_reuse_variant_command`
for the same filtered rerun and `replay_plan.direct_variant_commands` for
running a single parser variant directly against the baseline `measure.test`
binary.

To list replay commands without parsing the catalog by hand:

```sh
python3 cgo_harness/tier_scan/wringer_summary.py \
  cgo_harness/harness_out/wringer-rst-full \
  --emit-replay-commands all
```

The catalog is the wringer source of truth for frame replay. Each row preserves
the original manifest ordinal/index/total, source `size` and `sha256`,
baseline log path, suspicious reasons, terminal baseline state, and replay
fields:

| field | purpose |
| --- | --- |
| `replay_plan.baseline_command` | Rerun only this baseline frame through the wringer baseline stage. |
| `replay_plan.direct_baseline_command` | Replay this exact baseline file directly against the compiled `measure.test` binary. |
| `replay_plan.baseline_reuse_variant_command` | Reuse the existing baseline and run only this ordinal through the selected variants. |
| `replay_plan.direct_variant_commands` | Per-variant direct `timeout env ... measure.test` commands for this exact file. |
| `replay_plan.baseline_reuse_firstdiff_command` | Reuse the existing baseline and run first-diff diagnostics only for this ordinal. |
| `replay_plan.direct_firstdiff_command` | Direct `TestFirstDiffDiag` command for this exact file. |

First-diff replay commands preserve recorded `GTS_WRINGER_GLR_TRACE=1` and
`GTS_WRINGER_DEBUG_DFA=1` settings from `wringer_manifest.json`.

`wringer_frames.jsonl` is the machine-readable event stream. Baseline frame
events include stage/mode, lifecycle, active grammar/path, log path, source
size/hash, and terminal timeout/fail rows. Variant and first-diff rows include
stage/mode, ordinal, lifecycle (`end`, `timeout`, or `fail`), rc, log path, and
the generated replay command. For live monitoring during variant and first-diff
replays, prefer `wringer_active.txt` and `wringer_events.jsonl`; those files
are updated immediately around each child process and continue to heartbeat
while the child is alive. During baseline, prefer `wringer_active.txt` plus
`baseline/active_grammar.txt`, `baseline/progress.log`, and `baseline/status.tsv`.
`wringer_frames.jsonl` and `frame_matrix.jsonl` are regenerated by the summary
helper.

Stage controls are conservative and explicit:

| env var | effect |
| --- | --- |
| `GTS_WRINGER_MODE=full-integrity` | Strict full frame-control preset. Defaults variants and first-diff to every selected frame, enables parser progress unless `GTS_WRINGER_PARSE_PROGRESS` is explicitly set, and fail-closes plan assertions when any selected frame lacks a planned baseline, configured variant, or first-diff action. |
| `GTS_WRINGER_PLAN_ONLY=1` | Writes deterministic selected-frame manifests and wringer control artifacts for one grammar without running parser measure, variant, or first-diff children. Use this to choose frames before a targeted run. |
| `GTS_WRINGER_REUSE_BASELINE=1` | Reuses an existing `baseline/` under the output directory instead of rerunning the isolated baseline scan. |
| `GTS_WRINGER_FULL=1` / `GTS_WRINGER_PROFILE=full` | Full/every-frame preset. Defaults variant replay to all selected frames and first-diff replay to `all`; explicit replay selectors and `GTS_WRINGER_VARIANT_SCOPE` override it. |
| `GTS_WRINGER_ASSERTS` | Wrapper assertion gate after final summary. Defaults to `closed,plan,telemetry,artifacts` when summary runs; set `0` to disable. |
| `GTS_WRINGER_HEARTBEAT` | Wringer child heartbeat interval in seconds for delegated baseline, variant, and first-diff actions. Defaults to `15`; set `0` to disable. The baseline stage passes this through as `GTS_TIER_SCAN_HEARTBEAT`. |
| `GTS_WRINGER_PARSE_PROGRESS=1` | Opts wringer measurement children into parser-loop progress by setting `GOT_PARSE_PROGRESS=1` for the delegated baseline tier scan and variant replays. Normal tier scans remain quiet unless `GOT_PARSE_PROGRESS` is set directly. |
| `GTS_WRINGER_PARSE_PROGRESS_INTERVAL_MS` | Optional parser progress interval passed as `GOT_PARSE_PROGRESS_INTERVAL_MS` when nonempty. |
| `GTS_WRINGER_STAGES` | `all` by default, or a comma/space list of `baseline`, `variants`, `firstdiff`, and `summary`. Skipping `baseline` requires existing baseline artifacts. |
| `GTS_WRINGER_BASELINE_FRAMES` | Baseline isolated scan frame selector. Applied after deterministic `GTS_WRINGER_N` selection and preserves original frame ordinals and totals. |
| `GTS_WRINGER_FRAMES` | Fallback selector applied to both variant and first-diff replay when the stage-specific filters are unset. Accepts comma-separated ordinals/ranges, `sha256:<prefix>`, `base:<filename>`, `path:<substring>`, `all`, or `*`. |
| `GTS_WRINGER_VARIANT_FRAMES` | Variant replay frame selector. Defaults to all selected or suspicious files according to `GTS_WRINGER_VARIANT_SCOPE`. |
| `GTS_WRINGER_FIRSTDIFF_FRAMES` | First-diff replay frame selector. When set, it can target any selected baseline frame and runs exactly those explicit frames. When unset, first-diff defaults to suspicious files capped by `GTS_WRINGER_MAX_DIAG_FILES`. |
| `GTS_WRINGER_CLEAR_FRAMES=1` | Ignores `GTS_WRINGER_BASELINE_FRAMES`, `GTS_WRINGER_VARIANT_FRAMES`, `GTS_WRINGER_FIRSTDIFF_FRAMES`, and `GTS_WRINGER_FRAMES` for the current invocation. Use this when rerunning from a shell with stale selector env vars. |
| `GTS_WRINGER_DEBUG_DFA=1` | Adds `REPRO_DEBUG_DFA=1` to first-diff child env for scanner/token proof runs. |

Fresh baseline wringer runs truncate `commands.log` and start a new
`wringer_events.jsonl`. Reuse and summary-only runs preserve append-only
command/event history; they only refresh derived summaries, frame catalogs, and
filtered replay input lists for the current invocation.

Status checks should be fail-closed. During a live replay, `--print-status`
may show `lifecycle open=1` for the active child. After a normal run or after a
completed child frame, `open` should return to `0` and `wringer_active` should
be `state=idle`. If `open` stays nonzero while the active log has stopped
growing, inspect the printed `open_frame` log and the corresponding
`wringer_events.jsonl` START row; the harness writes one terminal `END`,
`FAIL`, or `TIMEOUT` row per child and the exit trap only fills in missing
terminal state for a child that is still in progress.

For a first-diff-only rerun of one suspicious frame:

```sh
GTS_WRINGER_REUSE_BASELINE=1 \
GTS_WRINGER_STAGES=firstdiff,summary \
GTS_WRINGER_FIRSTDIFF_FRAMES=4 \
GTS_WRINGER_MAX_DIAG_FILES=1 \
  bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --label wringer-json-firstdiff-frame4 \
  --mount /path/to/corpus_sources:/workspace/corpus_sources:ro -- \
  "cd /workspace && GTS_CORPUS_DIR=/workspace/corpus_sources cgo_harness/docker/run_grammar_integrity_wringer.sh json cgo_harness/harness_out/wringer-json-frame4"
```

The matching catalog row carries
`replay_plan.baseline_reuse_firstdiff_command` and
`replay_plan.direct_firstdiff_command` for replaying only that ordinal.

The full `selected_files.jsonl` and `suspicious_files.jsonl` lists remain
JSONL records from the summary helper, so file paths with whitespace are not
split by the shell. Filtered replay inputs are written as
`variant_replay_files.jsonl` and `firstdiff_replay_files.jsonl`.

## Parallel worker runs and reducer

The tier scan can be split across disjoint worker run directories and reduced
afterward into one inventory view. The parallel model is intentionally simple:
each worker is just `run_tier_scan.sh` with a non-overlapping
`GTS_TIER_SCAN_LANGS` allowlist and its own output directory. Workers must not
share `tier_scan.txt`, `clean.txt`, `tier_iv.txt`, `unmeasured.txt`,
`visited_grammars.txt`, JSONL telemetry, or manifest files during execution.

The thin orchestrator creates shard allowlists under one parent directory,
runs workers under `workers/shard-...`, and writes the reduced inventory under
`merged/`:

```sh
GTS_CORPUS_DIR=/path/to/gotreesitter-corpora/corpus_sources \
GTS_TIER_SCAN_ISOLATE_FILES=1 \
GTS_TIER_SCAN_SKIP_TIER_PUBLISH=1 \
GTS_TIER_SCAN_PARALLELISM=2 \
GTS_TIER_SCAN_SHARDS=4 \
  cgo_harness/docker/run_tier_scan_parallel.sh \
  cgo_harness/harness_out/tier_scan_parallel/<run-id>
```

Use `--dry-run` to inspect shard membership without launching parser work:

```sh
GTS_TIER_SCAN_LANGS='json toml gitcommit' \
  cgo_harness/docker/run_tier_scan_parallel.sh --dry-run
```

The reducer can also be run directly after manual worker launches:

```sh
python3 cgo_harness/tier_scan/merge_runs.py \
  cgo_harness/harness_out/tier_scan_parallel/<run-id>/merged \
  cgo_harness/harness_out/tier_scan_parallel/<run-id>/workers/shard-000 \
  cgo_harness/harness_out/tier_scan_parallel/<run-id>/workers/shard-001
```

For a live view while workers are still running, use snapshot mode. It reduces
the currently written artifacts, records active worker/checkpoint state, and
does not fail merely because a visited grammar is not classified yet or a
worker has not written `summary.json`:

```sh
python3 cgo_harness/tier_scan/merge_runs.py --allow-incomplete \
  cgo_harness/harness_out/tier_scan_parallel/<run-id>/snapshot \
  cgo_harness/harness_out/tier_scan_parallel/<run-id>/workers/shard-*
```

`merge_runs.py` reduces `tier_scan.txt`, `clean.txt`, `tier_iv.txt`,
`unmeasured.txt`, `visited_grammars.txt`, `events.jsonl`, `frames.jsonl`, and
`manifest.json`, then writes a merged `summary.json`, `resume.env`, and
`active_grammar.txt`. It preserves each worker's per-grammar manifest path in
the merged manifest by adding `worker_run_dir` and `source_manifest` metadata.
If the same grammar appears in multiple workers, the reducer fails clearly
instead of choosing a winner; keep shard allowlists disjoint. The default mode
is still the strict final reducer: missing worker summaries, incomplete worker
states, or visited grammars without a final classification exit nonzero.
Snapshot mode writes `run_state: snapshot-incomplete` and includes
`classified`, `in_progress`/`missing_classification`, and per-worker
`active`/`checkpoint` details in `summary.json`. It also tolerates a malformed
or truncated final line in worker JSONL telemetry, matching the diagnostic
summarizer's live-file behavior. Strict final mode still rejects malformed
JSONL.
Because merged manifests and JSONL rows reference worker directories, archive
the parent parallel run directory rather than copying only `merged/`.

When enough scan artifacts are present, the reducer invokes
`summarize_scan.py` on the merged directory. The diagnostic summary is still
telemetry-only; it does not run parser work. Correctness gating remains the
Docker parity path, one grammar at a time while diagnosing regressions. Perf
validation stays separate from correctness: use the stable benchmark loop only
after parity artifacts are clean enough to justify performance work.

## Published tiers (staged)

A tier-publication release publishes the full 206-grammar tier table at
`cgo_harness/tier_scan/tiers.md` (+ machine-readable `tiers.json`),
regenerated by the scan's final step (`cgo_harness/tier_scan/gen_tiers.py`)
and committed in the release PR. Current parity comes from
`tier_classification.tsv`: `CLEAN` rows are eligible for performance tiers,
and assessed `IV-*` rows publish as tier IV even if the grammar remains in
the historical clean ratchet. Perf ranks come from the local perf-picture
measurements (falling back to `cgo_harness/tier_scan/tier_floors.json`
floors). A parity-clean grammar with no perf evidence publishes as
*unranked* rather than guessing.

With `GTS_TIERS_REQUIRE_ZERO_IV=1` the scan exits 1 while ANY grammar is
tier IV. Once zero-IV publication is activated, the first tier-publishing
release and each subsequent tier-publication refresh require IV=0: all 206
grammars byte-clean against the C oracle.

Set `GTS_TIER_SCAN_SKIP_TIER_PUBLISH=1` for diagnostics, restart probes, and
smoke validation so `cgo_harness/tier_scan/tiers.{md,json}` are not rewritten.

## Files

- `exts.tsv` — grammar → comma-separated source extensions measured for it
  (the lock-filter used by `TestMeasureDtierVsC`).
- `clean_grammars.txt` — the ratchet: grammars that must stay at 100%
  corpus parity. Append newly-clean grammars; never remove one without an
  accepted engineering decision.

## Measurement contract

`TestMeasureDtierVsC` picks the first `GTS_TIER_SCAN_N` (default 40)
path-sorted files between 32B and 200KB per grammar and compares production
`Parser.Parse` output against the linked tree-sitter C grammar node-by-node.
Grammars with no corpus dir, zero eligible files, or a per-grammar timeout
(default 600s) are reported as UNMEASURED — they are not silently dropped.
When `REPRO_FILE` is set, the test measures exactly that file while still
using `REPRO_LANG` to select both the Go grammar and C oracle; this is the
mechanism used by isolated tier-scan mode.
