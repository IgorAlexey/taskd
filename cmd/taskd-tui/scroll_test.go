package main

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestHalfPageScroll(t *testing.T) {
	u, _, _ := stub(t)

	// Create 40 tasks so we have rows 1 to 40.
	tasks := make([]task, 40)
	for i := 0; i < 40; i++ {
		tasks[i] = task{
			ID:      fmt.Sprintf("task%02d", i+1),
			Project: "proj-a",
			Status:  "pending",
			Body:    fmt.Sprintf("task number %d", i+1),
		}
	}
	u.render(tasks)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.table.SetRect(0, 0, 80, 25)
	u.table.Draw(sim)

	// Start at row 20.
	u.table.Select(20, 0)
	if r := u.selectedRow(); r != 20 {
		t.Fatalf("expected initial row 20, got %d", r)
	}

	// Visible height is 25, fixed header is 1 row.
	// Step is (25 - 1) / 2 = 12 rows.
	evCtrlD := tcell.NewEventKey(tcell.KeyCtrlD, 0, 0)
	evCtrlU := tcell.NewEventKey(tcell.KeyCtrlU, 0, 0)

	// Ctrl+D from row 20 -> row 32.
	ret := u.keys(evCtrlD)
	if ret != nil {
		t.Fatalf("expected keys(CtrlD) to return nil, got %v", ret)
	}
	if r := u.selectedRow(); r != 32 {
		t.Fatalf("after first Ctrl+D expected row 32, got %d", r)
	}

	// Ctrl+D from row 32 -> row 40 (clamped to len(u.shown)).
	u.keys(evCtrlD)
	if r := u.selectedRow(); r != 40 {
		t.Fatalf("after second Ctrl+D expected row 40, got %d", r)
	}

	// Ctrl+D from row 40 -> still row 40.
	u.keys(evCtrlD)
	if r := u.selectedRow(); r != 40 {
		t.Fatalf("after third Ctrl+D expected row 40, got %d", r)
	}

	// Ctrl+U from row 40 -> row 28.
	ret = u.keys(evCtrlU)
	if ret != nil {
		t.Fatalf("expected keys(CtrlU) to return nil, got %v", ret)
	}
	if r := u.selectedRow(); r != 28 {
		t.Fatalf("after first Ctrl+U expected row 28, got %d", r)
	}

	// Ctrl+U from row 28 -> row 16.
	u.keys(evCtrlU)
	if r := u.selectedRow(); r != 16 {
		t.Fatalf("after second Ctrl+U expected row 16, got %d", r)
	}

	// Ctrl+U from row 16 -> row 4.
	u.keys(evCtrlU)
	if r := u.selectedRow(); r != 4 {
		t.Fatalf("after third Ctrl+U expected row 4, got %d", r)
	}

	// Ctrl+U from row 4 -> row 1 (clamped to top selectable row).
	u.keys(evCtrlU)
	if r := u.selectedRow(); r != 1 {
		t.Fatalf("after fourth Ctrl+U expected row 1, got %d", r)
	}

	// Ctrl+U from row 1 -> still row 1.
	u.keys(evCtrlU)
	if r := u.selectedRow(); r != 1 {
		t.Fatalf("after fifth Ctrl+U expected row 1, got %d", r)
	}

	// Empty list should not panic and return nil.
	u.shown = nil
	if ret := u.keys(evCtrlD); ret != nil {
		t.Fatalf("expected nil for Ctrl+D on empty shown, got %v", ret)
	}
	if ret := u.keys(evCtrlU); ret != nil {
		t.Fatalf("expected nil for Ctrl+U on empty shown, got %v", ret)
	}

	// Re-render and test fallback step with zero/small visible height.
	u.render(tasks)
	u.table.SetRect(0, 0, 80, 1) // h=1 => (1-1)/2 = 0, step clamped to 1
	u.table.Select(5, 0)
	u.keys(evCtrlD)
	if r := u.selectedRow(); r != 6 {
		t.Fatalf("small height: expected row 6, got %d", r)
	}
	u.keys(evCtrlU)
	if r := u.selectedRow(); r != 5 {
		t.Fatalf("small height: expected row 5, got %d", r)
	}
}
