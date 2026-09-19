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

func TestRejectTrailingJSONData(t *testing.T) {
	type payload struct {
		Project string `json:"project"`
		Body    string `json:"body"`
	}

	invalidCases := []struct {
		name string
		body string
	}{
		{
			name: "concatenated json objects",
			body: `{"project":"p","body":"a"}{"project":"p","body":"b"}`,
		},
		{
			name: "trailing garbage characters",
			body: `{"project":"p","body":"a"}trailing`,
		},
		{
			name: "trailing comma",
			body: `{"project":"p","body":"a"},`,
		},
		{
			name: "trailing number",
			body: `{"project":"p","body":"a"}123`,
		},
		{
			name: "trailing array",
			body: `{"project":"p","body":"a"}[1, 2]`,
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(tc.body))
			var p payload
			if decodeJSON(rec, req, &p) {
				t.Fatalf("expected decodeJSON to fail on %s", tc.name)
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rec.Code)
			}
		})
	}

	t.Run("valid payload with trailing whitespace passes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader("{\"project\":\"p\",\"body\":\"a\"}   \r\n\t  "))
		var p payload
		if !decodeJSON(rec, req, &p) {
			t.Fatalf("expected decodeJSON to succeed on valid payload with whitespace")
		}
		if p.Project != "p" || p.Body != "a" {
			t.Fatalf("unexpected parsed values: %+v", p)
		}
	})
}
