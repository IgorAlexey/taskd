package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const maxTaskIDLen = 128

type formField int

const (
	fieldProject formField = iota
	fieldPriority
	fieldAsset
	fieldID
	fieldBody
	fieldSave
	fieldCancel
)

type formModel struct {
	title           string
	project         textinput.Model
	priority        textinput.Model
	asset           textinput.Model
	customID        textinput.Model
	body            textarea.Model
	focus           formField
	editing         bool
	id              string
	version         int
	errText         string
	done            bool
	cancelled       bool
	discarding      bool
	origProject     string
	origPriority    int
	origHasPriority bool
	origAsset       string
	origBody        string
	width, height   int
	th              theme
}

func newCreateForm(project string) (formModel, tea.Cmd) {
	f := formModel{
		title:       "New Task",
		editing:     false,
		origProject: project,
	}

	f.project = textinput.New()
	f.project.Prompt = ""
	f.project.SetValue(project)

	f.priority = textinput.New()
	f.priority.Prompt = ""
	f.priority.Placeholder = "3 (1 is top, blank for default)"

	f.asset = textinput.New()
	f.asset.Prompt = ""
	f.asset.Placeholder = "optional"

	f.customID = textinput.New()
	f.customID.Prompt = ""
	f.customID.Placeholder = "optional"
	f.body = textarea.New()
	f.body.Prompt = ""
	f.body.Placeholder = "first line is the title"
	f.body.ShowLineNumbers = false

	if project == "" {
		return f, f.setFocus(fieldProject)
	}
	return f, f.setFocus(fieldBody)
}

func newEditForm(t task) (formModel, tea.Cmd) {
	f := formModel{
		title:           "Edit Task",
		editing:         true,
		id:              t.ID,
		version:         t.Version,
		origProject:     t.Project,
		origPriority:    t.Priority,
		origHasPriority: true,
		origAsset:       t.AssetPath,
		origBody:        t.Body,
	}

	f.project = textinput.New()
	f.project.Prompt = ""
	f.project.SetValue(t.Project)

	f.priority = textinput.New()
	f.priority.Prompt = ""
	f.priority.Placeholder = "3 (1 is top, blank for default)"
	f.priority.SetValue(strconv.Itoa(t.Priority))

	f.asset = textinput.New()
	f.asset.Prompt = ""
	f.asset.Placeholder = "optional"
	f.asset.SetValue(t.AssetPath)

	f.body = textarea.New()
	f.body.Prompt = ""
	f.body.Placeholder = "first line is the title"
	f.body.ShowLineNumbers = false
	f.customID = textinput.New()
	f.customID.Prompt = ""
	f.customID.SetValue(t.ID)

	f.body.SetValue(t.Body)

	cmd := f.setFocus(fieldBody)
	return f, cmd
}

// boxSize is an overlay's outer width for a terminal, between lo and hi
// with a two-column margin, and the text width inside its border and
// padding. Under five columns there is no text width; box draws nothing.
func boxSize(width, lo, hi int) (outer, inner int) {
	outer = min(max(lo, min(width-4, hi)), width)
	return outer, outer - 4
}

func (f *formModel) resize(inner int) {
	f.project.SetWidth(max(1, inner-11))
	f.priority.SetWidth(max(1, inner-11))
	f.asset.SetWidth(max(1, inner-11))
	f.customID.SetWidth(max(1, inner-11))
	f.body.SetWidth(inner)
}

// formRow is one row of the form with its drop rank: when the terminal
// is short, rows go one at a time from the lowest rank up. Rank 0 is
// never dropped; the focused field holds it, so nothing is typed into a
// row nobody can see. What is drawn is not what is sent: submit sends
// every field.
type formRow struct {
	text  string
	rank  int
	field formField
}

// Drop ranks, lowest goes first. A field row with text in it adds
// rankFilled, so filled fields (at most rankProject+rankFilled) outlive
// empty ones and the title, and still go before the button; the button
// goes before the error because a button without its error invites a
// silent failure. The focused row, field or button, is rank 0.
const (
	rankHint      = 1
	rankSeparator = 2
	rankBodyLabel = 3
	rankAsset     = 4
	rankID        = 4
	rankTitle     = 5
	rankBody      = 6
	rankPriority  = 7
	rankProject   = 8
	rankFilled    = 5
	rankButton    = 14
	rankError     = 15
)

