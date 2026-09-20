package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCopyBodyEmpty(t *testing.T) {
	t.Run("BodyCopiedToClipboardWhenPresent", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:        "t-body",
			AssetPath: "ignored/asset.path",
			Body:      "important task body",
			Status:    "pending",
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

	t.Run("EmptyBodyWithAssetPathCopiesAssetPath", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:        "t-asset-only",
			AssetPath: "models/render.blend",
			Body:      "",
			Status:    "pending",
		}}
		m.rebuildShown()
		m.cursor = 0

		up, cmd := m.Update(tea.KeyPressMsg{Text: "Y"})
		m = up.(model)

		wantMsg := "copied asset path to clipboard"
		if m.msg != wantMsg {
			t.Fatalf("msg = %q, want %q", m.msg, wantMsg)
		}
		if cmd == nil {
			t.Fatal("expected non-nil cmd for copying asset path")
		}

		res := cmd()
		batch, ok := res.(tea.BatchMsg)
		if !ok || len(batch) < 1 {
			t.Fatalf("expected BatchMsg with subcommands, got %T", res)
		}
		clipboardMsg := batch[0]()
		if !strings.Contains(fmt.Sprintf("%v", clipboardMsg), "models/render.blend") {
			t.Errorf("expected clipboard command to copy asset path, got %v", clipboardMsg)
		}
	})

	t.Run("EmptyBodyAndEmptyAssetPathDisplaysNothingToCopy", func(t *testing.T) {
		m := newModel(config{worker: "test-worker"}, nil)
		m.tasks = []task{{
			ID:        "t-empty",
			AssetPath: "",
			Body:      "",
			Status:    "pending",
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
