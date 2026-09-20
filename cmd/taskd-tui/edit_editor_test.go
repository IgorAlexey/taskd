package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestEditBodyInEditor(t *testing.T) {
	t.Run("SuccessModifiesAndPatches", func(t *testing.T) {
		var gotPatch map[string]any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PATCH" && r.URL.Path == "/tasks/task-editor-1" {
				json.NewDecoder(r.Body).Decode(&gotPatch)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"id":"task-editor-1","version":5,"status":"pending","body":"new content"}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer ts.Close()

		t.Setenv("VISUAL", "echo \"new content\" >")
		t.Setenv("EDITOR", "")

		m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))
		m.tasks = []task{{
			ID:      "task-editor-1",
			Status:  "pending",
			Version: 4,
			Body:    "old content",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, cmd := m.Update(tea.KeyPressMsg{Text: "E"})
		m = up.(model)
		if cmd == nil {
			t.Fatal("expected non-nil cmd from actionEditInEditor")
		}

		tmpFile, err := os.CreateTemp("", "taskd-test-draft-*.md")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.WriteString("new content")
		tmpFile.Close()

		patchCmd := editorPatchCmd(m.client, "task-editor-1", 4, "new content", tmpPath)
		resMsg := patchCmd()
		act, ok := resMsg.(actMsg)
		if !ok || act.err != nil || act.msg != "task body updated" {
			t.Fatalf("unexpected patch result: %#v", resMsg)
		}

		if gotPatch == nil {
			t.Fatal("expected PATCH request to be sent")
		}
		if gotPatch["if_version"] != float64(4) {
			t.Errorf("expected if_version = 4, got %v", gotPatch["if_version"])
		}
		if strings.TrimSpace(gotPatch["body"].(string)) != "new content" {
			t.Errorf("expected body 'new content', got %v", gotPatch["body"])
		}

		if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
			t.Errorf("expected temp file to be removed after successful patch, but it still exists")
		}
	})

	t.Run("ZeroVersionOmitsIfVersion", func(t *testing.T) {
		var gotPatch map[string]any
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotPatch)
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		tmpFile, err := os.CreateTemp("", "taskd-draft-*.md")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmpFile.Name()
		tmpFile.WriteString("content")
		tmpFile.Close()

		patchCmd := editorPatchCmd(newClient(ts.URL), "task-zero-ver", 0, "content", tmpPath)
		resMsg := patchCmd()
		act, ok := resMsg.(actMsg)
		if !ok || act.err != nil {
			t.Fatalf("unexpected patch result: %#v", resMsg)
		}
		if gotPatch == nil {
			t.Fatal("expected PATCH request to be sent")
		}
		if gotPatch["body"] != "content" {
			t.Errorf("expected body 'content', got %v", gotPatch["body"])
		}
		if _, ok := gotPatch["if_version"]; ok {
			t.Errorf("expected if_version to be omitted when version is 0, got %v", gotPatch["if_version"])
		}
	})

	t.Run("PatchFailurePreservesDraftFile", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"version conflict"}`))
		}))
		defer ts.Close()

		m := newModel(config{url: ts.URL, worker: "w1"}, newClient(ts.URL))

		tmpFile, err := os.CreateTemp("", "taskd-preserve-*.md")
		if err != nil {
			t.Fatal(err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)
		tmpFile.WriteString("precious user text")
		tmpFile.Close()

		patchCmd := editorPatchCmd(m.client, "task-editor-fail", 2, "precious user text", tmpPath)
		resMsg := patchCmd()
		act, ok := resMsg.(actMsg)
		if !ok || act.err == nil {
			t.Fatalf("expected error from failed patch, got %#v", resMsg)
		}

		if !strings.Contains(act.err.Error(), tmpPath) {
			t.Errorf("expected error message to reference draft file %s, got %v", tmpPath, act.err)
		}

		data, err := os.ReadFile(tmpPath)
		if err != nil || string(data) != "precious user text" {
			t.Fatalf("draft file was lost or corrupted on patch failure: err=%v data=%q", err, string(data))
		}
		up, _ := m.Update(act)
		m = up.(model)
		wantDraft := "draft saved to " + tmpPath
		if !strings.Contains(m.msg, wantDraft) {
			t.Fatalf("expected status message to contain %q, got %q", wantDraft, m.msg)
		}
	})

	t.Run("UnchangedLeavesTaskUntouched", func(t *testing.T) {
		m := newModel(config{}, nil)
		up, _ := m.Update(editorFinishedMsg{status: "task body unchanged"})
		m = up.(model)
		if m.msg != "task body unchanged" {
			t.Errorf("expected 'task body unchanged', got %q", m.msg)
		}
	})

	t.Run("EmptyLeavesTaskUntouched", func(t *testing.T) {
		m := newModel(config{}, nil)
		up, _ := m.Update(editorFinishedMsg{status: "task body empty, unchanged"})
		m = up.(model)
		if m.msg != "task body empty, unchanged" {
			t.Errorf("expected 'task body empty, unchanged', got %q", m.msg)
		}
	})

	t.Run("EditorErrorReportsStatus", func(t *testing.T) {
		m := newModel(config{}, nil)
		up, _ := m.Update(editorFinishedMsg{err: errors.New("exit status 2")})
		m = up.(model)
		if !strings.Contains(m.msg, "exit status 2") {
			t.Errorf("expected msg to mention exit status 2, got %q", m.msg)
		}
	})

	t.Run("GuardsDoneAndLeasedTasks", func(t *testing.T) {
		m := newModel(config{}, nil)
		m.now = time.Now()

		m.tasks = []task{{ID: "t-done", Status: "done"}}
		m.rebuildShown()
		m.cursor = 0
		up, _ := m.Update(tea.KeyPressMsg{Text: "E"})
		m = up.(model)
		if m.msg != "cannot edit done task" {
			t.Errorf("expected 'cannot edit done task', got %q", m.msg)
		}

		m.tasks = []task{{ID: "t-leased", Status: "leased", LeaseExpires: m.now.Unix() + 60}}
		m.rebuildShown()
		m.cursor = 0
		up, _ = m.Update(tea.KeyPressMsg{Text: "E"})
		m = up.(model)
		if m.msg != "cannot edit actively leased task" {
			t.Errorf("expected 'cannot edit actively leased task', got %q", m.msg)
		}
	})

	t.Run("ResolutionOrder", func(t *testing.T) {
		t.Setenv("VISUAL", "visual-ed")
		t.Setenv("EDITOR", "std-ed")
		if got := resolveEditor(); got != "visual-ed" {
			t.Errorf("expected VISUAL 'visual-ed', got %q", got)
		}

		t.Setenv("VISUAL", "")
		if got := resolveEditor(); got != "std-ed" {
			t.Errorf("expected EDITOR 'std-ed', got %q", got)
		}
	})

	t.Run("BuildEditorCmdArgs", func(t *testing.T) {
		cmd := buildEditorCmd("nano -w -l", "/path/to/file.md")
		if cmd.Path == "" {
			t.Fatal("empty command path")
		}
		if len(cmd.Args) != 4 || cmd.Args[0] != "nano" || cmd.Args[1] != "-w" || cmd.Args[2] != "-l" || cmd.Args[3] != "/path/to/file.md" {
			t.Errorf("unexpected command args: %v", cmd.Args)
		}
	})
}
