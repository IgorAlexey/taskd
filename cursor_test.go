package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"testing"
)

func cursorServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	srv := httptest.NewServer(newHandler(db, 300))
	t.Cleanup(srv.Close)
	return srv
}

func cursorTask(t *testing.T, srvURL, project, body string, priority int) string {
	t.Helper()
	code, raw := post(t, srvURL+"/tasks", map[string]any{
		"project":  project,
		"body":     body,
		"priority": priority,
	})
	if code != http.StatusCreated {
		t.Fatalf("create %s expected 201, got %d: %s", body, code, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("unmarshal created %s: %v", body, err)
	}
	return created.ID
}

func cursorList(t *testing.T, url string) (int, []taskItem, string, string, []byte) {
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
			t.Fatalf("unmarshal %s: %v (body %s)", url, err, data)
		}
	}
	return resp.StatusCode, tasks, resp.Header.Get("X-Total-Count"), resp.Header.Get("X-Next-Cursor"), data
}

func itemBodies(items []taskItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Body)
	}
	return out
}

func itemIDs(items []taskItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestListAfterCursorSurvivesClaims(t *testing.T) {
	srv := cursorServer(t)

	ids := map[string]string{}
	for i := 1; i <= 6; i++ {
		body := fmt.Sprintf("pgx-%d", i)
		ids[body] = cursorTask(t, srv.URL, "pgx", body, 5)
	}

	base := srv.URL + "/tasks?project=pgx&status=pending&limit=3"
	code, page1, total, cursor, raw := cursorList(t, base)
	if code != http.StatusOK {
		t.Fatalf("page 1 expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(page1); !slices.Equal(got, []string{"pgx-1", "pgx-2", "pgx-3"}) {
		t.Fatalf("page 1 bodies %v", got)
	}
	if total != "6" {
		t.Fatalf("page 1 X-Total-Count %q, want 6", total)
	}
	if cursor == "" {
		t.Fatal("page 1 missing X-Next-Cursor")
	}

	for _, body := range []string{"pgx-1", "pgx-2"} {
		code, raw := post(t, srv.URL+"/tasks/"+ids[body]+"/claim", map[string]any{"worker": "w"})
		if code != http.StatusOK {
			t.Fatalf("claim %s expected 200, got %d: %s", body, code, raw)
		}
	}

	code, page2, total2, _, raw := cursorList(t, base+"&after="+url.QueryEscape(cursor))
	if code != http.StatusOK {
		t.Fatalf("page 2 expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(page2); !slices.Equal(got, []string{"pgx-4", "pgx-5", "pgx-6"}) {
		t.Fatalf("page 2 bodies %v, want pgx-4..6", got)
	}
	if total2 != "" {
		t.Fatalf("page 2 set X-Total-Count %q; a cursor page must not report a total", total2)
	}

	seen := append(itemIDs(page1), itemIDs(page2)...)
	for body, id := range ids {
		if !slices.Contains(seen, id) {
			t.Fatalf("%s (%s) missing from the union of both pages", body, id)
		}
	}
	for _, body := range []string{"pgx-4", "pgx-5"} {
		if !slices.Contains(itemBodies(page2), body) {
			t.Fatalf("%s stayed pending but was skipped by the cursor", body)
		}
	}
}

func TestListAfterCursorInvalid(t *testing.T) {
	srv := cursorServer(t)
	cursorTask(t, srv.URL, "bad-cursor", "only", 5)

	enc := func(payload string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(payload))
	}
	cases := []string{
		"garbage",
		"!!!!",
		"",
		enc(`{"p":1,"r":1}`) + "trailing",
		enc(`{"p":1,"r":1}{"p":2,"r":2}`),
		enc(`{"p":1,"r":1,"x":2}`),
		enc(`{"p":-1,"r":1}`),
		enc(`{"p":0,"r":0}`),
		enc(`[1,2]`),
		enc(`{"p":5,"r":1}`),
		enc(`{"p":5,"r":1,"f":"not-the-real-fingerprint"}`),
	}
	if padded := base64.StdEncoding.EncodeToString([]byte(`{"p":1,"r":1}`)); padded != enc(`{"p":1,"r":1}`) {
		cases = append(cases, padded)
	}

	for _, bad := range cases {
		target := srv.URL + "/tasks?project=bad-cursor&after=" + url.QueryEscape(bad)
		code, _, _, _, raw := cursorList(t, target)
		if code != http.StatusBadRequest {
			t.Fatalf("after=%q expected 400, got %d: %s", bad, code, raw)
		}
		assertErrorBody(t, raw, "invalid after")
	}
}

func TestListAfterRejectsOffset(t *testing.T) {
	srv := cursorServer(t)
	for i := 1; i <= 3; i++ {
		cursorTask(t, srv.URL, "mix", fmt.Sprintf("mix-%d", i), 5)
	}

	base := srv.URL + "/tasks?project=mix&limit=1"
	code, _, _, cursor, raw := cursorList(t, base)
	if code != http.StatusOK {
		t.Fatalf("first page expected 200, got %d: %s", code, raw)
	}
	if cursor == "" {
		t.Fatal("first page missing X-Next-Cursor")
	}

	code, _, _, _, raw = cursorList(t, base+"&after="+url.QueryEscape(cursor)+"&offset=1")
	if code != http.StatusBadRequest {
		t.Fatalf("after with offset expected 400, got %d: %s", code, raw)
	}
	assertErrorBody(t, raw, "after and offset are mutually exclusive")

	code, _, _, _, raw = cursorList(t, base+"&after="+url.QueryEscape(cursor)+"&offset=abc")
	if code != http.StatusBadRequest {
		t.Fatalf("after with unparseable offset expected 400, got %d: %s", code, raw)
	}
	assertErrorBody(t, raw, "after and offset are mutually exclusive")

	code, items, _, _, raw := cursorList(t, base+"&offset=1")
	if code != http.StatusOK {
		t.Fatalf("offset alone expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(items); !slices.Equal(got, []string{"mix-2"}) {
		t.Fatalf("offset alone bodies %v, want [mix-2]", got)
	}
}

func TestListNextCursorWalksWholeQueue(t *testing.T) {
	srv := cursorServer(t)
	for i := 1; i <= 4; i++ {
		cursorTask(t, srv.URL, "walk", fmt.Sprintf("low-%d", i), 5)
	}
	for i := 1; i <= 3; i++ {
		cursorTask(t, srv.URL, "walk", fmt.Sprintf("high-%d", i), 1)
	}

	code, all, _, _, raw := cursorList(t, srv.URL+"/tasks?project=walk&limit=100")
	if code != http.StatusOK {
		t.Fatalf("full listing expected 200, got %d: %s", code, raw)
	}

	base := srv.URL + "/tasks?project=walk&limit=2"
	var walked []string
	cursor := ""
	pages := 0
	for {
		if pages > 10 {
			t.Fatalf("cursor walk did not terminate after %d pages (collected %v)", pages, walked)
		}
		target := base
		if cursor != "" {
			target += "&after=" + url.QueryEscape(cursor)
		}
		code, page, _, next, raw := cursorList(t, target)
		if code != http.StatusOK {
			t.Fatalf("walk page %d expected 200, got %d: %s", pages, code, raw)
		}
		pages++
		walked = append(walked, itemIDs(page)...)
		if next == "" {
			if len(page) == 0 {
				t.Fatalf("walk page %d was empty: the short page before it should have ended the walk", pages)
			}
			if len(page) == 2 {
				t.Fatalf("walk page %d was full (%d rows) but carried no X-Next-Cursor", pages, len(page))
			}
			break
		}
		if len(page) != 2 {
			t.Fatalf("walk page %d was short (%d rows) yet carried X-Next-Cursor %q", pages, len(page), next)
		}
		cursor = next
	}
	if pages != 4 {
		t.Fatalf("cursor walk took %d pages, want 4 (7 rows at limit=2, final page short and cursorless)", pages)
	}

	want := itemIDs(all)
	if !slices.Equal(walked, want) {
		t.Fatalf("cursor walk visited %v, want %v", walked, want)
	}
	seen := map[string]bool{}
	for _, id := range walked {
		if seen[id] {
			t.Fatalf("cursor walk returned %s twice: %v", id, walked)
		}
		seen[id] = true
	}
}

func TestListEmptyPageHasNoCursor(t *testing.T) {
	srv := cursorServer(t)
	cursorTask(t, srv.URL, "present", "present-1", 5)

	code, items, total, cursor, raw := cursorList(t, srv.URL+"/tasks?project=absent")
	if code != http.StatusOK {
		t.Fatalf("empty listing expected 200, got %d: %s", code, raw)
	}
	if len(items) != 0 {
		t.Fatalf("empty listing returned %d rows", len(items))
	}
	if total != "0" {
		t.Fatalf("empty listing X-Total-Count %q, want 0", total)
	}
	if cursor != "" {
		t.Fatalf("empty listing set X-Next-Cursor %q", cursor)
	}
}

func TestListNextCursorFullFinalPageYieldsEmptyPage(t *testing.T) {
	srv := cursorServer(t)
	for i := 1; i <= 4; i++ {
		cursorTask(t, srv.URL, "even", fmt.Sprintf("even-%d", i), 5)
	}

	base := srv.URL + "/tasks?project=even&limit=2"
	code, page1, _, cursor1, raw := cursorList(t, base)
	if code != http.StatusOK {
		t.Fatalf("page 1 expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(page1); !slices.Equal(got, []string{"even-1", "even-2"}) {
		t.Fatalf("page 1 bodies %v, want [even-1 even-2]", got)
	}
	if cursor1 == "" {
		t.Fatal("page 1 is full and must carry X-Next-Cursor")
	}

	code, page2, _, cursor2, raw := cursorList(t, base+"&after="+url.QueryEscape(cursor1))
	if code != http.StatusOK {
		t.Fatalf("page 2 expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(page2); !slices.Equal(got, []string{"even-3", "even-4"}) {
		t.Fatalf("page 2 bodies %v, want [even-3 even-4]", got)
	}
	if cursor2 == "" {
		t.Fatal("page 2 filled the limit exactly at the end of the set and still must carry X-Next-Cursor")
	}

	code, page3, _, cursor3, raw := cursorList(t, base+"&after="+url.QueryEscape(cursor2))
	if code != http.StatusOK {
		t.Fatalf("page 3 expected 200, got %d: %s", code, raw)
	}
	if len(page3) != 0 {
		t.Fatalf("page 3 returned %d rows, want the empty page that a full final page costs", len(page3))
	}
	if cursor3 != "" {
		t.Fatalf("empty page set X-Next-Cursor %q", cursor3)
	}
}

func TestListAfterCursorRejectsForeignFilters(t *testing.T) {
	srv := cursorServer(t)
	for i := 1; i <= 4; i++ {
		cursorTask(t, srv.URL, "alpha", fmt.Sprintf("alpha-%d", i), 5)
	}
	for i := 1; i <= 2; i++ {
		cursorTask(t, srv.URL, "beta", fmt.Sprintf("beta-%d", i), 5)
	}

	code, page1, _, cursor, raw := cursorList(t, srv.URL+"/tasks?project=alpha&status=pending&limit=1")
	if code != http.StatusOK {
		t.Fatalf("seed page expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(page1); !slices.Equal(got, []string{"alpha-1"}) {
		t.Fatalf("seed page bodies %v, want [alpha-1]", got)
	}
	if cursor == "" {
		t.Fatal("seed page missing X-Next-Cursor")
	}
	after := "&after=" + url.QueryEscape(cursor)

	foreign := []struct {
		name  string
		query string
	}{
		{"different project", "/tasks?project=beta&status=pending&limit=1"},
		{"different status", "/tasks?project=alpha&status=done&limit=1"},
		{"added q filter", "/tasks?project=alpha&status=pending&q=alpha&limit=1"},
	}
	for _, tc := range foreign {
		code, _, _, _, raw := cursorList(t, srv.URL+tc.query+after)
		if code != http.StatusBadRequest {
			t.Fatalf("%s expected 400, got %d: %s", tc.name, code, raw)
		}
		assertErrorBody(t, raw, "invalid after")
	}

	code, resized, _, _, raw := cursorList(t, srv.URL+"/tasks?project=alpha&status=pending&limit=2"+after)
	if code != http.StatusOK {
		t.Fatalf("same filters with a different limit expected 200, got %d: %s", code, raw)
	}
	if got := itemBodies(resized); !slices.Equal(got, []string{"alpha-2", "alpha-3"}) {
		t.Fatalf("resized page bodies %v, want [alpha-2 alpha-3]", got)
	}
}
