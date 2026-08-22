# Swift #586 compact-parser correctness blocker

Status: NO-GO. Keep [issue #586](https://github.com/odvcencio/gotreesitter/issues/586) open.

Base commit: `56b97e092d0fb034bddb9e65cc617ebb933cc718`.

This receipt records a shared Swift recovery-cost witness and its first token divergence.
It does not change parser behavior or claim a performance fix.

## Canonical witness

The existing Go test records the Swift `FloatingPointToString` corpus witness.
The locked-C test records the same source with the pinned C runtime.

| Field | Value |
| --- | --- |
| Source | `grammars/testdata/swift_corpus/stdlib_FloatingPointToString.swift` |
| Source bytes | `104681` |
| Source SHA-256 | `ec96801e5237dff8da773f617a8a2f36e95b6a0a7c94b581855a451cd6507fdc` |
| Go deep SHA-256 | `ec51c633a3f99515cc0cd1c0cff435a44ddc7db8e83705977d28f78bdfb0fc0e` |
| Locked-C deep SHA-256 | `ab96dddf088487acc700d72af9342c338901504dcf1d32b9644e9f6f6638190d` |
| Swift grammar commit | `41d6e5fe811ec94229ee71771174a8cce558dfee` |
| C runtime | `0.25.1@f5afe475deb7c0bae6407fb776c76824f717bb61` |
| C grammar artifact SHA-256 | `2a9f14046d4ca88b6db1316ee5f48b876aea1700e3c09811b3c87257fe827c5c` |

## Recovery cost

The focused Docker witness passed with one worker and one Swift grammar.
It returned an accepted error tree that covered the complete source.

| Fact | Value |
| --- | ---: |
| Recovery entries | `390` |
| Recovery cost competitions | `193358` |
| Recovery cost walks | `386716` |
| Retry passes | `4` |
| Retry reason | `initial_result_requires_merge_width` |
| Selected retry | `initial_merge` |
| Selected tree has an error | `true` |
| Selected tree covers the source | `true` |
| Maximum stacks seen | `28` |

These counters describe recovery work. They do not prove a safe retry reduction.
The run completed without an out-of-memory kill or a timeout.

## Retry threshold

A focused prefix scan found the first tested retry threshold at `42000` bytes.
The `40000`-byte prefix used zero retries.
The `42000`-byte prefix used four retries and `231372` cost walks.
This is the smallest tested threshold, not a proof of global minimality.

## First producer divergence

The canonical minimal witness is `let x = unsafe bar()` with 20 bytes.
The diagnostic trace added one trailing newline and kept the same first difference.

At byte span `15..18`, Go emits generic identifier symbol `160` for `bar`.
Go then pauses at state `47` because that symbol has no action.
Go resumes at byte `14` and recovers to state `1509` at depth `2`.

The locked C lexer enters error mode after `unsafe`.
It emits an `ERROR` lookahead, detects the error, and resumes version `0`.
It skips that error and recovers to state `2543` at depth `2`.

The first public tree difference is:

```text
Path: root[0][3][1]
Go:   ERROR [15:18] children=0
C:    ERROR [15:18] children=1
C child: ERROR [15:18] "bar"
```

The first corpus region has the same producer shape.
At bytes `6828..6839`, Go emits `MutableSpan` as generic symbol `160`.
The locked C lexer skips unrecognized characters and emits local `ERROR` lookaheads.
It then recovers to state `2543` at depth `4`.
The first local C error spans `6828..6839`.
The Go local error spans `6828..6984`.

The divergence starts in token and error-node production.
It starts before retry selection.

## Bounded correction checks

The retry-policy probe compared the default ladder with a policy that skipped every retry.

| Policy | Retry passes | Selected rung | Go deep SHA-256 |
| --- | ---: | --- | --- |
| Default | `4` | `initial_merge` | `ec51c633a3f99515cc0cd1c0cff435a44ddc7db8e83705977d28f78bdfb0fc0e` |
| Skip all retries | `0` | `initial` | `356fb1f20e02a20ae9f449eda55e648c6b09e5c988775a197f781074fa3499f1` |

Skipping retries changes the tree digest. Reject this generic bypass.

The stack-cap probe set `GOT_GLR_MAX_STACKS=8`.
It preserved the default Go digest and reduced retry passes to `1`.
This environment setting is diagnostic only.
It does not repair the producer divergence or prove a production cap.

## D6 disposition

The authenticated D6 frontier work does not change this result.
D6 acts after token production and retry certification.
The first difference occurs in lexer and error-token production.

The skip probe changed the selected tree digest.
Therefore this receipt does not certify a safe-decline retry bypass.
Do not add a Swift exception or a grammar-name rule.

Keep issue #586 open.
Resume implementation only after a generic producer fix emits the locked-C error shape.
Require both Go and locked-C Docker witnesses to pass before implementation work resumes.

## Docker receipts

Go witness command:

```text
bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --repo-root /tmp/gotreesitter-main-586-receipt-20260822 \
  --out-root /tmp/gts-586-receipt-repro \
  --label swift-586-receipt-go \
  --memory 8g --cpus 1 --gomemlimit 6GiB \
  --goflags '-p=1' --test-parallel 1 --timeout 15m -- \
  "cd /workspace && go test ./ -run '^TestSwiftRecoveryTelemetryWitnesses/swift-586-floating-point$' -count=1 -v -timeout 15m"
```

Go witness artifact: `/tmp/gts-586-receipt-repro/20260822T214520Z-swift-586-receipt-go`

Locked-C witness command:

```text
bash cgo_harness/docker/run_parity_in_docker.sh --no-build \
  --repo-root /tmp/gotreesitter-main-586-receipt-20260822 \
  --out-root /tmp/gts-586-receipt-repro \
  --label swift-586-receipt-c \
  --memory 8g --cpus 1 --gomemlimit 6GiB \
  --goflags '-p=1' --test-parallel 1 --timeout 15m -- \
  "cd /workspace/cgo_harness && go test . -tags treesitter_c_parity -run '^TestB16SwiftRecoveryTelemetryCOracle/swift-586-floating-point$' -count=1 -parallel 1 -timeout 15m -v"
```

Locked-C witness artifact: `/tmp/gts-586-receipt-repro/20260822T214540Z-swift-586-receipt-c`

The focused trace artifacts are:

- Minimal Go trace: `/tmp/gts-586-repro/20260822T212025Z-swift-586-min-glr`
- Minimal locked-C trace: `/tmp/gts-586-repro/20260822T212013Z-swift-586-min-clog`
- Corpus Go token trace: `/tmp/gts-586-repro/20260822T213335Z-swift-586-dfa-first-diff`
- Corpus locked-C trace: `/tmp/gts-586-repro/20260822T213447Z-swift-586-c-log`
- Prefix threshold scan: `/tmp/gts-586-repro/20260822T211813Z-swift-586-prefixes-b`
- Retry-policy probe: `/tmp/gts-586-repro/20260822T212213Z-swift-586-retry-probe-b`
- Stack-cap Go probe: `/tmp/gts-586-repro/20260822T212252Z-swift-586-cap8`
- Stack-cap locked-C probe: `/tmp/gts-586-repro/20260822T212307Z-swift-586-cap8-c`

All focused runs passed. No production code changed.
