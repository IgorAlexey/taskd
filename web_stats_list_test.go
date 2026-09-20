package main

import (
	"strings"
	"testing"
)

func TestWebUIStatsCardsList(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, `<ul class="grid-stats">`) {
		t.Error("expected <ul class=\"grid-stats\"> in web/index.html")
	}
	if !strings.Contains(ui, `<li class="card">`) {
		t.Error("expected <li class=\"card\"> in web/index.html")
	}

	labels := []string{"Live", "Pending", "Leased", "Done", "Buried", "Total"}
	for _, label := range labels {
		heading := `<h3 class="stat-label">` + label + `</h3>`
		if !strings.Contains(ui, heading) {
			t.Errorf("expected heading %q in web/index.html", heading)
		}
	}

	for _, check := range []string{
		`list-style: none`,
		`class="card-btn"`,
		`filterByStatus(card.dataset.statusFilter)`,
	} {
		if !strings.Contains(ui, check) {
			t.Errorf("expected %q in web/index.html", check)
		}
	}
}
