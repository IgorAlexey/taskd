package main

import (
	"strings"
	"testing"
)

func TestEmptyStateStages(t *testing.T) {
	t.Run("EmptyProject", func(t *testing.T) {
		m := model{
			connected: true,
			project:   "beta",
			projects:  []string{"alpha"},
			hasStats:  true,
			stats:     stats{Total: 10},
		}
		got := m.emptyState()
		want := "No tasks in project beta. Press 'p' to cycle project."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("EmptyStatus", func(t *testing.T) {
		m := model{
			project:   "alpha",
			projects:  []string{"alpha"},
			connected: true,
			filter:    "done",
			hasStats:  true,
			stats:     stats{Pending: 5, Total: 5, Done: 0},
		}
		got := m.emptyState()
		want := "No done tasks. Press '0' to show all."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("SearchMissInProject", func(t *testing.T) {
		m := model{
			project:   "alpha",
			projects:  []string{"alpha"},
			connected: true,
			query:     "nomatch",
			hasStats:  true,
			stats:     stats{Pending: 3, Total: 3},
		}
		got := m.emptyState()
		want := "No task matches \"nomatch\". Press 'Esc' to clear."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("SearchMissGlobal", func(t *testing.T) {
		m := model{
			query:     "zzzznope",
			hasStats:  true,
			connected: true,
			stats:     stats{Pending: 235, Leased: 56, Total: 291},
		}
		got := m.emptyState()
		want := "No task matches \"zzzznope\" (0 of 291). Press 'Esc' to clear."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if !strings.Contains(got, "zzzznope") || !strings.Contains(got, "291") {
			t.Fatalf("expected query string and count in %q", got)
		}
		if strings.Contains(got, "No tasks yet") {
			t.Fatalf("should not claim empty queue in %q", got)
		}
	})

	t.Run("EmptyDBWithSearch", func(t *testing.T) {
		m := model{
			query:     "zzzznope",
			hasStats:  true,
			connected: true,
			stats:     stats{Total: 0},
		}
		got := m.emptyState()
		want := "No tasks yet. Press 'n' to create a task."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("PagingNonEmptyCorpus", func(t *testing.T) {
		m := model{
			hasStats:  true,
			stats:     stats{Total: 10},
			connected: true,
		}
		got := m.emptyState()
		want := "No tasks match filter."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestEmptyStateDisconnected(t *testing.T) {
	t.Run("DisconnectedWithoutError", func(t *testing.T) {
		m := model{
			cfg:       config{url: "http://localhost:8080"},
			connected: false,
		}
		got := m.emptyState()
		want := "Disconnected from http://localhost:8080"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if strings.Contains(got, "No tasks yet") {
			t.Fatalf("should not claim empty queue when disconnected: %q", got)
		}
	})

	t.Run("DisconnectedWithError", func(t *testing.T) {
		m := model{
			cfg:       config{url: "http://localhost:8080"},
			connected: false,
			lastErr:   "connection refused",
		}
		got := m.emptyState()
		want := "Disconnected from http://localhost:8080: connection refused"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if strings.Contains(got, "No tasks yet") {
			t.Fatalf("should not claim empty queue when disconnected: %q", got)
		}
	})

	t.Run("DisconnectedWithoutURL", func(t *testing.T) {
		m := model{
			connected: false,
		}
		got := m.emptyState()
		want := "Disconnected from (no URL configured)"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