func (f formModel) focusOrder() []formField {
	if f.editing {
		return []formField{fieldProject, fieldPriority, fieldAsset, fieldBody, fieldSave, fieldCancel}
	}
	return []formField{fieldProject, fieldPriority, fieldAsset, fieldID, fieldBody, fieldSave, fieldCancel}
}

// rows is the whole form in reading order. Field rows take their rank
// unless focused, which pins them; a field with text in it outranks
// every empty one, so what the user typed stays on screen longest.
func (f formModel) cancel() formModel {
	if f.dirty() {
		f.discarding = true
		return f
	}
	f.cancelled = true
	return f
}
func (f formModel) activateSave() (formModel, tea.Cmd) {
	if err := f.validate(); err != "" {
		f.errText = err
		return f.refit(), nil
	}
	f.errText = ""
	f.done = true
	return f, nil
}

func (f formModel) rows(th theme) []formRow {
	field := func(n formField, rank int, value, text string) formRow {
		switch {
		case f.focus == n:
			rank = 0
		case value != "":
			rank += rankFilled
		}
		return formRow{text: text, rank: rank, field: n}
	}
	saveToken := "[ save ]"
	cancelToken := "[ cancel ]"
	save, saveRank := th.dim.Render(saveToken), rankButton
	if f.focus == fieldSave {
		save, saveRank = th.accentPill.Render(saveToken), 0
	}
	cancel, cancelRank := th.dim.Render(cancelToken), rankButton
	if f.focus == fieldCancel {
		cancel, cancelRank = th.accentPill.Render(cancelToken), 0
	}
	btnRank := min(saveRank, cancelRank)
	gapStr := strings.Repeat(" ", buttonGap)
	rows := []formRow{
		{text: th.accent.Render(f.title), rank: rankTitle, field: -1},
		{rank: rankSeparator, field: -1},
		field(fieldProject, rankProject, f.project.Value(), th.dim.Render("project:  ")+f.project.View()),
		field(fieldPriority, rankPriority, f.priority.Value(), th.dim.Render("priority: ")+f.priority.View()),
		field(fieldAsset, rankAsset, f.asset.Value(), th.dim.Render("asset:    ")+f.asset.View()),
	}
	if !f.editing {
		rows = append(rows, field(fieldID, rankID, f.customID.Value(), th.dim.Render("ID:       ")+f.customID.View()))
	}
	rows = append(rows,
		formRow{text: th.dim.Render("body:"), rank: rankBodyLabel, field: -1},
		field(fieldBody, rankBody, f.body.Value(), ""),
		formRow{rank: rankSeparator, field: -1},
	)
	if f.errText != "" {
		rows = append(rows, formRow{text: th.err.Render(f.errText), rank: rankError, field: -1})
	}
	rows = append(rows,
		formRow{text: save + gapStr + cancel, rank: btnRank, field: fieldSave},
		formRow{rank: rankHint, field: -1},
		formRow{text: th.dim.Render("Tab next  ctrl-s save  Esc cancel"), rank: rankHint, field: -1},
	)
	return rows
}

