package main

import (
	"strings"
	"testing"
)

func TestWebUILiveStatCard(t *testing.T) {
	ui := string(uiHTML)

	card := `<button type="button" class="card-btn" data-status-filter="live">`
	if !strings.Contains(ui, card) {
		t.Fatalf("expected live card button %q in web/index.html", card)
	}

	liveIdx := strings.Index(ui, card)
	gridStatsIdx := strings.Index(ui, `class="grid-stats"`)
	if liveIdx == -1 || gridStatsIdx == -1 || liveIdx < gridStatsIdx {
		t.Fatal("expected live card inside .grid-stats")
	}

	if !strings.Contains(ui, `<h3 class="stat-label">Live</h3>`) {
		t.Fatal("expected Live stat label heading in web/index.html")
	}
	if !strings.Contains(ui, `<div class="stat-val" id="stat-live">-</div>`) {
		t.Fatal("expected #stat-live initial placeholder in web/index.html")
	}

	setLive := "setVal('stat-live', (stats.pending || 0) + (stats.leased || 0));"
	if !strings.Contains(ui, setLive) {
		t.Fatalf("expected %q in loadStats in web/index.html", setLive)
	}

	clickFilter := "filterByStatus(card.dataset.statusFilter);"
	if !strings.Contains(ui, clickFilter) {
		t.Fatalf("expected %q in card click handler in web/index.html", clickFilter)
	}
}
