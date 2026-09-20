package main

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestBodyHalfPageScroll(t *testing.T) {
	u, _, _ := stub(t)

	var body string
	for i := 1; i <= 100; i++ {
		body += fmt.Sprintf("body line %03d\n", i)
	}

	tasks := make([]task, 20)
	for i := range 20 {
		tasks[i] = task{
			ID:      fmt.Sprintf("task%02d", i+1),
			Project: "proj-a",
			Status:  "pending",
			Body:    fmt.Sprintf("task body %d", i+1),
		}
	}
	tasks[0].Body = body

	u.render(tasks)
	u.table.Select(1, 0)
	u.showBody()

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)

	u.body.SetRect(0, 0, 80, 20)
	u.body.Draw(sim)

	_, _, _, innerHeight := u.body.GetInnerRect()
	if innerHeight != 18 {
		t.Fatalf("expected inner height 18 for rect height 20 with border, got %d", innerHeight)
	}
	expectedStep := max(1, innerHeight/2)
	if expectedStep != 9 {
		t.Fatalf("expected derived step 9, got %d", expectedStep)
	}

	row, col := u.body.GetScrollOffset()
	if row != 0 || col != 0 {
		t.Fatalf("expected initial scroll offset (0, 0), got (%d, %d)", row, col)
	}

	evCtrlD := tcell.NewEventKey(tcell.KeyCtrlD, 0, 0)
	evCtrlU := tcell.NewEventKey(tcell.KeyCtrlU, 0, 0)

	ret := u.bodyKeys(evCtrlD)
	if ret != nil {
		t.Fatalf("expected bodyKeys(CtrlD) to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != expectedStep {
		t.Fatalf("after first Ctrl+D expected row %d (concrete 9), got %d (draw invoked to settle clamp)", expectedStep, row)
	}
	if col != 0 {
		t.Fatalf("after first Ctrl+D expected col 0, got %d", col)
	}

	ret = u.bodyKeys(evCtrlD)
	if ret != nil {
		t.Fatalf("expected second bodyKeys(CtrlD) to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != 2*expectedStep {
		t.Fatalf("after second Ctrl+D expected row %d (concrete 18), got %d (draw invoked to settle clamp)", 2*expectedStep, row)
	}
	if col != 0 {
		t.Fatalf("after second Ctrl+D expected col 0, got %d", col)
	}

	ret = u.bodyKeys(evCtrlU)
	if ret != nil {
		t.Fatalf("expected first bodyKeys(CtrlU) to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != expectedStep {
		t.Fatalf("after first Ctrl+U expected row %d (concrete 9), got %d (draw invoked to settle clamp)", expectedStep, row)
	}
	if col != 0 {
		t.Fatalf("after first Ctrl+U expected col 0, got %d", col)
	}

	ret = u.bodyKeys(evCtrlU)
	if ret != nil {
		t.Fatalf("expected second bodyKeys(CtrlU) to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != 0 {
		t.Fatalf("after second Ctrl+U expected row 0, got %d (draw invoked to settle clamp)", row)
	}
	if col != 0 {
		t.Fatalf("after second Ctrl+U expected col 0, got %d", col)
	}

	ret = u.bodyKeys(evCtrlU)
	if ret != nil {
		t.Fatalf("expected bodyKeys(CtrlU) at top to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != 0 {
		t.Fatalf("after Ctrl+U at top expected row 0, got %d (draw invoked to settle clamp)", row)
	}
	if col != 0 {
		t.Fatalf("after Ctrl+U at top expected col 0, got %d", col)
	}

	u.body.SetRect(0, 0, 80, 1)
	u.body.Draw(sim)
	_, _, _, smallH := u.body.GetInnerRect()
	smallStep := max(1, smallH/2)
	if smallStep != 1 {
		t.Fatalf("expected degenerate step 1, got %d", smallStep)
	}

	ret = u.bodyKeys(evCtrlD)
	if ret != nil {
		t.Fatalf("expected bodyKeys(CtrlD) on 1-row rect to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != 1 {
		t.Fatalf("after Ctrl+D on 1-row rect expected row 1, got %d (draw invoked to settle clamp)", row)
	}
	if col != 0 {
		t.Fatalf("after Ctrl+D on 1-row rect expected col 0, got %d", col)
	}

	ret = u.bodyKeys(evCtrlU)
	if ret != nil {
		t.Fatalf("expected bodyKeys(CtrlU) on 1-row rect to return nil, got %v", ret)
	}
	u.body.Draw(sim)
	row, col = u.body.GetScrollOffset()
	if row != 0 {
		t.Fatalf("after Ctrl+U on 1-row rect expected row 0, got %d (draw invoked to settle clamp)", row)
	}
	if col != 0 {
		t.Fatalf("after Ctrl+U on 1-row rect expected col 0, got %d", col)
	}

	u.table.SetRect(0, 0, 80, 25)
	u.table.Draw(sim)
	u.table.Select(5, 0)
	if r := u.selectedRow(); r != 5 {
		t.Fatalf("expected table selected row 5, got %d", r)
	}
	if ret := u.keys(evCtrlD); ret != nil {
		t.Fatalf("expected table keys(CtrlD) to return nil, got %v", ret)
	}
	if r := u.selectedRow(); r != 17 {
		t.Fatalf("expected table Ctrl+D to move selection to row 17, got %d", r)
	}
	if ret := u.keys(evCtrlU); ret != nil {
		t.Fatalf("expected table keys(CtrlU) to return nil, got %v", ret)
	}
	if r := u.selectedRow(); r != 5 {
		t.Fatalf("expected table Ctrl+U to move selection back to row 5, got %d", r)
	}
}
