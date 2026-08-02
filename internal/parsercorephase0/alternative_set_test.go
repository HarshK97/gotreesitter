package parsercorephase0

import (
	"errors"
	"slices"
	"testing"
)

func alternativeSetMembersForTest(t *testing.T, compact *Core, set AlternativeSet) []uint32 {
	t.Helper()
	members, ok := compact.alternativeSetMembers(set)
	if !ok {
		t.Fatalf("alternativeSetMembers: unresolvable spill reference for %+v", set)
	}
	out := make([]uint32, len(members))
	copy(out, members)
	return out
}

func TestNewAlternativeSetMember(t *testing.T) {
	if got := NewAlternativeSetMember(0, 0); got != (AlternativeSet{}) {
		t.Fatalf("NewAlternativeSetMember(0, 0) = %+v, want empty set", got)
	}
	// The recording gate defaults to off (GTS_B4B_SHADOW_CENSUS unset): a
	// nonzero event still returns the empty set until recording is enabled.
	if got := NewAlternativeSetMember(7, 0); got != (AlternativeSet{}) {
		t.Fatalf("NewAlternativeSetMember(7, 0) with recording disabled = %+v, want empty set", got)
	}
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := &Core{}
	set := NewAlternativeSetMember(7, 0)
	if got := alternativeSetMembersForTest(t, compact, set); !slices.Equal(got, []uint32{packAlternativeSetMember(7, 0)}) {
		t.Fatalf("members = %v, want [pack(7,0)]", got)
	}
	if set.Len() != 1 || set.Overflowed() {
		t.Fatalf("singleton set = %+v, want len 1 not overflowed", set)
	}
	// The branch half is packed into the low 16 bits: two members sharing an
	// event but differing only in branch are distinct (spec.b4b-alternative-
	// set.v2 section 3.1).
	branched := NewAlternativeSetMember(7, 1)
	if branched == set {
		t.Fatalf("NewAlternativeSetMember(7,0) and NewAlternativeSetMember(7,1) packed identically: %+v", branched)
	}
	if AlternativeSetMemberEvent(branched.inline[0]) != 7 || AlternativeSetMemberBranch(branched.inline[0]) != 1 {
		t.Fatalf("branched member unpacks to event=%d branch=%d, want 7/1",
			AlternativeSetMemberEvent(branched.inline[0]), AlternativeSetMemberBranch(branched.inline[0]))
	}
}

func TestAlternativeSetInsertAscendingStaysInline(t *testing.T) {
	compact := &Core{}
	var set AlternativeSet
	for _, member := range []uint32{2, 5, 9, 20} {
		if !compact.alternativeSetInsert(&set, member) {
			t.Fatalf("insert(%d) reported no change", member)
		}
	}
	if set.spilled() {
		t.Fatalf("four ascending inserts spilled: %+v", set)
	}
	if got := alternativeSetMembersForTest(t, compact, set); !slices.Equal(got, []uint32{2, 5, 9, 20}) {
		t.Fatalf("members = %v, want [2 5 9 20]", got)
	}
	// Duplicate and zero are no-ops.
	if compact.alternativeSetInsert(&set, 9) {
		t.Fatal("duplicate insert reported a change")
	}
	if compact.alternativeSetInsert(&set, 0) {
		t.Fatal("zero-member insert reported a change")
	}
	if got := alternativeSetMembersForTest(t, compact, set); !slices.Equal(got, []uint32{2, 5, 9, 20}) {
		t.Fatalf("members after no-ops = %v, want [2 5 9 20]", got)
	}
}

func TestAlternativeSetFifthMemberSpills(t *testing.T) {
	compact := &Core{}
	var set AlternativeSet
	for _, member := range []uint32{1, 2, 3, 4} {
		compact.alternativeSetInsert(&set, member)
	}
	if set.spilled() {
		t.Fatal("four members already spilled")
	}
	if !compact.alternativeSetInsert(&set, 5) {
		t.Fatal("fifth insert reported no change")
	}
	if !set.spilled() {
		t.Fatal("fifth member did not spill")
	}
	if got := alternativeSetMembersForTest(t, compact, set); !slices.Equal(got, []uint32{1, 2, 3, 4, 5}) {
		t.Fatalf("members after spill = %v, want [1 2 3 4 5]", got)
	}
	if len(compact.alternativeSpillArena) != 5 {
		t.Fatalf("spill arena length = %d, want 5", len(compact.alternativeSpillArena))
	}
}

