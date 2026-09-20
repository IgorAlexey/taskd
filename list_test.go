package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func listTotalCount(t *testing.T, url string) (int, []taskItem, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("http.Get %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll failed: %v", err)
	}
	var tasks []taskItem
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(data, &tasks); err != nil {
			t.Fatalf("unmarshal list %s failed: %v (%s)", url, err, data)
		}
	}
	return resp.StatusCode, tasks, resp.Header.Get("X-Total-Count")
}

func TestListTotalCountHeader(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		if code, body := post(t, srv.URL+"/tasks", map[string]any{
			"project": "p-total",
			"body":    fmt.Sprintf("task %d", i),
		}); code != http.StatusCreated {
			t.Fatalf("create task %d: got %d, body %s", i, code, body)
		}
	}
	if code, body := post(t, srv.URL+"/tasks", map[string]any{
		"project": "other",
		"body":    "not counted",
	}); code != http.StatusCreated {
		t.Fatalf("create other task: got %d, body %s", code, body)
	}

	code, tasks, total := listTotalCount(t, srv.URL+"/tasks?project=p-total&limit=1")
	if code != http.StatusOK {
		t.Fatalf("list expected 200, got %d", code)
	}
	if len(tasks) != 1 {
		t.Fatalf("list expected 1 row, got %d", len(tasks))
	}
	if total != "3" {
		t.Fatalf("list expected X-Total-Count: 3, got %q", total)
	}

	// The header counts the filtered set, not the whole table.
	if _, _, total := listTotalCount(t, srv.URL+"/tasks"); total != "4" {
		t.Fatalf("unfiltered list expected X-Total-Count: 4, got %q", total)
	}
	if _, _, total := listTotalCount(t, srv.URL+"/tasks?project=missing"); total != "0" {
		t.Fatalf("empty list expected X-Total-Count: 0, got %q", total)
	}
}

func TestListTotalCountMatchesFilters(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for i := 0; i < 4; i++ {
		if code, body := post(t, srv.URL+"/tasks", map[string]any{
			"project":  "p-filter",
			"body":     fmt.Sprintf("task %d", i),
			"priority": i % 2,
		}); code != http.StatusCreated {
			t.Fatalf("create task %d: got %d, body %s", i, code, body)
		}
	}
	// One task is leased, one has an expired lease and is pending again.
	if _, err := db.Exec("UPDATE tasks SET status='leased', worker='w1', lease_expires=unixepoch()+300 WHERE body='task 0'"); err != nil {
		t.Fatalf("lease task 0 failed: %v", err)
	}
	if _, err := db.Exec("UPDATE tasks SET status='leased', worker='w2', lease_expires=unixepoch()-10 WHERE body='task 1'"); err != nil {
		t.Fatalf("expire task 1 failed: %v", err)
	}

	for _, tc := range []struct {
		query string
		total string
	}{
		{"status=pending", "3"},
		{"status=leased", "1"},
		{"status=done", "0"},
		{"priority=1", "2"},
		{"worker=w1", "1"},
		{"status=pending&priority=0", "1"},
	} {
		code, tasks, total := listTotalCount(t, srv.URL+"/tasks?project=p-filter&limit=1000&"+tc.query)
		if code != http.StatusOK {
			t.Fatalf("list %s expected 200, got %d", tc.query, code)
		}
		if total != tc.total {
			t.Fatalf("list %s expected X-Total-Count: %s, got %q", tc.query, tc.total, total)
		}
		if got := fmt.Sprint(len(tasks)); got != total {
			t.Fatalf("list %s returned %s rows but reported %s", tc.query, got, total)
		}
	}
}

func TestListTotalCountBeyondDefaultLimit(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for i := 0; i < 150; i++ {
		status := "pending"
		if i < 120 {
			status = "done"
		}
		if _, err := db.Exec("INSERT INTO tasks (id, body, project, status) VALUES (?, ?, 'p-big', ?)",
			fmt.Sprintf("id-%03d", i), fmt.Sprintf("task %d", i), status); err != nil {
			t.Fatalf("insert task %d failed: %v", i, err)
		}
	}

	code, tasks, total := listTotalCount(t, srv.URL+"/tasks")
	if code != http.StatusOK {
		t.Fatalf("list expected 200, got %d", code)
	}
	if len(tasks) != 100 {
		t.Fatalf("list expected the default page of 100 rows, got %d", len(tasks))
	}
	if total != "150" {
		t.Fatalf("list expected X-Total-Count: 150, got %q", total)
	}

	// Offset pages report the same total so a caller can page to the end,
	// including an offset that lands past the last row.
	if _, tasks, total := listTotalCount(t, srv.URL+"/tasks?offset=100"); total != "150" || len(tasks) != 50 {
		t.Fatalf("offset page expected 50 rows and X-Total-Count: 150, got %d rows and %q", len(tasks), total)
	}
	if _, tasks, total := listTotalCount(t, srv.URL+"/tasks?offset=200"); total != "150" || len(tasks) != 0 {
		t.Fatalf("offset past the end expected 0 rows and X-Total-Count: 150, got %d rows and %q", len(tasks), total)
	}
	// A page exactly as long as the set still reports it once.
	if _, tasks, total := listTotalCount(t, srv.URL+"/tasks?limit=150"); total != "150" || len(tasks) != 150 {
		t.Fatalf("full page expected 150 rows and X-Total-Count: 150, got %d rows and %q", len(tasks), total)
	}
}

