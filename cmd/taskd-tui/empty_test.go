package main

import (
	"strings"
	"testing"
)

func TestEmptyStateMessage(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, filter, project string
		in                    []task
		want                  []string
	}{
		{"empty queue", "", "", nil, []string{"No tasks yet", "'n'"}},
		// ts[:1] is pending only, so the done filter matches nothing.
		{"status filter", "done", "", ts[:1], []string{"No done tasks", "'0'"}},
		{"project filter", "", "proj-x", ts, []string{"No tasks in proj-x", "'p'"}},
		{"both filters", "pending", "proj-x", ts, []string{"No tasks match filter", "'0'", "'p'"}},
	} {
		u.filter, u.project = tc.filter, tc.project
		u.render(tc.in)
		if len(u.shown) != 0 {
			t.Fatalf("%s: expected nothing shown, got %d", tc.name, len(u.shown))
		}
		got := u.body.GetText(true)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Fatalf("%s: placeholder %q lacks %q", tc.name, got, want)
			}
		}
		// The cache pair must keep meaning what it says: no task shown.
		if u.shownID != "" || u.shownBody != got {
			t.Fatalf("%s: cache = %q/%q, want \"\"/%q", tc.name, u.shownID, u.shownBody, got)
		}
	}

	// A match again: the placeholder gives way to the real task.
	u.filter, u.project = "", ""
	u.render(ts)
	if got := u.body.GetText(true); !strings.Contains(got, "first task") {
		t.Fatalf("detail pane not restored: %q", got)
	}
	if got := u.table.GetRowCount(); got != len(u.shown)+1 {
		t.Fatalf("rows = %d, want %d", got, len(u.shown)+1)
	}
}
