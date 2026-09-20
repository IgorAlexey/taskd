package main

import (
	"regexp"
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

func TestWebUIStatusFilterShortcuts(t *testing.T) {
	ui := string(uiHTML)
	generalIdx := strings.Index(ui, "<h3>General</h3>")
	if generalIdx == -1 {
		t.Fatal("expected General section in shortcuts modal")
	}
	generalSection := ui[generalIdx:]
	if !strings.Contains(generalSection, "0-5") {
		t.Fatal("expected 0-5 listed under general shortcuts")
	}

	if !strings.Contains(ui, "STATUS_KEYS[e.key]") {
		t.Fatal("expected STATUS_KEYS[e.key] status lookup in setupGlobalShortcuts")
	}
	if !strings.Contains(ui, "filterByStatus(STATUS_KEYS[e.key])") {
		t.Fatal("expected filterByStatus dispatch for 0-5 status keys")
	}
	if !strings.Contains(ui, "e.key >= '0' && e.key <= '5'") {
		t.Fatal("expected range check for keys 0-5")
	}
	if !strings.Contains(ui, "isInputTarget(e.target)") {
		t.Fatal("expected input target guard in setupGlobalShortcuts")
	}
	if !strings.Contains(ui, "!e.ctrlKey && !e.metaKey && !e.altKey") {
		t.Fatal("expected modifier key guard for status filter shortcuts")
	}
}

func TestWebUIFooterShortcutButtons(t *testing.T) {
	ui := string(uiHTML)

	legendStart := strings.Index(ui, `class="shortcut-legend`)
	if legendStart == -1 {
		t.Fatal("expected .shortcut-legend in web/index.html")
	}
	legendEnd := strings.Index(ui[legendStart:], "</div>")
	if legendEnd == -1 {
		t.Fatal("expected closing div for .shortcut-legend")
	}
	legend := ui[legendStart : legendStart+legendEnd]

	matches := regexp.MustCompile(`<button\b[^>]*>.*?</button>`).FindAllString(legend, -1)
	var hasSearchHandler, hasRefreshHandler bool
	for _, btn := range matches {
		if strings.Contains(btn, `class="shortcut-btn"`) && strings.Contains(btn, "<kbd>/</kbd>") && strings.Contains(btn, "Search") {
			if strings.Contains(btn, "onclick=") {
				hasSearchHandler = true
			}
		}
		if strings.Contains(btn, `class="shortcut-btn"`) && strings.Contains(btn, "<kbd>r</kbd>") && strings.Contains(btn, "Refresh") {
			if strings.Contains(btn, "onclick=") {
				hasRefreshHandler = true
			}
		}
	}
	if !hasSearchHandler {
		t.Error("expected Search shortcut button in footer legend to have an onclick handler")
	}
	if !hasRefreshHandler {
		t.Error("expected Refresh shortcut button in footer legend to have an onclick handler")
	}
}
func TestWebUITaskActionShortcuts(t *testing.T) {
	ui := string(uiHTML)

	modalIdx := strings.Index(ui, `<dialog id="shortcuts-modal"`)
	if modalIdx == -1 {
		t.Fatal("expected shortcuts modal in web/index.html")
	}
	modal := ui[modalIdx:]
	if !strings.Contains(modal, "<h3>Task actions</h3>") {
		t.Fatal("expected Task actions section in shortcuts modal")
	}
	for _, want := range []string{
		"<kbd>e</kbd> <span>Toggle edit mode</span>",
		"<kbd>c</kbd> <span>Claim task</span>",
		"<kbd>a</kbd> <span>Focus note input</span>",
		"<kbd>x</kbd> <span>Complete task (when leased)</span>",
	} {
		if !strings.Contains(modal, want) {
			t.Fatalf("expected shortcut %q documented in modal", want)
		}
	}

	for _, check := range []string{
		"!e.ctrlKey && !e.metaKey && !e.altKey",
		"selectedTaskId",
		"toggleTaskEdit()",
		"claimTask(selectedTaskId)",
		"document.getElementById('note-text')",
		"completeTask(currentTask.id, currentTask.worker)",
		"isActivelyLeased(currentTask)",
	} {
		if !strings.Contains(ui, check) {
			t.Fatalf("expected %q in web/index.html global shortcuts handling", check)
		}
	}
}
