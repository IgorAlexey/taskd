package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestWebUIAssetFilter(t *testing.T) {
	ui := string(uiHTML)
	if !strings.Contains(ui, `id="filter-asset-path"`) || !strings.Contains(ui, `name="asset_path"`) {
		t.Fatal("expected asset_path input in web/index.html filter bar")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available: " + err.Error())
	}

	out, err := exec.Command(node, "testdata/asset_filter.js", "web/index.html").CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\noutput:\n%s", err, out)
	}

	var got struct {
		InitVal      string `json:"initVal"`
		U1           string `json:"u1"`
		SyncedSearch string `json:"syncedSearch"`
		U2           string `json:"u2"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}

	if got.InitVal != "models/robot.glb" {
		t.Errorf("initVal = %q, want models/robot.glb", got.InitVal)
	}
	if !strings.Contains(got.U1, "asset_path=models%2Frobot.glb") {
		t.Errorf("u1 = %q, want asset_path parameter", got.U1)
	}
	if !strings.Contains(got.SyncedSearch, "asset_path=textures%2Fskin.png") {
		t.Errorf("syncedSearch = %q, want asset_path parameter", got.SyncedSearch)
	}
	if !strings.Contains(got.U2, "asset_path=textures%2Fskin.png") {
		t.Errorf("u2 = %q, want updated asset_path parameter", got.U2)
	}
}
