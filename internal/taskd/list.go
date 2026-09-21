package taskd

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type listCursor struct {
	Rowid   int64  `json:"r"`
	Filters string `json:"f"`
}

func listFilterFingerprint(q url.Values) string {
	var b strings.Builder
	for _, k := range []string{"status", "project", "worker", "priority", "q"} {
		if q.Has(k) {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(q.Get(k))
		}
		b.WriteByte(0)
	}
	if q.Get("order") == "desc" {
		b.WriteString("order=desc")
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:9])
}

func encodeListCursor(c listCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeListCursor(s, fingerprint string) (listCursor, bool) {
	var c listCursor
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return c, false
	}
	if c.Rowid < 1 || c.Filters != fingerprint {
		return c, false
	}
	return c, true
}

const validTaskFieldsList = "[id, status, worker, lease_expires, priority, body, primitives, project, claim_count, summary, created_at, version, after]"

func parseTaskFields(q url.Values) ([]string, error) {
	if !q.Has("fields") && !q.Has("columns") {
		return nil, nil
	}
	fieldsParam := q.Get("fields")
	if fieldsParam == "" && q.Has("columns") {
		fieldsParam = q.Get("columns")
	}
	if strings.TrimSpace(fieldsParam) == "" {
		return nil, fmt.Errorf("invalid field %q, must be one of %s", fieldsParam, validTaskFieldsList)
	}
	parts := strings.Split(fieldsParam, ",")
	seen := make(map[string]bool, len(parts))
	var fields []string
	for _, p := range parts {
		f := strings.TrimSpace(p)
		switch f {
		case "id", "status", "worker", "lease_expires", "priority", "body", "primitives", "project", "claim_count", "summary", "created_at", "version", "after":
			if !seen[f] {
				seen[f] = true
				fields = append(fields, f)
			}
		default:
			return nil, fmt.Errorf("invalid field %q, must be one of %s", f, validTaskFieldsList)
		}
	}
	return fields, nil
}

func taskSummary(item taskItem) string {
	text := item.summaryPrefix
	if text == "" {
		text = item.Body
	}
	return summaryLine(text)
}

func writeProjectedTask(buf *bytes.Buffer, item taskItem, fields []string) {
	buf.WriteByte('{')
	for j, f := range fields {
		if j > 0 {
			buf.WriteByte(',')
		}
		buf.WriteByte('"')
		buf.WriteString(f)
		buf.WriteString(`":`)
		switch f {
		case "id":
			buf.WriteString(strconv.FormatInt(item.ID, 10))
		case "status":
			b, _ := json.Marshal(item.Status)
			buf.Write(b)
		case "worker":
			b, _ := json.Marshal(item.Worker)
			buf.Write(b)
		case "lease_expires":
			buf.WriteString(strconv.FormatInt(item.LeaseExpires, 10))
		case "priority":
			buf.WriteString(strconv.Itoa(item.Priority))
		case "version":
			buf.WriteString(strconv.Itoa(item.Version))
		case "body":
			b, _ := json.Marshal(item.Body)
			buf.Write(b)
		case "primitives":
			if len(item.Primitives) > 0 {
				buf.Write(item.Primitives)
			} else {
				buf.WriteString("null")
			}
		case "project":
			b, _ := json.Marshal(item.Project)
			buf.Write(b)
		case "claim_count":
			buf.WriteString(strconv.Itoa(item.ClaimCount))
		case "created_at":
			buf.WriteString(strconv.FormatInt(item.CreatedAt, 10))
		case "summary":
			b, _ := json.Marshal(taskSummary(item))
			buf.Write(b)
		case "after":
			if len(item.After) == 0 {
				buf.WriteString("[]")
			} else {
				b, _ := json.Marshal(item.After)
				buf.Write(b)
			}
		}
	}
	buf.WriteByte('}')
}

func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func workerFilterClause(worker string, now int64) (string, []any) {
	if worker != "" {
		return "worker = ? AND (status = 'done' OR (status = 'leased' AND lease_expires >= ?))", []any{worker, now}
	}
	return "(worker IS NULL OR worker = '' OR (status = 'leased' AND lease_expires < ?))", []any{now}
}

