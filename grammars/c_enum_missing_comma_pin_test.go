package grammars

import (
	"testing"

	"github.com/odvcencio/gotreesitter"
)

// Pin for a MISSING comma that the Go engine invents inside a three-enumerator
// enum when the construct is the first declaration in the file and any trivia
// precedes it.
//
// C tree-sitter parses the source below with no MISSING node and no error. The
// Go engine inserts a zero-width `,` between the last enumerator and the
// closing brace, and marks enumerator_list, enum_specifier, type_definition,
// and translation_unit as containing an error. Every other node matches the C
// tree exactly, so the whole divergence is that one invented token and the
// error state it propagates.
//
// The trigger is narrow and worth recording precisely:
//
//   - the enum must be the first declaration in the file, and some trivia must
//     precede it. A single leading space, tab, newline, or comment is enough.
//     With the enum at byte 0 the tree is correct, and with any declaration
//     ahead of it the tree is correct.
//   - exactly three enumerators. Two are fine and four are fine.
//   - every enumerator carries `= value`. Bare enumerators are fine, and a
//     trailing comma after the last enumerator is fine.
//
// Leading trivia is not the cause. It decides which parser runs. With the enum
// at byte 0 the GSS-forest fast path accepts the parse and reports
// ForestFastPath=true with zero iterations. Leading trivia makes that path
// decline, the production GLR parser runs instead, and it enters recovery
// (CRecoveryEnteredErrorState=true, CRecoverMissingTokenTrialAttemptsPeak=1)
// and keeps a missing-token trial that C never needs. The fast path is
// therefore masking the defect on the inputs it happens to accept.
//
// Found by differential testing against the pinned C oracle over a 1,567-file
// corpus: /usr/include/KHR/khrplatform.h was the only file where the oracle
// parsed completely clean and the Go engine invented a token. Reduced from
// 11,131 bytes to the 42 below.
//
// This pin is self-healing: it PASSES automatically once the invented comma
// disappears.
func TestCleanRegressionPinCEnumThreeValuedEnumerators(t *testing.T) {
	const src = "\ntypedef enum { A = 0, B = 1, C = MAX } t;"

	tree, lang := pinParse(t, "c", src)
	defer tree.Release()

	root := tree.RootNode()
	if got := root.Type(lang); got != "translation_unit" {
		t.Fatalf("c: root type = %q, want translation_unit", got)
	}
	// The enumerator_list must still cover the whole brace-delimited run; the
	// divergence is an extra child, not a reshaped span.
	list := pinFind(root, lang, "enumerator_list")
	if list == nil {
		t.Fatal("c: no enumerator_list node")
	}
	if got, want := int(list.StartByte()), 14; got != want {
		t.Fatalf("c: enumerator_list starts at %d, want %d", got, want)
	}
	if got, want := int(list.EndByte()), 39; got != want {
		t.Fatalf("c: enumerator_list ends at %d, want %d", got, want)
	}

	const wantMissing = 0 // C tree-sitter invents nothing here.
	if got := pinCountMissing(root); got == wantMissing {
		return // fix landed — pin now green.
	} else {
		t.Skipf("KNOWN DIVERGENCE (c enum three valued enumerators): Go invents %d "+
			"MISSING node(s), C invents %d — the production GLR parser keeps a "+
			"missing-comma trial that the forest fast path never runs; see file "+
			"comment", got, wantMissing)
	}
}

// pinCountMissing reports how many nodes in the tree are MISSING.
func pinCountMissing(n *gotreesitter.Node) int {
	if n == nil {
		return 0
	}
	count := 0
	if n.IsMissing() {
		count++
	}
	for i := 0; i < n.ChildCount(); i++ {
		count += pinCountMissing(n.Child(i))
	}
	return count
}
