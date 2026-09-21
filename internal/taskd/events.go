package taskd

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

var ssePingInterval = 15 * time.Second

// broadcaster is a one-bit doorbell: publish wakes every subscriber, and a
// subscriber that has not drained its last wake gets no second one.
type broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newBroadcaster() *broadcaster {
	return &broadcaster{
		subs: make(map[chan struct{}]struct{}),
	}
}

func (b *broadcaster) publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (b *broadcaster) subscribe() (<-chan struct{}, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan struct{}, 1)
	b.subs[ch] = struct{}{}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
		})
	}
	return ch, cancel
}

func (b *broadcaster) subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func (b *broadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := flusherOf(w)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, cancel := b.subscribe()
	defer cancel()

	if _, err := fmt.Fprint(w, "retry: 1000\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(ssePingInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if _, err := fmt.Fprint(w, "event: change\ndata: reload\n\n"); err != nil {
				return
			}
			flusher.Flush()
			ticker.Reset(ssePingInterval)
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// flusherOf walks ResponseWriter wrappers (like loggingResponseWriter) via
// Unwrap until it finds one that can flush.
func flusherOf(w http.ResponseWriter) (http.Flusher, bool) {
	for {
		if f, ok := w.(http.Flusher); ok {
			return f, true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil, false
		}
		w = u.Unwrap()
	}
}