func TestListTotalCountExposedToBrowsers(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandlerWithCORS(db, 300, "*"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks")
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "X-Total-Count" {
		t.Fatalf("expected Access-Control-Expose-Headers: X-Total-Count, got %q", got)
	}
}
func TestListFieldsProjection(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	if code, body := post(t, srv.URL+"/tasks", map[string]any{
		"project":  "proj",
		"body":     "prompt body 1",
		"priority": 1,
	}); code != http.StatusCreated {
		t.Fatalf("create task: got %d, body %s", code, body)
	}

	code, resp := do(t, http.MethodGet, srv.URL+"/tasks?fields=id,status", nil)
	if code != http.StatusOK {
		t.Fatalf("GET fields=id,status expected 200, got %d: %s", code, resp)
	}
	var items []map[string]any
	if err := json.Unmarshal(resp, &items); err != nil {
		t.Fatalf("unmarshal expected json array: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(items[0]) != 2 || items[0]["id"] == nil || items[0]["status"] == nil {
		t.Fatalf("expected only id and status keys, got %+v", items[0])
	}
	if _, ok := items[0]["body"]; ok {
		t.Fatalf("unexpected body field in projection: %+v", items[0])
	}

	code, resp = do(t, http.MethodGet, srv.URL+"/tasks?columns=id,priority", nil)
	if code != http.StatusOK {
		t.Fatalf("GET columns=id,priority expected 200, got %d: %s", code, resp)
	}
	var colItems []map[string]any
	if err := json.Unmarshal(resp, &colItems); err != nil {
		t.Fatalf("unmarshal expected json array: %v", err)
	}
	if len(colItems) != 1 || len(colItems[0]) != 2 || colItems[0]["id"] == nil || colItems[0]["priority"] == nil {
		t.Fatalf("expected only id and priority keys, got %+v", colItems[0])
	}

	for _, invalid := range []string{"", "foo", "id,bad", "unknown"} {
		c, _ := do(t, http.MethodGet, srv.URL+"/tasks?fields="+invalid, nil)
		if c != http.StatusBadRequest {
			t.Fatalf("GET fields=%q expected 400, got %d", invalid, c)
		}
	}
}

func TestListFieldsPayloadSize200Tasks(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	fourKB := strings.Repeat("A", 4096)
	for i := 0; i < 200; i++ {
		if code, body := post(t, srv.URL+"/tasks", map[string]any{
			"project": "bench",
			"body":    fourKB,
		}); code != http.StatusCreated {
			t.Fatalf("create task %d: got %d, body %s", i, code, body)
		}
	}

	respDefault, err := http.Get(srv.URL + "/tasks?limit=1")
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer respDefault.Body.Close()
	var defaultItems []taskItem
	if err := json.NewDecoder(respDefault.Body).Decode(&defaultItems); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(defaultItems) != 1 || defaultItems[0].Body != fourKB {
		t.Fatalf("expected untruncated full body in default list")
	}

	respProj, err := http.Get(srv.URL + "/tasks?limit=200&fields=id,status,project,priority,claim_count,worker,asset_path")
	if err != nil {
		t.Fatalf("http.Get projected failed: %v", err)
	}
	defer respProj.Body.Close()
	if respProj.StatusCode != http.StatusOK {
		t.Fatalf("GET projected expected 200, got %d", respProj.StatusCode)
	}
	dataProj, err := io.ReadAll(respProj.Body)
	if err != nil {
		t.Fatalf("io.ReadAll projected failed: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(dataProj, &items); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(items) != 200 {
		t.Fatalf("expected 200 items, got %d", len(items))
	}
	if len(dataProj) >= 51200 {
		t.Fatalf("projected payload size %d expected under 51200", len(dataProj))
	}
}

func TestWebUISequentialAutoRefresh(t *testing.T) {
	ui := string(uiHTML)
	if strings.Contains(ui, "setInterval(loadAll") {
		t.Fatal("expected loadAll polled by refreshTick, not a bare setInterval")
	}
}
