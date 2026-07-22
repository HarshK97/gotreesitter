#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 validate <shard-count> | names <shard-index> <shard-count> | regex <shard-index> <shard-count>" >&2
  exit 2
}

require_uint() {
  local value="$1"
  [[ "$value" =~ ^[0-9]+$ ]] || usage
}

load_targets() {
  mapfile -t root_targets < <(
    go test -race . -list '^(Test|Example|Fuzz)' |
      awk '/^(Test|Example|Fuzz)/ { print }' |
      LC_ALL=C sort -u
  )
  if [ "${#root_targets[@]}" -eq 0 ]; then
    echo "root race shard selector found no top-level tests, examples, or fuzz targets" >&2
    exit 1
  fi
}

validate_partition() {
  local shard_count="$1"
  local -a counts=()
  local -A seen=()
  local position shard name

  for ((shard = 0; shard < shard_count; shard++)); do
    counts[shard]=0
  done
  for position in "${!root_targets[@]}"; do
    shard=$((position % shard_count))
    name="${root_targets[position]}"
    counts[shard]=$((counts[shard] + 1))
    seen["$name"]=$(( ${seen["$name"]:-0} + 1 ))
  done

  for name in "${root_targets[@]}"; do
    if [ "${seen["$name"]}" -ne 1 ]; then
      echo "root race partition assigned $name ${seen["$name"]} times" >&2
      exit 1
    fi
  done

  local min="${counts[0]}"
  local max="${counts[0]}"
  for count in "${counts[@]}"; do
    (( count < min )) && min="$count"
    (( count > max )) && max="$count"
  done
  if (( max - min > 1 )); then
    echo "root race partition is not count-balanced: ${counts[*]}" >&2
    exit 1
  fi
  echo "root race partition valid: ${#root_targets[@]} tests/examples/fuzz targets across ${shard_count} shards (${counts[*]})" >&2
}

selected_names() {
  local shard_index="$1"
  local shard_count="$2"
  local position
  for position in "${!root_targets[@]}"; do
    if (( position % shard_count == shard_index )); then
      printf '%s\n' "${root_targets[position]}"
    fi
  done
}

mode="${1:-}"
case "$mode" in
  validate)
    [ "$#" -eq 2 ] || usage
    require_uint "$2"
    (( $2 > 0 )) || usage
    load_targets
    validate_partition "$2"
    ;;
  names|regex)
    [ "$#" -eq 3 ] || usage
    require_uint "$2"
    require_uint "$3"
    (( $3 > 0 && $2 < $3 )) || usage
    load_targets
    validate_partition "$3"
    mapfile -t selected < <(selected_names "$2" "$3")
    if [ "${#selected[@]}" -eq 0 ]; then
      echo "root race shard $2/$3 is empty" >&2
      exit 1
    fi
    if [ "$mode" = "names" ]; then
      printf '%s\n' "${selected[@]}"
      exit 0
    fi
    pattern="$(IFS='|'; echo "${selected[*]}")"
    printf '^(%s)$\n' "$pattern"
    ;;
  *)
    usage
    ;;
esac
