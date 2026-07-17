package parsercorephase0

import (
	"crypto/sha256"
	"errors"
	"runtime"
	"testing"
	"unsafe"
)

func TestCheckpointInternerExactIdentityAndDigestCollision(t *testing.T) {
	interner := newCheckpointInterner(8, 64)
	empty, err := interner.intern(nil)
	if err != nil || empty != 0 {
		t.Fatalf("empty checkpoint=(%d,%v), want (0,nil)", empty, err)
	}

	scratch := []byte{1, 2, 3}
	first, err := interner.intern(scratch)
	if err != nil || first == 0 {
		t.Fatalf("first checkpoint=(%d,%v)", first, err)
	}
	scratch[0] = 9
	repeated, err := interner.intern([]byte{1, 2, 3})
	if err != nil || repeated != first {
		t.Fatalf("owned exact checkpoint=(%d,%v), want %d", repeated, err, first)
	}

	forced := [32]byte{7}
	left, err := interner.internDigest([]byte("left"), forced)
	if err != nil {
		t.Fatal(err)
	}
	right, err := interner.internDigest([]byte("right"), forced)
	if err != nil {
		t.Fatal(err)
	}
	if left == right || left == 0 || right == 0 {
		t.Fatalf("digest collision collapsed exact states: left=%d right=%d", left, right)
	}
	if again, _ := interner.internDigest([]byte("left"), forced); again != left {
		t.Fatalf("collision-chain lookup=%d, want %d", again, left)
	}
	emptyDigest := sha256.Sum256(nil)
	nonempty, err := interner.internDigest([]byte{1}, emptyDigest)
	if err != nil || nonempty == 0 {
		t.Fatalf("nonempty state using empty-like digest=(%d,%v)", nonempty, err)
	}
	if stats := interner.stats(); stats.Unique != 4 || stats.SerializedBytes != 13 || stats.DigestCollisions != 1 {
		t.Fatalf("checkpoint stats=%+v", stats)
	}
}

func TestEmptyCheckpointReceiptIsConstantAndAllocationFree(t *testing.T) {
	interner := newCheckpointInterner(1, 1)
	if length, digest, ok := interner.receipt(0); !ok || length != 0 || digest != emptyCheckpointDigest || digest != sha256.Sum256(nil) {
		t.Fatalf("empty receipt=(%d,%x,%t)", length, digest, ok)
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		length, digest, ok := interner.receipt(0)
		if !ok || length != 0 || digest != emptyCheckpointDigest {
			panic("empty checkpoint receipt drift")
		}
	}); allocs != 0 {
		t.Fatalf("empty checkpoint receipt allocations=%v, want 0", allocs)
	}
}

func BenchmarkCheckpointReceiptEmpty(b *testing.B) {
	interner := newCheckpointInterner(1, 1)
	for b.Loop() {
		_, _, _ = interner.receipt(0)
	}
}

func TestCheckpointInternerBoundsAreAtomic(t *testing.T) {
	countBound := newCheckpointInterner(1, 16)
	if _, err := countBound.intern([]byte{1}); err != nil {
		t.Fatal(err)
	}
	before := countBound.stats()
	if _, err := countBound.intern([]byte{2}); err == nil {
		t.Fatal("checkpoint identity cap was not enforced")
	}
	if got := countBound.stats(); got != before {
		t.Fatalf("identity-cap failure mutated stats: got=%+v want=%+v", got, before)
	}

	byteBound := newCheckpointInterner(4, 2)
	if _, err := byteBound.intern([]byte{1}); err != nil {
		t.Fatal(err)
	}
	before = byteBound.stats()
	if _, err := byteBound.intern([]byte{2, 3}); err == nil {
		t.Fatal("checkpoint byte cap was not enforced")
	}
	if got := byteBound.stats(); got != before {
		t.Fatalf("byte-cap failure mutated stats: got=%+v want=%+v", got, before)
	}
}

func TestCheckpointInternerResetAndTransactionContract(t *testing.T) {
	compact, err := New(&fakeTable{}, Limits{MaxCheckpoints: 4, MaxCheckpointBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	id := mustInternCheckpoint(t, compact, []byte{1, 2, 3})
	if err := compact.SetPhaseCheckpoint(id); err != nil {
		t.Fatal(err)
	}
	recordCap, byteCap := cap(compact.checkpoints.records), cap(compact.checkpoints.bytes)
	sentinel := errors.New("rollback")
	err = compact.ApplyAtomic(func() error {
		if _, err := compact.InternCheckpoint([]byte{4}); err == nil {
			return errors.New("transaction admitted checkpoint interning")
		}
		if err := compact.SetPhaseCheckpoint(id); err == nil {
			return errors.New("transaction admitted checkpoint selection")
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || compact.checkpoint != id || compact.CheckpointInternerStats().Unique != 1 {
		t.Fatalf("transaction contract drift: err=%v checkpoint=%d stats=%+v", err, compact.checkpoint, compact.CheckpointInternerStats())
	}
	if err := compact.Reset(); err != nil {
		t.Fatal(err)
	}
	if compact.checkpoint != 0 || compact.CheckpointInternerStats() != (CheckpointInternerStats{}) {
		t.Fatalf("reset retained checkpoint state: id=%d stats=%+v", compact.checkpoint, compact.CheckpointInternerStats())
	}
	if cap(compact.checkpoints.records) != recordCap || cap(compact.checkpoints.bytes) != byteCap {
		t.Fatalf("reset changed interner capacity: records=%d/%d bytes=%d/%d", cap(compact.checkpoints.records), recordCap, cap(compact.checkpoints.bytes), byteCap)
	}
	if err := compact.SetPhaseCheckpoint(id); err == nil {
		t.Fatal("reset-stale checkpoint identity was accepted")
	}
	if next := mustInternCheckpoint(t, compact, []byte{9}); next != 1 {
		t.Fatalf("reset checkpoint identity=%d, want reused ID 1", next)
	}
}

func TestCheckpointCompactLayoutsAMD64(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("amd64 layout receipt")
	}
	checks := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"CheckpointID", unsafe.Sizeof(CheckpointID(0)), 4},
		{"boundaryKey", unsafe.Sizeof(boundaryKey{}), 24},
		{"boundaryIdentity", unsafe.Sizeof(boundaryIdentity{}), 16},
		{"boundarySlot", unsafe.Sizeof(boundarySlot{}), 24},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s size=%d, want %d", check.name, check.got, check.want)
		}
	}
}