// wrapRows renders rows at width so each element is one terminal line;
// a row that soft-wraps becomes several. Counting happens after this.
func wrapRows(rows []string, width int) []string {
	if width < 1 {
		return nil
	}
	var out []string
	for _, r := range rows {
		soft := lipgloss.NewStyle().Width(width).Render(r)
		for _, l := range strings.Split(ansi.Hardwrap(soft, width, true), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}

// box draws an overlay. head are lines that may be cut; keep are the
// lines of one row, already wrapped to boxWidth-4, drawn after them and
// meant to survive: when the frame is short, head goes first and keep is
// trimmed to its opening lines rather than removed.
func box(head, keep []string, boxWidth, height int, align lipgloss.Position, th theme) string {
	if boxWidth < 5 {
		return ""
	}
	room := max(1, height-2)
	if len(head)+len(keep) > room {
		head = head[:max(0, room-len(keep))]
	}
	lines := append(append([]string{}, head...), keep...)
	if len(lines) > room {
		lines = lines[:room]
	}
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.accent.GetForeground()).
		Padding(0, 1).
		Width(boxWidth).
		Align(align).
		Render(lipgloss.JoinVertical(align, lines...))
}

func (f *formModel) setFocus(target formField) tea.Cmd {
	f.focus = target
	f.project.Blur()
	f.priority.Blur()
	f.asset.Blur()
	f.customID.Blur()
	f.body.Blur()
	switch f.focus {
	case fieldProject:
		return f.project.Focus()
	case fieldPriority:
		return f.priority.Focus()
	case fieldAsset:
		return f.asset.Focus()
	case fieldID:
		if !f.editing {
			return f.customID.Focus()
		}
	case fieldBody:
		return f.body.Focus()
	}
	return nil
}

func validTaskByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
}

func validateCustomID(id string) string {
	if id == "" {
		return ""
	}
	if len(id) > maxTaskIDLen {
		return fmt.Sprintf("id must not exceed %d characters", maxTaskIDLen)
	}
	if id == "." || id == ".." || strings.EqualFold(id, "claim") || strings.EqualFold(id, "purge") {
		return fmt.Sprintf("id %q is reserved", id)
	}
	for i := range len(id) {
		if !validTaskByte(id[i]) {
			return "id may only contain [A-Za-z0-9._-]"
		}
	}
	return ""
}

func (f formModel) validate() string {
	if strings.TrimSpace(f.body.Value()) == "" && strings.TrimSpace(f.asset.Value()) == "" {
		return "missing asset_path or body"
	}

	pri := strings.TrimSpace(f.priority.Value())
	if pri != "" {
		p, err := strconv.Atoi(pri)
		if err != nil || p < 0 {
			return "priority must be an integer >= 0"
		}
	}

	proj := strings.TrimSpace(f.project.Value())
	if !f.editing && proj == "" {
		return "project cannot be blank"
	}
	if proj != "" {
		if len(proj) > maxProjectLen {
			return fmt.Sprintf("project must not exceed %d characters", maxProjectLen)
		}
		if !validProjectChars(proj) {
			return "project may only contain [A-Za-z0-9._-]"
		}
	}

	if !f.editing {
		if idErr := validateCustomID(strings.TrimSpace(f.customID.Value())); idErr != "" {
			return idErr
		}
	}

	return ""
}

func (f formModel) dirty() bool {
	if f.project.Value() != f.origProject {
		return true
	}
	if f.asset.Value() != f.origAsset {
		return true
	}
	if f.body.Value() != f.origBody {
		return true
	}
	if !f.editing && f.customID.Value() != "" {
		return true
	}
	if f.editing {
		return f.priority.Value() != strconv.Itoa(f.origPriority)
	}
	return f.priority.Value() != ""
}

