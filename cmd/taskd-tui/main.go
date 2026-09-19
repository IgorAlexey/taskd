package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type task struct {
	ID           string          `json:"id"`
	Project      string          `json:"project"`
	AssetPath    string          `json:"asset_path"`
	Status       string          `json:"status"`
	Worker       string          `json:"worker"`
	LeaseExpires int64           `json:"lease_expires"`
	Priority     int             `json:"priority"`
	Body         string          `json:"body"`
	Primitives   json.RawMessage `json:"primitives"`
}

var client = &http.Client{Timeout: 3 * time.Second}

type ui struct {
	url       string
	app       *tview.Application
	table     *tview.Table
	body      *tview.TextView
	status    *tview.TextView
	root      tview.Primitive
	form      *tview.Form
	modal     *tview.Modal
	filter    string
	project   string
	all       []task
	shown     []task
	msg       string
	shownID   string
	shownBody string
}

func (u *ui) fetch() ([]task, error) {
	resp, err := client.Get(u.url + "/tasks?limit=500")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ts []task
	return ts, json.NewDecoder(resp.Body).Decode(&ts)
}

func (u *ui) call(method, path string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, u.url+path, &buf)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (u *ui) selected() (task, bool) {
	r, _ := u.table.GetSelection()
	if r < 1 || r > len(u.shown) {
		return task{}, false
	}
	return u.shown[r-1], true
}

func lease(t task, now int64) string {
	if t.Status != "leased" {
		return ""
	}
	if rem := t.LeaseExpires - now; rem > 0 {
		return fmt.Sprintf("%dm%02ds", rem/60, rem%60)
	}
	return "expired"
}

func (u *ui) render(all []task) {
	keep, _ := u.selected()
	u.all, u.shown = all, u.shown[:0]
	counts := map[string]int{}
	for _, t := range all {
		counts[t.Status]++
		if (u.filter == "" || t.Status == u.filter) && (u.project == "" || t.Project == u.project) {
			u.shown = append(u.shown, t)
		}
	}
	u.table.Clear()
	for i, h := range []string{"STATUS", "PRI", "PROJECT", "LEASE", "WORKER", "ID", "TITLE"} {
		u.table.SetCell(0, i, tview.NewTableCell(h).SetTextColor(tcell.ColorYellow).SetSelectable(false))
	}
	colors := map[string]tcell.Color{"pending": tcell.ColorWhite, "leased": tcell.ColorOrange, "done": tcell.ColorGreen}
	now, row := time.Now().Unix(), 1
	for i, t := range u.shown {
		title := cmp.Or(strings.SplitN(t.Body, "\n", 2)[0], t.AssetPath)
		cells := []string{t.Status, fmt.Sprint(t.Priority), t.Project, lease(t, now), t.Worker, t.ID[:min(7, len(t.ID))], title}
		for c, s := range cells {
			u.table.SetCell(i+1, c, tview.NewTableCell(s).SetTextColor(colors[t.Status]).SetExpansion(c/6))
		}
		if t.ID == keep.ID {
			row = i + 1
		}
	}
	u.table.Select(row, 0)
	u.showBody()
	u.status.SetText(fmt.Sprintf(" %s  project %s  pending %d  leased %d  done %d   [j/k] move  [0-3] filter  [p] project  [n] new  [e] edit  [+/-] priority  [D] delete  [q] quit   %s",
		cmp.Or(u.filter, "all"), cmp.Or(u.project, "all"), counts["pending"], counts["leased"], counts["done"], u.msg))
}

func (u *ui) showBody() {
	t, ok := u.selected()
	if !ok {
		u.shownID, u.shownBody = "", ""
		u.body.SetText("")
		return
	}
	text := t.Body
	if len(t.Primitives) > 0 && string(t.Primitives) != "null" {
		text += "\n\nresult: " + string(t.Primitives)
	}
	if t.ID != u.shownID || text != u.shownBody {
		u.shownID, u.shownBody = t.ID, text
		u.body.SetText(text).ScrollToBeginning()
	}
}

