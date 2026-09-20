package main

import (
	"testing"
)

func TestFormValidateAssetOnly(t *testing.T) {
	t.Run("both blank rejected", func(t *testing.T) {
		f, _ := newCreateForm("my-project")
		f.asset.SetValue("")
		f.body.SetValue("")
		if got := f.validate(); got != "missing asset_path or body" {
			t.Fatalf("validate() = %q, want %q", got, "missing asset_path or body")
		}

		f.asset.SetValue("   ")
		f.body.SetValue("   \n\t  ")
		if got := f.validate(); got != "missing asset_path or body" {
			t.Fatalf("validate() with whitespace = %q, want %q", got, "missing asset_path or body")
		}
	})

	t.Run("asset only allowed", func(t *testing.T) {
		f, _ := newCreateForm("my-project")
		f.asset.SetValue("data/output.csv")
		f.body.SetValue("")
		if got := f.validate(); got != "" {
			t.Fatalf("validate() = %q, want empty string", got)
		}
	})

	t.Run("body only allowed", func(t *testing.T) {
		f, _ := newCreateForm("my-project")
		f.asset.SetValue("")
		f.body.SetValue("task description")
		if got := f.validate(); got != "" {
			t.Fatalf("validate() = %q, want empty string", got)
		}
	})

	t.Run("both filled allowed", func(t *testing.T) {
		f, _ := newCreateForm("my-project")
		f.asset.SetValue("data/output.csv")
		f.body.SetValue("task description")
		if got := f.validate(); got != "" {
			t.Fatalf("validate() = %q, want empty string", got)
		}
	})
}

func TestFormSubmitAssetOnly(t *testing.T) {
	t.Run("create task", func(t *testing.T) {
		f, _ := newCreateForm("my-project")
		f.asset.SetValue("models/checkpoint.pt")
		f.body.SetValue("")

		method, path, body, _, errText := f.submit()
		if errText != "" {
			t.Fatalf("submit errText = %q, want empty", errText)
		}
		if method != "POST" || path != "/tasks" {
			t.Fatalf("submit got %s %s, want POST /tasks", method, path)
		}
		if body["asset_path"] != "models/checkpoint.pt" {
			t.Fatalf("submit asset_path = %v, want models/checkpoint.pt", body["asset_path"])
		}
	})

	t.Run("edit task", func(t *testing.T) {
		item := task{
			ID:        "asset-task-001",
			Project:   "my-project",
			AssetPath: "models/checkpoint.pt",
			Body:      "",
			Priority:  3,
		}
		f, _ := newEditForm(item)
		f.priority.SetValue("1")

		method, path, body, _, errText := f.submit()
		if errText != "" {
			t.Fatalf("submit errText = %q, want empty", errText)
		}
		if method != "PATCH" || path != "/tasks/asset-task-001" {
			t.Fatalf("submit got %s %s, want PATCH /tasks/asset-task-001", method, path)
		}
		if body["priority"] != 1 {
			t.Fatalf("submit priority = %v, want 1", body["priority"])
		}
	})
}