func (f formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	f.done = false
	f.cancelled = false

	if f.discarding {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch {
			case msg.Text == "y" || msg.Text == "Y":
				f.cancelled = true
				f.discarding = false
				return f, nil
			case msg.Code == tea.KeyEnter || msg.Code == tea.KeyEscape ||
				msg.Text == "n" || msg.Text == "N" || msg.Text == "q":
				f.discarding = false
				return f.refit(), nil
			}
		}
		return f, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.Code == tea.KeyEscape {
			return f.cancel(), nil
		}

		if msg.Code == 's' && msg.Mod&tea.ModCtrl != 0 {
			if err := f.validate(); err != "" {
				f.errText = err
				return f.refit(), nil
			}
			f.errText = ""
			f.done = true
			return f, nil
		}

		if msg.Code == tea.KeyTab {
			order := f.focusOrder()
			curIdx := 0
			for i, fld := range order {
				if fld == f.focus {
					curIdx = i
					break
				}
			}
			if msg.Mod&tea.ModShift != 0 {
				next := order[(curIdx-1+len(order))%len(order)]
				cmd := f.setFocus(next)
				return f.refit(), cmd
			}
			next := order[(curIdx+1)%len(order)]
			cmd := f.setFocus(next)
			return f.refit(), cmd
		}

		if msg.Code == tea.KeyEnter {
			switch f.focus {
			case fieldProject:
				cmd := f.setFocus(fieldPriority)
				return f.refit(), cmd
			case fieldPriority:
				cmd := f.setFocus(fieldAsset)
				return f.refit(), cmd
			case fieldAsset:
				if f.editing {
					cmd := f.setFocus(fieldBody)
					return f.refit(), cmd
				}
				cmd := f.setFocus(fieldID)
				return f.refit(), cmd
			case fieldID:
				cmd := f.setFocus(fieldBody)
				return f.refit(), cmd
			case fieldSave:
				return f.activateSave()
			case fieldCancel:
				return f.cancel(), nil
			}
		}

		var cmd tea.Cmd
		switch f.focus {
		case fieldProject:
			f.project, cmd = f.project.Update(msg)
		case fieldPriority:
			f.priority, cmd = f.priority.Update(msg)
		case fieldAsset:
			f.asset, cmd = f.asset.Update(msg)
		case fieldID:
			if !f.editing {
				f.customID, cmd = f.customID.Update(msg)
			}
		case fieldBody:
			f.body, cmd = f.body.Update(msg)
		case fieldSave:
			if msg.Code == tea.KeySpace && msg.Mod == 0 {
				return f.activateSave()
			}
		case fieldCancel:
			if msg.Code == tea.KeySpace && msg.Mod == 0 {
				return f.cancel(), nil
			}
		}
		return f.refit(), cmd

	case tea.MouseClickMsg:
		return f.handleClick(msg)

	default:
		var cmds []tea.Cmd
		var cmd tea.Cmd
		f.project, cmd = f.project.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		f.priority, cmd = f.priority.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		f.asset, cmd = f.asset.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if !f.editing {
			f.customID, cmd = f.customID.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		f.body, cmd = f.body.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return f, tea.Batch(cmds...)
	}
}

func (f formModel) submit() (method, path string, body map[string]any, success string, errText string) {
	if err := f.validate(); err != "" {
		return "", "", nil, "", err
	}

	body = make(map[string]any)

	if !f.editing {
		method = "POST"
		path = "/tasks"
		success = "created task"

		body["project"] = strings.TrimSpace(f.project.Value())
		body["body"] = f.body.Value()

		customID := strings.TrimSpace(f.customID.Value())
		if customID != "" {
			body["id"] = customID
		}

		asset := strings.TrimSpace(f.asset.Value())
		if asset != "" {
			body["asset_path"] = asset
		}

		priStr := strings.TrimSpace(f.priority.Value())
		if priStr != "" {
			if p, err := strconv.Atoi(priStr); err == nil {
				body["priority"] = p
			}
		}
		return method, path, body, success, ""
	}

	method = "PATCH"
	path = "/tasks/" + f.id
	id7 := f.id
	if len(id7) > 7 {
		id7 = id7[:7]
	}
	success = "updated task " + id7

	if f.body.Value() != f.origBody {
		body["body"] = f.body.Value()
	}

	proj := strings.TrimSpace(f.project.Value())
	if proj != f.origProject && proj != "" {
		body["project"] = proj
	}

	asset := strings.TrimSpace(f.asset.Value())
	if asset != f.origAsset {
		body["asset_path"] = asset
	}

	priStr := strings.TrimSpace(f.priority.Value())
	if priStr != "" {
		if p, err := strconv.Atoi(priStr); err == nil {
			if !f.origHasPriority || p != f.origPriority {
				body["priority"] = p
			}
		}
	}
	if len(body) > 0 && f.version > 0 {
		body["if_version"] = f.version
	}
	return method, path, body, success, ""
}

