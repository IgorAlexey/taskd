package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestTouchRejectsExpiredLease(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	code, body := post(t, srv.URL+"/tasks/"+id+"/touch", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "lease has expired")
}

func TestBuryRejectsExpiredLease(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	code, body := post(t, srv.URL+"/tasks/"+id+"/bury", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "lease has expired")
}

func TestExpiredLeaseOfAnotherWorkerIsNotMine(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	for _, action := range []string{"done", "touch", "release", "bury"} {
		t.Run(action, func(t *testing.T) {
			code, body := post(t, srv.URL+"/tasks/"+id+"/"+action, map[string]any{"worker": "w2"})
			assertConflict(t, code, body, "task not leased by worker")
		})
	}
}

func TestDoneDistinguishesDoneState(t *testing.T) {
	_, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("done expected 204, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "task is done")

	code, body = post(t, srv.URL+"/tasks/"+id+"/touch", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "task is done")
}

func TestConflictDistinguishesPendingAndForeignLease(t *testing.T) {
	_, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "task is pending")

	claimTask(t, srv.URL, "p", "w1")
	code, body = post(t, srv.URL+"/tasks/"+id+"/release", map[string]any{"worker": "w2"})
	assertConflict(t, code, body, "task not leased by worker")

	code, body = post(t, srv.URL+"/tasks/"+id+"/bury", map[string]any{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("bury expected 204, got %d: %s", code, body)
	}
	code, body = post(t, srv.URL+"/tasks/"+id+"/touch", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "task is buried")
}

func TestConflictStillReportsMissingTask(t *testing.T) {
	_, srv := conflictServer(t)
	code, body := post(t, srv.URL+"/tasks/"+strings.Repeat("a", 32)+"/done", map[string]any{"worker": "w1"})
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", code, body)
	}
	assertErrorBody(t, body, "task not found")
}
