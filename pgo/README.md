# Build-time PGO profile

`default.pgo` is a CPU profile collected from `cmd/pgo_repdriver`, a driver
that parses a representative multi-grammar spread of real source files
(go, c_sharp, bash, python, cmake) pulled from `cgo_harness/corpus_real`.
Go's compiler uses it to guide inlining/devirtualization decisions
(profile-guided optimization, PGO) when a binary is built with
`-pgo=pgo/default.pgo` (or with this file copied next to that binary's own
main package for auto-discovery).

Measured effect (see the `rowan/pgo` PR description for full methodology):
on a fixed multi-grammar parse workload, PGO builds were consistently
**~7% faster** (wall clock, medians, order-alternated interleaved A/B,
n=20, p=0.001) than non-PGO builds, with byte-identical parse trees
(`SExpr` digest diffed clean between PGO and non-PGO builds) and identical
`go test` outcomes either way. PGO only changes codegen, never parse
semantics — this is an ungated, correctness-neutral perf win.

## Regenerating the profile

The profile should be regenerated whenever the parser/GLR/lexer hot path
changes meaningfully (new grammar families with materially different
parsing costs, GLR algorithm rewrites, lexer/scanner rewrites). Stale
profiles are not harmful (Go just guides on slightly outdated hot-path
data), but keeping it fresh keeps the win honest.

1. Build the real-file corpus if you don't already have one checked out
   locally (`cgo_harness/corpus_real/` is gitignored and rebuilt from the
   lock file, not committed). `cgo_harness` is its own Go module, so run
   this from inside `cgo_harness/`, not the repo root:

   ```sh
   cd cgo_harness
   go run ./cmd/build_real_corpus -out corpus_real -langs go,c_sharp,bash,python,cmake
   # -langs all (or top50) rebuilds the full corpus; see -h for lock/output flags.
   cd ..
   ```

2. Collect a fresh profile from the driver, from the repo root:

   ```sh
   go run ./cmd/pgo_repdriver -mode=profile -cpuprofile=pgo/default.pgo -iterations=1500
   ```

3. Sanity-check correctness didn't move (should always print nothing):

   ```sh
   go build -pgo=off -o /tmp/digest_nopgo ./cmd/pgo_repdriver
   go build -pgo=pgo/default.pgo -o /tmp/digest_pgo ./cmd/pgo_repdriver
   diff <(/tmp/digest_nopgo -mode=digest) <(/tmp/digest_pgo -mode=digest)
   ```

4. Commit the updated `pgo/default.pgo` (it's a small binary file, no
   `.gitignore` rule excludes it).

## Where it's wired in

- `cmd/parity_report`, invoked by CI (`.github/workflows/ci.yml`,
  `parity_report` job) against all 206 registered grammars on every PR, is
  built with `-pgo=pgo/default.pgo` — the closest thing this repo has to a
  representative "production" binary run in its own pipeline.
- gotreesitter itself ships as a Go module (no compiled release binary or
  release Dockerfile lives in this repo); downstream consumers who build
  their own binary against this module get the PGO win only if *they*
  build with `-pgo=<path>` pointing at a copy of this profile (or their own
  profile) — Go's PGO auto-discovery only looks in the *building* binary's
  own main package directory, not inside imported module dependencies.
  Projects that embed gotreesitter for production parsing (e.g. the
  separate API service) should copy `pgo/default.pgo` from a pinned
  gotreesitter version into their own main package directory, or pass
  `-pgo=<path-to-this-file>` explicitly in their build.
- Other `cmd/*` tools in this repo (`tsquery`, `grammargen`, `ts2go`, etc.)
  are not yet wired; they're dev tooling, not run at meaningful volume in
  CI, so the win there is negligible. Wire them the same way
  (`-pgo=pgo/default.pgo` or a co-located `default.pgo`) if that changes.
