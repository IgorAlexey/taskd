package taskd

import (
	"encoding/json"
	"net/http"
	"time"
)

// A project is a row: made by POST /projects or by the first task that
// names it, gone when deleted, whatever its tasks do in between.
func (s *server) projectsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ro.Query("SELECT name FROM projects ORDER BY name ASC")
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	projects := make([]string, 0)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			internalError(w, err)
			return
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		internalError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

func (s *server) createProjectHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req, false) {
		return
	}
	if msg, ok := checkProject(req.Name); !ok {
		writeFieldError(w, http.StatusBadRequest, msg, "name")
		return
	}
	res, err := s.db.rw.Exec("INSERT OR IGNORE INTO projects (name, created_at) VALUES (?, unixepoch())", req.Name)
	if err != nil {
		internalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeFieldError(w, http.StatusConflict, "project exists", "name")
		return
	}
	s.db.events.publish()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"name": req.Name})
}

// Deleting a project deletes its tasks, unless a worker still holds one,
// since that worker would report to a task that no longer exists.
func (s *server) deleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if msg, ok := checkProject(project); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	tx, err := s.db.rw.Begin()
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	var leased int
	err = tx.QueryRow("SELECT COUNT(*) FROM tasks WHERE project = ? AND status = 'leased' AND lease_expires >= ?", project, time.Now().Unix()).Scan(&leased)
	if err != nil {
		internalError(w, err)
		return
	}
	if leased > 0 {
		writeError(w, http.StatusConflict, "project has a claimed task")
		return
	}
	projs, err := blockedProjectsBy(tx, project)
	if err != nil {
		internalError(w, err)
		return
	}
	res, err := tx.Exec("DELETE FROM projects WHERE name = ?", project)
	if err != nil {
		internalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "project not found")
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

// Renaming a project moves every task that names it. The new name must
// not already be in use: merging two projects is not what a rename means.
func (s *server) renameProjectHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if msg, ok := checkProject(project); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req, false) {
		return
	}
	if msg, ok := checkProject(req.Name); !ok {
		writeFieldError(w, http.StatusBadRequest, msg, "name")
		return
	}
	if req.Name == project {
		var exists bool
		if err := s.db.ro.QueryRow("SELECT EXISTS(SELECT 1 FROM projects WHERE name = ?)", project).Scan(&exists); err != nil {
			internalError(w, err)
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	tx, err := s.db.rw.Begin()
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	var taken bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM projects WHERE name = ?)", req.Name).Scan(&taken); err != nil {
		internalError(w, err)
		return
	}
	if taken {
		writeFieldError(w, http.StatusConflict, "project exists", "name")
		return
	}
	res, err := tx.Exec("UPDATE projects SET name = ? WHERE name = ?", req.Name, project)
	if err != nil {
		internalError(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	s.db.events.publish()
	s.db.notifyPending(req.Name)
	w.WriteHeader(http.StatusNoContent)
}
