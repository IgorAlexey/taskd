package main

import "testing"

// The columns the detail pane reprints one keypress away are shed before
// the title, which is the only thing on a row that names the task.
func TestBudgetColumnsShedsRedundancyBeforeTitle(t *testing.T) {
	cases := []struct {
		name string
		w    int
		want tableCols
	}{
		{
			"110 pays for everything",
			110,
			tableCols{priority: 1, scope: 12, title: 46, claims: 4, worker: 14, lease: 8, left: 7, id: 7},
		},
		{
			"80 sheds the claim count and the worker",
			80,
			tableCols{priority: 1, scope: 12, title: 36, claims: 0, worker: 0, lease: 8, left: 7, id: 7},
		},
		{
			"70 still pays for the lease bar",
			70,
			tableCols{priority: 1, scope: 12, title: 26, claims: 0, worker: 0, lease: 8, left: 7, id: 7},
		},
		{
			"58 sheds the lease bar and the remaining time",
			58,
			tableCols{priority: 1, scope: 12, title: 31, claims: 0, worker: 0, lease: 0, left: 0, id: 7},
		},
		{
			"50 degrades the scope to its header",
			50,
			tableCols{priority: 1, scope: 5, title: 30, claims: 0, worker: 0, lease: 0, left: 0, id: 7},
		},
		{
			"42 gives up the id last",
			42,
			tableCols{priority: 1, scope: 5, title: 30, claims: 0, worker: 0, lease: 0, left: 0, id: 0},
		},
		{
			"32 has nothing left to shed",
			32,
			tableCols{priority: 1, scope: 5, title: 20, claims: 0, worker: 0, lease: 0, left: 0, id: 0},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := budgetColumns(c.w, 1, 14, 25, 4, 7, true)
			if got != c.want {
				t.Fatalf("budgetColumns(%d) = %+v, want %+v", c.w, got, c.want)
			}
		})
	}
}

// A title cut to 17 characters is a row the operator cannot tell from its
// neighbour: down to the width where the scope is already a stub, the
// title keeps 24 columns and the row keeps one identifier.
func TestBudgetColumnsKeepsTitleAndOneIdentifier(t *testing.T) {
	for w := 44; w <= 60; w++ {
		got := budgetColumns(w, 1, 14, 25, 4, 7, true)
		if got.title < 24 {
			t.Fatalf("%d cols: title %d, want at least 24 (%+v)", w, got.title, got)
		}
		if got.id == 0 {
			t.Fatalf("%d cols: id dropped while the scope could still degrade (%+v)", w, got)
		}
	}
}

func TestBudgetColumnsIDWidth(t *testing.T) {
	gotMin := budgetColumns(110, 1, 5, 0, 0, 1, false)
	if gotMin.id != 2 {
		t.Fatalf("budgetColumns with maxID 1: got id %d, want 2", gotMin.id)
	}
	gotWide := budgetColumns(110, 1, 5, 0, 0, 5, false)
	if gotWide.id != 5 {
		t.Fatalf("budgetColumns with maxID 5: got id %d, want 5", gotWide.id)
	}
}
