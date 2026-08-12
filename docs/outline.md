# File outlines

`Outliner` projects a symbol outline from a parsed tree: the kind of view an
IDE (integrated development environment) shows in a file's sidebar. The
result is a forest of `OutlineSymbol` values, each with a kind, a name, byte
and point spans, and its own lexically nested children.

The projection is read-only. It runs no parse and changes no tree.

The outline fails closed. When a definition candidate is ambiguous — no
usable name, a conflicting kind, a conflicting name, an overlapping span —
`OutlineTree` never guesses which reading is right. It drops the candidate
and counts the drop in the returned `OutlineReport`, so an omission is
always visible and never silently absorbed into the result.

It reuses the same tags-query data `Tagger` consumes (see the "Tagging"
section of the [README](../README.md)): a capture named `@name`, paired with
a capture whose name starts with `definition.`, for example
`@definition.function`. `Outliner` adds one thing `Tagger` does not: it
groups matches into a nesting forest instead of a flat capture list, and it
resolves every ambiguous candidate through explicit rules instead of
guessing.

## Quick start

This example follows the same pipeline
`grammars/outline_coverage_witness_test.go` uses to prove per-language
coverage: detect the language, resolve its tags query, parse, then build the
outline.

```go
package main

import (
	"fmt"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func main() {
	entry := grammars.DetectLanguageByName("go")
	lang := entry.Language()
	query := grammars.ResolveTagsQuery(*entry)

	source := []byte(`package greet

func Hello(name string) string {
	return "hi " + name
}
`)

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		panic(err)
	}

	outliner, err := gotreesitter.NewOutliner(lang, query)
	if err != nil {
		panic(err)
	}

	symbols, report := outliner.OutlineTree(tree)
	for _, sym := range symbols {
		fmt.Printf("%s %s\n", sym.Kind, sym.Name)
	}
	fmt.Println("symbols:", report.Symbols, "omitted:", report.Omitted())
}
```

Output:

```
function Hello
symbols: 1 omitted: 0
```

`grammars.ResolveTagsQuery` returns the language's hand-written tags query
when one ships, or an inferred query assembled from a shared, data-driven
pattern table otherwise. `gotreesitter.NewOutliner` compiles that query
once. Reuse the returned `*Outliner` and call `OutlineTree` for every tree
the same language produces.

## The API

```go
func NewOutliner(lang *Language, tagsQuery string, opts ...OutlinerOption) (*Outliner, error)
func (o *Outliner) OutlineTree(tree *Tree) ([]OutlineSymbol, OutlineReport)
func (o *Outliner) DefinitionKinds() []string
func (o *Outliner) QueryEmpty() bool
func (o *Outliner) Language() *Language
```

`NewOutliner` builds one outliner for one language and one compiled tags
query. `OutlineTree` opens its own query cursor on each call and writes no
field on the outliner, so a single `*Outliner` is safe to call from several
goroutines at once.

`NewOutliner` returns an error when:

- `lang` is `nil`.
- an attached owner rule (see "Owner rules" below) names no `NodeType` or
  no `OwnerField`.
- `tagsQuery` is non-blank text that fails to compile.

An empty or blank `tagsQuery` is not an error. `NewOutliner` still returns a
usable `*Outliner`, but every `OutlineTree` call on it declines instead of
running a query; see "Declining and omitting" below. `QueryEmpty` reports
this construction-time fact directly, without a tree.

`DefinitionKinds` returns the normalized kinds the compiled query can emit,
computed from its capture names alone, not from the language's grammar as a
whole. A Go outliner, for example, reports only `function` and `method`,
because the Go tags override carries no pattern for a type, a constant, or a
variable. Check this list before trusting a zero omission count: a kind
absent from it can never appear in the outline, however many such
definitions the source holds.

## OutlineSymbol

| Field | Type | Meaning |
|---|---|---|
| `Kind` | `string` | Normalized definition kind. Derived from the `definition.X` capture suffix through a fixed table, then refined by node type for `enum` and `record`. Never derived from the language name. |
| `Name` | `string` | Text of the `@name` capture, trimmed. Read through `QueryCapture.Text`, so a `#strip!` directive on the capture applies. |
| `NodeType` | `string` | Grammar node type of the captured definition node. |
| `Range` | `Range` | Full byte and point span of the definition node. |
| `NameRange` | `Range` | Span of the `@name` capture. Always contained in `Range`; a candidate that breaks containment is omitted, not emitted with a broken span. |
| `Owner` | `string` | Non-lexical owner, for example a method's receiver type. Always empty today; see "Owner rules" below. |
| `Children` | `[]OutlineSymbol` | Definitions lexically nested inside this one, in source order. Nesting comes from byte containment, never from the language name. |

