package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestWorkersProjectFilter(t *testing.T) {
	db, err := openDB(":memory:", 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	srv := httptest.NewServer(newHandler(db, 300))
	t.Cleanup(srv.Close)

	now := time.Now().Unix()
	_, err = db.rw.Exec(`INSERT INTO tasks (id, project, status, body, worker, lease_expires, created_at) VALUES
		('t1', 'foo', 'leased', 'b1', 'w1', ?, ?),
		('t2', 'foo', 'done',   'b2', 'w2', NULL, ?),
		('t3', 'foo', 'leased', 'b3', 'w3', ?, ?),
		('t4', 'bar', 'leased', 'b4', 'w4', ?, ?),
		('t5', 'bar', 'done',   'b5', 'w5', NULL, ?)`,
		now+300, now,
		now,
		now-300, now,
		now+300, now,
		now,
	)
	if err != nil {
		t.Fatalf("insert tasks failed: %v", err)
	}

	getWorkers := func(urlPath string) (int, []string) {
		resp, err := http.Get(srv.URL + urlPath)
		if err != nil {
			t.Fatalf("GET %s failed: %v", urlPath, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, nil
		}
		var list []string
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("decode %s response failed: %v", urlPath, err)
		}
		return resp.StatusCode, list
	}

	code, workers := getWorkers("/workers?project=foo")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	wantFoo := []string{"w1", "w2"}
	if !reflect.DeepEqual(workers, wantFoo) {
		t.Fatalf("workers = %v, want %v", workers, wantFoo)
	}

	code, workers = getWorkers("/workers?project=*")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	wantAll := []string{"w1", "w2", "w4", "w5"}
	if !reflect.DeepEqual(workers, wantAll) {
		t.Fatalf("workers = %v, want %v", workers, wantAll)
	}

	code, workers = getWorkers("/workers")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if !reflect.DeepEqual(workers, wantAll) {
		t.Fatalf("workers = %v, want %v", workers, wantAll)
	}

	badURLs := []string{
		"/workers?project=bad/name",
		"/workers?project=",
		"/workers?other=param",
	}
	for _, u := range badURLs {
		resp, err := http.Get(srv.URL + u)
		if err != nil {
			t.Fatalf("GET %s failed: %v", u, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want %d", u, resp.StatusCode, http.StatusBadRequest)
		}
	}
}
