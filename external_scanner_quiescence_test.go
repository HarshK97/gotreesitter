package gotreesitter

import "testing"

// quiescenceOptOutScanner models a genuinely stateful scanner (Python's
// indentation stack is the production example): it implements
// IncrementalReuseExternalScanner and opts out. The classifier must refute it.
type quiescenceOptOutScanner struct {
	parserTestUnsafeExternalScanner
}

func (quiescenceOptOutScanner) SupportsIncrementalReuse() bool { return false }

// quiescenceStatelessScanner models Go's automatic-semicolon scanner: it
// carries no cross-token state and declares itself stateless. The classifier
// must prove it quiescent.
type quiescenceStatelessScanner struct {
	parserTestUnsafeExternalScanner
}

func (quiescenceStatelessScanner) ExternalScannerIsStateless() bool { return true }

func TestClassifyExternalScannerQuiescence(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		want externalScannerQuiescence
	}{
		{"nil language", nil, scannerQuiescenceProven},
		{"no scanner", &Language{}, scannerQuiescenceProven},
		{"stateless scanner", &Language{ExternalScanner: quiescenceStatelessScanner{}}, scannerQuiescenceProven},
		{"opt-out scanner", &Language{ExternalScanner: quiescenceOptOutScanner{}}, scannerQuiescenceRefuted},
		{"undecided scanner", &Language{ExternalScanner: parserTestUnsafeExternalScanner{}}, scannerQuiescenceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyExternalScannerQuiescence(tc.lang); got != tc.want {
				t.Fatalf("classifyExternalScannerQuiescence = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestExternalScannerBoundaryQuiescentFailsClosed proves the predicate rejects
// a boundary it cannot prove quiescent. A stateful opt-out scanner is the
// "deliberately unprovable boundary" the W4 hard gate requires (an edit inside
// a raw string, or any state a fresh parse could not reconstruct, lands here).
func TestExternalScannerBoundaryQuiescentFailsClosed(t *testing.T) {
	if externalScannerBoundaryQuiescentWithoutCheckpoint(&Language{ExternalScanner: quiescenceOptOutScanner{}}) {
		t.Fatal("opt-out scanner admitted: predicate must fail closed on a refuted boundary")
	}
	if !externalScannerBoundaryQuiescentWithoutCheckpoint(&Language{ExternalScanner: quiescenceStatelessScanner{}}) {
		t.Fatal("stateless scanner rejected: predicate must admit a proven boundary")
	}
	if !externalScannerBoundaryQuiescentWithoutCheckpoint(&Language{}) {
		t.Fatal("no-scanner language rejected: predicate must admit a proven boundary")
	}
}

// TestCanReuseNodeFailsClosedOnRefutedScanner drives the actual reuse-admission
// hook with a refuted scanner and proves it rejects, without reading the node.
// The language-level gate blocks such scanners upstream in production, so this
// is the fail-closed backstop that keeps the hook itself sound.
func TestCanReuseNodeFailsClosedOnRefutedScanner(t *testing.T) {
	ts := &dfaTokenSource{language: &Language{ExternalScanner: quiescenceOptOutScanner{}}}
	if _, ok := canReuseNodeWithExternalScannerCheckpoint(ts, 0, nil); ok {
		t.Fatal("canReuseNodeWithExternalScannerCheckpoint admitted a refuted scanner")
	}
}

// TestCanReuseNodeAdmitsStatelessScanner proves the hook admits a proven
// (stateless) boundary, returning a zero checkpoint the non-checkpoint reuse
// path never reads.
func TestCanReuseNodeAdmitsStatelessScanner(t *testing.T) {
	ts := &dfaTokenSource{language: &Language{ExternalScanner: quiescenceStatelessScanner{}}}
	cp, ok := canReuseNodeWithExternalScannerCheckpoint(ts, 0, nil)
	if !ok {
		t.Fatal("canReuseNodeWithExternalScannerCheckpoint rejected a proven stateless scanner")
	}
	if cp != (externalScannerCheckpointRef{}) {
		t.Fatal("proven stateless boundary returned a non-zero checkpoint")
	}
}