## OutlineReport

`OutlineTree` returns a report together with the symbols. Every definition
candidate the tags query produces is either emitted as a symbol or counted
in exactly one field below, so a dropped candidate can never look like an
absent one.

| Field | Meaning |
|---|---|
| `Symbols` | Emitted symbols, nested symbols included. |
| `OmittedNoName` | The definition capture had no name capture, or the name text trimmed to empty. |
| `OmittedDuplicate` | An earlier candidate already held the identical range, kind, name, and name range. A true repeat. |
| `OmittedNameConflict` | Two or more candidates shared a range and a kind but disagreed on the name. Every member of the group was dropped. |
| `OmittedConflict` | Two or more candidates shared a range with different kinds. Every member of the group was dropped. |
| `OmittedOverlap` | The candidate partially overlapped an already-accepted symbol; neither span contained the other. |
| `OmittedInvalidNameRange` | The name span sat outside the definition span, or the definition span itself was inverted. |
| `OmittedMultipleDefinitions` | One query match carried more than one `definition.X` capture, so the outline refused to pick one. |
| `OwnerRuleMisses` | Reserved for the owner-resolution work. Always `0` today; see "Owner rules" below. |
| `DeclineReason` | Why the outliner produced nothing, or `""` if it ran the query. See "Declining and omitting" below. |
| `Truncated` | The query hit the match limit or the match-work budget; the symbol list is partial. |
| `TreeHasError` | The parsed tree holds an `ERROR` or a `MISSING` node. See "Declining and omitting" below. |

Three methods summarize the counters:

```go
func (r OutlineReport) Declined() bool  // DeclineReason != ""
func (r OutlineReport) Omitted() int    // sum of every Omitted* counter
func (r OutlineReport) Candidates() int // Symbols + Omitted()
```

`Candidates()` counts query output, not source content. A definition shape
the query has no pattern for produces no candidate and no omission; it stays
invisible to every counter here. Read `DefinitionKinds()` to check for that
gap. Do not read `Omitted() == 0` as proof the outline is complete.

## Declining and omitting

`OutlineTree` fails closed at two separate levels, and the report keeps them
apart.

**Declining** means the outliner never ran the query at all. `DeclineReason`
names the cause:

| Reason | Cause |
|---|---|
| `nil_outliner` | The call ran on a `nil` `*Outliner`. |
| `query_empty` | The language has no tags query; `NewOutliner` already recorded this. |
| `nil_tree` | The `*Tree` argument was `nil`. |
| `nil_root_node` | The tree had no root node. |
| `language_mismatch` | The tree's language does not match the outliner's language. This is caller misuse, not a fact about the source. |

An empty `DeclineReason` together with zero `Symbols` states a different
fact from every declined case above: the query ran and matched nothing.

**Omitting** means the query ran and produced a candidate that the ambiguity
rules then dropped, recorded in one of the `Omitted*` fields. The rules
never guess. A span that could name two different things, or hold two
different kinds at once, is dropped and counted — not resolved by picking
one option over the other.

`TreeHasError` is a third, separate signal. It reports that the parser did
not fully recover the source. Definitions inside or after an unrecovered
region can be absent, over-long, or missing outright, and no counter fires
for them, because the query never produced a candidate to omit in the first
place. Do not treat "every counter is zero" as "the outline is complete" on
such a tree. Do not assume the symbols before the first error are safe
either — a truncated definition is itself a symbol, and its range can
swallow everything that follows it.

## Bounding query work

Two options bound the underlying query execution and set `Truncated` when
the bound is hit:

```go
func WithOutlineMatchLimit(limit uint32) OutlinerOption
func WithOutlineMatchWorkBudget(budget int) OutlinerOption
```

`WithOutlineMatchLimit` caps the number of query matches the outliner
accepts. `WithOutlineMatchWorkBudget` caps the enumeration steps the matcher
takes per pattern and per node; raise it for a large file whose outline
comes back truncated. A value of `0` on either option leaves the query
engine's default in place — it does not disable the underlying guard.

## Owner rules

`OutlineOwnerRule` declares how to resolve a definition's non-lexical owner,
for example the receiver type of a Go method:

```go
type OutlineOwnerRule struct {
	NodeType   string   // definition node type the rule applies to
	OwnerField string   // field name read through Node.ChildByFieldName
	Unwrap     []string // node types the rule descends through
	NameTypes  []string // accepted terminal node types
}
```

A Go receiver rule looks like this:

```go
gotreesitter.OutlineOwnerRule{
	NodeType:   "method_declaration",
	OwnerField: "receiver",
	Unwrap:     []string{"parameter_list", "parameter_declaration", "pointer_type", "generic_type"},
	NameTypes:  []string{"type_identifier"},
}
```

