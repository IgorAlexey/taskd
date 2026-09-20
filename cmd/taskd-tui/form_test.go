package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCreateFormTypingAndAdvancing(t *testing.T) {
	f := newCreateForm("", 80)
	if f.focus != 0 {
		t.Fatalf("expected initial focus 0, got %d", f.focus)
	}

	// Simulate typing into project via tea.KeyPressMsg per rune
	for _, r := range "my-project" {
		var cmd tea.Cmd
		f, cmd = f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		_ = cmd
	}
	if f.project.Value() != "my-project" {
		t.Fatalf("expected project 'my-project', got %q", f.project.Value())
	}

	// Enter-on-field advances
	// Enter on field 0 (project) -> advances to 1 (priority)
	f, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.focus != 1 {
		t.Fatalf("expected focus 1 after enter on project, got %d", f.focus)
	}

	// Enter on field 1 (priority) -> advances to 2 (asset)
	f, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.focus != 2 {
		t.Fatalf("expected focus 2 after enter on priority, got %d", f.focus)
	}

	// Enter on field 2 (asset) -> advances to 3 (body)
	f, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.focus != 3 {
		t.Fatalf("expected focus 3 after enter on asset, got %d", f.focus)
	}

	// Simulate typing into body via tea.KeyPressMsg per rune
	for _, r := range "task title\nsecond line" {
		f, _ = f.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if f.body.Value() != "task title\nsecond line" {
		t.Fatalf("expected body 'task title\\nsecond line', got %q", f.body.Value())
	}
}

func TestTabCyclingOrder(t *testing.T) {
	f := newCreateForm("proj", 80)
	// Prefilled project starts with focus on body (3)
	if f.focus != 3 {
		t.Fatalf("expected initial focus 3 for prefilled project, got %d", f.focus)
	}

	// Set focus to 0 for orderly test
	f.setFocus(0)

	expectedForward := []int{1, 2, 3, 4, 0}
	for _, exp := range expectedForward {
		f, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if f.focus != exp {
			t.Fatalf("expected Tab to focus %d, got %d", exp, f.focus)
		}
	}

	// Shift-Tab backwards cycling from 0: 4, 3, 2, 1, 0
	expectedBackward := []int{4, 3, 2, 1, 0}
	for _, exp := range expectedBackward {
		f, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		if f.focus != exp {
			t.Fatalf("expected Shift-Tab to focus %d, got %d", exp, f.focus)
		}
	}
}

func TestCtrlSValidationAndSubmit(t *testing.T) {
	f := newCreateForm("proj", 80)
	f.body.SetValue("")

	// ctrl-s with blank body sets errText and done==false
	f, _ = f.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if f.done {
		t.Fatalf("expected done==false with blank body, got true")
	}
	if f.errText == "" {
		t.Fatalf("expected non-empty errText with blank body")
	}

	// Valid submit yields POST /tasks with the right map and priority omitted when blank
	f.body.SetValue("a valid task title")
	f.priority.SetValue("") // blank priority
	f, _ = f.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !f.done {
		t.Fatalf("expected done==true on valid submit, got false (err: %s)", f.errText)
	}

	method, path, body, success, errText := f.submit()
	if errText != "" {
		t.Fatalf("expected empty errText on valid submit, got %q", errText)
	}
	if method != "POST" {
		t.Fatalf("expected method POST, got %q", method)
	}
	if path != "/tasks" {
		t.Fatalf("expected path /tasks, got %q", path)
	}
	if success != "created task" {
		t.Fatalf("expected success 'created task', got %q", success)
	}
	if body["project"] != "proj" {
		t.Fatalf("expected body['project'] == 'proj', got %v", body["project"])
	}
	if body["body"] != "a valid task title" {
		t.Fatalf("expected body['body'] == 'a valid task title', got %v", body["body"])
	}
	if _, hasPri := body["priority"]; hasPri {
		t.Fatalf("expected priority to be omitted when blank, got %v", body["priority"])
	}
}

func TestProjectWithSpaceFailsValidation(t *testing.T) {
	f := newCreateForm("", 80)
	f.project.SetValue("invalid project name")
	f.body.SetValue("valid body")

	f, _ = f.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if f.done {
		t.Fatalf("expected done==false for project with spaces")
	}
	if f.errText == "" {
		t.Fatalf("expected errText for project with spaces")
	}

	_, _, _, _, errText := f.submit()
	if errText == "" {
		t.Fatalf("expected submit() to fail validation for project with spaces")
	}
}

func TestEscSetsCancelled(t *testing.T) {
	f := newCreateForm("proj", 80)
	f, _ = f.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !f.cancelled {
		t.Fatalf("expected cancelled==true after Esc, got false")
	}
}

func TestEditFormSendsOnlyChangedFields(t *testing.T) {
	originalTask := task{
		ID:        "1234567890abcdef",
		Project:   "my-proj",
		Priority:  3,
		AssetPath: "some/asset",
		Body:      "initial body text",
	}

	f := newEditForm(originalTask, 80)
	if !f.editing {
		t.Fatalf("expected editing==true in newEditForm")
	}
	if f.id != originalTask.ID {
		t.Fatalf("expected id %q, got %q", originalTask.ID, f.id)
	}
	if f.focus != 3 {
		t.Fatalf("expected focus on body (3) in newEditForm, got %d", f.focus)
	}

	// Change only the body
	f.body.SetValue("modified body text")

	f, _ = f.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !f.done {
		t.Fatalf("expected done==true after ctrl-s, got false (err: %s)", f.errText)
	}

	method, path, body, success, errText := f.submit()
	if errText != "" {
		t.Fatalf("expected empty errText, got %q", errText)
	}
	if method != "PATCH" {
		t.Fatalf("expected method PATCH, got %q", method)
	}
	if path != "/tasks/1234567890abcdef" {
		t.Fatalf("expected path /tasks/1234567890abcdef, got %q", path)
	}
	if success != "updated task 1234567" {
		t.Fatalf("expected success 'updated task 1234567', got %q", success)
	}

	if body["body"] != "modified body text" {
		t.Fatalf("expected body['body'] == 'modified body text', got %v", body["body"])
	}
	if _, hasProj := body["project"]; hasProj {
		t.Fatalf("expected project omitted when unchanged, got %v", body["project"])
	}
	if _, hasPri := body["priority"]; hasPri {
		t.Fatalf("expected priority omitted when unchanged, got %v", body["priority"])
	}
	if _, hasAsset := body["asset_path"]; hasAsset {
		t.Fatalf("expected asset_path omitted when unchanged, got %v", body["asset_path"])
	}
}

func TestViewFormattingAndWidthLimits(t *testing.T) {
	th := newTheme(true)

	widths := []int{60, 70, 80, 90, 100}
	for _, width := range widths {
		f := newCreateForm("myproj", width)
		view := f.View(width, 24, th)
		stripped := ansi.Strip(view)

		if !strings.Contains(stripped, f.title) {
			t.Fatalf("at width %d: view missing title %q", width, f.title)
		}
		expectedHint := "Tab next  ctrl-s save  Esc cancel"
		if !strings.Contains(stripped, expectedHint) {
			t.Fatalf("at width %d: view missing hint %q", width, expectedHint)
		}

		lines := strings.Split(stripped, "\n")
		for i, line := range lines {
			w := ansi.StringWidth(line)
			if w > width {
				t.Fatalf("at width %d: line %d exceeds width (actual %d): %q", width, i, w, line)
			}
		}
	}
}

func TestConfirmView(t *testing.T) {
	th := newTheme(true)
	c := confirmModel{
		text:    "Delete task 1234567?",
		button:  "delete",
		method:  "DELETE",
		path:    "/tasks/1234567",
		success: "deleted task 1234567",
	}

	view := c.View(80, 24, th)
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "Delete task 1234567?") {
		t.Fatalf("missing text in confirm view")
	}
	if !strings.Contains(stripped, "[y] delete") {
		t.Fatalf("missing '[y] delete' in confirm view")
	}
	if !strings.Contains(stripped, "[n] cancel") {
		t.Fatalf("missing '[n] cancel' in confirm view")
	}

	for _, line := range strings.Split(stripped, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatalf("line exceeds width 80: %q", line)
		}
	}
}

func TestHelpView(t *testing.T) {
	th := newTheme(true)
	view := helpView(80, 24, th)
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "move") {
		t.Fatalf("missing 'move' in help view")
	}
	if !strings.Contains(stripped, "quit") {
		t.Fatalf("missing 'quit' in help view")
	}

	for _, line := range strings.Split(stripped, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatalf("line exceeds width 80: %q", line)
		}
	}
}