func (u *ui) refresh() {
	ts, err := u.fetch()
	u.app.QueueUpdateDraw(func() {
		if err != nil {
			u.msg = err.Error()
			u.status.SetText(" " + u.msg)
			return
		}
		u.render(ts)
	})
}

func (u *ui) act(method, path string, body any) {
	go func() {
		err := u.call(method, path, body)
		u.app.QueueUpdateDraw(func() {
			u.msg = ""
			if err != nil {
				u.msg = err.Error()
			}
		})
		u.refresh()
	}()
}

func (u *ui) showCreateForm() {
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" new task ")
	f.AddInputField("Project", cmp.Or(u.project, "taskd"), 20, nil, nil)
	f.AddInputField("Priority", "0", 10, tview.InputFieldInteger, nil)
	f.AddTextArea("Body", "", 0, 0, 0, nil)
	proj := f.GetFormItem(0).(*tview.InputField)
	pri := f.GetFormItem(1).(*tview.InputField)
	body := f.GetFormItem(2).(*tview.TextArea)
	close := func() {
		u.form = nil
		u.app.SetRoot(u.root, true).SetFocus(u.table)
	}
	submit := func() {
		close()
		p, _ := strconv.Atoi(pri.GetText())
		u.act("POST", "/tasks", map[string]any{
			"project":  proj.GetText(),
			"priority": p,
			"body":     body.GetText(),
		})
	}
	f.AddButton("Submit", submit).AddButton("Cancel", close).SetCancelFunc(close)
	u.form = f
	u.app.SetRoot(f, true)
}

func (u *ui) showEditForm(t task) {
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" edit task ")
	body := tview.NewTextArea().SetLabel("Body").SetText(t.Body, false).SetSize(5, 0)
	pri := tview.NewInputField().SetLabel("Priority").SetText(strconv.Itoa(t.Priority)).
		SetFieldWidth(10).SetAcceptanceFunc(tview.InputFieldInteger)
	f.AddFormItem(body).AddFormItem(pri)
	close := func() {
		u.form = nil
		u.app.SetRoot(u.root, true).SetFocus(u.table)
	}
	submit := func() {
		p, err := strconv.Atoi(pri.GetText())
		if err != nil || p < 0 {
			f.SetTitle(" edit task (invalid priority) ")
			return
		}
		close()
		u.act("PATCH", "/tasks/"+t.ID, map[string]any{
			"body":     body.GetText(),
			"priority": p,
		})
	}
	f.AddButton("Submit", submit).AddButton("Cancel", close).SetCancelFunc(close)
	u.form = f
	u.app.SetRoot(f, true)
}

func (u *ui) showDeleteConfirm(t task) {
	title := cmp.Or(strings.SplitN(t.Body, "\n", 2)[0], t.AssetPath)
	name := t.ID
	if title != "" && title != t.ID {
		name = fmt.Sprintf("%s (%s)", t.ID, title)
	}
	m := tview.NewModal()
	m.SetText(fmt.Sprintf("Delete task %s?\nDeleted tasks cannot be recovered.", name))
	m.AddButtons([]string{"Delete", "Cancel"})
	close := func() {
		u.modal = nil
		u.app.SetRoot(u.root, true).SetFocus(u.table)
	}
	m.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		close()
		if buttonIndex == 0 {
			path := "/tasks/" + t.ID
			if t.Status == "done" {
				path += "?force=true"
			}
			u.act("DELETE", path, nil)
		}
	})
	u.modal = m
	u.app.SetRoot(m, true)
}