// TestAlternativeSetAscendingGrowthExtendsSpillSegmentInPlace pins the O(1)
// amortized fast path: once spilled, a further ascending (tail) insert that
// is still this set's live arena tail extends by exactly one element instead
// of copying the whole segment to a fresh one. Without this, a set that
// accumulates many members in ascending order (the common case: a header or
// node's own establishment/union history is temporally, hence ascending,
// ordered -- spec.b4b-alternative-set.v1 section 3.1) would cost O(n^2)
// copying across its growth instead of O(n).
func TestAlternativeSetAscendingGrowthExtendsSpillSegmentInPlace(t *testing.T) {
	compact := &Core{}
	var set AlternativeSet
	for member := uint32(1); member <= 5; member++ {
		compact.alternativeSetInsert(&set, member)
	}
	if !set.spilled() {
		t.Fatal("five ascending inserts did not spill")
	}
	spillRefAfterFifth := set.spillRef
	arenaLenAfterFifth := len(compact.alternativeSpillArena)
	for member := uint32(6); member <= 32; member++ {
		before := len(compact.alternativeSpillArena)
		if !compact.alternativeSetInsert(&set, member) {
			t.Fatalf("insert(%d) reported no change", member)
		}
		if set.spillRef != spillRefAfterFifth {
			t.Fatalf("insert(%d) moved to a fresh segment (spillRef %d != %d): the in-place extend path did not fire",
				member, set.spillRef, spillRefAfterFifth)
		}
		if got := len(compact.alternativeSpillArena) - before; got != 1 {
			t.Fatalf("insert(%d) grew the arena by %d elements, want 1 (in-place extend)", member, got)
		}
	}
	if got := len(compact.alternativeSpillArena) - arenaLenAfterFifth; got != 27 {
		t.Fatalf("arena grew by %d elements across members 6..32, want 27 (one per insert, no re-copy)", got)
	}
	want := make([]uint32, 32)
	for i := range want {
		want[i] = uint32(i + 1)
	}
	if got := alternativeSetMembersForTest(t, compact, set); !slices.Equal(got, want) {
		t.Fatalf("members = %v, want 1..32", got)
	}
}

