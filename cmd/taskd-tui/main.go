package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rivo/uniseg"
)

type task struct {
	ID           string          `json:"id"`
	Project      string          `json:"project"`
	AssetPath    string          `json:"asset_path"`
	Status       string          `json:"status"`
	Worker       string          `json:"worker"`
	LeaseExpires int64           `json:"lease_expires"`
	Priority     int             `json:"priority"`
	ClaimCount   int             `json:"claim_count"`
	Body         string          `json:"body"`
	Primitives   json.RawMessage `json:"primitives"`
	searchText   string
}

var client = &http.Client{Timeout: 3 * time.Second}

var (
	copyToClipboard           = defaultCopyToClipboard
	clipboardOut    io.Writer = os.Stderr
	gitCheckoutName           = defaultGitCheckoutName
	nowUnix                   = func() int64 { return time.Now().Unix() }
)

func defaultGitCheckoutName() string {
	cur, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		gitPath := filepath.Join(cur, ".git")
		fi, err := os.Stat(gitPath)
		if err == nil {
			if !fi.IsDir() {
				data, err := os.ReadFile(gitPath)
				if err == nil {
					content := strings.TrimSpace(string(data))
					if gd, ok := strings.CutPrefix(content, "gitdir:"); ok {
						gd = strings.TrimSpace(gd)
						if !filepath.IsAbs(gd) {
							gd = filepath.Clean(filepath.Join(cur, gd))
						}
						if before, _, found := strings.Cut(gd, filepath.FromSlash("/.git/worktrees/")); found {
							return filepath.Base(before)
						}
						if before, _, found := strings.Cut(gd, filepath.FromSlash("/.git")); found {
							return filepath.Base(before)
						}
					}
				}
			}
			return filepath.Base(cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

func defaultCopyToClipboard(text string) {
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", b64)
	if os.Getenv("TMUX") != "" {
		seq = fmt.Sprintf("\x1bPtmux;\x1b%s\x1b\\", seq)
	}
	fmt.Fprint(clipboardOut, seq)
}

func (u *ui) copySelectedID() {
	if t, ok := u.selected(); ok {
		copyToClipboard(t.ID)
		u.setMsg(fmt.Sprintf("copied %s to clipboard", t.ID))
	}
}

func (u *ui) copySelectedBody() {
	if t, ok := u.selected(); ok {
		copyToClipboard(t.Body)
		u.setMsg("copied body to clipboard")
	}
}

type ui struct {
	url    string
	origin string
	app    *tview.Application
	table  *tview.Table
	body   *tview.TextView
	status *tview.TextView
	flex   *tview.Flex
	pages  *tview.Pages
	form   *tview.Form
	modal  *tview.Modal
	filter string
	// project is the active filter. The event goroutine is its only
	// writer and takes projectMu; readers on other goroutines use proj().
	projectMu             sync.Mutex
	project               string
	searching             bool
	query                 string
	icons                 bool
	worker                string
	width                 int
	all                   []task
	shown                 []task
	projects              []string
	pending, leased, done int
	hasServerStats        bool
	statsProject          string
	msg                   string
	msgRev                int
	msgTimeout            time.Duration
	shownID               string
	shownBody             string
	refreshing            atomic.Bool
	// loaded reports whether an answer for the live filter has landed.
	// Adopting a filter clears it; any answer sets it, including a failed
	// one, so a filter nobody fetches cannot wedge the table.
	loaded bool
	zoomed bool
}

func (u *ui) proj() string {
	u.projectMu.Lock()
	defer u.projectMu.Unlock()
	return u.project
}

func (u *ui) setProj(project string) {
	u.projectMu.Lock()
	defer u.projectMu.Unlock()
	u.project = project
	u.loaded = false
}

// fetch scopes the queue to project.
func (u *ui) fetch(project string) ([]task, error) {
	reqURL, err := url.Parse(u.url + "/tasks?limit=500")
	if err != nil {
		return nil, err
	}
	if project != "" {
		q := reqURL.Query()
		q.Set("project", project)
		reqURL.RawQuery = q.Encode()
	}
	resp, err := client.Get(reqURL.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		body := strings.TrimSpace(string(b))
		if body != "" {
			return nil, fmt.Errorf("GET /tasks: %s: %s", resp.Status, body)
		}
		return nil, fmt.Errorf("GET /tasks: %s", resp.Status)
	}
	var ts []task
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		return nil, err
	}
	for i := range ts {
		ts[i].searchText = strings.ToLower(ts[i].Body + "\x00" + ts[i].AssetPath + "\x00" + ts[i].Worker + "\x00" + ts[i].ID)
	}
	return ts, nil
}

func (u *ui) fetchProjects() ([]string, error) {
	resp, err := client.Get(u.url + "/projects")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		body := strings.TrimSpace(string(b))
		if body != "" {
			return nil, fmt.Errorf("GET /projects: %s: %s", resp.Status, body)
		}
		return nil, fmt.Errorf("GET /projects: %s", resp.Status)
	}
	var projects []string
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, err
	}
	return projects, nil
}

