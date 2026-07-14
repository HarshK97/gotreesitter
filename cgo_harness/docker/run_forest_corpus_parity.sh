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
CPUSET_CPUS=""
HOST_LABEL=""
HOST_FINGERPRINT=""
PIDS_LIMIT="4096"
TIMEOUT="20m"
GOMEMLIMIT_VALUE="10GiB"
LANGS="bash"
BUILD_IMAGE=1
MANIFEST=""
CORPUS_ROOT=""
CORPUS_LOCK=""
RESULTS_ROOT=""
LEGACY_MANUAL=0
LOCK_SHA256=""
GIT_REVISION=""
CONFIRMATION_TRIAL=""
EXECUTION_ORDER="production_first"
REPEAT_COUNT="1"
PREVIOUS_TRIAL_SHA256=""

usage() {
  cat <<'EOF'
Usage: run_forest_corpus_parity.sh [options]
  --langs <name>     one authenticated language to gate (default: bash)
  --image <tag>      docker image tag (default: gotreesitter/cgo-harness:go1.25-local)
  --memory <limit>   container memory limit (default: 12g)
  --cpus <count>     cpu limit (default: 4; confirmation trials require 1)
  --cpuset-cpus <id> pin to one numeric CPU (required for confirmation trials)
  --host-label <name> operator label for the physical host (required for confirmation trials)
  --gomemlimit <v>   GOMEMLIMIT inside the container (default: 10GiB)
  --timeout <dur>    go test timeout (default: 20m)
  --manifest <path>  authenticated manifest host path (mounted read-only)
  --corpus-root <p>  locked checkout root host path (mounted read-only)
  --corpus-lock <p>  authenticated corpus_sources.lock host path
  --results-root <p> host root for production, C-oracle, and confirmation receipts
  --lock-sha256 <hex> expected corpus board lock SHA-256 (default: checked-in perf_scan digest)
  --confirmation-trial <id> write a confirmation envelope (pair-a, pair-b, or ABBA id)
  --execution-order <order>  production_first or routed_first
  --repeat-count <n>         complete corpus repetitions inside the isolated shard (1..20)
  --previous-trial-sha256 <hex> selected predecessor receipt for non-pair-a trials
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
    --cpuset-cpus) CPUSET_CPUS="$2"; shift 2 ;;
    --host-label) HOST_LABEL="$2"; shift 2 ;;
    --hostname) echo "--hostname was replaced by the honest --host-label receipt field" >&2; exit 2 ;;
    --gomemlimit) GOMEMLIMIT_VALUE="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --manifest) MANIFEST="$2"; shift 2 ;;
    --corpus-root) CORPUS_ROOT="$2"; shift 2 ;;
    --corpus-lock) CORPUS_LOCK="$2"; shift 2 ;;
    --results-root) RESULTS_ROOT="$2"; shift 2 ;;
    --lock-sha256) LOCK_SHA256="$2"; shift 2 ;;
    --confirmation-trial) CONFIRMATION_TRIAL="$2"; shift 2 ;;
    --execution-order) EXECUTION_ORDER="$2"; shift 2 ;;
    --repeat-count) REPEAT_COUNT="$2"; shift 2 ;;
    --previous-trial-sha256) PREVIOUS_TRIAL_SHA256="$2"; shift 2 ;;
    --legacy-manual) LEGACY_MANUAL=1; shift ;;
    --no-build) BUILD_IMAGE=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ "$EXECUTION_ORDER" != "production_first" && "$EXECUTION_ORDER" != "routed_first" ]]; then
  echo "--execution-order must be production_first or routed_first" >&2
  exit 2
fi
if ! [[ "$REPEAT_COUNT" =~ ^([1-9]|1[0-9]|20)$ ]]; then
  echo "--repeat-count must be an integer from 1 through 20" >&2
  exit 2
fi
if [[ -n "$CPUSET_CPUS" && ! "$CPUSET_CPUS" =~ ^[0-9]+$ ]]; then
  echo "--cpuset-cpus must name one numeric CPU" >&2
  exit 2