// fit lays the form out for a terminal: rows are built, wrapped to the
// box and counted, then dropped one at a time by rank until they fit;
// the textarea, when drawn, takes what is left. It sets the stored
// textarea size, so Update and View agree on the layout.
func (f *formModel) fitRows(width, height int, th theme) (rows []formRow, wrapped [][]string, head []string, boxWidth int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	f.width, f.height, f.th = width, height, th
	boxWidth, inner := boxSize(width, 20, 90)
	f.resize(inner)

	rows = f.rows(th)
	wrapped = make([][]string, len(rows))
	total := 2
	for i, r := range rows {
		if r.field == fieldBody {
			wrapped[i] = []string{""}
		} else {
			wrapped[i] = wrapRows([]string{r.text}, inner)
		}
		total += len(wrapped[i])
	}
	for total > height {
		drop := -1
		for i, r := range rows {
			if r.rank > 0 && (drop < 0 || r.rank < rows[drop].rank) {
				drop = i
			}
		}
		if drop < 0 {
			break
		}
		total -= len(wrapped[drop])
		rows = append(rows[:drop], rows[drop+1:]...)
		wrapped = append(wrapped[:drop], wrapped[drop+1:]...)
	}

	body := -1
	for i, r := range rows {
		if r.field == fieldBody {
			body = i
		}
	}
	if body >= 0 {
		bodyH := max(1, min(12, height-total+1))
		f.body.SetHeight(bodyH)
		lines := wrapRows([]string{f.body.View()}, inner)
		if len(lines) > bodyH {
			lines = lines[:bodyH]
		}
		wrapped[body] = lines
	}

	for _, w := range wrapped {
		head = append(head, w...)
	}
	return rows, wrapped, head, boxWidth
}

type formButtonTarget struct {
	field formField
	start int
	end   int
}

func formButtonBounds(left int) []formButtonTarget {
	saveW := ansi.StringWidth("[ save ]")
	cancelW := ansi.StringWidth("[ cancel ]")
	return []formButtonTarget{
		{field: fieldSave, start: left, end: left + saveW},
		{field: fieldCancel, start: left + saveW + buttonGap, end: left + saveW + buttonGap + cancelW},
	}
}

func (f formModel) handleClick(msg tea.MouseClickMsg) (formModel, tea.Cmd) {
	if msg.Button != tea.MouseLeft || f.discarding {
		return f, nil
	}
	w, h := f.width, f.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	rows, wrapped, head, boxWidth := f.fitRows(w, h, f.th)
	room := max(1, h-2)
	lines := head
	if len(lines) > room {
		lines = lines[:room]
	}
	boxH := len(lines) + 2
	top := max(0, (h-boxH)/2)
	left := max(0, (w-boxWidth)/2)
	if msg.X < left || msg.X >= left+boxWidth {
		return f, nil
	}
	boxStyle := lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(0, 1)
	contentTop := top + boxStyle.GetBorderTopSize() + boxStyle.GetPaddingTop()
	if msg.Y < contentTop || msg.Y >= contentTop+len(lines) {
		return f, nil
	}
	lineIdx := msg.Y - contentTop
	cur := 0
	for i, r := range rows {
		rowLines := len(wrapped[i])
		if lineIdx >= cur && lineIdx < cur+rowLines {
			switch r.field {
			case fieldProject, fieldPriority, fieldAsset, fieldID, fieldBody:
				cmd := f.setFocus(r.field)
				return f.refit(), cmd
			case fieldSave:
				contentLeft := left + boxStyle.GetBorderLeftSize() + boxStyle.GetPaddingLeft()
				for _, btn := range formButtonBounds(contentLeft) {
					if msg.X >= btn.start && msg.X < btn.end {
						f.setFocus(btn.field)
						if btn.field == fieldSave {
							return f.activateSave()
						}
						return f.cancel(), nil
					}
				}
			default:
				if r.rank == rankBodyLabel {
					cmd := f.setFocus(fieldBody)
					return f.refit(), cmd
				}
			}
			return f, nil
		}
		cur += rowLines
	}
	return f, nil
}

func (f *formModel) fit(width, height int, th theme) (head []string, boxWidth int) {
	_, _, head, boxWidth = f.fitRows(width, height, th)
	return head, boxWidth
}

