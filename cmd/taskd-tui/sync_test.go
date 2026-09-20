package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestCtrlLSync(t *testing.T) {
	u := newUI("http://localhost:8080", "", false, "")
	capture := u.app.GetInputCapture()
	if capture == nil {
		t.Fatal("expected application input capture to be installed")
	}

	keyEv := tcell.NewEventKey(tcell.KeyCtrlL, 0, 0)
	if ret := capture(keyEv); ret != nil {
		t.Fatalf("capture(Ctrl+L) = %v, want nil", ret)
	}

	otherEv := tcell.NewEventKey(tcell.KeyRune, 'a', 0)
	if ret := capture(otherEv); ret != otherEv {
		t.Fatalf("capture('a') = %v, want %v", ret, otherEv)
	}
}