// TestAlternativeSetOrphanedSegmentFallsBackToFreshCopy verifies that once a
// set's spill segment is no longer the arena's live tail (another set grew
// after it), a further insert correctly falls back to a fresh-segment copy
// rather than corrupting the arena.
func TestAlternativeSetOrphanedSegmentFallsBackToFreshCopy(t *testing.T) {
	compact := &Core{}
	var first AlternativeSet
	for _, member := range []uint32{1, 2, 3, 4, 5} {
		compact.alternativeSetInsert(&first, member)
	}
	var second AlternativeSet
	for _, member := range []uint32{100, 101, 102, 103, 104} {
		compact.alternativeSetInsert(&second, member)
	}
	// first's segment is no longer the arena tail; growing it must not
	// silently corrupt second's segment.
	if !compact.alternativeSetInsert(&first, 6) {
		t.Fatal("orphaned-segment insert reported no change")
	}
	if got := alternativeSetMembersForTest(t, compact, first); !slices.Equal(got, []uint32{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("first members = %v, want [1 2 3 4 5 6]", got)
	}
	if got := alternativeSetMembersForTest(t, compact, second); !slices.Equal(got, []uint32{100, 101, 102, 103, 104}) {
		t.Fatalf("second members = %v, want [100 101 102 103 104] (must survive first's re-spill untouched)", got)
	}
}

func TestAlternativeSetOutOfOrderInsertForcesSpillAndStaysSorted(t *testing.T) {
	compact := &Core{}
	var set AlternativeSet
	compact.alternativeSetInsert(&set, 5)
	compact.alternativeSetInsert(&set, 9)
	if set.spilled() {
		t.Fatal("two ascending inserts already spilled")
	}
	// 2 is not the new maximum: inserting it in the middle cannot stay a pure
	// inline tail append, so this must spill even though the result has only
	// three members (spec.b4b-alternative-set.v1 section 3.3's append-only
	// byte-level invariant).
	if !compact.alternativeSetInsert(&set, 2) {
		t.Fatal("out-of-order insert reported no change")
	}
	if !set.spilled() {
		t.Fatal("out-of-order insert of a non-maximum member did not spill")
	}
	if got := alternativeSetMembersForTest(t, compact, set); !slices.Equal(got, []uint32{2, 5, 9}) {
		t.Fatalf("members = %v, want [2 5 9]", got)
	}
}

func TestAlternativeSetHardCapOverflows(t *testing.T) {
	compact := &Core{}
	var set AlternativeSet
	for member := uint32(1); member <= alternativeSetHardCap; member++ {
		if !compact.alternativeSetInsert(&set, member) {
			t.Fatalf("insert(%d) reported no change", member)
		}
	}
	if set.Overflowed() {
		t.Fatal("set overflowed before exceeding the hard cap")
	}
	if set.Len() != alternativeSetHardCap {
		t.Fatalf("len = %d, want %d", set.Len(), alternativeSetHardCap)
	}
	if !compact.alternativeSetInsert(&set, alternativeSetHardCap+1) {
		t.Fatal("first over-cap insert did not report a change (the overflow flag itself)")
	}
	if !set.Overflowed() {
		t.Fatal("set did not report Overflowed after exceeding the hard cap")
	}
	if set.Len() != alternativeSetHardCap {
		t.Fatalf("len after overflow = %d, want %d (recorded members stay a subset)", set.Len(), alternativeSetHardCap)
	}
	if compact.alternativeSetInsert(&set, alternativeSetHardCap+2) {
		t.Fatal("second over-cap insert reported a change; overflow flag should already be set")
	}
}

func TestAlternativeSetUnion(t *testing.T) {
	compact := &Core{}
	var dst AlternativeSet
	compact.alternativeSetInsert(&dst, 1)
	compact.alternativeSetInsert(&dst, 2)
	var src AlternativeSet
	compact.alternativeSetInsert(&src, 2)
	compact.alternativeSetInsert(&src, 3)
	if !compact.alternativeSetUnion(&dst, src) {
		t.Fatal("union reported no change")
	}
	if got := alternativeSetMembersForTest(t, compact, dst); !slices.Equal(got, []uint32{1, 2, 3}) {
		t.Fatalf("union members = %v, want [1 2 3]", got)
	}
	// Union with a subset is a no-op.
	if compact.alternativeSetUnion(&dst, src) {
		t.Fatal("re-union of an already-contained set reported a change")
	}
	// Empty union is a no-op.
	if compact.alternativeSetUnion(&dst, AlternativeSet{}) {
		t.Fatal("union with the empty set reported a change")
	}
}

// TestAlternativeSetUnionEmptyDestinationAliasesSourceSafely pins the O(1)
// empty-destination fast path: unioning into an empty set copies src's value
// (including its spill reference, if any) directly. This aliases the two
// sets' spill storage, which is only safe if neither set's later growth can
// disturb the other's already-recorded view; this test grows both sides
// after the alias and checks each set's own membership stays exact.
func TestAlternativeSetUnionEmptyDestinationAliasesSourceSafely(t *testing.T) {
	compact := &Core{}
	var src AlternativeSet
	for _, member := range []uint32{1, 2, 3, 4, 5} {
		compact.alternativeSetInsert(&src, member)
	}
	if !src.spilled() {
		t.Fatal("setup did not spill src")
	}
	var dst AlternativeSet
	if !compact.alternativeSetUnion(&dst, src) {
		t.Fatal("union into empty destination reported no change")
	}
	if dst.spillRef != src.spillRef || dst.count != src.count {
		t.Fatalf("empty-destination union did not alias src: dst=%+v src=%+v", dst, src)
	}
	// Grow dst: must not disturb src's recorded members.
	compact.alternativeSetInsert(&dst, 6)
	if got := alternativeSetMembersForTest(t, compact, src); !slices.Equal(got, []uint32{1, 2, 3, 4, 5}) {
		t.Fatalf("src members after growing the aliased dst = %v, want [1 2 3 4 5]", got)
	}
	if got := alternativeSetMembersForTest(t, compact, dst); !slices.Equal(got, []uint32{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("dst members = %v, want [1 2 3 4 5 6]", got)
	}
	// Grow src too (now orphaned, since dst's growth moved the arena tail):
	// must not disturb dst's already-recorded members.
	compact.alternativeSetInsert(&src, 7)
	if got := alternativeSetMembersForTest(t, compact, src); !slices.Equal(got, []uint32{1, 2, 3, 4, 5, 7}) {
		t.Fatalf("src members after growing = %v, want [1 2 3 4 5 7]", got)
	}
	if got := alternativeSetMembersForTest(t, compact, dst); !slices.Equal(got, []uint32{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("dst members after src regrew = %v, want [1 2 3 4 5 6] (must survive src's re-spill untouched)", got)
	}
}

func TestAlternativeSetUnionPropagatesOverflow(t *testing.T) {
	compact := &Core{}
	var src AlternativeSet
	for member := uint32(1); member <= alternativeSetHardCap+1; member++ {
		compact.alternativeSetInsert(&src, member)
	}
	if !src.Overflowed() {
		t.Fatal("source set did not overflow in setup")
	}
	var dst AlternativeSet
	compact.alternativeSetInsert(&dst, 1000)
	if !compact.alternativeSetUnion(&dst, src) {
		t.Fatal("union with an overflowed source reported no change")
	}
	if !dst.Overflowed() {
		t.Fatal("overflow did not propagate through union")
	}
}

func TestAlternativeSetMembersRejectsUnresolvableSpill(t *testing.T) {
	compact := &Core{}
	bad := AlternativeSet{}
	// Force the spilled representation with a spill reference beyond the
	// (empty) arena: alternativeSetMembers must fail closed, not panic or
	// silently truncate.
	bad.flags |= alternativeSetFlagSpilled
	bad.spillRef = 1
	bad.count = 1
	if _, ok := compact.alternativeSetMembers(bad); ok {
		t.Fatal("alternativeSetMembers resolved an out-of-range spill reference")
	}
}

// TestAlternativeSetIncomparableCoversComparableAndIncomparablePairs pins the
// blended-mark predicate (spec.b4b-alternative-set.v2 section 3.4): a pair
// is comparable (not incomparable) exactly when one side's recorded members
// are a subset of the other's.
func TestAlternativeSetIncomparableCoversComparableAndIncomparablePairs(t *testing.T) {
	compact := &Core{}
	member := func(event, branch uint16) AlternativeSet {
		var set AlternativeSet
		compact.alternativeSetInsert(&set, packAlternativeSetMember(event, branch))
		return set
	}
	union := func(sets ...AlternativeSet) AlternativeSet {
		var out AlternativeSet
		for _, s := range sets {
			compact.alternativeSetUnion(&out, s)
		}
		return out
	}
	if compact.AlternativeSetIncomparable(AlternativeSet{}, member(1, 0)) {
		t.Fatal("the empty set is comparable (a subset) of anything")
	}
	if compact.AlternativeSetIncomparable(member(1, 0), member(1, 0)) {
		t.Fatal("a set is comparable to an identical copy of itself")
	}
	a := member(1, 0)
	b := union(member(1, 0), member(2, 3))
	if compact.AlternativeSetIncomparable(a, b) {
		t.Fatal("a subset of b must be comparable")
	}
	if compact.AlternativeSetIncomparable(b, a) {
		t.Fatal("comparability is symmetric: b contains a is still comparable")
	}
	// Neither {(1,0)} nor {(1,1)} contains the other: incomparable, even
	// though both share the same event -- this is exactly the Kotlin-class
	// shape the branch half exists to discriminate (spec section 6.1).
	if !compact.AlternativeSetIncomparable(member(1, 0), member(1, 1)) {
		t.Fatal("differing-branch same-event singleton sets must be incomparable")
	}
	// The fold-merge class (spec section 6.3): {(E1,1),(E2,3)} and
	// {(E1,4),(E2,2)} share no member at all and neither contains the other.
	left := union(member(10, 1), member(11, 3))
	right := union(member(10, 4), member(11, 2))
	if !compact.AlternativeSetIncomparable(left, right) {
		t.Fatal("disjoint multi-member sets must be incomparable")
	}
}

// TestAlternativeSetJournalRestoresExactMembershipAcrossSpill exercises the
// journal-correctness property spec.b4b-alternative-set.v1 section 3.3
// claims: restoring count/flags/spillRef alone reconstructs the exact prior
// set, even across an out-of-order union that forces a fresh spill segment
// mid-transaction.
func TestAlternativeSetJournalRestoresExactMembershipAcrossSpill(t *testing.T) {
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := newTinyCore(t, 4)
	head, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageSetOwned(owner, head, NewAlternativeSetMember(5, 0), false)
	}); err != nil {
		t.Fatal(err)
	}
	before, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	beforeSet := before.set
	if got := alternativeSetMembersForTest(t, compact, beforeSet); !slices.Equal(got, []uint32{packAlternativeSetMember(5, 0)}) {
		t.Fatalf("baseline members = %v, want [pack(5,0)]", got)
	}

	sentinel := errors.New("rollback alternative set")
	err = compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		// 2 is smaller than the recorded maximum (5): this union cannot be a
		// pure tail append and must spill.
		if err := compact.RecordHeadLineageSetOwned(owner, head, NewAlternativeSetMember(2, 0), false); err != nil {
			return err
		}
		mid, err := compact.nodeLineage(head.Node)
		if err != nil {
			return err
		}
		if !mid.set.spilled() {
			return errors.New("mid-transaction union did not spill")
		}
		if got, _ := compact.alternativeSetMembers(mid.set); !slices.Equal(got, []uint32{packAlternativeSetMember(2, 0), packAlternativeSetMember(5, 0)}) {
			return errors.New("mid-transaction members are not [2 5]")
		}
		// A second union within the same transaction, also out of order.
		if err := compact.RecordHeadLineageSetOwned(owner, head, NewAlternativeSetMember(1, 0), false); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback err = %v, want %v", err, sentinel)
	}
	after, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if after.set != beforeSet {
		t.Fatalf("rolled-back set = %+v, want %+v", after.set, beforeSet)
	}
	if got := alternativeSetMembersForTest(t, compact, after.set); !slices.Equal(got, []uint32{packAlternativeSetMember(5, 0)}) {
		t.Fatalf("rolled-back members = %v, want [pack(5,0)]", got)
	}

	// Commit the same mutation this time; membership should now include both.
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageSetOwned(owner, head, NewAlternativeSetMember(2, 0), false)
	}); err != nil {
		t.Fatal(err)
	}
	committed, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if got := alternativeSetMembersForTest(t, compact, committed.set); !slices.Equal(got, []uint32{packAlternativeSetMember(2, 0), packAlternativeSetMember(5, 0)}) {
		t.Fatalf("committed members = %v, want [pack(2,0) pack(5,0)]", got)
	}
}

