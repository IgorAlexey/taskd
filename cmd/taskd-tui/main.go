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
	// project, workerFilter, and query are the active scope. The event goroutine
	// is their only writer and takes projectMu; readers on other
	// goroutines use proj() or scope().
	projectMu      sync.Mutex
	project        string
	workerFilter   string
	searching      bool
	query          string
	icons          bool
	worker         string
	width          int
	all            []task
	shown          []task
	total          int
	maxRows        int
	inScope        int
	passFilter     int
	projects       []string
	stats          stats
	hasServerStats bool
	statsProject   string
	msg            string
	msgRev         int
	msgTimeout     time.Duration
	actMsg         bool
	actSeq         atomic.Int64
	shownID        string
	shownBody      string
	refreshing     atomic.Bool
	disconnected   atomic.Bool
	expanded       atomic.Bool
	pollInterval   time.Duration
	// loaded reports whether an answer for the live filter has landed.
	// Adopting a filter clears it; any answer sets it, including a failed
	// one, so a filter nobody fetches cannot wedge the table.
	loaded     bool
	zoomed     bool
	jumpBottom bool
	paintWide  bool
}

func (u *ui) proj() string {
	u.projectMu.Lock()
	defer u.projectMu.Unlock()
	return u.project
}

func (u *ui) scope() (string, string, string) {
	u.projectMu.Lock()
	defer u.projectMu.Unlock()
	return u.project, u.workerFilter, u.query
}

func (u *ui) setQuery(q string) {
	u.projectMu.Lock()
	u.query = q
	u.projectMu.Unlock()
	u.render(u.all)
	go u.refresh(u.proj())
}

func (u *ui) setProj(project string) {
	u.projectMu.Lock()
	defer u.projectMu.Unlock()
	u.project = project
	u.loaded = false
	u.expanded.Store(false)
}

func (u *ui) setWorker(worker string) {
	u.projectMu.Lock()
	defer u.projectMu.Unlock()
	u.workerFilter = worker
	u.loaded = false
	u.expanded.Store(false)
}

func (u *ui) getJSON(path string, out any) error {
	_, err := u.getJSONTotal(path, out)
	return err
}

func (u *ui) getJSONTotal(path string, out any) (int, error) {
	resp, err := client.Get(u.url + path)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	label, _, _ := strings.Cut(path, "?")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		body := strings.TrimSpace(string(b))
		if body != "" {
			return -1, fmt.Errorf("GET %s: %s: %s", label, resp.Status, body)
		}
		return -1, fmt.Errorf("GET %s: %s", label, resp.Status)
	}
	total := -1
	if h := resp.Header.Get("X-Total-Count"); h != "" {
		if n, convErr := strconv.Atoi(strings.TrimSpace(h)); convErr == nil {
			total = n
		}
	}
	return total, json.NewDecoder(resp.Body).Decode(out)
}

// daemonMaxLimit mirrors the ?limit= bound enforced by listTasksHandler in
// the daemon; a wider window is answered with 400 invalid limit.
const (
	fetchPageSize  = 500
	daemonMaxLimit = 1000
)

func searchTextFor(t task) string {
	return strings.ToLower(t.Body + "\x00" + t.AssetPath + "\x00" + t.Worker + "\x00" + t.ID + "\x00" + t.Project)
}

func (u *ui) wide(query string) bool {
	return query != "" || u.expanded.Load()
}

