package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBulkUnburyAction(t *testing.T) {
	var (
		receivedMethod string
		receivedURL    string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedURL = r.URL.RequestURI()
		if strings.HasPrefix(r.URL.Path, "/tasks/kick") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"kicked":2}`))
			return
		}
		if r.URL.Path == "/tasks" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	for _, tc := range []struct {
		name    string
		mde     mode
		key     tea.KeyPressMsg
		project string
		wantURL string
	}{
		{
			name:    "table_key_B",
			mde:     modeTable,
			key:     tea.KeyPressMsg{Text: "B"},
			wantURL: "/tasks/kick",
		},
		{
			name:    "detail_ctrl_k",
			mde:     modeDetail,
			key:     tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'k'},
			wantURL: "/tasks/kick",
		},
		{
			name:    "scoped_project",
			mde:     modeTable,
			key:     tea.KeyPressMsg{Text: "B"},
			project: "alpha",
			wantURL: "/tasks/kick?project=alpha",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receivedMethod = ""
			receivedURL = ""

			m := newModel(config{
				url:    ts.URL,
				worker: "worker-1",
			}, newClient(ts.URL))
			m.project = tc.project
			m.tasks = []task{
				{ID: "t-1", Project: "alpha", Status: "buried", Body: "buried 1"},
				{ID: "t-2", Project: "beta", Status: "buried", Body: "buried 2"},
			}
			m.rebuildShown()
			m.mode = tc.mde
			m.cursor = 0
			m.syncDetail()

			up, _ := m.Update(tc.key)
			m = up.(model)
			if m.mode != modeConfirm {
				t.Fatalf("mode after key = %v, want modeConfirm", m.mode)
			}
			if m.confirm.method != http.MethodPost || m.confirm.path != tc.wantURL || m.confirm.button != "unbury" {
				t.Fatalf("unexpected confirm modal: %+v", m.confirm)
			}
			if tc.project == "" && m.confirm.text != "Unbury all buried tasks?" {
				t.Fatalf("confirm text = %q, want 'Unbury all buried tasks?'", m.confirm.text)
			}
			if tc.project != "" && (!strings.Contains(m.confirm.text, "Unbury buried tasks in project") || !strings.Contains(m.confirm.text, tc.project)) {
				t.Fatalf("confirm text = %q, want project %q", m.confirm.text, tc.project)
			}

			up, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
			m = up.(model)
			if m.mode != modeTable {
				t.Fatalf("mode after confirm = %v, want modeTable", m.mode)
			}
			if cmd == nil {
				t.Fatal("expected actCmd after confirming unbury")
			}
			actResult := cmd()
			act, ok := actResult.(actMsg)
			if !ok || act.err != nil {
				t.Fatalf("bulk unbury action failed: %+v", actResult)
			}
			if receivedMethod != http.MethodPost || receivedURL != tc.wantURL {
				t.Fatalf("got %s %s, want POST %s", receivedMethod, receivedURL, tc.wantURL)
			}

			mNext, refreshCmd := m.Update(act)
			m = mNext.(model)
			if m.msg != "unburied tasks" {
				t.Fatalf("footer message = %q, want 'unburied tasks'", m.msg)
			}
			if refreshCmd == nil {
				t.Fatal("expected refresh poll command after actMsg")
			}
		})
	}

	t.Run("cancel", func(t *testing.T) {
		m := newModel(config{
			worker: "worker-1",
		}, newClient("http://localhost:8080"))
		m.tasks = []task{
			{ID: "t-1", Project: "alpha", Status: "buried", Body: "buried in alpha"},
		}
		m.rebuildShown()

		up, _ := m.Update(tea.KeyPressMsg{Text: "B"})
		m = up.(model)
		if m.mode != modeConfirm {
			t.Fatalf("mode = %v, want modeConfirm", m.mode)
		}

		up, cmd := m.Update(tea.KeyPressMsg{Text: "n"})
		m = up.(model)
		if m.mode != modeTable {
			t.Fatalf("mode after cancel = %v, want modeTable", m.mode)
		}
		if cmd != nil {
			t.Fatal("expected nil cmd after cancelling bulk unbury")
		}
	})
}
