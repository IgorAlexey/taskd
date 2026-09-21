package taskd

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventsRetryHeaderAndFirstEvent(t *testing.T) {
	b := newBroadcaster()
	ts := httptest.NewServer(b)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %q", cc)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Fatalf("expected Connection keep-alive, got %q", conn)
	}
	if buf := resp.Header.Get("X-Accel-Buffering"); buf != "no" {
		t.Fatalf("expected X-Accel-Buffering no, got %q", buf)
	}

	reader := bufio.NewReader(resp.Body)

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "retry: 1000" {
		t.Fatalf("expected retry: 1000, got %q", line)
	}

	// Empty line separating the retry directive
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "" {
		t.Fatalf("expected empty line after retry, got %q", line)
	}

	// Publish an event
	b.publish()

	// Read event line
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "event: change" {
		t.Fatalf("expected event: change, got %q", line)
	}

	// Read data line
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "data: reload" {
		t.Fatalf("expected data: reload, got %q", line)
	}

	// Read trailing empty line
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "" {
		t.Fatalf("expected empty line after event block, got %q", line)
	}
}

func TestEventsPingWithoutEvents(t *testing.T) {
	orig := ssePingInterval
	ssePingInterval = 20 * time.Millisecond
	defer func() { ssePingInterval = orig }()

	b := newBroadcaster()
	ts := httptest.NewServer(b)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	foundPing := false
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == ": ping" {
			foundPing = true
			break
		}
	}
	if !foundPing {
		t.Fatal("did not receive : ping within deadline")
	}
}

func TestEventsCoalesce(t *testing.T) {
	b := newBroadcaster()
	ts := httptest.NewServer(b)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Wait until client has connected and registered as a subscriber
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if b.subscribers() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if b.subscribers() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", b.subscribers())
	}

	reader := bufio.NewReader(resp.Body)

	// Read initial retry header
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "retry: 1000" {
		t.Fatalf("expected retry: 1000, got %q", line)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "" {
		t.Fatalf("expected empty line after retry, got %q", line)
	}

	// Publish 100 times quickly before reading further
	for i := 0; i < 100; i++ {
		b.publish()
	}

	lines := make(chan string, 200)
	errCh := make(chan error, 1)
	go func() {
		for {
			l, readErr := reader.ReadString('\n')
			if readErr != nil {
				errCh <- readErr
				return
			}
			lines <- strings.TrimSpace(l)
		}
	}()

	// The client receives at least one event and, after draining for 100ms, no more than a handful
	eventCount := 0
	firstTimer := time.NewTimer(500 * time.Millisecond)
	defer firstTimer.Stop()

	for eventCount == 0 {
		select {
		case l := <-lines:
			if l == "event: change" {
				eventCount++
			}
		case readErr := <-errCh:
			t.Fatalf("unexpected read error: %v", readErr)
		case <-firstTimer.C:
			t.Fatal("timed out waiting for first event")
		}
	}

	drainTimer := time.NewTimer(100 * time.Millisecond)
	defer drainTimer.Stop()
drainLoop:
	for {
		select {
		case l := <-lines:
			if l == "event: change" {
				eventCount++
			}
		case readErr := <-errCh:
			t.Fatalf("unexpected read error during drain: %v", readErr)
		case <-drainTimer.C:
			break drainLoop
		}
	}

	if eventCount < 1 {
		t.Fatalf("expected at least 1 event, got %d", eventCount)
	}
	if eventCount > 3 {
		t.Fatalf("expected no more than a handful of events (<= 3), got %d", eventCount)
	}

	// Publish once more and assert exactly one more event arrives
	b.publish()

	moreEvents := 0
	moreTimer := time.NewTimer(500 * time.Millisecond)
	defer moreTimer.Stop()
	for moreEvents == 0 {
		select {
		case l := <-lines:
			if l == "event: change" {
				moreEvents++
			}
		case readErr := <-errCh:
			t.Fatalf("unexpected read error waiting for next event: %v", readErr)
		case <-moreTimer.C:
			t.Fatal("timed out waiting for next event")
		}
	}

	drainTimer2 := time.NewTimer(100 * time.Millisecond)
	defer drainTimer2.Stop()
drainLoop2:
	for {
		select {
		case l := <-lines:
			if l == "event: change" {
				moreEvents++
			}
		case readErr := <-errCh:
			t.Fatalf("unexpected read error during second drain: %v", readErr)
		case <-drainTimer2.C:
			break drainLoop2
		}
	}

	if moreEvents != 1 {
		t.Fatalf("expected exactly 1 more event, got %d", moreEvents)
	}
}

