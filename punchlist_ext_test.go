package gotreesitter_test

import (
	"testing"
	"time"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestSkipToByteOutOfRangeNoHang: an included range past EOF must not spin the
// DFA token source forever (audit DoS finding).
func TestSkipToByteOutOfRangeNoHang(t *testing.T) {
	src := []byte("package main\n")
	p := gts.NewParser(grammars.GoLanguage())
	p.SetIncludedRanges([]gts.Range{{StartByte: 100, EndByte: 200,
		StartPoint: gts.Point{Row: 9, Column: 0}, EndPoint: gts.Point{Row: 18, Column: 0}}})
	done := make(chan struct{})
	go func() { _, _ = p.Parse(src); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Parse hung on an out-of-range included range (SkipToByte spin)")
	}
}

// TestPointRangeMatchesByteRangeHalfOpen: SetPointRange and SetByteRange must
// agree — a node touching the range only at a boundary is excluded by both.
func TestPointRangeMatchesByteRangeHalfOpen(t *testing.T) {
	lang := grammars.GoLanguage()
	src := []byte("package main\n")
	tree, err := gts.NewParser(lang).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	q, err := gts.NewQuery(`(package_identifier) @id`, lang)
	if err != nil {
		t.Fatal(err)
	}
	// package_identifier "main" spans bytes 8-12, points (0,8)-(0,12).
	// A range ending exactly at byte 8 / point (0,8) touches it at the boundary
	// only -> zero overlap -> excluded under BOTH restrictions.
	byteCur := q.Exec(tree.RootNode(), lang, src)
	byteCur.SetByteRange(0, 8)
	_, byteHit := byteCur.NextMatch()
	pointCur := q.Exec(tree.RootNode(), lang, src)
	pointCur.SetPointRange(gts.Point{Row: 0, Column: 0}, gts.Point{Row: 0, Column: 8})
	_, pointHit := pointCur.NextMatch()
	if byteHit != pointHit {
		t.Fatalf("byte-range hit=%v but point-range hit=%v for the same boundary span", byteHit, pointHit)
	}
	if pointHit {
		t.Fatal("boundary-touching node should be excluded (half-open), but point-range matched it")
	}
}
