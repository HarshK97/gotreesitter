package grammargen

import "testing"

// TestLanguageVersionStampedForSupertypeMapWithoutReservedWords pins the
// Stage-1 fix to assemble()'s LanguageVersion stamping: any ABI-15-only
// surface (not just the reserved-word table) must bump LanguageVersion to
// 15. Before the fix, assemble() hardcoded LanguageVersion=14 and only
// bumped it to 15 inside buildReservedWordTables, so a grammar with
// supertypes but no reserved words shipped a non-empty SupertypeMapEntries
// (an ABI-15 surface) under a LanguageVersion=14 stamp — an internally
// inconsistent blob.
func TestLanguageVersionStampedForSupertypeMapWithoutReservedWords(t *testing.T) {
	g := NewGrammar("supertype_only_abi15")
	g.Define("source_file", Sym("expression"))
	g.Define("expression", Choice(
		Sym("number_literal"),
		Sym("string_literal"),
	))
	g.Define("number_literal", Str("1"))
	g.Define("string_literal", Str("x"))
	g.SetSupertypes("expression")

	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage: %v", err)
	}
	if len(lang.SupertypeMapEntries) == 0 {
		t.Fatal("test grammar did not exercise SupertypeMapEntries; test is not testing what it claims")
	}
	if len(lang.ReservedWords) != 0 {
		t.Fatal("test grammar unexpectedly populated ReservedWords; test is not isolating the supertype-map surface")
	}
	if lang.LanguageVersion != 15 {
		t.Fatalf("LanguageVersion = %d, want 15 (SupertypeMapEntries is an ABI-15 surface)", lang.LanguageVersion)
	}
}

// TestLanguageVersionStaysAtBaselineWithoutAnyABI15Surface guards the other
// direction: a grammar with no reserved words, no supertypes, and no
// metadata must not be over-stamped to 15 — that would make the language
// falsely claim ABI-15 features it doesn't have.
func TestLanguageVersionStaysAtBaselineWithoutAnyABI15Surface(t *testing.T) {
	g := NewGrammar("no_abi15_surface")
	g.Define("source_file", Str("x"))

	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage: %v", err)
	}
	if len(lang.SupertypeMapEntries) != 0 || len(lang.ReservedWords) != 0 {
		t.Fatal("test grammar unexpectedly populated an ABI-15 surface; test is not isolating the baseline case")
	}
	if lang.LanguageVersion != 14 {
		t.Fatalf("LanguageVersion = %d, want 14 (no ABI-15 surface present)", lang.LanguageVersion)
	}
}
