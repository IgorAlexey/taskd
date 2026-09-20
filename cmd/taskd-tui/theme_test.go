package main

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTUIBackgroundColorMsg(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	m.width = 100
	m.height = 24
	m.tasks = []task{
		{
			ID:       1,
			Priority: 1,
			Status:   "pending",
			Project:  "taskd",
			Body:     "test theme change",
		},
	}
	m.notesCache[1] = []taskNote{
		{
			ID:     1,
			Author: "tester",
			Text:   "important cached note",
		},
	}
	m.rebuildShown()
	m.syncDetail()

	darkTheme := newTheme(true)
	lightTheme := newTheme(false)

	if !reflect.DeepEqual(m.theme, darkTheme) {
		t.Fatalf("expected initial dark theme")
	}

	darkView := m.View().Content
	if !strings.Contains(darkView, "229;165;75") {
		t.Fatalf("expected dark theme view to contain dark accent color (229;165;75)")
	}
	if strings.Contains(darkView, "180;83;9") {
		t.Fatalf("expected dark theme view to not contain light accent color (180;83;9)")
	}

	whiteMsg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	if whiteMsg.IsDark() {
		t.Fatalf("expected whiteMsg to not be dark")
	}

	res, _ := m.Update(whiteMsg)
	m = res.(model)

	if !reflect.DeepEqual(m.theme, lightTheme) {
		t.Fatalf("expected theme to update to light theme after white BackgroundColorMsg; got %+v want %+v", m.theme, lightTheme)
	}

	lightView := m.View().Content
	if !strings.Contains(lightView, "180;83;9") {
		t.Fatalf("expected light theme view to contain light accent color (180;83;9)")
	}
	if strings.Contains(lightView, "229;165;75") {
		t.Fatalf("expected light theme view to not contain dark accent color (229;165;75)")
	}

	if !strings.Contains(m.detail.GetContent(), "important cached note") {
		t.Fatalf("syncDetail should preserve cached notes in detail pane; got:\n%s", m.detail.GetContent())
	}

	darkMsg := tea.BackgroundColorMsg{Color: color.RGBA{R: 0, G: 0, B: 0, A: 255}}
	if !darkMsg.IsDark() {
		t.Fatalf("expected darkMsg to be dark")
	}

	res, _ = m.Update(darkMsg)
	m = res.(model)

	if !reflect.DeepEqual(m.theme, darkTheme) {
		t.Fatalf("expected theme to revert to dark theme after dark BackgroundColorMsg")
	}

	revertedView := m.View().Content
	if !strings.Contains(revertedView, "229;165;75") {
		t.Fatalf("expected reverted dark theme view to contain dark accent color")
	}

	initCmd := m.Init()
	if initCmd == nil {
		t.Fatalf("expected non-nil Init cmd for auto-detection")
	}

	cfgFlag, err := parseFlags([]string{"-light"})
	if err != nil {
		t.Fatalf("unexpected error parsing -light flag: %v", err)
	}
	if cfgFlag.light == nil || !*cfgFlag.light {
		t.Fatalf("expected cfg.light to be &true with -light flag")
	}
	mFlag := newModel(cfgFlag, nil)
	if !reflect.DeepEqual(mFlag.theme, lightTheme) {
		t.Fatalf("expected newModel with -light to initialize with light theme")
	}

	resFlag, _ := mFlag.Update(darkMsg)
	mFlag = resFlag.(model)
	if !reflect.DeepEqual(mFlag.theme, lightTheme) {
		t.Fatalf("expected -light flag to preserve light theme against dark BackgroundColorMsg")
	}

	flagInitCmd := mFlag.Init()
	if flagInitCmd == nil {
		t.Fatalf("expected non-nil Init cmd for explicit light mode")
	}

	cfgDarkFlag, err := parseFlags([]string{"-light=false"})
	if err != nil {
		t.Fatalf("unexpected error parsing -light=false flag: %v", err)
	}
	if cfgDarkFlag.light == nil || *cfgDarkFlag.light {
		t.Fatalf("expected cfg.light to be &false with -light=false flag")
	}
	mDarkFlag := newModel(cfgDarkFlag, nil)
	if !reflect.DeepEqual(mDarkFlag.theme, darkTheme) {
		t.Fatalf("expected newModel with -light=false to initialize with dark theme")
	}

	resDarkFlag, _ := mDarkFlag.Update(whiteMsg)
	mDarkFlag = resDarkFlag.(model)
	if !reflect.DeepEqual(mDarkFlag.theme, darkTheme) {
		t.Fatalf("expected -light=false flag to preserve dark theme against white BackgroundColorMsg")
	}

	t.Setenv("TASKD_LIGHT", "1")
	cfgEnv1, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error parsing TASKD_LIGHT=1: %v", err)
	}
	if cfgEnv1.light == nil || !*cfgEnv1.light {
		t.Fatalf("expected cfg.light to be &true with TASKD_LIGHT=1")
	}
	mEnv1 := newModel(cfgEnv1, nil)
	if !reflect.DeepEqual(mEnv1.theme, lightTheme) {
		t.Fatalf("expected newModel with TASKD_LIGHT=1 to initialize with light theme")
	}

	t.Setenv("TASKD_LIGHT", "true")
	cfgEnvTrue, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error parsing TASKD_LIGHT=true: %v", err)
	}
	if cfgEnvTrue.light == nil || !*cfgEnvTrue.light {
		t.Fatalf("expected cfg.light to be &true with TASKD_LIGHT=true")
	}
	mEnvTrue := newModel(cfgEnvTrue, nil)
	if !reflect.DeepEqual(mEnvTrue.theme, lightTheme) {
		t.Fatalf("expected newModel with TASKD_LIGHT=true to initialize with light theme")
	}

	t.Setenv("TASKD_LIGHT", "0")
	cfgEnvZero, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error parsing TASKD_LIGHT=0: %v", err)
	}
	if cfgEnvZero.light == nil || *cfgEnvZero.light {
		t.Fatalf("expected cfg.light to be &false with TASKD_LIGHT=0")
	}
	mEnvZero := newModel(cfgEnvZero, nil)
	if !reflect.DeepEqual(mEnvZero.theme, darkTheme) {
		t.Fatalf("expected newModel with TASKD_LIGHT=0 to initialize with dark theme")
	}
	resEnvZero, _ := mEnvZero.Update(whiteMsg)
	mEnvZero = resEnvZero.(model)
	if !reflect.DeepEqual(mEnvZero.theme, darkTheme) {
		t.Fatalf("expected TASKD_LIGHT=0 to preserve dark theme against white BackgroundColorMsg")
	}

	t.Setenv("TASKD_LIGHT", "false")
	cfgEnvFalse, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error parsing TASKD_LIGHT=false: %v", err)
	}
	if cfgEnvFalse.light == nil || *cfgEnvFalse.light {
		t.Fatalf("expected cfg.light to be &false with TASKD_LIGHT=false")
	}

	t.Setenv("TASKD_LIGHT", "")
	cfgEnvUnset, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error parsing unset TASKD_LIGHT: %v", err)
	}
	if cfgEnvUnset.light != nil {
		t.Fatalf("expected cfg.light to be nil when unset")
	}
}

func TestTUILightFlagInvalid(t *testing.T) {
	_, err := parseFlags([]string{"-light=maybe"})
	if err == nil {
		t.Fatalf("expected error for invalid -light value")
	}
	t.Setenv("TASKD_LIGHT", "maybe")
	_, err = parseFlags(nil)
	if err == nil {
		t.Fatalf("expected error for invalid TASKD_LIGHT value")
	}
}