type stats struct {
	Pending int `json:"pending"`
	Leased  int `json:"leased"`
	Done    int `json:"done"`
}

func (u *ui) fetchStats(project string) (stats, error) {
	reqURL, err := url.Parse(u.url + "/stats")
	if err != nil {
		return stats{}, err
	}
	if project != "" {
		q := reqURL.Query()
		q.Set("project", project)
		reqURL.RawQuery = q.Encode()
	}
	resp, err := client.Get(reqURL.String())
	if err != nil {
		return stats{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		body := strings.TrimSpace(string(b))
		if body != "" {
			return stats{}, fmt.Errorf("GET /stats: %s: %s", resp.Status, body)
		}
		return stats{}, fmt.Errorf("GET /stats: %s", resp.Status)
	}
	var st stats
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return stats{}, err
	}
	return st, nil
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

func short(id string) string {
	return id[:min(7, len(id))]
}

func (u *ui) selectedRow() int {
	r, _ := u.table.GetSelection()
	if len(u.shown) == 0 {
		return 0
	}
	return min(max(1, r), len(u.shown))
}

func (u *ui) selected() (task, bool) {
	r := u.selectedRow()
	if r == 0 {
		return task{}, false
	}
	return u.shown[r-1], true
}

const (
	iconUnknown = "\uf059"
	iconPending = "\uf017"
	iconLeased  = "\uf021"
	iconDone    = "\uf00c"
	iconPriLow  = "\uf107"
	iconPriMed  = "\uf106"
	iconPriHigh = "\uf102"
	iconPriFire = "\uf06d"
)

func statusText(status string, icons bool) string {
	if !icons || status == "" {
		return status
	}
	glyph := iconUnknown
	switch status {
	case "pending":
		glyph = iconPending
	case "leased":
		glyph = iconLeased
	case "done":
		glyph = iconDone
	}
	return glyph + " " + status
}

func priorityText(pri int, icons bool) string {
	if !icons {
		return strconv.Itoa(pri)
	}
	glyph := iconPriLow
	switch {
	case pri <= 0:
		glyph = iconPriFire
	case pri == 1:
		glyph = iconPriHigh
	case pri == 2:
		glyph = iconPriMed
	}
	return glyph + " " + strconv.Itoa(pri)
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
func (t task) activelyLeased(now int64) bool {
	return t.Status == "leased" && t.LeaseExpires >= now
}

// emptyState is the placeholder shown when no task is visible, so the
// operator can tell an empty queue apart from a filter that hid every
// task, and knows which key clears the filter that did it.
func matchTask(t task, qLower string) bool {
	if qLower == "" {
		return true
	}
	return strings.Contains(t.searchText, qLower)
}

func (u *ui) emptyState() string {
	switch {
	case !u.loaded:
		return "Loading " + cmp.Or(u.project, "tasks") + "..."
	case u.query != "":
		return "No tasks match query. Press 'Esc' to clear."
	case u.filter != "" && u.project != "":
		return "No tasks match filter. Press '0' to clear filter, 'p' to cycle project."
	case u.filter != "":
		return "No " + u.filter + " tasks. Press '0' to show all."
	case u.project != "":
		return "No tasks in " + u.project + ". Press 'p' to cycle project."
	}
	return "No tasks yet. Press 'n' to create a task."
}

const maxMetaWidth = 16

func (u *ui) render(all []task) {
	keep, _ := u.selected()
	u.all, u.shown = all, u.shown[:0]
	if !u.hasServerStats || u.statsProject != u.project {
		u.pending, u.leased, u.done = 0, 0, 0
		for i := range all {
			t := all[i]
			if u.project != "" && t.Project != u.project {
				continue
			}
			switch t.Status {
			case "pending":
				u.pending++
			case "leased":
				u.leased++
			case "done":
				u.done++
			}
		}
	}
	qLower := strings.ToLower(u.query)
	for i := range all {
		if all[i].searchText == "" {
			all[i].searchText = strings.ToLower(all[i].Body + "\x00" + all[i].AssetPath + "\x00" + all[i].Worker + "\x00" + all[i].ID)
		}
		t := all[i]
		if u.project != "" && t.Project != u.project {
			continue
		}
		var matchFilter bool
		switch u.filter {
		case "live":
			matchFilter = t.Status != "done"
		case "":
			matchFilter = true
		default:
			matchFilter = t.Status == u.filter
		}
		if matchFilter && matchTask(t, qLower) {
			u.shown = append(u.shown, t)
		}
	}
	u.table.Clear()
	headerClicked := func() bool { return true }
	for i, h := range []string{"STATUS", "PRI", "PROJECT", "LEASE", "WORKER", "ID", "CLAIMS", "TITLE"} {
		u.table.SetCell(0, i, tview.NewTableCell(h).SetTextColor(tcell.ColorYellow).SetSelectable(false).SetClickedFunc(headerClicked))
	}
	colors := map[string]tcell.Color{"pending": tcell.ColorWhite, "leased": tcell.ColorOrange, "done": tcell.ColorGreen}
	now, row := time.Now().Unix(), u.selectedRow()
	for i, t := range u.shown {
		title := taskTitle(t)
		cells := []string{
			statusText(t.Status, u.icons),
			priorityText(t.Priority, u.icons),
			truncWidth(t.Project, maxMetaWidth),
			lease(t, now),
			truncWidth(t.Worker, maxMetaWidth),
			short(t.ID),
			strconv.Itoa(t.ClaimCount),
			title,
		}
		for c, s := range cells {
			exp := 0
			if c == len(cells)-1 {
				exp = 1
			}
			u.table.SetCell(i+1, c, tview.NewTableCell(tview.Escape(s)).SetTextColor(colors[t.Status]).SetExpansion(exp))
		}
		if t.ID == keep.ID {
			row = i + 1
		}
	}
	off, coff := u.table.GetOffset()
	curRow, _ := u.table.GetSelection()
	if curRow != row {
		u.table.Select(row, 0)
	}
	_, _, _, h := u.table.GetInnerRect()
	if h > 0 {
		if maxOff := max(0, len(u.shown)+1-h); off > maxOff {
			off = maxOff
		}
	}
	if off < 0 {
		off = 0
	}
	u.table.SetOffset(off, coff)
	u.showBody()
	u.renderStatus()
}

func truncWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= maxWidth {
		return s
	}
	target := maxWidth - 3
	suffix := "..."
	if maxWidth <= 3 {
		target = maxWidth
		suffix = ""
	}
	g := uniseg.NewGraphemes(s)
	var width, end int
	for g.Next() {
		w := g.Width()
		if width+w > target {
			break
		}
		width += w
		_, to := g.Positions()
		end = to
	}
	return s[:end] + suffix
}

func daemonOrigin(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimSpace(rawURL)
	}
	return u.Host
}