// TestAlternativeSetEstablishmentInsertsMemberAlongsideScalar verifies that
// RecordReductionLineageOwned's set insert never changes the existing scalar
// merge outcome (stage 1: no behavior change) while also recording the
// (event, branch) member -- branch 0, this dispatch's only output.
func TestAlternativeSetEstablishmentInsertsMemberAlongsideScalar(t *testing.T) {
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := newTinyCore(t, 4)
	head, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []ReductionOutput{{
		Head: head, CleanPathRank: CleanPathRankUnselected, MultiplePopPaths: true,
	}}
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordReductionLineageOwned(owner, outputs, 9)
	}); err != nil {
		t.Fatal(err)
	}
	record, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if record.rank != CleanPathRankUnselected || record.lineage != 9 || !record.converged {
		t.Fatalf("scalar record = %+v, want unselected lineage 9 converged", *record)
	}
	if got := alternativeSetMembersForTest(t, compact, record.set); !slices.Equal(got, []uint32{packAlternativeSetMember(9, 0)}) {
		t.Fatalf("established members = %v, want [pack(9,0)]", got)
	}
	if record.blended {
		t.Fatal("fresh establishment must never mark blended")
	}
}

// TestAlternativeSetEstablishmentBranchOrdinalTracksOutputIndex pins the
// branch identity itself (spec.b4b-alternative-set.v2 section 3.1): each
// output's recorded branch is its index within the outputs slice handed to
// RecordReductionLineageOwned, the dispatch's stable first-boundary order.
func TestAlternativeSetEstablishmentBranchOrdinalTracksOutputIndex(t *testing.T) {
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := newTinyCore(t, 8)
	first, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compact.Seed(2, 0)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []ReductionOutput{
		{Head: first, CleanPathRank: CleanPathRankSelected, MultiplePopPaths: true},
		{Head: second, CleanPathRank: CleanPathRankUnselected, MultiplePopPaths: true},
	}
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordReductionLineageOwned(owner, outputs, 11)
	}); err != nil {
		t.Fatal(err)
	}
	firstRecord, err := compact.nodeLineage(first.Node)
	if err != nil {
		t.Fatal(err)
	}
	secondRecord, err := compact.nodeLineage(second.Node)
	if err != nil {
		t.Fatal(err)
	}
	if got := alternativeSetMembersForTest(t, compact, firstRecord.set); !slices.Equal(got, []uint32{packAlternativeSetMember(11, 0)}) {
		t.Fatalf("first output members = %v, want [pack(11,0)] (branch 0)", got)
	}
	if got := alternativeSetMembersForTest(t, compact, secondRecord.set); !slices.Equal(got, []uint32{packAlternativeSetMember(11, 1)}) {
		t.Fatalf("second output members = %v, want [pack(11,1)] (branch 1)", got)
	}
}

