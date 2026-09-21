package taskd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilteredTasksTotalCount(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := openDB(dbPath, 0)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for range 3 {
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"alpha","priority":1,"body":"b"}`))
		if err != nil {
			t.Fatalf("POST /tasks: %v", err)
		}
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/tasks?project=alpha&limit=1")
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	defer resp.Body.Close()

	if total := resp.Header.Get("X-Total-Count"); total != "3" {
		t.Fatalf("X-Total-Count = %q, want 3", total)
	}
}