func (u *ui) renderStatus() {
	cols := u.width
	if cols <= 0 {
		cols = 80
	}
	if u.zoomed {
		line := " [z/Esc] unzoom [j/k] scroll [y] copy [q] quit"
		if u.origin != "" {
			line = fmt.Sprintf(" %s  [z/Esc] unzoom [j/k] scroll [y] copy [q] quit", u.origin)
		}
		if u.msg != "" {
			if u.origin != "" {
				line = fmt.Sprintf(" %s  %s", u.origin, u.msg)
			} else {
				line = " " + u.msg
			}
		}
		u.status.SetText(truncWidth(line, cols))
		return
	}
	proj := truncWidth(cmp.Or(u.project, "all"), 20)
	idx := fmt.Sprintf("  row %d of %d", u.selectedRow(), len(u.shown))
	var prefix string
	if u.origin != "" {
		prefix = fmt.Sprintf(" %s  %s  project %s  pending %d  leased %d  done %d",
			u.origin, cmp.Or(u.filter, "all"), proj, u.pending, u.leased, u.done)
	} else {
		prefix = fmt.Sprintf(" %s  project %s  pending %d  leased %d  done %d",
			cmp.Or(u.filter, "all"), proj, u.pending, u.leased, u.done)
	}
	avail := cols - uniseg.StringWidth(idx)
	var line1 string
	if avail > 0 {
		line1 = truncWidth(prefix, avail) + idx
	} else {
		line1 = truncWidth(prefix, cols)
	}
	line2 := " [j/k] [0-4] filt [n] new [e] edit [+/-] pri [D] del [z] zoom [?] help [q] quit"
	if u.searching {
		line2 = truncWidth("/"+u.query, cols)
	} else if u.msg != "" {
		line2 = truncWidth(" "+u.msg, cols)
	} else if u.query != "" {
		line2 = truncWidth(fmt.Sprintf(" filter: %s  (press / to edit, Esc to clear)", u.query), cols)
	}
	u.status.SetText(line1 + "\n" + line2)
}

func (u *ui) setMsg(msg string) {
	u.msg = msg
	u.msgRev++
	rev := u.msgRev
	u.renderStatus()
	if msg != "" {
		dur := u.msgTimeout
		if dur <= 0 {
			dur = 3 * time.Second
		}
		time.AfterFunc(dur, func() {
			u.app.QueueUpdateDraw(func() {
				if u.msgRev == rev {
					u.msg = ""
					u.renderStatus()
				}
			})
		})
	}
}

func metaHeader(t task, now int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID:        %s\n", t.ID)
	fmt.Fprintf(&b, "Project:   %s\n", t.Project)
	fmt.Fprintf(&b, "Status:    %s\n", t.Status)
	fmt.Fprintf(&b, "Priority:  %d\n", t.Priority)
	fmt.Fprintf(&b, "Claims:    %d\n", t.ClaimCount)
	if t.Worker != "" {
		fmt.Fprintf(&b, "Worker:    %s\n", t.Worker)
	}
	if t.Status == "leased" {
		fmt.Fprintf(&b, "Lease:     %s\n", lease(t, now))
	}
	if t.AssetPath != "" {
		fmt.Fprintf(&b, "Asset:     %s\n", t.AssetPath)
	}
	return b.String()
}

