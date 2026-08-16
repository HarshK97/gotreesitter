#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_root=$(cd -- "$script_dir/.." && pwd -P)
runner="$script_dir/run_randomized_benchmarks.sh"
test_root=$(mktemp -d "${TMPDIR:-/tmp}/gotreesitter-bench-lock-test.XXXXXX")
mock_bin="$test_root/bin"
mkdir -p -- "$mock_bin"

first_pid=""
dash_pid=""
signal_pid=""
independent_a_pid=""
independent_b_pid=""

cleanup() {
	local pid
	for pid in "$first_pid" "$dash_pid" "$signal_pid" "$independent_a_pid" "$independent_b_pid"; do
		if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
			kill -TERM "$pid" 2>/dev/null || true
		fi
	done
	rm -rf -- "$test_root"
}
trap cleanup EXIT

fail() {
	printf 'not ok - %s\n' "$1" >&2
	exit 1
}

pass() {
	printf 'ok - %s\n' "$1"
}

assert_contains() {
	local needle=$1
	local file=$2
	grep -F -- "$needle" "$file" >/dev/null || fail "missing '$needle' in $file"
}

assert_not_contains() {
	local needle=$1
	local file=$2
	if grep -F -- "$needle" "$file" >/dev/null; then
		fail "unexpected '$needle' in $file"
	fi
}

line_count() {
	if [[ ! -f "$1" ]]; then
		printf '0\n'
		return
	fi
	wc -l <"$1" | tr -d ' '
}

wait_for_file() {
	local file=$1
	local attempt
	for ((attempt = 0; attempt < 500; attempt++)); do
		[[ -e "$file" ]] && return 0
		sleep 0.01
	done
	fail "timed out waiting for $file"
}

wait_for_status() {
	local pid=$1
	local status=0
	wait "$pid" || status=$?
	return "$status"
}

cat >"$mock_bin/go" <<'EOF'
#!/usr/bin/env bash

set -euo pipefail

: "${MOCK_GO_LOG:?MOCK_GO_LOG is required}"
printf 'pid=%s args=%s\n' "$$" "$*" >>"$MOCK_GO_LOG"
if [[ -n "${MOCK_GO_READY:-}" ]]; then
	: >"$MOCK_GO_READY"
fi
if [[ -n "${MOCK_GO_WAIT_FILE:-}" ]]; then
	while [[ ! -e "$MOCK_GO_WAIT_FILE" ]]; do
		sleep 0.01
	done
fi
EOF
chmod +x "$mock_bin/go"

cd -- "$repo_root"

first_lock="$test_root/first.lock"
first_log="$test_root/first-go.log"
first_ready="$test_root/first-ready"
first_release="$test_root/first-release"
first_output="$test_root/first.txt"
first_stderr="$test_root/first.stderr"

PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$first_log" \
MOCK_GO_READY="$first_ready" \
MOCK_GO_WAIT_FILE="$first_release" \
bash "$runner" --output "$first_output" --runs 1 --seed-start 7 \
	--benchtime 1ns --lock-path "$first_lock" >"$test_root/first.stdout" 2>"$first_stderr" &
first_pid=$!
wait_for_file "$first_ready"
assert_contains "pid=$first_pid" "$first_lock"
assert_contains "started=" "$first_lock"
assert_contains "cwd=$repo_root" "$first_lock"
assert_contains "output=$first_output" "$first_lock"
if flock -n "$first_lock" -c ':'; then
	fail 'the first runner did not hold its lock'
fi

second_output="$test_root/second.txt"
second_stderr="$test_root/second.stderr"
second_status=0
PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$first_log" \
bash "$runner" --output "$second_output" --runs 1 --seed-start 8 \
	--benchtime 1ns --lock-path "$first_lock" >"$test_root/second.stdout" 2>"$second_stderr" || second_status=$?
[[ "$second_status" -eq 75 ]] || fail "second runner returned $second_status, want 75"
assert_contains 'benchmark lock is busy' "$second_stderr"
assert_contains "pid=$first_pid" "$second_stderr"
assert_contains "output=$first_output" "$second_stderr"
[[ "$(line_count "$first_log")" -eq 1 ]] || fail 'second runner invoked go'
pass 'the first runner owns the lock and the second runner fails before go'

: >"$first_release"
first_status=0
wait "$first_pid" || first_status=$?
[[ "$first_status" -eq 0 ]] || fail "first runner returned $first_status"

printf '%s\n' \
	'pid=stale' \
	'started=1970-01-01T00:00:00Z' \
	'cwd=/stale/worktree' \
	'output=/stale/output' >"$first_lock"
PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$first_log" \
bash "$runner" --output "$test_root/stale-replacement.txt" --runs 1 --seed-start 9 \
	--benchtime 1ns --lock-path "$first_lock" >"$test_root/stale.stdout" 2>"$test_root/stale.stderr"
[[ "$(line_count "$first_log")" -eq 2 ]] || fail 'stale metadata blocked a released lock'
pass 'stale metadata does not block after lock release'

dash_lock='-benchmark.lock'
dash_lock_file="$test_root/$dash_lock"
dash_log="$test_root/dash-go.log"
dash_ready="$test_root/dash-ready"
dash_release="$test_root/dash-release"
dash_output="$test_root/dash-first.txt"
dash_second_stderr="$test_root/dash-second.stderr"
(
	cd -- "$test_root"
	PATH="$mock_bin:$PATH" \
	MOCK_GO_LOG="$dash_log" \
	MOCK_GO_READY="$dash_ready" \
	MOCK_GO_WAIT_FILE="$dash_release" \
	bash "$runner" --output "$dash_output" --runs 1 --seed-start 10 \
		--benchtime 1ns --lock-path "$dash_lock" >"$test_root/dash-first.stdout" \
		2>"$test_root/dash-first.stderr"
) &
dash_pid=$!
wait_for_file "$dash_ready"
if flock -n "$dash_lock_file" -c ':'; then
	fail 'the dash-prefixed lock was not held'
