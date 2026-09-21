package taskd

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

func validateTask(req taskCreateReq) (validatedTask, error) {
	if req.ID != nil {
		return validatedTask{}, fieldError{msg: "id is assigned by the server", field: "id"}
	}
	project := strings.TrimSpace(req.Project)
	if strings.TrimSpace(req.Body) == "" {
		return validatedTask{}, fieldError{msg: "missing body", field: "body"}
	}
	if project == "" {
		return validatedTask{}, fieldError{msg: "missing project", field: "project"}
	}
	if msg, ok := checkProject(project); !ok {
		return validatedTask{}, fieldError{msg: msg, field: "project"}
	}
	priority := defaultPriority
	if req.Priority != nil {
		if *req.Priority < 0 {
			return validatedTask{}, fieldError{msg: fmt.Sprintf("invalid priority %d, must be 0 or greater", *req.Priority), field: "priority"}
		}
		priority = *req.Priority
	}
	for _, depID := range req.After {
		if depID <= 0 {
			return validatedTask{}, fieldError{msg: fmt.Sprintf("unknown task %d", depID), field: "after"}
		}
	}
	return validatedTask{
		body:     req.Body,
		priority: priority,
		project:  project,
		after:    req.After,
	}, nil
}

func (s *server) createTaskHandler(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	isForm := strings.HasPrefix(ct, "application/x-www-form-urlencoded")
	var reqs []taskCreateReq
	var isBatch bool
	if isForm {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data: "+err.Error())
			return
		}
		var prio *int
		if pStr := strings.TrimSpace(r.FormValue("priority")); pStr != "" {
			p, err := strconv.Atoi(pStr)
			if err != nil {
				writeFieldError(w, http.StatusBadRequest, "priority must be an integer", "priority")
				return
			}
			prio = &p
		}
		reqs = []taskCreateReq{{
			Body:     r.FormValue("body"),
			Priority: prio,
			Project:  r.FormValue("project"),
		}}
		if id := r.FormValue("id"); id != "" {
			reqs[0].ID = json.RawMessage(id)
		}
	} else {
		var raw json.RawMessage
		if !decodeJSON(w, r, &raw) {
			return
		}
		trimmed := bytes.TrimSpace(raw)
		isBatch = len(trimmed) > 0 && trimmed[0] == '['

		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()

		if isBatch {
			if err := dec.Decode(&reqs); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
		} else {
			var single taskCreateReq
			if err := dec.Decode(&single); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
			reqs = []taskCreateReq{single}
		}
	}

	tasks := make([]validatedTask, len(reqs))
	for i, req := range reqs {
		v, err := validateTask(req)
		if err != nil {
			var fe fieldError
			if errors.As(err, &fe) {
				writeFieldError(w, http.StatusBadRequest, fe.msg, fe.field)
				return
			}
			internalError(w, err)
			return
		}
		tasks[i] = v
	}

	tx, err := s.db.rw.Begin()
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO tasks (body, priority, project, created_at) VALUES (?, ?, ?, unixepoch()) RETURNING id")
	if err != nil {
		internalError(w, err)
		return
	}
	defer stmt.Close()

	createdIDs := make([]int64, len(tasks))
	for i, t := range tasks {
		if err := stmt.QueryRow(t.body, t.priority, t.project).Scan(&createdIDs[i]); err != nil {
			internalError(w, err)
			return
		}
	}
	for i, t := range tasks {
		if err := setDeps(tx, createdIDs[i], t.after); err != nil {
			var fe fieldError
			if errors.As(err, &fe) {
				writeFieldError(w, http.StatusBadRequest, fe.msg, fe.field)
				return
			}
			internalError(w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()
	for _, t := range tasks {
		s.db.notifyPending(t.project)
	}

	if isForm && strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if !isBatch {
		w.Header().Set("Location", fmt.Sprintf("/tasks/%d", createdIDs[0]))
	}
	w.WriteHeader(http.StatusCreated)
	if isBatch {
		resp := make([]map[string]int64, len(createdIDs))
		for i, id := range createdIDs {
			resp[i] = map[string]int64{"id": id}
		}
		json.NewEncoder(w).Encode(resp)
	} else {
		json.NewEncoder(w).Encode(map[string]int64{"id": createdIDs[0]})
	}
}

func (s *server) getTaskHandler(w http.ResponseWriter, r *http.Request) {
	q := requestQuery(r)
	requestedFields, err := parseTaskFields(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	var (
		item         taskItem
		worker       sql.NullString
		leaseExpires sql.NullInt64
		prim         []byte
	)
	now := time.Now().Unix()
	err = s.db.ro.QueryRow(`SELECT id,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN 'pending' ELSE status END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE worker END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE lease_expires END,
  priority, version, body, primitives, project, claim_count, created_at FROM tasks WHERE id = ?`, now, now, now, id).
		Scan(&item.ID, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Version, &item.Body, &prim, &item.Project, &item.ClaimCount, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	item.Worker = worker.String
	item.LeaseExpires = leaseExpires.Int64
	item.Primitives = prim
	if requestedFields == nil || slices.Contains(requestedFields, "after") {
		item.After, err = fetchAfter(s.db.ro, item.ID)
		if err != nil {
			internalError(w, err)
			return
		}
	}

	var notes []taskNote
	if requestedFields == nil {
		notes, err = fetchTaskNotes(s.db.ro, item.ID)
		if err != nil {
			internalError(w, err)
			return
		}
	}

	var buf bytes.Buffer
	if requestedFields != nil {
		writeProjectedTask(&buf, item, requestedFields)
		buf.WriteByte('\n')
	} else {
		if err := json.NewEncoder(&buf).Encode(taskDetail{taskItem: item, Notes: notes}); err != nil {
			internalError(w, err)
			return
		}
	}
	h := fnv.New64a()
	h.Write(buf.Bytes())
	etag := fmt.Sprintf("\"%x\"", h.Sum64())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(buf.Bytes())
}

func (s *server) patchTaskHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body      *string  `json:"body"`
		Priority  *int     `json:"priority"`
		Project   *string  `json:"project"`
		IfVersion *int     `json:"if_version"`
		After     *[]int64 `json:"after"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Body == nil && req.Priority == nil && req.Project == nil && req.After == nil {
		writeError(w, http.StatusBadRequest, "missing fields to update")
		return
	}
	if req.IfVersion != nil && *req.IfVersion < 1 {
		writeError(w, http.StatusBadRequest, "invalid if_version")
		return
	}
	if req.Body != nil && strings.TrimSpace(*req.Body) == "" {
		writeFieldError(w, http.StatusBadRequest, "missing body", "body")
		return
	}
	if req.Priority != nil && *req.Priority < 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid priority %d, must be 0 or greater", *req.Priority))
		return
	}
	if req.Project != nil {
		*req.Project = strings.TrimSpace(*req.Project)
		if msg, ok := checkProject(*req.Project); !ok {
			writeFieldError(w, http.StatusBadRequest, msg, "project")
			return
		}
	}

	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	var prevProject string
	if req.Project != nil {
		_ = s.db.ro.QueryRow("SELECT project FROM tasks WHERE id = ?", id).Scan(&prevProject)
	}

	tx, err := s.db.rw.Begin()
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()

	var updatedStatus, updatedProject string
	err = tx.QueryRow(`UPDATE tasks
SET body = COALESCE(?, body), priority = COALESCE(?, priority), project = COALESCE(?, project), version = version + 1
WHERE id = ? AND status != 'done' AND NOT (status = 'leased' AND lease_expires >= unixepoch())
  AND (? IS NULL OR version = ?)
RETURNING status, project`,
		req.Body, req.Priority, req.Project, id, req.IfVersion, req.IfVersion).
		Scan(&updatedStatus, &updatedProject)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if req.IfVersion == nil {
				writeTaskStateError(w, s.db.ro, id, "", nil)
				return
			}
			var (
				status   string
				holder   sql.NullString
				claims   int
				curVer   int
				isLeased int
			)
			err := s.db.ro.QueryRow(`SELECT status, worker, claim_count, version, (status = 'leased' AND lease_expires >= unixepoch()) FROM tasks WHERE id = ?`, id).
				Scan(&status, &holder, &claims, &curVer, &isLeased)
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "task not found")
				return
			}
			if err != nil {
				internalError(w, err)
				return
			}
			if isLeased == 1 {
				writeAPIError(w, http.StatusConflict, leaseConflict{Error: "task is leased", Worker: holder.String, ClaimCount: claims})
				return
			}
			if status == "done" {
				writeError(w, http.StatusConflict, "task is done")
				return
			}
			if curVer != *req.IfVersion {
				writeError(w, http.StatusConflict, "version conflict")
				return
			}
			writeError(w, http.StatusConflict, "task is "+status)
			return
		}
		internalError(w, err)
		return
	}
	if req.After != nil {
		if err := setDeps(tx, id, *req.After); err != nil {
			var fe fieldError
			if errors.As(err, &fe) {
				writeFieldError(w, http.StatusBadRequest, fe.msg, fe.field)
				return
			}
			internalError(w, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()

	if ((req.Project != nil && *req.Project != prevProject) || req.After != nil) && updatedStatus == "pending" && updatedProject != "" {
		s.db.notifyPending(updatedProject)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteTaskHandler(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.rw.Begin()
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()

	id, ok := pathTaskID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	var (
		status       string
		leaseExpires sql.NullInt64
	)
	err = tx.QueryRow("SELECT status, lease_expires FROM tasks WHERE id = ?", id).Scan(&status, &leaseExpires)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if status == "leased" && leaseExpires.Int64 >= time.Now().Unix() {
		writeError(w, http.StatusConflict, "task is leased")
		return
	}
	f := requestQuery(r).Get("force")
	force := f == "1" || f == "true"
	if status == "done" && !force {
		writeError(w, http.StatusConflict, "task is done")
		return
	}
	projs, _ := blockedProjects(tx, id)
	if _, err := tx.Exec("DELETE FROM tasks WHERE id = ?", id); err != nil {
		internalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()
	for _, p := range projs {
		s.db.notifyPending(p)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) purgeTasksHandler(w http.ResponseWriter, r *http.Request) {
	q := requestQuery(r)
	var req struct {
		Project *string `json:"project"`
	}
	if !decodeBody(w, r, &req, true) {
		return
	}

	if q.Has("project") && req.Project != nil && q.Get("project") != *req.Project {
		writeError(w, http.StatusBadRequest, "conflicting project parameter")
		return
	}

	var project string
	if req.Project != nil {
		project = *req.Project
	} else if q.Has("project") {
		project = q.Get("project")
	}

	var projectFilter string
	if project != "" {
		if project != "*" {
			if msg, ok := checkProject(project); !ok {
				writeError(w, http.StatusBadRequest, msg)
				return
			}
			projectFilter = project
		}
	} else if q.Has("project") || req.Project != nil {
		writeError(w, http.StatusBadRequest, "project cannot be empty")
		return
	}

	query := "DELETE FROM tasks WHERE status = 'done'"
	var args []any
	if projectFilter != "" {
		query += " AND project = ?"
		args = append(args, projectFilter)
	}
	res, err := s.db.rw.Exec(query, args...)
	if err != nil {
		internalError(w, err)
		return
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		internalError(w, err)
		return
	}
	if deleted > 0 {
		s.db.events.publish()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{"deleted": deleted})
}
