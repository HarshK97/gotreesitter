#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

IMAGE_TAG="gotreesitter/cgo-harness:go1.25-local"
CORPUS_ROOT="${GOT_JAVA_CORPUS_ROOT:-/tmp/gotreesitter-java-corpus/apache-lucene}"
OUT_ROOT=""
LABEL="java-materialization"
MEMORY_LIMIT="4g"
CPUS_LIMIT="1"
PIDS_LIMIT="512"
GO_TEST_TIMEOUT="12m"
BUILD_IMAGE=1
DRY_RUN=0

CORPUS_ORDER="${GOT_JAVA_CORPUS_ORDER:-largest}"
CORPUS_MAX_FILES="${GOT_JAVA_CORPUS_MAX_FILES:-10}"
CORPUS_MAX_FILE_BYTES="${GOT_JAVA_CORPUS_MAX_FILE_BYTES:-300000}"
CORPUS_RANDOM_SEED="${GOT_JAVA_CORPUS_RANDOM_SEED:-}"
BENCH_TIMEOUT="${GOT_JAVA_BENCH_TIMEOUT:-2s}"
BENCH_TIME="30x"
GOMAXPROCS_VALUE="1"

usage() {
  cat <<'EOF'
Usage: run_java_materialization_profiles.sh [options]

Profile Java full-tree DFA against no-tree DFA in a bounded Docker container.
This builds the cgo_harness test binary into the writable artifact directory,
then captures CPU and alloc_space profiles for identical corpus settings.
The historical corpus benchmarks use treesitter_c_bench_legacy and are not a
publication C-oracle lane.

Options:
  --repo-root <path>         Repository/worktree root mounted at /workspace
  --corpus-root <path>       Host Java corpus root mounted at /java-corpus
                              (default: /tmp/gotreesitter-java-corpus/apache-lucene)
  --image <tag>              Docker image tag (default: gotreesitter/cgo-harness:go1.25-local)
  --memory <limit>           Container memory limit (default: 4g)
  --cpus <count>             CPU limit passed to Docker (default: 1)
  --pids <count>             PID limit passed to Docker (default: 512)
  --go-timeout <duration>    test binary -test.timeout value (default: 12m)
  --out-root <path>          Artifact output root (default: <repo-root>/harness_out/profiles)
  --label <name>             Optional run label suffix (default: java-materialization)
  --no-build                 Skip docker build step
  --dry-run                  Print settings without running Docker
  -h, --help                 Show this help

Java corpus knobs:
  --order <path|largest|smallest|random>
  --random-seed <n>
  --max-files <n>
  --max-file-bytes <n>
  --bench-timeout <duration>

Benchmark knobs:
  --benchtime <duration>     test binary -test.benchtime (default: 30x, fixed iterations)
  --gomaxprocs <n>           GOMAXPROCS inside the container (default: 1)

Outputs:
  normal.cpu, notree.cpu, normal.mem, notree.mem
  normal.cpu.top.txt, notree.cpu.top.txt, cpu.diff.top.txt, alloc_space.diff.top.txt
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo-root) REPO_ROOT="$2"; shift 2 ;;
    --corpus-root) CORPUS_ROOT="$2"; shift 2 ;;
    --image) IMAGE_TAG="$2"; shift 2 ;;
    --memory) MEMORY_LIMIT="$2"; shift 2 ;;
    --cpus) CPUS_LIMIT="$2"; shift 2 ;;
    --pids) PIDS_LIMIT="$2"; shift 2 ;;
    --go-timeout) GO_TEST_TIMEOUT="$2"; shift 2 ;;
    --out-root) OUT_ROOT="$2"; shift 2 ;;
    --label) LABEL="$2"; shift 2 ;;
    --order) CORPUS_ORDER="$2"; shift 2 ;;
    --random-seed) CORPUS_RANDOM_SEED="$2"; shift 2 ;;
    --max-files) CORPUS_MAX_FILES="$2"; shift 2 ;;
    --max-file-bytes) CORPUS_MAX_FILE_BYTES="$2"; shift 2 ;;
    --bench-timeout) BENCH_TIMEOUT="$2"; shift 2 ;;
    --benchtime) BENCH_TIME="$2"; shift 2 ;;
    --gomaxprocs) GOMAXPROCS_VALUE="$2"; shift 2 ;;
    --no-build) BUILD_IMAGE=0; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

