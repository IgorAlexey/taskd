package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCopyBodyEmpty(t *testing.T) {
	t.Run("BodyCopiedToClipboardWhenPresent", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:     1,
			Body:   "important task body",
			Status: "pending",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, cmd := m.Update(tea.KeyPressMsg{Text: "Y"})
		m = up.(model)

		wantMsg := "copied body to clipboard"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if cmd == nil {
			t.Fatal("expected non-nil cmd for copying body")
		}

		res := cmd()
		batch, ok := res.(tea.BatchMsg)
		if !ok || len(batch) < 1 {
			t.Fatalf("expected BatchMsg with subcommands, got %T", res)
		}
		clipboardMsg := batch[0]()
		if !strings.Contains(fmt.Sprintf("%v", clipboardMsg), "important task body") {
			t.Errorf("expected clipboard command to copy body, got %v", clipboardMsg)
		}
	})

	t.Run("EmptyBodyDisplaysNothingToCopy", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:     2,
			Body:   "",
			Status: "pending",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, _ := m.Update(tea.KeyPressMsg{Text: "Y"})
		m = up.(model)

		wantMsg := "nothing to copy"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
	})
}

func TestCopyPrimitives(t *testing.T) {
	t.Run("CtrlYCopiesPrimitives", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:         3,
			Primitives: json.RawMessage(`{"status":"success","exit":0}`),
			Status:     "done",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
		m = up.(model)

		wantMsg := "copied result to clipboard"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if cmd == nil {
			t.Fatal("expected non-nil cmd for copying primitives")
		}

		res := cmd()
		batch, ok := res.(tea.BatchMsg)
		if !ok || len(batch) < 1 {
			t.Fatalf("expected BatchMsg with subcommands, got %T", res)
		}
		clipboardMsg := batch[0]()
		if !strings.Contains(fmt.Sprintf("%v", clipboardMsg), `{"status":"success","exit":0}`) {
			t.Errorf("expected clipboard command to copy primitives, got %v", clipboardMsg)
		}
	})

	t.Run("CtrlYEmptyPrimitivesDisplaysNothingToCopy", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:     4,
			Status: "done",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, _ := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
		m = up.(model)

		wantMsg := "nothing to copy"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
	})

	t.Run("CtrlYPreservesPrimitivesWhenBodyPresent", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:         5,
			Body:       "task description body",
			Primitives: json.RawMessage(`{"output":"only primitives"}`),
			Status:     "done",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
		m = up.(model)

		wantMsg := "copied result to clipboard"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if cmd == nil {
			t.Fatal("expected non-nil cmd for copying primitives")
		}

		res := cmd()
		batch, ok := res.(tea.BatchMsg)
		if !ok || len(batch) < 1 {
			t.Fatalf("expected BatchMsg with subcommands, got %T", res)
		}
		clipboardMsg := batch[0]()
		if !strings.Contains(fmt.Sprintf("%v", clipboardMsg), `{"output":"only primitives"}`) {
			t.Errorf("expected clipboard command to copy primitives, got %v", clipboardMsg)
		}
	})
}
