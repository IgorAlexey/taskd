package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestCreatedAge(t *testing.T) {
	now := time.Unix(1700000000, 0)

	cases := []struct {
		name      string
		createdAt int64
		now       time.Time
		want      string
	}{
		{name: "zero created_at", createdAt: 0, now: now, want: ""},
		{name: "negative created_at", createdAt: -10, now: now, want: ""},
		{name: "zero now", createdAt: now.Unix(), now: time.Time{}, want: ""},
		{name: "future created_at", createdAt: now.Unix() + 10, now: now, want: "0s"},
		{name: "seconds", createdAt: now.Unix() - 30, now: now, want: "30s"},
		{name: "minutes", createdAt: now.Unix() - 120, now: now, want: "2m"},
		{name: "hours", createdAt: now.Unix() - 7200, now: now, want: "2h"},
		{name: "days", createdAt: now.Unix() - 86400*3, now: now, want: "3d"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := createdAge(tc.createdAt, tc.now)
			if got != tc.want {
				t.Fatalf("createdAge(%d, %v) = %q, want %q", tc.createdAt, tc.now, got, tc.want)
			}
		})
	}
}

func TestDetailPaneCreatedAt(t *testing.T) {
	now := time.Unix(1700000000, 0)

	t.Run("renders age in chips line", func(t *testing.T) {
		sampleTask := task{
			ID:        "task123456",
			Project:   "taskd",
			Priority:  1,
			CreatedAt: now.Unix() - 120,
			Body:      "sample task title\nsample task body line",
		}

		for _, mde := range []mode{modeTable, modeZoom} {
			m := newModel(config{icons: false}, nil)
			m.width = 80
			m.height = 24
			m.now = now
			m.mode = mde
			m.tasks = []task{sampleTask}
			m.rebuildShown()
			m.cursor = 0
			m.syncDetail()

			rendered := ansi.Strip(m.View().Content)
			lines := strings.Split(rendered, "\n")
			panes := m.panes()

			if panes.detailRows < 3 {
				t.Fatalf("mode %v: detailRows=%d, want >= 3", mde, panes.detailRows)
			}

			chipsLine := lines[panes.detailTop+2]
			if !strings.Contains(chipsLine, "/ taskd") {
				t.Fatalf("mode %v: chips line missing project: %q", mde, chipsLine)
			}
			if !strings.Contains(chipsLine, "p 1") {
				t.Fatalf("mode %v: chips line missing priority: %q", mde, chipsLine)
			}
			if !strings.Contains(chipsLine, "2m") {
				t.Fatalf("mode %v: chips line missing creation age: %q", mde, chipsLine)
			}
		}
	})

	t.Run("omits creation age when created_at is zero", func(t *testing.T) {
		sampleTask := task{
			ID:        "task123456",
			Project:   "taskd",
			Priority:  1,
			CreatedAt: 0,
			Body:      "sample task title\nsample task body line",
		}

		m := newModel(config{icons: false}, nil)
		m.width = 80
		m.height = 24
		m.now = now
		m.mode = modeTable
		m.tasks = []task{sampleTask}
		m.rebuildShown()
		m.cursor = 0
		m.syncDetail()

		rendered := ansi.Strip(m.View().Content)
		lines := strings.Split(rendered, "\n")
		panes := m.panes()
		chipsLine := lines[panes.detailTop+2]

		if strings.Contains(chipsLine, "2m") {
			t.Fatalf("expected no creation age in chips line, got: %q", chipsLine)
		}
	})
}
