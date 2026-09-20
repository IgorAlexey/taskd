package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestCLIUsageHelpQueryParameters(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()

	required := []string{
		"buried", "live", "1..1000", "project=*",
		"?status=", "?project=", "?worker=", "?priority=",
		"?limit=", "?offset=", "?after=", "?asset_path=",
		"?q=", "?fields=", "?columns=", "?force=",
	}
	for _, token := range required {
		if !strings.Contains(usage, token) {
			t.Fatalf("usage output missing %q:\n%s", token, usage)
		}
	}
}

func TestWebUIProjectDatalist(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `<datalist id="form-project-list">`) {
		t.Fatal("expected <datalist id=\"form-project-list\"> in web/index.html")
	}
	projInput := regexp.MustCompile(`<input[^>]*id="form-project"[^>]*>`).FindString(ui)
	if projInput == "" {
		t.Fatal("expected #form-project input in web/index.html")
	}
	if !strings.Contains(projInput, `list="form-project-list"`) {
		t.Fatalf("expected list attribute on #form-project, got %q", projInput)
	}
	if !strings.Contains(ui, `document.getElementById('form-project-list')`) {
		t.Fatal("expected script to populate form-project-list")
	}
}
