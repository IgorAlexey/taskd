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
			ID:       "id-a",
			Priority: 10,
			Status:   "pending",
			Project:  "test",
			Body:     "task alpha",
		},
		{
			ID:       "id-b",
			Priority: 25,
			Status:   "pending",
			Project:  "test",
			Body:     "task beta",
		},
		{
			ID:       "id-c",
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

func TestHeaderAlignmentAcrossSortModes(t *testing.T) {
	for _, sortMode := range []sortColumn{sortPriority, sortStatus, sortProject} {
		m := newModel(config{icons: false, refresh: time.Hour}, nil)
		m.width = 100
		m.height = 24
		m.sortCol = sortMode
		m.tasks = []task{
			{
				ID:       "id-a",
				Priority: 10,
				Status:   "pending",
				Project:  "test",
				Body:     "task alpha",
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

		var colHeadLine, rowLine string
		for _, l := range lines {
			if strings.Contains(l, "scope") && strings.Contains(l, "title") {
				colHeadLine = l
			}
			if strings.Contains(l, "task alpha") {
				rowLine = l
			}
		}

		if colHeadLine == "" || rowLine == "" {
			t.Fatalf("mode %v: could not find header or row:\n%s", sortMode, view)
		}

		headerScopeIdx := strings.Index(colHeadLine, "scope")
		rowScopeIdx := strings.Index(rowLine, "test")
		if headerScopeIdx != rowScopeIdx {
			t.Fatalf("mode %v alignment mismatch: header 'scope' at col %d, data row 'test' at col %d\nheader: %q\nrow:    %q",
				sortMode, headerScopeIdx, rowScopeIdx, colHeadLine, rowLine)
		}
	}
}

func TestHeaderRendersActiveWorker(t *testing.T) {
	t.Run("FitAt80Columns", func(t *testing.T) {
		m := newModel(config{
			url:     "http://localhost:8080",
			worker:  "worker-1",
			refresh: 2 * time.Second,
		}, nil)
		m.width = 80
		m.height = 24
		m.connected = true
		m.stats.DB = "taskd.db"

		content := ansi.Strip(m.View().Content)
		lines := strings.Split(content, "\n")
		if len(lines) == 0 {
			t.Fatal("expected rendered view lines, got none")
		}
		headerLine := lines[0]
		if !strings.Contains(headerLine, "as: worker-1") {
			t.Fatalf("expected header line to contain %q, got: %q", "as: worker-1", headerLine)
		}
		if !strings.Contains(headerLine, "taskd.db") {
			t.Fatalf("expected header line to retain database info at width 80, got: %q", headerLine)
		}
		if !strings.Contains(headerLine, "every 2s") {
			t.Fatalf("expected header line to retain refresh interval at width 80, got: %q", headerLine)
		}
		if ansi.StringWidth(headerLine) > 80 {
			t.Fatalf("header line width %d exceeds 80 columns: %q", ansi.StringWidth(headerLine), headerLine)
		}
	})

	t.Run("TruncatesLongWorkerAt80Columns", func(t *testing.T) {
		m := newModel(config{
			url:     "http://localhost:8080",
			worker:  "worker-very-long-hostname-and-path",
			refresh: 2 * time.Second,
		}, nil)
		m.width = 80
		m.height = 24
		m.connected = true
		m.stats.DB = "taskd.db"

		content := ansi.Strip(m.View().Content)
		lines := strings.Split(content, "\n")
		headerLine := lines[0]
		if !strings.Contains(headerLine, "as: work") || !strings.Contains(headerLine, m.glyph.ellipsis) {
			t.Fatalf("expected truncated worker with ellipsis in header line, got: %q", headerLine)
		}
		if !strings.Contains(headerLine, "taskd.db") {
			t.Fatalf("expected header line to retain database info at width 80, got: %q", headerLine)
		}
		if ansi.StringWidth(headerLine) > 80 {
			t.Fatalf("header line width %d exceeds 80 columns: %q", ansi.StringWidth(headerLine), headerLine)
		}
	})
}
