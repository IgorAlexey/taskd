package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebUIURLState(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is required to run the web UI harness")
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/urlstate.js", "web/index.html").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("harness failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("harness failed: %v", err)
	}

	var got struct {
		Priority struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
			QueueCount   string `json:"queueCount"`
			List         string `json:"list"`
		}
		PriorityNegative struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
		}
		PriorityChange struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
			List         string `json:"list"`
		}
		PriorityRefuse struct {
			Search       string `json:"search"`
			ControlValue string `json:"controlValue"`
		}
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad harness output: %v\n%s", err, out)
	}

	if got.Priority.ControlValue != "6" {
		t.Errorf("priority boot control value = %q, want 6", got.Priority.ControlValue)
	}
	if !strings.Contains(got.Priority.Search, "priority=6") {
		t.Errorf("priority boot search = %q, want priority=6", got.Priority.Search)
	}
	if !strings.Contains(got.Priority.List, "&priority=6") {
		t.Errorf("priority boot list fetch = %q, want &priority=6", got.Priority.List)
	}
	if got.Priority.QueueCount != "Showing 1-1 of 1" {
		t.Errorf("priority queue count = %q, want Showing 1-1 of 1", got.Priority.QueueCount)
	}

	if got.PriorityNegative.ControlValue != "" {
		t.Errorf("priority negative control value = %q, want empty", got.PriorityNegative.ControlValue)
	}
	if strings.Contains(got.PriorityNegative.Search, "priority") {
		t.Errorf("priority negative search = %q, want priority omitted", got.PriorityNegative.Search)
	}

	if got.PriorityChange.ControlValue != "6" || !strings.Contains(got.PriorityChange.Search, "priority=6") {
		t.Errorf("priority change = %+v, want control 6 and priority=6 in search", got.PriorityChange)
	}
	if !strings.Contains(got.PriorityChange.List, "&priority=6") {
		t.Errorf("priority change list fetch = %q, want &priority=6", got.PriorityChange.List)
	}

	if got.PriorityRefuse.ControlValue != "" || strings.Contains(got.PriorityRefuse.Search, "priority") {
		t.Errorf("priority refuse = %+v, want control empty and priority omitted", got.PriorityRefuse)
	}
}

func TestWebUIBuryAction(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="bury-task-btn"`) {
		t.Fatal("expected #bury-task-btn in web/index.html")
	}
	if !strings.Contains(ui, `Bury Task`) {
		t.Fatal("expected Bury Task button text in web/index.html")
	}
	if !strings.Contains(ui, `buryBtn.onclick = () => buryTask(t.id, t.worker)`) {
		t.Fatal("expected bury button onclick handler wiring")
	}
	if !strings.Contains(ui, `async function buryTask(id, worker)`) {
		t.Fatal("expected buryTask function in web/index.html")
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.rw.Close()
	defer db.ro.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createResp, err := http.Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"p1","body":"task to bury"}`))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		createResp.Body.Close()
		t.Fatalf("decode create response failed: %v", err)
	}
	createResp.Body.Close()
	taskID := created.ID

	claimResp, err := http.Post(srv.URL+"/tasks/"+taskID+"/claim", "application/json", strings.NewReader(`{"worker":"alice"}`))
	if err != nil {
		t.Fatalf("claim task failed: %v", err)
	}
	claimResp.Body.Close()
	if claimResp.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want 200", claimResp.StatusCode)
	}

	badBuryResp, err := http.Post(srv.URL+"/tasks/"+taskID+"/bury", "application/json", strings.NewReader(`{"worker":""}`))
	if err != nil {
		t.Fatalf("bury empty worker failed: %v", err)
	}
	badBuryResp.Body.Close()
	if badBuryResp.StatusCode != http.StatusBadRequest {
		t.Errorf("bury with empty worker = %d, want 400", badBuryResp.StatusCode)
	}

	wrongWorkerResp, err := http.Post(srv.URL+"/tasks/"+taskID+"/bury", "application/json", strings.NewReader(`{"worker":"bob"}`))
	if err != nil {
		t.Fatalf("bury wrong worker failed: %v", err)
	}
	wrongWorkerResp.Body.Close()
	if wrongWorkerResp.StatusCode != http.StatusConflict {
		t.Errorf("bury with wrong worker = %d, want 409", wrongWorkerResp.StatusCode)
	}

	buryResp, err := http.Post(srv.URL+"/tasks/"+taskID+"/bury", "application/json", strings.NewReader(`{"worker":"alice"}`))
	if err != nil {
		t.Fatalf("bury task failed: %v", err)
	}
	buryResp.Body.Close()
	if buryResp.StatusCode != http.StatusNoContent {
		t.Fatalf("bury status = %d, want 204", buryResp.StatusCode)
	}

	getResp, err := http.Get(srv.URL + "/tasks/" + taskID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	var buriedTask struct {
		Status string `json:"status"`
		Worker string `json:"worker"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&buriedTask); err != nil {
		getResp.Body.Close()
		t.Fatalf("decode get response failed: %v", err)
	}
	getResp.Body.Close()
	if buriedTask.Status != "buried" {
		t.Errorf("task status = %q, want buried", buriedTask.Status)
	}
	if buriedTask.Worker != "" {
		t.Errorf("buried task worker = %q, want empty", buriedTask.Worker)
	}

	statsResp, err := http.Get(srv.URL + "/stats")
	if err != nil {
		t.Fatalf("get stats failed: %v", err)
	}
	var stats struct {
		Buried int `json:"buried"`
	}
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		statsResp.Body.Close()
		t.Fatalf("decode stats failed: %v", err)
	}
	statsResp.Body.Close()
	if stats.Buried != 1 {
		t.Errorf("stats.buried = %d, want 1", stats.Buried)
	}

	kickResp, err := http.Post(srv.URL+"/tasks/"+taskID+"/kick", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("kick task failed: %v", err)
	}
	io.Copy(io.Discard, kickResp.Body)
	kickResp.Body.Close()
	if kickResp.StatusCode != http.StatusNoContent {
		t.Fatalf("kick status = %d, want 204", kickResp.StatusCode)
	}

	getAfterKick, err := http.Get(srv.URL + "/tasks/" + taskID)
	if err != nil {
		t.Fatalf("get task after kick failed: %v", err)
	}
	var kickedTask struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(getAfterKick.Body).Decode(&kickedTask); err != nil {
		getAfterKick.Body.Close()
		t.Fatalf("decode get after kick failed: %v", err)
	}
	getAfterKick.Body.Close()
	if kickedTask.Status != "pending" {
		t.Errorf("kicked task status = %q, want pending", kickedTask.Status)
	}
}
