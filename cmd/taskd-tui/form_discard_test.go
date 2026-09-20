package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func assertCleanEscape(t *testing.T, h *testHarness, form *tview.Form) {
	t.Helper()
	h.query(func() {
		form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
	})
	eventually(t, func() bool {
		var open bool
		h.query(func() { open = h.u.form != nil || h.u.modal != nil })
		return !open
	})
}

func assertDiscardFlow(t *testing.T, h *testHarness, form *tview.Form, trigger func(), checkPreserved func() bool) {
	t.Helper()
	h.query(trigger)
	eventually(t, func() bool {
		var modal *tview.Modal
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})
	if !strings.Contains(h.screenText(), "Discard unsaved changes?") {
		t.Fatalf("expected discard confirmation prompt on screen, got:\n%s", h.screenText())
	}
	h.query(func() {
		h.u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		var modal *tview.Modal
		h.query(func() { modal = h.u.modal })
		return modal == nil
	})
	var preserved bool
	h.query(func() {
		preserved = h.u.form != nil && checkPreserved()
	})
	if !preserved {
		t.Fatal("form must remain open with preserved inputs after cancelling discard")
	}
	h.query(trigger)
	eventually(t, func() bool {
		var modal *tview.Modal
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})
	h.query(func() {
		h.u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
		h.u.modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		var open bool
		h.query(func() { open = h.u.form != nil || h.u.modal != nil })
		return !open
	})
}

func TestTUIFormDiscardConfirmation(t *testing.T) {
	openCreate := func(h *testHarness) {
		h.query(func() { h.press('n') })
	}
	openEdit := func(h *testHarness) {
		h.selectID("aaaaaaa1")
		h.query(func() { h.press('e') })
	}

	t.Run("CleanForms", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			open func(h *testHarness)
		}{
			{"Create", openCreate},
			{"Edit", openEdit},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newTestHarness(t)
				tc.open(h)
				var form *tview.Form
				eventually(t, func() bool {
					h.query(func() { form = h.u.form })
					return form != nil
				})
				assertCleanEscape(t, h, form)
			})
		}
	})

	t.Run("DirtyForms", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			open    func(h *testHarness)
			modify  func(f *tview.Form)
			check   func(f *tview.Form) bool
			trigger func(f *tview.Form)
		}{
			{
				name:   "CreateBody",
				open:   openCreate,
				modify: func(f *tview.Form) { f.GetFormItem(3).(*tview.TextArea).SetText("dirty body", true) },
				check:  func(f *tview.Form) bool { return f.GetFormItem(3).(*tview.TextArea).GetText() == "dirty body" },
			},
			{
				name:   "CreateProject",
				open:   openCreate,
				modify: func(f *tview.Form) { f.GetFormItem(0).(*tview.InputField).SetText("new-proj") },
				check:  func(f *tview.Form) bool { return f.GetFormItem(0).(*tview.InputField).GetText() == "new-proj" },
			},
			{
				name:   "CreatePriority",
				open:   openCreate,
				modify: func(f *tview.Form) { f.GetFormItem(1).(*tview.InputField).SetText("99") },
				check:  func(f *tview.Form) bool { return f.GetFormItem(1).(*tview.InputField).GetText() == "99" },
			},
			{
				name:   "CreateAssetPath",
				open:   openCreate,
				modify: func(f *tview.Form) { f.GetFormItem(2).(*tview.InputField).SetText("path/asset") },
				check:  func(f *tview.Form) bool { return f.GetFormItem(2).(*tview.InputField).GetText() == "path/asset" },
			},
			{
				name:   "CreateCancelButton",
				open:   openCreate,
				modify: func(f *tview.Form) { f.GetFormItem(3).(*tview.TextArea).SetText("btn text", true) },
				check:  func(f *tview.Form) bool { return f.GetFormItem(3).(*tview.TextArea).GetText() == "btn text" },
				trigger: func(f *tview.Form) {
					f.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
				},
			},
			{
				name:   "EditBody",
				open:   openEdit,
				modify: func(f *tview.Form) { f.GetFormItem(3).(*tview.TextArea).SetText("edited body", true) },
				check:  func(f *tview.Form) bool { return f.GetFormItem(3).(*tview.TextArea).GetText() == "edited body" },
			},
			{
				name:   "EditProject",
				open:   openEdit,
				modify: func(f *tview.Form) { f.GetFormItem(0).(*tview.InputField).SetText("edited-proj") },
				check:  func(f *tview.Form) bool { return f.GetFormItem(0).(*tview.InputField).GetText() == "edited-proj" },
			},
			{
				name:   "EditPriority",
				open:   openEdit,
				modify: func(f *tview.Form) { f.GetFormItem(1).(*tview.InputField).SetText("77") },
				check:  func(f *tview.Form) bool { return f.GetFormItem(1).(*tview.InputField).GetText() == "77" },
			},
			{
				name:   "EditAssetPath",
				open:   openEdit,
				modify: func(f *tview.Form) { f.GetFormItem(2).(*tview.InputField).SetText("new/path") },
				check:  func(f *tview.Form) bool { return f.GetFormItem(2).(*tview.InputField).GetText() == "new/path" },
			},
			{
				name:   "EditCancelButton",
				open:   openEdit,
				modify: func(f *tview.Form) { f.GetFormItem(3).(*tview.TextArea).SetText("btn edit text", true) },
				check:  func(f *tview.Form) bool { return f.GetFormItem(3).(*tview.TextArea).GetText() == "btn edit text" },
				trigger: func(f *tview.Form) {
					f.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h := newTestHarness(t)
				tc.open(h)
				var form *tview.Form
				eventually(t, func() bool {
					h.query(func() { form = h.u.form })
					return form != nil
				})
				h.query(func() { tc.modify(form) })
				trigger := func() {
					if tc.trigger != nil {
						tc.trigger(form)
					} else {
						form.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), nil)
					}
				}
				assertDiscardFlow(t, h, form, trigger, func() bool { return tc.check(form) })
			})
		}
	})

	t.Run("EditReverted", func(t *testing.T) {
		h := newTestHarness(t)
		openEdit(h)
		var (
			form *tview.Form
			body *tview.TextArea
		)
		eventually(t, func() bool {
			h.query(func() {
				form = h.u.form
				if form != nil {
					body = form.GetFormItem(3).(*tview.TextArea)
				}
			})
			return form != nil && body != nil
		})
		h.query(func() {
			orig := body.GetText()
			body.SetText("temporary edit", true)
			body.SetText(orig, true)
		})
		assertCleanEscape(t, h, form)
	})
}
