# Scheduled hard zero-cliff gate

The authoritative real-corpus run is a nightly and manually dispatchable hard
gate on a dedicated `[self-hosted, perf]` runner. The job itself is blocking:
there is no `continue-on-error`, and a single full-parse ratio above `10.0x`,
Go parser/resource stop, incomplete locked-corpus row, or unauthenticated
corpus selection makes the run red.

This is intentionally not a required check on every pull request. A 206-
language largest-file fleet sweep needs the external corpus, C-reference build
cache, multiple GiB of isolated memory, and hours of quiet-box time. The normal
PR lane instead runs the fast budget/status unit tests and validates the
checked-in contract. Performance-sensitive changes should use a manually
dispatched focused run before merge; the next nightly run closes fleet-wide
coverage.

The executable workflow is `.github/workflows/perf-scan-gate.yml`.

## Runner contract

The runner must provide:

- labels `self-hosted` and `perf`;
- Docker and enough disk for C-reference builds;
- at least 8 GiB available to the isolated parity container;
- an uncontended CPU allocation;
- `/srv/gotreesitter-perf/corpus_sources` and
  `/srv/gotreesitter-perf/corpus_sources.lock`.

The lock file is authenticated before the container starts. Its SHA-256 must
match both `perf_scan/corpus_sources.lock.sha256` and the structured budget
metadata. Before timing, the workflow walks all 206 lock rows and requires a
corresponding Git checkout, the exact locked `HEAD`, a resolvable commit
object, no tracked worktree or index changes, and the locked corpus subpath.
The harness then selects every locked language and checks that all locked
languages exist in the grammar registry. Corpus or lock drift is a gate
failure, not an implicit baseline update.

The checkout check intentionally does not reject every untracked path. The
current corpus builder uses untracked nested dependency checkouts for several
languages and deterministic `.gts-extracted/<language>` trees for languages
whose fixtures are embedded inside another syntax. Some lock rows explicitly
select those extracted directories, so `git status --porcelain` cleanliness
would reject the corpus by construction. The scheduled job mounts the whole
corpus read-only and does require the parent tracked tree and index to be clean.
Longer term, the supplemental trees should gain their own content manifest so
their bytes—not only the parent checkout—are independently authenticated.

## Gate semantics

For every selected file:

- the full axis must produce positive Go and C median timings;
- `Go median / C median <= 10.0` passes; exactly `10.0` passes;
- a value above `10.0` fails;
- every Go parser timeout, parser-budget stop, language wall timeout, RSS
  stop, or OOM/kill fails on every axis;
- files at or below `0.10x` are listed separately as 10x-or-better wins.

The sweep remains a timing/resource gate. Structural shape and error-tree
parity stay in the independent correctness suites, so a timing pass never
substitutes for the correctness gate.

## Artifacts and failure handling

The harness writes `scoreboard.json`, `scoreboard.md`, per-language fragments,
and child logs. Artifact upload runs under `if: always()` so a red gate retains
the exact file, axis, implementation, phase, stop class, and partial progress.
The workflow also renders the checked-in status reporter against the new
scoreboard when one exists.

The practical cadence is nightly plus manual dispatch. If the dedicated runner
is temporarily unavailable, the run should remain queued or fail visibly; it
must not silently fall back to a shared hosted runner whose corpus, memory, and
timing characteristics differ. A future artifact-backed corpus can add more
runners without changing the authenticated lock or gate semantics.
