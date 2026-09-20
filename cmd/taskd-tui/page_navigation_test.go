package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func createNavigationTestModel(totalTasks int, bodyLines int) model {
	m := newModel(config{icons: false, refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	tasks := make([]task, totalTasks)
	body := strings.Repeat("line\n", bodyLines)
	for i := range tasks {
		tasks[i] = task{
			ID:       fmt.Sprintf("task-%d", i),
			Body:     fmt.Sprintf("Task %d\n%s", i, body),
			Status:   "pending",
			Priority: 1,
		}
	}
	m.tasks = tasks
	m.rebuildShown()
	m.cursor = 0
	m.syncDetail()
	return m
}

func TestPageNavigation(t *testing.T) {
	t.Run("modeTable", func(t *testing.T) {
		m := createNavigationTestModel(50, 10)
		tRows := m.tableRows()
		if tRows <= 1 {
			t.Fatalf("tableRows = %d, want > 1", tRows)
		}
		if m.cursor != 0 {
			t.Fatalf("initial cursor = %d, want 0", m.cursor)
		}

		res, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = res.(model)
		if m.cursor != tRows {
			t.Fatalf("cursor after PgDown = %d, want %d", m.cursor, tRows)
		}

		res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = res.(model)
		if m.cursor != 0 {
			t.Fatalf("cursor after PgUp = %d, want 0", m.cursor)
		}
	})

	for _, mde := range []mode{modeDetail, modeZoom} {
		t.Run(fmt.Sprintf("mode_%v", mde), func(t *testing.T) {
			m := createNavigationTestModel(5, 50)
			m.mode = mde
			m.syncDetail()

			vpHeight := m.detail.Height()
			if vpHeight <= 0 {
				t.Fatalf("mode %v: detail viewport height = %d, want > 0", mde, vpHeight)
			}
			if m.detail.YOffset() != 0 {
				t.Fatalf("mode %v: initial YOffset = %d, want 0", mde, m.detail.YOffset())
			}

			res, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
			m = res.(model)
			if m.detail.YOffset() != vpHeight {
				t.Fatalf("mode %v: YOffset after PgDown = %d, want %d", mde, m.detail.YOffset(), vpHeight)
			}

			res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
			m = res.(model)
			if m.detail.YOffset() != 0 {
				t.Fatalf("mode %v: YOffset after PgUp = %d, want 0", mde, m.detail.YOffset())
			}
		})
	}
}

func TestDetailArrowKeys(t *testing.T) {
	for _, mde := range []mode{modeDetail, modeZoom} {
		t.Run(fmt.Sprintf("mode_%v", mde), func(t *testing.T) {
			m := createNavigationTestModel(5, 50)
			m.mode = mde
			m.syncDetail()

			if m.detail.YOffset() != 0 {
				t.Fatalf("mode %v: initial YOffset = %d, want 0", mde, m.detail.YOffset())
			}

			res, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m = res.(model)
			if m.detail.YOffset() != 1 {
				t.Fatalf("mode %v: YOffset after KeyDown = %d, want 1", mde, m.detail.YOffset())
			}

			res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
			m = res.(model)
			if m.detail.YOffset() != 0 {
				t.Fatalf("mode %v: YOffset after KeyUp = %d, want 0", mde, m.detail.YOffset())
			}
		})
	}
}
