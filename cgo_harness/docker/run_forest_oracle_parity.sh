#!/usr/bin/env bash
# Forest-vs-C ORACLE gate. Compares the GSS-forest fast path DIRECTLY against
# tree-sitter-c, skipping the production parser — so a language whose production
# parse is too slow to use as the parity baseline (haskell's O(n^2) deep-merge
# blowup times out the production-vs-forest gate, run_forest_corpus_parity.sh)
# can still be vetted for forest promotion. A per-file budget keeps a forest that
# itself blows up (haskell's forestReducer.dfs) from hanging the run; such files
# are reported as fallback reason "timeout".
#
# Heavy (real corpus + CGo) -> runs in Docker per repo testing discipline.
#
# Run with --help for the authenticated one-language interface.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUT_ROOT="$REPO_ROOT/harness_out/forest_oracle"

IMAGE_TAG="gotreesitter/cgo-harness:go1.25-local"
MEMORY_LIMIT="8g"
CPUS_LIMIT="4"
PIDS_LIMIT="4096"
TIMEOUT="10m"
LANGS="haskell"
BUDGET="10s"
BUILD_IMAGE=1
MANIFEST=""
CORPUS_ROOT=""
CORPUS_LOCK=""
RESULTS_ROOT=""
LOCK_SHA256=""
GIT_REVISION=""

usage() {
  cat <<'EOF'
Usage: run_forest_oracle_parity.sh [options]
  --langs <name>     one language to gate (default: haskell)
  --budget <dur>     per-file forest budget (default: 10s)
  --image <tag>      docker image tag (default: gotreesitter/cgo-harness:go1.25-local)
  --memory <limit>   container memory limit (default: 8g)
  --cpus <count>     cpu limit (default: 4)
  --timeout <dur>    go test timeout (default: 10m)
  --manifest <path>  authenticated manifest host path (mounted read-only)
  --corpus-root <p>  locked checkout root host path (mounted read-only)
  --corpus-lock <p>  authenticated corpus_sources.lock host path
  --results-root <p> host root for c_oracle/<language>.json
  --lock-sha256 <h>  expected corpus board lock SHA-256 (default: checked-in perf_scan digest)
  --no-build         skip docker build step
  -h, --help         show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --langs) LANGS="$2"; shift 2 ;;
    --budget) BUDGET="$2"; shift 2 ;;
    --image) IMAGE_TAG="$2"; shift 2 ;;
    --memory) MEMORY_LIMIT="$2"; shift 2 ;;
    --cpus) CPUS_LIMIT="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --manifest) MANIFEST="$2"; shift 2 ;;
    --corpus-root) CORPUS_ROOT="$2"; shift 2 ;;
    --corpus-lock) CORPUS_LOCK="$2"; shift 2 ;;
    --results-root) RESULTS_ROOT="$2"; shift 2 ;;
    --lock-sha256) LOCK_SHA256="$2"; shift 2 ;;
    --no-build) BUILD_IMAGE=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$MANIFEST" || ! -f "$MANIFEST" ]]; then
  echo "--manifest must name an authenticated forest corpus manifest" >&2
  exit 2
fi
if [[ -z "$CORPUS_ROOT" || ! -d "$CORPUS_ROOT" ]]; then
  echo "--corpus-root must name the locked checkout root" >&2
  exit 2
fi
if [[ -z "$CORPUS_LOCK" || ! -f "$CORPUS_LOCK" ]]; then
  echo "--corpus-lock must name the authenticated corpus lock" >&2
  exit 2
fi
if [[ -z "$RESULTS_ROOT" ]]; then
  echo "--results-root is required for authoritative per-language evidence" >&2
  exit 2
fi
if [[ "$LANGS" == *,* ]]; then
  echo "authoritative forest gates run exactly one language per container" >&2
  exit 2
fi
if [[ ! "$LANGS" =~ ^[A-Za-z0-9][A-Za-z0-9_+-]*$ ]]; then
  echo "--langs must contain one safe language name" >&2
  exit 2