// refit lays the form out again for the terminal it was last fitted
// to, so a keypress that changes the rows (an error, a focus move at
// the focused-only level) leaves the stored textarea size current.
// Before any fit it is a no-op.
func (f formModel) refit() formModel {
	if f.width > 0 {
		f.fit(f.width, f.height, f.th)
	}
	return f
}

// View renders the form for the terminal it was last fitted to; the
// copy is refitted so the drawing and the stored layout are the same.
func (f formModel) View() string {
	if f.discarding {
		actions := f.th.accent.Render("[y] discard") + "   " + f.th.dim.Render("[n] cancel")
		boxWidth, inner := boxSize(f.width, 20, 54)
		head := wrapRows([]string{"Discard unsaved changes?", ""}, inner)
		return box(head, wrapRows([]string{actions}, inner), boxWidth, f.height, lipgloss.Center, f.th)
	}
	head, boxWidth := f.fit(f.width, f.height, f.th)
	return box(head, nil, boxWidth, f.height, lipgloss.Left, f.th)
}

type confirmModel struct {
	text, button, method, path, success string
	body                                any
}

const (
	modalBorderW = 1
	modalPadding = 1
	modalFrameW  = modalBorderW + modalPadding
	buttonGap    = 3
)

type confirmLayout struct {
	boxWidth int
	inner    int
	head     []string
	keep     []string
	yToken   string
	nToken   string
	targets  []confirmTarget
}

func (c confirmModel) layout(width, height int) confirmLayout {
	if width < 5 || height < 3 {
		return confirmLayout{}
	}
	btn := c.button
	if btn == "" {
		btn = "confirm"
	}
	yToken := "[y] " + btn
	nToken := "[n] cancel"
	gapStr := strings.Repeat(" ", buttonGap)

	boxWidth, inner := boxSize(width, 20, 54)
	head := wrapRows([]string{c.text, ""}, inner)

	yW := ansi.StringWidth(yToken)
	nW := ansi.StringWidth(nToken)
	singleLineW := yW + buttonGap + nW

	var keep []string
	if singleLineW <= inner {
		keep = []string{yToken + gapStr + nToken}
	} else {
		keep = []string{yToken, nToken}
	}

	room := max(1, height-2*modalBorderW)
	if len(head)+len(keep) > room {
		head = head[:max(0, room-len(keep))]
	}
	if len(head)+len(keep) > room {
		keep = keep[:max(0, room-len(head))]
	}
	if len(keep) == 0 {
		return confirmLayout{boxWidth: boxWidth, inner: inner, head: head}
	}

	boxHeight := len(head) + len(keep) + 2*modalBorderW
	boxX := (width - boxWidth) / 2
	boxY := (height - boxHeight) / 2

	var targets []confirmTarget
	if len(keep) == 1 {
		actY := boxY + modalBorderW + len(head)
		pad := max(0, (inner-singleLineW)/2)
		startX := boxX + modalFrameW + pad
		targets = []confirmTarget{
			{action: confirmActionYes, y: actY, start: startX, end: startX + yW},
			{action: confirmActionNo, y: actY, start: startX + yW + buttonGap, end: startX + yW + buttonGap + nW},
		}
	} else if len(keep) >= 1 {
		actY := boxY + modalBorderW + len(head)
		padY := max(0, (inner-yW)/2)
		startX := boxX + modalFrameW + padY
		targets = append(targets, confirmTarget{
			action: confirmActionYes,
			y:      actY,
			start:  startX,
			end:    startX + yW,
		})
		if len(keep) >= 2 {
			padN := max(0, (inner-nW)/2)
			startN := boxX + modalFrameW + padN
			targets = append(targets, confirmTarget{
				action: confirmActionNo,
				y:      actY + 1,
				start:  startN,
				end:    startN + nW,
			})
		}
	}

	return confirmLayout{
		boxWidth: boxWidth,
		inner:    inner,
		head:     head,
		keep:     keep,
		yToken:   yToken,
		nToken:   nToken,
		targets:  targets,
	}
}

