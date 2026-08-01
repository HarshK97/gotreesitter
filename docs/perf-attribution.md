# Compact-lane perf attribution board

This document defines one boundary-exact attribution tree over the compact
parser core's fresh-full-parse CPU. It states the disambiguation rule for
every function that more than one component could otherwise claim. It
records the noise floor of the local measurement host, and the first
published receipt.

This is measurement infrastructure only. It changes no parser code, no
routing, and no shipped behavior.

## Why this document exists

Three prior estimates put the compact scheduler's share of full-parse CPU at
77-79%, at approximately 50%, and at 78-82%. Each estimate used a different,
undocumented attribution boundary: one drew the scheduler boundary wide
(covering shift, reduce, and election together), one drew it narrow, and one
used a different profiling method. The three numbers do not contradict each
other once you know their boundaries, but no one had written the boundaries
down, so the campaign could not tell whether a proposed perf lever targeted
77% of parse CPU or 50% of it.

Campaign v7 tranche C0 (`spec.campaign.v7`, `hypha://m31labs/gotreesitter`)
blocks new perf-lever bets until one receipt exists with a named boundary for
every component, a documented noise floor, and a cost-per-event model. This
document is that receipt's home. The attached tool
(`cgo_harness/attribution`) regenerates the receipt from one command.

## Scope

The receipt covers the compact parser core's fresh, non-incremental full
parse: the default route for eligible full parses below the 64 KiB compact
admission floor (`parseRuntimeMemoryMinSourceBytes`,
`admission_switch.go`), and the diagnostic runner path
(`parserCoreFreshFullRunner`) that exercises the same scheduler and
materialization code above that floor.

It does not cover: incremental reparse, error recovery, retry passes, the
production (non-compact) GLR engine, or any language other than the four
pinned Go canonical fixtures.

## The compact lane, by file

| Area | File(s) | Key entry points |
|---|---|---|
| Scheduler driver (root package) | `parsercore_phase0_driver.go` | `run`, `dispatchPass`, `dispatchPassActive`, `elect`, `canonicalize` |
| Core structures (`internal/parsercorephase0`) | `core.go`, `scheduler_owned.go`, `boundary_index.go`, `checkpoint_interner.go` | `ApplySchedulerAtomic`, `condenseWithOutcomeAtomic`, `popPaths`, `Derivations`, `MaterializationOrder` |
| Lexing | `parser_dfa_token_source.go`, `lexer.go` | `(*dfaTokenSource).Next` |
| Runner glue | `parsercore_phase0_fresh_full_runner.go` | `executeSchedulerOpen`, `parse`, `materialize` |
| Shipped-route fidelity tail | `admission_switch.go`, `admission_switch_candidate.go` | `normalizeReturnedTreeForParse`, `resolveCRecoverySwallowedError`, `maybeCompactReturnedFullTree` |

The four canonical fixtures are SHA-256-pinned in
`internal/benchfixtures/testdata` and authenticated the same way
`cgo_harness/pure_c/run_canonical_go_full_parse.sh` and
`diagnosticParserCoreCanonicalAdmissions` authenticate them:

| Fixture | Bytes | Below 64 KiB floor |
|---|---:|---|
| `rewrite.go` | 5,116 | yes |
| `query_compile.go` | 20,168 | yes |
| `language.go` | 41,387 | yes |
| `grammargen/lr.go` | 235,626 | no |

`grammargen/lr.go` never takes the shipped `Parser.Parse` route: it is
diagnostic-lane only. The other three fixtures run through both lanes.

## Two lanes, one methodology

Every fixture profiles under one or both of two lanes. Both lanes time an
identical, already-committed lifecycle; the tool never adds new parser calls.

- **Diagnostic lane.** `runner.parse`, shallow completeness validation, and
  `Tree.Release` — the exact timed region of
  `BenchmarkParserCoreFreshFullCanonical` (build tag
  `gts_parsercorephase0`). Runs for all four fixtures.
- **Shipped route.** `Parser.Parse` and `Tree.Release` — the exact timed
  region of `BenchmarkDiagnosticParserCoreWarmProductionParseQueryCompile`,
  generalized to all three fixtures under the 64 KiB floor. Every sample is
  verified, by the admission-candidate routed/fallback counters, to have
  actually taken the compact route rather than falling back to production.

