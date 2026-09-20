package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebUIBulkKick(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, `id="kick-buried-btn"`) {
		t.Fatal("expected #kick-buried-btn in web/index.html")
	}
	if !strings.Contains(ui, `onclick="openKickModal()"`) {
		t.Fatal("expected #kick-buried-btn to trigger openKickModal()")
	}
	if !strings.Contains(ui, `aria-haspopup="dialog"`) {
		t.Fatal("expected #kick-buried-btn to declare aria-haspopup=dialog")
	}
	if !strings.Contains(ui, `<dialog id="kick-modal"`) {
		t.Fatal("expected #kick-modal dialog in web/index.html")
	}
	if !strings.Contains(ui, `data-state="closed"`) {
		t.Fatal("expected #kick-modal to start with data-state=closed")
	}
	if !strings.Contains(ui, `id="kick-confirm-btn"`) {
		t.Fatal("expected #kick-confirm-btn in web/index.html")
	}
	if !strings.Contains(ui, `onclick="confirmKick()"`) {
		t.Fatal("expected #kick-confirm-btn to trigger confirmKick()")
	}
	if !strings.Contains(ui, `id="kick-cancel-btn"`) {
		t.Fatal("expected #kick-cancel-btn in web/index.html")
	}
	if !strings.Contains(ui, `onclick="closeKickModal()"`) {
		t.Fatal("expected #kick-cancel-btn to trigger closeKickModal()")
	}
	if !strings.Contains(ui, "#kick-modal, #purge-modal") {
		t.Fatal("expected #kick-modal styling in stylesheet")
	}
	if !strings.Contains(ui, "#kick-modal::backdrop") {
		t.Fatal("expected #kick-modal::backdrop styling in stylesheet")
	}
	if !strings.Contains(ui, `'/tasks/kick'`) && !strings.Contains(ui, `"/tasks/kick"`) {
		t.Fatal("expected /tasks/kick API endpoint call in web/index.html")
	}
	if !strings.Contains(ui, "await finishTaskTransition()") {
		t.Fatal("expected confirmKick to refresh queue and task via finishTaskTransition")
	}
	if !strings.Contains(ui, "updateKickBuriedButton") {
		t.Fatal("expected updateKickBuriedButton helper in web/index.html")
	}
	if !strings.Contains(ui, "(statusFilter === 'buried') || (buriedCount > 0)") {
		t.Fatal("expected button visibility condition checking status filter and buried count")
	}
}

func TestServerBulkKickIntegration(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	for _, id := range []string{"t1", "t2"} {
		_, err := db.rw.Exec(
			"INSERT INTO tasks (id, project, status, body, priority, claim_count, worker, created_at) VALUES (?, 'proj-kick', 'buried', 'task body', 1, 1, 'w1', 1000)",
			id,
		)
		if err != nil {
			t.Fatalf("insert buried task %s: %v", id, err)
		}
	}

	kickResp, err := http.Post(srv.URL+"/tasks/kick?project=proj-kick", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post /tasks/kick: %v", err)
	}
	defer kickResp.Body.Close()
	if kickResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /tasks/kick, got %d", kickResp.StatusCode)
	}

	statsResp, err := http.Get(srv.URL + "/stats?project=proj-kick")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	defer statsResp.Body.Close()

	var st struct {
		Buried  int `json:"buried"`
		Pending int `json:"pending"`
	}
	if err := json.NewDecoder(statsResp.Body).Decode(&st); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if st.Buried != 0 {
		t.Errorf("expected 0 buried tasks, got %d", st.Buried)
	}
	if st.Pending != 2 {
		t.Errorf("expected 2 pending tasks, got %d", st.Pending)
	}
}
