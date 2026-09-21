package taskd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireToken(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 5)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(requireToken(newHandler(db, 300), "s3cret"))
	defer srv.Close()

	get := func(path string, set func(*http.Request)) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if set != nil {
			set(req)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	bearer := func(v string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+v) }
	}
	cookie := func(v string) func(*http.Request) {
		return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "taskd_token", Value: v}) }
	}

	if res := get("/tasks", nil); res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("no token: %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
	if res := get("/tasks", bearer("wrong")); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: %d", res.StatusCode)
	}
	if res := get("/tasks", cookie("wrong")); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong cookie: %d", res.StatusCode)
	}
	if res := get("/tasks", bearer("s3cret")); res.StatusCode != http.StatusOK {
		t.Fatalf("bearer: %d", res.StatusCode)
	}
	if res := get("/tasks", cookie("s3cret")); res.StatusCode != http.StatusOK {
		t.Fatalf("cookie: %d", res.StatusCode)
	}
	for _, p := range []string{"/", "/health", "/static/oat.min.css"} {
		if res := get(p, nil); res.StatusCode != http.StatusOK {
			t.Fatalf("%s should be open: %d", p, res.StatusCode)
		}
	}
	if res := get("/api/events", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/events without token: %d", res.StatusCode)
	}
	if res := get("/static/../tasks", nil); res.StatusCode == http.StatusOK {
		t.Fatal("/static/../tasks was served without a token")
	}
}

func TestCLIToken(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 5)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(requireToken(newHandler(db, 300), "s3cret"))
	defer srv.Close()
	t.Setenv("TASKD_URL", srv.URL)

	var out, errb bytes.Buffer
	t.Setenv("TASKD_TOKEN", "")
	if code := runClient(&out, &errb, strings.NewReader(""), []string{"list"}); code != 1 || !strings.Contains(errb.String(), "requires TASKD_TOKEN") {
		t.Fatalf("without token: exit %d, stderr %q", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	t.Setenv("TASKD_TOKEN", "wrong")
	if code := runClient(&out, &errb, strings.NewReader(""), []string{"list"}); code != 1 || !strings.Contains(errb.String(), "rejected TASKD_TOKEN") {
		t.Fatalf("wrong token: exit %d, stderr %q", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	t.Setenv("TASKD_TOKEN", "s3cret")
	if code := runClient(&out, &errb, strings.NewReader(""), []string{"list"}); code != 0 {
		t.Fatalf("with token: exit %d, stderr %q", code, errb.String())
	}
}
