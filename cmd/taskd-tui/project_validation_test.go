package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestValidProject(t *testing.T) {
	valid := []string{
		"proj",
		"PROJ",
		"proj-123",
		"proj_123",
		"proj.123",
		"my-cool_proj.v1",
		strings.Repeat("a", 64),
	}
	for _, p := range valid {
		if !validProject(p) {
			t.Fatalf("expected project %q to be valid", p)
		}
	}

	invalid := []string{
		"",
		" ",
		"   ",
		"proj with space",
		"proj/slash",
		"proj\\slash",
		"*",
		"proj*wild",
		"proj@domain",
		"proj:name",
		"proj?query",
		"proj#tag",
		"proj!bang",
		"proj$cash",
		"proj%pct",
		strings.Repeat("a", 65),
	}
	for _, p := range invalid {
		if validProject(p) {
			t.Fatalf("expected project %q to be invalid", p)
		}
	}
}

func TestCreateFormProjectValidation(t *testing.T) {
	h := newTestHarness(t)

	h.query(func() {
		h.press('n')
	})

	var form *tview.Form
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})

	var (
		projItem *tview.InputField
		bodyItem *tview.TextArea
	)
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
		bodyItem = form.GetFormItem(3).(*tview.TextArea)
		bodyItem.SetText("brand new body", true)
	})

	for _, invalid := range []string{"proj with spaces", "proj/with/slash"} {
		h.query(func() {
			projItem.SetText(invalid)
			form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
		})

		h.query(func() { form = h.u.form })
		if form == nil {
			t.Fatalf("expected create form to stay open on invalid project %q", invalid)
		}
		if title := form.GetTitle(); !strings.Contains(title, "invalid project") {
			t.Fatalf("expected title to indicate invalid project on %q, got %q", invalid, title)
		}
		if got := projItem.GetText(); got != invalid {
			t.Fatalf("expected project buffer %q preserved, got %q", invalid, got)
		}
		if got := bodyItem.GetText(); got != "brand new body" {
			t.Fatalf("expected body buffer preserved, got %q", got)
		}
	}

	h.query(func() {
		projItem.SetText("  valid-proj  ")
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})

	eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(*h.tasks) == 4
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	if (*h.tasks)[3].Project != "valid-proj" {
		t.Fatalf("expected trimmed project %q, got %q", "valid-proj", (*h.tasks)[3].Project)
	}
}

func TestEditFormProjectValidation(t *testing.T) {
	h := newTestHarness(t)

	h.selectID("aaaaaaa1")
	h.query(func() {
		h.press('e')
	})

	var form *tview.Form
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})

	var (
		projItem *tview.InputField
		bodyItem *tview.TextArea
	)
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
		bodyItem = form.GetFormItem(3).(*tview.TextArea)
	})

	origBody := bodyItem.GetText()

	for _, invalid := range []string{"proj with spaces", "proj/with/slash"} {
		h.query(func() {
			projItem.SetText(invalid)
			form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
		})

		h.query(func() { form = h.u.form })
		if form == nil {
			t.Fatalf("expected edit form to stay open on invalid project %q", invalid)
		}
		if title := form.GetTitle(); !strings.Contains(title, "invalid project") {
			t.Fatalf("expected title to indicate invalid project on %q, got %q", invalid, title)
		}
		if got := projItem.GetText(); got != invalid {
			t.Fatalf("expected project buffer %q preserved, got %q", invalid, got)
		}
		if got := bodyItem.GetText(); got != origBody {
			t.Fatalf("expected body buffer preserved, got %q", got)
		}
	}

	h.query(func() {
		projItem.SetText("  valid-edited-proj  ")
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})

	eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return (*h.tasks)[0].Project == "valid-edited-proj"
	})
}
func TestCreateFormDefaultProject(t *testing.T) {
	h := newTestHarness(t)

	h.selectID("aaaaaaa1")
	h.query(func() {
		h.press('n')
	})

	var form *tview.Form
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})

	var projItem *tview.InputField
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
	})

	if got := projItem.GetText(); got != "proj-b" {
		t.Fatalf("expected default project proj-b from selected row, got %q", got)
	}

	h.query(func() {
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})

	h.selectID("bbbbbbb2")
	h.query(func() {
		h.press('n')
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
	})
	if got := projItem.GetText(); got != "proj-a" {
		t.Fatalf("expected default project proj-a from selected row, got %q", got)
	}
	h.query(func() {
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})

	h.query(func() {
		h.u.project = "explicit-filter"
		h.press('n')
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
	})
	if got := projItem.GetText(); got != "explicit-filter" {
		t.Fatalf("expected default project explicit-filter from u.project, got %q", got)
	}
	h.query(func() {
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
		h.u.project = ""
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})

	origGit := gitCheckoutName
	gitCheckoutName = func() string { return "wt-checkout" }
	t.Cleanup(func() { gitCheckoutName = origGit })

	h.query(func() {
		h.u.shown = nil
		h.press('n')
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
	})
	if got := projItem.GetText(); got != "wt-checkout" {
		t.Fatalf("expected default project wt-checkout from git fallback, got %q", got)
	}
	h.query(func() {
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})
}

