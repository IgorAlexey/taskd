package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestModalMouseIsolation(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(model) model
		want mode
	}{
		{
			name: "modeForm",
			open: func(m model) model {
				up, _ := m.Update(tea.KeyPressMsg{Text: "n"})
				return up.(model)
			},
			want: modeForm,
		},
		{
			name: "modeConfirm",
			open: func(m model) model {
				up, _ := m.Update(tea.KeyPressMsg{Text: "D"})
				return up.(model)
			},
			want: modeConfirm,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(config{icons: true, refresh: time.Hour}, nil)
			m.width = 80
			m.height = 24
			m.tasks = []task{
				{ID: "task-0", Project: "test", Status: "pending", Body: "zero\nzero detail"},
				{ID: "task-1", Project: "test", Status: "pending", Body: "one\none detail"},
				{ID: "task-2", Project: "test", Status: "pending", Body: "two\ntwo detail"},
			}
			m.rebuildShown()
			m.syncDetail()

			m = tc.open(m)
			if m.mode != tc.want {
				t.Fatalf("expected mode %v, got %v", tc.want, m.mode)
			}

			bandTop := headerRows + tabRows + 1 + colHeadRows
			clickRow1 := tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: bandTop + 1}
			up, _ := m.Update(clickRow1)
			m = up.(model)
			if m.mode != tc.want {
				t.Fatalf("mode after click = %v, want %v", m.mode, tc.want)
			}
			if m.cursor != 0 {
				t.Fatalf("cursor after click = %d, want 0", m.cursor)
			}
			if m.detailID != "task-0" {
				t.Fatalf("detailID after click = %q, want task-0", m.detailID)
			}

			wheelDown := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 10, Y: bandTop + 1}
			up, _ = m.Update(wheelDown)
			m = up.(model)
			if m.mode != tc.want {
				t.Fatalf("mode after wheel = %v, want %v", m.mode, tc.want)
			}
			if m.cursor != 0 {
				t.Fatalf("cursor after wheel = %d, want 0", m.cursor)
			}
			if m.offset != 0 {
				t.Fatalf("offset after wheel = %d, want 0", m.offset)
			}
			if m.detailID != "task-0" {
				t.Fatalf("detailID after wheel = %q, want task-0", m.detailID)
			}
		})
	}
}
