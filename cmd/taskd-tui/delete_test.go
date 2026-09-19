package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestDeleteConfirmDefaultsToCancel(t *testing.T) {
	u, tasks, mu := stub(t)
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

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'D', 0))
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal != nil
	})

	var focusedButton int
	var focusedLabel string
	query(func() {
		u.modal.Focus(func(p tview.Primitive) {
			if form, ok := p.(*tview.Form); ok {
				_, focusedButton = form.GetFocusedItemIndex()
				if focusedButton >= 0 && focusedButton < form.GetButtonCount() {
					focusedLabel = form.GetButton(focusedButton).GetLabel()
				}
			}
		})
	})

	if focusedButton != 1 {
		t.Fatalf("expected focused button index 1, got %d", focusedButton)
	}
	if focusedLabel != "Cancel" {
		t.Fatalf("expected focused button label Cancel, got %q", focusedLabel)
	}

	u.app.QueueUpdateDraw(func() {
		u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		query(func() { modal = u.modal })
		return modal == nil
	})

	mu.Lock()
	count := len(*tasks)
	mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 3 tasks after bare Enter on delete confirmation, got %d", count)
	}

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyRune, 'D', 0))
	})

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

	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, tk := range *tasks {
			if tk.ID == "aaaaaaa1" {
				return false
			}
		}
		return true
	})
}
