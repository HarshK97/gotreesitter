#!/usr/bin/env bash
# Build the grammars package once per language with that language's
# grammar_subset tag, and report every language that does not compile.
#
# A single-language subset build drops every other grammar's source files. A
# shared helper that lives in a language-gated file therefore breaks each subset
# build that selects a different language. The default CI matrix never sets
# grammar_subset, so only this sweep finds that class of break.
#
# Usage:
#   scripts/subset_build_sweep.sh            # sweep every built-in grammar
#   scripts/subset_build_sweep.sh go java    # sweep the named grammars only
#   JOBS=4 scripts/subset_build_sweep.sh     # set the parallelism (default 8)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

JOBS="${JOBS:-8}"

if [[ $# -gt 0 ]]; then
  langs=("$@")
else
  mapfile -t langs < <(
    ls grammars/z_subset_registry_register_*.go |
      sed 's|.*register_||; s|\.go$||' |
      sort
  )
fi

if [[ ${#langs[@]} -eq 0 ]]; then
  echo "no grammars found to sweep" >&2
  exit 2
fi

report="$(mktemp)"
trap 'rm -f "$report"' EXIT

build_one() {
  local lang="$1"
  local out
  out="$(go build -tags "grammar_subset,grammar_subset_${lang}" ./grammars/ 2>&1)"
  if [[ -n "$out" ]]; then
    printf 'BROKEN\t%s\t%s\n' "$lang" "$(printf '%s' "$out" | grep -v '^#' | head -1)"
  else
    printf 'OK\t%s\t\n' "$lang"
  fi
}
export -f build_one

printf '%s\n' "${langs[@]}" | xargs -P "$JOBS" -I{} bash -c 'build_one "$@"' _ {} >"$report"

ok_count="$(grep -c '^OK' "$report" || true)"
broken_count="$(grep -c '^BROKEN' "$report" || true)"

echo "grammar subset build sweep: ${ok_count} ok, ${broken_count} broken (of ${#langs[@]})"

if [[ "$broken_count" -gt 0 ]]; then
  echo
  echo "broken subset builds:"
  grep '^BROKEN' "$report" | sort | while IFS=$'\t' read -r _ lang err; do
    printf '  %-24s %s\n' "$lang" "$err"
  done
  echo
  echo "Move the missing symbol into a file with no grammar_subset tag, or widen"
  echo "the tag on the file that defines it to cover the grammars that need it."
  exit 1
fi