func padCenter(line string, width int) string {
	w := ansi.StringWidth(line)
	left := max(0, (width-w)/2)
	right := max(0, width-w-left)
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", right)
}

func (c confirmModel) View(width, height int, th theme) string {
	l := c.layout(width, height)
	if l.boxWidth < 5 {
		return ""
	}
	gapStr := strings.Repeat(" ", buttonGap)

	var styledKeep []string
	if len(l.keep) == 1 {
		styledKeep = []string{th.accent.Render(l.yToken) + gapStr + th.dim.Render(l.nToken)}
	} else if len(l.keep) >= 2 {
		styledKeep = []string{th.accent.Render(l.yToken), th.dim.Render(l.nToken)}
	}

	var centeredLines []string
	for _, h := range l.head {
		centeredLines = append(centeredLines, padCenter(h, l.inner))
	}
	for _, k := range styledKeep {
		centeredLines = append(centeredLines, padCenter(k, l.inner))
	}

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.accent.GetForeground()).
		Padding(0, modalPadding).
		Width(l.boxWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, centeredLines...))
}

type noteModel struct {
	taskID  string
	author  string
	input   textinput.Model
	prev    mode
	errText string
	width   int
	height  int
	th      theme
	done    bool
	cancel  bool
}

func newNoteModel(taskID, author string, prev mode, width, height int, th theme) (noteModel, tea.Cmd) {
	if author == "" {
		author = os.Getenv("USER")
	}
	if author == "" {
		author = "operator"
	}
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "type a note and press enter..."
	_, inner := boxSize(width, 20, 60)
	ti.SetWidth(max(1, inner-2))
	cmd := ti.Focus()
	return noteModel{
		taskID: taskID,
		author: author,
		input:  ti,
		prev:   prev,
		width:  width,
		height: height,
		th:     th,
	}, cmd
}

func (n *noteModel) resize(width, height int) {
	n.width = width
	n.height = height
	_, inner := boxSize(width, 20, 60)
	n.input.SetWidth(max(1, inner-2))
}

func (n noteModel) Update(msg tea.Msg) (noteModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if isCtrlC(msg) || msg.Code == tea.KeyEscape {
			n.cancel = true
			return n, nil
		}
		if msg.Code == tea.KeyEnter || (msg.Mod&tea.ModCtrl != 0 && (msg.Code == 's' || msg.Code == tea.KeyEnter)) {
			val := strings.TrimSpace(n.input.Value())
			if val == "" {
				n.errText = "note text cannot be empty"
				return n, nil
			}
			n.done = true
			return n, nil
		}
	}
	var cmd tea.Cmd
	n.input, cmd = n.input.Update(msg)
	return n, cmd
}

func (n noteModel) View(width, height int, th theme) string {
	boxWidth, inner := boxSize(width, 20, 60)
	if boxWidth < 5 {
		return ""
	}
	title := th.bold.Render("Add Note")
	if len(n.taskID) > 0 {
		title += " " + th.dim.Render("("+shortID(n.taskID)+")")
	}
	head := []string{title, ""}
	if n.errText != "" {
		head = append(head, th.err.Render(n.errText), "")
	}
	inputLine := n.input.View()
	head = append(head, inputLine, "")
	keep := []string{th.accent.Render("[enter] save") + "   " + th.dim.Render("[esc] cancel")}
	return box(wrapRows(head, inner), wrapRows(keep, inner), boxWidth, height, lipgloss.Left, th)
}

