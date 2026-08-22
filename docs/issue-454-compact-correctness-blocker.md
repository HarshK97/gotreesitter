# Issue #454 compact-parser correctness blocker

Status: **NO-GO**. Ship no parser change from this investigation. Keep issue
[#454](https://github.com/odvcencio/gotreesitter/issues/454) open.

## Scope

This receipt uses base commit
`30f470f5c2bf18540f7a18b2b22a7e33b88d4e10`.

The issue #454 report describes C latency and marks correctness as OK. This
repository uses an internal deterministic C witness. The witness uses a
repeated source near 137 kibibytes (KiB). The edit removes the first `x` from
`x0`. The transient malformed text becomes `0`. The original source has
140,288 bytes. The edited source has 140,287 bytes.

The smallest locked-C witness uses the same source prefix at 1,024 bytes. The
known-divergence ratchet is
`cgo_harness/issue454_c_compact_blocker_parity_test.go`.

## Locked-C evidence

The guard compares a fresh Go tree with the pinned C parser. Both roots report
an error. The first deep-tree difference is:

```text
/translation_unit/function_definition[0]/compound_statement[2]/ERROR[2]/number_literal[0]
category=error Go=true C=false
```

The fresh Go tree differs from locked C at every tested size:

| Source size | Result |
| --- | --- |
| 1 KiB (1,024 bytes) | The first difference is the recorded `number_literal` error flag. |
| 4 KiB (4,096 bytes) | The same first difference remains. |
| 16 KiB (16,384 bytes) | The same first difference remains. |
| 64 KiB (65,536 bytes) | The same first difference remains. |
| 137 KiB (140,288 bytes) | The same first difference remains. |

The structure probe records all digest pairs in
`/tmp/gts-issue454-artifacts/20260822T221718Z-issue454-c-structure/container.log`.

## Incremental evidence

The 137 KiB incremental Go tree equals the fresh Go tree. Both have digest
`9c979bb436f92e7f96885454de81d9d95d2befff242145b1026addf5d9395c4d`.

The locked-C digest is
`8fe04819317a4f225b5c298a71acdceb5aa965abb3a397e35eb48c5849888d5c`.

The incremental profile reports
`ReuseUnsupportedReason=incremental_parse_memory_budget_full_retry`.
The parser completes the edit after the memory-budget fallback. The existing
replace, insert, and delete regression passes in
`/tmp/gts-issue454-artifacts/20260822T221438Z-issue454-c-current`.

The fresh Go mismatch proves that retry selection cannot repair the C parity
difference. The fallback fixes the incremental failure mode only.

## Ownership and decision

The first difference is a child error flag inside a recovered declaration. It
appears during fresh parsing, before incremental retry selection. The likely
owner is generic recovery or error-node materialization. A bounded generic fix
is not safe without wider recovery validation.

The 1 KiB known-divergence ratchet passes in
`/tmp/gts-issue454-artifacts-rebase/20260822T230037Z-issue454-c-1k-ratchet-20260822`.

## Reopening conditions

Reopen this work only when all conditions pass:

1. Keep the 1 KiB known-divergence ratchet as the only CI guard.
2. Trace the producer that sets the `number_literal` error flag.
3. Repair generic recovery materialization without changing unrelated routes.
4. Require fresh and incremental trees to match locked C at 1, 4, 16, 64, and 137 KiB.
5. Re-run the existing replace, insert, delete, and C recovery gates.
6. Preserve the memory-budget fallback as a separate correctness concern.
