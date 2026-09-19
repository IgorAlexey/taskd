package main

import (
	"testing"

	"github.com/rivo/tview"
)

func TestModalFitsTerminal(t *testing.T) {
	for _, tc := range []struct {
		sw, sh int
		wantW  int
		wantH  int
	}{
		{40, 10, 38, 7},
		{52, 14, 50, 10},
		{60, 15, 58, 11},
		{80, 25, 60, 15},
		{100, 30, 60, 15},
		{2, 2, 1, 1},
	} {
		if w := modalWidth(60, tc.sw); w != tc.wantW {
			t.Errorf("modalWidth(60, %d) = %d, want %d", tc.sw, w, tc.wantW)
		}
		if h := modalHeight(15, tc.sh); h != tc.wantH {
			t.Errorf("modalHeight(15, %d) = %d, want %d", tc.sh, h, tc.wantH)
		}
	}

	for _, tc := range []struct {
		pw, ph int
		wantX  int
		wantY  int
		wantW  int
		wantH  int
	}{
		{40, 10, 1, 1, 38, 7},
		{52, 14, 1, 2, 50, 10},
		{80, 25, 10, 5, 60, 15},
	} {
		f := tview.NewBox()
		m := centerModal(f, 60, 15)
		m.SetRect(0, 0, tc.pw, tc.ph)
		x, y, w, h := f.GetRect()
		if x != tc.wantX || y != tc.wantY || w != tc.wantW || h != tc.wantH {
			t.Errorf("centerModal child rect in %dx%d = (%d, %d, %d, %d), want (%d, %d, %d, %d)",
				tc.pw, tc.ph, x, y, w, h, tc.wantX, tc.wantY, tc.wantW, tc.wantH)
		}
	}
}
