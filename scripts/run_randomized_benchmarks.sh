#!/usr/bin/env bash

set -euo pipefail

usage() {
	cat >&2 <<'EOF'
Usage: scripts/run_randomized_benchmarks.sh --output PATH [options]

Run the combined performance set once for each shuffle seed.

Options:
  --output PATH       Append raw go test output to PATH.
  --runs N            Number of seeds to run. Default: 20.
  --seed-start N      First shuffle seed. Default: 1.
  --benchtime D       Benchmark duration. Default: 750ms.
  --tags TAGS         Go build tags. Default: gts_parsercorephase0.
  --help              Show this help.
EOF
}

output_path=""
runs=20
seed_start=1
benchtime=750ms
build_tags=gts_parsercorephase0

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

output_dir=$(dirname -- "$output_path")
mkdir -p -- "$output_dir"

# Keep this set aligned across baseline and candidate runs.
benchmark_re='^(BenchmarkGoParse(FullDFA|CoreDFA|IncrementalSingleByteEditDFA|IncrementalNoEditDFA|IncrementalRandomSingleByteEdit)|BenchmarkKDLRecoveryGarbageSuffix|BenchmarkExpectedRootCanFrameLongRepeat|BenchmarkDiagnosticParserCore(CorridorSchedulerOnly|WarmSchedulerOnlyQueryCompile|WarmMaterializationOnlyQueryCompile)|BenchmarkParserCoreFreshFull(Canonical|SelectedStoreCanonical)|Benchmark(TaggerTag(Tree)?Go|ExtractCodeUnderstanding(Tree)?Go|ExtractAllFactsTreeGo|FactProgram(All)?(Tree)?Go))$'

printf 'randomized benchmark output: %s\n' "$output_path" >&2
printf 'seeds: %s..%s\n' "$seed_start" "$((seed_start + runs - 1))" >&2
printf 'benchtime: %s\n' "$benchtime" >&2

for ((offset = 0; offset < runs; offset++)); do
	seed=$((seed_start + offset))
	printf 'running shuffle seed %d\n' "$seed" >&2
	GOMAXPROCS=1 go test -tags "$build_tags" . \
		-run '^$' \
		-bench "$benchmark_re" \
		-benchmem \
		-count=1 \
		-benchtime="$benchtime" \
		-shuffle="$seed" >>"$output_path"
done

printf 'completed %d randomized runs\n' "$runs" >&2