func TestNewTaskUnknownProject(t *testing.T) {
	h := newTestHarness(t)
	h.query(func() {
		h.u.projects = []string{"proj-a", "proj-b"}
	})

	h.query(func() {
		h.press('n')
	})
	var form *tview.Form
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})

	var (
		projItem *tview.InputField
		bodyItem *tview.TextArea
	)
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
		bodyItem = form.GetFormItem(3).(*tview.TextArea)
		projItem.SetText("unknown-proj")
		bodyItem.SetText("task for unknown project", true)
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	var modal *tview.Modal
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})

	var (
		focusedButton int
		focusedLabel  string
	)
	h.query(func() {
		modal.Focus(func(p tview.Primitive) {
			if f, ok := p.(*tview.Form); ok {
				_, focusedButton = f.GetFocusedItemIndex()
				if focusedButton >= 0 && focusedButton < f.GetButtonCount() {
					focusedLabel = f.GetButton(focusedButton).GetLabel()
				}
			}
		})
	})

	var modalText string
	eventually(t, func() bool {
		modalText = h.screenText()
		return strings.Contains(modalText, "unknown-proj") && strings.Contains(modalText, "proj-a, proj-b")
	})
	if focusedButton != 1 || focusedLabel != "Cancel" {
		t.Fatalf("expected default focus on Cancel button (index 1), got index %d label %q", focusedButton, focusedLabel)
	}
	h.query(func() {
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal == nil
	})

	h.mu.Lock()
	initialCount := len(*h.tasks)
	h.mu.Unlock()
	if initialCount != 3 {
		t.Fatalf("expected 3 tasks after canceling unknown project, got %d", initialCount)
	}

	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.query(func() {
		if got := projItem.GetText(); got != "unknown-proj" {
			t.Fatalf("expected project preserved after cancel, got %q", got)
		}
		if got := bodyItem.GetText(); got != "task for unknown project" {
			t.Fatalf("expected body preserved after cancel, got %q", got)
		}
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal != nil
	})

	h.query(func() {
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyLeft, 0, 0), nil)
		modal.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})
	eventually(t, func() bool {
		h.query(func() { modal = h.u.modal })
		return modal == nil
	})

	eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(*h.tasks) == 4 && (*h.tasks)[3].Project == "unknown-proj"
	})

	h.query(func() {
		h.press('n')
	})
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})
	h.query(func() {
		projItem = form.GetFormItem(0).(*tview.InputField)
		bodyItem = form.GetFormItem(3).(*tview.TextArea)
		projItem.SetText("proj-a")
		bodyItem.SetText("known project direct creation", true)
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form == nil
	})
	h.query(func() {
		if h.u.modal != nil {
			t.Fatalf("expected no modal confirmation for known project proj-a")
		}
	})

	eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(*h.tasks) == 5 && (*h.tasks)[4].Project == "proj-a"
	})
}
