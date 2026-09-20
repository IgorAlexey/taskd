package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type client struct {
	base string
	http *http.Client
	mu   sync.Mutex
	etag string
}

func newClient(base string) *client {
	return &client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func (c *client) getETag() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.etag
}

func (c *client) setETag(etag string) {
	c.mu.Lock()
	c.etag = etag
	c.mu.Unlock()
}

func (c *client) resetETag() {
	c.mu.Lock()
	c.etag = ""
	c.mu.Unlock()
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

func (c *client) list(project string) ([]task, bool, error) {
	relPath := "/tasks?limit=500"
	if project != "" {
		relPath += "&project=" + url.QueryEscape(project)
	}
	u := c.base + relPath
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		c.resetETag()
		return nil, false, err
	}

	etag := c.getETag()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.resetETag()
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, false, nil
	}

	if resp.StatusCode == http.StatusOK {
		var tasks []task
		if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
			c.resetETag()
			return nil, false, err
		}
		if etagHeader := resp.Header.Get("ETag"); etagHeader != "" {
			c.setETag(etagHeader)
		}
		return tasks, true, nil
	}

	c.resetETag()
	return nil, false, parseError(resp, http.MethodGet, relPath)
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

func pollCmd(c *client, project string) tea.Cmd {
	return func() tea.Msg {
		tasks, changed, err := c.list(project)
		if err != nil {
			return pollMsg{err: err}
		}
		st, err := c.getStats(project)
		if err != nil {
			return pollMsg{tasks: tasks, changed: changed, err: err}
		}
		projs, _ := c.getProjects()
		return pollMsg{
			tasks:    tasks,
			changed:  changed,
			stats:    st,
			projects: projs,
		}
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
