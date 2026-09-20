package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestClaimMaxClaims(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 60, 2, ""))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{"body": "poison", "project": "p"})
	if code != http.StatusCreated {
		t.Fatalf("create task expected 201, got %d: %s", code, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created failed: %v", err)
	}

	for _, want := range []struct {
		worker string
		count  int
	}{{"w1", 1}, {"w2", 2}} {
		code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": want.worker})
		if code != http.StatusOK {
			t.Fatalf("claim by %s expected 200, got %d: %s", want.worker, code, body)
		}
		var item taskItem
		if err := json.Unmarshal(body, &item); err != nil {
			t.Fatalf("unmarshal claim failed: %v", err)
		}
		if item.Status != "leased" || item.ClaimCount != want.count {
			t.Fatalf("claim by %s: got status=%s claim_count=%d, want leased/%d", want.worker, item.Status, item.ClaimCount, want.count)
		}
		if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 0 WHERE id = ?", created.ID); err != nil {
			t.Fatalf("expire lease failed: %v", err)
		}
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w3"})
	if code != http.StatusNoContent {
		t.Fatalf("third claim expected 204, got %d: %s", code, body)
	}
	if len(body) != 0 {
		t.Fatalf("third claim expected empty body, got %q", body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+created.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("GET task expected 200, got %d: %s", code, body)
	}
	var final taskItem
	if err := json.Unmarshal(body, &final); err != nil {
		t.Fatalf("unmarshal final failed: %v", err)
	}
	if final.Status != "buried" || final.ClaimCount != 2 {
		t.Fatalf("got status=%s claim_count=%d, want buried/2", final.Status, final.ClaimCount)
	}
	if final.Worker != "" || final.LeaseExpires != 0 {
		t.Fatalf("buried task still holds lease: worker=%q lease_expires=%d", final.Worker, final.LeaseExpires)
	}
}

func TestClaimMaxClaimsUnlimitedByDefault(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 60))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{"body": "poison", "project": "p"})
	if code != http.StatusCreated {
		t.Fatalf("create task expected 201, got %d: %s", code, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal created failed: %v", err)
	}

	for _, worker := range []string{"w1", "w2", "w3"} {
		code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": worker})
		if code != http.StatusOK {
			t.Fatalf("claim by %s expected 200, got %d: %s", worker, code, body)
		}
		if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 0 WHERE id = ?", created.ID); err != nil {
			t.Fatalf("expire lease failed: %v", err)
		}
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/"+created.ID, nil)
	if code != http.StatusOK {
		t.Fatalf("GET task expected 200, got %d: %s", code, body)
	}
	var final taskItem
	if err := json.Unmarshal(body, &final); err != nil {
		t.Fatalf("unmarshal final failed: %v", err)
	}
	if final.Status != "pending" || final.ClaimCount != 3 {
		t.Fatalf("got status=%s claim_count=%d, want pending/3", final.Status, final.ClaimCount)
	}
}

func TestClaimByIDMaxClaims(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 60, 1, ""))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{"id": "poison", "body": "x", "project": "p"})
	if code != http.StatusCreated {
		t.Fatalf("create task expected 201, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/poison/claim", map[string]string{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("first claim expected 200, got %d: %s", code, body)
	}
	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 0 WHERE id = 'poison'"); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/poison/claim", map[string]string{"worker": "w2"})
	if code != http.StatusConflict {
		t.Fatalf("second claim expected 409, got %d: %s", code, body)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/poison", nil)
	if code != http.StatusOK {
		t.Fatalf("GET task expected 200, got %d: %s", code, body)
	}
	var final taskItem
	if err := json.Unmarshal(body, &final); err != nil {
		t.Fatalf("unmarshal final failed: %v", err)
	}
	if final.Status != "buried" || final.ClaimCount != 1 {
		t.Fatalf("got status=%s claim_count=%d, want buried/1", final.Status, final.ClaimCount)
	}
}

func TestClaimMaxClaimsSkipsBuriedHead(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 60, 1, ""))
	defer srv.Close()

	for _, task := range []map[string]any{
		{"id": "poison", "body": "x", "priority": 1, "project": "p"},
		{"id": "good", "body": "y", "priority": 2, "project": "p"},
	} {
		code, body := post(t, srv.URL+"/tasks", task)
		if code != http.StatusCreated {
			t.Fatalf("create task expected 201, got %d: %s", code, body)
		}
	}

	code, body := post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("first claim expected 200, got %d: %s", code, body)
	}
	var first taskItem
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatalf("unmarshal first claim failed: %v", err)
	}
	if first.ID != "poison" {
		t.Fatalf("expected poison claimed first, got %s", first.ID)
	}
	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 0 WHERE id = 'poison'"); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w2"})
	if code != http.StatusOK {
		t.Fatalf("second claim expected 200, got %d: %s", code, body)
	}
	var second taskItem
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("unmarshal second claim failed: %v", err)
	}
	if second.ID != "good" || second.Status != "leased" {
		t.Fatalf("expected good leased, got id=%s status=%s", second.ID, second.Status)
	}

	code, body = do(t, http.MethodGet, srv.URL+"/tasks/poison", nil)
	if code != http.StatusOK {
		t.Fatalf("GET poison expected 200, got %d: %s", code, body)
	}
	var poison taskItem
	if err := json.Unmarshal(body, &poison); err != nil {
		t.Fatalf("unmarshal poison failed: %v", err)
	}
	if poison.Status != "buried" {
		t.Fatalf("expected poison buried, got %s", poison.Status)
	}
}

func TestKickResetsClaimCount(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 60, 1, ""))
	defer srv.Close()

	code, body := post(t, srv.URL+"/tasks", map[string]any{"id": "poison", "body": "x", "project": "p"})
	if code != http.StatusCreated {
		t.Fatalf("create task expected 201, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w1"})
	if code != http.StatusOK {
		t.Fatalf("first claim expected 200, got %d: %s", code, body)
	}
	if _, err := db.rw.Exec("UPDATE tasks SET lease_expires = 0 WHERE id = 'poison'"); err != nil {
		t.Fatalf("expire lease failed: %v", err)
	}
	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w2"})
	if code != http.StatusNoContent {
		t.Fatalf("claim past limit expected 204, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/poison/kick", nil)
	if code != http.StatusNoContent {
		t.Fatalf("kick expected 204, got %d: %s", code, body)
	}

	code, body = post(t, srv.URL+"/tasks/claim", map[string]string{"worker": "w3"})
	if code != http.StatusOK {
		t.Fatalf("claim after kick expected 200, got %d: %s", code, body)
	}
	var item taskItem
	if err := json.Unmarshal(body, &item); err != nil {
		t.Fatalf("unmarshal claim failed: %v", err)
	}
	if item.ID != "poison" || item.ClaimCount != 1 {
		t.Fatalf("expected poison with claim_count 1, got id=%s claim_count=%d", item.ID, item.ClaimCount)
	}
}

func TestParseFlagsMaxClaims(t *testing.T) {
	cfg, err := parseFlags([]string{"-max-claims", "3"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.maxClaims != 3 {
		t.Fatalf("got maxClaims %d, want 3", cfg.maxClaims)
	}

	cfg, err = parseFlags(nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags failed: %v", err)
	}
	if cfg.maxClaims != 0 {
		t.Fatalf("got default maxClaims %d, want 0", cfg.maxClaims)
	}

	if _, err := parseFlags([]string{"-max-claims", "-1"}, io.Discard, io.Discard); err == nil {
		t.Fatal("expected error for negative max claims")
	}
}
