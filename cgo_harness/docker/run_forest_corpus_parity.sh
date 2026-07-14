#!/usr/bin/env bash
# Forest correctness gate: parse authenticated real-corpus inputs with the
# production parser and the GSS-forest fast path, then compare every node, range,
# flag, child, and field on clean, complete forest results. Divergences block
# promotion onto the default-on path. Also reports dispatch rate and wall speedup.
#
# Heavy (real corpus) → runs in Docker per repo testing discipline.
#
# Run with --help for the authenticated one-language interface.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUT_ROOT="$REPO_ROOT/harness_out/forest_corpus"

IMAGE_TAG="gotreesitter/cgo-harness:go1.25-local"
MEMORY_LIMIT="12g"
CPUS_LIMIT="4"
PIDS_LIMIT="4096"
TIMEOUT="20m"
LANGS="bash"
BUILD_IMAGE=1
MANIFEST=""
CORPUS_ROOT=""
CORPUS_LOCK=""
RESULTS_ROOT=""
LEGACY_MANUAL=0
LOCK_SHA256=""
GIT_REVISION=""

usage() {
  cat <<'EOF'
Usage: run_forest_corpus_parity.sh [options]
  --langs <name>     one authenticated language to gate (default: bash)
  --image <tag>      docker image tag (default: gotreesitter/cgo-harness:go1.25-local)
  --memory <limit>   container memory limit (default: 12g)
  --cpus <count>     cpu limit (default: 4)
  --timeout <dur>    go test timeout (default: 20m)
  --manifest <path>  authenticated manifest host path (mounted read-only)
  --corpus-root <p>  locked checkout root host path (mounted read-only)
  --corpus-lock <p>  authenticated corpus_sources.lock host path
  --results-root <p> host root for production/<language>.json
  --lock-sha256 <hex> expected corpus board lock SHA-256 (default: checked-in perf_scan digest)
  --legacy-manual    use the flat corpus explicitly as a non-authoritative manual run
  --no-build         skip docker build step
  -h, --help         show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --langs) LANGS="$2"; shift 2 ;;
    --image) IMAGE_TAG="$2"; shift 2 ;;
    --memory) MEMORY_LIMIT="$2"; shift 2 ;;
    --cpus) CPUS_LIMIT="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --manifest) MANIFEST="$2"; shift 2 ;;
    --corpus-root) CORPUS_ROOT="$2"; shift 2 ;;
    --corpus-lock) CORPUS_LOCK="$2"; shift 2 ;;
    --results-root) RESULTS_ROOT="$2"; shift 2 ;;
    --lock-sha256) LOCK_SHA256="$2"; shift 2 ;;
    --legacy-manual) LEGACY_MANUAL=1; shift ;;
    --no-build) BUILD_IMAGE=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$MANIFEST" && "$LEGACY_MANUAL" != "1" ]]; then
  echo "--manifest is required for an authoritative gate; use --legacy-manual only for non-authoritative inspection" >&2
  exit 2
fi
if [[ -n "$MANIFEST" && "$LEGACY_MANUAL" == "1" ]]; then
  echo "--manifest and --legacy-manual are mutually exclusive" >&2
  exit 2
fi
if [[ -z "$MANIFEST" && -n "$LOCK_SHA256" ]]; then
  echo "--lock-sha256 requires --manifest" >&2
  exit 2
fi
if [[ -n "$MANIFEST" ]]; then
  if [[ ! -f "$MANIFEST" ]]; then
    echo "manifest is not a file: $MANIFEST" >&2
    exit 2
  fi
  MANIFEST="$(realpath "$MANIFEST")"
  if [[ -z "$CORPUS_ROOT" || ! -d "$CORPUS_ROOT" ]]; then
    echo "--corpus-root must name the locked checkout root" >&2
    exit 2
  fi
  CORPUS_ROOT="$(realpath "$CORPUS_ROOT")"
  if [[ -z "$CORPUS_LOCK" || ! -f "$CORPUS_LOCK" ]]; then
    echo "--corpus-lock must name the authenticated corpus lock" >&2
    exit 2
  fi
  CORPUS_LOCK="$(realpath "$CORPUS_LOCK")"
  if [[ "$LANGS" == *,* ]]; then
    echo "authoritative forest gates run exactly one language per container" >&2
    exit 2
  fi
  if [[ ! "$LANGS" =~ ^[A-Za-z0-9][A-Za-z0-9_+-]*$ ]]; then
    echo "--langs must contain one safe language name" >&2
    exit 2
  fi
  if [[ -z "$RESULTS_ROOT" ]]; then
    echo "--results-root is required for authoritative per-language evidence" >&2
    exit 2
  fi
  mkdir -p "$RESULTS_ROOT/production"
  RESULTS_ROOT="$(realpath "$RESULTS_ROOT")"
  if [[ -n "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=normal)" ]]; then
    echo "authoritative forest corpus gates require a clean gotreesitter worktree" >&2
    exit 2
  fi
  GIT_REVISION="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  if [[ -z "$LOCK_SHA256" ]]; then
    read -r LOCK_SHA256 _ < "$REPO_ROOT/cgo_harness/perf_scan/corpus_sources.lock.sha256"
  fi
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="$OUT_ROOT/$STAMP"
mkdir -p "$OUT_DIR"

