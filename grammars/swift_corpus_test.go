package grammars

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/odvcencio/gotreesitter"
)

// swiftCorpusStatus records the expected parse outcome for one corpus file.
type swiftCorpusStatus int

const (
	// swiftCorpusClean means the file must parse with HasError() == false.
	// A file in this state that starts reporting an error is a regression.
	swiftCorpusClean swiftCorpusStatus = iota
	// swiftCorpusKnownFailing means the file currently reports a parse
	// error from a known, tracked cause. A file in this state that starts
	// parsing cleanly means the underlying bug got fixed — the test forces
	// a ratchet: update the row to swiftCorpusClean.
	swiftCorpusKnownFailing
)

// swiftCorpusExpectation is one row of the corpus ratchet table.
type swiftCorpusExpectation struct {
	status swiftCorpusStatus
	// issue names the open GitHub issue(s) whose construct explains the
	// failure, for example "#558". Empty for clean rows. For a failure
	// that matches no open issue, this holds a "new:" prefixed note
	// describing the construct instead of an issue number.
	issue string
}

// swiftCorpusExpectations is the ratchet table for
// grammars/testdata/swift_corpus. Every *.swift file in that directory must
// have exactly one row here; TestSwiftCorpusManifestComplete enforces that.
//
// Populated by running gotreesitter's Swift grammar against each corpus file
// at the branch point (main @ 96671b92, before PRs #566-#570 land) and, for
// every failure, locating the first ERROR/MISSING node and matching its
// construct against the open issue that describes it.
var swiftCorpusExpectations = map[string]swiftCorpusExpectation{
	"stdlib_ASCII.swift": {
		status: swiftCorpusKnownFailing,
		// Matches no open issue. Minimal repro: `let x = 1&<<7`. The
		// masking left-shift operator `&<<` (and, presumably, the rest of
		// the overflow-operator family `&+`, `&-`, `&*`, `&>>`) breaks the
		// surrounding infix_expression, leaving an ERROR node in place of
		// the right operand. See stdlib_ASCII.swift's `encode(_:)`:
		// `guard source.value < (1&<<7) else { return nil }`.
		issue: "new: masking left-shift operator &<< breaks the enclosing infix_expression",
	},
	"stdlib_Collection.swift": {
		status: swiftCorpusKnownFailing,
		// Matches no open issue. Minimal repro:
		//   protocol P {
		//     associatedtype Iterator
		//     override __consuming func makeIterator() -> Iterator
		//   }
		// The `override` modifier combined with the `__consuming`
		// parameter-passing modifier on a protocol function requirement
		// produces an ERROR node covering `__consuming`. See
		// stdlib_Collection.swift's Sequence protocol requirement
		// `override __consuming func makeIterator() -> Iterator`.
		issue: "new: `override __consuming func` modifier combination on a protocol requirement",
	},
	"stdlib_CollectionAlgorithms.swift": {
		status: swiftCorpusKnownFailing,
		// Same root cause as stdlib_FloatingPointToString.swift: the
		// `unsafe` expression-prefix keyword. Verified by bisection: the
		// file parses clean through the `_halfStablePartition` function
		// (byte 13232) and only starts failing once `partition(by:)`
		// appears, which contains `try unsafe
		// withContiguousMutableStorageIfAvailable { ... }`.
		issue: "new: `unsafe` expression-prefix keyword (see stdlib_FloatingPointToString.swift)",
	},
	"stdlib_FloatingPointToString.swift": {
		status: swiftCorpusKnownFailing,
		// Matches no open issue. Minimal repro: `let x = unsafe bar()`.
		// The `unsafe` keyword used as an expression prefix (Swift's
		// strict-memory-safety opt-in, used throughout this file as
		// `unsafe buffer.storeBytes(...)`, `let x = unsafe
		// utf8Buffer.mutableBytes`, etc.) is not recognized; it leaves an
		// ERROR node in place of the wrapped expression.
		issue: "new: `unsafe` expression-prefix keyword is not recognized",
	},
	"stdlib_Optional.swift": {
		status: swiftCorpusKnownFailing,
		// Matches no open issue. Minimal repro:
		//   struct S {
		//     @lifetime(copy value)
		//     public init(_ value: Int) {}
		//   }
		// The `@lifetime(copy value)` / `@lifetime(borrow x)` lifetime-
		// dependency attribute argument is misparsed: HasError() is true
		// with NO ERROR node anywhere in the tree, only a zero-width
		// MISSING `integer_literal` inside the attribute's argument list
		// (same "phantom failure" shape #559 documents for a different
		// construct). See stdlib_Optional.swift's noncopyable-Optional
		// initializer `@lifetime(copy value) public init(_ value:
		// consuming Wrapped)`.
		issue: "new: `@lifetime(copy/borrow x)` attribute argument syntax produces a phantom MISSING node",
	},
	"stdlib_Repeat.swift": {
		status: swiftCorpusClean,
	},
	"stdlib_Stride.swift": {
		status: swiftCorpusKnownFailing,
		// Matches no open issue. Minimal repro:
		//   protocol P {
		//     associatedtype Stride: SignedNumeric, Comparable
		//   }
		// An associated type with more than one comma-separated
		// conformance constraint produces an ERROR node spanning the
		// first constraint and the comma. See stdlib_Stride.swift's
		// `associatedtype Stride: SignedNumeric, Comparable` in the
		// `Strideable` protocol.
		issue: "new: associatedtype with a comma-separated multi-conformance constraint list",
	},
	"swift-algorithms_AdjacentPairsTests.swift": {
		status: swiftCorpusKnownFailing,
		// Two independent causes, per issue #561's own text: the
		// triple-nested generic `IndexValidator<AdjacentPairsCollection<
		// Range<Int>>>()` on line 73 (issue #559), and the `for n in
		// 4...100 { }` loops on lines 37 and 66 each followed by another
		// test method inside the class body (issue #561). Either one
		// alone is sufficient to break this file.
		issue: "#559, #561",
	},
	"swift-algorithms_Chunked.swift": {
		status: swiftCorpusKnownFailing,
		// makeOffsetIndex's nested `if let limit = limit { if baseDistance
		// > 0 && limit == endIndex { if self.distance(from: i, to: limit)
		// < distance { ... } } }` is the repro 2 shape #558 names
		// verbatim for this file.
		issue: "#558",
	},
	"swift-algorithms_FlattenCollection.swift": {
		status: swiftCorpusKnownFailing,
		// Contains the nested-if-let shape #558 describes as repro 1
		// (`if let indexInner = index.inner { ... if let inner =
		// element.index(...) { ... } ... }` in the index-arithmetic
		// helpers), and #558 names this file explicitly.
		issue: "#558",
	},
	"swift-algorithms_Stride.swift": {
		status: swiftCorpusKnownFailing,
		// offsetForward/offsetBackward's `if limit < i { if let idx =
		// base.index(...) { ... } else { ... } } else if let idx =
		// base.index(...) { ... }` is the exact shape #560 names for this
		// file, at the exact function names (offsetForward,
		// offsetBackward) #560 cites.
		issue: "#560",
	},
	"swift-algorithms_Windows.swift": {
		status: swiftCorpusKnownFailing,
		// `index(before:)`'s `if index == endIndex { return Index(...) }
		// else { return Index(...) }` is the verbatim repro #560 quotes
		// for this file.
		issue: "#560",
	},
}

