package main

import (
	"strings"
	"testing"
)

func TestWebUIShortcutsModal(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, "<footer") || !strings.Contains(ui, "</footer>") {
		t.Fatal("expected footer element in web/index.html")
	}

	if !strings.Contains(ui, "shortcut-legend") {
		t.Fatal("expected shortcut-legend in footer in web/index.html")
	}

	if !strings.Contains(ui, `<dialog id="shortcuts-modal"`) {
		t.Fatal("expected <dialog id=\"shortcuts-modal\"> in web/index.html")
	}

	for _, want := range []string{
		"Table navigation",
		"Search",
		"Form submit",
		"openShortcutsModal",
		"closeShortcutsModal",
		"setupShortcutsModal",
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("expected %q in web/index.html", want)
		}
	}

	for _, key := range []string{"ArrowUp", "ArrowDown", "Enter", "Space", "Ctrl+Enter"} {
		if !strings.Contains(ui, key) {
			t.Fatalf("expected key %q documented in web/index.html", key)
		}
	}
}

func TestWebUISearchArrowDown(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="filter-search"`) {
		t.Fatal("expected filter-search input in web/index.html")
	}
	if !strings.Contains(ui, "setupSearch") {
		t.Fatal("expected setupSearch in web/index.html")
	}
	if !strings.Contains(ui, "ArrowDown") {
		t.Fatal("expected ArrowDown handling in web/index.html")
	}
}

func TestWebUIDeselectTask(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, `id="task-details-close"`) {
		t.Fatal("expected #task-details-close button in web/index.html")
	}
	if !strings.Contains(ui, `aria-label="Close task details"`) {
		t.Fatal("expected dismiss button with aria-label=\"Close task details\" in web/index.html")
	}
	if !strings.Contains(ui, `onclick="clearSelectedTask()"`) {
		t.Fatal("expected dismiss button to call clearSelectedTask() in web/index.html")
	}
	if !strings.Contains(ui, "e.key === 'Escape'") {
		t.Fatal("expected Escape key handling in web/index.html")
	}
	if !strings.Contains(ui, "Deselect task and clear details") {
		t.Fatal("expected Deselect task shortcut documented in web/index.html")
	}
}
