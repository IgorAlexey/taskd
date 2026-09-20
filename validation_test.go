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
	t.Run("claim wait invalid", func(t *testing.T) {
		payload := `{"wait":-1}`
		resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(payload))
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
		want := `invalid wait -1, must be between 0 and 86400 seconds`
		if apiErr.Error != want {
			t.Fatalf("got error %q, want %q", apiErr.Error, want)
		}
	})
}

func TestValidProject(t *testing.T) {
	cases := []struct {
		project string
		want    bool
	}{
		{".", false},
		{"..", false},
		{"", false},
		{"my-project", true},
		{"valid-project.123", true},
	}
	for _, tc := range cases {
		if got := validProject(tc.project); got != tc.want {
			t.Errorf("validProject(%q) = %v, want %v", tc.project, got, tc.want)
		}
		msg, ok := checkProject(tc.project)
		if ok != tc.want {
			t.Errorf("checkProject(%q) ok = %v, want %v", tc.project, ok, tc.want)
		}
		if !ok && tc.project != "" && msg == "" {
			t.Errorf("checkProject(%q) expected error message, got empty", tc.project)
		}
	}
}
func TestPatchValidationFieldErrors(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createResp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"p","body":"initial"}`))
	if err != nil {
		t.Fatalf("POST /tasks failed: %v", err)
	}
	defer createResp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created task failed: %v", err)
	}

	cases := []struct {
		name      string
		payload   string
		wantError string
		wantField string
	}{
		{"invalid body", `{"body":"   "}`, "invalid body", "body"},
		{"invalid project", `{"project":"bad proj"}`, `invalid project "bad proj": must contain only [a-zA-Z0-9._-]`, "project"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPatch, srv.URL+"/tasks/"+created.ID, strings.NewReader(tc.payload))
			if err != nil {
				t.Fatalf("new request failed: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("PATCH failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			var apiErr apiError
			if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if apiErr.Error != tc.wantError || apiErr.Field != tc.wantField {
				t.Fatalf("got error=%q field=%q, want error=%q field=%q", apiErr.Error, apiErr.Field, tc.wantError, tc.wantField)
			}
		})
	}
}

func TestWorkerValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	cases := []struct {
		name       string
		worker     string
		wantStatus int
		wantError  string
	}{
		{"newline in worker", "bad\nworker", http.StatusBadRequest, "invalid worker \"bad\\nworker\""},
		{"tabs in worker", "worker\twith\ttabs", http.StatusBadRequest, "invalid worker \"worker\\twith\\ttabs\""},
		{"spaces in worker", "bad worker", http.StatusBadRequest, "invalid worker \"bad worker\""},
		{"special char in worker", "worker@host", http.StatusBadRequest, "invalid worker \"worker@host\""},
		{"empty worker", "", http.StatusBadRequest, "missing worker"},
		{"whitespace only worker", "   ", http.StatusBadRequest, "invalid worker \"   \""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"worker": tc.worker})
			resp, err := http.Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(string(payload)))
			if err != nil {
				t.Fatalf("POST claim failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			var apiErr apiError
			if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if apiErr.Error != tc.wantError {
				t.Fatalf("got error = %q, want %q", apiErr.Error, tc.wantError)
			}
		})
	}
}
func TestValidationProjectCharacters(t *testing.T) {
	cases := []struct {
		fn   func() (string, bool)
		want string
	}{
		{
			fn:   func() (string, bool) { return checkProject("p 1") },
			want: `invalid project "p 1": must contain only [a-zA-Z0-9._-]`,
		},
		{
			fn:   func() (string, bool) { return checkTaskID("bad id") },
			want: `invalid id "bad id": must contain only [a-zA-Z0-9._-]`,
		},
	}
	for _, tc := range cases {
		msg, ok := tc.fn()
		if ok {
			t.Fatal("expected validation failure")
		}
		if msg != tc.want {
			t.Fatalf("got %q, want %q", msg, tc.want)
		}
	}
}
