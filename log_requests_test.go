package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogRequestsMiddleware(t *testing.T) {
	db, err := openDB(t.TempDir()+"/test.db", 0)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	var buf bytes.Buffer
	origWriter := log.Writer()
	origFlags := log.Flags()
	defer func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
	}()
	log.SetOutput(&buf)
	log.SetFlags(0)

	handler := withRequestLogging(newHandler(db, 300))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks")
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/tasks/nope-does-not-exist")
	if err != nil {
		t.Fatalf("GET /tasks/nope-does-not-exist: %v", err)
	}
	resp.Body.Close()

	logged := buf.String()
	if !strings.Contains(logged, "GET /tasks 200") {
		t.Errorf("expected log to contain %q, got:\n%s", "GET /tasks 200", logged)
	}
	if !strings.Contains(logged, "GET /tasks/nope-does-not-exist 404") {
		t.Errorf("expected log to contain %q, got:\n%s", "GET /tasks/nope-does-not-exist 404", logged)
	}

	buf.Reset()
	disabledHandler := newHandler(db, 300)
	disabledSrv := httptest.NewServer(disabledHandler)
	defer disabledSrv.Close()

	resp, err = http.Get(disabledSrv.URL + "/tasks")
	if err != nil {
		t.Fatalf("GET /tasks: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Get(disabledSrv.URL + "/tasks/nope-does-not-exist")
	if err != nil {
		t.Fatalf("GET /tasks/nope-does-not-exist: %v", err)
	}
	resp.Body.Close()

	if strings.Contains(buf.String(), "GET /tasks") {
		t.Errorf("expected no request logs when disabled, got:\n%s", buf.String())
	}
}

func TestLogRequestsPanicSuppression(t *testing.T) {
	var buf bytes.Buffer
	origWriter := log.Writer()
	origFlags := log.Flags()
	defer func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
	}()
	log.SetOutput(&buf)
	log.SetFlags(0)

	panicHandler := withRequestLogging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("crash")
	}))

	func() {
		defer func() {
			_ = recover()
		}()
		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()
		panicHandler.ServeHTTP(rec, req)
	}()

	if strings.Contains(buf.String(), "200") {
		t.Errorf("panicked request must not be logged as 200 OK, got:\n%s", buf.String())
	}
}
