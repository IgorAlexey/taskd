package main

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func statusLine(t *testing.T, u *ui) string {
	t.Helper()
	return strings.SplitN(u.status.GetText(true), "\n", 2)[0]
}

func TestProjectScopedCounts(t *testing.T) {
	u := newUI("http://localhost:8080", "", false)
	all := []task{
		{ID: "a1", Project: "proj-a", Status: "pending", Body: "a pending"},
		{ID: "a2", Project: "proj-a", Status: "done", Body: "a done"},
		{ID: "b1", Project: "proj-b", Status: "pending", Body: "b pending"},
		{ID: "b2", Project: "proj-b", Status: "pending", Body: "b pending too"},
		{ID: "b3", Project: "proj-b", Status: "leased", Worker: "w1", Body: "b leased"},
	}

	u.render(all)
	if u.pending != 3 || u.leased != 1 || u.done != 1 {
		t.Fatalf("unscoped counts = %d/%d/%d", u.pending, u.leased, u.done)
	}

	u.project = "proj-a"
	u.render(all)
	if u.pending != 1 || u.leased != 0 || u.done != 1 {
		t.Fatalf("proj-a counts = %d/%d/%d", u.pending, u.leased, u.done)
	}
	if got := statusLine(t, u); !strings.Contains(got, "pending 1  leased 0  done 1") {
		t.Fatalf("proj-a status line = %q", got)
	}

	u.project = "proj-b"
	u.render(all)
	if u.pending != 2 || u.leased != 1 || u.done != 0 {
		t.Fatalf("proj-b counts = %d/%d/%d", u.pending, u.leased, u.done)
	}
	if got := statusLine(t, u); !strings.Contains(got, "pending 2  leased 1  done 0") {
		t.Fatalf("proj-b status line = %q", got)
	}

	u.filter = "pending"
	u.render(all)
	if u.pending != 2 || u.leased != 1 || u.done != 0 {
		t.Fatalf("status filter must not change counts, got %d/%d/%d", u.pending, u.leased, u.done)
	}
	if len(u.shown) != 2 {
		t.Fatalf("proj-b pending rows = %d", len(u.shown))
	}

	u.project = "proj-c"
	u.filter = ""
	u.render(all)
	if u.pending != 0 || u.leased != 0 || u.done != 0 {
		t.Fatalf("unknown project counts = %d/%d/%d", u.pending, u.leased, u.done)
	}
}

func TestStatusBarIndex(t *testing.T) {
	u := newUI("http://localhost:8080", "", false)
	u.filter = ""
	all := []task{
		{ID: "a1", Project: "proj-a", Status: "pending", Body: "first"},
		{ID: "a2", Project: "proj-a", Status: "pending", Body: "second"},
		{ID: "b1", Project: "proj-b", Status: "done", Body: "third"},
	}

	u.render(all)
	if got := statusLine(t, u); !strings.Contains(got, "row 1 of 3") {
		t.Fatalf("initial status line = %q", got)
	}

	u.table.Select(3, 0)
	if got := statusLine(t, u); !strings.Contains(got, "row 3 of 3") {
		t.Fatalf("status line after select = %q", got)
	}

	u.table.Select(2, 0)
	if got := statusLine(t, u); !strings.Contains(got, "row 2 of 3") {
		t.Fatalf("status line after move up = %q", got)
	}

	u.project = "proj-b"
	u.render(all)
	if got := statusLine(t, u); !strings.Contains(got, "row 1 of 1") {
		t.Fatalf("scoped status line = %q", got)
	}

	u.filter = "pending"
	u.render(all)
	if got := statusLine(t, u); !strings.Contains(got, "row 0 of 0") {
		t.Fatalf("empty status line = %q", got)
	}

	u.filter, u.project = "", strings.Repeat("long-project-", 5)
	u.render(all)
	got := statusLine(t, u)
	if !strings.Contains(got, "row 0 of 0") {
		t.Fatalf("long project status line = %q", got)
	}
	if w := uniseg.StringWidth(got); w > 80 {
		t.Fatalf("status line width = %d: %q", w, got)
	}
}
