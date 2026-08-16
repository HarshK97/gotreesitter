#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat >&2 <<'EOF'
Usage: scripts/run_randomized_benchmarks.sh --output PATH [options]

Run the combined performance set once for each shuffle seed.

Options:
  --output PATH       Append raw go test output to PATH.
  --lock-path PATH    Host lock path. Default: /tmp/gotreesitter-run-randomized-benchmarks.lock.
  --runs N            Number of seeds to run. Default: 20.
  --seed-start N      First shuffle seed. Default: 1.
  --benchtime D       Benchmark duration. Default: 750ms.
  --tags TAGS         Go build tags. Default: gts_parsercorephase0.
  --bench-regex REGEX Benchmark selection regex. Default: combined set.
  --package PATH      Go package path. Default: .
  --help              Show this help.
EOF
}

output_path=""
lock_path="/tmp/gotreesitter-run-randomized-benchmarks.lock"
runs=20
seed_start=1
benchtime=750ms
build_tags=gts_parsercorephase0
package_path=.
benchmark_re='^(BenchmarkGoParse(FullDFA|CoreDFA|IncrementalSingleByteEditDFA|IncrementalNoEditDFA|IncrementalRandomSingleByteEdit)|Benchmark(KDLRecoveryGarbageSuffix|RecoveryCorpusFile)|BenchmarkExpectedRootCanFrameLongRepeat|BenchmarkDiagnosticParserCore(CorridorSchedulerOnly|WarmSchedulerOnlyQueryCompile|WarmMaterializationOnlyQueryCompile)|BenchmarkParserCoreFreshFull(Canonical|SelectedStoreCanonical)|Benchmark(TaggerTag(Tree)?Go|ExtractCodeUnderstanding(Tree)?Go|ExtractAllFactsTreeGo|FactProgram(All)?(Tree)?Go))$'

while (($# > 0)); do
	case "$1" in
	--output)
		if (($# < 2)); then
			printf '%s\n' "--output requires a path" >&2
			exit 2
		fi
		output_path=$2
		shift 2
		;;
	--lock-path)
		if (($# < 2)); then
			printf '%s\n' "--lock-path requires a path" >&2
			exit 2
		fi
		lock_path=$2
		shift 2
		;;
	--runs)
		if (($# < 2)); then
			printf '%s\n' "--runs requires a positive integer" >&2
			exit 2
		fi
		runs=$2
		shift 2
		;;
	--seed-start)
		if (($# < 2)); then
			printf '%s\n' "--seed-start requires a non-negative integer" >&2
			exit 2
		fi
		seed_start=$2
		shift 2
		;;
	--benchtime)
		if (($# < 2)); then
			printf '%s\n' "--benchtime requires a duration" >&2
			exit 2
		fi
		benchtime=$2
		shift 2
		;;
	--tags)
		if (($# < 2)); then
			printf '%s\n' "--tags requires a comma-separated tag list" >&2
			exit 2
		fi
		build_tags=$2
		shift 2
		;;
	--bench-regex)
		if (($# < 2)); then
			printf '%s\n' "--bench-regex requires a regex" >&2
			exit 2
		fi
		benchmark_re=$2
		shift 2
		;;
	--package)
		if (($# < 2)); then
			printf '%s\n' "--package requires a path" >&2
			exit 2
		fi
		package_path=$2
		shift 2
		;;
	--help)
		usage
		exit 0
		;;
	*)
		printf 'unknown option: %s\n' "$1" >&2
		usage
		exit 2
		;;
	esac
done

if [[ -z "$output_path" ]]; then
	printf '%s\n' "--output is required" >&2
	usage
	exit 2
fi

if [[ ! "$runs" =~ ^[1-9][0-9]*$ ]]; then
	printf 'invalid --runs value: %s\n' "$runs" >&2
	exit 2
fi

if [[ ! "$seed_start" =~ ^[0-9]+$ ]]; then
	printf 'invalid --seed-start value: %s\n' "$seed_start" >&2
	exit 2
fi

if [[ -e "$output_path" ]]; then
	printf 'output already exists: %s\n' "$output_path" >&2
	exit 2
fi

if [[ -z "$lock_path" ]]; then
	printf '%s\n' "--lock-path requires a path" >&2
	exit 2
fi

if ! command -v flock >/dev/null 2>&1; then
	printf '%s\n' 'flock is required to serialize benchmark campaigns' >&2
	exit 2
fi

lock_dir=$(dirname -- "$lock_path")
mkdir -p -- "$lock_dir"
lock_fd=""
lock_held=0

release_lock() {
	if ((lock_held == 0)); then
		return 0
	fi
	: >"$lock_path" || true
	flock -u "$lock_fd" || true
	lock_held=0
}

exit_with_lock_status() {
	local status=$?
	release_lock
	exit "$status"
}

exit_after_signal() {
	local signal=$1
	release_lock
	trap - "$signal"
	kill -s "$signal" "$$"
}

trap exit_with_lock_status EXIT
trap 'exit_after_signal HUP' HUP
trap 'exit_after_signal INT' INT
trap 'exit_after_signal TERM' TERM

if ! exec {lock_fd}>>"$lock_path"; then
	printf 'cannot open benchmark lock: %s\n' "$lock_path" >&2
	exit 2
fi

if ! flock -n "$lock_fd"; then
	printf 'benchmark lock is busy: %s\n' "$lock_path" >&2
	if [[ -s "$lock_path" ]]; then
		sed 's/^/owner: /' <"$lock_path" >&2
	else
		printf '%s\n' 'owner: metadata unavailable' >&2
	fi
	exit 75
fi

lock_held=1
lock_started=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
lock_cwd=$(pwd -P)
{
	printf 'pid=%s\n' "$$"
	printf 'started=%s\n' "$lock_started"
	printf 'cwd=%s\n' "$lock_cwd"
	printf 'output=%s\n' "$output_path"
} >"$lock_path"

output_dir=$(dirname -- "$output_path")
mkdir -p -- "$output_dir"

printf 'randomized benchmark output: %s\n' "$output_path" >&2
printf 'seeds: %s..%s\n' "$seed_start" "$((seed_start + runs - 1))" >&2
printf 'benchtime: %s\n' "$benchtime" >&2
{
	printf '# package: %s\n' "$package_path"
	printf '# bench regex: %s\n' "$benchmark_re"
	printf '# build tags: %s\n' "$build_tags"
	printf '# seeds: %s..%s\n' "$seed_start" "$((seed_start + runs - 1))"
	printf '# benchtime: %s\n' "$benchtime"
} >"$output_path"

for ((offset = 0; offset < runs; offset++)); do
	seed=$((seed_start + offset))
	printf 'running shuffle seed %d\n' "$seed" >&2
	GOMAXPROCS=1 go test -tags "$build_tags" "$package_path" \
		-run '^$' \
		-bench "$benchmark_re" \
		-benchmem \
		-count=1 \
		-benchtime="$benchtime" \
		-shuffle="$seed" >>"$output_path"
done

printf 'completed %d randomized runs\n' "$runs" >&2
