package taskd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

const defaultPriority = 3

const maxWorkerLen = 128

var errMissingWorker = errors.New("missing worker")

var errWorkerTooLong = errors.New("worker too long")

func cleanWorker(raw string) (string, error) {
	if raw == "" {
		return "", errMissingWorker
	}
	if len(raw) > maxWorkerLen {
		return "", errWorkerTooLong
	}
	for i := range len(raw) {
		if !validNameOrPathByte(raw[i]) {
			return "", fmt.Errorf("invalid worker %q", raw)
		}
	}
	return raw, nil
}

func checkWorker(w http.ResponseWriter, raw string) (string, bool) {
	worker, err := cleanWorker(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return worker, true
}

type workerBody struct {
	Worker string `json:"worker"`
}

func decodeWorker(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req workerBody
	if !decodeJSON(w, r, &req) {
		return "", false
	}
	return checkWorker(w, req.Worker)
}

func decodeOptionalWorker(w http.ResponseWriter, r *http.Request) bool {
	var req workerBody
	return decodeBody(w, r, &req, true)
}

type taskCreateReq struct {
	ID       json.RawMessage `json:"id"`
	Body     string          `json:"body"`
	Priority *int            `json:"priority"`
	Project  string          `json:"project"`
	After    []int64         `json:"after"`
}

type fieldError struct {
	msg   string
	field string
}

func (e fieldError) Error() string {
	return e.msg
}

type validatedTask struct {
	body     string
	priority int
	project  string
	after    []int64
}

const maxProjectLen = 64

const maxAuthorLen = 128

const validNameChars = "[a-zA-Z0-9._-]"

func validNameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
}

// validNameOrPathByte is the worker and author charset: a name, or a path
// or address made of names.
func validNameOrPathByte(c byte) bool {
	return validNameByte(c) || c == '/' || c == ':'
}

func validProject(p string) bool {
	_, ok := checkProject(p)
	return ok
}

func checkProject(p string) (string, bool) {
	if p == "" {
		return "invalid project", false
	}
	if len(p) > maxProjectLen {
		return fmt.Sprintf("invalid project: exceeds %d characters", maxProjectLen), false
	}
	if p == "." || p == ".." {
		return "invalid project", false
	}
	for i := range len(p) {
		if !validNameByte(p[i]) {
			return fmt.Sprintf("invalid project %q: must contain only %s", p, validNameChars), false
		}
	}
	return "", true
}

func checkAuthor(author string) (string, bool) {
	if author == "" {
		return "invalid author", false
	}
	if len(author) > maxAuthorLen {
		return "author too long", false
	}
	for i := range len(author) {
		if !validNameOrPathByte(author[i]) {
			return "invalid author", false
		}
	}
	return "", true
}

func validateProjectFilter(w http.ResponseWriter, q url.Values) (string, bool) {
	if !q.Has("project") {
		return "", true
	}
	project := q.Get("project")
	if project == "" {
		writeError(w, http.StatusBadRequest, "project cannot be empty")
		return "", false
	}
	if project != "*" {
		if msg, ok := checkProject(project); !ok {
			writeError(w, http.StatusBadRequest, msg)
			return "", false
		}
	}
	return project, true
}
