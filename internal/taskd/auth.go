package taskd

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"path"
	"strings"
)

// requireToken lets a request through when it carries the token as a bearer
// header or, for the browser, as the taskd_token cookie. The page and its
// assets stay open so it can ask for the token, and /health stays open for
// load balancers. Preflight requests carry no credentials by design. The
// path is cleaned first so /ui/../tasks is judged as /tasks.
func requireToken(next http.Handler, token string) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean(r.URL.Path)
		open := r.Method == http.MethodOptions ||
			r.Method == http.MethodGet && (p == "/" || p == "/health" || p == "/ui" || strings.HasPrefix(p, "/ui/"))
		if open || tokenMatches(r, want) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized")
	})
}

func tokenMatches(r *http.Request, want [sha256.Size]byte) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		c, err := r.Cookie("taskd_token")
		if err != nil {
			return false
		}
		got = c.Value
	}
	// Digests are the same length, so the comparison leaks neither the
	// token nor its length.
	gotSum := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(gotSum[:], want[:]) == 1
}
