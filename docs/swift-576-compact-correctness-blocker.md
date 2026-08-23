# Swift #576 compact-parser correctness blocker

Status: NO-GO. Keep [issue #576](https://github.com/odvcencio/gotreesitter/issues/576) open.

Base commit: `97a7bde26bac9b1a110bbf9216cc681ca59cc5aa`.

This receipt records the Swift `unsafe` expression-prefix mismatch. It does not
change parser behavior or admit a compact recovery path.

## Canonical tests

The Swift corpus ratchet records both #576 files in
`grammars/swift_corpus_test.go`.

The locked-C parity tests record the corpus witness and the 20-byte minimal
witness in `cgo_harness/parity_swift_recovery_probe_test.go`. The minimal test
also pins the first divergence path and both child counts.

## Locked evidence

| Witness | Source bytes | Source SHA-256 | Go deep SHA-256 | C deep SHA-256 |
|---|---:|---|---|---|
| `let x = unsafe bar()` | 20 | `b511d81ace2a89b05e8e5e0ca6730c10f2ac9295111dae013097c7c6be8861fe` | `860b79483c37e217690deae43036bada15b259bed77713606124fa851702e62f` | `c64b894edc4a20e15f2b4127bad4223f698c8996dba091c06c34aa89386d3c68` |
| `stdlib_FloatingPointToString.swift` | 104681 | `ec96801e5237dff8da773f617a8a2f36e95b6a0a7c94b581855a451cd6507fdc` | `ec51c633a3f99515cc0cd1c0cff435a44ddc7db8e83705977d28f78bdfb0fc0e` | `ab96dddf088487acc700d72af9342c338901504dcf1d32b9644e9f6f6638190d` |

The Swift Go grammar blob is
`be4575bc0acc3c60324aab635d067f940ac5f0557b80a8e3565d1e7d02d53582`.

The locked C oracle uses grammar commit
`41d6e5fe811ec94229ee71771174a8cce558dfee` from
[tree-sitter-swift](https://github.com/alex-pinkus/tree-sitter-swift).
It uses tree-sitter runtime `0.25.1` at commit
`f5afe475deb7c0bae6407fb776c76824f717bb61`.
The C grammar artifact SHA-256 is
`2a9f14046d4ca88b6db1316ee5f48b876aea1700e3c09811b3c87257fe827c5c`.

## Upstream provenance probe

The locked Swift grammar uses commit
`41d6e5fe811ec94229ee71771174a8cce558dfee` and package version `0.7.2`.

The authoritative `tree-sitter-swift` `origin/main` uses commit
`172ada1cc4117d0260d9340680b4134adba2bc2c` and package version `0.7.3`.

The current upstream grammar adds newer Swift syntax. It does not add an
`unsafe` expression rule. Its new `unsafe` reference appears only in
`nonisolated(unsafe)`.

The isolated probe regenerated the current upstream grammar with this command:

```text
go run ./cmd/grammargen -json /tmp/tree-sitter-swift-ref.Hslb4F/src/grammar.json -bin /tmp/swift-current.bin -go /tmp/swift_grammar.go -pkg grammargen -func SwiftGrammarUpstreamProbe
```

The generated blob has 373,991 bytes and SHA-256
`be5cd0bf8df7077804fe4b54ee47d76005c9a85c7c33b857ef6d2aff34461286`.
The shipped blob has 373,670 bytes and SHA-256
`be4575bc0acc3c60324aab635d067f940ac5f0557b80a8e3565d1e7d02d53582`.

The regenerated minimal witness keeps Go digest
`860b79483c37e217690deae43036bada15b259bed77713606124fa851702e62f`.
It keeps the first mismatch at `15..18`: Go has a childless `ERROR`, while
locked C has an `ERROR` child containing `bar`.

The regenerated corpus digest is
`758b85044c6cd5600ace2884f999ea1c834dd565066a2a6732996ed5e572f2df`.
The broader digest changes because the current grammar contains unrelated
updates. The target region does not improve. It remains an `ERROR` span
`6828..6984`, with `MutableSpan` at `6828..6839` inside the error.

The focused generated-language Docker artifact is:

`/tmp/gts-swift-upstream-probe-artifacts/20260823T000638Z-swift-upstream-issue576-range`

The focused generated-language parity artifact is:

`/tmp/gts-swift-upstream-probe-artifacts/20260823T000433Z-swift-upstream-issue576-parity`

Both artifacts passed with one CPU, one test worker, no out-of-memory kill,
and no timeout.

## First parser-visible divergence

The minimal diagnostic fixture added one trailing newline. It therefore had 21 bytes. The pinned witness has 20 bytes. The first divergent span is unchanged.

At byte span `15..18`, Go emits generic identifier symbol `160` for `bar`.
After one reduction, Go reaches state `47` and finds action index `0`.
The Go parser pauses that stack and enters its C-style recovery path.

The locked C lexer skips the same bytes in its current lex state. It emits an
`ERROR` token of size `4`, calls `detect_error`, and resumes from version `0`.
It then skips that error token, calls `recover_to_previous` at state `2543`
and depth `2`, and reduces `_simple_user_type` and `_expression`.

The Go trace records `C-RESUME state=47 byte=14` and
`C-RECOVER-TO-STATE state=1509 depth=2`. Go and C state numbers use different
table encodings. Compare each trace within its own runtime.

The first public-tree difference is:

``` text
Path: /source_file/property_declaration[0]/call_expression[3]/ERROR[1]
Go:  ERROR [15:18] children=0
C:   ERROR [15:18] children=1
C child: ERROR [15:18] "bar"
```

The existing `pushLexErrorRunLeaf` path already builds the C-shaped
`ERROR -> ERROR` wrapper. Go does not receive an `ERROR` token here, so that
producer path does not run.

## Compact-route boundary

Canopy located this call chain:

``` text
parseSwiftCleanFullSourceRecovery
  parser_result_swift.go:62
  -> parseForRecoveryWithMode
     parser_api.go:568
  -> Parse
     parser_api.go:1064
  -> parseInternal
     parser.go:4526
```

Canopy located the generic compact scheduler separately at
`parsercore_phase0_driver.go:5521` (`dispatchPass`) and
`parsercore_phase0_driver.go:7203` (`dropGenericNoActionHeads`).

The D6a frontier producer is default-off and producer-only. The D6b consumer is
default-off and leaves route admission and production drops disabled.
See `docs/compact-route-real-corpus-matrix.md` sections D6a and D6b.

Recovery snippet parsers pin themselves to the production route in
`parser.go:1778`. Swift also lacks compact strategy-2 error-region and
converged-split-drop certification. Therefore D6 frontier work cannot change
this Swift token producer or recovery shape.

A forced candidate probe recorded `strategy2=false`, `frontier_drop=false`,
`routed=0`, and `fallback=1`. Its decline was
`parser-core fresh-full runner did not accept EOF`.

## Decision

Keep both focused tests and the known mismatch receipts. Do not add a Swift
exception or a grammar-name rule. Do not ship parser or grammar code.

Keep issue #576 open. Reopen implementation only after an authoritative
upstream grammar or lexer change alters token production at `bar`, or after a
generic runtime fix produces the locked-C error token without a Swift rule.
Then regenerate the blob and require both Docker witnesses to pass.

## Docker receipts

The focused corpus witness passed without an out-of-memory kill or timeout:

`/tmp/gts-576-repro/20260822T204808Z-swift-576-minimal-corpus`

The focused minimal witness passed without an out-of-memory kill or timeout:

`/tmp/gts-576-repro/20260822T204837Z-swift-576-minimal`

The action traces are stored at:

- `/tmp/gts-576-diag/20260822T205114Z-swift-576-minimal-full-go-trace`
- `/tmp/gts-576-diag/20260822T205101Z-swift-576-minimal-c-log`
- `/tmp/gts-576-diag/20260822T205647Z-swift-576-admission-reason2`
