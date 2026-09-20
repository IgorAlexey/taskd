package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTUIBuriedAndKick(t *testing.T) {
	u, tasks, mu := stub(t)
	ts, err := u.fetch("", "")
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(120, 25)
	u.app.SetScreen(sim)

	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()
	defer func() {
		u.app.Stop()
		<-done
	}()

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}
	press := func(r rune) {
		u.app.QueueUpdateDraw(func() {
			u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
		})
	}
	confirm := func() {
		var modal *tview.Modal
		eventually(t, func() bool {
			query(func() { modal = u.modal })
			return modal != nil
		})
		u.app.QueueUpdateDraw(func() {
			u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
			u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
		})
		eventually(t, func() bool {
			query(func() { modal = u.modal })
			return modal == nil
		})
	}
	status := func(id string) string {
		var got string
		mu.Lock()
		defer mu.Unlock()
		for _, tk := range *tasks {
			if tk.ID == id {
				got = tk.Status
			}
		}
		return got
	}

	query(func() { u.table.Select(1, 0) })
	press('b')
	var msg string
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == "task is not actively leased"
	})

	query(func() {
		u.worker = "w2"
		u.table.Select(2, 0)
	})
	press('b')
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == "cannot bury task leased by another worker"
	})
	if got := buryCallsSnapshot(); len(got) != 0 {
		t.Fatalf("bury issued for a lease held by another worker: %v", got)
	}

	query(func() { u.worker = "w1" })
	press('b')
	confirm()

	eventually(t, func() bool { return status("bbbbbbb2") == "buried" })
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == "buried task bbbbbbb"
	})
	if got := buryCallsSnapshot(); !slices.Equal(got, []string{"bbbbbbb2 w1"}) {
		t.Fatalf("bury calls = %v", got)
	}

	eventually(t, func() bool {
		var n int
		query(func() { n = u.stats.Buried })
		return n == 1
	})

	var line, statusCell string
	var color tcell.Color
	var shown []task
	query(func() {
		u.filter = "buried"
		u.width = 120
		u.render(u.all)
		shown = slices.Clone(u.shown)
		statusCell = u.table.GetCell(1, 0).Text
		color, _, _ = u.table.GetCell(1, 0).Style.Decompose()
		line = u.status.GetText(true)
	})
	if len(shown) != 1 || shown[0].ID != "bbbbbbb2" {
		t.Fatalf("buried filter shows %v", shown)
	}
	if statusCell != "buried" {
		t.Fatalf("status cell = %q", statusCell)
	}
	if color != tcell.ColorRed {
		t.Fatalf("buried row color = %v", color)
	}
	if !strings.Contains(line, "buried 1") {
		t.Fatalf("status bar = %q", line)
	}

	query(func() {
		u.filter = ""
		u.keys(tcell.NewEventKey(tcell.KeyRune, '5', 0))
		shown = slices.Clone(u.shown)
	})
	if u.filter != "buried" || len(shown) != 1 || shown[0].ID != "bbbbbbb2" {
		t.Fatalf("key 5 filter = %q rows %v", u.filter, shown)
	}

	query(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '4', 0))
		shown = slices.Clone(u.shown)
	})
	if u.filter != "live" {
		t.Fatalf("key 4 filter = %q", u.filter)
	}
	for _, tk := range shown {
		if tk.Status == "buried" {
			t.Fatalf("live filter kept buried task %s", tk.ID)
		}
	}

	query(func() {
		u.filter = "buried"
		u.render(u.all)
		u.table.Select(1, 0)
	})
	press('K')
	confirm()

	eventually(t, func() bool { return status("bbbbbbb2") == "pending" })
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == "kicked task bbbbbbb"
	})
	got := kickCallsSnapshot()
	if len(got) != 1 || got[0].uri != "/tasks/bbbbbbb2/kick" {
		t.Fatalf("kick calls = %v", got)
	}
	if got[0].body > 0 {
		t.Fatalf("kick sent a request body of %d bytes", got[0].body)
	}

	query(func() {
		u.filter = ""
		u.render(u.all)
		u.table.Select(2, 0)
	})
	press('K')
	eventually(t, func() bool {
		query(func() { msg = u.msg })
		return msg == "task is not buried"
	})
}
