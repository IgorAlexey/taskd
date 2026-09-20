package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCreateFormTypingAndAdvancing(t *testing.T) {
	f, _ := newCreateForm("")
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
	f, _ := newCreateForm("proj")
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
	f, _ := newCreateForm("proj")
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
	f, _ := newCreateForm("")
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
	f, _ := newCreateForm("proj")
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

	f, _ := newEditForm(originalTask)
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
		f, _ := newCreateForm("myproj")
		f.fit(width, 24, th)
		view := f.View()
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

func TestBlinkMessagesRoundTripThroughTheForm(t *testing.T) {
	f, cmd := newCreateForm("")
	if cmd == nil {
		t.Fatalf("focusing the first field must schedule the cursor blink")
	}
	msg := cmd()
	if _, ok := msg.(tea.KeyPressMsg); ok {
		t.Fatalf("blink command must not be a key press")
	}
	f, next := f.Update(msg)
	if next == nil {
		t.Fatalf("a blink message must schedule the next blink")
	}
	if f.done || f.cancelled {
		t.Fatalf("a widget message must not submit or cancel the form")
	}
}

func TestEmptyAssetRowGoesBeforeFieldsAndComesBackWithFocus(t *testing.T) {
	th := newTheme(true)
	f, _ := newCreateForm("p")
	f.fit(40, 7, th) // one row short: the title goes, the asset row stays
	if v := ansi.Strip(f.View()); strings.Contains(v, "New Task") || !strings.Contains(v, "asset:") {
		t.Fatalf("at 40x7 the title goes before the empty asset row:\n%s", v)
	}
	f.fit(40, 6, th) // another row short: now the empty asset row goes
	if v := ansi.Strip(f.View()); strings.Contains(v, "asset:") || !strings.Contains(v, "project:") {
		t.Fatalf("at 40x6 the empty asset row goes and the fields stay:\n%s", v)
	}
	f.setFocus(2)
	f.fit(40, 6, th)
	if v := ansi.Strip(f.View()); !strings.Contains(v, "asset:") || !strings.Contains(v, "project:") || !strings.Contains(v, "priority:") {
		t.Fatalf("a focused asset field is drawn without hiding the other fields:\n%s", v)
	}
}

func TestAssetFieldKeepsItsRowWhileFocusedOrFilled(t *testing.T) {
	m := newModel(config{}, nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 24})
	m, _ = send(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // project -> priority
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // -> asset
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 6})
	view := ansi.Strip(m.View().Content)
	if m.form.focus != 2 || !strings.Contains(view, "asset:") || !strings.Contains(view, "project:") {
		t.Fatalf("a focused asset field keeps its row next to the others: focus=%d\n%s", m.form.focus, view)
	}
	for _, r := range "a.gltf" {
		m, _ = send(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // -> body
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "a.gltf") {
		t.Fatalf("a filled asset field keeps its row after focus leaves:\n%s", view)
	}
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) // back onto asset
	for range "a.gltf" {
		m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab}) // -> body; asset now empty
	view = ansi.Strip(m.View().Content)
	if m.form.focus != 3 || strings.Contains(view, "asset:") {
		t.Fatalf("an empty, unfocused asset field is the first field row to go: focus=%d\n%s", m.form.focus, view)
	}
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	view = ansi.Strip(m.View().Content)
	if m.form.focus != 2 || !strings.Contains(view, "asset:") {
		t.Fatalf("Shift-Tab reaches the asset field and draws it again: focus=%d\n%s", m.form.focus, view)
	}
}

func TestFocusRingStaysWholeOnAShortTerminal(t *testing.T) {
	m := newModel(config{}, nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 60, Height: 5})
	m, _ = send(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	var seen []int
	for i := 0; i < 5; i++ {
		seen = append(seen, m.form.focus)
		m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	want := []int{0, 1, 2, 3, 4} // every field stays reachable
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("Tab ring = %v, want %v", seen, want)
		}
	}
	// The loop left focus on project; Shift-Tab walks back onto the button.
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.form.focus != 4 {
		t.Fatalf("Shift-Tab from project must reach the button, got %d", m.form.focus)
	}
}

func TestTabbingOntoTheAssetFieldCostsOneRow(t *testing.T) {
	// At 50x6 the empty asset row is the one row that does not fit;
	// focusing it must cost one other row, not the other fields.
	m := newModel(config{}, nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 50, Height: 6})
	m, _ = send(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	before := ansi.Strip(m.View().Content)
	if !strings.Contains(before, "project:") || strings.Contains(before, "asset:") {
		t.Fatalf("unexpected start:\n%s", before)
	}
	for i := 0; i < 2; i++ { // project -> priority -> asset
		m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	after := ansi.Strip(m.View().Content)
	if m.form.focus != 2 || !strings.Contains(after, "asset:") || !strings.Contains(after, "project:") || !strings.Contains(after, "priority:") {
		t.Fatalf("focusing asset hid the other fields (focus %d):\n%s", m.form.focus, after)
	}
}
