package gotreesitter

import (
	"bytes"
	"testing"
)

func TestStructuralDensityNodeCapSmallSourceUnbounded(t *testing.T) {
	src := bytes.Repeat([]byte("x := f(a, b)\n"), 1000) // ~13KB, under 1MiB
	if _, ok := parseFullArenaStructuralDensityNodeCap(src); ok {
		t.Fatal("density cap engaged below the 1MiB threshold")
	}
}

func TestStructuralDensityNodeCapSparseLiteralClamps(t *testing.T) {
	// Model bug257-style sources: a giant literal body with a tiny code frame.
	var b bytes.Buffer
	b.WriteString("package main\n\nconst data = `")
	b.Write(bytes.Repeat([]byte("☺"), 1<<21)) // ~6MB of smileys, no structural bytes
	b.WriteString("`\n")
	src := b.Bytes()

	capNodes, ok := parseFullArenaStructuralDensityNodeCap(src)
	if !ok {
		t.Fatal("density cap did not engage on a large source")
	}
	byteEstimate := parseFullArenaInitialNodeCapacity(len(src))
	if capNodes >= byteEstimate/10 {
		t.Fatalf("sparse literal cap = %d, want well under byte estimate %d", capNodes, byteEstimate)
	}
	got := parseFullArenaNodeCapacityForSource(src, nil, 0)
	if got > max(capNodes, nodeCapacityForClass(arenaClassFull)) {
		t.Fatalf("capacity for sparse source = %d, want <= max(cap %d, base %d)", got, capNodes, nodeCapacityForClass(arenaClassFull))
	}
}

func TestStructuralDensityNodeCapDenseCodeUnclamped(t *testing.T) {
	// Model opGen-style dense generated code; the cap must sit above the
	// byte-length estimate so dense sources keep their preallocation.
	line := []byte("\tcase OpAMD64ADDQ:\n\t\tv.Op = OpAMD64ADDL\n\t\treturn true\n")
	src := bytes.Repeat(line, 1<<16) // ~3.3MB dense code
	capNodes, ok := parseFullArenaStructuralDensityNodeCap(src)
	if !ok {
		t.Fatal("density cap did not engage on a large source")
	}
	byteEstimate := parseFullArenaInitialNodeCapacity(len(src))
	if capNodes < byteEstimate {
		t.Fatalf("dense code cap = %d clamps below byte estimate %d; dense sources must keep their preallocation", capNodes, byteEstimate)
	}
	if got := parseFullArenaNodeCapacityForSource(src, nil, 0); got != byteEstimate {
		t.Fatalf("capacity for dense source = %d, want unchanged byte estimate %d", got, byteEstimate)
	}
}

func TestStructuralDensityNodeCapPreservesLearnedHint(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("package main\n\nconst data = `")
	b.Write(bytes.Repeat([]byte("☺"), 1<<21))
	b.WriteString("`\n")
	src := b.Bytes()

	hint := 500_000 // a previous parse on this parser really used this many nodes
	got := parseFullArenaNodeCapacityForSource(src, nil, hint)
	if got < hint {
		t.Fatalf("capacity with learned hint = %d, want at least the hint %d", got, hint)
	}
}