func (u *ui) showBody() {
	var id, text string
	if t, ok := u.selected(); ok {
		text = t.Body
		if len(t.Primitives) > 0 && string(t.Primitives) != "null" {
			text += "\n\nresult: " + string(t.Primitives)
		}
		if text != "" {
			text = strings.Repeat("-", 60) + "\n" + text
		}
		id, text = t.ID, metaHeader(t, nowUnix())+text
	} else {
		text = u.emptyState()
	}
	if id != u.shownID || text != u.shownBody {
		u.shownID, u.shownBody = id, text
		u.body.SetText(text).ScrollToBeginning()
	}
}

// refresh paints the queue for project. A refresh already in flight absorbs
// the request rather than dropping it, so a keypress landing on a poll is
// never lost, and an answer for a filter the operator has already left is
// thrown away instead of clobbering the new one.
func (u *ui) refresh(project string) {
	for {
		if !u.refreshing.CompareAndSwap(false, true) {
			return
		}
		u.refreshOnce(project)
		// Read after the release above: a keypress whose CAS failed
		// published its project first, so it cannot be missed here.
		current := u.proj()
		if current == project {
			return
		}
		project = current
	}
}

// refreshOnce fetches one round for project and paints it if the operator
// has not moved on.
func (u *ui) refreshOnce(project string) {
	defer u.refreshing.Store(false)
	ts, err := u.fetch(project)
	ps, perr := u.fetchProjects()
	st, serr := u.fetchStats(project)
	u.app.QueueUpdateDraw(func() {
		// Connectivity is not project-scoped, and neither is the project
		// list, so both land before the answer itself is judged.
		switch {
		case err != nil:
			u.setMsg(err.Error())
		case perr != nil:
			u.setMsg(perr.Error())
		}
		if perr == nil {
			u.projects = ps
		}
		if u.project != project {
			return
		}
		u.loaded = true
		if err != nil {
			// The pane still reads "Loading ..." from the switch; the
			// answer was "no", so repaint it.
			u.showBody()
			return
		}
		if serr == nil {
			u.pending, u.leased, u.done = st.Pending, st.Leased, st.Done
			u.hasServerStats = true
			u.statsProject = project
		} else {
			u.hasServerStats = false
		}
		u.render(ts)
	})
}

func (u *ui) act(method, path string, body any, success string, callbacks ...func(err error)) {
	project := u.project
	go func() {
		err := u.call(method, path, body)
		msg := success
		ts, fetchErr := u.fetch(project)
		ps, projErr := u.fetchProjects()
		st, statsErr := u.fetchStats(project)
		u.app.QueueUpdateDraw(func() {
			for _, cb := range callbacks {
				cb(err)
			}
			switch {
			case err != nil:
				u.setMsg(err.Error())
			case fetchErr != nil:
				u.setMsg(fetchErr.Error())
			case projErr != nil:
				u.setMsg(projErr.Error())
			default:
				u.setMsg(msg)
			}
			if projErr == nil {
				u.projects = ps
			}
			if project != u.project {
				return
			}
			u.loaded = true
			if statsErr == nil {
				u.pending, u.leased, u.done = st.Pending, st.Leased, st.Done
				u.hasServerStats = true
				u.statsProject = project
			} else {
				u.hasServerStats = false
			}
			if fetchErr == nil {
				u.render(ts)
			}
		})
	}()
}

const maxProjectLen = 64

func validNameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
}

func validProject(p string) bool {
	if p == "" || len(p) > maxProjectLen {
		return false
	}
	for i := range len(p) {
		if !validNameByte(p[i]) {
			return false
		}
	}
	return true
}

func modalWidth(requested, window int) int {
	return min(requested, max(1, window-2))
}

func modalHeight(requested, window int) int {
	return min(requested, max(1, window*3/4))
}

type modalBox struct {
	*tview.Box
	content tview.Primitive
	width   int
	height  int
}

func (m *modalBox) SetRect(x, y, width, height int) {
	m.Box.SetRect(x, y, width, height)
	w := modalWidth(m.width, width)
	h := modalHeight(m.height, height)
	m.content.SetRect(x+(width-w)/2, y+(height-h)/2, w, h)
}

func (m *modalBox) Draw(screen tcell.Screen) {
	m.content.Draw(screen)
}

func (m *modalBox) Focus(delegate func(p tview.Primitive)) {
	delegate(m.content)
}

func (m *modalBox) HasFocus() bool {
	return m.content.HasFocus()
}

func (m *modalBox) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return m.content.MouseHandler()
}

func (m *modalBox) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return m.content.InputHandler()
}

func centerModal(p tview.Primitive, width, height int) tview.Primitive {
	return &modalBox{
		Box:     tview.NewBox(),
		content: p,
		width:   width,
		height:  height,
	}
}

func (u *ui) defaultProject() string {
	if u.project != "" {
		return u.project
	}
	if t, ok := u.selected(); ok && t.Project != "" {
		return t.Project
	}
	if name := gitCheckoutName(); name != "" {
		return name
	}
	return "taskd"
}

