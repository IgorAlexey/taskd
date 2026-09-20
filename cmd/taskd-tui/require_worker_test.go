package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTUIRequireWorkerConsistent(t *testing.T) {
	const wantMsg = "worker required; set via -worker flag or TASKD_WORKER"

	cases := []struct {
		key  string
		name string
	}{
		{key: "u", name: "release"},
		{key: "b", name: "bury"},
		{key: "x", name: "complete"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(config{}, newClient("http://localhost:8080"))
			m.now = time.Now()
			m.tasks = []task{{
				ID:           1,
				Status:       "leased",
				Worker:       "worker-held",
				LeaseExpires: m.now.Unix() + 300,
			}}
			m.rebuildShown()

			up, _ := m.Update(tea.KeyPressMsg{Text: tc.key})
			updated := up.(model)
			if updated.mode == modeConfirm {
				t.Fatalf("expected key %q without worker not to open confirmation modal", tc.key)
			}
			if updated.msg != wantMsg {
				t.Fatalf("key %q message = %q, want %q", tc.key, updated.msg, wantMsg)
			}
		})
	}
	t.Run("UnleasedTaskReportsNotLeased", func(t *testing.T) {
		for _, key := range []string{"u", "b"} {
			m := newModel(config{}, newClient("http://localhost:8080"))
			m.tasks = []task{{
				ID:     1,
				Status: "pending",
			}}
			m.rebuildShown()

			up, _ := m.Update(tea.KeyPressMsg{Text: key})
			updated := up.(model)
			if updated.msg != "task is not leased" {
				t.Fatalf("key %q on pending task message = %q, want %q", key, updated.msg, "task is not leased")
			}
		}
	})
}
