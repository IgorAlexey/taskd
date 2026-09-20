package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestPriorityColumnMultiDigit(t *testing.T) {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	m.tasks = []task{
		{
			ID:       1,
			Priority: 10,
			Status:   "pending",
			Project:  "test",
			Body:     "task alpha",
		},
		{
			ID:       2,
			Priority: 25,
			Status:   "pending",
			Project:  "test",
			Body:     "task beta",
		},
		{
			ID:       3,
			Priority: 3,
			Status:   "pending",
			Project:  "test",
			Body:     "task gamma",
		},
	}
	m.rebuildShown()

	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")

	for i, l := range lines {
		if strings.Contains(l, "-- #") {
			lines = lines[:i]
			break
		}
	}

	var tableLines []string
	for _, l := range lines {
		if strings.Contains(l, "task alpha") || strings.Contains(l, "task beta") || strings.Contains(l, "task gamma") {
			tableLines = append(tableLines, l)
		}
	}

	if len(tableLines) != 3 {
		t.Fatalf("expected 3 table rows, got %d:\n%s", len(tableLines), view)
	}

	var found10, found25, found3 bool
	for _, l := range tableLines {
		if strings.Contains(l, "10") {
			found10 = true
		}
		if strings.Contains(l, "25") {
			found25 = true
		}
		if strings.Contains(l, " 3 ") {
			found3 = true
		}
	}

	if !found10 {
		t.Fatalf("expected queue table row to contain full priority 10, got:\n%s", strings.Join(tableLines, "\n"))
	}
	if !found25 {
		t.Fatalf("expected queue table row to contain full priority 25, got:\n%s", strings.Join(tableLines, "\n"))
	}
	if !found3 {
		t.Fatalf("expected queue table row to right-justify single-digit priority, got:\n%s", strings.Join(tableLines, "\n"))
	}
}
