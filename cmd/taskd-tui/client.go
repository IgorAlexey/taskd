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
	"time"

	tea "charm.land/bubbletea/v2"
)

type client struct {
	base string
	http *http.Client
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

// list fetches the queue. etag is the tag of the list the caller already
// holds; when the daemon answers 304 the returned tasks are nil and
// changed is false. On 200 the new tag comes back with the tasks.
func (c *client) list(project, etag string) (tasks []task, newETag string, changed bool, err error) {
	relPath := "/tasks?limit=500"
	if project != "" {
		relPath += "&project=" + url.QueryEscape(project)
	}
	req, err := http.NewRequest(http.MethodGet, c.base+relPath, nil)
	if err != nil {
		return nil, "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotModified:
		return nil, etag, false, nil
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
			return nil, "", false, err
		}
		return tasks, resp.Header.Get("ETag"), true, nil
	}
	return nil, "", false, parseError(resp, http.MethodGet, relPath)
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

func pollCmd(c *client, project, etag string) tea.Cmd {
	return func() tea.Msg {
		tasks, newETag, changed, err := c.list(project, etag)
		if err != nil {
			return pollMsg{project: project, err: err}
		}
		st, err := c.getStats(project)
		if err != nil {
			return pollMsg{project: project, tasks: tasks, etag: newETag, changed: changed, err: err}
		}
		projs, _ := c.getProjects()
		return pollMsg{
			project:  project,
			tasks:    tasks,
			etag:     newETag,
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
