//go:build !gts_no_parsercorephase0

package gotreesitter

import (
	"reflect"
	"sync"
	"testing"
)

// The per-language derived parser tables are built once and shared by every
// Parser of that Language, where NewParser previously rebuilt them per call.
// These tests prove the shared tables are the SAME tables, that the build is
// safe under concurrent first use, and that the inputs the builders read are
// disjoint from the fields callers mutate after load.

// derivedTablesTestLanguage decodes a FRESH Language from the certified Go
// blob for each test. A fresh decode matters: the memo is per-Language, so
// sharing one instance across tests would let an earlier test's build satisfy
// a later test's first-use assertion.
func derivedTablesTestLanguage(t *testing.T) *Language {
	t.Helper()
	lang, err := LoadLanguage(parserCoreCertifiedGoBlob)
	if err != nil {
		t.Skip(err)
	}
	return lang
}

// TestParserDerivedTablesMatchFreshBuilds is the correctness claim the whole
// change rests on: memoizing must not change WHAT is built, only how often.
// It rebuilds each table directly from the Language and compares against the
// memoized instance the Parser now receives.
func TestParserDerivedTablesMatchFreshBuilds(t *testing.T) {
	lang := derivedTablesTestLanguage(t)
	derived := lang.acquireParserDerivedTables()

	freshSmallTokenLookup := buildSmallTokenLookup(lang)
	if !reflect.DeepEqual(derived.smallTokenLookup, freshSmallTokenLookup) {
		t.Fatal("memoized smallTokenLookup differs from a fresh build")
	}
	if !reflect.DeepEqual(derived.smallLookup, buildSmallLookup(lang, freshSmallTokenLookup)) {
		t.Fatal("memoized smallLookup differs from a fresh build")
	}
	if !reflect.DeepEqual(derived.classifiedActions, buildClassifiedParseActions(lang)) {
		t.Fatal("memoized classifiedActions differs from a fresh build")
	}
	if !reflect.DeepEqual(derived.keepSameNamedAnonChildSymbol, buildKeepSameNamedAnonChildSymbols(lang)) {
		t.Fatal("memoized keepSameNamedAnonChildSymbol differs from a fresh build")
	}
	if !reflect.DeepEqual(derived.sharedAnonymousTokenSymbol, buildSharedAnonymousTokenSymbols(lang)) {
		t.Fatal("memoized sharedAnonymousTokenSymbol differs from a fresh build")
	}

	// eagerDefaultReduces is the one table whose builder needs a *Parser. Build
	// it the way NewParser would have, through a real Parser, and require the
	// memoized copy to match. This is what catches a drifted denseLimit or
	// smallBase on the scratch parser inside acquireParserDerivedTables.
	reference := NewParser(lang)
	if !reflect.DeepEqual(derived.eagerDefaultReduces, buildEagerDefaultReduceActions(reference)) {
		t.Fatal("memoized eagerDefaultReduces differs from one built through a real Parser")
	}
}

// TestParserDerivedTablesAreSharedNotCopied proves the memo actually shares:
// two Parsers of one Language must receive the identical backing arrays, which
// is where the allocation saving comes from.
func TestParserDerivedTablesAreSharedNotCopied(t *testing.T) {
	lang := derivedTablesTestLanguage(t)
	first, second := NewParser(lang), NewParser(lang)

	if len(first.classifiedActions) == 0 {
		t.Fatal("fixture language produced no classified actions")
	}
	if &first.classifiedActions[0] != &second.classifiedActions[0] {
		t.Fatal("two Parsers of one Language hold different classifiedActions arrays")
	}
	if len(first.sharedAnonymousTokenSymbol) != 0 &&
		&first.sharedAnonymousTokenSymbol[0] != &second.sharedAnonymousTokenSymbol[0] {
		t.Fatal("two Parsers of one Language hold different sharedAnonymousTokenSymbol arrays")
	}
	if len(first.smallTokenLookup) != 0 &&
		&first.smallTokenLookup[0] != &second.smallTokenLookup[0] {
		t.Fatal("two Parsers of one Language hold different smallTokenLookup arrays")
	}
}

// TestParserDerivedTablesConcurrentFirstUse covers the reason the build is
// lazy rather than eager. A cached *Language is served to every goroutine
// (grammars' embedded loader), and ParserPool constructs Parsers concurrently
// against it, so first use races by construction. Run with -race.
func TestParserDerivedTablesConcurrentFirstUse(t *testing.T) {
	lang := derivedTablesTestLanguage(t)

	const goroutines = 16
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make([]*parserDerivedTables, goroutines)
	for i := range results {
		done.Add(1)
		go func(index int) {
			defer done.Done()
			start.Wait()
			results[index] = lang.acquireParserDerivedTables()
		}(i)
	}
	start.Done()
	done.Wait()

	for i, got := range results {
		if got == nil {
			t.Fatalf("goroutine %d received no derived tables", i)
		}
		if got != results[0] {
			t.Fatalf("goroutine %d received a different derived-table instance; the build ran more than once", i)
		}
	}
}

// TestParserDerivedTablesReadOnlyPostLoadMutableFields is the scoping guard.
// Callers DO mutate a *Language after load: runtime profiles set the compact
// certification flags, and scanner attach swaps ExternalScanner. Memoizing is
// only safe because the memoized builders read none of those fields. This test
// pins the boundary by mutating the post-load-mutable fields and requiring the
// derived tables to be unchanged.
//
// It does NOT prove the general claim by construction; it fails loudly if
// someone later memoizes a table that does read one of these.
func TestParserDerivedTablesReadOnlyPostLoadMutableFields(t *testing.T) {
	lang := derivedTablesTestLanguage(t)
	before := lang.acquireParserDerivedTables()

	restoreErrorRegion := lang.CompactStrategy2ErrorRegionCertified
	restoreSplitDrops := lang.CompactConvergedReductionSplitDropsCertified
	t.Cleanup(func() {
		lang.CompactStrategy2ErrorRegionCertified = restoreErrorRegion
		lang.CompactConvergedReductionSplitDropsCertified = restoreSplitDrops
	})
	lang.CompactStrategy2ErrorRegionCertified = !restoreErrorRegion
	lang.CompactConvergedReductionSplitDropsCertified = !restoreSplitDrops

	after := lang.acquireParserDerivedTables()
	if before != after {
		t.Fatal("mutating a post-load-mutable Language field rebuilt the derived tables")
	}
	if !reflect.DeepEqual(after.classifiedActions, buildClassifiedParseActions(lang)) {
		t.Fatal("a memoized table now disagrees with a fresh build after a post-load mutation; a builder reads a mutable field")
	}
}
