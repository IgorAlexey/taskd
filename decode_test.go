package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name       string
		body       string
		wantSubstr string
	}{
		{
			name:       "unknown field",
			body:       `{"name":"test","extra":1}`,
			wantSubstr: "unknown field",
		},
		{
			name:       "syntax error",
			body:       `{"name":`,
			wantSubstr: "unexpected EOF",
		},
		{
			name:       "type mismatch",
			body:       `{"name":123}`,
			wantSubstr: "cannot unmarshal number into Go struct field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(tc.body))
			var p payload
			if decodeJSON(rec, req, &p) {
				t.Fatalf("expected decodeJSON to fail")
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantSubstr) {
				t.Fatalf("expected body to contain %q, got %q", tc.wantSubstr, rec.Body.String())
			}
		})
	}
}