// etagMatches implements If-None-Match: a comma-separated list of
// entity tags, each optionally weak (W/), or the wildcard "*".
func etagMatches(header, etag string) bool {
	for header != "" {
		var tag string
		tag, header, _ = strings.Cut(header, ",")
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}

func (s *server) listTasksHandler(w http.ResponseWriter, r *http.Request) {
	projects, err := s.db.sweep()
	if err != nil {
		internalError(w, err)
		return
	}
	for _, p := range projects {
		s.db.notifyPending(p)
	}
	q := requestQuery(r)
	status := q.Get("status")
	switch status {
	case "", "pending", "leased", "done", "buried", "live":
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid status %q, must be one of [pending, leased, done, buried, live]", status))
		return
	}
	if status == "" {
		q.Del("status")
	}
	project, ok := validateProjectFilter(w, q)
	if !ok {
		return
	}
	var priorityFilter *int
	if q.Has("priority") {
		raw := q.Get("priority")
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid priority %q: not an integer", raw))
			return
		}
		if v < 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid priority %d, must be 0 or greater", v))
			return
		}
		priorityFilter = &v
	}
	limit := 100
	if q.Has("limit") {
		raw := q.Get("limit")
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit %q: not an integer", raw))
			return
		}
		if v < 1 || v > 1000 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit %d, must be between 1 and 1000", v))
			return
		}
		limit = v
	}
	if q.Has("after") && q.Has("offset") {
		writeError(w, http.StatusBadRequest, "after and offset are mutually exclusive")
		return
	}
	offset := 0
	if q.Has("offset") {
		raw := q.Get("offset")
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid offset %q: not an integer", raw))
			return
		}
		if v < 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid offset %d, must be 0 or greater", v))
			return
		}
		offset = v
	}
	order := "asc"
	if q.Has("order") {
		order = strings.ToLower(strings.TrimSpace(q.Get("order")))
		if order != "asc" && order != "desc" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid order %q, must be one of [asc, desc]", order))
			return
		}
		q.Set("order", order)
	}
	var after *listCursor
	if q.Has("after") {
		c, ok := decodeListCursor(q.Get("after"), listFilterFingerprint(q))
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid after")
			return
		}
		after = &c
	}
	sortCol := ""
	if q.Has("sort") {
		sortCol = strings.ToLower(strings.TrimSpace(q.Get("sort")))
		switch sortCol {
		case "id", "project", "status", "priority", "claim_count", "worker", "created_at":
		default:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid sort %q, must be one of [id, project, status, priority, claim_count, worker, created_at]", sortCol))
			return
		}
		if after != nil {
			writeError(w, http.StatusBadRequest, "cannot combine sort and after")
			return
		}
		q.Set("sort", sortCol)
	}
	now := time.Now().Unix()
	requestedFields, err := parseTaskFields(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	bodyCol, primCol, summaryCol := "body", "primitives", "''"
	if requestedFields != nil {
		if !slices.Contains(requestedFields, "body") {
			bodyCol = "''"
		}
		if !slices.Contains(requestedFields, "primitives") {
			primCol = "NULL"
		}
		if slices.Contains(requestedFields, "summary") {
			summaryCol = summaryPrefixCol
		}
	}
	query := fmt.Sprintf(`SELECT id,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN 'pending' ELSE status END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE worker END,
  CASE WHEN status = 'leased' AND lease_expires < ? THEN NULL ELSE lease_expires END,
  priority, version, %s, %s, %s, project, claim_count, created_at, rowid FROM tasks`, bodyCol, primCol, summaryCol)
	var where []string
	var whereArgs []any
	if status == "pending" {
		where = append(where, "(status = 'pending' OR (status = 'leased' AND lease_expires < ?))")
		whereArgs = append(whereArgs, now)
	} else if status == "leased" {
		where = append(where, "(status = 'leased' AND lease_expires >= ?)")
		whereArgs = append(whereArgs, now)
	} else if status == "live" {
		where = append(where, "(status = 'pending' OR status = 'leased')")
	} else if status != "" {
		where = append(where, "status = ?")
		whereArgs = append(whereArgs, status)
	}
	if project != "" && project != "*" {
		where = append(where, "project = ?")
		whereArgs = append(whereArgs, project)
	}
	if q.Has("worker") {
		clause, cargs := workerFilterClause(strings.TrimSpace(q.Get("worker")), now)
		where = append(where, clause)
		whereArgs = append(whereArgs, cargs...)
	}
	if priorityFilter != nil {
		where = append(where, "priority = ?")
		whereArgs = append(whereArgs, *priorityFilter)
	}
	if q.Has("q") {
		search := strings.TrimSpace(q.Get("q"))
		if search != "" {
			pat := "%" + escapeLike(search) + "%"
			where = append(where, "(CAST(id AS TEXT) LIKE ? ESCAPE '\\' OR body LIKE ? ESCAPE '\\' OR project LIKE ? ESCAPE '\\' OR (worker LIKE ? ESCAPE '\\' AND (status = 'done' OR (status = 'leased' AND lease_expires >= ?))))")
			whereArgs = append(whereArgs, pat, pat, pat, pat, now)
		}
	}
	var whereSQL string
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	dataWhereSQL := whereSQL
	args := make([]any, 0, 3+len(whereArgs)+3)
	args = append(args, now, now, now)
	args = append(args, whereArgs...)
	if after != nil {
		rowidClause := "rowid > ?"
		if order == "desc" {
			rowidClause = "rowid < ?"
		}
		dataWhere := append(slices.Clone(where), rowidClause)
		dataWhereSQL = " WHERE " + strings.Join(dataWhere, " AND ")
		args = append(args, after.Rowid)
	}
	query += dataWhereSQL
	orderDir := "ASC"
	if order == "desc" {
		orderDir = "DESC"
	}
	if sortCol != "" {
		query += fmt.Sprintf(" ORDER BY %s %s, rowid %s LIMIT ?", sortCol, orderDir, orderDir)
	} else if order == "desc" {
		query += " ORDER BY rowid DESC LIMIT ?"
	} else {
		query += " ORDER BY rowid ASC LIMIT ?"
	}
	args = append(args, limit)
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}
	rows, err := s.db.ro.Query(query, args...)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()

	tasks := make([]taskItem, 0)
	var next listCursor
	for rows.Next() {
		var (
			item         taskItem
			worker       sql.NullString
			leaseExpires sql.NullInt64
			prim         []byte
			rowid        int64
		)
		if err := rows.Scan(&item.ID, &item.Status, &worker, &leaseExpires, &item.Priority, &item.Version, &item.Body, &prim, &item.summaryPrefix, &item.Project, &item.ClaimCount, &item.CreatedAt, &rowid); err != nil {
			internalError(w, err)
			return
		}
		item.Worker = worker.String
		item.LeaseExpires = leaseExpires.Int64
		item.Primitives = prim
		next = listCursor{Rowid: rowid}
		tasks = append(tasks, item)
	}
	if err := rows.Err(); err != nil {
		internalError(w, err)
		return
	}
	if (requestedFields == nil || slices.Contains(requestedFields, "after")) && len(tasks) > 0 {
		taskIndex := make(map[int64]int, len(tasks))
		inArgs := make([]any, len(tasks))
		for i := range tasks {
			tasks[i].After = make([]int64, 0)
			taskIndex[tasks[i].ID] = i
			inArgs[i] = tasks[i].ID
		}
		depQuery := "SELECT task_id, depends_on_id FROM task_deps WHERE task_id IN (" + strings.Repeat("?,", len(tasks)-1) + "?) ORDER BY task_id, depends_on_id"
		depRows, err := s.db.ro.Query(depQuery, inArgs...)
		if err != nil {
			internalError(w, err)
			return
		}
		defer depRows.Close()
		for depRows.Next() {
			var tid, depID int64
			if err := depRows.Scan(&tid, &depID); err != nil {
				internalError(w, err)
				return
			}
			if idx, ok := taskIndex[tid]; ok {
				tasks[idx].After = append(tasks[idx].After, depID)
			}
		}
		if err := depRows.Err(); err != nil {
			internalError(w, err)
			return
		}
	}

	// The last note rides along with each row so a list can show what
	// a worker last said without a request per task. The ids travel as
	// one JSON array, so a page of any size stays one bound variable.
	if requestedFields == nil && len(tasks) > 0 {
		taskIndex := make(map[int64]int, len(tasks))
		ids := make([]int64, len(tasks))
		for i := range tasks {
			taskIndex[tasks[i].ID] = i
			ids[i] = tasks[i].ID
		}
		idJSON, _ := json.Marshal(ids)
		noteRows, err := s.db.ro.Query(`SELECT n.task_id, n.id, n.created_at, n.author, n.text
FROM json_each(?) j JOIN notes n ON n.id = (SELECT MAX(id) FROM notes WHERE task_id = j.value)`, string(idJSON))
		if err != nil {
			internalError(w, err)
			return
		}
		defer noteRows.Close()
		for noteRows.Next() {
			var tid int64
			var n taskNote
			if err := noteRows.Scan(&tid, &n.ID, &n.CreatedAt, &n.Author, &n.Text); err != nil {
				internalError(w, err)
				return
			}
			if idx, ok := taskIndex[tid]; ok {
				tasks[idx].LastNote = &n
			}
		}
		if err := noteRows.Err(); err != nil {
			internalError(w, err)
			return
		}
	}

	// A short page means the query reached the end of the set, so the
	// rows in hand already give the total; only a full page can hide
	// more. An empty page behind an offset says nothing about what it
	// skipped, so that case still has to count.
	total := offset + len(tasks)
	if after == nil && (len(tasks) == limit || (offset > 0 && len(tasks) == 0)) {
		if err := s.db.ro.QueryRow("SELECT COUNT(*) FROM tasks"+whereSQL, whereArgs...).Scan(&total); err != nil {
			internalError(w, err)
			return
		}
	}

	var buf bytes.Buffer
	if requestedFields == nil {
		json.NewEncoder(&buf).Encode(tasks)
	} else {
		buf.WriteByte('[')
		for i, item := range tasks {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeProjectedTask(&buf, item, requestedFields)
		}
		buf.WriteString("]\n")
	}

	h := fnv.New64a()
	h.Write(buf.Bytes())
	etag := fmt.Sprintf("\"%x\"", h.Sum64())
	w.Header().Set("Content-Type", "application/json")
	if after == nil {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	if sortCol == "" && len(tasks) == limit {
		next.Filters = listFilterFingerprint(q)
		w.Header().Set("X-Next-Cursor", encodeListCursor(next))
	}
	w.Header().Set("ETag", etag)

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(buf.Bytes())
}
