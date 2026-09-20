package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type client struct {
	base string
	http *http.Client

	// mu guards cancelWalk, the stop switch of the list walk currently on
	// the wire. Starting a walk cancels the one it supersedes, so a
	// keypress does not leave pages being pulled for an answer nobody
	// will read.
	mu         sync.Mutex
	cancelWalk context.CancelFunc
}

func newClient(base string) *client {
	return &client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func parseError(resp *http.Response, method, path string) error {
	body, err := io.ReadAll(resp.Body)
	if err == nil && len(body) > 0 {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return errors.New(errResp.Error)
		}
	}
	return fmt.Errorf("%s %s: %s", method, path, resp.Status)
}

// listScope is the question put to GET /tasks: the rows wanted and how many
// pages of the daemon cursor to walk for them.
type listScope struct {
	filter listFilter
	pages  int
}

// listFilter is what selects rows: project, status and search term. It is
// one comparable value, so a reply for a question the operator has moved on
// from can be dropped on arrival.
type listFilter struct {
	project string
	status  string
	query   string
}

// listResult is one answer. total is the daemon's count for the question
// and more reports rows it held back, so the footer can say the list is
// short rather than pretend it is whole.
type listResult struct {
	tasks   []task
	etag    string
	changed bool
	total   int
	more    bool
}

const (
	tasksPageLimit = 500
	tasksMaxPages  = 10
	maxPageDepth   = 40
	maxWalkTime    = 15 * time.Second
)

// list fetches the queue. etag is the tag of the list the caller already
// holds; when the daemon answers 304 the returned tasks are nil and changed
// is false. A walk deeper than one page cannot be answered by a single tag,
// so it always asks in full, and a walk that breaks halfway hands back the
// pages it did get along with the error.
func (c *client) list(sc listScope, etag string) (listResult, error) {
	q := url.Values{"limit": {strconv.Itoa(tasksPageLimit)}}
	if sc.filter.project != "" {
		q.Set("project", sc.filter.project)
	}
	if sc.filter.status != "" {
		q.Set("status", sc.filter.status)
	}
	if sc.filter.query != "" {
		q.Set("q", sc.filter.query)
	}
	pages := max(sc.pages, 1)
	budget := min(time.Duration(pages)*c.http.Timeout, maxWalkTime)
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	c.mu.Lock()
	if c.cancelWalk != nil {
		c.cancelWalk()
	}
	c.cancelWalk = cancel
	c.mu.Unlock()

	var out listResult
	total := -1
	partial := func(page int, err error) (listResult, error) {
		if page == 0 {
			return listResult{}, err
		}
		out.changed, out.more, out.etag = true, true, ""
		out.total = max(total, len(out.tasks))
		return out, err
	}
	// The daemon's cursor is (priority, rowid) and priority is mutable, so
	// a task repriced between two pages can come back on both. Dropping
	// the second copy keeps the list countable.
	seen := make(map[string]bool, tasksPageLimit)
	for page := 0; ; page++ {
		relPath := "/tasks?" + q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+relPath, nil)
		if err != nil {
			return partial(page, err)
		}
		if page == 0 && pages == 1 && etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return partial(page, err)
		}
		if resp.StatusCode == http.StatusNotModified {
			resp.Body.Close()
			return listResult{etag: etag}, nil
		}
		if resp.StatusCode != http.StatusOK {
			err = parseError(resp, http.MethodGet, relPath)
			resp.Body.Close()
			return partial(page, err)
		}
		var ts []task
		if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
			resp.Body.Close()
			return partial(page, err)
		}
		if page == 0 {
			out.etag = resp.Header.Get("ETag")
			if n, err := strconv.Atoi(resp.Header.Get("X-Total-Count")); err == nil {
				total = n
			}
		}
		next := resp.Header.Get("X-Next-Cursor")
		resp.Body.Close()
		for i := range ts {
			if seen[ts[i].ID] {
				continue
			}
			seen[ts[i].ID] = true
			out.tasks = append(out.tasks, ts[i])
		}
		// The daemon hands out a cursor for any full page without looking
		// ahead, so a queue of exactly one page says "more" until its own
		// count contradicts it. An empty page cannot be walked past.
		done := next == "" || len(ts) == 0 || (total >= 0 && len(out.tasks) >= total)
		if done || page+1 >= pages {
			out.more = !done
			break
		}
		q.Set("after", next)
	}
	out.changed = true
	out.total = max(total, len(out.tasks))
	return out, nil
}

func (c *client) getStats(project string) (stats, error) {
	relPath := "/stats"
	if project != "" {
		relPath += "?project=" + url.QueryEscape(project)
	}
	u := c.base + relPath
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return stats{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return stats{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return stats{}, parseError(resp, http.MethodGet, relPath)
	}
	var s stats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return stats{}, err
	}
	return s, nil
}

func (c *client) getProjects() ([]string, error) {
	relPath := "/projects"
	u := c.base + relPath
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseError(resp, http.MethodGet, relPath)
	}
	var projects []string
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, err
	}
	return projects, nil
}

func (c *client) do(method, path string, body any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}
	u := c.base + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequest(method, u, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return parseError(resp, method, path)
}

func pollCmd(c *client, sc listScope, etag string, seq uint64) tea.Cmd {
	return func() tea.Msg {
		res, err := c.list(sc, etag)
		msg := pollMsg{
			seq:     seq,
			scope:   sc,
			tasks:   res.tasks,
			etag:    res.etag,
			changed: res.changed,
			total:   res.total,
			more:    res.more,
		}
		if err != nil {
			msg.err = err
			return msg
		}
		st, err := c.getStats(sc.filter.project)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.stats = st
		msg.projects, _ = c.getProjects()
		return msg
	}
}

// statsCmd refreshes the counters alone. A paged snapshot must not be
// re-walked on every tick, but the header has no reason to go stale with it.
func statsCmd(c *client, project string) tea.Cmd {
	return func() tea.Msg {
		st, err := c.getStats(project)
		return statsMsg{stats: st, err: err}
	}
}

func actCmd(c *client, method, path string, body any, success string) tea.Cmd {
	return func() tea.Msg {
		if err := c.do(method, path, body); err != nil {
			return actMsg{err: err}
		}
		return actMsg{msg: success}
	}
}

func copyToClipboard(text string) tea.Cmd {
	return tea.Batch(
		tea.SetClipboard(text),
		func() tea.Msg {
			return actMsg{msg: "copied to clipboard"}
		},
	)
}
