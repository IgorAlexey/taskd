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
	if _, err := db.rw.Exec("UPDATE tasks SET status='leased', worker='w1', lease_expires=unixepoch()+300 WHERE body='task 0'"); err != nil {
		t.Fatalf("lease task 0 failed: %v", err)
	}
	if _, err := db.rw.Exec("UPDATE tasks SET status='leased', worker='w2', lease_expires=unixepoch()-10 WHERE body='task 1'"); err != nil {
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
		if _, err := db.rw.Exec("INSERT INTO tasks (id, body, project, status) VALUES (?, ?, 'p-big', ?)",
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

	srv := httptest.NewServer(newHandlerWithCORS(db, 300, 0, "*"))
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

func TestSummaryLine(t *testing.T) {
	rockets := strings.Repeat("\U0001F680", 60)
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"multi line", "first line\nsecond line", "first line"},
		{"crlf", "first line\r\nsecond line", "first line"},
		{"leading blank lines", "\n\n  Indented title\nrest", "Indented title"},
		{"truncated", strings.Repeat("A", 80) + "\nsecond", strings.Repeat("A", 50) + "\u2026"},
		{"exactly 50 runes", strings.Repeat("A", 50) + "\nsecond", strings.Repeat("A", 50)},
		{"exactly 51 runes", strings.Repeat("A", 51) + "\nsecond", strings.Repeat("A", 50) + "\u2026"},
		{"exactly 50 runes crlf", strings.Repeat("A", 50) + "\r\nsecond", strings.Repeat("A", 50)},
		{"exactly 50 emoji", strings.Repeat("\U0001F680", 50) + "\nsecond", strings.Repeat("\U0001F680", 50)},
		{"exactly 51 emoji", strings.Repeat("\U0001F680", 51) + "\nsecond", strings.Repeat("\U0001F680", 50) + "\u2026"},
		{"runes not bytes", "x" + rockets + "\nsecond line", "x" + strings.Repeat("\U0001F680", 49) + "\u2026"},
		{"trailing spaces kept", "title   \nrest", "title   "},
		{"empty", "", ""},
		{"whitespace only", "\n \n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := summaryLine(tc.body); got != tc.want {
				t.Fatalf("summaryLine(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestListFieldsSummaryProjection(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	body := "summary headline\nrest of the body"
	if code, resp := post(t, srv.URL+"/tasks", map[string]any{
		"project": "proj",
		"body":    body,
	}); code != http.StatusCreated {
		t.Fatalf("create task: got %d, body %s", code, resp)
	}

	code, resp := do(t, http.MethodGet, srv.URL+"/tasks?fields=id,summary", nil)
	if code != http.StatusOK {
		t.Fatalf("GET fields=id,summary expected 200, got %d: %s", code, resp)
	}
	var items []map[string]any
	if err := json.Unmarshal(resp, &items); err != nil {
		t.Fatalf("unmarshal expected json array: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if len(items[0]) != 2 || items[0]["id"] == nil {
		t.Fatalf("expected only id and summary keys, got %+v", items[0])
	}
	if items[0]["summary"] != "summary headline" {
		t.Fatalf("expected summary %q, got %+v", "summary headline", items[0]["summary"])
	}
	if _, ok := items[0]["body"]; ok {
		t.Fatalf("unexpected body field in summary projection: %+v", items[0])
	}

	code, resp = do(t, http.MethodGet, srv.URL+"/tasks", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /tasks expected 200, got %d: %s", code, resp)
	}
	var full []map[string]any
	if err := json.Unmarshal(resp, &full); err != nil {
		t.Fatalf("unmarshal expected json array: %v", err)
	}
	if len(full) != 1 {
		t.Fatalf("expected 1 item, got %d", len(full))
	}
	if _, ok := full[0]["summary"]; ok {
		t.Fatalf("unexpected summary field in full list: %+v", full[0])
	}

	code, resp = do(t, http.MethodGet, srv.URL+"/tasks?fields=summary,body", nil)
	if code != http.StatusOK {
		t.Fatalf("GET fields=summary,body expected 200, got %d: %s", code, resp)
	}
	var both []map[string]any
	if err := json.Unmarshal(resp, &both); err != nil {
		t.Fatalf("unmarshal expected json array: %v", err)
	}
	if len(both) != 1 || len(both[0]) != 2 {
		t.Fatalf("expected only summary and body keys, got %+v", both)
	}
	if both[0]["summary"] != "summary headline" || both[0]["body"] != body {
		t.Fatalf("expected summary and full body, got %+v", both[0])
	}

	if c, _ := do(t, http.MethodGet, srv.URL+"/tasks?fields=id,summaries", nil); c != http.StatusBadRequest {
		t.Fatalf("GET fields=id,summaries expected 400, got %d", c)
	}
}

func TestListSummaryPrefixMatchesFullBody(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	bodies := map[string]string{
		"b-exact-50":      strings.Repeat("A", 50) + "\nsecond line",
		"b-exact-51":      strings.Repeat("A", 51) + "\nsecond line",
		"b-crlf-50":       strings.Repeat("B", 50) + "\r\nsecond line",
		"b-crlf-51":       strings.Repeat("B", 51) + "\r\nsecond line",
		"b-emoji-50":      strings.Repeat("\U0001F680", 50) + "\nsecond line",
		"b-emoji-51":      strings.Repeat("\U0001F680", 51) + "\nsecond line",
		"b-emoji-tail":    "x" + strings.Repeat("\U0001F680", 60),
		"b-leading-blank": "\n\n   " + strings.Repeat("C", 51) + "\nrest",
		"b-trailing-sp":   "title   \nrest",
		"b-whitespace":    " \t\r\n \n",
		"b-short":         "short title",
		"b-huge":          strings.Repeat("D", 4096),
	}
	for id, body := range bodies {
		if _, err := db.rw.Exec("INSERT INTO tasks (id, asset_path, status, body, priority, project) VALUES (?, '', 'pending', ?, 3, 'sumproj')", id, body); err != nil {
			t.Fatalf("insert %s failed: %v", id, err)
		}
	}

	code, resp := do(t, http.MethodGet, srv.URL+"/tasks?project=sumproj&limit=100&fields=id,summary", nil)
	if code != http.StatusOK {
		t.Fatalf("GET fields=id,summary expected 200, got %d: %s", code, resp)
	}
	var items []map[string]any
	if err := json.Unmarshal(resp, &items); err != nil {
		t.Fatalf("unmarshal summary projection failed: %v", err)
	}
	if len(items) != len(bodies) {
		t.Fatalf("expected %d items, got %d", len(bodies), len(items))
	}
	for _, item := range items {
		id, _ := item["id"].(string)
		body, ok := bodies[id]
		if !ok {
			t.Fatalf("unexpected task id %q in response", id)
		}
		if _, ok := item["body"]; ok {
			t.Fatalf("task %q unexpectedly carries a body: %+v", id, item)
		}
		if want := summaryLine(body); item["summary"] != want {
			t.Fatalf("task %q summary = %+v, want %q", id, item["summary"], want)
		}
	}

	code, resp = do(t, http.MethodGet, srv.URL+"/tasks?project=sumproj&limit=100&fields=id,summary,body", nil)
	if code != http.StatusOK {
		t.Fatalf("GET fields=id,summary,body expected 200, got %d: %s", code, resp)
	}
	var withBody []map[string]any
	if err := json.Unmarshal(resp, &withBody); err != nil {
		t.Fatalf("unmarshal summary+body projection failed: %v", err)
	}
	if len(withBody) != len(bodies) {
		t.Fatalf("expected %d items, got %d", len(bodies), len(withBody))
	}
	for _, item := range withBody {
		id, _ := item["id"].(string)
		body, ok := bodies[id]
		if !ok {
			t.Fatalf("unexpected task id %q in response", id)
		}
		if item["body"] != body {
			t.Fatalf("task %q body was truncated: %+v", id, item["body"])
		}
		if want := summaryLine(body); item["summary"] != want {
			t.Fatalf("task %q summary with body = %+v, want %q", id, item["summary"], want)
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

	respProj, err := http.Get(srv.URL + "/tasks?limit=200&fields=id,status,project,priority,claim_count,worker,asset_path,summary")
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
	wantSummary := strings.Repeat("A", 50) + "\u2026"
	for i, item := range items {
		if item["summary"] != wantSummary {
			t.Fatalf("item %d summary = %+v, want %q", i, item["summary"], wantSummary)
		}
		if _, ok := item["body"]; ok {
			t.Fatalf("item %d unexpectedly carries a body: %+v", i, item)
		}
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
