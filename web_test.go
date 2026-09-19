package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestWebUIFormLabels(t *testing.T) {
	ui := string(uiHTML)

	for _, id := range []string{
		"auto-refresh",
		"filter-project",
		"filter-status",
		"form-project",
		"form-priority",
		"form-body",
		"form-asset",
		"form-id",
	} {
		if !strings.Contains(ui, `id="`+id+`"`) {
			t.Fatalf("expected control with id=%q in web/index.html", id)
		}
		if !strings.Contains(ui, `for="`+id+`"`) {
			t.Fatalf("expected a label with for=%q in web/index.html", id)
		}
	}

	if strings.Contains(ui, "<label>") {
		t.Fatal("found orphan <label> without attributes in web/index.html")
	}

	labels := regexp.MustCompile(`<label[^>]*>`).FindAllString(ui, -1)
	if len(labels) == 0 {
		t.Fatal("expected labels in web/index.html")
	}
	seen := map[string]bool{}
	forAttr := regexp.MustCompile(`for="([^"]*)"`)
	for _, l := range labels {
		m := forAttr.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("label %q has no for attribute", l)
		}
		if seen[m[1]] {
			t.Fatalf("control %q is targeted by more than one label", m[1])
		}
		seen[m[1]] = true
	}

	req := regexp.MustCompile(`<input[^>]*id="form-project"[^>]*>`).FindString(ui)
	if req == "" {
		t.Fatal("expected #form-project input in web/index.html")
	}
	if !strings.Contains(req, `required`) || !strings.Contains(req, `aria-required="true"`) {
		t.Fatalf("expected required and aria-required=\"true\" on #form-project, got %q", req)
	}
}

func TestWebUISkipLinkAndLandmarks(t *testing.T) {
	ui := string(uiHTML)

	body := regexp.MustCompile(`<body[^>]*>`).FindStringIndex(ui)
	if body == nil {
		t.Fatal("no <body> in web/index.html")
	}
	first := regexp.MustCompile(`<[a-zA-Z][^>]*>`).FindString(ui[body[1]:])
	if !strings.Contains(first, `class="skip-link"`) {
		t.Fatalf("expected the skip link first in <body>, got %q", first)
	}
	if !strings.Contains(first, `href="#queue"`) {
		t.Fatalf("expected skip link to target #queue, got %q", first)
	}

	hidden := regexp.MustCompile(`\.skip-link\s*\{([^}]*)\}`).FindStringSubmatch(ui)
	if hidden == nil || !strings.Contains(hidden[1], "clip-path: inset(50%)") {
		t.Fatal("expected .skip-link to be clipped out of view until focused")
	}
	shown := regexp.MustCompile(`\.skip-link:focus\s*\{([^}]*)\}`).FindStringSubmatch(ui)
	if shown == nil || !strings.Contains(shown[1], "clip-path: none") {
		t.Fatal("expected .skip-link:focus to unclip the link")
	}

	target := regexp.MustCompile(`<[a-zA-Z]+[^>]*id="queue"[^>]*>`).FindStringIndex(ui)
	if target == nil {
		t.Fatal("expected an element with id=\"queue\" in web/index.html")
	}
	if !strings.Contains(ui[target[0]:target[1]], `tabindex="-1"`) {
		t.Fatalf("expected tabindex=\"-1\" on the skip link target, got %q",
			ui[target[0]:target[1]])
	}
	if i := strings.Index(ui[target[1]:], "<table>"); i < 0 || i > 40 {
		t.Fatal("expected the skip link to land past the filters, on the table")
	}
	if strings.Contains(ui[target[1]:], `class="filter-bar"`) {
		t.Fatal("expected the filter bar before the skip link target, not after")
	}

	main := regexp.MustCompile(`<main[^>]*>`).FindStringIndex(ui)
	if main == nil || main[0] > target[0] || main[0] < body[1] {
		t.Fatal("expected a <main> landmark in <body> wrapping the queue")
	}

	sections := regexp.MustCompile(`<section[^>]*>`).FindAllString(ui, -1)
	if len(sections) < 3 {
		t.Fatalf("expected the queue, details and submit regions, got %d", len(sections))
	}
	labelledBy := regexp.MustCompile(`aria-labelledby="([^"]+)"`)
	for _, sec := range sections {
		m := labelledBy.FindStringSubmatch(sec)
		if m == nil {
			t.Fatalf("section %q has no aria-labelledby", sec)
		}
		name := regexp.MustCompile(`<h2[^>]*id="` + m[1] + `"[^>]*>([^<]*)</h2>`).
			FindStringSubmatch(ui)
		if name == nil {
			t.Fatalf("no <h2> carries id %q", m[1])
		}
		if strings.TrimSpace(name[1]) == "" {
			t.Fatalf("heading %q is empty", m[1])
		}
	}
	if regexp.MustCompile(`(?s)<h2[^>]*>[^<]*<span`).MatchString(ui) {
		t.Fatal("expected no live counters nested inside a heading")
	}

	group := regexp.MustCompile(`<[a-zA-Z]+[^>]*role="group"[^>]*>`).FindString(ui)
	if group == "" || !strings.Contains(group, `aria-label="Task filters"`) {
		t.Fatalf("expected a labelled role=\"group\" filter region, got %q", group)
	}

	if regexp.MustCompile(`aria-label(?:ledby)?="\s*"`).MatchString(ui) {
		t.Fatal("found an empty accessible name in web/index.html")
	}
}
