package main

import (
	"strings"
	"testing"
)

// fetch() scopes /tasks to the active project, so re-rendering the cached
// slice after p can only match zero tasks: p must refetch at once.
func TestCycleProjectRefreshesImmediately(t *testing.T) {
	u, _ := projectStub(t, false)
	cycleProject(u)
	waitShown(t, u, "bbbbbbb2")
}

// The poll loop can hold the refresh slot when p is pressed, which drops the
// refresh the keypress starts. The in-flight one must notice its answer went
// stale and fetch the project the operator actually selected.
func TestCycleProjectDuringInFlightRefresh(t *testing.T) {
	u, release := projectStub(t, true)

	go u.refresh("proj-a")
	eventually(t, func() bool { return u.refreshing.Load() })

	cycleProject(u)

	// While the fetch is out, the empty table must not claim proj-b is
	// empty; we simply do not know yet.
	if body := bodyText(u); !strings.Contains(body, "Loading proj-b") {
		t.Fatalf("expected a loading state while the fetch is out, got %q", body)
	}

	release()
	waitShown(t, u, "bbbbbbb2")
}

// The loading state must be transient: a project that really is empty has
// to settle on the message that tells the operator how to leave it.
func TestCycleToEmptyProjectShowsEmptyState(t *testing.T) {
	u, _ := projectStub(t, false)
	cycleProject(u)
	waitShown(t, u, "bbbbbbb2")

	cycleProject(u)
	eventually(t, func() bool {
		return bodyText(u) == "No tasks match filter. Press '0' to clear filter, 'p' to cycle project."
	})
}

// -project is adopted before the first poll answers, so startup must not
// claim the project is empty either.
func TestStartupProjectShowsLoading(t *testing.T) {
	u, _ := projectStub(t, false)
	if body := bodyText(u); !strings.Contains(body, "Loading proj-a") {
		t.Fatalf("expected a loading state before the first fetch, got %q", body)
	}
}

// A failed fetch is an answer too: it landed as "no". Otherwise a daemon
// that stays down leaves the pane reading "Loading ..." forever.
func TestCycleProjectFetchErrorClearsLoading(t *testing.T) {
	u, _ := projectStub(t, false)
	set := make(chan struct{})
	u.app.QueueUpdate(func() {
		u.url = "http://127.0.0.1:1"
		close(set)
	})
	<-set

	cycleProject(u)
	eventually(t, func() bool {
		return !strings.Contains(bodyText(u), "Loading")
	})
}

// Cycling past the last project selects all projects, which is a filter
// switch like any other: the cache cannot answer it either.
func TestCycleToAllProjectsShowsLoading(t *testing.T) {
	u, _ := projectStub(t, true)
	cycleProject(u)
	cycleProject(u)
	cycleProject(u)

	var project string
	queried := make(chan struct{})
	u.app.QueueUpdate(func() {
		project = u.project
		close(queried)
	})
	<-queried
	if project != "" {
		t.Fatalf("expected the all-projects position, got %q", project)
	}
	if body := bodyText(u); !strings.Contains(body, "Loading") {
		t.Fatalf("expected a loading state on the all-projects switch, got %q", body)
	}
}