func (u *ui) showCreateForm() {
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" new task ")
	f.AddInputField("Project", u.defaultProject(), 20, nil, nil)
	f.AddInputField("Priority", "", 10, tview.InputFieldInteger, nil)
	f.AddInputField("Asset Path", "", 0, nil, nil)
	f.AddTextArea("Body", "", 0, 0, 0, nil)
	proj := f.GetFormItem(0).(*tview.InputField)
	pri := f.GetFormItem(1).(*tview.InputField)
	asset := f.GetFormItem(2).(*tview.InputField)
	body := f.GetFormItem(3).(*tview.TextArea)
	close := func() {
		u.form = nil
		u.pages.RemovePage("create")
		u.app.SetFocus(u.table)
	}
	submit := func() {
		pname := strings.TrimSpace(proj.GetText())
		if !validProject(pname) {
			f.SetTitle(" new task (invalid project) ")
			return
		}
		payload := map[string]any{"project": pname}
		if ptext := strings.TrimSpace(pri.GetText()); ptext != "" {
			p, err := strconv.Atoi(ptext)
			if err != nil || p < 0 {
				f.SetTitle(" new task (invalid priority) ")
				return
			}
			payload["priority"] = p
		}
		btext := strings.TrimSpace(body.GetText())
		apath := strings.TrimSpace(asset.GetText())
		if btext == "" && apath == "" {
			f.SetTitle(" new task (missing body or asset path) ")
			return
		}
		payload["body"] = btext
		payload["asset_path"] = apath
		send := func() {
			u.act("POST", "/tasks", payload, "task created", func(err error) {
				if err != nil {
					f.SetTitle(fmt.Sprintf(" new task (%s) ", err.Error()))
					return
				}
				close()
			})
		}
		if len(u.projects) > 0 && !slices.Contains(u.projects, pname) {
			text := fmt.Sprintf("Project %q is not in known projects (%s).\nCreate task anyway?",
				tview.Escape(pname), tview.Escape(strings.Join(u.projects, ", ")))
			u.confirmWithCancel("unknown-project", text, "Create", func() {
				u.app.SetFocus(f)
			}, func() {
				u.app.SetFocus(f)
				send()
			})
			return
		}
		send()
	}
	f.AddButton("Submit", submit).AddButton("Cancel", close).SetCancelFunc(close)
	u.form = f
	u.pages.AddPage("create", centerModal(f, 60, 15), true, true)
	u.app.SetFocus(f)
}

func (u *ui) showEditForm(t task) {
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" edit task ")
	proj := tview.NewInputField().SetLabel("Project").SetText(t.Project).SetFieldWidth(20)
	pri := tview.NewInputField().SetLabel("Priority").SetText(strconv.Itoa(t.Priority)).
		SetFieldWidth(10).SetAcceptanceFunc(tview.InputFieldInteger)
	asset := tview.NewInputField().SetLabel("Asset Path").SetText(t.AssetPath)
	body := tview.NewTextArea().SetLabel("Body").SetText(t.Body, false).SetSize(5, 0)
	f.AddFormItem(proj).AddFormItem(pri).AddFormItem(asset).AddFormItem(body)
	close := func() {
		u.form = nil
		u.pages.RemovePage("edit")
		u.app.SetFocus(u.table)
	}
	submit := func() {
		pname := strings.TrimSpace(proj.GetText())
		if !validProject(pname) {
			f.SetTitle(" edit task (invalid project) ")
			return
		}
		p, err := strconv.Atoi(pri.GetText())
		if err != nil || p < 0 {
			f.SetTitle(" edit task (invalid priority) ")
			return
		}
		newBody := body.GetText()
		apath := strings.TrimSpace(asset.GetText())
		if strings.TrimSpace(newBody) == "" && apath == "" {
			f.SetTitle(" edit task (missing body or asset path) ")
			return
		}
		payload := map[string]any{}
		if pname != t.Project {
			payload["project"] = pname
		}
		if p != t.Priority {
			payload["priority"] = p
		}
		if apath != t.AssetPath {
			payload["asset_path"] = apath
		}
		if newBody != t.Body {
			payload["body"] = newBody
		}
		if len(payload) == 0 {
			close()
			return
		}
		u.act("PATCH", "/tasks/"+t.ID, payload, "task updated", func(err error) {
			if err != nil {
				f.SetTitle(fmt.Sprintf(" edit task (%s) ", err.Error()))
				return
			}
			close()
		})
	}
	f.AddButton("Submit", submit).AddButton("Cancel", close).SetCancelFunc(close)
	u.form = f
	u.pages.AddPage("edit", centerModal(f, 60, 15), true, true)
	u.app.SetFocus(f)
}

func taskTitle(t task) string {
	b := strings.TrimLeft(t.Body, " \t\r\n")
	line, _, _ := strings.Cut(b, "\n")
	return cmp.Or(strings.TrimRight(line, "\r"), t.AssetPath)
}