func (c confirmModel) buttonBounds(width, height int) []confirmTarget {
	return c.layout(width, height).targets
}
func padRightVisual(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

type helpModel struct {
	vp       viewport.Model
	prev     mode
	boxWidth int
}

func newHelpModel(width, height int, prev mode, th theme) helpModel {
	type keyRef struct {
		key  string
		desc string
	}
	const col1KeyW = 11
	const col1DescW = 10
	const col2KeyW = 5

	col1 := []keyRef{
		{"[j/k]", "move"},
		{"[g/G]", "first/last"},
		{"[ctrl-d/u]", "half page"},
		{"[PgUp/Dn]", "page"},
		{"[Enter]", "detail"},
		{"[Tab]", "focus"},
		{"[Esc]", "back/reset"},
		{"[p/P]", "project"},
		{"[w/W]", "worker"},
		{"[s/S]", "sort"},
		{"[i]", "invert sort"},
		{"[/]", "search"},
		{"[z]", "zoom"},
		{"[[/]]", "prev/next"},
		{"[n]", "new"},
		{"[e]", "edit"},
		{"[E]", "editor"},
		{"[a]", "note"},
		{"[ctrl-s]", "save form"},
		{"[?]", "help"},
	}
	col2 := []keyRef{
		{"[0-5]", "filter (status)"},
		{"+/-", "priority"},
		{"[c]", "claim"},
		{"[u]", "release"},
		{"[t]", "touch lease"},
		{"[b]", "bury"},
		{"[K]", "kick"},
		{"[D]", "delete"},
		{"[x]", "complete"},
		{"[y/Y]", "copy id/body"},
		{"[r]", "refresh"},
		{"[q]", "quit"},
		{"[X]", "purge done"},
	}

	var head []string
	head = append(head, th.accent.Render("Keyboard Shortcuts"), "")
	n := max(len(col1), len(col2))
	for i := range n {
		var left, right string
		if i < len(col1) {
			left = th.accent.Render(padRightVisual(col1[i].key, col1KeyW)) + " " + th.dim.Render(padRightVisual(col1[i].desc, col1DescW))
		} else {
			left = strings.Repeat(" ", col1KeyW+1+col1DescW)
		}
		if i < len(col2) {
			right = th.accent.Render(padRightVisual(col2[i].key, col2KeyW)) + " " + th.dim.Render(col2[i].desc)
		}
		head = append(head, left+"  "+right)
	}
	head = append(head, "")
	keep := []string{th.dim.Render("Press ? or Esc to Close")}

	natural := lipgloss.Width(lipgloss.JoinVertical(lipgloss.Left, append(head, keep...)...)) + 4
	boxWidth, inner := boxSize(width, 20, natural)

	wrappedHead := wrapRows(head, inner)
	room := max(1, height-2)
	headRoom := min(len(wrappedHead), max(1, room-len(keep)))
	vp := viewport.New()
	vp.SetWidth(inner)
	vp.SetHeight(headRoom)
	vp.SetContent(strings.Join(wrappedHead, "\n"))
	return helpModel{
		vp:       vp,
		prev:     prev,
		boxWidth: boxWidth,
	}
}

func (h helpModel) Update(msg tea.Msg) (helpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case msg.Text == "j" || msg.Code == tea.KeyDown:
			h.vp.ScrollDown(1)
			return h, nil
		case msg.Text == "k" || msg.Code == tea.KeyUp:
			h.vp.ScrollUp(1)
			return h, nil
		case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'd':
			h.vp.HalfPageDown()
			return h, nil
		case msg.Mod&tea.ModCtrl != 0 && msg.Code == 'u':
			h.vp.HalfPageUp()
			return h, nil
		case msg.Code == tea.KeyPgDown:
			h.vp.PageDown()
			return h, nil
		case msg.Code == tea.KeyPgUp:
			h.vp.PageUp()
			return h, nil
		case msg.Text == "g" || msg.Code == tea.KeyHome:
			h.vp.GotoTop()
			return h, nil
		case msg.Text == "G" || msg.Code == tea.KeyEnd:
			h.vp.GotoBottom()
			return h, nil
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			h.vp.ScrollUp(3)
			return h, nil
		case tea.MouseWheelDown:
			h.vp.ScrollDown(3)
			return h, nil
		}
	}
	var cmd tea.Cmd
	h.vp, cmd = h.vp.Update(msg)
	return h, cmd
}

func (h helpModel) View(height int, th theme) string {
	if h.boxWidth < 5 {
		return ""
	}
	head := strings.Split(h.vp.View(), "\n")
	keep := []string{th.dim.Render("Press ? or Esc to Close")}
	return box(head, keep, h.boxWidth, height, lipgloss.Left, th)
}
