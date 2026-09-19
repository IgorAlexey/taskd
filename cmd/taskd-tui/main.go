package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
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
	url     string
	app     *tview.Application
	table   *tview.Table
	body    *tview.TextView
	status  *tview.TextView
	root    tview.Primitive
	form    *tview.Form
	filter  string
	project string
	all     []task
	shown   []task
	msg     string
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
	u.status.SetText(fmt.Sprintf(" %s  project %s  pending %d  leased %d  done %d   [j/k] move  [0-3] filter  [p] project  [n] new  [+/-] priority  [D] delete  [q] quit   %s",
		cmp.Or(u.filter, "all"), cmp.Or(u.project, "all"), counts["pending"], counts["leased"], counts["done"], u.msg))
}

func (u *ui) showBody() {
	t, ok := u.selected()
	if !ok {
		u.body.SetText("")
		return
	}
	text := t.Body
	if len(t.Primitives) > 0 && string(t.Primitives) != "null" {
		text += "\n\nresult: " + string(t.Primitives)
	}
	u.body.SetText(text).ScrollToBeginning()
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
	f.AddInputField("Body", "", 40, nil, nil)
	proj := f.GetFormItem(0).(*tview.InputField)
	pri := f.GetFormItem(1).(*tview.InputField)
	body := f.GetFormItem(2).(*tview.InputField)
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

func (u *ui) keys(ev *tcell.EventKey) *tcell.EventKey {
	t, ok := u.selected()
	switch ev.Rune() {
	case 'q':
		u.app.Stop()
	case 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, 0)
	case 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, 0)
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
			u.act("PATCH", "/tasks/"+t.ID, map[string]int{"priority": t.Priority + d})
		}
	case 'D':
		if ok {
			u.act("DELETE", "/tasks/"+t.ID, nil)
		}
	case 'n':
		u.showCreateForm()
	default:
		return ev
	}
	return nil
}

func main() {
	url := flag.String("url", "http://localhost:8080", "taskd address")
	flag.Parse()
	u := &ui{url: strings.TrimRight(*url, "/"), app: tview.NewApplication()}
	u.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	u.table.SetSelectionChangedFunc(func(int, int) { u.showBody() }).SetInputCapture(u.keys)
	u.body = tview.NewTextView().SetWrap(true)
	u.body.SetBorder(true).SetTitle(" task ")
	u.status = tview.NewTextView()
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.table, 0, 3, true).AddItem(u.body, 0, 2, false).AddItem(u.status, 1, 0, false)
	u.root = flex
	go func() {
		for ; ; time.Sleep(time.Second) {
			u.refresh()
		}
	}()
	if err := u.app.SetRoot(flex, true).Run(); err != nil {
		fmt.Println(err)
	}
}
