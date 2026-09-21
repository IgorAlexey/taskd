package taskd

import (
	"net/http"
	"strings"

	"github.com/IgorAlexey/taskd/web"
)

// newHandler serves the HTTP API and the web page over db.
func newHandler(db *store, lease int) http.Handler {
	return newHandlerWithCORS(db, lease, "")
}

// server holds what every handler needs: the store and the lease length.
type server struct {
	db     *store
	lease  int
	cors   string
	dbName string
}

func newHandlerWithCORS(db *store, lease int, corsOrigin string) http.Handler {
	s := &server{db: db, lease: lease, cors: corsOrigin, dbName: mainDBName(db.ro)}
	return s.handler()
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()

	handleMethods(mux, "/{$}", map[string]route{
		http.MethodGet: {handler: func(w http.ResponseWriter, r *http.Request) {
			redirectWithQuery(w, r, "/ui", http.StatusFound)
		}, anyParams: true},
	})
	handleMethods(mux, "/ui", map[string]route{
		http.MethodGet: {handler: uiHandler, anyParams: true},
	})
	mux.HandleFunc("GET /ui/{$}", func(w http.ResponseWriter, r *http.Request) {
		redirectWithQuery(w, r, "/ui", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /ui/{file}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, web.FS, r.PathValue("file"))
	})

	handleMethods(mux, "/health", map[string]route{
		http.MethodGet: {handler: s.healthHandler, anyParams: true},
	})

	handleMethods(mux, "/stats", map[string]route{
		http.MethodGet: {handler: s.statsHandler, params: []string{"project", "worker"}},
	})
	handleMethods(mux, "/tasks", map[string]route{
		http.MethodGet: {handler: s.listTasksHandler, params: []string{
			"status", "project", "worker", "priority", "limit", "offset",
			"q", "fields", "columns", "after", "order", "sort",
		}},
		http.MethodPost: {handler: s.createTaskHandler},
	})
	handleMethods(mux, "/tasks/purge", map[string]route{
		http.MethodPost: {handler: s.purgeTasksHandler, params: []string{"project"}},
	})
	handleMethods(mux, "/tasks/kick", map[string]route{
		http.MethodPost: {handler: s.bulkKickTasksHandler, params: []string{"project", "limit"}},
	})
	handleMethods(mux, "/tasks/claim", map[string]route{
		http.MethodPost: {handler: s.claimHandler},
	})
	handleMethods(mux, "/tasks/{id}/claim", map[string]route{
		http.MethodPost: {handler: s.claimIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/done", map[string]route{
		http.MethodPost: {handler: s.doneIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/close", map[string]route{
		http.MethodPost: {handler: s.closeIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/touch", map[string]route{
		http.MethodPost: {handler: s.touchIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/release", map[string]route{
		http.MethodPost: {handler: s.releaseIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/bury", map[string]route{
		http.MethodPost: {handler: s.buryIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/kick", map[string]route{
		http.MethodPost: {handler: s.kickIDHandler},
	})
	handleMethods(mux, "/tasks/{id}/notes", map[string]route{
		http.MethodGet:  {handler: s.getNotesHandler},
		http.MethodPost: {handler: s.createNoteHandler},
	})
	handleMethods(mux, "/tasks/{id}", map[string]route{
		http.MethodGet:    {handler: s.getTaskHandler, params: []string{"fields", "columns"}},
		http.MethodPatch:  {handler: s.patchTaskHandler},
		http.MethodDelete: {handler: s.deleteTaskHandler, params: []string{"force"}},
	})
	handleMethods(mux, "/projects", map[string]route{
		http.MethodGet: {handler: s.projectsHandler},
	})
	handleMethods(mux, "/projects/{project}", map[string]route{
		http.MethodPatch:  {handler: s.renameProjectHandler},
		http.MethodDelete: {handler: s.deleteProjectHandler},
	})
	handleMethods(mux, "/workers", map[string]route{
		http.MethodGet: {handler: s.workersHandler, params: []string{"project", "status"}},
	})
	mux.Handle("GET /api/events", s.db.events)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cors != "" && s.cors != "*" {
			w.Header().Add("Vary", "Origin")
		}
		origin := r.Header.Get("Origin")
		originMatched := s.cors != "" && (s.cors == "*" || origin == s.cors)
		if originMatched {
			w.Header().Set("Access-Control-Allow-Origin", s.cors)
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Access-Control-Expose-Headers", "X-Total-Count, X-Next-Cursor, ETag, Location")
		}
		if strings.HasSuffix(r.URL.Path, "/") && r.URL.Path != "/" {
			cleanReq := *r
			cleanURL := *r.URL
			cleanURL.Path = strings.TrimSuffix(cleanURL.Path, "/")
			if cleanURL.RawPath != "" {
				cleanURL.RawPath = strings.TrimSuffix(cleanURL.RawPath, "/")
			}
			cleanReq.URL = &cleanURL
			if _, matched := mux.Handler(&cleanReq); matched != "" && matched != "/" {
				target := cleanURL.EscapedPath()
				if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
					redirectWithQuery(w, r, target, http.StatusPermanentRedirect)
					return
				}
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		mux.ServeHTTP(w, r)
	})
}

func uiHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, web.FS, "index.html")
}