if [[ "$BUILD_IMAGE" == "1" ]]; then
  docker build -t "$IMAGE_TAG" "$SCRIPT_DIR"
fi

CONTAINER_NAME="gts-forest-corpus-$STAMP"
DOCKER_AUTH_ARGS=(
  --env "GTS_FOREST_CORPUS=1"
  --env "GTS_FOREST_LANGS=$LANGS"
)
if [[ -n "$MANIFEST" ]]; then
  DOCKER_AUTH_ARGS+=(
    --env "GTS_FOREST_CORPUS_MANIFEST=/forest-manifest.json"
    --env "GTS_FOREST_CORPUS_ROOT=/forest-corpus"
    --env "GTS_FOREST_CORPUS_LOCK_PATH=/forest-corpus.lock"
    --env "GTS_FOREST_GOTREESITTER_REVISION=$GIT_REVISION"
    --env "GTS_FOREST_CORPUS_LOCK_SHA256=$LOCK_SHA256"
    --env "GTS_FOREST_AUDIT_RESULT_OUT=/forest-results/production/$LANGS.json"
    --mount "type=bind,src=$MANIFEST,dst=/forest-manifest.json,readonly"
    --mount "type=bind,src=$CORPUS_ROOT,dst=/forest-corpus,readonly"
    --mount "type=bind,src=$CORPUS_LOCK,dst=/forest-corpus.lock,readonly"
    --mount "type=bind,src=$RESULTS_ROOT,dst=/forest-results"
  )
else
  DOCKER_AUTH_ARGS+=(--env "GTS_FOREST_CORPUS_LEGACY_MANUAL=1")
fi
INNER_CMD="cd /workspace/cgo_harness && /usr/bin/time -v go test ./ -run '^TestForestCorpusParity$' -count=1 -timeout $TIMEOUT -v"

CID="$(docker create \
  --name "$CONTAINER_NAME" \
  --init \
  --memory "$MEMORY_LIMIT" \
  --memory-swap "$MEMORY_LIMIT" \
  --cpus "$CPUS_LIMIT" \
  --pids-limit "$PIDS_LIMIT" \
  "${DOCKER_AUTH_ARGS[@]}" \
  --mount "type=bind,src=$REPO_ROOT,dst=/workspace" \
  --mount "type=volume,src=gotreesitter-go-mod-cache,dst=/go/pkg/mod" \
  --mount "type=volume,src=gotreesitter-go-build-cache,dst=/root/.cache/go-build" \
  "$IMAGE_TAG" \
  bash -c "$INNER_CMD")"

docker start "$CID" >/dev/null
docker logs -f "$CID" 2>&1 | tee "$OUT_DIR/container.log"
EXIT_CODE="$(docker wait "$CID")"
OOM_KILLED="$(docker inspect -f '{{.State.OOMKilled}}' "$CID")"
docker rm "$CID" >/dev/null 2>&1 || true

{
  echo "langs=$LANGS"
  echo "image=$IMAGE_TAG"
  echo "memory=$MEMORY_LIMIT"
  echo "manifest=$MANIFEST"
  echo "corpus_root=$CORPUS_ROOT"
  echo "corpus_lock=$CORPUS_LOCK"
  echo "results_root=$RESULTS_ROOT"
  echo "legacy_manual=$LEGACY_MANUAL"
  echo "gotreesitter_revision=$GIT_REVISION"
  echo "corpus_lock_sha256=$LOCK_SHA256"
  echo "exit_code=$EXIT_CODE"
  echo "oom_killed=$OOM_KILLED"
} | tee "$OUT_DIR/summary.txt"

echo "artifacts: $OUT_DIR"
exit "$EXIT_CODE"
