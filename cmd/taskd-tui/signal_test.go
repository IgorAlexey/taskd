package main

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestCtrlCQuit(t *testing.T) {
	u, _, _ := stub(t)
	ts, err := u.fetch()
	if err != nil {
		t.Fatal(err)
	}
	u.render(ts)

	sim := tcell.NewSimulationScreen("")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	sim.SetSize(80, 25)
	u.app.SetScreen(sim)

	stop := u.quitOnSignal()
	defer stop()

	done := make(chan error, 1)
	go func() { done <- u.app.Run() }()
	eventually(t, func() bool {
		ch := make(chan bool, 1)
		u.app.QueueUpdate(func() { ch <- u.table.HasFocus() })
		return <-ch
	})

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("app.Run() = %v", err)
		}
	case <-time.After(5 * time.Second):
		u.app.Stop()
		t.Fatal("app still running after SIGINT; terminal would stay in raw mode")
	}
}
