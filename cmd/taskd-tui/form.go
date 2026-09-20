package main

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type formModel struct {
	title     string
	project   textinput.Model
	priority  textinput.Model
	asset     textinput.Model
	body      textarea.Model
	focus     int // 0 project, 1 priority, 2 asset, 3 body, 4 save button
	editing   bool
	id        string
	errText   string
	done      bool
	cancelled bool

	origProject     string
	origPriority    int
	origHasPriority bool
	origAsset       string
	origBody        string
}

func newCreateForm(project string, width int) formModel {
	f := formModel{
		title:   "New Task",
		editing: false,
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

	f.body = textarea.New()
	f.body.Prompt = ""
	f.body.Placeholder = "first line is the title"
	f.body.ShowLineNumbers = false

	f.resize(width, 24)

	if project == "" {
		f.setFocus(0)
	} else {
		f.setFocus(3)
	}

	return f
}

func newEditForm(t task, width int) formModel {
	f := formModel{
		title:           "Edit Task",
		editing:         true,
		id:              t.ID,
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
	f.body.SetValue(t.Body)

	f.resize(width, 24)

	f.setFocus(3)

	return f
}

func (f *formModel) resize(width, height int) {
	boxWidth := max(60, min(width-4, 90))
	if width > 0 && boxWidth > width {
		boxWidth = width
	}
	inner := max(20, boxWidth-4)
	inputW := max(10, inner-11)
	f.project.SetWidth(inputW)
	f.priority.SetWidth(inputW)
	f.asset.SetWidth(inputW)
	f.body.SetWidth(inner)
	bodyH, _ := formBodyHeight(height, f.errText != "")
	f.body.SetHeight(bodyH)
}

// formBodyHeight fits the form to the terminal: the textarea takes what
// is left after the fixed rows, and when even one row does not fit the
// two blank separators and the hint line go (compact), never the button.
func formBodyHeight(height int, hasErr bool) (bodyH int, compact bool) {
	fixed := 12 // border 2, title, blank, 3 fields, body label, blank, save, blank, hint
	if hasErr {
		fixed++
	}
	spare := height - fixed
	if spare < 1 {
		compact = true
		spare += 3
	}
	return max(1, min(12, spare)), compact
}

func (f *formModel) setFocus(target int) tea.Cmd {
	f.focus = (target%5 + 5) % 5
	f.project.Blur()
	f.priority.Blur()
	f.asset.Blur()
	f.body.Blur()
	switch f.focus {
	case 0:
		return f.project.Focus()
	case 1:
		return f.priority.Focus()
	case 2:
		return f.asset.Focus()
	case 3:
		return f.body.Focus()
	case 4:
		return nil
	}
	return nil
}

func (f formModel) validate() string {
	if strings.TrimSpace(f.body.Value()) == "" {
		return "body cannot be blank"
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
		for i := 0; i < len(proj); i++ {
			c := proj[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
				return "project may only contain [A-Za-z0-9._-]"
			}
		}
	}

	return ""
}

func (f formModel) Update(msg tea.Msg) (formModel, tea.Cmd) {
	f.done = false
	f.cancelled = false

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.Code == tea.KeyEscape {
			f.cancelled = true
			return f, nil
		}

		if msg.Code == 's' && msg.Mod&tea.ModCtrl != 0 {
			if err := f.validate(); err != "" {
				f.errText = err
				return f, nil
			}
			f.errText = ""
			f.done = true
			return f, nil
		}

		if msg.Code == tea.KeyTab {
			if msg.Mod&tea.ModShift != 0 {
				cmd := f.setFocus(f.focus - 1)
				return f, cmd
			}
			cmd := f.setFocus(f.focus + 1)
			return f, cmd
		}

		if msg.Code == tea.KeyEnter {
			if f.focus >= 0 && f.focus <= 2 {
				cmd := f.setFocus(f.focus + 1)
				return f, cmd
			}
			if f.focus == 4 {
				if err := f.validate(); err != "" {
					f.errText = err
					return f, nil
				}
				f.errText = ""
				f.done = true
				return f, nil
			}
		}

		var cmd tea.Cmd
		switch f.focus {
		case 0:
			f.project, cmd = f.project.Update(msg)
		case 1:
			f.priority, cmd = f.priority.Update(msg)
		case 2:
			f.asset, cmd = f.asset.Update(msg)
		case 3:
			f.body, cmd = f.body.Update(msg)
		case 4:
		}
		return f, cmd

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

	return method, path, body, success, ""
}

func (f formModel) View(width, height int, th theme) string {
	f.resize(width, height)

	boxWidth := max(60, min(width-4, 90))
	if width > 0 && boxWidth > width {
		boxWidth = width
	}

	_, compact := formBodyHeight(height, f.errText != "")
	var lines []string
	lines = append(lines, th.accent.Render(f.title))
	if !compact {
		lines = append(lines, "")
	}
	lines = append(lines, th.dim.Render("project:  ")+f.project.View())
	lines = append(lines, th.dim.Render("priority: ")+f.priority.View())
	lines = append(lines, th.dim.Render("asset:    ")+f.asset.View())
	lines = append(lines, th.dim.Render("body:"))
	lines = append(lines, f.body.View())
	if !compact {
		lines = append(lines, "")
	}

	if f.focus == 4 {
		lines = append(lines, th.accentPill.Render("[ save ]"))
	} else {
		lines = append(lines, th.dim.Render("[ save ]"))
	}

	if f.errText != "" {
		lines = append(lines, th.err.Render(f.errText))
	}
	if !compact {
		lines = append(lines, "")
		lines = append(lines, th.dim.Render("Tab next  ctrl-s save  Esc cancel"))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	boxStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.accent.GetForeground()).
		Padding(0, 1).
		Width(boxWidth)

	return boxStyle.Render(content)
}

type confirmModel struct {
	text, button, method, path, success string
	body                                any
}

func (c confirmModel) View(width, height int, th theme) string {
	btn := c.button
	if btn == "" {
		btn = "confirm"
	}

	actions := th.accent.Render("[y] "+btn) + "   " + th.dim.Render("[n] cancel")
	content := lipgloss.JoinVertical(lipgloss.Center, c.text, "", actions)

	boxWidth := max(30, min(width-4, 50))
	if width > 0 && boxWidth > width {
		boxWidth = width
	}

	boxStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.accent.GetForeground()).
		Padding(1, 2).
		Width(boxWidth).
		Align(lipgloss.Center)

	return boxStyle.Render(content)
}

func padRightVisual(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

func helpView(width, height int, th theme) string {
	type keyRef struct {
		key  string
		desc string
	}

	col1 := []keyRef{
		{"j/k, ↑/↓", "move"},
		{"g/G", "first/last"},
		{"ctrl-d/ctrl-u", "half page"},
		{"PgUp/PgDn", "page"},
		{"p", "project"},
		{"/", "search"},
		{"Tab", "detail"},
		{"z", "zoom"},
		{"n", "new"},
		{"e", "edit"},
	}

	col2 := []keyRef{
		{"0-4", "filter (all/pending/leased/done/buried)"},
		{"+/-", "priority"},
		{"c", "claim"},
		{"u", "release"},
		{"D", "delete"},
		{"x", "complete"},
		{"y/Y", "copy id/body"},
		{"r", "refresh"},
		{"?", "help"},
		{"q", "quit"},
	}

	var rows []string
	rows = append(rows, th.accent.Render("Keyboard Shortcuts"))
	rows = append(rows, "")

	for i := 0; i < len(col1) && i < len(col2); i++ {
		k1 := th.accent.Render(padRightVisual(col1[i].key, 13))
		d1 := th.dim.Render(padRightVisual(col1[i].desc, 10))

		k2 := th.accent.Render(padRightVisual(col2[i].key, 4))
		d2 := th.dim.Render(col2[i].desc)

		row := k1 + " " + d1 + "  " + k2 + " " + d2
		rows = append(rows, row)
	}

	rows = append(rows, "")
	rows = append(rows, th.dim.Render("Press ? or Esc to close"))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)

	maxW := max(20, width-4)
	maxH := max(3, height)

	boxStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.accent.GetForeground()).
		Padding(0, 1).
		MaxWidth(maxW).
		MaxHeight(maxH)

	return boxStyle.Render(content)
}