fi
if [[ -n "$HOST_LABEL" && ! "$HOST_LABEL" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
  echo "--host-label contains unsupported characters" >&2
  exit 2
fi
if [[ ! "$MEMORY_LIMIT" =~ ^[0-9]+([kKmMgGtT][bB]?)?$ ]]; then
  echo "--memory must be a Docker byte limit such as 12g" >&2
  exit 2
fi
if [[ ! "$CPUS_LIMIT" =~ ^[0-9]+([.][0-9]+)?$ || "$CPUS_LIMIT" =~ ^0([.]0+)?$ ]]; then
  echo "--cpus must be a positive number" >&2
  exit 2
fi
if [[ ! "$GOMEMLIMIT_VALUE" =~ ^[0-9]+([KMGTP]i?B)?$ ]]; then
  echo "--gomemlimit must be a Go byte limit such as 10GiB" >&2
  exit 2
fi
if [[ ! "$TIMEOUT" =~ ^[0-9]+(ns|us|ms|s|m|h)$ ]]; then
  echo "--timeout must be a single Go duration such as 20m" >&2
  exit 2
fi
if [[ -n "$CONFIRMATION_TRIAL" ]]; then
  if [[ -z "$MANIFEST" ]]; then
    echo "--confirmation-trial requires an authenticated manifest" >&2
    exit 2
  fi
  if [[ -z "$CPUSET_CPUS" || -z "$HOST_LABEL" || "$CPUS_LIMIT" != "1" ]]; then
    echo "confirmation trials require --cpus 1, one numeric --cpuset-cpus, and --host-label" >&2
    exit 2
  fi
  case "$CONFIRMATION_TRIAL:$EXECUTION_ORDER" in
    pair-a:production_first|pair-b:routed_first|abba-a1:production_first|abba-b1:routed_first|abba-b2:routed_first|abba-a2:production_first) ;;
    *) echo "confirmation trial id/order pair is invalid" >&2; exit 2 ;;
  esac
  case "$CONFIRMATION_TRIAL" in
    pair-a)
      if [[ -n "$PREVIOUS_TRIAL_SHA256" ]]; then
        echo "pair-a must not name a predecessor" >&2
        exit 2
      fi
      ;;
    *)
      if [[ ! "$PREVIOUS_TRIAL_SHA256" =~ ^[0-9a-f]{64}$ ]]; then
        echo "non-pair-a confirmation trials require --previous-trial-sha256" >&2
        exit 2
      fi
      ;;
  esac
fi

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
  mkdir -p "$RESULTS_ROOT/production" "$RESULTS_ROOT/c_oracle" \
    "$RESULTS_ROOT/confirmation/trials" "$RESULTS_ROOT/confirmation/cohorts" \
    "$RESULTS_ROOT/confirmation/indexes" "$RESULTS_ROOT/confirmation/run-configs"
  RESULTS_ROOT="$(realpath "$RESULTS_ROOT")"
  if [[ -n "$CONFIRMATION_TRIAL" ]]; then
    if [[ -n "$PREVIOUS_TRIAL_SHA256" && ! -f "$RESULTS_ROOT/confirmation/trials/$PREVIOUS_TRIAL_SHA256.json" ]]; then
      echo "selected predecessor receipt is missing: $RESULTS_ROOT/confirmation/trials/$PREVIOUS_TRIAL_SHA256.json" >&2
      exit 2
    fi
  fi
  if [[ -n "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=normal)" ]]; then
    echo "authoritative forest corpus gates require a clean gotreesitter worktree" >&2
    exit 2
  fi
  GIT_REVISION="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  HOST_FINGERPRINT="$({
    if [[ -r /etc/machine-id ]]; then
      cat /etc/machine-id
    elif [[ -r /sys/class/dmi/id/product_uuid ]]; then
      cat /sys/class/dmi/id/product_uuid
    else
      hostname
    fi
    uname -m
  } | sha256sum | awk '{print $1}')"
  if [[ -z "$LOCK_SHA256" ]]; then
    read -r LOCK_SHA256 _ < "$REPO_ROOT/cgo_harness/perf_scan/corpus_sources.lock.sha256"
  fi
fi

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="$OUT_ROOT/$STAMP"
ATTEMPT_DIR="$OUT_DIR/attempt"
mkdir -p "$ATTEMPT_DIR"

if [[ "$BUILD_IMAGE" == "1" ]]; then
  docker build -t "$IMAGE_TAG" "$SCRIPT_DIR"
fi
IMAGE_ID="$(docker image inspect -f '{{.Id}}' "$IMAGE_TAG")"

