package taskd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebUIServe(t *testing.T) {
	db, err := openDB(t.TempDir()+"/test.db", 300)
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui")
	if err != nil {
		t.Fatalf("GET /ui: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, err := noFollow.Get(srv.URL + "/ui/"); err != nil {
		t.Fatalf("GET /ui/: %v", err)
	} else if res.StatusCode != http.StatusPermanentRedirect || res.Header.Get("Location") != "/ui" {
		t.Errorf("GET /ui/ = %d %q, want 308 to /ui", res.StatusCode, res.Header.Get("Location"))
	}
	for _, p := range []string{"/ui/oat.min.css", "/ui/oat.min.js"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", p, res.StatusCode)
		}
	}
}
