package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotes_ValidationAndNotFound(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var createdVal map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdVal)
	createRes.Body.Close()
	taskValURL := fmt.Sprintf("%s/tasks/%d/notes", srv.URL, createdVal["id"])

	cases := []struct {
		url       string
		body      string
		status    int
		wantField string
	}{
		{taskValURL, `{"author":"","text":"hello"}`, http.StatusBadRequest, "author"},
		{taskValURL, `{"author":"   ","text":"hello"}`, http.StatusBadRequest, "author"},
		{taskValURL, `{"author":"alice","text":""}`, http.StatusBadRequest, "text"},
		{taskValURL, `{"author":"alice","text":"   "}`, http.StatusBadRequest, "text"},
		{taskValURL, fmt.Sprintf(`{"author":"%s","text":"hello"}`, strings.Repeat("a", 129)), http.StatusBadRequest, "author"},
		{taskValURL, `{}`, http.StatusBadRequest, "author"},
		{srv.URL + "/tasks/99999/notes", `{"author":"alice","text":"note"}`, http.StatusNotFound, ""},
	}

	for _, tc := range cases {
		res, err := http.Post(tc.url, "application/json", bytes.NewBufferString(tc.body))
		if err != nil {
			t.Fatalf("post failed: %v", err)
		}
		if res.StatusCode != tc.status {
			t.Fatalf("POST %s with %s: expected status %d, got %d", tc.url, tc.body, tc.status, res.StatusCode)
		}
		if tc.wantField != "" {
			var errResp struct {
				Error string `json:"error"`
				Field string `json:"field"`
			}
			if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error response failed: %v", err)
			}
			if errResp.Field != tc.wantField {
				t.Fatalf("POST %s with %s: expected field %q, got %q", tc.url, tc.body, tc.wantField, errResp.Field)
			}
		}
		res.Body.Close()
	}
}

func TestNoteTextLengthLimit(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var createdLimit map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdLimit)
	createRes.Body.Close()
	taskLimitPath := fmt.Sprintf("/tasks/%d", createdLimit["id"])

	longText := strings.Repeat("a", 65537)
	body, _ := json.Marshal(map[string]string{
		"author": "worker-1",
		"text":   longText,
	})

	res, err := http.Post(srv.URL+taskLimitPath+"/notes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d", res.StatusCode)
	}

	var errResp map[string]string
	if err := json.NewDecoder(res.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response failed: %v", err)
	}
	if errResp["error"] != "text too long" {
		t.Fatalf("expected error %q, got %q", "text too long", errResp["error"])
	}

	maxValidText := strings.Repeat("b", 65536)
	validBody, _ := json.Marshal(map[string]string{
		"author": "worker-1",
		"text":   maxValidText,
	})
	resValid, err := http.Post(srv.URL+taskLimitPath+"/notes", "application/json", bytes.NewReader(validBody))
	if err != nil {
		t.Fatalf("post valid note failed: %v", err)
	}
	defer resValid.Body.Close()

	if resValid.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201 Created for 65536 byte note, got %d", resValid.StatusCode)
	}
}

func TestCreateNoteAuthorValidation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 30))
	defer srv.Close()

	createRes, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(`{"body":"spec","project":"p1"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var createdAuthor map[string]int64
	json.NewDecoder(createRes.Body).Decode(&createdAuthor)
	createRes.Body.Close()
	taskAuthorPath := fmt.Sprintf("/tasks/%d", createdAuthor["id"])

	validTrimBody := `{"author":"  alice:worker  ","text":"test trimmed author"}`
	res, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewBufferString(validTrimBody))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for trimmed author, got %d", res.StatusCode)
	}
	var createdNote taskNote
	if err := json.NewDecoder(res.Body).Decode(&createdNote); err != nil {
		t.Fatalf("decode note response failed: %v", err)
	}
	if createdNote.Author != "alice:worker" {
		t.Fatalf("expected author %q, got %q", "alice:worker", createdNote.Author)
	}

	getRes, err := http.Get(srv.URL + taskAuthorPath + "/notes")
	if err != nil {
		t.Fatalf("get notes failed: %v", err)
	}
	defer getRes.Body.Close()
	var notes []taskNote
	if err := json.NewDecoder(getRes.Body).Decode(&notes); err != nil {
		t.Fatalf("decode notes failed: %v", err)
	}
	if len(notes) != 1 || notes[0].Author != "alice:worker" {
		t.Fatalf("expected 1 note with author 'alice:worker', got %+v", notes)
	}

	validCharsBody := `{"author":"worker.1_sub:dir/node-a","text":"test valid chars"}`
	resChars, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewBufferString(validCharsBody))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer resChars.Body.Close()
	if resChars.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for valid characters, got %d", resChars.StatusCode)
	}
	validWhitespaceBody := `{"author":"  alice\n\t  ","text":"test whitespace author"}`
	resWs, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewBufferString(validWhitespaceBody))
	if err != nil {
		t.Fatalf("post note failed: %v", err)
	}
	defer resWs.Body.Close()
	if resWs.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for whitespace trimmed author, got %d", resWs.StatusCode)
	}
	var wsNote taskNote
	if err := json.NewDecoder(resWs.Body).Decode(&wsNote); err != nil {
		t.Fatalf("decode note failed: %v", err)
	}
	if wsNote.Author != "alice" {
		t.Fatalf("expected author 'alice', got %q", wsNote.Author)
	}

	invalidAuthors := []string{
		"  alice\nworker  ",
		"alice\tbob",
		"bad author",
		"author@domain",
		"author!",
		"author#1",
		"author$foo",
	}
	for _, badAuthor := range invalidAuthors {
		reqBody, _ := json.Marshal(map[string]string{
			"author": badAuthor,
			"text":   "some note",
		})
		badRes, err := http.Post(srv.URL+taskAuthorPath+"/notes", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			t.Fatalf("post note with bad author %q failed: %v", badAuthor, err)
		}
		if badRes.StatusCode != http.StatusBadRequest {
			badRes.Body.Close()
			t.Fatalf("expected 400 Bad Request for author %q, got %d", badAuthor, badRes.StatusCode)
		}
		var errResp map[string]string
		if err := json.NewDecoder(badRes.Body).Decode(&errResp); err != nil {
			badRes.Body.Close()
			t.Fatalf("decode error response for author %q failed: %v", badAuthor, err)
		}
		badRes.Body.Close()
		if errResp["error"] != "invalid author" {
			t.Fatalf("expected error 'invalid author' for author %q, got %q", badAuthor, errResp["error"])
		}
	}
}
