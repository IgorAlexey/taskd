package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidationErrorsDetailed(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	t.Run("limit out of bounds", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/tasks?limit=2000")
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "2000") || !strings.Contains(apiErr.Error, "1000") {
			t.Fatalf("expected error containing 2000 and 1000, got %q", apiErr.Error)
		}
	})

	t.Run("limit not integer", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/tasks?limit=abc")
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "abc") || !strings.Contains(apiErr.Error, "not an integer") {
			t.Fatalf("expected parse error for limit, got %q", apiErr.Error)
		}
	})

	t.Run("offset negative", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/tasks?offset=-5")
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "-5") || !strings.Contains(apiErr.Error, "0") {
			t.Fatalf("expected error containing -5 and 0, got %q", apiErr.Error)
		}
	})

	t.Run("priority filter negative", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/tasks?priority=-1")
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "-1") || !strings.Contains(apiErr.Error, "0") {
			t.Fatalf("expected error containing -1 and 0, got %q", apiErr.Error)
		}
	})

	t.Run("status invalid", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/tasks?status=bogus")
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		for _, want := range []string{"pending", "leased", "done", "buried"} {
			if !strings.Contains(apiErr.Error, want) {
				t.Fatalf("expected error containing %q, got %q", want, apiErr.Error)
			}
		}
	})

	t.Run("post reserved id claim", func(t *testing.T) {
		payload := `{"project":"p","body":"b","id":"claim"}`
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "reserved") {
			t.Fatalf("expected error saying id is reserved, got %q", apiErr.Error)
		}
	})

	t.Run("post overlength id names 128", func(t *testing.T) {
		longID := strings.Repeat("a", 129)
		payload := `{"project":"p","body":"b","id":"` + longID + `"}`
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "128") {
			t.Fatalf("expected error naming 128, got %q", apiErr.Error)
		}
		if strings.Contains(apiErr.Error, longID) {
			t.Fatalf("error must not echo unbounded overlength id")
		}
	})

	t.Run("post overlength project names 64", func(t *testing.T) {
		longProj := strings.Repeat("p", 65)
		payload := `{"project":"` + longProj + `","body":"b"}`
		resp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var apiErr apiError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !strings.Contains(apiErr.Error, "64") {
			t.Fatalf("expected error naming 64, got %q", apiErr.Error)
		}
		if strings.Contains(apiErr.Error, longProj) {
			t.Fatalf("error must not echo unbounded overlength project")
		}
	})
}