sanitize_label() {
  local in="$1"
  in="${in,,}"
  in="$(echo "$in" | sed -E 's/[^a-z0-9_.-]+/-/g; s/^-+//; s/-+$//; s/-+/-/g')"
  if [[ -z "$in" ]]; then
    in="run"
  fi
  echo "$in"
}

require_non_negative_int() {
  local name="$1"
  local value="$2"
  if [[ -n "$value" && ! "$value" =~ ^[0-9]+$ ]]; then
    echo "invalid $name: $value (expected non-negative integer)" >&2
    exit 2
  fi
}

case "$CORPUS_ORDER" in
  path|largest|smallest|random) ;;
  *)
    echo "invalid --order: $CORPUS_ORDER (expected path|largest|smallest|random)" >&2
    exit 2
    ;;
esac

require_non_negative_int "--random-seed" "$CORPUS_RANDOM_SEED"
require_non_negative_int "--max-files" "$CORPUS_MAX_FILES"
require_non_negative_int "--max-file-bytes" "$CORPUS_MAX_FILE_BYTES"
require_non_negative_int "--gomaxprocs" "$GOMAXPROCS_VALUE"

REPO_ROOT="${REPO_ROOT/#\~/$HOME}"
CORPUS_ROOT="${CORPUS_ROOT/#\~/$HOME}"
if [[ -z "$OUT_ROOT" ]]; then
  OUT_ROOT="$REPO_ROOT/harness_out/profiles"
fi
OUT_ROOT="${OUT_ROOT/#\~/$HOME}"

if [[ ! -d "$REPO_ROOT" ]]; then
  echo "repo root does not exist: $REPO_ROOT" >&2
  exit 2
fi
REPO_ROOT="$(cd "$REPO_ROOT" && pwd)"
if [[ ! -d "$CORPUS_ROOT" ]]; then
  echo "java corpus root does not exist: $CORPUS_ROOT" >&2
  echo "seed it with: cgo_harness/seed_java_corpus.sh --dest '$CORPUS_ROOT'" >&2
  exit 2
fi
CORPUS_ROOT="$(cd "$CORPUS_ROOT" && pwd)"

LABEL_SLUG="$(sanitize_label "$LABEL")"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="$OUT_ROOT/$STAMP-$LABEL_SLUG"

ENV_ARGS=(
  -e "HOME=/tmp"
  -e "GOCACHE=/tmp/go-build-cache"
  -e "GOMODCACHE=/tmp/go-mod-cache"
  -e "GOFLAGS=-mod=readonly -p=1"
  -e "GOMAXPROCS=$GOMAXPROCS_VALUE"
  -e "GOT_JAVA_CORPUS_ROOT=/java-corpus"
  -e "GOT_JAVA_CORPUS_ORDER=$CORPUS_ORDER"
  -e "GOT_JAVA_CORPUS_MAX_FILES=$CORPUS_MAX_FILES"
  -e "GOT_JAVA_CORPUS_MAX_FILE_BYTES=$CORPUS_MAX_FILE_BYTES"
  -e "GOT_JAVA_BENCH_TIMEOUT=$BENCH_TIMEOUT"
)
[[ -n "$CORPUS_RANDOM_SEED" ]] && ENV_ARGS+=(-e "GOT_JAVA_CORPUS_RANDOM_SEED=$CORPUS_RANDOM_SEED")

DOCKER_ARGS=(
  --rm
  --init
  --memory "$MEMORY_LIMIT"
  --memory-swap "$MEMORY_LIMIT"
  --cpus "$CPUS_LIMIT"
  --pids-limit "$PIDS_LIMIT"
  --user "$(id -u):$(id -g)"
  --mount "type=bind,src=$REPO_ROOT,dst=/workspace,readonly"
  --mount "type=bind,src=$CORPUS_ROOT,dst=/java-corpus,readonly"
  --mount "type=bind,src=$OUT_DIR,dst=/out"
)

