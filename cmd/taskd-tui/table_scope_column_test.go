package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTitleOf(t *testing.T) {
	cases := []struct {
		body      string
		project   string
		wantScope string
		wantTitle string
	}{
		{
			body:      "taskd-tui: example title\nbody text",
			project:   "taskd",
			wantScope: "taskd-tui",
			wantTitle: "example title",
		},
		{
			body:      "taskd: fix something",
			project:   "taskd",
			wantScope: "taskd",
			wantTitle: "fix something",
		},
		{
			body:      "taskd/web: fix layout",
			project:   "taskd",
			wantScope: "taskd/web",
			wantTitle: "fix layout",
		},
		{
			body:      "[auth] handle login",
			project:   "taskd",
			wantScope: "auth",
			wantTitle: "handle login",
		},
		{
			body:      "https://example.com/foo",
			project:   "taskd",
			wantScope: "",
			wantTitle: "https://example.com/foo",
		},
		{
			body:      "TODO: fix something",
			project:   "taskd",
			wantScope: "",
			wantTitle: "TODO: fix something",
		},
		{
			body:      "fix: broken button",
			project:   "taskd",
			wantScope: "",
			wantTitle: "fix: broken button",
		},
		{
			body:      "perf: optimize render",
			project:   "taskd",
			wantScope: "",
			wantTitle: "perf: optimize render",
		},
		{
			body:      "no prefix title",
			project:   "taskd",
			wantScope: "",
			wantTitle: "no prefix title",
		},
	}
	for _, tc := range cases {
		taskItem := task{Body: tc.body, Project: tc.project}
		gotScope, gotTitle := titleOf(taskItem)
		if gotScope != tc.wantScope || gotTitle != tc.wantTitle {
			t.Fatalf("titleOf(%q, %q) = (%q, %q), want (%q, %q)",
				tc.body, tc.project, gotScope, gotTitle, tc.wantScope, tc.wantTitle)
		}
	}
}

func TestDisplayScope(t *testing.T) {
	tWithScope := task{
		Project: "taskd",
		Body:    "taskd-tui: example title",
	}
	tWithoutScope := task{
		Project: "taskd",
		Body:    "example title",
	}

	mFilter := model{project: "taskd"}
	if sc, _ := mFilter.displayScope(tWithScope); sc != "taskd-tui" {
		t.Fatalf("displayScope with project filter = %q, want %q", sc, "taskd-tui")
	}
	if sc, _ := mFilter.displayScope(tWithoutScope); sc != "" {
		t.Fatalf("displayScope without scope with project filter = %q, want empty", sc)
	}

	mAll := model{project: ""}
	if sc, _ := mAll.displayScope(tWithScope); sc != "taskd-tui" {
		t.Fatalf("displayScope all projects = %q, want %q", sc, "taskd-tui")
	}
	if sc, _ := mAll.displayScope(tWithoutScope); sc != "taskd" {
		t.Fatalf("displayScope without scope all projects fallback = %q, want %q", sc, "taskd")
	}
}

func TestTableScopeColumn(t *testing.T) {
	m := newModel(config{
		project: "taskd",
	}, nil)
	m.width = 100
	m.height = 24
	m.tasks = []task{
		{
			ID:       "task-123",
			Project:  "taskd",
			Status:   "pending",
			Priority: 1,
			Body:     "taskd-tui: example title",
		},
	}
	m.rebuildShown()

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "taskd-tui") {
		t.Fatalf("expected view to contain taskd-tui in scope column, got:\n%s", view)
	}
	if strings.Contains(view, "taskd taskd-tui") {
		t.Fatalf("expected view not to repeat project before scope, got:\n%s", view)
	}
}