The tool labels every row with its lane so a reader never mixes shipped-route
public-API overhead into the diagnostic lane's scheduler-only numbers, or
vice versa.

## The attribution tree

Nine components, mutually exclusive and collectively exhaustive. Every
CPU-profile sample lands in exactly one.

1. **scheduler-dispatch** — the scheduler loop, per-token boundary
   classification, and the mechanical application of shift and accept
   actions. Owning functions: `run`, `dispatchPass`, `dispatchPassActive`,
   `applyGenericShifts(Owned)`, `applyGenericExtraShifts(Owned)`,
   `applyGenericAccept`, `finish`, `Core.Shift*`, `Core.ClassifyBoundary`,
   `Core.Actions`, scheduler construction and seeding, and the accepted-tail
   cleanliness check (`requireParserCoreFreshFullAcceptance`,
   `parserCoreFreshFullAcceptedTailIsClean` — see the naming-collision note
   below).
2. **elections** — choosing among competing alternatives: which GSS heads
   advance each round (frontier election), and which action wins at an
   ambiguous cell (GLR conflict resolution / dynamic precedence). Owning
   functions: `elect`, `executeDiagnosticParserCoreGenericConflictDetailed`,
   `applyGenericConflict(Owned)`, `diagnosticParserCoreConflictPolicyOrdinal`,
   `diagnosticParserCoreRepetitionFoldOrdinal`.
3. **reductions-and-pops** — applying a reduce action: enumerating GSS pop
   paths back to a production's children, building the parent subtree, and
   merging (condensing) the result into the derivation graph. Owning
   functions: `applyGenericReduction(Owned)`, `Core.Reduce`,
   `Core.ReduceOutputs*`, `Core.popPaths`, `Core.popSingleLinkPath`,
   `Core.factorExactPredecessor`, `Core.mergePredecessorsBounded`,
   `Core.insertLinkBounded`, `Core.appendNode`, `Core.appendSubtree`,
   `Core.walkCleanPrefixRanks`.
4. **canonicalization** — post-step header-lineage deduplication: merging
   headers that reconverge to the same `(state, byte-offset)` boundary after
   any dispatch action. Owning functions: `canonicalize`,
   `canonicalizeDiagnosticParserCoreHeaders`,
   `diagnosticParserCoreCanonicalScratch.canonicalize(Linear|Mapped)`,
   `mergeDiagnosticParserCoreCleanPathLineage`,
   `persistHeaderLineageOwned`, `RecordReductionLineageOwned`,
   `RecordHeadLineageOwned`.
5. **lexing** — producing the next token from source bytes. Owning function:
   `(*dfaTokenSource).Next`, and every function defined in
   `parser_dfa_token_source.go` or `lexer.go` (a whole-file rule, since both
   files exist for exactly this one job).
6. **materialization** — turning the accepted derivation into the returned
   public `*Tree`: selecting the canonical derivation when more than one
   exists, building nodes, attaching parent/sibling links, replaying parser
   states, and finalizing the root span. Owning functions:
   `completeAcceptance`, `selectCompactAcceptanceDerivation`,
   `Core.Derivations`, `materializeDiagnosticParserCoreAcceptedTree`,
   `materializeDiagnosticParserCoreAcceptedSelection`,
   `finalizeDiagnosticParserCoreAcceptedRootSpan`, `Core.MaterializationOrder`,
   `Core.VisitMaterializationPostorder`, `(*Parser).replayCompactDerivation`.
7. **compat-tail** — the shipped-route-only public-API fidelity tail that
   runs after the compact tree exists, so every `Parser.Parse` return matches
   production's API surface exactly. Not reachable from the diagnostic
   runner path. Owning functions: `normalizeReturnedTreeForParse`,
   `resolveCRecoverySwallowedError`, `maybeCompactReturnedFullTree`,
   `tryCompactFullParseRoute`, `attemptAdmissionCandidateFullParse`,
   `admissionCandidateFullParseEligible` (the whole of `admission_switch.go`
   and `admission_switch_candidate.go` — a whole-file rule).
