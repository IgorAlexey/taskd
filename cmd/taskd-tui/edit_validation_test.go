package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestEditFormValidation(t *testing.T) {
	h := newTestHarness(t)

	h.selectID("aaaaaaa1")
	h.press('e')

	var form *tview.Form
	eventually(t, func() bool {
		h.query(func() { form = h.u.form })
		return form != nil
	})

	var (
		bodyItem  *tview.TextArea
		assetItem *tview.InputField
	)
	h.query(func() {
		assetItem = form.GetFormItem(2).(*tview.InputField)
		bodyItem = form.GetFormItem(3).(*tview.TextArea)
	})

	if got := assetItem.GetText(); got != "" {
		t.Fatalf("expected empty asset path, got %q", got)
	}

	h.query(func() {
		bodyItem.SetText("", true)
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	h.query(func() { form = h.u.form })
	if form == nil {
		t.Fatal("clearing body on task with no asset_path must leave u.form non-nil")
	}
	if got := bodyItem.GetText(); got != "" {
		t.Fatalf("expected empty text to stay in TextArea, got %q", got)
	}
	if title := form.GetTitle(); !strings.Contains(title, "missing body or asset path") {
		t.Fatalf("expected title to indicate missing body error, got %q", title)
	}

	h.query(func() {
		bodyItem.SetText("   \n   ", true)
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	})

	h.query(func() { form = h.u.form })
	if form == nil {
		t.Fatal("whitespace body on task with no asset_path must leave u.form non-nil")
	}
	if got := bodyItem.GetText(); got != "   \n   " {
		t.Fatalf("expected whitespace text to stay in TextArea, got %q", got)
	}
	if title := form.GetTitle(); !strings.Contains(title, "missing body or asset path") {
		t.Fatalf("expected title to indicate missing body error, got %q", title)
	}
}
