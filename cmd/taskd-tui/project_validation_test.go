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
