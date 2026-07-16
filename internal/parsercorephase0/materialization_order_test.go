package parsercorephase0

import (
	"reflect"
	"strings"
	"testing"
)

func TestMaterializationOrderRejectsRepeatedOwnership(t *testing.T) {
	compact, err := New(&fakeTable{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := compact.appendSubtree(subtreeRecord{symbol: 1, endByte: 1, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := compact.appendSubtree(subtreeRecord{symbol: 2, endByte: 1}, []SubtreeID{leaf, leaf}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compact.MaterializationOrder([]SubtreeID{parent}, nil); err == nil || !strings.Contains(err.Error(), "repeated public-tree ownership") {
		t.Fatalf("repeated compact child error = %v", err)
	}
	visits := 0
	if err := compact.VisitMaterializationPostorder([]SubtreeID{parent}, nil, func(SubtreeID, MaterializationSubtreeView) error {
		visits++
		return nil
	}); err == nil || !strings.Contains(err.Error(), "repeated public-tree ownership") {
		t.Fatalf("fused repeated compact child error = %v", err)
	}
	if visits != 1 {
		t.Fatalf("fused repeated compact child published %d visits", visits)
	}
}

func TestMaterializationOrderRejectsCycle(t *testing.T) {
	compact, err := New(&fakeTable{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := compact.appendSubtree(subtreeRecord{symbol: 1, endByte: 1, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := compact.appendSubtree(subtreeRecord{symbol: 2, endByte: 1}, []SubtreeID{leaf}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := compact.subtree(parent)
	if err != nil {
		t.Fatal(err)
	}
	compact.children[record.firstChild] = parent
	if _, err := compact.MaterializationOrder([]SubtreeID{parent}, nil); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("compact cycle error = %v", err)
	}
	if err := compact.VisitMaterializationPostorder([]SubtreeID{parent}, nil, func(SubtreeID, MaterializationSubtreeView) error {
		return nil
	}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("fused compact cycle error = %v", err)
	}
}

func TestMaterializationOrderValidatesRemappedMetadata(t *testing.T) {
	tables := &fakeTable{
		fields: map[uint16][]FieldMapEntry{7: {{FieldID: 3, ChildIndex: 0}}},
		aliases: map[productionKey][]Symbol{
			{productionID: 7, childCount: 1}: {9},
		},
	}
	compact, err := New(tables, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	extra, err := compact.appendSubtree(subtreeRecord{symbol: 1, endByte: 1, extra: true, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := compact.appendSubtree(subtreeRecord{symbol: 2, endByte: 1, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := compact.appendSubtree(
		subtreeRecord{symbol: 3, productionID: 7, endByte: 1},
		[]SubtreeID{extra, child},
		[]FieldMapEntry{{FieldID: 3, ChildIndex: 1}},
		[]Symbol{0, 9},
	)
	if err != nil {
		t.Fatal(err)
	}
	order, err := compact.MaterializationOrder([]SubtreeID{parent}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 || order[2] != parent {
		t.Fatalf("materialization order = %v", order)
	}
	var fusedOrder []SubtreeID
	var parentView MaterializationSubtreeView
	if err := compact.VisitMaterializationPostorder([]SubtreeID{parent}, nil, func(id SubtreeID, view MaterializationSubtreeView) error {
		fusedOrder = append(fusedOrder, id)
		if id == parent {
			parentView = view
			parentView.Children = append([]SubtreeID(nil), view.Children...)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fusedOrder, order) {
		t.Fatalf("fused materialization order = %v want %v", fusedOrder, order)
	}
	if parentView.Symbol != 3 || parentView.ProductionID != 7 || parentView.StartByte != 0 || parentView.EndByte != 1 ||
		parentView.Terminal || parentView.Extra || parentView.External || !reflect.DeepEqual(parentView.Children, []SubtreeID{extra, child}) {
		t.Fatalf("fused parent view = %+v", parentView)
	}

	record, err := compact.subtree(parent)
	if err != nil {
		t.Fatal(err)
	}
	compact.fields[record.firstField].ChildIndex = 0
	if _, err := compact.MaterializationOrder([]SubtreeID{parent}, nil); err == nil || !strings.Contains(err.Error(), "metadata does not match") {
		t.Fatalf("field metadata mismatch error = %v", err)
	}
	if err := compact.VisitMaterializationPostorder([]SubtreeID{parent}, nil, func(SubtreeID, MaterializationSubtreeView) error { return nil }); err == nil || !strings.Contains(err.Error(), "metadata does not match") {
		t.Fatalf("fused field metadata mismatch error = %v", err)
	}
	compact.fields[record.firstField].ChildIndex = 1
	compact.aliases[record.firstAlias+1] = 8
	if _, err := compact.MaterializationOrder([]SubtreeID{parent}, nil); err == nil || !strings.Contains(err.Error(), "metadata does not match") {
		t.Fatalf("alias metadata mismatch error = %v", err)
	}
	if err := compact.VisitMaterializationPostorder([]SubtreeID{parent}, nil, func(SubtreeID, MaterializationSubtreeView) error { return nil }); err == nil || !strings.Contains(err.Error(), "metadata does not match") {
		t.Fatalf("fused alias metadata mismatch error = %v", err)
	}
}

func TestMaterializationOrderPollsAndPublishesNothingOnStop(t *testing.T) {
	compact, err := New(&fakeTable{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := compact.appendSubtree(subtreeRecord{symbol: 1, endByte: 1, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	polls := 0
	if order, err := compact.MaterializationOrder([]SubtreeID{leaf}, func() error {
		polls++
		return errMaterializationOrderStop
	}); err != errMaterializationOrderStop || order != nil || polls != 1 {
		t.Fatalf("stopped order=%v err=%v polls=%d", order, err, polls)
	}
}

func TestVisitMaterializationPostorderDeepIterativeTree(t *testing.T) {
	const depth = 20_000
	compact, err := New(&fakeTable{}, Limits{MaxSubtrees: depth + 1})
	if err != nil {
		t.Fatal(err)
	}
	root, err := compact.appendSubtree(subtreeRecord{symbol: 1, endByte: 1, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < depth; i++ {
		root, err = compact.appendSubtree(subtreeRecord{symbol: 2, endByte: 1}, []SubtreeID{root}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	visits := 0
	if err := compact.VisitMaterializationPostorder([]SubtreeID{root}, nil, func(_ SubtreeID, view MaterializationSubtreeView) error {
		if visits == 0 && !view.Terminal {
			t.Fatal("deep iterative traversal did not visit the leaf first")
		}
		visits++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if visits != depth+1 {
		t.Fatalf("deep iterative visits=%d want=%d", visits, depth+1)
	}
}

func TestVisitMaterializationPostorderStopsBeforeVisit(t *testing.T) {
	compact, err := New(&fakeTable{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := compact.appendSubtree(subtreeRecord{symbol: 1, endByte: 1, terminal: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	polls, visits := 0, 0
	err = compact.VisitMaterializationPostorder([]SubtreeID{leaf}, func() error {
		polls++
		return errMaterializationOrderStop
	}, func(SubtreeID, MaterializationSubtreeView) error {
		visits++
		return nil
	})
	if err != errMaterializationOrderStop || polls != 1 || visits != 0 {
		t.Fatalf("stopped err=%v polls=%d visits=%d", err, polls, visits)
	}
}

var errMaterializationOrderStop = materializationOrderTestError("stop")

type materializationOrderTestError string

func (err materializationOrderTestError) Error() string { return string(err) }
