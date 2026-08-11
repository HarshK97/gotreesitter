# Scripts

For heavy correctness, parity, and race work, use CI or the Docker runners under
`cgo_harness/docker` and keep runs to one language at a time. The scripts in
this directory are focused host-side helpers, not the default path for OOM
diagnosis.

`with_grammar_subset.sh` is the host-side low-memory wrapper for focused grammar
work. It forces serial subset builds, wires in external blob loading, and can
point built-in grammar loaders at local grammargen `.bin` overrides.

`prune_harness_artifacts.sh` reports root build products, reproducible caches,
and run receipts separately. It defaults to a dry run. `--delete` removes only
root build products and reproducible caches; durable harness and benchmark
receipts require the separate `--delete-receipts` opt-in. It never includes
private `.gts/`, `.tiller/`, or other agent notes.

`canopy_query.sh` validates the cached index before each structural query. It
uses a fresh scoped query when indexed files or the source set changed.

`refresh_canopy_index.sh` builds a temporary index and validates it. It records
the Git commit only after it promotes the validated index.

`run_randomized_benchmarks.sh` runs the production, incremental, recovery,
replay, compact-core, and corridor benchmarks once per explicit shuffle seed.
Pass each output file to `benchstat` for a before-and-after comparison.

The default set includes:

- the primary full-parse and incremental benchmarks;
- the random-edit and parser-core controls;
- the recovery and synthetic-root replay targets;
- the warm and corridor scheduler lanes;
- the fresh full and selected-store canonical fixtures;
- the tags, legacy fact, and compiled `FactProgram` extraction lanes.

Set `GTS_RECOVERY_CORPUS_FILE` and `GTS_RECOVERY_CORPUS_LANG` to add one exact
corpus file. The script skips `BenchmarkRecoveryCorpusFile` when either value
is absent. Use the same file and language for both comparison runs.

```sh
export GTS_RECOVERY_CORPUS_FILE=/absolute/path/to/corpus-file
export GTS_RECOVERY_CORPUS_LANG=elixir
```

Run both checkouts with the same seed range. Alternate checkout order between
seed batches when the host cannot stay thermally stable.

```sh
bash scripts/run_randomized_benchmarks.sh --output /tmp/gts-base.txt
bash scripts/run_randomized_benchmarks.sh --output /tmp/gts-head.txt
benchstat /tmp/gts-base.txt /tmp/gts-head.txt
```
