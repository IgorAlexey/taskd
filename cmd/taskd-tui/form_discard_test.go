package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIFormDiscardConfirmation(t *testing.T) {
	openCreate := func(m model) model {
		next, _ := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
		return next.(model)
	}
	openEdit := func(m model) model {
		next, _ := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
		return next.(model)
	}

	t.Run("CleanForms", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			open func(m model) model
		}{
			{"Create", openCreate},
			{"Edit", openEdit},
		} {
			t.Run(tc.name, func(t *testing.T) {
				base := newTestModel()
				updated, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				m := updated.(model)

				mForm := tc.open(m)
				if mForm.mode != modeForm {
					t.Fatalf("expected modeForm, got %v", mForm.mode)
				}
				escModel, escCmd := mForm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				mEsc := escModel.(model)
				if mEsc.mode != modeTable {
					t.Fatalf("expected clean Escape to return modeTable, got %v", mEsc.mode)
				}
				if escCmd != nil && isQuitCmd(escCmd) {
					t.Fatalf("clean Escape should not quit")
				}

				mFormAgain := tc.open(m)
				ctrlcModel, ctrlcCmd := mFormAgain.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				mCtrlC := ctrlcModel.(model)
				if mCtrlC.mode != modeTable {
					t.Fatalf("expected clean Ctrl+C to return modeTable, got %v", mCtrlC.mode)
				}
				if ctrlcCmd != nil && isQuitCmd(ctrlcCmd) {
					t.Fatalf("clean Ctrl+C should not quit the application")
				}
			})
		}
	})

	t.Run("DirtyForms", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			open   func(m model) model
			modify func(m *model)
			check  func(m model) bool
		}{
			{
				name: "CreateBody",
				open: openCreate,
				modify: func(m *model) {
					m.form.body.SetValue("dirty body")
				},
				check: func(m model) bool {
					return m.form.body.Value() == "dirty body"
				},
			},
			{
				name: "CreateProject",
				open: openCreate,
				modify: func(m *model) {
					m.form.project.SetValue("different-project")
				},
				check: func(m model) bool {
					return m.form.project.Value() == "different-project"
				},
			},
			{
				name: "CreatePriority",
				open: openCreate,
				modify: func(m *model) {
					m.form.priority.SetValue("9")
				},
				check: func(m model) bool {
					return m.form.priority.Value() == "9"
				},
			},
			{
				name: "CreateAssetPath",
				open: openCreate,
				modify: func(m *model) {
					m.form.asset.SetValue("new/asset/path")
				},
				check: func(m model) bool {
					return m.form.asset.Value() == "new/asset/path"
				},
			},
			{
				name: "EditBody",
				open: openEdit,
				modify: func(m *model) {
					m.form.body.SetValue("modified body")
				},
				check: func(m model) bool {
					return m.form.body.Value() == "modified body"
				},
			},
			{
				name: "EditProject",
				open: openEdit,
				modify: func(m *model) {
					m.form.project.SetValue("edited-project")
				},
				check: func(m model) bool {
					return m.form.project.Value() == "edited-project"
				},
			},
			{
				name: "EditPriority",
				open: openEdit,
				modify: func(m *model) {
					m.form.priority.SetValue("7")
				},
				check: func(m model) bool {
					return m.form.priority.Value() == "7"
				},
			},
			{
				name: "EditAssetPath",
				open: openEdit,
				modify: func(m *model) {
					m.form.asset.SetValue("edited/asset")
				},
				check: func(m model) bool {
					return m.form.asset.Value() == "edited/asset"
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				base := newTestModel()
				updated, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				m := updated.(model)

				mForm := tc.open(m)
				tc.modify(&mForm)

				escNext, _ := mForm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				mConfirmEsc := escNext.(model)
				if !mConfirmEsc.form.discarding {
					t.Fatalf("expected form to be in discarding state on Escape from dirty form")
				}
				viewEsc := ansi.Strip(mConfirmEsc.View().Content)
				if !strings.Contains(viewEsc, "Discard unsaved changes?") {
					t.Fatalf("expected discard prompt in view, got:\n%s", viewEsc)
				}

				cancelN, _ := mConfirmEsc.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
				mPreservedN := cancelN.(model)
				if mPreservedN.form.discarding {
					t.Fatalf("expected cancelling with n to exit discarding state")
				}
				if !tc.check(mPreservedN) {
					t.Fatalf("expected form inputs to be preserved after cancelling discard with n")
				}

				escAgain, _ := mPreservedN.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				mConfirmAgain := escAgain.(model)
				if !mConfirmAgain.form.discarding {
					t.Fatalf("expected form to be in discarding state on second Escape")
				}
				discardY, _ := mConfirmAgain.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
				mDiscarded := discardY.(model)
				if mDiscarded.mode != modeTable {
					t.Fatalf("expected confirming discard with y to return modeTable, got %v", mDiscarded.mode)
				}

				mForm2 := tc.open(m)
				tc.modify(&mForm2)

				ctrlcNext, _ := mForm2.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				mConfirmCtrlC := ctrlcNext.(model)
				if !mConfirmCtrlC.form.discarding {
					t.Fatalf("expected form to be in discarding state on Ctrl+C from dirty form")
				}
				viewCtrlC := ansi.Strip(mConfirmCtrlC.View().Content)
				if !strings.Contains(viewCtrlC, "Discard unsaved changes?") {
					t.Fatalf("expected discard prompt in view, got:\n%s", viewCtrlC)
				}

				cancelEsc, _ := mConfirmCtrlC.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
				mPreservedEsc := cancelEsc.(model)
				if mPreservedEsc.form.discarding {
					t.Fatalf("expected cancelling with Escape to exit discarding state")
				}
				if !tc.check(mPreservedEsc) {
					t.Fatalf("expected form inputs to be preserved after cancelling discard with Escape")
				}

				ctrlcAgain, _ := mPreservedEsc.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				mConfirmCtrlC2 := ctrlcAgain.(model)
				if !mConfirmCtrlC2.form.discarding {
					t.Fatalf("expected form to be in discarding state on subsequent Ctrl+C")
				}

				cancelEnter, _ := mConfirmCtrlC2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				mPreservedEnter := cancelEnter.(model)
				if mPreservedEnter.form.discarding {
					t.Fatalf("expected cancelling with Enter to exit discarding state")
				}
				if !tc.check(mPreservedEnter) {
					t.Fatalf("expected form inputs to be preserved after cancelling discard with Enter")
				}

				ctrlcAgain3, _ := mPreservedEnter.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				mConfirmCtrlC3 := ctrlcAgain3.(model)
				if !mConfirmCtrlC3.form.discarding {
					t.Fatalf("expected form to be in discarding state")
				}
				_, quitCmd := mConfirmCtrlC3.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				if quitCmd == nil || !isQuitCmd(quitCmd) {
					t.Fatalf("expected Ctrl+C inside discard prompt to quit application")
				}

				mForm3 := tc.open(m)
				tc.modify(&mForm3)
				cPrompt, _ := mForm3.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				mCPrompt := cPrompt.(model)
				discardY2, _ := mCPrompt.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
				mDiscarded2 := discardY2.(model)
				if mDiscarded2.mode != modeTable {
					t.Fatalf("expected confirming discard with y after Ctrl+C prompt to return modeTable, got %v", mDiscarded2.mode)
				}
			})
		}
	})

	t.Run("EditReverted", func(t *testing.T) {
		base := newTestModel()
		updated, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m := updated.(model)

		mForm := openEdit(m)
		origBody := mForm.form.body.Value()
		mForm.form.body.SetValue("temporary edit")
		mForm.form.body.SetValue(origBody)

		escNext, _ := mForm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		mEsc := escNext.(model)
		if mEsc.mode != modeTable {
			t.Fatalf("expected reverted edit form to exit to modeTable on Escape without confirmation, got %v", mEsc.mode)
		}
	})

	t.Run("MainViewCtrlC", func(t *testing.T) {
		base := newTestModel()
		updated, _ := base.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m := updated.(model)

		if m.mode != modeTable {
			t.Fatalf("expected initial modeTable, got %v", m.mode)
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatalf("expected non-nil cmd on Ctrl+C in modeTable")
		}
		if !isQuitCmd(cmd) {
			t.Fatalf("expected tea.Quit on Ctrl+C in modeTable")
		}
	})
}

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	_, ok := msg.(tea.QuitMsg)
	return ok
}
