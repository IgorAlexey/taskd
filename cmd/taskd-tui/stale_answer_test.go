package main

import "testing"

// A refresh that started before the operator cycled must not paint its
// answer over the project they switched to.
func TestRefreshDropsAnswerForLeftProject(t *testing.T) {
	u, _ := projectStub(t, false)

	// proj-a is active; this answer is for the project they left.
	u.refresh("proj-b")

	var cached []string
	var statsProject string
	queried := make(chan struct{})
	u.app.QueueUpdate(func() {
		for _, tk := range u.all {
			cached = append(cached, tk.Project)
		}
		statsProject = u.statsProject
		close(queried)
	})
	<-queried
	for _, project := range cached {
		if project == "proj-b" {
			t.Fatalf("proj-b rows landed while proj-a is active: %v", cached)
		}
	}
	if statsProject == "proj-b" {
		t.Fatal("counts stamped for proj-b while proj-a is active")
	}
}
