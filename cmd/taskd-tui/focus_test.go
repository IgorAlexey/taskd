package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestFocusIndicator(t *testing.T) {
	u, query, _, cleanup := setupTestApp(t)
	defer cleanup()

	expectFocus := func(focused bool, color tcell.Color) {
		t.Helper()
		eventually(t, func() bool {
			var ok bool
			query(func() {
				ok = u.body.HasFocus() == focused &&
					u.table.HasFocus() == !focused &&
					u.body.GetBorderColor() == color
			})
			return ok
		})
	}

	expectFocus(false, tview.Styles.BorderColor)

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	})
	expectFocus(true, tcell.ColorYellow)

	u.app.QueueUpdateDraw(func() {
		u.bodyKeys(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	})
	expectFocus(false, tview.Styles.BorderColor)

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyBacktab, 0, 0))
	})
	expectFocus(true, tcell.ColorYellow)

	u.app.QueueUpdateDraw(func() {
		u.bodyKeys(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	})
	expectFocus(false, tview.Styles.BorderColor)

	u.app.QueueUpdateDraw(func() {
		u.keys(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
	})
	expectFocus(true, tcell.ColorYellow)

	u.app.QueueUpdateDraw(func() {
		u.bodyKeys(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	})
	expectFocus(false, tview.Styles.BorderColor)
}