// TestSwiftCorpusManifestComplete ensures every *.swift file under
// testdata/swift_corpus has exactly one row in swiftCorpusExpectations, and
// that no stale rows point at files that no longer exist.
func TestSwiftCorpusManifestComplete(t *testing.T) {
	dir := "testdata/swift_corpus"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var onDisk []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".swift" {
			onDisk = append(onDisk, e.Name())
		}
	}
	sort.Strings(onDisk)

	var expected []string
	for name := range swiftCorpusExpectations {
		expected = append(expected, name)
	}
	sort.Strings(expected)

	onDiskSet := make(map[string]bool, len(onDisk))
	for _, name := range onDisk {
		onDiskSet[name] = true
	}
	for _, name := range expected {
		if !onDiskSet[name] {
			t.Errorf("swiftCorpusExpectations has a row for %q, but that file does not exist under %s", name, dir)
		}
	}

	expectedSet := make(map[string]bool, len(expected))
	for _, name := range expected {
		expectedSet[name] = true
	}
	for _, name := range onDisk {
		if !expectedSet[name] {
			t.Errorf("%s has no row in swiftCorpusExpectations; add one (clean or known_failing)", name)
		}
	}
}

// TestSwiftCorpus parses every file in grammars/testdata/swift_corpus and
// checks the outcome against swiftCorpusExpectations. A "clean" file that
// starts erroring is a regression. A "known_failing" file that starts
// parsing cleanly means the underlying bug got fixed, so the test fails on
// purpose to force the expectations table to ratchet forward.
func TestSwiftCorpus(t *testing.T) {
	dir := "testdata/swift_corpus"
	lang := SwiftLanguage()

	for name, exp := range swiftCorpusExpectations {
		name, exp := name, exp
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("reading corpus file: %v", err)
			}

			parser := gotreesitter.NewParser(lang)
			tree, err := parser.Parse(src)
			if err != nil {
				t.Fatalf("parsing corpus file: %v", err)
			}
			defer tree.Release()

			root := tree.RootNode()
			hasErr := root.HasError()

			switch exp.status {
			case swiftCorpusClean:
				if hasErr {
					t.Fatalf("regression: %s now reports a parse error but the expectations table marks it clean; "+
						"first bad node: %s", name, describeFirstSwiftCorpusFailure(root, lang))
				}
			case swiftCorpusKnownFailing:
				if !hasErr {
					t.Fatalf("ratchet needed: %s now parses cleanly, but the expectations table marks it "+
						"known_failing (%s); update its row in swiftCorpusExpectations to swiftCorpusClean", name, exp.issue)
				}
			default:
				t.Fatalf("%s has an unrecognized status", name)
			}
		})
	}
}

// describeFirstSwiftCorpusFailure narrows down from the root to the
// smallest descendant whose subtree still reports an error, then returns a
// short description of that node for failure messages.
func describeFirstSwiftCorpusFailure(root *gotreesitter.Node, lang *gotreesitter.Language) string {
	n := root
	for {
		if n.IsError() || n.IsMissing() {
			break
		}
		var next *gotreesitter.Node
		for i := 0; i < n.ChildCount(); i++ {
			c := n.Child(i)
			if c != nil && c.HasError() {
				next = c
				break
			}
		}
		if next == nil {
			break
		}
		n = next
	}
	return n.Type(lang)
}