func taskLabel(t task) string {
	title := taskTitle(t)
	if title == "" || title == t.ID {
		return t.ID
	}
	return fmt.Sprintf("%s (%s)", t.ID, title)
}

func (u *ui) confirm(page, text, button string, do func()) {
	u.confirmWithCancel(page, text, button, nil, do)
}

func (u *ui) confirmWithCancel(page, text, button string, onCancel func(), do func()) {
	m := tview.NewModal()
	m.SetText(text)
	m.AddButtons([]string{button, "Cancel"}).SetFocus(1)
	m.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		u.modal = nil
		u.pages.RemovePage(page)
		if buttonIndex == 0 {
			do()
		} else if onCancel != nil {
			onCancel()
		} else {
			u.app.SetFocus(u.table)
		}
	})
	u.modal = m
	u.pages.AddPage(page, m, false, true)
	u.app.SetFocus(m)
}

func (u *ui) showDeleteConfirm(t task) {
	text := fmt.Sprintf("Delete task %s?\nDeleted tasks cannot be recovered.", tview.Escape(taskLabel(t)))
	u.confirm("delete", text, "Delete", func() {
		path := "/tasks/" + t.ID
		if t.Status == "done" {
			path += "?force=true"
		}
		u.act("DELETE", path, nil, "deleted task "+short(t.ID))
	})
}

func (u *ui) showCompleteConfirm(t task) {
	text := fmt.Sprintf("Complete task %s?\nThe task is marked done without a worker result.", tview.Escape(taskLabel(t)))
	u.confirm("complete", text, "Complete", func() {
		if u.worker != "" && t.activelyLeased(time.Now().Unix()) && t.Worker == u.worker {
			u.act("POST", "/tasks/"+t.ID+"/done", map[string]string{"worker": u.worker}, "completed task "+short(t.ID))
			return
		}
		u.act("POST", "/tasks/"+t.ID+"/close", nil, "completed task "+short(t.ID))
	})

}

func (u *ui) showHelp() {
	prev := u.app.GetFocus()
	m := tview.NewModal()
	m.SetText(tview.Escape("Keyboard Shortcuts\n\n" +
		"[j/k] move\n" +
		"[g/G] top/bottom\n" +
		"[Ctrl+D/U] half page\n" +
		"[0-4] filter status\n" +
		"[/] keyword filter\n" +
		"[p] cycle project\n" +
		"[n] new task [e] edit task\n" +
		"[c] claim task\n" +
		"[u] release task\n" +
		"[t] touch lease\n" +
		"[x] complete task\n" +
		"[D] delete task\n" +
		"[+/-] priority\n" +
		"[z] zoom task body\n" +
		"[y] copy ID  [Y] copy body\n" +
		"[r] refresh  [?] help\n" +
		"[Tab] toggle pane focus\n" +
		"[q] quit"))
	m.AddButtons([]string{"Close"})
	close := func() {
		u.modal = nil
		u.pages.RemovePage("help")
		if prev != nil {
			u.app.SetFocus(prev)
		} else {
			u.app.SetFocus(u.table)
		}
	}
	m.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		close()
	})
	m.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Rune() == '?' || ev.Rune() == 'q' {
			close()
			return nil
		}
		return ev
	})
	u.modal = m
	u.pages.AddPage("help", m, false, true)
	u.app.SetFocus(m)
}

func deleteWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndex(s, " "); i >= 0 {
		return s[:i+1]
	}
	return ""
}