RUN_CONFIG_FILE="$OUT_DIR/run-config.json"
cat > "$RUN_CONFIG_FILE" <<EOF
{"schema":"forest-audit-run-config-v1","host_label":"$HOST_LABEL","host_fingerprint":"$HOST_FINGERPRINT","image_id":"$IMAGE_ID","memory":"$MEMORY_LIMIT","cpus":"$CPUS_LIMIT","cpuset_cpus":"$CPUSET_CPUS","pids":"$PIDS_LIMIT","gomemlimit":"$GOMEMLIMIT_VALUE","gomaxprocs":1,"timeout":"$TIMEOUT"}
EOF
RUN_CONFIG_SHA256="$(sha256sum "$RUN_CONFIG_FILE" | awk '{print $1}')"

CONTAINER_NAME="gts-forest-corpus-$STAMP"
DOCKER_AUTH_ARGS=(
  --env "GTS_FOREST_CORPUS=1"
  --env "GTS_FOREST_LANGS=$LANGS"
  --env "GOMAXPROCS=1"
  --env "GOMEMLIMIT=$GOMEMLIMIT_VALUE"
)
if [[ -n "$MANIFEST" ]]; then
  DOCKER_AUTH_ARGS+=(
    --env "GTS_FOREST_CORPUS_MANIFEST=/forest-manifest.json"
    --env "GTS_FOREST_CORPUS_ROOT=/forest-corpus"
    --env "GTS_FOREST_CORPUS_LOCK_PATH=/forest-corpus.lock"
    --env "GTS_FOREST_GOTREESITTER_REVISION=$GIT_REVISION"
    --env "GTS_FOREST_CORPUS_LOCK_SHA256=$LOCK_SHA256"
    --mount "type=bind,src=$MANIFEST,dst=/forest-manifest.json,readonly"
    --mount "type=bind,src=$CORPUS_ROOT,dst=/forest-corpus,readonly"
    --mount "type=bind,src=$CORPUS_LOCK,dst=/forest-corpus.lock,readonly"
    --mount "type=bind,src=$ATTEMPT_DIR,dst=/forest-output"
  )
  if [[ -n "$CONFIRMATION_TRIAL" ]]; then
    DOCKER_AUTH_ARGS+=(
      --env "GTS_FOREST_AUDIT_CONFIRMATION_OUT=/forest-output/trial.json"
      --env "GTS_FOREST_AUDIT_TRIAL_ID=$CONFIRMATION_TRIAL"
      --env "GTS_FOREST_AUDIT_EXECUTION_ORDER=$EXECUTION_ORDER"
      --env "GTS_FOREST_AUDIT_REPEAT_COUNT=$REPEAT_COUNT"
      --env "GTS_FOREST_AUDIT_RUN_CONFIG_SHA256=$RUN_CONFIG_SHA256"
      --env "GTS_FOREST_AUDIT_PREVIOUS_TRIAL_SHA256=$PREVIOUS_TRIAL_SHA256"
    )
  else
    DOCKER_AUTH_ARGS+=(--env "GTS_FOREST_AUDIT_RESULT_OUT=/forest-output/result.json")
  fi
else
  DOCKER_AUTH_ARGS+=(--env "GTS_FOREST_CORPUS_LEGACY_MANUAL=1")
fi
INNER_CMD="cd /workspace/cgo_harness && /usr/bin/time -v go test ./ -run '^TestForestCorpusParity$' -count=1 -timeout $TIMEOUT -v"

DOCKER_PLACEMENT_ARGS=()
if [[ -n "$CPUSET_CPUS" ]]; then
  DOCKER_PLACEMENT_ARGS+=(--cpuset-cpus "$CPUSET_CPUS")
fi

CID="$(docker create \
  --name "$CONTAINER_NAME" \
  --init \
  --memory "$MEMORY_LIMIT" \
  --memory-swap "$MEMORY_LIMIT" \
  --cpus "$CPUS_LIMIT" \
  --pids-limit "$PIDS_LIMIT" \
  "${DOCKER_PLACEMENT_ARGS[@]}" \
  "${DOCKER_AUTH_ARGS[@]}" \
  --mount "type=bind,src=$REPO_ROOT,dst=/workspace,readonly" \
  --mount "type=volume,src=gotreesitter-go-mod-cache,dst=/go/pkg/mod" \
  --mount "type=volume,src=gotreesitter-go-build-cache,dst=/root/.cache/go-build" \
  "$IMAGE_ID" \
  bash -c "$INNER_CMD")"