// fetch scopes the queue to project, worker, and query. The daemon owns those
// filters and the search, so one request carries the whole window: a poll pays
// for fetchPageSize rows, and only a live search or an operator who asked for
// the bottom of a truncated queue widens it to maxRows. The wide window is
// deliberately sticky until the scope changes or g goes back to the top,
// because narrowing it under a cursor past row 500 would yank the selection.
func (u *ui) fetch(project, worker, query string) ([]task, int, error) {
	limit := fetchPageSize
	if u.wide(query) {
		limit = u.maxRows
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if project != "" {
		q.Set("project", project)
	}
	if worker != "" {
		q.Set("worker", worker)
	}
	if query != "" {
		q.Set("q", query)
	}
	var all []task
	total, err := u.getJSONTotal("/tasks?"+q.Encode(), &all)
	if err != nil {
		return nil, -1, err
	}
	for i := range all {
		all[i].searchText = searchTextFor(all[i])
	}
	if total < 0 {
		total = len(all)
	}
	return all, total, nil
}

func (u *ui) fetchProjects() ([]string, error) {
	var projects []string
	if err := u.getJSON("/projects", &projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func (u *ui) fetchWorkers() ([]string, error) {
	var workers []string
	if err := u.getJSON("/workers", &workers); err != nil {
		return nil, err
	}
	return workers, nil
}

type stats struct {
	Pending int `json:"pending"`
	Leased  int `json:"leased"`
	Done    int `json:"done"`
	Buried  int `json:"buried"`
}

func (u *ui) fetchStats(project string) (stats, error) {
	path := "/stats"
	if project != "" {
		path += "?" + url.Values{"project": {project}}.Encode()
	}
	var st stats
	if err := u.getJSON(path, &st); err != nil {
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
	iconBuried  = "\uf186"
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
	case "buried":
		glyph = iconBuried
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

func (t task) frozenReason(now int64) string {
	if t.Status == "done" {
		return "done task"
	}
	if t.activelyLeased(now) {
		return "actively leased task"
	}
	return ""
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
	case u.disconnected.Load():
		if u.origin != "" {
			return "Disconnected from daemon (" + u.origin + "). Reconnecting..."
		}
		return "Disconnected from daemon. Reconnecting..."
	case !u.loaded:
		return "Loading " + cmp.Or(u.project, "tasks") + "..."
	case u.passFilter > 0:
		return "No tasks match query. Press 'Esc' to clear."
	case u.inScope > 0 && u.workerFilter != "":
		return "No " + u.filter + " tasks for worker " + u.workerFilter + ". Press '0' to clear filter, 'w' to cycle worker."
	case u.inScope > 0 && u.project != "":
		return "No tasks match filter. Press '0' to clear filter, 'p' to cycle project."
	case u.inScope > 0:
		return "No " + u.filter + " tasks. Press '0' to show all."
	case u.workerFilter != "":
		return "No tasks for worker " + u.workerFilter + ". Press 'w' to cycle worker."
	case u.project != "":
		return "No tasks in " + u.project + ". Press 'p' to cycle project."
	}
	return "No tasks yet. Press 'n' to create a task."
}

const maxMetaWidth = 16

func (u *ui) render(all []task) {
	keep, _ := u.selected()
	u.all, u.shown = all, u.shown[:0]
	u.inScope, u.passFilter = 0, 0
	if !u.hasServerStats || u.statsProject != u.project {
		u.stats = stats{}
		for i := range all {
			t := all[i]
			if u.project != "" && t.Project != u.project {
				continue
			}
			switch t.Status {
			case "pending":
				u.stats.Pending++
			case "leased":
				u.stats.Leased++
			case "done":
				u.stats.Done++
			case "buried":
				u.stats.Buried++
			}
		}
	}
	qLower := strings.ToLower(u.query)
	for i := range all {
		if all[i].searchText == "" {
			all[i].searchText = searchTextFor(all[i])
		}
		t := all[i]
		if u.project != "" && t.Project != u.project {
			continue
		}
		u.inScope++
		var matchFilter bool
		switch u.filter {
		case "live":
			matchFilter = t.Status != "done" && t.Status != "buried"
		case "":
			matchFilter = true
		default:
			matchFilter = t.Status == u.filter
		}
		if !matchFilter {
			continue
		}
		u.passFilter++
		if matchTask(t, qLower) {
			u.shown = append(u.shown, t)
		}
	}
	u.table.Clear()
	headerClicked := func() bool { return true }
	for i, h := range []string{"STATUS", "PRI", "PROJECT", "LEASE", "WORKER", "ID", "CLAIMS", "TITLE"} {
		u.table.SetCell(0, i, tview.NewTableCell(h).SetTextColor(tcell.ColorYellow).SetSelectable(false).SetClickedFunc(headerClicked))
	}
	colors := map[string]tcell.Color{"pending": tcell.ColorWhite, "leased": tcell.ColorOrange, "done": tcell.ColorGreen, "buried": tcell.ColorRed}
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
	if u.jumpBottom && u.paintWide {
		if len(u.shown) > 0 {
			row = len(u.shown)
		}
		u.jumpBottom = false
	}
	u.paintWide = false
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
	var wk string
	if u.workerFilter != "" {
		wk = "  worker " + truncWidth(u.workerFilter, 20)
	}
	counts := fmt.Sprintf("pending %d  leased %d  buried %d  done %d",
		u.stats.Pending, u.stats.Leased, u.stats.Buried, u.stats.Done)
	idx := fmt.Sprintf("  row %d of %d", u.selectedRow(), len(u.shown))
	if u.total > len(u.all) {
		idx += fmt.Sprintf("  fetched %d of %d", len(u.all), u.total)
	}
	var prefix string
	if u.disconnected.Load() {
		if u.origin != "" {
			prefix = fmt.Sprintf(" %s  [disconnected]  project %s%s", u.origin, proj, wk)
		} else {
			prefix = fmt.Sprintf(" [disconnected]  project %s%s", proj, wk)
		}
	} else if u.origin != "" {
		prefix = fmt.Sprintf(" %s  %s  project %s%s  %s",
			u.origin, cmp.Or(u.filter, "all"), proj, wk, counts)
	} else {
		prefix = fmt.Sprintf(" %s  project %s%s  %s",
			cmp.Or(u.filter, "all"), proj, wk, counts)
	}
	avail := cols - uniseg.StringWidth(idx)
	var line1 string
	if avail > 0 {
		line1 = truncWidth(prefix, avail) + idx
	} else {
		line1 = truncWidth(prefix, cols)
	}
	line2 := " [j/k] [0-5] filt [n] new [e] edit [+/-] pri [D] del [z] zoom [?] help [q] quit"
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

// setStaleMsg reports a background refresh failure. From the moment an
// action reports until a whole refresh round lands again it says nothing,
// so a mutation the daemon already committed is never relabelled as the
// poll behind it timing out, not even after the action's own message has
// expired. For that stretch the [disconnected] marker and the body banner
// are the only report of staleness, which is where it belongs. A round is
// /tasks plus /projects because those are the two fetches whose errors can
// reach setMsg; /stats is left out only because its error merely clears
// hasServerStats. Teach stats to speak here and it joins the round.
func (u *ui) setStaleMsg(msg string) {
	if u.actMsg {
		return
	}
	u.setMsg(msg)
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
			var buf bytes.Buffer
			if err := json.Indent(&buf, t.Primitives, "", "  "); err == nil {
				text += "\n\nresult: " + buf.String()
			} else {
				text += "\n\nresult: " + string(t.Primitives)
			}
		}
		if text != "" {
			text = strings.Repeat("-", 60) + "\n" + text
		}
		id, text = t.ID, metaHeader(t, nowUnix())+text
		if u.disconnected.Load() {
			disc := "Disconnected from daemon (" + cmp.Or(u.origin, u.url) + "). Reconnecting..."
			text = disc + "\n\n" + text
		}
	} else {
		text = u.emptyState()
	}
	if id != u.shownID {
		u.shownID, u.shownBody = id, text
		u.body.SetText(text).ScrollToBeginning()
	} else if text != u.shownBody {
		row, col := u.body.GetScrollOffset()
		u.shownBody = text
		u.body.SetText(text).ScrollTo(row, col)
	}
}

// refresh paints the queue for project. A refresh already in flight absorbs
// the request rather than dropping it, so a keypress landing on a poll is
// never lost, and an answer for a filter the operator has already left is
// thrown away instead of clobbering the new one.
func (u *ui) refresh(project string) {
	_, worker, query := u.scope()
	for {
		if !u.refreshing.CompareAndSwap(false, true) {
			return
		}
		wide := u.expanded.Load()
		u.refreshOnce(project, worker, query)
		// Read after the release above: a keypress whose CAS failed
		// published its scope first, so it cannot be missed here.
		curProject, curWorker, curQuery := u.scope()
		if curProject == project && curWorker == worker && curQuery == query && u.expanded.Load() == wide {
			return
		}
		project, worker, query = curProject, curWorker, curQuery
	}
}

// refreshOnce fetches one round for project and paints it if the operator
// has not moved on.
func (u *ui) refreshOnce(project, worker, query string) {
	defer u.refreshing.Store(false)
	wide := u.wide(query)
	seq := u.actSeq.Load()
	ts, total, err := u.fetch(project, worker, query)
	if err != nil {
		u.disconnected.Store(true)
		u.app.QueueUpdateDraw(func() {
			u.setStaleMsg(err.Error())
			u.jumpBottom = false
			if u.project != project || u.workerFilter != worker || u.query != query {
				return
			}
			u.loaded = true
			u.showBody()
			u.renderStatus()
		})
		return
	}
	u.disconnected.Store(false)
	ps, perr := u.fetchProjects()
	st, serr := u.fetchStats(project)
	u.app.QueueUpdateDraw(func() {
		if perr != nil {
			u.setStaleMsg(perr.Error())
		} else {
			u.projects = ps
			if u.actSeq.Load() == seq {
				u.actMsg = false
			}
		}
		if u.project != project || u.workerFilter != worker || u.query != query {
			return
		}
		u.loaded = true
		if serr == nil && worker == "" {
			u.stats = st
			u.hasServerStats = true
			u.statsProject = project
		} else {
			u.hasServerStats = false
		}
		u.total = total
		u.paintWide = wide
		u.render(ts)
	})
}

func (u *ui) act(method, path string, body any, success string, callbacks ...func(err error)) {
	project, worker, query := u.scope()
	go func() {
		err := u.call(method, path, body)
		msg := success
		wide := u.wide(query)
		ts, total, fetchErr := u.fetch(project, worker, query)
		var ps []string
		var projErr error
		var st stats
		var statsErr error
		if fetchErr == nil {
			ps, projErr = u.fetchProjects()
			st, statsErr = u.fetchStats(project)
		}
		u.app.QueueUpdateDraw(func() {
			for _, cb := range callbacks {
				cb(err)
			}
			if err != nil {
				u.setMsg(err.Error())
			} else {
				u.setMsg(msg)
			}
			u.actMsg = fetchErr != nil || projErr != nil
			if u.actMsg {
				u.actSeq.Add(1)
			}
			if fetchErr != nil {
				u.disconnected.Store(true)
				if project == u.project && worker == u.workerFilter && query == u.query {
					u.loaded = true
					u.showBody()
					u.renderStatus()
				}
				return
			}
			u.disconnected.Store(false)
			if projErr == nil {
				u.projects = ps
			}
			if project != u.project || worker != u.workerFilter || query != u.query {
				return
			}
			u.loaded = true
			if statsErr == nil && worker == "" {
				u.stats = st
				u.hasServerStats = true
				u.statsProject = project
			} else {
				u.hasServerStats = false
			}
			u.total = total
			u.paintWide = wide
			u.render(ts)
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
	return func(action tview.MouseAction, ev *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		var consumed bool
		var capture tview.Primitive
		if h := m.content.MouseHandler(); h != nil {
			consumed, capture = h(action, ev, setFocus)
		}
		if consumed || !m.InRect(ev.Position()) {
			return consumed, capture
		}
		if action == tview.MouseLeftDown {
			setFocus(m.content)
		}
		return true, capture
	}
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
func (u *ui) confirmDiscard(f *tview.Form, dirty func() bool, close func()) func() {
	return func() {
		if !dirty() {
			close()
			return
		}
		u.confirmWithCancel("discard", "Discard unsaved changes?", "Discard", func() {
			u.app.SetFocus(f)
		}, close)
	}
}
func bindFormSubmit(f *tview.Form, submit func()) {
	capture := func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS || (ev.Modifiers()&tcell.ModCtrl != 0 && (ev.Rune() == 's' || ev.Rune() == 'S')) {
			submit()
			return nil
		}
		return ev
	}
	f.SetInputCapture(capture)
	for i := range f.GetFormItemCount() {
		if c, ok := f.GetFormItem(i).(interface {
			SetInputCapture(func(*tcell.EventKey) *tcell.EventKey) *tview.Box
		}); ok {
			c.SetInputCapture(capture)
		}
	}
	for i := range f.GetButtonCount() {
		f.GetButton(i).SetInputCapture(capture)
	}
}

func (u *ui) showCreateForm() {
	prev := u.app.GetFocus()
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" new task ")
	defaultProj := u.defaultProject()
	f.AddInputField("Project", defaultProj, 20, nil, nil)
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
		u.restoreFocus(prev)
	}
	dirty := func() bool {
		return proj.GetText() != defaultProj ||
			pri.GetText() != "" ||
			asset.GetText() != "" ||
			body.GetText() != ""
	}
	cancel := u.confirmDiscard(f, dirty, close)
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
	f.AddButton("Submit", submit).AddButton("Cancel", cancel).SetCancelFunc(cancel)
	bindFormSubmit(f, submit)
	u.form = f
	u.pages.AddPage("create", centerModal(f, 60, 15), true, true)
	u.app.SetFocus(f)
}

func (u *ui) showEditForm(t task) {
	prev := u.app.GetFocus()
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" edit task ")
	proj := tview.NewInputField().SetLabel("Project").SetText(t.Project).SetFieldWidth(20)
	priStr := strconv.Itoa(t.Priority)
	pri := tview.NewInputField().SetLabel("Priority").SetText(priStr).
		SetFieldWidth(10).SetAcceptanceFunc(tview.InputFieldInteger)
	asset := tview.NewInputField().SetLabel("Asset Path").SetText(t.AssetPath)
	body := tview.NewTextArea().SetLabel("Body").SetText(t.Body, false).SetSize(5, 0)
	f.AddFormItem(proj).AddFormItem(pri).AddFormItem(asset).AddFormItem(body)
	close := func() {
		u.form = nil
		u.pages.RemovePage("edit")
		u.restoreFocus(prev)
	}
	dirty := func() bool {
		return proj.GetText() != t.Project ||
			pri.GetText() != priStr ||
			asset.GetText() != t.AssetPath ||
			body.GetText() != t.Body
	}
	cancel := u.confirmDiscard(f, dirty, close)
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
	f.AddButton("Submit", submit).AddButton("Cancel", cancel).SetCancelFunc(cancel)
	bindFormSubmit(f, submit)
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
	prev := u.app.GetFocus()
	m := tview.NewModal()
	m.SetText(text)
	m.AddButtons([]string{button, "Cancel"}).SetFocus(1)
	m.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		u.modal = nil
		u.pages.RemovePage(page)
		u.restoreFocus(prev)
		if buttonIndex == 0 {
			do()
		} else if onCancel != nil {
			onCancel()
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

func (u *ui) showBuryConfirm(t task) {
	text := fmt.Sprintf("Bury task %s?\nThe task is parked out of the queue until it is kicked.", tview.Escape(taskLabel(t)))
	u.confirm("bury", text, "Bury", func() {
		u.act("POST", "/tasks/"+t.ID+"/bury", map[string]string{"worker": u.worker}, "buried task "+short(t.ID))
	})
}

func (u *ui) showKickConfirm(t task) {
	text := fmt.Sprintf("Kick task %s?\nThe task returns to pending and its claim count resets.", tview.Escape(taskLabel(t)))
	u.confirm("kick", text, "Kick", func() {
		u.act("POST", "/tasks/"+t.ID+"/kick", nil, "kicked task "+short(t.ID))
	})
}

func (u *ui) showHelp() {
	prev := u.app.GetFocus()
	m := tview.NewModal()
	m.SetText(tview.Escape("Keyboard Shortcuts\n\n" +
		"[j/k] move\n" +
		"[g/G] top/bottom\n" +
		"[Ctrl+D/U] half page\n" +
		"[0-5] filter status\n" +
		"[/] keyword filter\n" +
		"[p] cycle project  [w] cycle worker\n" +
		"[n] new task [e] edit task\n" +
		"[c] claim task\n" +
		"[u] release task\n" +
		"[t] touch lease\n" +
		"[x] complete task\n" +
		"[b] bury own task\n" +
		"[K] kick task\n" +
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
		u.restoreFocus(prev)
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
			u.setQuery("")
			return nil
		case tcell.KeyEnter:
			u.searching = false
			u.renderStatus()
			return nil
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if len(u.query) > 0 {
				_, size := utf8.DecodeLastRuneInString(u.query)
				u.setQuery(u.query[:len(u.query)-size])
			}
			return nil
		case tcell.KeyCtrlU:
			u.setQuery("")
			return nil
		case tcell.KeyCtrlW:
			u.setQuery(deleteWord(u.query))
			return nil
		case tcell.KeyDown, tcell.KeyCtrlN:
			if len(u.shown) > 0 {
				r := u.selectedRow()
				u.table.Select(min(len(u.shown), r+1), 0)
			}
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP:
			if len(u.shown) > 0 {
				r := u.selectedRow()
				u.table.Select(max(1, r-1), 0)
			}
			return nil
		case tcell.KeyRune:
			if ev.Rune() != 0 {
				u.setQuery(u.query + string(ev.Rune()))
			}
			return nil
		default:
			return nil
		}
	}

	switch ev.Key() {
	case tcell.KeyEscape:
		if u.query != "" {
			u.setQuery("")
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
		u.expanded.Store(false)
	case 'G':
		if len(u.shown) > 0 {
			u.table.Select(len(u.shown), 0)
		}
		if len(u.all) < u.total && len(u.all) < u.maxRows {
			u.expanded.Store(true)
			u.jumpBottom = true
			go u.refresh(u.proj())
		}
	case '0', '1', '2', '3', '4', '5':
		u.filter = []string{"", "pending", "leased", "done", "live", "buried"}[ev.Rune()-'0']
		u.render(u.all)
	case 'l':
		u.filter = "live"
		u.render(u.all)
	default:
		if u.actionKeys(ev) {
			return nil
		}
		return ev
	}
	return nil
}

func (u *ui) actionKeys(ev *tcell.EventKey) bool {
	t, ok := u.selected()
	switch ev.Rune() {
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
	case 'w':
		go u.cycleWorker()
	case '+', '=', '-':
		if ok {
			if f := t.frozenReason(time.Now().Unix()); f != "" {
				u.setMsg("cannot adjust priority on " + f)
				break
			}
			d := -1
			if ev.Rune() == '-' {
				d = 1
			}
			if d < 0 && t.Priority == 0 {
				u.setMsg("already at highest priority")
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
	case 'b':
		if ok {
			if !t.activelyLeased(time.Now().Unix()) {
				u.setMsg("task is not actively leased")
				break
			}
			if u.worker == "" || t.Worker != u.worker {
				u.setMsg("cannot bury task leased by another worker")
				break
			}
			u.showBuryConfirm(t)
		}
	case 'K':
		if ok {
			if t.Status != "buried" {
				u.setMsg("task is not buried")
				break
			}
			u.showKickConfirm(t)
		}
	case 'z':
		u.toggleZoom()
	case 'n':
		u.showCreateForm()
	case 'e':
		if ok {
			if f := t.frozenReason(time.Now().Unix()); f != "" {
				u.setMsg("cannot edit " + f)
				break
			}
			u.showEditForm(t)
		}
	case 'y':
		u.copySelectedID()
	case 'Y':
		u.copySelectedBody()
	default:
		return false
	}
	return true
}

// nextWorker advances the cycle and reports whether the active worker has
// dropped out of the daemon's list, which is a change the operator has to
// be told about rather than discover as a silently wider table.
func nextWorker(workers []string, current string) (string, bool) {
	if current == "" {
		if len(workers) == 0 {
			return "", false
		}
		return workers[0], false
	}
	for i, w := range workers {
		if w == current {
			if i+1 < len(workers) {
				return workers[i+1], false
			}
			return "", false
		}
	}
	return "", true
}

func (u *ui) cycleWorker() {
	ws, err := u.fetchWorkers()
	u.app.QueueUpdateDraw(func() {
		if err != nil {
			u.setMsg(err.Error())
			return
		}
		next, gone := nextWorker(ws, u.workerFilter)
		switch {
		case gone:
			u.setMsg("worker " + u.workerFilter + " is no longer active")
		case len(ws) == 0:
			u.setMsg("no active workers")
		}
		u.setWorker(next)
		u.render(nil)
		go u.refresh(u.project)
	})
}

func (u *ui) restoreFocus(prev tview.Primitive) {
	if prev != nil {
		u.app.SetFocus(prev)
		return
	}
	u.app.SetFocus(u.table)
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
		if _, ok := u.selected(); !ok {
			return
		}
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
	case tcell.KeyCtrlD, tcell.KeyCtrlU:
		_, _, _, h := u.body.GetInnerRect()
		step := max(1, h/2)
		row, col := u.body.GetScrollOffset()
		if ev.Key() == tcell.KeyCtrlD {
			row += step
		} else {
			row = max(0, row-step)
		}
		u.body.ScrollTo(row, col)
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
	if u.actionKeys(ev) {
		return nil
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
	worker  string
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
  -worker string
    	worker identifier for claiming tasks (default: $TASKD_WORKER)
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
  5              Filter buried tasks
  p              Cycle project filter
  w              Cycle worker filter
  + / =          Raise task priority (lower number)
  -              Lower task priority (higher number)
  n              Create new task
  e              Edit selected task
  c              Claim selected pending task
  u              Release selected leased task back to pending
  t              Touch lease on selected leased task
  D              Delete selected task
  x              Complete selected task
  b              Bury task leased by this worker
  K              Kick selected buried task back to pending
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
	defaultWorkerID := defaultWorker()
	envIcons := os.Getenv("TASKD_TUI_ICONS")
	defaultIcons := envIcons == "1" || strings.EqualFold(envIcons, "true")

	var cfg config
	fs := flag.NewFlagSet("taskd-tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&cfg.url, "url", defaultURL, "taskd daemon address")
	fs.StringVar(&cfg.project, "project", defaultProject, "filter tasks by project")
	fs.StringVar(&cfg.worker, "worker", defaultWorkerID, "worker identifier for claiming tasks")
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

func newUI(url, project string, icons bool, worker string) *ui {
	u := &ui{
		url:          strings.TrimRight(url, "/"),
		origin:       daemonOrigin(url),
		project:      project,
		icons:        icons,
		worker:       worker,
		filter:       "live",
		maxRows:      daemonMaxLimit,
		pollInterval: time.Second,
		app:          tview.NewApplication().EnableMouse(true),
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

func (u *ui) poll(ctx context.Context) {
	interval := u.pollInterval
	if interval <= 0 {
		interval = time.Second
	}
	backoff := interval
	maxBackoff := 10 * interval
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		u.refresh(u.proj())
		if u.disconnected.Load() {
			backoff = min(backoff*2, maxBackoff)
		} else {
			backoff = interval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
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
	u := newUI(cfg.url, cfg.project, cfg.icons, cfg.worker)
	stop := u.quitOnSignal()
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go u.poll(ctx)
	if err := u.app.SetRoot(u.pages, true).Run(); err != nil {
		fmt.Println(err)
	}
}
