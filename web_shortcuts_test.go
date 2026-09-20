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
