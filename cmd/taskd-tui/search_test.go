package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestSearchFilter(t *testing.T) {
	u := newUI("http://localhost:8080", "", false)
	all := []task{
		{ID: "aaaa1111", Project: "proj-a", Status: "pending", Body: "rebuild zebra manifest", Worker: "worker1"},
		{ID: "bbbb2222", Project: "proj-b", Status: "leased", Body: "deploy pipeline", Worker: "ZEBRA-runner"},
		{ID: "cccc3333", Project: "proj-a", Status: "done", AssetPath: "models/Zebra.blend", Body: "render scene"},
		{ID: "zebra001", Project: "proj-c", Status: "pending", Body: "other task id"},
		{ID: "eeee5555", Project: "proj-a", Status: "pending", Body: "unrelated fix", Worker: "worker2"},
	}

	u.render(all)
	if len(u.shown) != 5 {
		t.Fatalf("expected 5 tasks shown, got %d", len(u.shown))
	}

	u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	if !u.searching {
		t.Fatal("expected searching to be active after /")
	}
	if u.query != "" {
		t.Fatalf("expected empty query initially, got %q", u.query)
	}

	status := u.status.GetText(true)
	lines := strings.Split(status, "\n")
	if len(lines) < 2 || lines[1] != "/" {
		t.Fatalf("expected query prompt '/' on line 2, got %q", status)
	}

	for _, r := range "zebra" {
		u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
	}
	if u.query != "zebra" {
		t.Fatalf("expected query 'zebra', got %q", u.query)
	}
	if len(u.shown) != 4 {
		t.Fatalf("expected 4 tasks matching 'zebra', got %d", len(u.shown))
	}
	status = u.status.GetText(true)
	if !strings.Contains(status, "row 1 of 4") {
		t.Fatalf("expected 'row 1 of 4' in status, got %q", status)
	}
	if !strings.Contains(status, "/zebra") {
		t.Fatalf("expected '/zebra' in status prompt, got %q", status)
	}

	u.keys(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if u.searching {
		t.Fatal("expected searching to be inactive after Escape")
	}
	if u.query != "" {
		t.Fatalf("expected empty query after Escape, got %q", u.query)
	}
	if len(u.shown) != 5 {
		t.Fatalf("expected 5 tasks restored after Escape, got %d", len(u.shown))
	}
	status = u.status.GetText(true)
	if !strings.Contains(status, "row 1 of 5") {
		t.Fatalf("expected 'row 1 of 5' in status after Escape, got %q", status)
	}
}

func TestSearchSwallowsCommands(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	if !u.searching {
		t.Fatal("expected searching to be active")
	}

	hazardRunes := []rune{'e', 'D', 'x', 'n', 'q', 'c', 'u', 'p', '0', '1', '2', '3', '+', '-', '='}
	for _, r := range hazardRunes {
		ret := u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
		if ret != nil {
			t.Fatalf("expected swallowed key event for rune %q, got %+v", r, ret)
		}
	}

	if u.form != nil {
		t.Fatal("search input must not open create or edit form")
	}
	if u.modal != nil {
		t.Fatal("search input must not open confirmation modal")
	}
	if !u.searching {
		t.Fatal("searching should remain active")
	}
	if u.query != string(hazardRunes) {
		t.Fatalf("expected query buffer to hold %q, got %q", string(hazardRunes), u.query)
	}
}

func TestSearchBackspaceAndClear(t *testing.T) {
	u := newUI("http://localhost:8080", "", false)
	all := []task{
		{ID: "aaaa1111", Body: "first zebra"},
		{ID: "bbbb2222", Body: "second"},
	}
	u.render(all)

	u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	for _, r := range "zeb" {
		u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
	}
	if len(u.shown) != 1 {
		t.Fatalf("expected 1 task matching 'zeb', got %d", len(u.shown))
	}

	u.keys(tcell.NewEventKey(tcell.KeyBackspace, 0, 0))
	if u.query != "ze" {
		t.Fatalf("expected query 'ze', got %q", u.query)
	}

	u.keys(tcell.NewEventKey(tcell.KeyBackspace2, 0, 0))
	if u.query != "z" {
		t.Fatalf("expected query 'z', got %q", u.query)
	}

	u.keys(tcell.NewEventKey(tcell.KeyBackspace, 0, 0))
	if u.query != "" {
		t.Fatalf("expected empty query after backspacing, got %q", u.query)
	}
	if len(u.shown) != 2 {
		t.Fatalf("empty query must restore all tasks, got %d", len(u.shown))
	}
	if !u.searching {
		t.Fatal("search prompt should remain active on empty buffer")
	}

	for _, r := range "zebra manifest" {
		u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
	}
	u.keys(tcell.NewEventKey(tcell.KeyCtrlW, 0, 0))
	if u.query != "zebra " {
		t.Fatalf("expected Ctrl+W to delete word, got %q", u.query)
	}
	u.keys(tcell.NewEventKey(tcell.KeyCtrlU, 0, 0))
	if u.query != "" {
		t.Fatalf("expected Ctrl+U to clear query, got %q", u.query)
	}
	if len(u.shown) != 2 {
		t.Fatalf("cleared query must restore all tasks, got %d", len(u.shown))
	}
}
func TestSearchEnterAccepts(t *testing.T) {
	u := newUI("http://localhost:8080", "", false)
	all := []task{
		{ID: "aaaa1111", Body: "alpha task"},
		{ID: "bbbb2222", Body: "beta task"},
	}
	u.render(all)

	u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	for _, r := range "beta" {
		u.keys(tcell.NewEventKey(tcell.KeyRune, r, 0))
	}
	u.keys(tcell.NewEventKey(tcell.KeyEnter, 0, 0))

	if u.searching {
		t.Fatal("expected searching to be inactive after Enter")
	}
	if u.query != "beta" {
		t.Fatalf("expected query 'beta' preserved, got %q", u.query)
	}
	if len(u.shown) != 1 {
		t.Fatalf("expected 1 task shown, got %d", len(u.shown))
	}
	u.keys(tcell.NewEventKey(tcell.KeyRune, '/', 0))
	if !u.searching {
		t.Fatal("expected searching to be active after /")
	}
	if u.query != "beta" {
		t.Fatalf("expected existing query preserved when editing, got %q", u.query)
	}

	u.keys(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if u.query != "" {
		t.Fatalf("expected query cleared after Escape, got %q", u.query)
	}
	if len(u.shown) != 2 {
		t.Fatalf("expected all tasks restored after Escape, got %d", len(u.shown))
	}
}
