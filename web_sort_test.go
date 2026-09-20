package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWebUITableSorting(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `class="sortable"`) {
		t.Fatal("expected class=\"sortable\" on table headers in web/index.html")
	}
	if !strings.Contains(ui, "sort(") {
		t.Fatal("expected sort( handler in web/index.html")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/table_sort.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("table sort harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("table sort harness failed: %v", err)
	}

	var got struct {
		URL1 string `json:"url1"`
		URL2 string `json:"url2"`
		URL3 string `json:"url3"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if !strings.Contains(got.URL1, "sort=priority") || !strings.Contains(got.URL1, "order=asc") {
		t.Errorf("url1 = %q, want sort=priority&order=asc", got.URL1)
	}
	if !strings.Contains(got.URL2, "sort=priority") || !strings.Contains(got.URL2, "order=desc") {
		t.Errorf("url2 = %q, want sort=priority&order=desc", got.URL2)
	}
	if !strings.Contains(got.URL3, "sort=claim_count") || !strings.Contains(got.URL3, "order=asc") {
		t.Errorf("url3 = %q, want sort=claim_count&order=asc", got.URL3)
	}
}
func TestWebUISortURLState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/table_sort_url.js", "web/index.html").CombinedOutput()
	if err != nil {
		t.Fatalf("table sort url harness failed: %v\noutput:\n%s", err, out)
	}

	var got struct {
		InitialAriaSort  string `json:"initialAriaSort"`
		InitialLoadedURL string `json:"initialLoadedURL"`
		ClickedSearch1   string `json:"clickedSearch1"`
		ClickedAriaSort1 string `json:"clickedAriaSort1"`
		ClickedSearch2   string `json:"clickedSearch2"`
		ClickedAriaSort2 string `json:"clickedAriaSort2"`
		ReplaceCount     int    `json:"replaceCount"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.InitialAriaSort != "descending" {
		t.Errorf("initial aria-sort = %q, want descending", got.InitialAriaSort)
	}
	if !strings.Contains(got.InitialLoadedURL, "sort=priority") || !strings.Contains(got.InitialLoadedURL, "order=desc") {
		t.Errorf("initial loaded URL = %q, want sort=priority&order=desc", got.InitialLoadedURL)
	}
	if !strings.Contains(got.ClickedSearch1, "sort=priority") || !strings.Contains(got.ClickedSearch1, "order=asc") {
		t.Errorf("clickedSearch1 = %q, want sort=priority&order=asc", got.ClickedSearch1)
	}
	if got.ClickedAriaSort1 != "ascending" {
		t.Errorf("clickedAriaSort1 = %q, want ascending", got.ClickedAriaSort1)
	}
	if !strings.Contains(got.ClickedSearch2, "sort=claim_count") || !strings.Contains(got.ClickedSearch2, "order=asc") {
		t.Errorf("clickedSearch2 = %q, want sort=claim_count&order=asc", got.ClickedSearch2)
	}
	if got.ClickedAriaSort2 != "ascending" {
		t.Errorf("clickedAriaSort2 = %q, want ascending", got.ClickedAriaSort2)
	}
	if got.ReplaceCount < 2 {
		t.Errorf("replaceCount = %d, want at least 2", got.ReplaceCount)
	}
}

func TestServerTasksSort(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	postTask := func(id, body string, prio int) {
		payload, _ := json.Marshal(map[string]any{"id": id, "body": body, "priority": prio, "project": "p"})
		resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("post task: %v", err)
		}
		resp.Body.Close()
	}

	postTask("t-mid", "mid", 2)
	postTask("t-high", "high", 1)
	postTask("t-low", "low", 3)

	getSorted := func(sortParam, orderParam string) []string {
		resp, err := http.Get(srv.URL + "/tasks?project=p&sort=" + sortParam + "&order=" + orderParam)
		if err != nil {
			t.Fatalf("get sorted: %v", err)
		}
		defer resp.Body.Close()
		var list []taskItem
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var ids []string
		for _, item := range list {
			ids = append(ids, item.ID)
		}
		return ids
	}

	asc := getSorted("priority", "asc")
	wantAsc := []string{"t-high", "t-mid", "t-low"}
	if !slices.Equal(asc, wantAsc) {
		t.Errorf("sort priority asc = %v, want %v", asc, wantAsc)
	}

	desc := getSorted("priority", "desc")
	wantDesc := []string{"t-low", "t-mid", "t-high"}
	if !slices.Equal(desc, wantDesc) {
		t.Errorf("sort priority desc = %v, want %v", desc, wantDesc)
	}
}

func TestWebUITableSortKeyboard(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}
	out, err := exec.Command(node, "testdata/table_sort_keyboard.js", "web/index.html").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("table sort keyboard harness failed: %v\nstderr:\n%s", err, exitErr.Stderr)
		}
		t.Fatalf("table sort keyboard harness failed: %v", err)
	}

	var got struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out, &got); err != nil || !got.OK {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}
}