// TestAlternativeSetRecordingDisabledByDefaultLeavesSetEmptyButScalarIntact
// pins the always-off-by-default recording gate (GTS_B4B_SHADOW_CENSUS):
// with no override, establishment records no member at all, while the
// existing scalar rank/lineage mechanism is completely unaffected.
func TestAlternativeSetRecordingDisabledByDefaultLeavesSetEmptyButScalarIntact(t *testing.T) {
	compact := newTinyCore(t, 4)
	head, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []ReductionOutput{{
		Head: head, CleanPathRank: CleanPathRankUnselected, MultiplePopPaths: true,
	}}
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordReductionLineageOwned(owner, outputs, 9)
	}); err != nil {
		t.Fatal(err)
	}
	record, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if record.rank != CleanPathRankUnselected || record.lineage != 9 || !record.converged {
		t.Fatalf("scalar record with recording disabled = %+v, want unselected lineage 9 converged (unaffected by the gate)", *record)
	}
	if record.set.Len() != 0 {
		t.Fatalf("set with recording disabled = %+v (len %d), want empty", record.set, record.set.Len())
	}
	if len(compact.alternativeSpillArena) != 0 {
		t.Fatalf("spill arena grew with recording disabled: len=%d, want 0", len(compact.alternativeSpillArena))
	}
}