func (u *ui) keys(ev *tcell.EventKey) *tcell.EventKey {
	if u.searching {
		switch ev.Key() {
		case tcell.KeyEscape:
			u.searching = false
			u.query = ""
			u.render(u.all)
			return nil
		case tcell.KeyEnter:
			u.searching = false
			u.renderStatus()
			return nil
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if len(u.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(u.query)
				u.query = u.query[:len(u.query)-size]
				u.render(u.all)
			}
			return nil
		case tcell.KeyCtrlU:
			u.query = ""
			u.render(u.all)
			return nil
		case tcell.KeyCtrlW:
			u.query = deleteWord(u.query)
			u.render(u.all)
			return nil
		case tcell.KeyRune:
			if ev.Rune() != 0 {
				u.query += string(ev.Rune())
				u.render(u.all)
			}
			return nil
		default:
			return nil
		}
	}

	switch ev.Key() {
	case tcell.KeyEscape:
		if u.query != "" {
			u.query = ""
			u.render(u.all)
			return nil
		}
	case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyEnter:
		u.app.SetFocus(u.body)
		return nil
	case tcell.KeyCtrlD, tcell.KeyCtrlU:
		if len(u.shown) > 0 {
			_, _, _, h := u.table.GetInnerRect()
			step := max(1, (h-1)/2)
			r := u.selectedRow()
			if ev.Key() == tcell.KeyCtrlD {
				u.table.Select(min(len(u.shown), r+step), 0)
			} else {
				u.table.Select(max(1, r-step), 0)
			}
		}
		return nil
	}
	t, ok := u.selected()
	switch ev.Rune() {
	case 'q':
		u.app.Stop()
	case '?':
		u.showHelp()
	case '/':
		u.searching = true
		u.renderStatus()
		return nil
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
	case '0', '1', '2', '3', '4':
		u.filter = []string{"", "pending", "leased", "done", "live"}[ev.Rune()-'0']
		u.render(u.all)
	case 'l':
		u.filter = "live"
		u.render(u.all)
	case 'r', 'R':
		go u.refresh(u.project)
	case 'p':
		next := ""
		for i, p := range u.projects {
			if p == u.project {
				if i+1 < len(u.projects) {
					next = u.projects[i+1]
				}
				break
			}
		}
		if u.project == "" && len(u.projects) > 0 {
			next = u.projects[0]
		}
		u.setProj(next)
		u.render(u.all)
		go u.refresh(next)
	case '+', '=', '-':
		if ok {
			d := map[rune]int{'+': -1, '=': -1, '-': 1}[ev.Rune()]
			if d < 0 && t.Priority == 0 {
				break
			}
			pri := t.Priority + d
			u.act("PATCH", "/tasks/"+t.ID, map[string]int{"priority": pri}, fmt.Sprintf("priority set to %d", pri))
		}
	case 'c':
		if ok {
			if t.Status != "pending" {
				u.setMsg("task is not pending")
				break
			}
			u.act("POST", "/tasks/"+t.ID+"/claim",
				map[string]string{"worker": u.worker},
				"claimed task "+short(t.ID))
		}
	case 'u':
		if ok {
			if t.Status != "leased" {
				u.setMsg("task is not leased")
				break
			}
			u.act("POST", "/tasks/"+t.ID+"/release", map[string]string{"worker": t.Worker}, "released task "+short(t.ID))
		}
	case 't':
		if ok {
			if t.Status != "leased" {
				u.setMsg("task is not leased")
				break
			}
			u.act("POST", "/tasks/"+t.ID+"/touch", map[string]string{"worker": u.worker}, "touched task "+short(t.ID))
		}
	case 'D':
		if ok {
			if t.activelyLeased(time.Now().Unix()) {
				u.setMsg("cannot delete actively leased task")
				break
			}
			u.showDeleteConfirm(t)
		}
	case 'x':
		if ok {
			if t.Status == "done" {
				u.setMsg("task is already done")
				break
			}
			if t.activelyLeased(time.Now().Unix()) && (u.worker == "" || t.Worker != u.worker) {
				u.setMsg("cannot complete actively leased task")
				break
			}
			u.showCompleteConfirm(t)
		}
	case 'z':
		if ok {
			u.toggleZoom()
		}
	case 'n':
		u.showCreateForm()
	case 'e':
		if ok {
			if t.Status == "done" {
				u.setMsg("cannot edit done task")
				break
			}
			if t.activelyLeased(time.Now().Unix()) {
				u.setMsg("cannot edit actively leased task")
				break
			}
			u.showEditForm(t)
		}
	case 'y':
		u.copySelectedID()
	case 'Y':
		u.copySelectedBody()
	default:
		return ev
	}
	return nil
}

func (u *ui) toggleZoom() {
	if u.zoomed {
		u.zoomed = false
		u.body.SetBorder(true)
		u.flex.ResizeItem(u.table, 0, 3)
		u.flex.ResizeItem(u.body, 0, 2)
		u.flex.ResizeItem(u.status, 2, 0)
		u.app.SetFocus(u.table)
	} else {
		u.zoomed = true
		u.body.SetBorder(false)
		u.flex.ResizeItem(u.table, 0, 0)
		u.flex.ResizeItem(u.body, 0, 1)
		u.flex.ResizeItem(u.status, 1, 0)
		u.app.SetFocus(u.body)
	}
	u.renderStatus()
}

func (u *ui) bodyKeys(ev *tcell.EventKey) *tcell.EventKey {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyBacktab:
		if u.zoomed {
			return nil
		}
		u.app.SetFocus(u.table)
		return nil
	case tcell.KeyEscape:
		if u.zoomed {
			u.toggleZoom()
			return nil
		}
		u.app.SetFocus(u.table)
		return nil
	}
	if ev.Rune() == 'q' {
		u.app.Stop()
		return nil
	}
	if ev.Rune() == '?' {
		u.showHelp()
		return nil
	}
	if ev.Rune() == 'y' {
		u.copySelectedID()
		return nil
	}
	if ev.Rune() == 'Y' {
		u.copySelectedBody()
		return nil
	}
	if ev.Rune() == 'z' {
		if u.zoomed {
			u.toggleZoom()
			return nil
		}
		if _, ok := u.selected(); ok {
			u.toggleZoom()
			return nil
		}
	}
	return ev
}

