package taskd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const maxBodyBytes = 1 << 20

type apiError struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeAPIError(w, code, apiError{Error: msg})
}

func writeFieldError(w http.ResponseWriter, code int, msg, field string) {
	writeAPIError(w, code, apiError{Error: msg, Field: field})
}

func writeAPIError(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(payload)
}

type route struct {
	handler   http.HandlerFunc
	params    []string
	anyParams bool
}

func handleMethods(mux *http.ServeMux, pattern string, methods map[string]route) {
	var allowed []string
	for m := range methods {
		allowed = append(allowed, m)
	}
	if slices.Contains(allowed, http.MethodGet) && !slices.Contains(allowed, http.MethodHead) {
		allowed = append(allowed, http.MethodHead)
	}
	slices.Sort(allowed)
	allowHeader := strings.Join(allowed, ", ")

	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		rt, ok := methods[r.Method]
		if !ok && r.Method == http.MethodHead {
			rt, ok = methods[http.MethodGet]
		}
		if !ok {
			w.Header().Set("Allow", allowHeader)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.URL.RawQuery != "" && !rt.anyParams {
			q, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid query string: %v", err))
				return
			}
			keys := make([]string, 0, len(q))
			for k := range q {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				if !slices.Contains(rt.params, k) {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown query parameter: %s", k))
					return
				}
			}
			r = r.WithContext(context.WithValue(r.Context(), queryContextKey{}, q))
		}
		rt.handler(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeBody(w, r, dst, false)
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	var maxErr *http.MaxBytesError
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if optional && errors.Is(err, io.EOF) {
			return true
		}
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body: unexpected trailing data")
		return false
	}
	return true
}

type queryContextKey struct{}

func requestQuery(r *http.Request) url.Values {
	if q, ok := r.Context().Value(queryContextKey{}).(url.Values); ok {
		return q
	}
	if r.URL.RawQuery == "" {
		return url.Values{}
	}
	return r.URL.Query()
}

func internalError(w http.ResponseWriter, err error) {
	log.Print(err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func redirectWithQuery(w http.ResponseWriter, r *http.Request, path string, code int) {
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, path, code)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (lrw *loggingResponseWriter) WriteHeader(status int) {
	if lrw.status == 0 {
		lrw.status = status
		lrw.ResponseWriter.WriteHeader(status)
	}
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += int64(n)
	return n, err
}

func (lrw *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return lrw.ResponseWriter
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingResponseWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				panic(rec)
			}
			status := lw.status
			if status == 0 {
				status = http.StatusOK
			}
			path := r.URL.RequestURI()
			if path == "" {
				path = r.URL.Path
			}
			log.Printf("%s %s %s %d %d %s", r.RemoteAddr, r.Method, path, status, lw.bytes, time.Since(start))
		}()
		next.ServeHTTP(lw, r)
	})
}