echo "java materialization profiles:"
echo "  repo:       $REPO_ROOT -> /workspace (read-only)"
echo "  corpus:     $CORPUS_ROOT -> /java-corpus (read-only)"
echo "  image:      $IMAGE_TAG"
echo "  resources:  memory=$MEMORY_LIMIT cpus=$CPUS_LIMIT pids=$PIDS_LIMIT"
echo "  corpus:     order=$CORPUS_ORDER max_files=$CORPUS_MAX_FILES max_file_bytes=$CORPUS_MAX_FILE_BYTES random_seed=${CORPUS_RANDOM_SEED:-unset}"
echo "  benchmark:  benchtime=$BENCH_TIME timeout=$BENCH_TIMEOUT gomaxprocs=$GOMAXPROCS_VALUE"
echo "  artifacts:  $OUT_DIR"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "dry-run: not building image or starting container"
  exit 0
fi

mkdir -p "$OUT_DIR"

if [[ "$BUILD_IMAGE" == "1" ]]; then
  docker build -t "$IMAGE_TAG" "$SCRIPT_DIR"
fi

docker run "${DOCKER_ARGS[@]}" "${ENV_ARGS[@]}" "$IMAGE_TAG" bash -lc '
  set -euo pipefail
  export PATH=/usr/local/go/bin:$PATH
  cd /workspace/cgo_harness
  go test -c -tags treesitter_c_bench_legacy -o /out/cgo_harness.test .
' 2>&1 | tee "$OUT_DIR/build.log"

run_profile() {
  local name="$1"
  local bench="$2"
  local kind="$3"
  local flags=()
  case "$kind" in
    cpu) flags=(-test.cpuprofile="/out/${name}.cpu") ;;
    mem) flags=(-test.memprofile="/out/${name}.mem" -test.memprofilerate=1) ;;
    *)
      echo "bad profile kind: $kind" >&2
      return 2
      ;;
  esac

  docker run "${DOCKER_ARGS[@]}" "${ENV_ARGS[@]}" "$IMAGE_TAG" bash -lc "
    set -euo pipefail
    cd /workspace/cgo_harness
    /usr/bin/time -v /out/cgo_harness.test \
      -test.run '^$' \
      -test.bench '$bench' \
      -test.benchmem \
      -test.count=1 \
      -test.benchtime '$BENCH_TIME' \
      -test.timeout '$GO_TEST_TIMEOUT' \
      ${flags[*]}
  " 2>&1 | tee "$OUT_DIR/${name}-${kind}.log"
}

run_profile normal '^BenchmarkJavaCorpusGoTreeSitterParseDFA$' cpu
run_profile notree '^BenchmarkJavaCorpusGoTreeSitterParseDFANoTree$' cpu
run_profile normal '^BenchmarkJavaCorpusGoTreeSitterParseDFA$' mem
run_profile notree '^BenchmarkJavaCorpusGoTreeSitterParseDFANoTree$' mem

if command -v go >/dev/null 2>&1; then
  go tool pprof -top -nodecount=40 "$OUT_DIR/cgo_harness.test" "$OUT_DIR/normal.cpu" >"$OUT_DIR/normal.cpu.top.txt"
  go tool pprof -top -nodecount=40 "$OUT_DIR/cgo_harness.test" "$OUT_DIR/notree.cpu" >"$OUT_DIR/notree.cpu.top.txt"
  go tool pprof -top -nodecount=60 -diff_base "$OUT_DIR/notree.cpu" "$OUT_DIR/cgo_harness.test" "$OUT_DIR/normal.cpu" >"$OUT_DIR/cpu.diff.top.txt"
  go tool pprof -top -alloc_space -nodecount=60 -diff_base "$OUT_DIR/notree.mem" "$OUT_DIR/cgo_harness.test" "$OUT_DIR/normal.mem" >"$OUT_DIR/alloc_space.diff.top.txt"
else
  echo "go not found on host; skipping pprof text summaries" | tee "$OUT_DIR/pprof-summary.log"
fi

echo "java materialization profile complete"
echo "artifacts: $OUT_DIR"