// TestAlternativeSetWarmInsertAllocations pins the warm insert/union path at
// zero allocations, mirroring
// TestDiagnosticParserCoreSelectedLineageProofAllocations
// (parsercore_phase0_selected_lineage_internal_test.go). The arena and set
// storage warm to their high-water mark on the first iteration (a spill, an
// allocation) and must not allocate again once retained capacity covers the
// steady-state shape (spec.b4b-alternative-set.v1 section 3.4).
func TestAlternativeSetWarmInsertAllocations(t *testing.T) {
	compact := &Core{}
	// Warm the arena once outside the timed loop.
	warm := func() {
		var set AlternativeSet
		compact.alternativeSetInsert(&set, 5)
		compact.alternativeSetInsert(&set, 9)
		compact.alternativeSetInsert(&set, 2)
		compact.alternativeSetInsert(&set, 20)
		compact.alternativeSetInsert(&set, 30)
	}
	warm()
	compact.alternativeSpillArena = compact.alternativeSpillArena[:0]
	if allocations := testing.AllocsPerRun(1000, func() {
		var set AlternativeSet
		compact.alternativeSetInsert(&set, 5)
		compact.alternativeSetInsert(&set, 9)
		compact.alternativeSetInsert(&set, 2)
		compact.alternativeSetInsert(&set, 20)
		compact.alternativeSetInsert(&set, 30)
		compact.alternativeSpillArena = compact.alternativeSpillArena[:0]
		if set.Len() != 5 {
			panic("warm alternative-set insert lost a member")
		}
	}); allocations != 0 {
		t.Fatalf("warm alternative-set insert allocations = %g, want 0", allocations)
	}
}

// TestAlternativeSetIncomparableWarmAllocations pins the blended-mark
// predicate itself at zero allocations once warm, mirroring
// TestAlternativeSetWarmInsertAllocations: every fold-class union site calls
// this once per union (spec.b4b-alternative-set.v2 section 3.4), so it must
// never allocate on the steady-state parse path.
func TestAlternativeSetIncomparableWarmAllocations(t *testing.T) {
	compact := &Core{}
	var a, b AlternativeSet
	compact.alternativeSetInsert(&a, 5)
	compact.alternativeSetInsert(&a, 9)
	compact.alternativeSetInsert(&b, 5)
	compact.alternativeSetInsert(&b, 20)
	if allocations := testing.AllocsPerRun(1000, func() {
		if !compact.AlternativeSetIncomparable(a, b) {
			panic("expected a and b to be incomparable")
		}
	}); allocations != 0 {
		t.Fatalf("warm AlternativeSetIncomparable allocations = %g, want 0", allocations)
	}
}

