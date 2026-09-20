package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestFormSubmitShortcut(t *testing.T) {
	t.Run("CreateFormTextAreaSubmit", func(t *testing.T) {
		h := newTestHarness(t)
		h.query(func() { h.press('n') })

		var form *tview.Form
		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form != nil
		})

		h.query(func() {
			body := form.GetFormItem(3).(*tview.TextArea)
			body.SetText("brand new task via ctrl+s\nsecond line", true)
			h.u.app.SetFocus(body)
		})
		h.sim.InjectKey(tcell.KeyCtrlS, 0, 0)

		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form == nil
		})
		eventually(t, func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, tk := range *h.tasks {
				if tk.Body == "brand new task via ctrl+s\nsecond line" {
					return true
				}
			}
			return false
		})
	})

	t.Run("CreateFormTextAreaValidation", func(t *testing.T) {
		h := newTestHarness(t)
		h.query(func() { h.press('n') })

		var form *tview.Form
		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form != nil
		})

		h.query(func() {
			body := form.GetFormItem(3).(*tview.TextArea)
			h.u.app.SetFocus(body)
		})
		h.sim.InjectKey(tcell.KeyCtrlS, 0, 0)

		eventually(t, func() bool {
			var title string
			h.query(func() {
				if h.u.form != nil {
					title = h.u.form.GetTitle()
				}
			})
			return strings.Contains(title, "missing body or asset path")
		})
	})

	t.Run("EditFormTextAreaSubmit", func(t *testing.T) {
		h := newTestHarness(t)
		h.selectID("aaaaaaa1")
		h.query(func() { h.press('e') })

		var form *tview.Form
		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form != nil
		})

		h.query(func() {
			body := form.GetFormItem(3).(*tview.TextArea)
			body.SetText("edited body text via ctrl+s", true)
			h.u.app.SetFocus(body)
		})
		h.sim.InjectKey(tcell.KeyCtrlS, 0, 0)

		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form == nil
		})
		eventually(t, func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, tk := range *h.tasks {
				if tk.ID == "aaaaaaa1" && tk.Body == "edited body text via ctrl+s" {
					return true
				}
			}
			return false
		})
	})

	t.Run("CancelButtonFocusedSubmit", func(t *testing.T) {
		h := newTestHarness(t)
		h.query(func() { h.press('n') })

		var form *tview.Form
		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form != nil
		})

		h.query(func() {
			body := form.GetFormItem(3).(*tview.TextArea)
			body.SetText("submit from cancel button", true)
			cancelBtn := form.GetButton(1)
			h.u.app.SetFocus(cancelBtn)
		})
		h.sim.InjectKey(tcell.KeyCtrlS, 0, 0)

		eventually(t, func() bool {
			h.query(func() { form = h.u.form })
			return form == nil
		})
		eventually(t, func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			for _, tk := range *h.tasks {
				if tk.Body == "submit from cancel button" {
					return true
				}
			}
			return false
		})
	})
}
