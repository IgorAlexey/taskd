package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestModalSwallowsClickOutsideForm(t *testing.T) {
	h := newTestHarness(t)
	h.query(func() { h.press('n') })
	eventually(t, func() bool {
		var f *tview.Form
		h.query(func() { f = h.u.form })
		return f != nil
	})
	h.screenText()

	var consumed, formFocus, tableFocus bool
	h.query(func() {
		ev := tcell.NewEventMouse(5, 5, tcell.ButtonPrimary, tcell.ModNone)
		consumed, _ = h.u.pages.MouseHandler()(tview.MouseLeftDown, ev, func(p tview.Primitive) {
			h.u.app.SetFocus(p)
		})
		formFocus, tableFocus = h.u.form.HasFocus(), h.u.table.HasFocus()
	})
	if !consumed {
		t.Error("click outside the centred form was not consumed by the modal")
	}
	if !formFocus || tableFocus {
		t.Fatalf("after clicking outside modal: form focus %v, table focus %v, want true, false",
			formFocus, tableFocus)
	}

	h.sim.InjectKey(tcell.KeyRune, 'q', 0)
	h.sim.InjectKey(tcell.KeyRune, 'D', 0)
	eventually(t, func() bool {
		var text string
		h.query(func() {
			text = h.u.form.GetFormItem(0).(*tview.InputField).GetText()
		})
		return strings.HasSuffix(text, "qD")
	})

	h.query(func() {
		if h.u.pages.HasPage("delete") {
			t.Error("D after a click outside the modal opened the delete confirm")
		}
		if !h.u.pages.HasPage("create") {
			t.Error("q after a click outside the modal closed the create form")
		}
	})
}
