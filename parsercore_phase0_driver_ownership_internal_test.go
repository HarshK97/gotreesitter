//go:build gts_parsercorephase0

package gotreesitter

import (
	"testing"
	"unsafe"

	core "github.com/odvcencio/gotreesitter/internal/parsercorephase0"
)

func TestDiagnosticParserCoreCanonicalVersionStateIdentity(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	for _, count := range []int{2, diagnosticParserCoreLinearCanonicalLimit + 1} {
		t.Run("frontier_"+string(rune('0'+count)), func(t *testing.T) {
			first := &diagnosticParserCoreVersionState{}
			second := &diagnosticParserCoreVersionState{}
			headers := make([]diagnosticParserCoreHeader, count)
			for index := range headers {
				headers[index] = diagnosticParserCoreHeader{head: head, versionState: first}
			}
			headers[count-1].versionState = second
			var scratch diagnosticParserCoreCanonicalScratch
			out, err := scratch.canonicalize(compact, headers)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != 2 {
				t.Fatalf("divergent version states merged: len=%d output=%+v", len(out), out)
			}
			seenFirst, seenSecond := false, false
			for _, header := range out {
				seenFirst = seenFirst || header.versionState == first
				seenSecond = seenSecond || header.versionState == second
			}
			if !seenFirst || !seenSecond {
				t.Fatalf("canonical output lost a version identity: %+v", out)
			}

			shared := make([]diagnosticParserCoreHeader, count)
			for index := range shared {
				shared[index] = diagnosticParserCoreHeader{head: head, versionState: first}
			}
			out, err = scratch.canonicalize(compact, shared)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != 1 || out[0].versionState != first {
				t.Fatalf("equal fork states did not merge: len=%d output=%+v", len(out), out)
			}
		})
	}
}

func TestDiagnosticParserCoreReductionSiblingAdoptionRequiresVersionStateIdentity(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	first := &diagnosticParserCoreVersionState{}
	second := &diagnosticParserCoreVersionState{}
	scheduler := &diagnosticParserCoreGenericScheduler{
		compact: compact,
		headers: []diagnosticParserCoreHeader{
			{head: head, versionState: first},
			{head: head, versionState: second, paused: true},
		},
	}
	adopted, err := scheduler.adoptUpdatedReductionSibling(
		0, head, core.CleanPathRankUnknown, 0, core.AlternativeSet{}, false, false, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if adopted {
		t.Fatal("sibling adoption merged divergent version states")
	}
	if scheduler.headers[1].versionState != second || !scheduler.headers[1].paused {
		t.Fatalf("divergent sibling changed: %+v", scheduler.headers[1])
	}

	scheduler.headers[1].versionState = first
	adopted, err = scheduler.adoptUpdatedReductionSibling(
		0, head, core.CleanPathRankUnknown, 0, core.AlternativeSet{}, false, false, false,
	)
	if err != nil || !adopted {
		t.Fatalf("equal version states were not adopted: adopted=%t err=%v", adopted, err)
	}
}

func TestDiagnosticParserCoreReductionUnchangedDistinctStateIsRetained(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	first := &diagnosticParserCoreVersionState{}
	second := &diagnosticParserCoreVersionState{}
	scheduler := &diagnosticParserCoreGenericScheduler{
		compact: compact,
		headers: []diagnosticParserCoreHeader{
			{head: head, versionState: first},
			{head: head, versionState: second},
		},
	}
	kept, adopted, err := scheduler.reconcileGenericConflictOutputs(0, []diagnosticParserCoreHeader{
		{head: head, versionState: second, freshness: core.ReductionUnchanged},
	})
	if err != nil {
		t.Fatal(err)
	}
	if adopted != 0 || len(kept) != 1 || kept[0].versionState != second || kept[0].freshness != core.ReductionUnchanged {
		t.Fatalf("distinct unchanged output was dropped: kept=%+v adopted=%d", kept, adopted)
	}
}

func TestDiagnosticParserCoreGenericReductionUnchangedDistinctStateIsRetained(t *testing.T) {
	table := &genericConflictTable{
		cells: map[genericConflictCell][]core.Action{
			{state: 1, symbol: 8}: {{Type: core.ActionShift, State: 3}},
			{state: 3, symbol: 9}: {{Type: core.ActionReduce, Symbol: 2, ChildCount: 1}},
		},
		gotos: map[genericConflictCell]core.StateID{{state: 1, symbol: 2}: 4},
	}
	compact, source := newGenericFreshnessSource(t, table)
	initial, err := compact.ReduceOutputs(source, 9, 0, core.ForkOrder{})
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial reduction outputs=%+v err=%v", initial, err)
	}
	first := &diagnosticParserCoreVersionState{}
	second := &diagnosticParserCoreVersionState{}
	scheduler := &diagnosticParserCoreGenericScheduler{
		compact: compact,
		headers: []diagnosticParserCoreHeader{
			{head: source, versionState: first},
			{head: initial[0].Head, versionState: second},
		},
		token: Token{Symbol: 9, StartByte: 1, EndByte: 2},
		options: DiagnosticParserCorePrefixOptions{
			MaxDispatches: 20,
			ReceiptMode:   DiagnosticParserCoreReceiptSummary,
		},
		receipt: &DiagnosticParserCoreGenericScheduler{},
	}
	before, err := diagnosticParserCoreHeaderReceipts(compact, scheduler.headers)
	if err != nil {
		t.Fatal(err)
	}
	cell := mustDiagnosticParserCoreGenericCell(t, compact, 0, scheduler.headers[0], 9)
	if err := scheduler.applyGenericReduction(before, cell); err != nil {
		t.Fatal(err)
	}
	if len(scheduler.headers) != 2 {
		t.Fatalf("distinct unchanged reduction output was dropped: headers=%+v", scheduler.headers)
	}
	seenFirst, seenSecond := false, false
	for _, header := range scheduler.headers {
		seenFirst = seenFirst || header.versionState == first
		seenSecond = seenSecond || header.versionState == second
	}
	if !seenFirst || !seenSecond {
		t.Fatalf("reduction retained the wrong version identities: %+v", scheduler.headers)
	}
}

