package taskd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestPatchCompareAndSwap(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "base", "project": "cas"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("decode created: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	var task struct {
		Version int    `json:"version"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		resp.Body.Close()
		t.Fatalf("decode task: %v", err)
	}
	resp.Body.Close()

	v := task.Version
	if v < 1 {
		t.Fatalf("expected initial version >= 1, got %d", v)
	}

	patch1, _ := json.Marshal(map[string]any{"body": "first", "if_version": v})
	req, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID), bytes.NewReader(patch1))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("first patch failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first patch expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	patch2, _ := json.Marshal(map[string]any{"body": "second", "if_version": v})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID), bytes.NewReader(patch2))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("second patch failed: %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale patch expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		resp.Body.Close()
		t.Fatalf("decode task: %v", err)
	}
	resp.Body.Close()

	if task.Body != "first" {
		t.Fatalf("expected body 'first', got %q", task.Body)
	}
	if task.Version <= v {
		t.Fatalf("expected version > %d, got %d", v, task.Version)
	}

	patchUncond, _ := json.Marshal(map[string]any{"body": "unconditional"})
	req, _ = http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID), bytes.NewReader(patchUncond))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unconditional patch failed: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unconditional patch expected 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		resp.Body.Close()
		t.Fatalf("decode task: %v", err)
	}
	resp.Body.Close()

	if task.Body != "unconditional" || task.Version <= v+1 {
		t.Fatalf("expected body 'unconditional' and version > %d, got body=%q version=%d", v+1, task.Body, task.Version)
	}

	resp, err = http.Get(srv.URL + "/tasks?project=cas")
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	var list []struct {
		ID      int64 `json:"id"`
		Version int   `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		resp.Body.Close()
		t.Fatalf("decode list: %v", err)
	}
	resp.Body.Close()
	if len(list) != 1 || list[0].Version != task.Version {
		t.Fatalf("expected list item with version %d, got %+v", task.Version, list)
	}

	resp, err = http.Get(srv.URL + "/tasks?project=cas&fields=id,version")
	if err != nil {
		t.Fatalf("list fields failed: %v", err)
	}
	var fieldList []struct {
		ID      int64 `json:"id"`
		Version int   `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fieldList); err != nil {
		resp.Body.Close()
		t.Fatalf("decode field list: %v", err)
	}
	resp.Body.Close()
	if len(fieldList) != 1 || fieldList[0].Version != task.Version {
		t.Fatalf("expected field projection version %d, got %+v", task.Version, fieldList)
	}
}

func TestMutationsBumpVersion(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()

	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	createBody, _ := json.Marshal(map[string]string{"body": "lifecycle", "project": "life"})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	getVer := func() int {
		r, err := http.Get(fmt.Sprintf("%s/tasks/%d", srv.URL, created.ID))
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		var item struct {
			Version int `json:"version"`
		}
		json.NewDecoder(r.Body).Decode(&item)
		r.Body.Close()
		return item.Version
	}

	v0 := getVer()
	if v0 != 1 {
		t.Fatalf("expected initial version 1, got %d", v0)
	}

	claimBody, _ := json.Marshal(map[string]string{"worker": "w1", "project": "life"})
	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("claim failed: %v", err)
	}
	resp.Body.Close()

	v1 := getVer()
	if v1 != v0+1 {
		t.Fatalf("expected version after claim %d, got %d", v0+1, v1)
	}

	touchBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/touch", srv.URL, created.ID), "application/json", bytes.NewReader(touchBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("touch failed: %v", err)
	}
	resp.Body.Close()

	v2 := getVer()
	if v2 != v1 {
		t.Fatalf("expected version unchanged after touch %d, got %d", v1, v2)
	}

	releaseBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/release", srv.URL, created.ID), "application/json", bytes.NewReader(releaseBody))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("release failed: %v", err)
	}
	resp.Body.Close()

	v3 := getVer()
	if v3 != v2+1 {
		t.Fatalf("expected version after release %d, got %d", v2+1, v3)
	}

	claimIDBody, _ := json.Marshal(map[string]string{"worker": "w2"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/claim", srv.URL, created.ID), "application/json", bytes.NewReader(claimIDBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("claim by id failed: %v", err)
	}
	resp.Body.Close()

	v4 := getVer()
	if v4 != v3+1 {
		t.Fatalf("expected version after claim by id %d, got %d", v3+1, v4)
	}

	buryBody, _ := json.Marshal(map[string]string{"worker": "w2"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/bury", srv.URL, created.ID), "application/json", bytes.NewReader(buryBody))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bury failed: %v", err)
	}
	resp.Body.Close()

	v5 := getVer()
	if v5 != v4+1 {
		t.Fatalf("expected version after bury %d, got %d", v4+1, v5)
	}

	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/kick", srv.URL, created.ID), "application/json", bytes.NewReader([]byte("{}")))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("kick failed: %v", err)
	}
	resp.Body.Close()

	v6 := getVer()
	if v6 != v5+1 {
		t.Fatalf("expected version after kick %d, got %d", v5+1, v6)
	}

	resp, err = http.Post(srv.URL+"/tasks/claim", "application/json", bytes.NewReader(claimBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("claim 3 failed: %v", err)
	}
	resp.Body.Close()

	v7 := getVer()
	if v7 != v6+1 {
		t.Fatalf("expected version after claim 3 %d, got %d", v6+1, v7)
	}

	doneBody, _ := json.Marshal(map[string]string{"worker": "w1"})
	resp, err = http.Post(fmt.Sprintf("%s/tasks/%d/done", srv.URL, created.ID), "application/json", bytes.NewReader(doneBody))
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("done failed: %v", err)
	}
	resp.Body.Close()

	v8 := getVer()
	if v8 != v7+1 {
		t.Fatalf("expected version after done %d, got %d", v7+1, v8)
	}
}
