package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func assertLeaseConflict(t *testing.T, code int, body []byte, want leaseConflict) {
	t.Helper()
	if code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", code, body)
	}
	var got leaseConflict
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal conflict body %q: %v", body, err)
	}
	if got != want {
		t.Fatalf("conflict body %+v, want %+v", got, want)
	}
}

func TestDoneAcceptsLapsedLeaseWhileUnclaimed(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	if status, _ := getTask(t, srv.URL, id); status != "pending" {
		t.Fatalf("status of a lapsed lease %q, want pending", status)
	}

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":     "w1",
		"primitives": map[string]any{"out": "x"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("late done expected 204, got %d: %s", code, body)
	}

	status, prim := getTask(t, srv.URL, id)
	if status != "done" || prim != `{"out":"x"}` {
		t.Fatalf("task after late done: status %q primitives %s", status, prim)
	}
	var claims int
	if err := db.ro.QueryRow("SELECT claim_count FROM tasks WHERE id = ?", id).Scan(&claims); err != nil {
		t.Fatalf("read claim_count: %v", err)
	}
	if claims != 1 {
		t.Fatalf("claim_count %d, want 1", claims)
	}
}

func TestDoneAfterTakeoverNamesTheHolder(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)
	claimTask(t, srv.URL, "p", "w2")

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":     "w1",
		"primitives": map[string]any{"out": "x"},
	})
	assertLeaseConflict(t, code, body, leaseConflict{Error: "task leased by another worker", Worker: "w2", ClaimCount: 2})

	status, prim := getTask(t, srv.URL, id)
	if status != "leased" || prim != "null" {
		t.Fatalf("task after refused done: status %q primitives %s", status, prim)
	}
}

func TestDoneFromSupersededGenerationIsRefused(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)
	claimTask(t, srv.URL, "p", "w1")

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":      "w1",
		"claim_count": 1,
		"primitives":  map[string]any{"out": "stale"},
	})
	assertLeaseConflict(t, code, body, leaseConflict{Error: "task claimed again", Worker: "w1", ClaimCount: 2})

	status, prim := getTask(t, srv.URL, id)
	if status != "leased" || prim != "null" {
		t.Fatalf("task after superseded done: status %q primitives %s", status, prim)
	}

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":      "w1",
		"claim_count": 2,
		"primitives":  map[string]any{"out": "fresh"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("done from the live generation expected 204, got %d: %s", code, body)
	}

	status, prim = getTask(t, srv.URL, id)
	if status != "done" || prim != `{"out":"fresh"}` {
		t.Fatalf("task after live done: status %q primitives %s", status, prim)
	}
}

func TestDoneWithMatchingClaimCountOnLapsedLease(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":      "w1",
		"claim_count": 1,
		"primitives":  map[string]any{"out": "x"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("fenced late done expected 204, got %d: %s", code, body)
	}
}

func TestAfterLapseTouchFailsButDoneSucceeds(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	code, body := post(t, srv.URL+"/tasks/"+id+"/touch", map[string]any{"worker": "w1"})
	assertConflict(t, code, body, "lease has expired")

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":     "w1",
		"primitives": map[string]any{"out": "x"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("late done expected 204, got %d: %s", code, body)
	}
}

func TestDoneWithoutTheFenceCannotTellGenerationsApart(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)
	claimTask(t, srv.URL, "p", "w1")

	code, body := post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":     "w1",
		"primitives": map[string]any{"stale": true},
	})
	if code != http.StatusNoContent {
		t.Fatalf("unfenced done expected 204, got %d: %s", code, body)
	}
	if status, prim := getTask(t, srv.URL, id); status != "done" || prim != `{"stale":true}` {
		t.Fatalf("task after unfenced done: status %q primitives %s", status, prim)
	}

	id = createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":     "w1",
		"primitives": map[string]any{"stale": true},
	})
	if code != http.StatusNoContent {
		t.Fatalf("unfenced done after two lapses expected 204, got %d: %s", code, body)
	}
	if status, prim := getTask(t, srv.URL, id); status != "done" || prim != `{"stale":true}` {
		t.Fatalf("task after two lapses: status %q primitives %s", status, prim)
	}

	id = createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)
	claimTask(t, srv.URL, "p", "w1")

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":      "w1",
		"claim_count": 1,
		"primitives":  map[string]any{"stale": true},
	})
	assertLeaseConflict(t, code, body, leaseConflict{Error: "task claimed again", Worker: "w1", ClaimCount: 2})
	if status, prim := getTask(t, srv.URL, id); status != "leased" || prim != "null" {
		t.Fatalf("task after fenced done: status %q primitives %s", status, prim)
	}
}

func TestTheFenceIsScopedToOneLifeOfTheTask(t *testing.T) {
	db, srv := conflictServer(t)
	id := createTask(t, srv.URL, "p")
	claimTask(t, srv.URL, "p", "w1")
	expireLease(t, db, id)
	claimTask(t, srv.URL, "p", "w1")

	code, body := post(t, srv.URL+"/tasks/"+id+"/bury", map[string]any{"worker": "w1"})
	if code != http.StatusNoContent {
		t.Fatalf("bury expected 204, got %d: %s", code, body)
	}
	code, body = post(t, srv.URL+"/tasks/"+id+"/kick", nil)
	if code != http.StatusNoContent {
		t.Fatalf("kick expected 204, got %d: %s", code, body)
	}
	claimTask(t, srv.URL, "p", "w1")

	code, body = post(t, srv.URL+"/tasks/"+id+"/done", map[string]any{
		"worker":      "w1",
		"claim_count": 1,
		"primitives":  map[string]any{"out": "stale-gen1"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("fenced done after a kick expected 204, got %d: %s", code, body)
	}
	if status, prim := getTask(t, srv.URL, id); status != "done" || prim != `{"out":"stale-gen1"}` {
		t.Fatalf("task after a kicked fence: status %q primitives %s", status, prim)
	}
}