Attach rules with the constructor option:

```go
func WithOutlineOwnerRules(rules []OutlineOwnerRule) OutlinerOption
```

**Current status: attachment only, no resolution.** `NewOutliner` validates
each rule — it must name a `NodeType` and an `OwnerField`, or construction
fails — and retains it, but `OutlineTree` does not apply owner rules yet.
`Owner` stays `""` on every symbol, and `OwnerRuleMisses` stays `0`, no
matter which rules are attached. Two committed tests pin this directly:
`TestOutlineOwnerStaysEmpty` attaches the Go receiver rule above to a
fixture and asserts the outline is byte-for-byte identical with and without
it; `TestOutlineGoFixtureHasReceiverMethods` proves that proof is not
vacuous by confirming the fixture's methods really do carry a `receiver`
field. Owner resolution reads a grammar field, so it ships as its own
change behind its own C-oracle differential gate, described below.

## Language coverage

The outline runs on whatever tags query `grammars.ResolveTagsQuery`
resolves for a language. Not every one of the 206 registered languages
resolves a non-empty query today.

`grammars/testdata/outline_census/baseline.json` is the committed source of
truth for coverage. Regenerate it with:

```sh
GTS_OUTLINE_CENSUS_OUT=harness_out/outline/tier_census.json \
GOFLAGS=-buildvcs=false go test ./grammars -run TestOutlineTierCensus -count=1
```

The census sorts every language into exactly one tier:

| Tier | Meaning | Count |
|---|---|---|
| T1 | The tags query resolves, and a real-corpus fixture for the language is present on the census host. | 83 |
| T2 | The tags query resolves, but no real-corpus fixture is present, so the language's own smoke sample stands in. | 1 |
| T3 | The tags query is empty, but the grammar defines at least one node type the shared inference table could match. Promoting it is a data edit, not new mechanism code. | 10 |
| T4 | The tags query is empty, and the grammar defines none of those node types. Declined, with a receipt (`DeclineReason: "query_empty"`), and nothing to promote without new data. | 112 |

T1 and T2 together are the languages that resolve a tags query at all: 84 of
206. `TestInferredTagsQueryCoverage` (`grammars/registry_test.go`) enforces
this count as a floor. A language that drops out of T1 or T2 without a
stated reason and an updated baseline fails that test.

A resolved query is necessary but not sufficient — a pattern can compile and
still never fire against real source. `grammars/outline_coverage_witness_test.go`
runs 30 of the T1 languages through the exact pipeline the quick-start
example above uses, and asserts each one produces a specific, named symbol,
not just a non-empty query string.

## The C-oracle differential

Everything above runs entirely inside this repository's own Go query engine
and Go builder. One test compares that result against an external
reference: `cgo_harness/outline_differential_test.go` runs the SAME
resolved tags query through both the pure-Go query engine and the official
C tree-sitter runtime, over the same parsed source, and diffs the two
capture streams field by field — pattern index, capture name, node type,
start byte, end byte, and captured text.

It gates in two tiers:

- CoreNine (`go`, `python`, `javascript`, `typescript`, `tsx`, `rust`,
  `java`, `c`, `cpp`): hard-asserted. A capture-stream divergence, a query
  compile failure against the locked C grammar, or an unexpectedly empty
  tags query fails the test.
- FleetCensus: every other language with a resolvable tags query. Log-only;
  it produces a census table, not a pass-or-fail result, at a breadth the
  `GTS_PARITY_MODE` environment variable controls.

Two outcomes count as neither divergence nor parity. A language with no
resolvable tags query is `vacuous`: nothing ran, so there is nothing to
compare. A query that fails to compile against one engine's grammar is
`query_incompatible`: an inferred query is generated from the Go grammar's
own symbol table, and is not guaranteed to compile against the separately
pinned C grammar. Both outcomes get their own census bucket, so a coverage
gap can never masquerade as parity.

Run it:

```sh
cd cgo_harness && CGO_ENABLED=1 GTS_PARITY_ALLOW_HOST=1 \
  go test . -tags "treesitter_c_parity" -run '^TestOutlineOracleDifferential$' -count=1
```

This gate is also how one class of defect surfaced and was fixed. The C
query compiler statically rejects a pattern whose child node type can never
occur at that position in the grammar's rules — an "Impossible pattern" —
and it rejects the whole multi-pattern query when it hits one. The pure-Go
query engine has no matching static check, so a dead pattern line still
"compiled" there and produced zero matches, without ever surfacing as a
failure on its own. Several core-language tags overrides carried exactly
this shape. The differential test caught it, and the affected rows were
removed or corrected so both engines now agree.
