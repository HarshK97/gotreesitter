//go:build gts_parsercorephase0

package gotreesitter

import (
	"strings"
	"testing"
)

func parserCoreFreshFullCanonicalOptions() DiagnosticParserCorePrefixOptions {
	return DiagnosticParserCorePrefixOptions{
		ReceiptMode:   DiagnosticParserCoreReceiptSummary,
		MaxTokens:     300000,
		MaxDispatches: 600000,
		Limits:        diagnosticParserCoreCanonicalLimits(),
	}
}

func TestParserCoreFreshFullRunnerRepeatedCanonicalLifecycle(t *testing.T) {
	runner, err := newParserCoreFreshFullRunner(parserCoreWarmGoScanner, parserCoreFreshFullCanonicalOptions())
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		for _, row := range diagnosticParserCoreCanonicalAdmissions {
			fixture := loadDiagnosticParserCoreCanonicalFixture(t, row.id)
			requireDiagnosticParserCoreCanonicalFixtureIdentity(t, fixture, row)
			tree, err := runner.parse(fixture.Source)
			if err != nil {
				t.Fatalf("pass %d fixture %s: %v", pass, row.id, err)
			}
			requireDiagnosticParserCoreCanonicalEOF(t, tree, len(fixture.Source))
			if got := requireDiagnosticParserCoreCanonicalTreeDigest(t, tree, runner.lang); got != row.deepTreeSHA256 {
				tree.Release()
				t.Fatalf("pass %d fixture %s deep-tree digest=%s want=%s", pass, row.id, got, row.deepTreeSHA256)
			}
			if work := runner.compact.Work(); work != row.work || work.Overflow {
				tree.Release()
				t.Fatalf("pass %d fixture %s compact work=%+v want=%+v", pass, row.id, work, row.work)
			}
			tree.Release()
		}
	}
}

func TestParserCoreFreshFullRunnerResetsAfterCap(t *testing.T) {
	options := parserCoreFreshFullCanonicalOptions()
	runner, err := newParserCoreFreshFullRunner(parserCoreWarmGoScanner, options)
	if err != nil {
		t.Fatal(err)
	}
	fixture := loadDiagnosticParserCoreCanonicalFixture(t, "rewrite")

	runner.options.MaxDispatches = 1
	if tree, err := runner.parse(fixture.Source); err == nil || tree != nil || !strings.Contains(err.Error(), "dispatch cap") {
		if tree != nil {
			tree.Release()
		}
		t.Fatalf("capped parse tree=%v err=%v, want fail-closed dispatch cap", tree != nil, err)
	}

	runner.options = options
	tree, err := runner.parse(fixture.Source)
	if err != nil {
		t.Fatalf("parse after cap did not reset reusable state: %v", err)
	}
	defer tree.Release()
	requireDiagnosticParserCoreCanonicalEOF(t, tree, len(fixture.Source))
	if work := runner.compact.Work(); work != diagnosticParserCoreCanonicalAdmissions[0].work || work.Overflow {
		t.Fatalf("parse after cap work=%+v want=%+v", work, diagnosticParserCoreCanonicalAdmissions[0].work)
	}
}

func TestParserCoreFreshFullRunnerFailsClosedAtConstruction(t *testing.T) {
	base := parserCoreFreshFullCanonicalOptions()
	if _, err := newParserCoreFreshFullRunner(nil, base); err == nil || !strings.Contains(err.Error(), "external scanner identity mismatch") {
		t.Fatalf("unauthenticated scanner error=%v", err)
	}

	tests := []struct {
		name   string
		mutate func(*DiagnosticParserCorePrefixOptions)
	}{
		{name: "recovery", mutate: func(o *DiagnosticParserCorePrefixOptions) { o.Recovery = true }},
		{name: "retry", mutate: func(o *DiagnosticParserCorePrefixOptions) { o.Retry = true }},
		{name: "incremental", mutate: func(o *DiagnosticParserCorePrefixOptions) { o.Incremental = true }},
		{name: "included-ranges", mutate: func(o *DiagnosticParserCorePrefixOptions) { o.IncludedRanges = true }},
		{name: "full-receipts", mutate: func(o *DiagnosticParserCorePrefixOptions) { o.ReceiptMode = DiagnosticParserCoreReceiptFull }},
		{name: "closed-prefix", mutate: func(o *DiagnosticParserCorePrefixOptions) {
			closed := uint32(1)
			o.GenericStopAtClosedByte = &closed
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := base
			test.mutate(&options)
			if runner, err := newParserCoreFreshFullRunner(parserCoreWarmGoScanner, options); err == nil || runner != nil {
				t.Fatalf("unsupported route runner=%v err=%v", runner != nil, err)
			}
		})
	}
}
