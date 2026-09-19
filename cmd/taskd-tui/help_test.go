package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func setupTestApp(t *testing.T) (*ui, func(func()), func() string, func()) {
	t.Helper()
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.app.SetScreen(sim)

	done := make(chan struct{})
	go func() {
		u.app.Run()
		close(done)
	}()

	cleanup := func() {
		u.app.Stop()
		<-done
	}

	query := func(fn func()) {
		ch := make(chan struct{})
		u.app.QueueUpdate(func() {
			fn()
			close(ch)
		})
		<-ch
	}

	screenText := func() string {
		var text string
		query(func() {
			cells, w, h := sim.GetContents()
			var sb strings.Builder
			for y := range h {
				for x := range w {
					if c := cells[y*w+x]; len(c.Runes) > 0 {
						sb.WriteRune(c.Runes[0])
					} else {
						sb.WriteByte(' ')
					}
				}
				sb.WriteByte('\n')
			}
			text = sb.String()
		})
		return text
	}

	return u, query, screenText, cleanup
}

func TestHelpModalContentAndDismissEsc(t *testing.T) {
	u, query, screenText, cleanup := setupTestApp(t)
	defer cleanup()

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '?', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	requiredKeybindings := []string{
		"[j/k] move",
		"[g/G] top/bottom",
		"[0-4] filter status",
		"[p] cycle project",
		"[n] new task",
		"[e] edit task",
		"[D] delete task",
		"[+/-] priority",
		"[c] claim task",
		"[u] release task",
		"[y] copy ID",
		"[r] refresh",
		"[Tab] toggle pane focus",
		"[q] quit",
	}

	eventually(t, func() bool {
		st := screenText()
		for _, kb := range requiredKeybindings {
			if !strings.Contains(st, kb) {
				return false
			}
		}
		return true
	})

	st := screenText()
	for _, kb := range requiredKeybindings {
		if !strings.Contains(st, kb) {
			t.Errorf("screen missing keybinding %q in:\n%s", kb, st)
		}
	}

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	var focus tview.Primitive
	query(func() { focus = u.app.GetFocus() })
	if focus != u.table {
		t.Fatalf("expected focus to return to table, got %T", focus)
	}
}

func TestHelpModalDismissEnter(t *testing.T) {
	u, query, _, cleanup := setupTestApp(t)
	defer cleanup()

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '?', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	var focus tview.Primitive
	query(func() { focus = u.app.GetFocus() })
	if focus != u.table {
		t.Fatalf("expected focus to return to table, got %T", focus)
	}
}

func TestHelpModalDismissQuestionMarkAndQ(t *testing.T) {
	u, query, _, cleanup := setupTestApp(t)
	defer cleanup()

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '?', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyRune, '?', 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, '?', 0))
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'q', 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})
}

func TestHelpModalFromBodyKeysRestoresFocus(t *testing.T) {
	u, query, _, cleanup := setupTestApp(t)
	defer cleanup()

	u.app.QueueUpdateDraw(func() {
		u.app.SetFocus(u.body)
	})

	eventually(t, func() bool {
		var focus tview.Primitive
		query(func() { focus = u.app.GetFocus() })
		return focus == u.body
	})

	u.app.QueueUpdateDraw(func() {
		u.bodyKeys(tcell.NewEventKey(tcell.KeyRune, '?', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	var focus tview.Primitive
	query(func() { focus = u.app.GetFocus() })
	if focus != u.body {
		t.Fatalf("expected focus to return to body pane, got %T", focus)
	}
}
