package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestCtrlCCancelSearchAndModal(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(model) model
		want mode
	}{
		{
			name: "modeSearch",
			open: func(m model) model {
				up, _ := m.Update(tea.KeyPressMsg{Text: "/"})
				m = up.(model)
				up, _ = m.Update(tea.KeyPressMsg{Text: "a"})
				return up.(model)
			},
			want: modeSearch,
		},
		{
			name: "modeConfirm",
			open: func(m model) model {
				up, _ := m.Update(tea.KeyPressMsg{Text: "D"})
				return up.(model)
			},
			want: modeConfirm,
		},
		{
			name: "modeHelp",
			open: func(m model) model {
				up, _ := m.Update(tea.KeyPressMsg{Text: "?"})
				return up.(model)
			},
			want: modeHelp,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(config{icons: true, refresh: time.Hour}, nil)
			m.width = 80
			m.height = 24
			m.tasks = []task{
				{ID: "task-0", Project: "test", Status: "pending", Body: "zero\nzero detail"},
			}
			m.rebuildShown()
			m.syncDetail()

			m = tc.open(m)
			if m.mode != tc.want {
				t.Fatalf("expected mode %v, got %v", tc.want, m.mode)
			}

			ctrlC := tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'c'}
			up, cmd := m.Update(ctrlC)
			m = up.(model)
			if m.mode != modeTable {
				t.Fatalf("expected modeTable after Ctrl+C, got %v", m.mode)
			}
			if cmd != nil {
				if _, isQuit := cmd().(tea.QuitMsg); isQuit {
					t.Fatalf("expected not to quit on Ctrl+C in %s", tc.name)
				}
			}
		})
	}

	t.Run("modeHelpFromDetail", func(t *testing.T) {
		m := newModel(config{icons: true, refresh: time.Hour}, nil)
		m.width = 80
		m.height = 24
		m.mode = modeDetail
		updated, _ := m.Update(tea.KeyPressMsg{Text: "?"})
		m = updated.(model)
		if m.mode != modeHelp {
			t.Fatalf("expected modeHelp, got %v", m.mode)
		}
		ctrlC := tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'c'}
		updated, cmd := m.Update(ctrlC)
		m = updated.(model)
		if m.mode != modeDetail {
			t.Fatalf("expected modeDetail after Ctrl+C from help, got %v", m.mode)
		}
		if cmd != nil {
			if _, isQuit := cmd().(tea.QuitMsg); isQuit {
				t.Fatalf("expected not to quit on Ctrl+C in help")
			}
		}
	})

	for _, tc := range []struct {
		name string
		mde  mode
	}{
		{name: "modeTable", mde: modeTable},
		{name: "modeDetail", mde: modeDetail},
		{name: "modeZoom", mde: modeZoom},
	} {
		t.Run("quit_"+tc.name, func(t *testing.T) {
			m := newModel(config{icons: true, refresh: time.Hour}, nil)
			m.mode = tc.mde
			ctrlC := tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'c'}
			_, cmd := m.Update(ctrlC)
			if cmd == nil {
				t.Fatalf("expected quit cmd on Ctrl+C in %s, got nil", tc.name)
			}
			if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
				t.Fatalf("expected QuitMsg on Ctrl+C in %s, got %T", tc.name, cmd())
			}
		})
	}
}