8. **recovery** — the compact core's native locked-C recovery mechanism
   (error cost, missing-token insertion, retry, and recovery election over
   compact subtrees; campaign v7 tranche B3). Added in tranche B3 stage S1,
   before any recovery engine code exists, per gate G5 ("classifier
   first... so recovery cost can never hide in `other`"). It owns no
   functions yet — compact has no recovery implementation (B3 stages S2-S5
   add error-cost, absorb/condense-resume, election, and missing-token code
   class by class) — so it reads 0.0% on every clean canonical fixture
   today, the same way `compat-tail`'s conditional work reads 0.0% below.
   The whole-file rule for `internal/parsercorephase0/recovery_cost.go`
   (stage S2's planned file) is forward-declared now so landing that file
   does not also require touching this table.
9. **other** — every sample the walk cannot attribute to a named component:
   Go runtime, garbage collection, and goroutine-scheduling frames with no
   gotreesitter-domain ancestor, plus any genuinely unclassified function.
   The tool separately reports the largest unclassified, non-runtime
   functions it saw; a nonzero entry there is a signal this table has a gap,
   not a license to guess.

### A naming collision to avoid

`parserCoreFreshFullAcceptedTailIsClean` checks that no real source byte
follows the accepted head (only parser padding may remain) before the
scheduler declares acceptance. This is **not** the compat-tail component. It
is a scheduler-dispatch acceptance check that runs on both lanes. The English
word "tail" names two unrelated things here: an accepted-frontier byte-range
check (scheduler-dispatch), and a post-parse public-API fidelity pass
(compat-tail, shipped route only). The classifier and this document both use
the full function name, never the bare word "tail", to keep them apart.

## The disambiguation rule

Every named function belongs to at most one component's owned set, chosen by
its primary job, not by which caller happens to invoke it. A handful of
low-level primitives exist purely to serve more than one component
(transaction bookkeeping, checkpoint identity, and pure counters). Those
primitives are explicitly marked shared and are **not** classified directly.

A CPU-profile sample is a full call stack, leaf (currently executing frame)
to root. Attribution walks the stack from the leaf toward the root and
assigns the sample's entire value to the first frame the table recognizes.
A shared primitive is transparent to this walk: the sample "sees through" it
to the nearer ancestor frame, which is always the specific call site that
decided to use the primitive. A sample with no recognized frame anywhere on
its stack is `other`.

This single rule resolves every case where the same function serves two
components:

- **`condense` / `condenseWithOutcomeAtomic`** run from both `Core.Shift*`
  (scheduler-dispatch, appending a shifted GSS node) and reduction-output
  application (reductions-and-pops, appending a reduced parent node). Marked
  shared; the walk finds whichever caller reached it.
- **`ApplySchedulerAtomic` / `RunSchedulerOwned`** wrap the shift, reduce,
  conflict, and extra-shift appliers uniformly (four call sites in
  `parsercore_phase0_driver.go`). Marked shared for the same reason.
- **Checkpoint interning and external-scanner-state capture**
  (`diagnosticParserCoreInternCheckpoint`,
  `captureExternalScannerStateInto`) run both nested inside a live call to
  `dfaTokenSource.Next` (lexing) and directly from `elect` for the
  election's checkpoint-continuity proof (elections). Marked shared; the
  walk finds `Next` when the capture is nested inside token production, and
  finds `elect` when it is not, without any special case.
- **`canonicalize`** is called from inside every applier (accept, shift,
  reduce, conflict, extra-shift) as a post-step. It is deliberately **not**
  marked shared: its job (header-lineage deduplication) is the same
  regardless of caller, so it is classified directly as canonicalization.
  The walk finds `canonicalize`'s own frame before it would reach the
  calling applier's frame, so canonicalization is correctly separated from
  whichever action triggered it.

## The tool

`cgo_harness/attribution` is a small, dependency-free Go program (it lives in
the `cgo_harness` module and imports nothing beyond the standard library,
including its own minimal pprof-profile reader, so this measurement
infrastructure does not widen either module's dependency footprint). It:

1. Extracts and SHA-256-verifies the four canonical fixtures.
2. Builds two test binaries: one tagged `gts_parsercorephase0` (capture and
   noise floor), one tagged `gts_parsercorephase0,gts_workcount` (event
   counts). Building happens once, before any timed measurement, so
   compilation never contends with the host for CPU during a timed sample.
3. Runs `TestParserCoreAttributionCapture` (added in
   `parsercore_phase0_attribution_capture_internal_test.go`, inert unless
   `GTS_ATTRIBUTION_OUT` is set) to collect one gzip'd pprof CPU profile per
   lane per fixture. Every one-time warm pass, digest check, and GC/arena
   drain happens outside `pprof.StartCPUProfile`/`StopCPUProfile`, so the
   profile is not diluted by non-representative setup cost.
4. Runs the existing `TestParserCoreWorkCountChild` (already committed,
   `work_count_parsercore_child_internal_test.go`) once per fixture for
   event counts.
5. Runs the noise floor (see below).
6. Decodes every profile with a minimal, read-only pprof protobuf reader,
   classifies every sample by the rule above, and emits `receipt.json` and
   `receipt.md`.

Reproduce the whole receipt with one command from the repository root:

```sh
(cd cgo_harness && go run ./attribution -repo .. -duration 800ms -noise-samples 10 -noise-benchtime 750ms)
```

Add `-out <dir>` to choose the output directory (default:
`harness_out/perf_attribution/<UTC timestamp>`, already git-ignored). Add
`-core <cpu>` to pin every subprocess with `taskset`; the published receipt
below did not pin a core (see host class, next section).

## Noise floor

**Protocol.** Using the pinned local proxy benchmark
`BenchmarkParserCoreFreshFullCanonical`, the tool builds one test binary and
invokes that identical binary twice per pair, labeled A and B, interleaved
A,B,A,B,... for n pairs (n >= 10). Each invocation runs with `GOMAXPROCS=1`
and times all four fixtures in one process. For each fixture and pair `i`,
`delta_i = |A_i - B_i|`. The noise floor is the 95th percentile (linear
interpolation over the sorted deltas) of `delta_i`, reported both in
nanoseconds and as a percentage of the fixture's median ns/op across the
combined A and B samples.

**Host class.** Shared WSL2 development host, background load uncontrolled.
This is the local-development floor, not the quiet-host or enclave floor
that `BENCH.md`'s sealed epoch uses. Treat every ns/op number in this
document as wall-clock on a busy, shared machine (other agents were active
on the same host during this run), not as a publication-grade performance
claim. The relative component-share percentages are far less sensitive to
this noise than the absolute ns/op and ns-per-event numbers are, because Go's
CPU profiler samples on-CPU (SIGPROF/`ITIMER_PROF`) time, not wall time: host
contention stretches wall-clock ns/op without changing which component a
given on-CPU sample belongs to.

## Cost-per-event join

For each fixture, the tool divides the diagnostic lane's measured wall
ns/op by the `gts_workcount` build's authenticated event counts for that
same fixture and lifecycle (`TestParserCoreWorkCountChild`, schema
`gts-work-count-parsercore-child/v3`): shifts, reductions, emitted pop
paths, emitted pop payloads, canonicalizations, elections, and action
lookups.

This is an **average attribution, not a causal model**. Dividing total wall
time by an event count says nothing about which event class actually causes
the most CPU per occurrence; it says how the average work per fixture spread
across event classes on this run, on this host. Two events of the same class
can cost very different amounts (a pop path over a clean linear GSS chain is
far cheaper than one over a branching, ambiguous chain), and the classes are
not independent (an election's checkpoint-continuity proof is not free, but
it has no "count" of its own in this join).

## Results — first receipt

Generated 2026-08-01T11:12:22Z. Host: shared WSL2 development host,
background load uncontrolled. Other agents were active on the same host
throughout this session (22 registered at session start). Git commit
`f91e9c8cc7d2c8830c973cfa28b658c70dd0d0ba`. Full machine-readable receipt:
`docs/perf-attribution-receipt.json`. Regenerate both (and the larger raw
pprof profiles, which are not committed) with the one command above.

Component shares are computed to sum to exactly 100% of attributed samples
by construction (`other` absorbs the remainder); the displayed 1-decimal
rows are checked to sum to 100% within +/-0.2 percentage points of rounding
tolerance. Every row below is within that tolerance.

### Attribution shares

| lane | fixture | wall ns/op | coverage % | scheduler-dispatch | elections | reductions-and-pops | canonicalization | lexing | materialization | compat-tail | other | sum % |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| diagnostic lane | rewrite | 2.674ms | 105.0 | 22.9 | 9.5 | 33.3 | 4.8 | 14.3 | 13.3 | 0.0 | 1.9 | 100.0 |
| diagnostic lane | query_compile | 12.340ms | 104.8 | 20.8 | 4.7 | 30.2 | 8.5 | 19.8 | 16.0 | 0.0 | 0.0 | 100.0 |
| diagnostic lane | language | 13.195ms | 105.7 | 22.6 | 7.5 | 33.0 | 7.5 | 13.2 | 16.0 | 0.0 | 0.0 | 99.8 |
| diagnostic lane | grammargen_lr | 126.019ms | 106.1 | 25.2 | 7.5 | 22.4 | 5.6 | 19.6 | 17.8 | 0.0 | 1.9 | 100.0 |
| shipped route | rewrite | 2.707ms | 104.8 | 21.9 | 7.6 | 28.6 | 9.5 | 19.0 | 11.4 | 0.0 | 1.9 | 99.9 |
| shipped route | query_compile | 13.026ms | 104.7 | 30.5 | 3.8 | 21.9 | 1.0 | 20.0 | 22.9 | 0.0 | 0.0 | 100.1 |
| shipped route | language | 11.753ms | 105.9 | 25.2 | 4.7 | 30.8 | 6.5 | 15.9 | 16.8 | 0.0 | 0.0 | 99.9 |

`coverage %` is profiled CPU time divided by measured wall time on this
single-core-bound (`GOMAXPROCS=1`) run. It runs 105-106% here, not 100%,
because the Go runtime's background workers (GC assist, sweep) can add a
small amount of concurrent on-CPU time beyond the profiled goroutine's own
wall-clock window. A coverage figure far below 100% would mean too few
samples landed to trust the percentages; every row here is comfortably
above that concern.

`grammargen_lr` has no shipped-route row: it is above the 64 KiB floor, so
`Parser.Parse` never routes it through the compact candidate. `compat-tail`
measures zero on every shipped-route row: for these four fixtures every
parse is clean (no error nodes, no C-recovery-swallowed error to resolve),
so the shipped-route fidelity tail's conditional work never fires. This is
a genuine finding, not a classifier gap: the compat-tail functions exist for
correctness parity on error/recovery paths this receipt does not exercise,
not for cost on the clean path.

`other` stays at 0.0-1.9% on every row. Where it is nonzero, the largest
non-runtime contributors are `(*nodeArena).reset` (arena-pool bookkeeping
inside `Tree.Release`), `hiddenTreeHasFieldIDs`, and
`maxRetainedNodeCapacityForClass` — each under 1% of one profile's samples.
None of them changes the qualitative picture; they are recorded as a known
small residual rather than folded into a component that would overstate its
weight.

### B3 stage S1 addendum: the `recovery` component

The table above predates the `recovery` component (added to
`cgo_harness/attribution/classify.go` in campaign v7 tranche B3 stage S1, per
gate G5) and so has no `recovery` column. A local reproduction of the same
command on stage S1's branch, after the classifier change and before any
recovery engine code, confirmed `recovery` reads exactly 0.0% on every lane
and fixture — diagnostic lane (`rewrite`, `query_compile`, `language`,
`grammargen_lr`) and shipped route (`rewrite`, `query_compile`, `language`)
alike — matching `compat-tail`'s 0.0% pattern above. This is expected: the
component owns no functions yet (B3 stages S2-S5 add error-cost,
absorb/condense-resume, election, and missing-token code class by class), so
no sample can land there. This local run is a verification check, not a new
sealed or C0-authoritative epoch; it does not replace the receipt above or
`docs/perf-attribution-receipt.json`. The next full regeneration (any future
tranche that re-seals this board) will show `recovery` alongside the other
eight components by construction, with no further classifier change needed.

### Noise floor (interleaved A/A, identical binary, 12 pairs per fixture)

| fixture | median ns/op | p95 \|delta\| | p95 \|delta\| as % of median |
|---|---:|---:|---:|
| `grammargen_lr` | 123.099ms | 9.069ms | 7.4% |
| `language` | 12.020ms | 1.299ms | 10.8% |
| `query_compile` | 11.772ms | 1.165ms | 9.9% |
| `rewrite` | 2.401ms | 0.256ms | 10.7% |

Read this as: on this shared, uncontrolled host, two runs of the literal
same binary can disagree by roughly 7-11% before you have measured any real
difference. Any comparison this receipt states below that floor is not a
finding; it is noise.

### Cost per event (diagnostic lane; average, not causal)

| fixture | wall ns/op | ns/shift | ns/reduction | ns/pop path | ns/pop payload | ns/canonicalization | ns/election | ns/action lookup |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `rewrite` | 2.674ms | 1984 | 1778 | 1625 | 894 | 1093 | 2581 | 753 |
| `query_compile` | 12.340ms | 1846 | 1643 | 1522 | 838 | 998 | 2407 | 708 |
| `language` | 13.195ms | 2026 | 1745 | 1575 | 834 | 1071 | 2609 | 707 |
| `grammargen_lr` | 126.019ms | 1906 | 1651 | 1506 | 830 | 1033 | 2525 | 684 |

The top-3 most expensive classes, by ns/event and consistent across all four
fixtures, are elections (2,407-2,609 ns), shifts (1,846-2,026 ns), and
reductions (1,643-1,778 ns). Elections costing more per event than shifts or
reductions is consistent with the shares table: on the diagnostic lane,
elections is the smallest of the four scheduler-family CPU shares
(4.7-9.5%) but fires the fewest times per fixture (1,036 on `rewrite`
versus 3,551 action lookups), so each election carries relatively more
weight. Full JSON: `docs/perf-attribution-receipt.json`.

### Which historical figure this receipt speaks to

Sum the four diagnostic-lane scheduler-family components
(scheduler-dispatch + elections + reductions-and-pops + canonicalization)
without lexing: 70.5% (`rewrite`), 64.2% (`query_compile`), 70.8%
(`language`), 60.7% (`grammargen_lr`) — average 66.5%. That number, on its
own, matches none of the three historical figures well.

Add lexing to that sum (scheduler-dispatch + elections +
reductions-and-pops + canonicalization + lexing): 84.8%, 84.0%, 84.0%,
80.3% — average 83.3%. This closely reproduces the **77-79% and 78-82%**
figures. Read literally, this means those two historical estimates most
likely counted token production as part of "scheduler cost per event" —
a defensible reading, since a shift cannot happen without first lexing the
token it shifts, but one this document did not previously state.

Sum only scheduler-dispatch and reductions-and-pops, the two components that
directly apply a table-driven action (excluding election/conflict overhead,
canonicalization bookkeeping, and lexing): 56.2%, 50.9%, 55.7%, 47.7% —
average 52.6%. This closely reproduces the **approximately-50%** figure. That
estimate most likely measured only the two dominant mechanical appliers and
implicitly folded election, canonicalization, and lexing cost into
"overhead" it did not separately name.

**Call: this receipt retires all three historical figures as decision
inputs**, not because any of them was numerically wrong, but because none of
them stated a boundary a later reader could reproduce or falsify. Each is a
reasonable reading of a different implicit boundary that this receipt now
makes explicit. From this tranche forward, campaign v7 should cite the
eight-component table above — with its stated boundary and disambiguation
rule — instead of any of the three legacy percentages.

## Caveats

- This is one run, on one noisy shared host, not a sealed-epoch,
  quiet-host, or enclave receipt. Treat the noise floor as a floor, not a
  ceiling: a quiet or enclave host would very likely show tighter deltas.
- The classifier's function table
  (`cgo_harness/attribution/classify.go`) is the single source of truth for
  every boundary rule in this document. If the two ever disagree, the code
  is authoritative; file a follow-up to reconcile the prose.
- `other` includes Go runtime and GC frames by design (a legitimate,
  expected component), not only classification gaps. The tool reports the
  largest non-runtime unclassified functions separately so a real gap is
  visible rather than silently absorbed.
- Cost-per-event numbers are an average, not a causal attribution (see
  above). Do not use them alone to justify a specific optimization target;
  pair them with the component shares.