func defaultWorker() string {
	if w := os.Getenv("TASKD_WORKER"); w != "" {
		return w
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	for _, k := range []string{"USER", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return host + ":" + v
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return host + ":" + u.Username
	}
	return host + ":unknown"
}

type config struct {
	url     string
	project string
	icons   bool
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: taskd-tui [options]

Terminal user interface for the taskd task queue.

Options:
  -url string
    	taskd daemon URL (default: $TASKD_URL, $T, or "http://localhost:8080")
  -project string
    	filter tasks by project (default: $TASKD_PROJECT)
  -icons
    	use Nerd Font glyphs for status and priority (default: $TASKD_TUI_ICONS)
  -h, -help
    	show this help message

Environment variables:
  TASKD_URL        taskd daemon address
  TASKD_PROJECT    default project filter
  TASKD_TUI_ICONS  enable Nerd Font glyphs (1 or true)
  TASKD_WORKER     worker identifier for claiming tasks
  T                shorthand taskd daemon address

Keyboard shortcuts:
  j, Down        Move selection down
  k, Up          Move selection up
  Ctrl+D, Ctrl+U Scroll half a page down or up
  g              Jump to first task
  G              Jump to last task
  Tab, Backtab   Switch focus between task table and task body
  /              Filter tasks by keyword
  0              Show all tasks
  1              Filter pending tasks
  2              Filter leased tasks
  3              Filter done tasks
  4, l           Filter live tasks (pending and leased)
  p              Cycle project filter
  + / =          Raise task priority (lower number)
  -              Lower task priority (higher number)
  n              Create new task
  e              Edit selected task
  c              Claim selected pending task
  u              Release selected leased task back to pending
  t              Touch lease on selected leased task
  D              Delete selected task
  x              Complete selected task
  z              Zoom task body to full screen
  y              Copy task ID to clipboard
  Y              Copy task body to clipboard
  r, R           Refresh task queue
  ?              Show this keyboard shortcut help
  q              Quit
`)
}

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url cannot be empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported protocol scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("url missing host")
	}
	return u.String(), nil
}

func parseFlags(args []string) (config, error) {
	defaultURL := strings.TrimSpace(os.Getenv("TASKD_URL"))
	if defaultURL == "" {
		defaultURL = strings.TrimSpace(os.Getenv("T"))
	}
	if defaultURL == "" {
		defaultURL = "http://localhost:8080"
	}
	defaultProject := os.Getenv("TASKD_PROJECT")
	envIcons := os.Getenv("TASKD_TUI_ICONS")
	defaultIcons := envIcons == "1" || strings.EqualFold(envIcons, "true")

	var cfg config
	fs := flag.NewFlagSet("taskd-tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&cfg.url, "url", defaultURL, "taskd daemon address")
	fs.StringVar(&cfg.project, "project", defaultProject, "filter tasks by project")
	fs.BoolVar(&cfg.icons, "icons", defaultIcons, "use Nerd Font glyphs for status and priority")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	normalizedURL, err := normalizeURL(cfg.url)
	if err != nil {
		return cfg, err
	}
	cfg.url = normalizedURL
	return cfg, nil
}

func newUI(url, project string, icons bool) *ui {
	u := &ui{
		url:     strings.TrimRight(url, "/"),
		origin:  daemonOrigin(url),
		project: project,
		icons:   icons,
		worker:  defaultWorker(),
		filter:  "live",
		app:     tview.NewApplication().EnableMouse(true),
	}
	u.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		width, _ := screen.Size()
		if width > 0 && width != u.width {
			u.width = width
			u.renderStatus()
		}
		return false
	})
	u.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlL {
			u.app.Sync()
			return nil
		}
		return ev
	})
	u.table = tview.NewTable().SetFixed(1, 0).SetSelectable(true, false)
	u.table.SetSelectionChangedFunc(func(int, int) { u.showBody(); u.renderStatus() }).SetInputCapture(u.keys)
	u.body = tview.NewTextView().SetWrap(true)
	u.body.SetBorder(true).SetTitle(" task ").SetInputCapture(u.bodyKeys)
	u.body.SetFocusFunc(func() {
		u.body.SetBorderColor(tcell.ColorYellow).SetTitleColor(tcell.ColorYellow)
	})
	u.body.SetBlurFunc(func() {
		u.body.SetBorderColor(tview.Styles.BorderColor).SetTitleColor(tview.Styles.TitleColor)
	})
	u.status = tview.NewTextView().SetWrap(false)
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.table, 0, 3, true).AddItem(u.body, 0, 2, false).AddItem(u.status, 2, 0, false)
	u.flex = flex
	u.pages = tview.NewPages().AddPage("main", flex, true, true)
	u.app.SetRoot(u.pages, true)
	u.renderStatus()
	return u
}

func (u *ui) quitOnSignal() context.CancelFunc {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		u.app.Stop()
	}()
	return stop
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(os.Stdout)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "taskd-tui: %v\n", err)
		os.Exit(2)
	}
	u := newUI(cfg.url, cfg.project, cfg.icons)
	stop := u.quitOnSignal()
	defer stop()
	go func() {
		for ; ; time.Sleep(time.Second) {
			u.refresh(u.proj())
		}
	}()
	if err := u.app.SetRoot(u.pages, true).Run(); err != nil {
		fmt.Println(err)
	}
}
