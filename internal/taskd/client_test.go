package taskd

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 5)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	t.Setenv("TASKD_URL", srv.URL)
	t.Setenv("TASKD_WORKER", "w1")
	run := func(stdin string, args ...string) (int, string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := runClient(&out, &errb, strings.NewReader(stdin), args)
		return code, out.String(), errb.String()
	}
	want := func(code int, args ...string) string {
		t.Helper()
		got, out, errs := run("", args...)
		if got != code {
			t.Fatalf("%v: exit %d, want %d; stdout %q stderr %q", args, got, code, out, errs)
		}
		return out
	}

	if out := want(0, "add", "first", "-project", "p", "-p", "2"); !strings.Contains(out, `"id":1`) {
		t.Fatalf("add printed %q", out)
	}
	if code, out, _ := run("piped\nbody\n", "add", "-q", "-project", "p"); code != 0 || out != "2\n" {
		t.Fatalf("add from stdin: exit %d, stdout %q", code, out)
	}
	if code, out, _ := run("", "add", "-project", "p"); code != 2 || out != "" {
		t.Fatalf("add without a body: exit %d, stdout %q", code, out)
	}
	if code, out, errs := run("", "add", "-project", "p", "-p", "-1", "--", "-x"); code != 1 || out != "" || !strings.Contains(errs, "priority") {
		t.Fatalf("-p -1 must reach the daemon and be refused there: exit %d, stdout %q, stderr %q", code, out, errs)
	}
	if code, out, _ := run("", "add", "-q", "-project", "p", "--", "-not -a -flag"); code != 0 || out != "3\n" {
		t.Fatalf("body after --: exit %d, stdout %q", code, out)
	}
	if code, out, _ := run("crlf\r\n", "add", "-q", "-project", "p", "-"); code != 0 || out != "4\n" {
		t.Fatalf("add from - : exit %d, stdout %q", code, out)
	}
	if out := want(0, "show", "4"); !strings.Contains(out, `"body":"crlf"`) {
		t.Fatalf("CRLF was not trimmed: %q", out)
	}
	if code, out, errs := run("", "help", "claim"); code != 0 || errs != "" || !strings.Contains(out, "-wait") || strings.Contains(out, "-after") {
		t.Fatalf("help claim: exit %d, stdout %q, stderr %q", code, out, errs)
	}
	if code, _, errs := run("", "done", "1", "-result", "--"); code != 2 || !strings.Contains(errs, "not JSON") {
		t.Fatalf("-- as a flag value: exit %d, stderr %q", code, errs)
	}
	if code, out, _ := run("", "help", "serve"); code != 0 || !strings.Contains(out, "-addr") {
		t.Fatalf("help serve: exit %d, stdout %q", code, out)
	}

	var task struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(want(0, "claim", "-project", "p")), &task); err != nil || task.ID != 1 || task.Body != "first" {
		t.Fatalf("claim printed task %+v (%v)", task, err)
	}
	if out := want(0, "claim", "-q"); out != "2\n" {
		t.Fatalf("claim -q printed %q", out)
	}
	if out := want(0, "claim", "4", "-q"); out != "4\n" {
		t.Fatalf("claim by id printed %q", out)
	}
	want(0, "claim", "3", "-q")
	if code, out, errs := run("", "claim"); code != exitNoTask || out != "" || errs != "" {
		t.Fatalf("empty claim: exit %d, stdout %q, stderr %q", code, out, errs)
	}
	if out := want(0, "touch", "1"); !strings.Contains(out, `"lease_expires"`) {
		t.Fatalf("touch printed %q", out)
	}
	want(0, "note", "1", "found it")
	if out := want(0, "show", "1"); !strings.Contains(out, `"text":"found it"`) || !strings.Contains(out, `"author":"w1"`) {
		t.Fatalf("show printed %q", out)
	}
	if out := want(0, "done", "1", "-result", `{"commit":"abc"}`); out != "" {
		t.Fatalf("done printed %q", out)
	}
	if code, out, errs := run("", "done", "1"); code != 1 || out != "" || !strings.HasPrefix(errs, "taskd: ") {
		t.Fatalf("done again: exit %d, stdout %q, stderr %q", code, out, errs)
	}
	want(0, "release", "2")
	var list []struct {
		Primitives json.RawMessage `json:"primitives"`
	}
	if err := json.Unmarshal([]byte(want(0, "list", "-status", "done")), &list); err != nil || len(list) != 1 || string(list[0].Primitives) != `{"commit":"abc"}` {
		t.Fatalf("list printed %+v (%v)", list, err)
	}

	if code, _, errs := run("", "nope"); code != 2 || !strings.Contains(errs, "unknown command") {
		t.Fatalf("unknown command: exit %d, stderr %q", code, errs)
	}
	var out, errb bytes.Buffer
	if code := runClient(&out, &errb, strings.NewReader(""), []string{"show", "1", "-url", "http://127.0.0.1:1"}); code != 1 || !strings.HasPrefix(errb.String(), "taskd: http://127.0.0.1:1: ") || strings.Contains(errb.String(), `Get "`) {
		t.Fatalf("unreachable: exit %d, stderr %q", code, errb.String())
	}
}
