package gotreesitter_test

import (
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type collapsedChildStrictSubsetRow struct {
	language   string
	load       func() *gotreesitter.Language
	parent     string
	child      string
	childNamed bool
	bySource   bool
}

func TestCollapsedChildNativePolicyIsStrictSubsetOfLegacyRepair(t *testing.T) {
	rows := []collapsedChildStrictSubsetRow{
		{language: "ruby", load: grammars.RubyLanguage, parent: "bare_string", child: "string_content", childNamed: true},
		{language: "ruby", load: grammars.RubyLanguage, parent: "bare_symbol", child: "string_content", childNamed: true},
		{language: "apex", load: grammars.ApexLanguage, parent: "after_delete", child: "after_delete"},
		{language: "apex", load: grammars.ApexLanguage, parent: "after_insert", child: "after_insert"},
		{language: "apex", load: grammars.ApexLanguage, parent: "after_undelete", child: "after_undelete"},
		{language: "apex", load: grammars.ApexLanguage, parent: "after_update", child: "after_update"},
		{language: "apex", load: grammars.ApexLanguage, parent: "before_delete", child: "before_delete"},
		{language: "apex", load: grammars.ApexLanguage, parent: "before_insert", child: "before_insert"},
		{language: "apex", load: grammars.ApexLanguage, parent: "before_update", child: "before_update"},
		{language: "apex", load: grammars.ApexLanguage, parent: "delete", child: "delete"},
		{language: "apex", load: grammars.ApexLanguage, parent: "insert", child: "insert"},
		{language: "apex", load: grammars.ApexLanguage, parent: "super", child: "super"},
		{language: "apex", load: grammars.ApexLanguage, parent: "system", child: "system"},
		{language: "apex", load: grammars.ApexLanguage, parent: "undelete", child: "undelete"},
		{language: "apex", load: grammars.ApexLanguage, parent: "upsert", child: "upsert"},
		{language: "apex", load: grammars.ApexLanguage, parent: "user", child: "user"},
		{language: "kotlin", load: grammars.KotlinLanguage, parent: "identifier", child: "simple_identifier", childNamed: true},
		{language: "hack", load: grammars.HackLanguage, parent: "true", child: "true", bySource: true},
		{language: "hack", load: grammars.HackLanguage, parent: "false", child: "false", bySource: true},
		{language: "hack", load: grammars.HackLanguage, parent: "null", child: "null", bySource: true},
		{language: "dart", load: grammars.DartLanguage, parent: "super", child: "super", bySource: true},
		{language: "dart", load: grammars.DartLanguage, parent: "this", child: "this", bySource: true},
		{language: "elixir", load: grammars.ElixirLanguage, parent: "nil", child: "nil", bySource: true},
	}
	if len(rows) != 23 {
		t.Fatalf("strict-subset rows=%d, want 23", len(rows))
	}
	for _, row := range rows {
		row := row
		t.Run(row.language+"/"+row.parent, func(t *testing.T) {
			lang := row.load()
			if lang.NativeResultCompatibility&gotreesitter.ResultCompatibilityNativeCollapsedChildren == 0 {
				t.Fatal("exact artifact omitted native collapsed-child capability")
			}
			parent, parentOK := collapsedChildStrictSubsetSymbol(lang, row.parent, true)
			child, childOK := collapsedChildStrictSubsetSymbol(lang, row.child, row.childNamed)
			if !parentOK || !childOK {
				t.Fatalf("could not resolve exact pair %s/%s", row.parent, row.child)
			}
			parser := gotreesitter.NewParser(lang)
			if !gotreesitter.ParserRetainsCollapsedChildOccurrenceForTest(parser, parent, child) {
				t.Fatal("native predicate rejected exact authenticated pair")
			}
			if gotreesitter.ParserRetainsCollapsedChildOccurrenceForTest(parser, parent, 0) ||
				gotreesitter.ParserRetainsCollapsedChildOccurrenceForTest(parser, 0, child) {
				t.Fatal("native predicate accepted a parent/raw-child mismatch")
			}

			if row.bySource {
				meta := lang.SymbolMetadata[child]
				if int(child) >= int(lang.TokenCount) || !meta.Visible || meta.Named || lang.SymbolNames[child] != row.child {
					t.Fatalf("source row child=%d token_count=%d meta=%+v canonical=%q", child, lang.TokenCount, meta, lang.SymbolNames[child])
				}
			}

			end := uint32(len(row.child))
			collapsed := gotreesitter.NewLeafNode(parent, true, 0, end, gotreesitter.Point{}, gotreesitter.Point{Column: end})
			visited, rewritten := gotreesitter.NormalizeCollapsedNamedLeafForTest(
				collapsed, []byte(row.child), lang, row.parent, row.child, row.bySource,
			)
			if visited == 0 || rewritten != 1 || collapsed.ChildCount() != 1 ||
				collapsed.Child(0).Symbol() != child || collapsed.Child(0).IsNamed() != row.childNamed {
				t.Fatalf("legacy repair visited=%d rewritten=%d shape=%s", visited, rewritten, collapsed.SExpr(lang))
			}
			if row.bySource {
				wrongSource := gotreesitter.NewLeafNode(parent, true, 0, end, gotreesitter.Point{}, gotreesitter.Point{Column: end})
				_, rewritten = gotreesitter.NormalizeCollapsedNamedLeafForTest(
					wrongSource, []byte("xxxxxx")[:len(row.child)], lang, row.parent, row.child, true,
				)
				if rewritten != 0 || wrongSource.ChildCount() != 0 {
					t.Fatal("source-verified legacy repair accepted a different literal")
				}
			}
		})
	}
}

func collapsedChildStrictSubsetSymbol(lang *gotreesitter.Language, name string, named bool) (gotreesitter.Symbol, bool) {
	for index, symbolName := range lang.SymbolNames {
		if symbolName == name && index < len(lang.SymbolMetadata) && lang.SymbolMetadata[index].Named == named {
			return gotreesitter.Symbol(index), true
		}
	}
	return 0, false
}