// TestRecordHeadLineageOwnedSetDirtyFalseSkipsSetButKeepsScalar pins
// RecordHeadLineageOwned's setDirty parameter: false must skip the set
// union entirely (the node's recorded set is untouched, even though the
// call passes a set argument), while the scalar rank/lineage merge still
// runs unconditionally, since a rank flip on an already-recorded lineage id
// changes the scalar pair without adding a member.
func TestRecordHeadLineageOwnedSetDirtyFalseSkipsSetButKeepsScalar(t *testing.T) {
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := newTinyCore(t, 4)
	head, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	unrelated := NewAlternativeSetMember(99, 0)
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageOwned(owner, head, CleanPathRankUnselected, 7, unrelated, false, false)
	}); err != nil {
		t.Fatal(err)
	}
	record, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if record.rank != CleanPathRankUnselected || record.lineage != 7 || !record.converged {
		t.Fatalf("scalar record with setDirty=false = %+v, want unselected lineage 7 converged", *record)
	}
	if record.set.Len() != 0 {
		t.Fatalf("set = %+v (len %d), want untouched/empty: setDirty=false must skip the union", record.set, record.set.Len())
	}
	// setDirty=true now unions the same set: scalar re-merges (rank flips to
	// Selected on the same lineage, matching applyDiagnosticParserCoreCleanPathOutput's
	// overwrite rule) and the set gains the member.
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageOwned(owner, head, CleanPathRankSelected, 7, unrelated, false, true)
	}); err != nil {
		t.Fatal(err)
	}
	record, err = compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if record.rank != CleanPathRankSelected {
		t.Fatalf("scalar rank after setDirty=true = %v, want Selected", record.rank)
	}
	if got := alternativeSetMembersForTest(t, compact, record.set); !slices.Equal(got, []uint32{packAlternativeSetMember(99, 0)}) {
		t.Fatalf("set members after setDirty=true = %v, want [pack(99,0)]", got)
	}
}

// TestRecordHeadLineageOwnedSetBlendedMarksNodeBlended pins the blended
// parameter's propagation into the node record (spec.b4b-alternative-set.v2
// section 3.4): a caller-asserted setBlended=true marks the node blended
// even when the union itself is comparable.
func TestRecordHeadLineageOwnedSetBlendedMarksNodeBlended(t *testing.T) {
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := newTinyCore(t, 4)
	head, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	set := NewAlternativeSetMember(5, 0)
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageOwned(owner, head, CleanPathRankUnselected, 5, set, true, true)
	}); err != nil {
		t.Fatal(err)
	}
	record, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if !record.blended {
		t.Fatal("setBlended=true must mark the node record blended even on a comparable (first) union")
	}
}

// TestRecordNodeLineageSetIncomparableUnionMarksBlended pins the fold-class
// incomparability computation itself (spec.b4b-alternative-set.v2 section
// 3.4): unioning two incomparable sets into one node marks it blended even
// when neither caller-side set was already blended.
func TestRecordNodeLineageSetIncomparableUnionMarksBlended(t *testing.T) {
	defer SetAlternativeSetRecordingEnabledForTest(true)()
	compact := newTinyCore(t, 4)
	head, err := compact.Seed(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageOwned(owner, head, CleanPathRankUnselected, 1, NewAlternativeSetMember(1, 0), false, true)
	}); err != nil {
		t.Fatal(err)
	}
	record, err := compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if record.blended {
		t.Fatal("the first union into an empty set is comparable and must not blend")
	}
	// pack(1,1) shares this node's event but not its branch: neither the
	// existing {(1,0)} nor the incoming {(1,1)} contains the other.
	if err := compact.ApplySchedulerAtomic(func(owner SchedulerTransactionToken) error {
		return compact.RecordHeadLineageOwned(owner, head, CleanPathRankUnselected, 1, NewAlternativeSetMember(1, 1), false, true)
	}); err != nil {
		t.Fatal(err)
	}
	record, err = compact.nodeLineage(head.Node)
	if err != nil {
		t.Fatal(err)
	}
	if !record.blended {
		t.Fatal("unioning an incomparable set must mark the node record blended")
	}
}