fi
dash_second_status=0
(
	cd -- "$test_root"
	PATH="$mock_bin:$PATH" \
	MOCK_GO_LOG="$dash_log" \
	bash "$runner" --output "$test_root/dash-second.txt" --runs 1 --seed-start 11 \
		--benchtime 1ns --lock-path "$dash_lock" >"$test_root/dash-second.stdout" \
		2>"$dash_second_stderr"
) || dash_second_status=$?
[[ "$dash_second_status" -eq 75 ]] || fail "dash-prefixed runner returned $dash_second_status, want 75"
assert_contains "benchmark lock is busy: $dash_lock" "$dash_second_stderr"
assert_contains 'owner: pid=' "$dash_second_stderr"
assert_contains 'owner: started=' "$dash_second_stderr"
assert_contains "owner: cwd=$test_root" "$dash_second_stderr"
assert_contains "owner: output=$dash_output" "$dash_second_stderr"
[[ "$(line_count "$dash_log")" -eq 1 ]] || fail 'dash-prefixed second runner invoked go'
: >"$dash_release"
dash_status=0
wait "$dash_pid" || dash_status=$?
[[ "$dash_status" -eq 0 ]] || fail "dash-prefixed first runner returned $dash_status"
pass 'dash-prefixed lock paths report owner metadata before go'

signal_lock="$test_root/signal.lock"
signal_log="$test_root/signal-go.log"
signal_ready="$test_root/signal-ready"
signal_release="$test_root/signal-release"
PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$signal_log" \
MOCK_GO_READY="$signal_ready" \
MOCK_GO_WAIT_FILE="$signal_release" \
setsid bash "$runner" --output "$test_root/signal.txt" --runs 1 --seed-start 10 \
	--benchtime 1ns --lock-path "$signal_lock" >"$test_root/signal.stdout" 2>"$test_root/signal.stderr" &
signal_pid=$!
wait_for_file "$signal_ready"
kill -TERM -- "-$signal_pid" 2>/dev/null || kill -TERM "$signal_pid"
signal_status=0
wait "$signal_pid" || signal_status=$?
[[ "$signal_status" -ne 0 ]] || fail 'signal termination returned success'
if ! flock -n "$signal_lock" -c ':'; then
	fail 'the signal path did not release the lock'
fi
PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$signal_log" \
bash "$runner" --output "$test_root/signal-replacement.txt" --runs 1 --seed-start 11 \
	--benchtime 1ns --lock-path "$signal_lock" >"$test_root/signal-replacement.stdout" \
	2>"$test_root/signal-replacement.stderr"
[[ "$(line_count "$signal_log")" -eq 2 ]] || fail 'the released signal lock rejected a new runner'
pass 'signal cleanup releases the lock'

independent_a_lock="$test_root/independent-a.lock"
independent_b_lock="$test_root/independent-b.lock"
independent_a_log="$test_root/independent-a-go.log"
independent_b_log="$test_root/independent-b-go.log"
independent_a_ready="$test_root/independent-a-ready"
independent_b_ready="$test_root/independent-b-ready"
independent_a_release="$test_root/independent-a-release"
independent_b_release="$test_root/independent-b-release"
PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$independent_a_log" \
MOCK_GO_READY="$independent_a_ready" \
MOCK_GO_WAIT_FILE="$independent_a_release" \
bash "$runner" --output "$test_root/independent-a.txt" --runs 1 --seed-start 12 \
	--benchtime 1ns --lock-path "$independent_a_lock" >"$test_root/independent-a.stdout" \
	2>"$test_root/independent-a.stderr" &
independent_a_pid=$!
PATH="$mock_bin:$PATH" \
MOCK_GO_LOG="$independent_b_log" \
MOCK_GO_READY="$independent_b_ready" \
MOCK_GO_WAIT_FILE="$independent_b_release" \
bash "$runner" --output "$test_root/independent-b.txt" --runs 1 --seed-start 13 \
	--benchtime 1ns --lock-path "$independent_b_lock" >"$test_root/independent-b.stdout" \
	2>"$test_root/independent-b.stderr" &
independent_b_pid=$!
wait_for_file "$independent_a_ready"
wait_for_file "$independent_b_ready"
if flock -n "$independent_a_lock" -c ':'; then
	fail 'explicit lock path A was not held'
fi
if flock -n "$independent_b_lock" -c ':'; then
	fail 'explicit lock path B was not held'
fi
: >"$independent_a_release"
: >"$independent_b_release"
independent_a_status=0
independent_b_status=0
wait "$independent_a_pid" || independent_a_status=$?
wait "$independent_b_pid" || independent_b_status=$?
[[ "$independent_a_status" -eq 0 ]] || fail "lock path A returned $independent_a_status"
[[ "$independent_b_status" -eq 0 ]] || fail "lock path B returned $independent_b_status"
[[ "$(line_count "$independent_a_log")" -eq 1 ]] || fail 'lock path A did not invoke its mock go'
[[ "$(line_count "$independent_b_log")" -eq 1 ]] || fail 'lock path B did not invoke its mock go'
pass 'distinct explicit lock paths run independently'

assert_not_contains 'go test' "$test_root/second.stderr"
printf '%s\n' 'all randomized benchmark lock tests passed'
