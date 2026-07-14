#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

SOURCE_ROOT="${GOT_JAVA_CORPUS_ROOT:-/tmp/gotreesitter-java-corpus/apache-lucene}"
FIXTURE_ROOT="${GOT_JAVA_UAX_FIXTURE_ROOT:-/tmp/gotreesitter-java-uax-fixture}"
UAX_REL="lucene/analysis/common/src/java/org/apache/lucene/analysis/email/UAX29URLEmailTokenizerImpl.java"
RUN_MODE="both"
TIMEOUT_SWEEP="100ms,0"
PARSE_MODES="dfa,dfa_no_tree,token_source,aspect_fallback"
BENCH_REGEX='BenchmarkJavaCorpus(GoTreeSitterParseDFA|GoTreeSitterParseDFANoTree|GoTreeSitterParseTokenSource|GoTreeSitterParseAspectFallback|CTreeSitterParseFull)$'
BENCH_COUNT="10"
BENCH_TIME="750ms"
MAX_ISSUES="5"

COMMON_ARGS=()

usage() {
  cat <<'EOF'
Usage: run_java_uax_stress.sh [options]

Run the generated Lucene UAX29URLEmailTokenizerImpl.java stress lane in Docker.
The source file is copied from the external seeded Lucene corpus into a one-file
temporary corpus. The Java file is not vendored into this repository.
The underlying historical corpus probe uses treesitter_c_bench_legacy and is
not a publication C-oracle lane.

Options:
  --mode <name>              Run mode: timeout-sweep|benchmark|both (default: both)
  --source-root <path>       Seeded Lucene checkout
                              (default: /tmp/gotreesitter-java-corpus/apache-lucene)
  --fixture-root <path>      One-file fixture directory
                              (default: /tmp/gotreesitter-java-uax-fixture)
  --timeout-sweep <list>     Timeout sweep for timeout-sweep mode (default: 100ms,0)
  --parse-modes <list>       Parse modes for timeout-sweep mode
                              (default: dfa,dfa_no_tree,token_source,aspect_fallback)
  --bench <regex>            Benchmark regex for benchmark mode
  --bench-count <n>          go test benchmark -count (default: 10)
  --benchtime <duration>     go test -benchtime (default: 750ms)
  --max-issues <n>           Timeout issue count captured by the harness (default: 5)
  --image <tag>              Docker image tag passed through to run_java_corpus_probe.sh
  --memory <limit>           Docker memory limit passed through
  --cpus <count>             Docker CPU limit passed through
  --pids <count>             Docker PID limit passed through
  --wall-timeout <duration>  Host-side wall deadline passed through
  --kill-grace <duration>    Grace period after wall timeout passed through
  --go-timeout <duration>    go test -timeout value passed through
  --out-root <path>          Artifact output root passed through
  --gomaxprocs <n>           GOMAXPROCS inside the container
  --no-build                 Skip docker image build
  --dry-run                  Print underlying Docker commands without running them
  -h, --help                 Show this help

Seed the public corpus first when needed:
  cgo_harness/seed_java_corpus.sh
EOF
}

need_value() {
  local opt="$1"
  if [[ $# -lt 2 || -z "${2:-}" ]]; then
    echo "missing value for $opt" >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      need_value "$@"
      RUN_MODE="$2"
      shift 2
      ;;
    --source-root)
      need_value "$@"
      SOURCE_ROOT="$2"
      shift 2
      ;;
    --fixture-root)
      need_value "$@"
      FIXTURE_ROOT="$2"
      shift 2
      ;;
    --timeout-sweep)
      need_value "$@"
      TIMEOUT_SWEEP="$2"
      shift 2
      ;;
    --parse-modes)
      need_value "$@"
      PARSE_MODES="$2"
      shift 2
      ;;
    --bench)
      need_value "$@"
      BENCH_REGEX="$2"
      shift 2
      ;;
    --bench-count)
      need_value "$@"
      BENCH_COUNT="$2"
      shift 2
      ;;
    --benchtime)
      need_value "$@"
      BENCH_TIME="$2"
      shift 2
      ;;
    --max-issues)
      need_value "$@"
      MAX_ISSUES="$2"
      shift 2
      ;;
    --image|--memory|--cpus|--pids|--wall-timeout|--kill-grace|--go-timeout|--out-root|--gomaxprocs)
      need_value "$@"
      COMMON_ARGS+=("$1" "$2")
      shift 2
      ;;
    --no-build|--dry-run)
      COMMON_ARGS+=("$1")
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$RUN_MODE" in
  timeout-sweep|benchmark|both) ;;
  *)
    echo "invalid --mode: $RUN_MODE (expected timeout-sweep|benchmark|both)" >&2
    exit 2
    ;;
esac

SOURCE_ROOT="${SOURCE_ROOT/#\~/$HOME}"
FIXTURE_ROOT="${FIXTURE_ROOT/#\~/$HOME}"
SOURCE_FILE="$SOURCE_ROOT/$UAX_REL"

if [[ ! -f "$SOURCE_FILE" ]]; then
  echo "UAX stress source file does not exist: $SOURCE_FILE" >&2
  echo "seed it with: cgo_harness/seed_java_corpus.sh --dest '$SOURCE_ROOT'" >&2
  exit 2
fi

case "$FIXTURE_ROOT" in
  ""|"/")
    echo "refusing unsafe fixture root: $FIXTURE_ROOT" >&2
    exit 2
    ;;
esac

rm -rf "$FIXTURE_ROOT"
mkdir -p "$FIXTURE_ROOT/lucene/analysis/email"
cp "$SOURCE_FILE" "$FIXTURE_ROOT/lucene/analysis/email/UAX29URLEmailTokenizerImpl.java"

read -r fixture_bytes < <(wc -c <"$FIXTURE_ROOT/lucene/analysis/email/UAX29URLEmailTokenizerImpl.java")

echo "java UAX stress fixture:"
echo "  source:  $SOURCE_FILE"
echo "  fixture: $FIXTURE_ROOT"
echo "  bytes:   $fixture_bytes"

run_timeout_sweep() {
  GOT_JAVA_AMBIGUITY_PROFILE="${GOT_JAVA_AMBIGUITY_PROFILE:-1}" \
    "$SCRIPT_DIR/run_java_corpus_probe.sh" \
      --mode timeout-sweep \
      --corpus-root "$FIXTURE_ROOT" \
      --order path \
      --timeout-sweep "$TIMEOUT_SWEEP" \
      --parse-modes "$PARSE_MODES" \
      --max-issues "$MAX_ISSUES" \
      --label java-uax-single-matrix \
      "${COMMON_ARGS[@]}"
}

run_benchmark() {
  "$SCRIPT_DIR/run_java_corpus_probe.sh" \
    --mode benchmark \
    --corpus-root "$FIXTURE_ROOT" \
    --order path \
    --bench "$BENCH_REGEX" \
    --bench-count "$BENCH_COUNT" \
    --benchtime "$BENCH_TIME" \
    --label java-uax-single-bench \
    "${COMMON_ARGS[@]}"
}

case "$RUN_MODE" in
  timeout-sweep)
    run_timeout_sweep
    ;;
  benchmark)
    run_benchmark
    ;;
  both)
    run_timeout_sweep
    run_benchmark
    ;;
esac
