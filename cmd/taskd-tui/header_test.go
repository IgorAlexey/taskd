package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTableHeaderClickNoSelect(t *testing.T) {
	u := newUI("http://127.0.0.1:1", "", false)
	u.render([]task{
		{ID: "t1", Status: "pending", Body: "hello"},
	})

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.app.SetScreen(sim)
	u.app.SetRoot(u.pages, true)
	u.table.Draw(sim)

	if selRow, _ := u.table.GetSelection(); selRow != 1 {
		t.Fatalf("initial selection = %d, want 1", selRow)
	}

	ev := tcell.NewEventMouse(5, 0, tcell.ButtonPrimary, 0)
	u.table.MouseHandler()(tview.MouseLeftClick, ev, func(p tview.Primitive) {})

	if selRow, _ := u.table.GetSelection(); selRow != 1 {
		t.Fatalf("after header click, table selection = %d, want 1", selRow)
	}
}
