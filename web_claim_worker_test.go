package main

import (
	"strings"
	"testing"
)

func TestWebUIPersistClaimWorker(t *testing.T) {
	ui := string(uiHTML)

	if !strings.Contains(ui, "localStorage.getItem('taskd-claim-worker')") {
		t.Fatal("expected localStorage.getItem('taskd-claim-worker') in web/index.html")
	}
	if !strings.Contains(ui, "localStorage.setItem('taskd-claim-worker', worker)") {
		t.Fatal("expected localStorage.setItem('taskd-claim-worker', worker) in web/index.html")
	}
	if !strings.Contains(ui, "prompt('Claim this task as which worker?', claimWorker)") {
		t.Fatal("expected prompt with claimWorker default in web/index.html")
	}
	if !strings.Contains(ui, "currentNoteAuthor || claimWorker || ''") {
		t.Fatal("expected authorInput to use claimWorker fallback in web/index.html")
	}

	claimIdx := strings.Index(ui, "fetchJSON('/tasks/' + encodeURIComponent(id) + '/claim'")
	saveIdx := strings.Index(ui, "localStorage.setItem('taskd-claim-worker', worker)")
	if claimIdx == -1 || saveIdx == -1 || saveIdx < claimIdx {
		t.Fatal("expected localStorage save to occur after claim fetch succeeds")
	}
}
