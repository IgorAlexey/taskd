package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

func statusLine(t *testing.T, u *ui) string {
	t.Helper()
	return strings.SplitN(u.status.GetText(true), "\n", 2)[0]
}

func TestProjectScopedCounts(t *testing.T) {
	u := newUI("http://localhost:8080", "", false, "")
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
	u := newUI("http://localhost:8080", "", false, "")
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

func TestStatusBarWidth(t *testing.T) {
	u := newUI("http://localhost:8080", "", false, "")
	all := []task{
		{ID: "a1", Project: "proj-a", Status: "pending", Body: "first"},
	}
	u.render(all)

	msg120 := "error: " + strings.Repeat("a", 113)
	if len(msg120) != 120 {
		t.Fatalf("expected 120 chars, got %d", len(msg120))
	}

	// 160-column status area keeps a 120-character message intact.
	u.width = 160
	u.setMsg(msg120)
	got160 := u.status.GetText(true)
	if !strings.Contains(got160, msg120) {
		t.Fatalf("expected 160-column status area to keep 120-character message intact, got: %q", got160)
	}

	// 80-column status area still fits.
	u.width = 80
	u.setMsg(msg120)
	got80 := u.status.GetText(true)
	for _, line := range strings.Split(strings.TrimRight(got80, "\n"), "\n") {
		if w := uniseg.StringWidth(line); w > 80 {
			t.Fatalf("expected 80-column status area to fit within 80 columns, got width %d: %q", w, line)
		}
	}
	// End-to-end simulation screen with resize via app beforeDraw hook.
	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	u2 := newUI("http://localhost:8080", "", false, "")
	u2.app.SetScreen(sim)
	sim.SetSize(160, 25)
	u2.app.SetRoot(u2.pages, true)
	u2.setMsg(msg120)
	u2.app.ForceDraw()
	gotSim160 := u2.status.GetText(true)
	if !strings.Contains(gotSim160, msg120) {
		t.Fatalf("expected 160-column drawn status area to keep 120-char message intact, got: %q", gotSim160)
	}

	sim.SetSize(80, 25)
	u2.app.ForceDraw()
	gotSim80 := u2.status.GetText(true)
	for _, line := range strings.Split(strings.TrimRight(gotSim80, "\n"), "\n") {
		if w := uniseg.StringWidth(line); w > 80 {
			t.Fatalf("expected 80-column drawn status area to fit within 80 columns, got width %d: %q", w, line)
		}
	}
}

func TestDaemonOriginInStatusBar(t *testing.T) {
	u := newUI("http://127.0.0.1:18842", "", false, "")
	all := []task{
		{ID: "t1", Status: "pending", Body: "task 1"},
	}
	u.render(all)
	st := u.status.GetText(true)
	if !strings.Contains(st, "18842") {
		t.Fatalf("expected daemon origin port 18842 in status, got: %q", st)
	}

	u.toggleZoom()
	zoomSt := u.status.GetText(true)
	if !strings.Contains(zoomSt, "18842") {
		t.Fatalf("expected daemon origin port 18842 in zoomed status, got: %q", zoomSt)
	}
	u.toggleZoom()
}

func TestDaemonOriginSurvives40Columns(t *testing.T) {
	u := newUI("http://127.0.0.1:18842", "", false, "")
	u.width = 40
	u.render([]task{
		{ID: "t1", Status: "pending", Project: "very-long-project-name", Body: "task 1"},
	})
	st := u.status.GetText(true)
	if !strings.Contains(st, "18842") {
		t.Fatalf("status at 40 columns missing 18842:\n%s", st)
	}
}
