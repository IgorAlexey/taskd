package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFooterContextualActions(t *testing.T) {
	hasItem := func(items [][2]string, key, desc string) bool {
		for _, it := range items {
			if it[0] == key && it[1] == desc {
				return true
			}
		}
		return false
	}

	sampleTask := task{
		ID:       1234,
		Project:  "taskd",
		Status:   "pending",
		Priority: 1,
		Body:     "test body",
	}

	t.Run("table mode with selected task includes note", func(t *testing.T) {
		m := newModel(config{icons: false}, nil)
		m.width = 120
		m.height = 24
		m.tasks = []task{sampleTask}
		m.rebuildShown()
		m.cursor = 0
		m.mode = modeTable

		items := m.footerItems()
		if !hasItem(items, "a", "note") {
			t.Fatalf("expected modeTable to contain {'a', 'note'}, got: %+v", items)
		}

		rendered := ansi.Strip(m.View().Content)
		lines := strings.Split(rendered, "\n")
		footerLine := lines[len(lines)-1]
		if !strings.Contains(footerLine, "a note") {
			t.Fatalf("rendered table footer missing 'a note': %q", footerLine)
		}
	})

	modes := []struct {
		name string
		mde  mode
	}{
		{name: "detail", mde: modeDetail},
		{name: "zoom", mde: modeZoom},
	}

	for _, tc := range modes {
		t.Run(tc.name+" mode includes actions and renders navigation at 80 cols", func(t *testing.T) {
			m := newModel(config{icons: false}, nil)
			m.width = 80
			m.height = 24
			m.tasks = []task{sampleTask}
			m.rebuildShown()
			m.cursor = 0
			m.mode = tc.mde

			items := m.footerItems()
			required := [][2]string{
				{"j/k", "scroll"},
				{"Tab", "back"},
				{"z", "zoom"},
				{"c", "claim"},
				{"e", "edit"},
				{"a", "note"},
				{"x", "complete"},
				{"D", "delete"},
			}
			for _, req := range required {
				if !hasItem(items, req[0], req[1]) {
					t.Fatalf("mode %s missing {%q, %q}, got: %+v", tc.name, req[0], req[1], items)
				}
			}

			rendered := ansi.Strip(m.View().Content)
			lines := strings.Split(rendered, "\n")
			footerLine := lines[len(lines)-1]
			if ansi.StringWidth(footerLine) > 80 {
				t.Fatalf("mode %s footer width %d exceeds 80: %q", tc.name, ansi.StringWidth(footerLine), footerLine)
			}
			for _, tok := range []string{"Tab back", "z zoom", "j/k scroll"} {
				if !strings.Contains(footerLine, tok) {
					t.Fatalf("mode %s footer dropped primary navigation %q at 80 cols: %q", tc.name, tok, footerLine)
				}
			}
		})

		t.Run(tc.name+" mode without selection omits mutation keys", func(t *testing.T) {
			m := newModel(config{icons: false}, nil)
			m.width = 80
			m.height = 24
			m.tasks = nil
			m.rebuildShown()
			m.cursor = -1
			m.mode = tc.mde

			items := m.footerItems()
			if !hasItem(items, "Tab", "back") || !hasItem(items, "z", "zoom") {
				t.Fatalf("mode %s without selection missing navigation: %+v", tc.name, items)
			}
			for _, bad := range []string{"edit", "complete", "delete", "claim"} {
				for _, it := range items {
					if it[1] == bad {
						t.Fatalf("mode %s without selection should not contain %q", tc.name, bad)
					}
				}
			}
		})
	}

	lifecycleTests := []struct {
		status    string
		wantItems [][2]string
	}{
		{
			status: "pending",
			wantItems: [][2]string{
				{"c", "claim"},
			},
		},
		{
			status: "leased",
			wantItems: [][2]string{
				{"t", "touch"},
				{"u", "release"},
				{"b", "bury"},
			},
		},
		{
			status: "buried",
			wantItems: [][2]string{
				{"K", "kick"},
			},
		},
	}

	for _, tc := range modes {
		for _, lc := range lifecycleTests {
			t.Run(tc.name+"_"+lc.status+"_lifecycle", func(t *testing.T) {
				tsk := sampleTask
				tsk.Status = lc.status
				m := newModel(config{icons: false}, nil)
				m.width = 80
				m.height = 24
				m.tasks = []task{tsk}
				m.rebuildShown()
				m.cursor = 0
				m.mode = tc.mde

				items := m.footerItems()
				for _, req := range lc.wantItems {
					if !hasItem(items, req[0], req[1]) {
						t.Fatalf("mode %s status %s missing {%q, %q}, got: %+v", tc.name, lc.status, req[0], req[1], items)
					}
				}
			})
		}
	}
}