CONTAINER_IMAGE_ID="$(docker inspect -f '{{.Image}}' "$CID")"
if [[ "$CONTAINER_IMAGE_ID" != "$IMAGE_ID" ]]; then
  docker rm "$CID" >/dev/null 2>&1 || true
  echo "container image drifted: recorded $IMAGE_ID, created $CONTAINER_IMAGE_ID" >&2
  exit 2
fi

docker start "$CID" >/dev/null
docker logs -f "$CID" 2>&1 | tee "$OUT_DIR/container.log"
EXIT_CODE="$(docker wait "$CID")"
OOM_KILLED="$(docker inspect -f '{{.State.OOMKilled}}' "$CID")"
docker rm "$CID" >/dev/null 2>&1 || true

if [[ "$EXIT_CODE" == "0" && -n "$MANIFEST" ]]; then
  AFTER_REVISION="$(git -C "$REPO_ROOT" rev-parse HEAD)"
  AFTER_STATUS="$(git -C "$REPO_ROOT" status --porcelain --untracked-files=normal)"
  if [[ "$AFTER_REVISION" != "$GIT_REVISION" || -n "$AFTER_STATUS" ]]; then
    echo "gotreesitter worktree changed during authoritative run; staged evidence was not published" >&2
    EXIT_CODE=2
  elif [[ -n "$CONFIRMATION_TRIAL" ]]; then
    if [[ ! -f "$ATTEMPT_DIR/trial.json" ]]; then
      echo "successful confirmation run produced no staged trial" >&2
      EXIT_CODE=2
    else
      PUBLISH_OUTPUT="$(cd "$REPO_ROOT/cgo_harness" && go run ./cmd/forest_audit publish-confirmation \
        --results-root "$RESULTS_ROOT" --run-config "$RUN_CONFIG_FILE" --trial "$ATTEMPT_DIR/trial.json")" || EXIT_CODE=$?
      if [[ "$EXIT_CODE" == "0" ]]; then
        printf '%s\n' "$PUBLISH_OUTPUT" | tee "$OUT_DIR/published-confirmation.txt"
      fi
    fi
  else
    if [[ ! -f "$ATTEMPT_DIR/result.json" ]]; then
      echo "successful forest screen produced no staged result" >&2
      EXIT_CODE=2
    else
      PUBLISH_OUTPUT="$(cd "$REPO_ROOT/cgo_harness" && go run ./cmd/forest_audit publish-result \
        --results-root "$RESULTS_ROOT" --result "$ATTEMPT_DIR/result.json")" || EXIT_CODE=$?
      if [[ "$EXIT_CODE" == "0" ]]; then
        printf '%s\n' "$PUBLISH_OUTPUT" | tee "$OUT_DIR/published-result.txt"
      fi
    fi
  fi
fi

{
  echo "langs=$LANGS"
  echo "image=$IMAGE_TAG"
  echo "image_id=$IMAGE_ID"
  echo "container_image_id=$CONTAINER_IMAGE_ID"
  echo "memory=$MEMORY_LIMIT"
  echo "cpus=$CPUS_LIMIT"
  echo "cpuset_cpus=$CPUSET_CPUS"
  echo "host_label=$HOST_LABEL"
  echo "host_fingerprint=$HOST_FINGERPRINT"
  echo "gomemlimit=$GOMEMLIMIT_VALUE"
  echo "manifest=$MANIFEST"
  echo "corpus_root=$CORPUS_ROOT"
  echo "corpus_lock=$CORPUS_LOCK"
  echo "results_root=$RESULTS_ROOT"
  echo "legacy_manual=$LEGACY_MANUAL"
  echo "gotreesitter_revision=$GIT_REVISION"
  echo "corpus_lock_sha256=$LOCK_SHA256"
  echo "confirmation_trial=$CONFIRMATION_TRIAL"
  echo "execution_order=$EXECUTION_ORDER"
  echo "repeat_count=$REPEAT_COUNT"
  echo "run_config_sha256=$RUN_CONFIG_SHA256"
  echo "previous_trial_sha256=$PREVIOUS_TRIAL_SHA256"
  echo "exit_code=$EXIT_CODE"
  echo "oom_killed=$OOM_KILLED"
} | tee "$OUT_DIR/summary.txt"

echo "artifacts: $OUT_DIR"
exit "$EXIT_CODE"
