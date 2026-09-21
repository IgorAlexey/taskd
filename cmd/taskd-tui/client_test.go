package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendsToken(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte("{}"))
	}))
	defer srv.Close()
	if _, err := newClient(srv.URL, "").getStats("", ""); err == nil || err.Error() != "unauthorized: the daemon requires TASKD_TOKEN" {
		t.Fatalf("without token: %v", err)
	}
	if _, err := newClient(srv.URL, "wrong").getStats("", ""); err == nil || err.Error() != "unauthorized: the daemon rejected TASKD_TOKEN" {
		t.Fatalf("wrong token: %v", err)
	}
	if _, err := newClient(srv.URL, "s3cret").getStats("", ""); err != nil {
		t.Fatalf("with token: %v", err)
	}
	if len(got) != 3 || got[0] != "" || got[1] != "Bearer wrong" || got[2] != "Bearer s3cret" {
		t.Fatalf("headers seen: %q", got)
	}
}