func TestEventsClientDisconnectCancelsSubscription(t *testing.T) {
	b := newBroadcaster()
	ts := httptest.NewServer(b)
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	// Verify subscriber registered
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if b.subscribers() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if b.subscribers() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", b.subscribers())
	}

	// Close client
	resp.Body.Close()
	cancel()

	// Assert the broadcaster's subscriber count drops to 0 within a second
	dropDeadline := time.Now().Add(1 * time.Second)
	dropped := false
	for time.Now().Before(dropDeadline) {
		if b.subscribers() == 0 {
			dropped = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !dropped {
		t.Fatalf("expected 0 subscribers within 1 second, got %d", b.subscribers())
	}
}

func TestEventsMethodNotAllowed(t *testing.T) {
	b := newBroadcaster()
	ts := httptest.NewServer(b)
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.StatusCode)
	}
}

type nonFlusherWriter struct {
	http.ResponseWriter
}

func TestEventsStreamingUnsupported(t *testing.T) {
	b := newBroadcaster()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	b.ServeHTTP(nonFlusherWriter{rec}, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "streaming unsupported") {
		t.Fatalf("expected 'streaming unsupported' in body, got %q", rec.Body.String())
	}
}

func TestEventsFireOnMutation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(withRequestLogging(newHandler(db, 300)))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	reader := bufio.NewReader(resp.Body)
	if line, _ := reader.ReadString('\n'); line != "retry: 1000\n" {
		t.Fatalf("first line = %q", line)
	}

	bad, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing body, got %d", bad.StatusCode)
	}
	ok, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"p","body":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	ok.Body.Close()
	if ok.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", ok.StatusCode)
	}

	var seen []string
	for len(seen) < 3 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended early: %v (got %q)", err, seen)
		}
		seen = append(seen, line)
	}
	if seen[0] != "\n" || seen[1] != "event: change\n" || seen[2] != "data: reload\n" {
		t.Fatalf("a rejected POST must not ring and a 201 must ring once; got %q", seen)
	}
}

func TestEventsRingOnlyOnMutation(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "t.db"), 0)
	if err != nil {
		t.Fatalf("openDB failed: %v", err)
	}
	defer db.Close()
	srv := httptest.NewServer(newHandler(db, 300))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed reading retry line: %v", err)
	}
	if !strings.HasPrefix(line, "retry:") {
		t.Fatalf("expected retry header, got %q", line)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed reading empty line after retry: %v", err)
	}
	if strings.TrimSpace(line) != "" {
		t.Fatalf("expected empty line after retry, got %q", line)
	}

	lines := make(chan string, 20)
	go func() {
		for {
			l, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			lines <- l
		}
	}()

	claimResp, err := srv.Client().Post(srv.URL+"/tasks/claim", "application/json", strings.NewReader(`{"worker":"w","wait":0.1}`))
	if err != nil {
		t.Fatal(err)
	}
	claimResp.Body.Close()
	if claimResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", claimResp.StatusCode)
	}

	select {
	case l := <-lines:
		t.Fatalf("expected no event within 300ms, got %q", l)
	case <-time.After(300 * time.Millisecond):
	}

	postResp, err := srv.Client().Post(srv.URL+"/tasks", "application/json", strings.NewReader(`{"project":"p","body":"task 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", postResp.StatusCode)
	}

	select {
	case l := <-lines:
		if strings.TrimSpace(l) != "event: change" {
			t.Fatalf("expected event: change line, got %q", l)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event: change")
	}

	select {
	case l := <-lines:
		if strings.TrimSpace(l) != "data: reload" {
			t.Fatalf("expected data: reload, got %q", l)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for data: reload")
	}
	select {
	case l := <-lines:
		if strings.TrimSpace(l) != "" {
			t.Fatalf("expected empty line, got %q", line)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for empty line")
	}

	select {
	case l := <-lines:
		t.Fatalf("expected exactly one event: change line, got extra: %q", l)
	case <-time.After(100 * time.Millisecond):
	}

	kickResp, err := srv.Client().Post(srv.URL+"/tasks/kick", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	kickResp.Body.Close()
	if kickResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", kickResp.StatusCode)
	}

	select {
	case l := <-lines:
		t.Fatalf("expected no event within 300ms after bulk kick with no buried tasks, got %q", l)
	case <-time.After(300 * time.Millisecond):
	}
}
