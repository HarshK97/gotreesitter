package main

import (
	"testing"
)

func TestScoreRowsSplitsCleanAndErrorRatios(t *testing.T) {
	rows := []reportRow{
		{Language: "kdl", Sample: "clean1.kdl", Mode: "cgo_full", MedianNS: 10, Parity: paritySummary{Clean: true}},
		{Language: "kdl", Sample: "clean1.kdl", Mode: "go_full", MedianNS: 15, Parity: paritySummary{Clean: true}},
		{Language: "kdl", Sample: "clean2.kdl", Mode: "cgo_full", MedianNS: 10, Parity: paritySummary{Clean: true}},
		{Language: "kdl", Sample: "clean2.kdl", Mode: "go_full", MedianNS: 17, Parity: paritySummary{Clean: true}},
		{Language: "kdl", Sample: "err1.kdl", Mode: "cgo_full", MedianNS: 10, Parity: paritySummary{Clean: false}},
		{Language: "kdl", Sample: "err1.kdl", Mode: "go_full", MedianNS: 1000, Parity: paritySummary{Clean: false}},
	}

	scores := scoreRows(rows)
	if len(scores) != 1 {
		t.Fatalf("scores length = %d, want 1", len(scores))
	}
	s := scores[0]

	// Combined ratio still reflects every sample: (15+17+1000)/(10+10+10) = 34.4x.
	if got, want := s.fullRatio, 1032.0/30.0; !almostEqual(got, want) {
		t.Fatalf("fullRatio = %v, want %v", got, want)
	}
	// Clean ratio isolates the two well-behaved files: (15+17)/(10+10) = 1.6x.
	if got, want := s.cleanRatio, 32.0/20.0; !almostEqual(got, want) {
		t.Fatalf("cleanRatio = %v, want %v", got, want)
	}
	// Error ratio isolates the single error-bearing file: 1000/10 = 100x.
	if got, want := s.errorRatio, 100.0; !almostEqual(got, want) {
		t.Fatalf("errorRatio = %v, want %v", got, want)
	}
	if got, want := s.cleanFileCount, 2; got != want {
		t.Fatalf("cleanFileCount = %d, want %d", got, want)
	}
	if got, want := s.errorFileCount, 1; got != want {
		t.Fatalf("errorFileCount = %d, want %d", got, want)
	}
	if got, want := s.errorFileShare, 1.0/3.0; !almostEqual(got, want) {
		t.Fatalf("errorFileShare = %v, want %v", got, want)
	}
}

func TestScoreRowsCleanRatioZeroWhenNoCleanFiles(t *testing.T) {
	rows := []reportRow{
		{Language: "kdl", Sample: "err1.kdl", Mode: "cgo_full", MedianNS: 10, Parity: paritySummary{Clean: false}},
		{Language: "kdl", Sample: "err1.kdl", Mode: "go_full", MedianNS: 1000, Parity: paritySummary{Clean: false}},
	}

	scores := scoreRows(rows)
	if len(scores) != 1 {
		t.Fatalf("scores length = %d, want 1", len(scores))
	}
	s := scores[0]
	if s.cleanRatio != 0 {
		t.Fatalf("cleanRatio = %v, want 0 (no clean samples measured)", s.cleanRatio)
	}
	if s.cleanFileCount != 0 {
		t.Fatalf("cleanFileCount = %d, want 0", s.cleanFileCount)
	}
	if got, want := s.errorFileShare, 1.0; !almostEqual(got, want) {
		t.Fatalf("errorFileShare = %v, want %v", got, want)
	}
}

func TestModeNSFromMap(t *testing.T) {
	if got := modeNSFromMap(nil, "go_full"); got != 0 {
		t.Fatalf("modeNSFromMap(nil) = %d, want 0", got)
	}
	modes := map[string]*modeAgg{"go_full": {ns: 42}}
	if got, want := modeNSFromMap(modes, "go_full"), int64(42); got != want {
		t.Fatalf("modeNSFromMap = %d, want %d", got, want)
	}
	if got := modeNSFromMap(modes, "cgo_full"); got != 0 {
		t.Fatalf("modeNSFromMap(missing mode) = %d, want 0", got)
	}
}

func TestFileShareText(t *testing.T) {
	if got, want := fileShareText(0, 0), "n/a"; got != want {
		t.Fatalf("fileShareText(0,0) = %q, want %q", got, want)
	}
	if got, want := fileShareText(0, 4), "0.0%"; got != want {
		t.Fatalf("fileShareText(0,4) = %q, want %q", got, want)
	}
	if got, want := fileShareText(1, 3), "33.3%"; got != want {
		t.Fatalf("fileShareText(1,3) = %q, want %q", got, want)
	}
}

func almostEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= epsilon
}
