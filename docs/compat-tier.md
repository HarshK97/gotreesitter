# The C-faithful result-compatibility tier

The root package contains a visible `parser_result_<language>*.go` tier.
It is post-parse compatibility scaffolding: where the parser does not yet
reproduce a C tree-selection or materialization behavior through a general
engine mechanism, a bounded pass reconciles the returned tree with the C
oracle.

This page is a readable guide. The mechanically checked source of truth is
[`testdata/result_compat_ownership_v1.json`](../testdata/result_compat_ownership_v1.json).
Do not update dispatcher coverage, ownership, or retirement state here
without updating that registry.

## Current denominator

The v1 registry freezes the current surface:

- 75 explicit `runLanguageResultCompatibility` switch arms covering 82
  language names;
- one predicate-dispatched COBOL entry matching exactly `cobol` or `COBOL`;
- one temporary read-only generic wrapper after language dispatch;
- one retained post-finalization second-pass fixpoint with two switch arms
  covering Scala and HTML.

That is 78 live registry entries. The registry covers only this documented
internal result-compatibility tier; scheduler experiments and other engine
research belong in their owning subsystem's durable traces.

The generic wrapper keeps the registry bisectable until its retirement commit
exists. It computes only the error summary and polls the stop source.
It does not mutate a tree.

R2 of `docs/root-normalization-retirement.md` retired three dispatcher arms:
OCaml's collapsed named-leaf restoration, Ruby's top-level module bound
shrink, and half of HTML's arm (the ERROR-root nested-custom-tag
reconstruction). HTML's other function,
`normalizeHTMLRecoveredNestedCustomTagRanges`, stays live: see "The retained
second pass" below. The retired entries stay in the registry as historical
receipts.

Each registry entry has a stable ID, functions and files, languages, purpose,
authoritative owner, witnesses, a retirement condition, coverage fields for
production/compact/forest/incremental/C-oracle routes, status, and optional
receipt references. A retired entry remains in the registry with its commit
and receipt references; deleting the historical record is not retirement.

## Ownership

Compatibility functions describe symptoms. Their `authoritative_owner`
records the earliest subsystem that must eventually produce the C-faithful
result:

| Owner | Responsibility |
|---|---|
| `scheduler_action_semantics` | action ordering, recovery, and work execution |
| `derivation_election_selection` | ambiguity and winning-derivation choice |
| `materialization` | node, field, alias, trivia, and span construction |
| `scanner_checkpoint_state` | external-scanner state and restoration |
| `incremental_edit_reuse` | edit invalidation and subtree reuse |
| `public_compatibility` | compatibility intentionally retained at an exported API boundary |

The route fields use a closed vocabulary enforced by the registry test:

- live dispatcher, predicate, and generic entries use
  `shared_result_compatibility_tail` for production/compact/forest/incremental
  and `curated_single_grammar_parity` for the C oracle;
- the live fixpoint uses `post_finalization_fixpoint` for those four parser
  routes and `language_witnesses_required` for the C oracle; and
- retired entries use `retired_exact_receipt` for all four native routes.
  The C oracle uses `retired_exact_receipt` or
  `retired_known_divergence_receipt`. Each retired entry must include a
  retirement commit and a receipt reference.

These are evidence labels, not claims that all five engines have independent
implementations. The shared-tail value means that route reaches the same
post-parse normalization tail.

## Why it exists

Two GLR parsers can accept the same input and still return different trees
under ambiguity, error recovery, aliasing, or extra/trivia attachment.
gotreesitter's parity target is byte-exact agreement with the selected C tree,
including error shapes and recovered spans. The cgo-backed suites under
`cgo_harness/` provide that oracle.

The tier stays internal because it operates on arena and node internals before
the tree is returned. Moving it into a package with exported plumbing would
make the exported API surface worse without changing its ownership.

## Current progress: collapsed named leaves

