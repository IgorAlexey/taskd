package main

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestRefreshKeepsScrollPosition(t *testing.T) {
	u, _, _ := stub(t)
	tasks := make([]task, 50)
	for i := range 50 {
		tasks[i] = task{
			ID:       fmt.Sprintf("%07d", i+1),
			Project:  "verify",
			Status:   "pending",
			Priority: 0,
			Body:     fmt.Sprintf("task number %d", i+1),
		}
	}
	u.render(tasks)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(120, 20)
	u.table.SetRect(0, 0, 120, 20)
	u.table.Draw(sim)

	u.table.SetOffset(10, 0)
	u.render(tasks)
	u.table.Draw(sim)

	rowOff, colOff := u.table.GetOffset()
	if rowOff != 10 || colOff != 0 {
		t.Fatalf("expected offset (10, 0), got (%d, %d)", rowOff, colOff)
	}
	sel, ok := u.selected()
	if !ok || sel.ID != "0000001" {
		t.Fatalf("expected selected task 0000001, got %q", sel.ID)
	}
}

func TestListShrinkClampsOffset(t *testing.T) {
	u, _, _ := stub(t)
	tasks := make([]task, 200)
	for i := range 200 {
		tasks[i] = task{
			ID:       fmt.Sprintf("%07d", i+1),
			Project:  "verify",
			Status:   "pending",
			Priority: 0,
			Body:     fmt.Sprintf("task number %d", i+1),
		}
	}
	u.render(tasks)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(120, 20)
	u.table.SetRect(0, 0, 120, 20)
	u.table.Draw(sim)

	u.table.SetOffset(100, 0)

	shrunk := tasks[:20]
	u.render(shrunk)
	u.table.Draw(sim)

	rowOff, _ := u.table.GetOffset()
	if rowOff > 1 {
		t.Fatalf("expected clamped offset <= 1, got %d", rowOff)
	}
}

func TestSelectionStaysVisible(t *testing.T) {
	u, _, _ := stub(t)
	tasks := make([]task, 200)
	for i := range 200 {
		tasks[i] = task{
			ID:       fmt.Sprintf("%07d", i+1),
			Project:  "verify",
			Status:   "pending",
			Priority: 0,
			Body:     fmt.Sprintf("task number %d", i+1),
		}
	}

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(120, 40)
	u.table.SetRect(0, 0, 120, 22)

	u.table.Draw(sim)

	u.render(tasks)
	u.table.Draw(sim)

	rowOff, _ := u.table.GetOffset()
	selRow := u.selectedRow()
	if rowOff != 0 {
		t.Fatalf("selected row %d is off screen: rowOffset=%d (trackEnd latched)", selRow, rowOff)
	}
}