func TestDiagnosticParserCoreCanonicalClearsDiscardedVersionStateTail(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	state := &diagnosticParserCoreVersionState{}
	input := make([]diagnosticParserCoreHeader, 3, 4)
	for index := range input {
		input[index] = diagnosticParserCoreHeader{head: head, versionState: state}
	}
	var scratch diagnosticParserCoreCanonicalScratch
	out, err := scratch.canonicalize(compact, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || cap(out) <= len(out) {
		t.Fatalf("canonical output len/cap=%d/%d, want a retained tail", len(out), cap(out))
	}
	for index, header := range out[:cap(out)][len(out):] {
		if header.versionState != nil {
			t.Fatalf("canonical discarded slot %d retained version state", index+len(out))
		}
	}
}

func TestDiagnosticParserCoreRollbackClearsDiscardedVersionStateTail(t *testing.T) {
	keep := &diagnosticParserCoreVersionState{}
	discarded := &diagnosticParserCoreVersionState{}
	headers := make([]diagnosticParserCoreHeader, 1, 4)
	headers[0].versionState = keep
	var scratch diagnosticParserCoreHeaderRollbackScratch
	if err := scratch.begin(headers); err != nil {
		t.Fatal(err)
	}
	headers = append(headers, diagnosticParserCoreHeader{versionState: discarded})
	scratch.finish(&headers, true)
	if len(headers) != 1 || headers[0].versionState != keep {
		t.Fatalf("rollback did not restore the original header: %+v", headers)
	}
	if headers[:cap(headers)][1].versionState != nil {
		t.Fatal("rollback retained a discarded header state beyond len")
	}
	if scratch.inline[0].versionState != nil {
		t.Fatal("rollback scratch retained its snapshot state")
	}
}

func TestDiagnosticParserCoreConflictScratchClearsDiscardedVersionStateTail(t *testing.T) {
	state := &diagnosticParserCoreVersionState{}
	scratch := diagnosticParserCoreConflictScratch{
		outputs:        make([]diagnosticParserCoreHeader, 3, 5),
		headerAssembly: make([]diagnosticParserCoreHeader, 3, 5),
	}
	for index := range scratch.outputs {
		scratch.outputs[index].versionState = state
		scratch.headerAssembly[index].versionState = state
	}
	if err := scratch.begin(1); err != nil {
		t.Fatal(err)
	}
	for name, headers := range map[string][]diagnosticParserCoreHeader{
		"outputs":  scratch.outputs,
		"assembly": scratch.headerAssembly,
	} {
		if len(headers) != 0 || cap(headers) <= len(headers) {
			t.Fatalf("%s len/cap=%d/%d, want an empty retained buffer", name, len(headers), cap(headers))
		}
		for index, header := range headers[:cap(headers)] {
			if header.versionState != nil {
				t.Fatalf("%s discarded slot %d retained version state", name, index)
			}
		}
	}
	scratch.finish()
}

func TestDiagnosticParserCoreReductionReplacementResetClearsStaleTail(t *testing.T) {
	state := &diagnosticParserCoreVersionState{}
	replacements := make([]diagnosticParserCoreHeader, 1, 4)
	replacements[0].versionState = state
	replacements[:cap(replacements)][1].versionState = state
	scheduler := diagnosticParserCoreGenericScheduler{reductionReplacements: replacements}
	if err := resetDiagnosticParserCoreGenericScheduler(&scheduler); err != nil {
		t.Fatal(err)
	}
	if replacements[:cap(replacements)][0].versionState != nil || replacements[:cap(replacements)][1].versionState != nil {
		t.Fatal("reduction replacement reset retained version state")
	}
}

func TestDiagnosticParserCoreNoActionClearsDiscardedVersionStateTail(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	keep := &diagnosticParserCoreVersionState{}
	discarded := &diagnosticParserCoreVersionState{}
	headers := make([]diagnosticParserCoreHeader, 2, 4)
	headers[0] = diagnosticParserCoreHeader{head: head, shifted: true, versionState: keep}
	headers[1] = diagnosticParserCoreHeader{head: head, paused: true, versionState: discarded}
	scheduler := diagnosticParserCoreGenericScheduler{
		compact:           compact,
		headers:           headers,
		epochProgress:     true,
		verifierBound:     len(headers),
		verifierHeaderPtr: &headers[0],
		options: DiagnosticParserCorePrefixOptions{
			ReceiptMode:                     DiagnosticParserCoreReceiptSummary,
			allowConvergedSplitDropArtifact: true,
		},
	}
	if err := scheduler.dropGenericNoActionHeads([]int{1}); err != nil {
		t.Fatal(err)
	}
	if len(scheduler.headers) != 1 || scheduler.headers[0].versionState != keep {
		t.Fatalf("no-action drop changed the survivor: %+v", scheduler.headers)
	}
	if headers[:cap(headers)][1].versionState != nil {
		t.Fatal("no-action drop retained the discarded header state")
	}
	if scheduler.verifierHeaderPtr != nil || scheduler.verifierBound != 0 {
		t.Fatal("no-action drop retained a stale verifier binding")
	}
}

func TestDiagnosticParserCoreRecoveryCollapseClearsDiscardedVersionStateTail(t *testing.T) {
	keep := &diagnosticParserCoreVersionState{}
	discarded := &diagnosticParserCoreVersionState{}
	active := make([]diagnosticParserCoreHeader, 2, 4)
	active[0].versionState = keep
	active[1].versionState = discarded
	canonical := make([]diagnosticParserCoreHeader, 2, 4)
	canonical[0].versionState = keep
	canonical[1].versionState = discarded
	scheduler := diagnosticParserCoreGenericScheduler{
		headers: active,
		canonicalScratch: diagnosticParserCoreCanonicalScratch{
			headerBuffers: [2][]diagnosticParserCoreHeader{canonical, canonical},
		},
	}
	scheduler.collapseToRecoveryWinner(0)
	if len(scheduler.headers) != 1 || scheduler.headers[0].versionState != keep {
		t.Fatalf("recovery collapse changed the winner: %+v", scheduler.headers)
	}
	if active[:cap(active)][1].versionState != nil {
		t.Fatal("recovery collapse retained a discarded active state")
	}
	for index, buffer := range scheduler.canonicalScratch.headerBuffers {
		if buffer[:cap(buffer)][0].versionState != nil || buffer[:cap(buffer)][1].versionState != nil {
			t.Fatalf("recovery collapse retained canonical buffer %d state", index)
		}
	}
}

func TestDiagnosticParserCoreReplacementClearsDiscardedVersionStateTail(t *testing.T) {
	state := &diagnosticParserCoreVersionState{}
	headers := make([]diagnosticParserCoreHeader, 3, 5)
	for index := range headers {
		headers[index].versionState = state
	}
	replacements := []diagnosticParserCoreHeader{}
	got := replaceDiagnosticParserCoreHeader(headers, 1, replacements)
	if len(got) != 2 || cap(got) != cap(headers) {
		t.Fatalf("replacement unexpectedly changed storage: len/cap=%d/%d", len(got), cap(got))
	}
	if got[:cap(got)][2].versionState != nil {
		t.Fatal("replacement retained its discarded tail state")
	}
}

func TestDiagnosticParserCoreSchedulerFootprintCountsWideSharedVersionStates(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	const totalHeaders = 300
	const uniqueStates = 257
	states := make([]*diagnosticParserCoreVersionState, uniqueStates)
	for index := range states {
		states[index] = &diagnosticParserCoreVersionState{}
	}
	headers := make([]diagnosticParserCoreHeader, totalHeaders)
	for index := range headers {
		stateIndex := index
		if stateIndex >= uniqueStates {
			stateIndex = uniqueStates - 1
		}
		headers[index] = diagnosticParserCoreHeader{head: head, versionState: states[stateIndex]}
	}
	base := diagnosticParserCoreSchedulerFootprintBytes(&diagnosticParserCoreGenericScheduler{compact: compact})
	scheduler := &diagnosticParserCoreGenericScheduler{
		compact: compact,
		headers: headers,
	}
	got := diagnosticParserCoreSchedulerFootprintBytes(scheduler) - base
	want := uint64(totalHeaders)*uint64(unsafe.Sizeof(diagnosticParserCoreHeader{})) +
		uint64(uniqueStates)*uint64(unsafe.Sizeof(diagnosticParserCoreVersionState{})) +
		uint64(cap(scheduler.footprintRefs))*uint64(unsafe.Sizeof(diagnosticParserCoreFootprintRef{}))
	if got != want {
		t.Fatalf("wide shared-state footprint=%d, want exact %d", got, want)
	}
	var gotAgain uint64
	if allocs := testing.AllocsPerRun(100, func() {
		gotAgain = diagnosticParserCoreSchedulerFootprintBytes(scheduler)
	}); allocs != 0 || gotAgain != diagnosticParserCoreSchedulerFootprintBytes(scheduler) {
		t.Fatalf("wide footprint steady state allocs=%v got=%d", allocs, gotAgain)
	}
}

func TestDiagnosticParserCoreCanonicalKeyAndGroupStatesAreAccounted(t *testing.T) {
	compact, head, _ := newDiagnosticParserCoreCanonicalTestCore(t)
	keyState := &diagnosticParserCoreVersionState{}
	groupState := &diagnosticParserCoreVersionState{}
	key := diagnosticParserCorePhaseHead{head: head, versionState: keyState}
	groupKey := diagnosticParserCorePhaseHead{head: head, versionState: groupState}
	scheduler := &diagnosticParserCoreGenericScheduler{
		compact: compact,
		canonicalScratch: diagnosticParserCoreCanonicalScratch{
			keys:   []diagnosticParserCorePhaseHead{key},
			groups: map[diagnosticParserCorePhaseHead]diagnosticParserCoreCanonicalGroup{groupKey: {}},
		},
	}
	base := diagnosticParserCoreSchedulerFootprintBytes(&diagnosticParserCoreGenericScheduler{compact: compact})
	got := diagnosticParserCoreSchedulerFootprintBytes(scheduler) - base
	minimum := uint64(2) * uint64(unsafe.Sizeof(diagnosticParserCoreVersionState{}))
	if got < minimum {
		t.Fatalf("canonical key/group states were not accounted: delta=%d minimum=%d", got, minimum)
	}
	if len(scheduler.footprintRefs) != 0 {
		t.Fatalf("footprint refs retained logical entries: len=%d", len(scheduler.footprintRefs))
	}
}
