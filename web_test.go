package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
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

const (
	pureHelpersBegin = "// --- begin pure url-state helpers ---"
	pureHelpersEnd   = "// --- end pure url-state helpers ---"
)

// TestURLStateHelpers runs the pure helpers out of web/index.html under node.
func TestURLStateHelpers(t *testing.T) {
	ui := string(uiHTML)
	start := strings.Index(ui, pureHelpersBegin)
	end := strings.Index(ui, pureHelpersEnd)
	if start < 0 || end < start {
		t.Fatalf("expected the pure url-state helpers in web/index.html to be delimited by %q and %q", pureHelpersBegin, pureHelpersEnd)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found in PATH; skipping execution of the pure url-state helpers")
	}

	type call struct {
		Fn  string `json:"fn"`
		Arg any    `json:"arg"`
	}
	st := func(project, status, task string) map[string]string {
		return map[string]string{"project": project, "status": status, "task": task}
	}
	cases := []struct {
		call call
		want any
	}{
		{call{"searchToState", ""}, st("", "", "")},
		{call{"searchToState", "?project=a&status=bogus&task=t1"}, st("a", "", "t1")},
		{call{"searchToState", "?status=done"}, st("", "done", "")},
		{call{"searchToState", "?task=t9"}, st("", "", "t9")},
		{call{"searchToState", "?project=a%20b&task=x%26y"}, st("a b", "", "x&y")},
		{call{"roundTrip", st("a b", "done", "x&y")}, st("a b", "done", "x&y")},
		{call{"stateToSearch", map[string]string{}}, ""},
		{call{"stateToSearch", st("", "", "")}, ""},
		{call{"stateToSearch", st("", "", "t9")}, "?task=t9"},
		{call{"stateToSearch", json.RawMessage(`{"task":"t","status":"done","project":"p"}`)}, "?project=p&status=done&task=t"},
	}

	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshalling %v for the node harness: %v", v, err)
		}
		return string(b)
	}
	calls := make([]call, len(cases))
	for i, c := range cases {
		calls[i] = c.call
	}
	script := ui[start:end+len(pureHelpersEnd)] + `
const CASES = ` + enc(calls) + `;
const FNS = {
  searchToState,
  stateToSearch,
  roundTrip: (s) => searchToState(stateToSearch(s)),
};
console.log(JSON.stringify(CASES.map((c) => FNS[c.fn](c.arg))));
`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the pure url-state helpers under node: %v\n%s", err, stderr.String())
	}

	var got []json.RawMessage
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("decoding the node harness output %q: %v", stdout.String(), err)
	}
	if len(got) != len(cases) {
		t.Fatalf("node returned %d results, want %d", len(got), len(cases))
	}
	for i, c := range cases {
		var v any
		if err := json.Unmarshal(got[i], &v); err != nil {
			t.Fatalf("decoding result %d (%s): %v", i, got[i], err)
		}
		if have, want := enc(v), enc(c.want); have != want {
			t.Fatalf("%s(%s) = %s, want %s", c.call.Fn, enc(c.call.Arg), have, want)
		}
	}
}

// jsFunctionBody returns the source of the named function in ui, braces matched.
// The counter is a heuristic: it does not know about strings, comments or
// template literals, so only call it for bodies kept free of literal braces.
func jsFunctionBody(t *testing.T, ui, name string) string {
	t.Helper()
	at := strings.Index(ui, "function "+name+"(")
	if at < 0 {
		t.Fatalf("could not find the body of %s() in web/index.html", name)
	}
	src := ui[at:]
	depth := 0
	for i := strings.Index(src, "{"); i >= 0 && i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return src[:i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces in the body of %s() in web/index.html", name)
	return ""
}

// TestWebUIURLStateWiring asserts what cannot be executed standalone.
func TestWebUIURLStateWiring(t *testing.T) {
	ui := string(uiHTML)

	for _, want := range []string{
		"URLSearchParams",
		"history.pushState(",
		"history.replaceState(",
		"window.addEventListener('popstate'",
	} {
		if !strings.Contains(ui, want) {
			t.Fatalf("expected %q in web/index.html to keep URL state in sync", want)
		}
	}

	for _, id := range []string{"filter-project", "filter-status"} {
		sel := regexp.MustCompile(`<select[^>]*id="` + id + `"[^>]*>`).FindString(ui)
		if sel == "" {
			t.Fatalf("expected #%s select in web/index.html", id)
		}
		if !strings.Contains(sel, `onchange="onFilterChange()"`) {
			t.Fatalf("expected onchange=\"onFilterChange()\" on #%s, got %q", id, sel)
		}
	}
	if bad := regexp.MustCompile(`<select[^>]*onchange="loadTasks\(\)"`).FindString(ui); bad != "" {
		t.Fatalf("filter select still bypasses the URL state: %q", bad)
	}

	if strings.Contains(ui, "selectedTaskId") {
		t.Fatal("expected the selectedTaskId global to be gone from web/index.html; uiState.task is the only selection state")
	}

	tasks := jsFunctionBody(t, ui, "loadTasks")
	if regexp.MustCompile(`uiState\.task\s*=[^=]`).MatchString(tasks) {
		t.Fatal("expected loadTasks() to never write uiState.task in web/index.html; the URL owns the selection")
	}
	if strings.Contains(tasks, "loadTaskDetails") {
		t.Fatal("expected loadTasks() to never call loadTaskDetails() in web/index.html")
	}
	if !regexp.MustCompile(`setURLState\([^;]*,\s*true\s*\)`).MatchString(jsFunctionBody(t, ui, "selectTask")) {
		t.Fatal("expected selectTask() to call setURLState(next, true) so selecting a task pushes history")
	}
	if !regexp.MustCompile(`setURLState\([^;]*,\s*false\s*\)`).MatchString(jsFunctionBody(t, ui, "onFilterChange")) {
		t.Fatal("expected onFilterChange() to call setURLState(next, false) so filtering replaces history")
	}
	details := jsFunctionBody(t, ui, "loadTaskDetails")
	if !strings.Contains(details, "notFoundOk") {
		t.Fatal("expected loadTaskDetails() to fetch with { notFoundOk: true } in web/index.html")
	}
	if !strings.Contains(details, "!== uiState.task") {
		t.Fatal("expected loadTaskDetails() to drop a response for a task that is no longer selected")
	}
}
