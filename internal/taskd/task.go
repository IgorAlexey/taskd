package taskd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

var errTaskNotFound = errors.New("task not found")

func pathTaskID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

type taskItem struct {
	ID           int64           `json:"id"`
	Status       string          `json:"status"`
	Worker       string          `json:"worker"`
	LeaseExpires int64           `json:"lease_expires"`
	Priority     int             `json:"priority"`
	Version      int             `json:"version"`
	Body         string          `json:"body"`
	Primitives   json.RawMessage `json:"primitives"`
	Project      string          `json:"project"`
	ClaimCount   int             `json:"claim_count"`
	CreatedAt    int64           `json:"created_at"`
	After        []int64         `json:"after"`
	LastNote     *taskNote       `json:"last_note,omitempty"`

	summaryPrefix string
}

const maxNoteTextLen = 65536

type taskNote struct {
	ID        int64  `json:"id"`
	CreatedAt int64  `json:"created_at"`
	Author    string `json:"author"`
	Text      string `json:"text"`
}

type taskDetail struct {
	taskItem
	Notes []taskNote `json:"notes"`
}

func fetchTaskNotes(q queryer, taskID int64) ([]taskNote, error) {
	notes := make([]taskNote, 0)
	rows, err := q.Query("SELECT id, created_at, author, text FROM notes WHERE task_id = ? ORDER BY id ASC", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n taskNote
		if err := rows.Scan(&n.ID, &n.CreatedAt, &n.Author, &n.Text); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notes, nil
}

func fetchAfter(q queryer, id int64) ([]int64, error) {
	after := make([]int64, 0)
	rows, err := q.Query("SELECT depends_on_id FROM task_deps WHERE task_id = ? ORDER BY depends_on_id ASC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var depID int64
		if err := rows.Scan(&depID); err != nil {
			return nil, err
		}
		after = append(after, depID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return after, nil
}

func setDeps(tx *sql.Tx, id int64, after []int64) error {
	if _, err := tx.Exec("DELETE FROM task_deps WHERE task_id = ?", id); err != nil {
		return err
	}
	if len(after) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(after))
	for _, depID := range after {
		if depID <= 0 {
			return fieldError{msg: fmt.Sprintf("unknown task %d", depID), field: "after"}
		}
		if depID == id {
			return fieldError{msg: "self reference", field: "after"}
		}
		if seen[depID] {
			continue
		}
		seen[depID] = true
		var exists int
		err := tx.QueryRow("SELECT 1 FROM tasks WHERE id = ?", depID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return fieldError{msg: fmt.Sprintf("unknown task %d", depID), field: "after"}
		}
		if err != nil {
			return err
		}
		var cycle int
		err = tx.QueryRow(`WITH RECURSIVE reachable(node) AS (
			SELECT ?
			UNION
			SELECT depends_on_id FROM task_deps JOIN reachable ON task_id = node
		)
		SELECT 1 FROM reachable WHERE node = ? LIMIT 1`, depID, id).Scan(&cycle)
		if err == nil {
			return fieldError{msg: "dependency cycle", field: "after"}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	stmt, err := tx.Prepare("INSERT INTO task_deps (task_id, depends_on_id) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	sorted := make([]int64, 0, len(seen))
	for depID := range seen {
		sorted = append(sorted, depID)
	}
	slices.Sort(sorted)
	for _, depID := range sorted {
		if _, err := stmt.Exec(id, depID); err != nil {
			return err
		}
	}
	return nil
}

func blockedProjects(q queryer, doneID int64) ([]string, error) {
	rows, err := q.Query("SELECT DISTINCT t.project FROM task_deps d JOIN tasks t ON t.id = d.task_id WHERE d.depends_on_id = ?", doneID)
	if err != nil {
		return nil, err
	}
	return scanProjects(rows)
}

// blockedProjectsBy names the other projects with a task waiting on any
// task of the given project.
func blockedProjectsBy(q queryer, project string) ([]string, error) {
	rows, err := q.Query(`SELECT DISTINCT t.project FROM task_deps d
JOIN tasks t ON t.id = d.task_id
JOIN tasks dep ON dep.id = d.depends_on_id
WHERE dep.project = ? AND t.project != ?`, project, project)
	if err != nil {
		return nil, err
	}
	return scanProjects(rows)
}

func scanProjects(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var projs []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		projs = append(projs, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projs, nil
}

const summaryRunes = 50

const summaryTrim = " \t\r\n"

var summaryPrefixCol = fmt.Sprintf("substr(ltrim(body, %s), 1, %d)", sqlCharset(summaryTrim), summaryRunes+1)

func sqlCharset(cut string) string {
	parts := make([]string, 0, len(cut))
	for _, r := range cut {
		parts = append(parts, fmt.Sprintf("char(%d)", r))
	}
	return strings.Join(parts, " || ")
}

func summaryLine(body string) string {
	s := strings.TrimLeft(body, summaryTrim)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > summaryRunes {
		return string(r[:summaryRunes]) + "\u2026"
	}
	return s
}