All 23 registered collapsed named-leaf rows for the six affected built-in
languages now produce their child shape natively. The occurrence policy is
admitted by the exact-profile receipt and compiled from exact named parent and
raw-child metadata identities, so true adapted clones retaining both take the
same upstream construction path. A display-name or pair-level metadata match
alone does not admit a caller-built or custom artifact. Focused production,
compact, forest, and incremental witnesses prove the shape without a
compatibility traversal, and the per-pair C-oracle census is nonzero and equal.
The generic reconstruction walk is therefore retired and deleted. Once raw
child identity has been lost, a display-name-compatible caller artifact is no
longer guessed into shape; custom artifacts must carry the explicit native
capability and exact metadata receipt to opt in.

## Current progress: terminal leaves

The generic terminal-leaf mutation is removed. Reduction and alias
materialization now own its tree shape.

The route receipt covers production, compact, forest, and incremental parsing.
The scanner-aware corpus receipt covers 45 languages and 868,010 nodes.
It finds no retired shape and reports 161 languages as uncovered.
The focused Go tree also matches the locked C tree.

The full error-summary walk remains. It preserves exact retry selection for
under-set descendant errors and keeps the existing stop polls.

The registry retains a temporary read-only wrapper until checkpoint 1 exists.
Checkpoint 2 must record checkpoint 1 as `retired_commit`.

## Current progress: trailing root trivia

Clean hidden whitespace-only root tails are now finalized as root span coverage
instead of reconstructed in the shared compatibility tail. The rule applies
before result compatibility, including compatibility-free production and
compact parses; forest and incremental routes share the same root finalizer.
Error roots retain their recovery extra, and lazy final-child references are
filtered without draining the compact range. Real RST and Comment fixtures are
exact against their C oracles. The generic trailing-extra pass is retired.

## The retained second pass

`normalizePostFinalizationReturnedTree` deliberately runs a bounded second
pass for Scala and HTML. It remains live because the first pass can expose
information needed by later normalization. In particular, it may not be
retired until the HTML producer/materializer emits final nested custom-tag
ranges without `normalizeHTMLRecoveredNestedCustomTagRanges`. Scala must
likewise emit its final annotations and spans in one pass, and all registered
route receipts must show the second pass is inert.

JavaScript no longer participates in this fixpoint. Its canonical
compatibility pipeline already extends `program` and recovery-root terminator
tails after every JavaScript shape and span rewrite. The only intervening work
before returned-tree publication is terminal-leaf normalization and optional
parent-link wiring; neither can shorten or reclassify the root. The registry
retains a retired historical entry for the deleted JavaScript arm and its
production, compact-final-ref, forest, and incremental span receipts.

Removing the second pass because it looks repetitive would reopen known
fixpoint behavior; its registry retirement condition is the deletion gate.

## Editing and validation

When adding, moving, or retiring compatibility code:

1. Update the JSON registry in the same change.
2. Keep dispatcher languages, called functions, ownership, witnesses, routes,
   and retirement evidence explicit.
3. For retirement, keep the entry and set `status` to `retired`, with
   `retired_commit` and at least one `receipt_refs` item.
4. Run the focused registry gate:

```sh
go test . -run '^TestResultCompatibilityOwnershipRegistry$' -count=1
```

The test parses `parser_result_compat.go`, `parser_result_helpers.go`, and
`parser_api.go` with the Go AST. It fails when a dispatcher arm or predicate
is unregistered, the exact COBOL predicate language set drifts, a generic pass
changes without a registry update, a post-finalization language arm or call
changes, or a live registered function disappears from its declared files.
The COBOL check requires the canonical nil-guard AND parenthesized two-equality
OR AST. The fixpoint check counts clause-local call occurrences and rejects
normalization in default clauses or outside registered cases. The gate also
enforces kind/status combinations, route vocabulary and semantics, required
metadata, and referenced witness paths.

This document is hand-maintained explanatory text, not generated output. The
JSON registry and the focused Go test are the regeneration/validation
contract.
