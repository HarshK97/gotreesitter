//go:build gts_workcount

package gotreesitter

import (
	"math"
	"testing"
)

func TestDiagnosticWorkCountSaturatesAndFlagsOverflow(t *testing.T) {
	BeginDiagnosticWorkCount()
	activeDiagnosticWorkCount.Shifts = math.MaxUint64
	workCountRecordShift()
	counts := EndDiagnosticWorkCount()
	if !counts.Overflow {
		t.Fatal("overflow flag is false")
	}
	if counts.Shifts != math.MaxUint64 {
		t.Fatalf("shifts=%d want saturation at %d", counts.Shifts, uint64(math.MaxUint64))
	}
}

func TestDiagnosticWorkCountLinearReductionPop(t *testing.T) {
	leaf := newLeafNodeInArena(nil, 1, true, 0, 1, Point{}, Point{Column: 1})
	extra := newLeafNodeInArena(nil, 2, false, 1, 2, Point{Column: 1}, Point{Column: 2})
	extra.setExtra(true)
	stack := newGLRStack(1)
	stack.entries = append(stack.entries,
		newStackEntryNode(2, leaf),
		newStackEntryNode(2, extra),
	)

	BeginDiagnosticWorkCount()
	workCountObserveReductionPop(&stack, 1)
	counts := EndDiagnosticWorkCount()
	if counts.EmittedPopPaths != 1 || counts.EmittedPopPayloads != 2 {
		t.Fatalf("paths=%d payloads=%d want 1/2", counts.EmittedPopPaths, counts.EmittedPopPayloads)
	}
}

func TestDiagnosticWorkCountBoardDirectEvents(t *testing.T) {
	BeginDiagnosticWorkCount()
	workCountRecordResolvedActionCell(4)
	workCountRecordResolvedActionCell(1)
	workCountRecordAlternatePredecessorLinkAppended()
	counts := EndDiagnosticWorkCount().BoardDirect()
	if counts.Schema != "gts-work-count-board-direct/v1" || counts.Overflow || counts.ResolvedActionCellsExamined != 2 || counts.RawActionEntriesBeyondFirst != 3 || counts.AlternatePredecessorLinksAppended != 1 {
		t.Fatalf("board direct counts=%+v", counts)
	}
}

func TestDiagnosticWorkCountSelectedCensusSaturates(t *testing.T) {
	counts := DiagnosticWorkCount{
		DiagnosticWorkCountValues: DiagnosticWorkCountValues{SelectedNodes: math.MaxUint64},
	}
	counts.AddDiagnosticSelectedNode(false)
	if !counts.Overflow || counts.SelectedNodes != math.MaxUint64 || counts.SelectedLeafNodes != 1 {
		t.Fatalf("selected census saturation=%+v", counts)
	}
}
