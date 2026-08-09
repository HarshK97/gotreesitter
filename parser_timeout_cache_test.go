package gotreesitter

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestActiveParseStopCheckReusesBoundMethod(t *testing.T) {
	parser := &Parser{parseBudgetDepth: 1}
	check := parser.activeParseStopCheck()
	if check == nil {
		t.Fatal("active parse stop check is nil")
	}

	parser.parseStoppedReason = ParseStopTimeout
	if got := check(); got != ParseStopTimeout {
		t.Fatalf("cached stop check returned %q, want %q", got, ParseStopTimeout)
	}

	allocs := testing.AllocsPerRun(100, func() {
		if parser.activeParseStopCheck() == nil {
			t.Fatal("cached active parse stop check is nil")
		}
	})
	if allocs != 0 {
		t.Fatalf("cached active parse stop check allocated %.2f objects per call", allocs)
	}
}

func TestNewParserBindsActiveParseStopCheck(t *testing.T) {
	parser := NewParser(nil)
	if parser.activeParseStopCheckFn == nil {
		t.Fatal("new parser did not bind its active parse stop check")
	}
}

func TestMaterializationStopPollsDeadlineAtBoundedCadence(t *testing.T) {
	parser := &Parser{
		parseBudgetDepth: 1,
		parseDeadline:    time.Now().Add(time.Hour),
		budgetScratch:    &parserScratch{},
	}
	if got := parser.materializationParseStopReason(); got != ParseStopNone {
		t.Fatalf("initial materialization stop = %q, want %q", got, ParseStopNone)
	}
	parser.parseDeadline = time.Now().Add(-time.Hour)
	for i := 0; i < materializationDeadlinePollMask; i++ {
		if got := parser.materializationParseStopReason(); got != ParseStopNone {
			t.Fatalf("materialization stop before deadline poll %d = %q, want %q", i, got, ParseStopNone)
		}
	}
	if got := parser.materializationParseStopReason(); got != ParseStopTimeout {
		t.Fatalf("bounded materialization deadline poll = %q, want %q", got, ParseStopTimeout)
	}
}

func TestMaterializationStopChecksCancellationEveryCall(t *testing.T) {
	var cancelled uint32
	parser := &Parser{
		parseBudgetDepth: 1,
		parseDeadline:    time.Now().Add(time.Hour),
		cancellationFlag: &cancelled,
		budgetScratch:    &parserScratch{},
	}
	if got := parser.materializationParseStopReason(); got != ParseStopNone {
		t.Fatalf("initial materialization stop = %q, want %q", got, ParseStopNone)
	}
	atomic.StoreUint32(&cancelled, 1)
	if got := parser.materializationParseStopReason(); got != ParseStopCancelled {
		t.Fatalf("materialization cancellation stop = %q, want %q", got, ParseStopCancelled)
	}
}