func (u *ui) keys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyBacktab:
		u.app.SetFocus(u.body)
		return nil
	}
	t, ok := u.selected()
	switch ev.Rune() {
	case 'q':
		u.app.Stop()
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, 0)
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, 0)
	case 'g':
		if len(u.shown) > 0 {
			u.table.Select(1, 0)
		}
	case 'G':
		if len(u.shown) > 0 {
			u.table.Select(len(u.shown), 0)
		}
	case '0', '1', '2', '3':
		u.filter = []string{"", "pending", "leased", "done"}[ev.Rune()-'0']
		u.render(u.all)
	case 'p':
		seen := map[string]bool{}
		for _, t := range u.all {
			if t.Project != "" {
				seen[t.Project] = true
			}
		}
		projects := slices.Sorted(maps.Keys(seen))
		next := ""
		for i, p := range projects {
			if p == u.project {
				if i+1 < len(projects) {
					next = projects[i+1]
				}
				break
			}
		}
		if u.project == "" && len(projects) > 0 {
			next = projects[0]
		}
		u.project = next
		u.render(u.all)
	case '+', '=', '-':
		if ok {
			d := map[rune]int{'+': 1, '=': 1, '-': -1}[ev.Rune()]
			if pri := max(0, t.Priority+d); pri != t.Priority {
				u.act("PATCH", "/tasks/"+t.ID, map[string]int{"priority": pri})
			}
		}
	case 'D':
		if ok {
			u.showDeleteConfirm(t)
		}
	case 'n':
		u.showCreateForm()
	case 'e':
		if ok {
			u.showEditForm(t)
		}
	default:
		return ev
	}
	return nil
}

func (u *ui) bodyKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyEscape:
		u.app.SetFocus(u.table)
		return nil
	}
	if ev.Rune() == 'q' {
		u.app.Stop()
		return nil
	}
	return ev
}

type config struct {
	url     string
	project string
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: taskd-tui [options]

Terminal user interface for the taskd task queue.

Options:
  -url string
    	taskd daemon URL (default: $TASKD_URL, $T, or "http://localhost:8080")
  -project string
    	filter tasks by project (default: $TASKD_PROJECT)
  -h, -help
    	show this help message

Environment variables:
  TASKD_URL      taskd daemon address
  TASKD_PROJECT  default project filter
  T              shorthand taskd daemon address

Keyboard shortcuts:
  j, Down        Move selection down
  k, Up          Move selection up
  Tab, Backtab   Switch focus between task table and task body
  0              Show all tasks
  1              Filter pending tasks
  2              Filter leased tasks
  3              Filter done tasks
  p              Cycle project filter
  + / =          Increase task priority
  -              Decrease task priority
  n              Create new task
  D              Delete selected task
  q              Quit
`)
}

func parseFlags(args []string) (config, error) {
	defaultURL := os.Getenv("TASKD_URL")
	if defaultURL == "" {
		defaultURL = os.Getenv("T")
	}
	if defaultURL == "" {
		defaultURL = "http://localhost:8080"
	}
	defaultProject := os.Getenv("TASKD_PROJECT")

	var cfg config
	fs := flag.NewFlagSet("taskd-tui", flag.ContinueOnError)
	fs.Usage = func() {
		printUsage(fs.Output())
	}
	fs.StringVar(&cfg.url, "url", defaultURL, "taskd daemon address")
	fs.StringVar(&cfg.project, "project", defaultProject, "filter tasks by project")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func newUI(url, project string) *ui {
	u := &ui{url: strings.TrimRight(url, "/"), project: project, app: tview.NewApplication().EnableMouse(true)}
	u.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	u.table.SetSelectionChangedFunc(func(int, int) { u.showBody() }).SetInputCapture(u.keys)
	u.body = tview.NewTextView().SetWrap(true)
	u.body.SetBorder(true).SetTitle(" task ").SetInputCapture(u.bodyKeys)
	u.status = tview.NewTextView()
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.table, 0, 3, true).AddItem(u.body, 0, 2, false).AddItem(u.status, 1, 0, false)
	u.root = flex
	return u
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	u := newUI(cfg.url, cfg.project)
	go func() {
		for ; ; time.Sleep(time.Second) {
			u.refresh()
		}
	}()
	if err := u.app.SetRoot(u.root, true).Run(); err != nil {
		fmt.Println(err)
	}
}