fi
if [[ -n "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=normal)" ]]; then
  echo "authoritative forest oracle gates require a clean gotreesitter worktree" >&2
  exit 2
fi
MANIFEST="$(realpath "$MANIFEST")"
CORPUS_ROOT="$(realpath "$CORPUS_ROOT")"
CORPUS_LOCK="$(realpath "$CORPUS_LOCK")"
mkdir -p "$RESULTS_ROOT/c_oracle"
RESULTS_ROOT="$(realpath "$RESULTS_ROOT")"
GIT_REVISION="$(git -C "$REPO_ROOT" rev-parse HEAD)"
if [[ -z "$LOCK_SHA256" ]]; then
  read -r LOCK_SHA256 _ < "$REPO_ROOT/cgo_harness/perf_scan/corpus_sources.lock.sha256"
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="$OUT_ROOT/$STAMP"
mkdir -p "$OUT_DIR"

if [[ "$BUILD_IMAGE" == "1" ]]; then
  docker build -f "$SCRIPT_DIR/Dockerfile" -t "$IMAGE_TAG" "$REPO_ROOT" \
    > "$OUT_DIR/build.log" 2>&1 || { echo "image build failed; see $OUT_DIR/build.log" >&2; exit 1; }
fi

set +e
docker run --rm \
  --mount "type=bind,src=$REPO_ROOT,dst=/workspace" \
  --mount "type=volume,src=gotreesitter-go-mod-cache,dst=/go/pkg/mod" \
  --mount "type=volume,src=gotreesitter-go-build-cache,dst=/root/.cache/go-build" \
  -w /workspace/cgo_harness \
  -e GTS_FOREST_ORACLE=1 \
  -e "GTS_FOREST_ORACLE_LANGS=$LANGS" \
  -e "GTS_FOREST_ORACLE_BUDGET=$BUDGET" \
  -e GTS_FOREST_CORPUS_MANIFEST=/forest-manifest.json \
  -e GTS_FOREST_CORPUS_ROOT=/forest-corpus \
  -e GTS_FOREST_CORPUS_LOCK_PATH=/forest-corpus.lock \
  -e "GTS_FOREST_GOTREESITTER_REVISION=$GIT_REVISION" \
  -e "GTS_FOREST_CORPUS_LOCK_SHA256=$LOCK_SHA256" \
  -e "GTS_FOREST_AUDIT_RESULT_OUT=/forest-results/c_oracle/$LANGS.json" \
  --mount "type=bind,src=$MANIFEST,dst=/forest-manifest.json,readonly" \
  --mount "type=bind,src=$CORPUS_ROOT,dst=/forest-corpus,readonly" \
  --mount "type=bind,src=$CORPUS_LOCK,dst=/forest-corpus.lock,readonly" \
  --mount "type=bind,src=$RESULTS_ROOT,dst=/forest-results" \
  --memory "$MEMORY_LIMIT" --cpus "$CPUS_LIMIT" --pids-limit "$PIDS_LIMIT" \
  "$IMAGE_TAG" \
  bash -c "go test . -tags treesitter_c_parity -run '^TestForestVsCOracleParity\$' -count=1 -v -timeout $TIMEOUT" \
  2>&1 | tee "$OUT_DIR/container.log"
code=${PIPESTATUS[0]}
set -e

{
  echo "langs=$LANGS"
  echo "budget=$BUDGET"
  echo "image=$IMAGE_TAG"
  echo "manifest=$MANIFEST"
  echo "corpus_root=$CORPUS_ROOT"
  echo "corpus_lock=$CORPUS_LOCK"
  echo "results_root=$RESULTS_ROOT"
  echo "gotreesitter_revision=$GIT_REVISION"
  echo "corpus_lock_sha256=$LOCK_SHA256"
  echo "exit_code=$code"
} > "$OUT_DIR/summary.txt"

echo "forest oracle parity complete -> $OUT_DIR"
exit "$code"
