package main

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func (m model) layout() (tableRows, detailRows int) {
	h := m.height
	if h <= 0 {
		h = 24
	}
	if m.mode == modeZoom {
		d := h - headerRows - tabRows - footerRows - 1
		if d < 0 {
			d = 0
		}
		return 0, d
	}
	fixed := headerRows + tabRows + gapRows + colHeadRows + footerRows
	avail := h - fixed
	d := h * 3 / 10
	if d < minDetail {
		d = minDetail
	}
	if avail-d < minTable {
		d = avail - minTable
	}
	if d < 0 {
		d = 0
	}
	t := avail - d
	if t < 0 {
		t = 0
	}
	return t, d
}

func (m model) tableRows() int {
	t, _ := m.layout()
	return t
}

func (m model) detailRows() int {
	_, d := m.layout()
	return d
}

func trunc(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if uniseg.StringWidth(s) <= w {
		return s
	}
	ew := ansi.StringWidth(ellipsis)
	if ew >= w {
		return ansi.Truncate(s, w, "")
	}
	return ansi.Truncate(s, w, ellipsis)
}

func padRight(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

func padLine(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	return s
}

func leaseLeft(t task, now time.Time) string {
	if t.Status != "leased" || t.LeaseExpires <= 0 {
		return ""
	}
	rem := t.LeaseExpires - now.Unix()
	if rem <= 0 {
		return "expired"
	}
	if rem >= 3600 {
		hours := rem / 3600
		mins := (rem % 3600) / 60
		return fmt.Sprintf("%dh%02dm", hours, mins)
	}
	mins := rem / 60
	secs := rem % 60
	return fmt.Sprintf("%dm%02ds", mins, secs)
}

func highlightCode(s string, th theme) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(s, '`')
		if start == -1 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		s = s[start+1:]
		end := strings.IndexByte(s, '`')
		if end == -1 {
			b.WriteByte('`')
			b.WriteString(s)
			break
		}
		for i, line := range strings.Split(s[:end], "\n") {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(th.code.Render(line))
		}
		s = s[end+1:]
	}
	return b.String()
}

func (m model) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	if m.mode == modeForm {
		box := m.form.View(w, h, m.theme)
		placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
		v := tea.NewView(placed)
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	if m.mode == modeConfirm {
		box := m.confirm.View(w, h, m.theme)
		placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
		v := tea.NewView(placed)
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	if m.mode == modeHelp {
		box := helpView(w, h, m.theme)
		placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
		v := tea.NewView(placed)
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	tRows, dRows := m.layout()

	// 1. Header (1 row)
	pill := m.theme.accent.Render(m.glyph.pillL) + m.theme.accentPill.Render(m.glyph.server+" taskd") + m.theme.accent.Render(m.glyph.pillR)
	urlPart := " " + m.cfg.url + " "
	var connPart string
	if m.connected {
		connPart = m.theme.ok.Render(m.glyph.dot + " connected")
	} else {
		connPart = m.theme.err.Render(m.glyph.dot + " disconnected")
		if m.lastErr != "" {
			connPart += " " + m.theme.dim.Render(m.lastErr)
		}
	}
	headLeft := pill + urlPart + connPart

	var dbPart string
	if m.stats.DB != "" {
		dbPart = m.glyph.db + " " + m.stats.DB + "   "
	}
	refreshDur := m.cfg.refresh
	if refreshDur == 0 {
		refreshDur = 2 * time.Second
	}
	refreshPart := m.glyph.refresh + " every " + refreshDur.String()
	headRight := m.theme.dim.Render(dbPart + refreshPart)

	hlw := lipgloss.Width(headLeft)
	hrw := lipgloss.Width(headRight)
	var headerLine string
	if hlw+hrw+1 <= w {
		spaces := w - hlw - hrw
		headerLine = headLeft + strings.Repeat(" ", spaces) + headRight
	} else {
		headerLine = padLine(headLeft+" "+headRight, w)
	}

	// 2. Tabs (1 row)
	type tabInfo struct {
		key    string
		name   string
		count  int
		filter string
	}
	st := m.stats
	if !m.connected {
		// The list may have been applied while /stats failed; count what
		// is on hand rather than show numbers from an older poll.
		st = stats{}
		for _, t := range m.tasks {
			if m.project != "" && t.Project != m.project {
				continue
			}
			st.Total++
			switch t.Status {
			case "pending":
				st.Pending++
			case "leased":
				st.Leased++
			case "done":
				st.Done++
			case "buried":
				st.Buried++
			}
		}
	}
	tabDefs := []tabInfo{
		{"0", "all", st.Total, ""},
		{"1", "pending", st.Pending, "pending"},
		{"2", "leased", st.Leased, "leased"},
		{"3", "done", st.Done, "done"},
	}
	if st.Buried > 0 || m.filter == "buried" {
		tabDefs = append(tabDefs, tabInfo{"4", "buried", st.Buried, "buried"})
	}

	var tabParts []string
	for _, tab := range tabDefs {
		isActive := (m.filter == tab.filter)
		countStr := strconv.Itoa(tab.count)
		if isActive {
			body := tab.key + " " + tab.name + " " + countStr
			tabParts = append(tabParts, m.theme.accent.Render(m.glyph.pillL)+m.theme.tabActive.Render(body)+m.theme.accent.Render(m.glyph.pillR))
		} else {
			tabParts = append(tabParts, m.theme.tabKey.Render(tab.key)+" "+tab.name+" "+m.theme.dim.Render(countStr))
		}
	}
	tabsLeft := strings.Join(tabParts, "  ")

	projName := m.project
	if projName == "" {
		projName = "all"
	}
	tabsRight := m.glyph.folder + " " + m.theme.tabKey.Render("p") + " " + m.theme.dim.Render("project ") + projName

	tlw := lipgloss.Width(tabsLeft)
	trw := lipgloss.Width(tabsRight)
	var tabLine string
	if tlw+trw+1 <= w {
		spaces := w - tlw - trw
		tabLine = tabsLeft + strings.Repeat(" ", spaces) + tabsRight
	} else {
		tabLine = padLine(tabsLeft+" "+tabsRight, w)
	}

	// Table calculation (only if not zoomed)
	var colHeadLine string
	var tableLines []string

	if m.mode != modeZoom && tRows > 0 {
		end := m.offset + tRows
		if end > len(m.shown) {
			end = len(m.shown)
		}
		maxScope := 0
		maxWorker := 0
		maxClaims := 0
		for _, idx := range m.shown {
			if idx >= 0 && idx < len(m.tasks) {
				t := m.tasks[idx]
				if t.Project != "" {
					sw := ansi.StringWidth(t.Project)
					if sw > maxScope {
						maxScope = sw
					}
				}
				_, co := workerParts(t.Worker)
				if co != "" {
					ww := ansi.StringWidth(m.glyph.branch + path.Base(co))
					if ww > maxWorker {
						maxWorker = ww
					}
				}
				if t.ClaimCount > 1 {
					cw := ansi.StringWidth(m.glyph.refresh + " " + strconv.Itoa(t.ClaimCount))
					if cw > maxClaims {
						maxClaims = cw
					}
				}
			}
		}
		wScope := max(maxScope, 5)
		if wScope > 12 {
			wScope = 12
		}
		wWorker := maxWorker
		if wWorker > 14 {
			wWorker = 14
		}
		wClaims := maxClaims
		wLease := 8
		wLeft := 7
		wID := 7

		hasScrollbar := len(m.shown) > tRows
		fixedWidth := 1 + 1 + 1 + 1 + 1 // cursor(1), status(1), space(1), pri(1), space(1) = 5
		if wScope > 0 {
			fixedWidth += wScope + 1
		}
		if wClaims > 0 {
			fixedWidth += 1 + wClaims
		}
		if wWorker > 0 {
			fixedWidth += 1 + wWorker
		}
		fixedWidth += 1 + wLease + 1 + wLeft + 1 + wID
		if hasScrollbar {
			fixedWidth += 1
		}
		wTitle := w - fixedWidth
		if wTitle < 5 {
			wTitle = 5
		}

		// 4. Column header (1 row, dim, lowercase)
		var colH strings.Builder
		colH.WriteString("  p ")
		if wScope > 0 {
			scopeHead := "scope"
			if len(scopeHead) > wScope {
				scopeHead = scopeHead[:wScope]
			}
			colH.WriteString(padRight(scopeHead, wScope))
			colH.WriteString(" ")
		}
		colH.WriteString(padRight("title", wTitle))
		if wClaims > 0 {
			colH.WriteString(" ")
			colH.WriteString(padRight("", wClaims))
		}
		if wWorker > 0 {
			colH.WriteString(" ")
			workerHead := "worker"
			if len(workerHead) > wWorker {
				workerHead = workerHead[:wWorker]
			}
			colH.WriteString(padRight(workerHead, wWorker))
		}
		colH.WriteString(" ")
		colH.WriteString(padRight("lease", wLease))
		colH.WriteString(" ")
		colH.WriteString(padRight("left", wLeft))
		colH.WriteString(" ")
		colH.WriteString(padRight("id", wID))
		if hasScrollbar {
			colH.WriteString(" ")
		}
		colHeadLine = padLine(m.theme.heading.Render(colH.String()), w)

		// 5. Table rows
		tableLines = make([]string, tRows)
		if len(m.shown) == 0 {
			var emptyMsg string
			if m.query != "" || m.filter != "" || m.project != "" {
				emptyMsg = "no tasks match (Esc clears filter)"
			} else {
				emptyMsg = "no tasks yet, press n to create one"
			}
			mid := tRows / 2
			for r := 0; r < tRows; r++ {
				if r == mid {
					tableLines[r] = padLine(lipgloss.PlaceHorizontal(w, lipgloss.Center, m.theme.dim.Render(emptyMsg)), w)
				} else {
					tableLines[r] = strings.Repeat(" ", w)
				}
			}
		} else {
			total := len(m.shown)
			thumbSize := tRows * tRows / total
			if thumbSize < 1 {
				thumbSize = 1
			}
			thumbStart := m.offset * tRows / total
			if thumbStart+thumbSize > tRows {
				thumbStart = tRows - thumbSize
			}
			if thumbStart < 0 {
				thumbStart = 0
			}

			ellipsis := m.glyph.ellipsis

			for i := 0; i < tRows; i++ {
				shownIdx := m.offset + i
				var scrollCell string
				if hasScrollbar {
					if i >= thumbStart && i < thumbStart+thumbSize {
						scrollCell = m.theme.accent.Render(m.glyph.thumb)
					} else {
						scrollCell = m.theme.dim.Render(m.glyph.track)
					}
				}

				if shownIdx >= end {
					rowW := w
					if hasScrollbar {
						rowW = w - 1
					}
					tableLines[i] = padLine(strings.Repeat(" ", rowW)+scrollCell, w)
					continue
				}

				taskIdx := m.shown[shownIdx]
				t := m.tasks[taskIdx]
				isSelected := (shownIdx == m.cursor)
				isDone := (t.Status == "done")

				var cursorCell string
				if isSelected {
					cursorCell = m.theme.accent.Render(m.glyph.cursor)
				} else {
					cursorCell = " "
				}

				var statusGlyph string
				switch t.Status {
				case "pending":
					statusGlyph = m.glyph.pending
				case "leased":
					statusGlyph = m.glyph.leased
				case "done":
					statusGlyph = m.glyph.done
				case "buried":
					statusGlyph = m.glyph.buried
				default:
					statusGlyph = " "
				}
				var statusStyled string
				if isDone {
					statusStyled = m.theme.dim.Render(statusGlyph)
				} else if t.Status == "leased" {
					statusStyled = m.theme.warn.Render(statusGlyph)
				} else if t.Status == "pending" {
					statusStyled = m.theme.ok.Render(statusGlyph)
				} else {
					statusStyled = m.theme.dim.Render(statusGlyph)
				}

				priStr := strconv.Itoa(t.Priority)
				if len(priStr) > 1 {
					priStr = priStr[:1]
				}
				priStyled := m.theme.dim.Render(priStr)

				var scopeStyled string
				if wScope > 0 {
					sText := padRight(trunc(t.Project, wScope, ""), wScope)
					if isDone {
						scopeStyled = m.theme.dim.Render(sText)
					} else {
						scopeStyled = m.theme.scope.Render(sText)
					}
				}

				_, rawTitle := titleOf(t)
				titleText := padRight(trunc(rawTitle, wTitle, ellipsis), wTitle)
				var titleStyled string
				if isDone {
					titleStyled = m.theme.dim.Render(titleText)
				} else {
					titleStyled = titleText
				}

				var claimsStyled string
				if t.ClaimCount > 1 {
					cStr := padRight(fmt.Sprintf("%s %d", m.glyph.refresh, t.ClaimCount), wClaims)
					if isDone {
						claimsStyled = m.theme.dim.Render(cStr)
					} else {
						claimsStyled = m.theme.err.Render(cStr)
					}
				} else {
					claimsStyled = strings.Repeat(" ", wClaims)
				}

				var workerStyled string
				if wWorker > 0 {
					_, co := workerParts(t.Worker)
					if co != "" {
						wStr := padRight(trunc(m.glyph.branch+path.Base(co), wWorker, ""), wWorker)
						workerStyled = m.theme.dim.Render(wStr)
					} else {
						workerStyled = strings.Repeat(" ", wWorker)
					}
				}

				var barStyled string
				if t.Status == "leased" && m.stats.LeaseSeconds > 0 {
					rem := t.LeaseExpires - m.now.Unix()
					var filled int
					if rem > 0 {
						ratio := float64(rem) / float64(m.stats.LeaseSeconds)
						filled = int(math.Round(8.0 * ratio))
						if filled < 0 {
							filled = 0
						}
						if filled > 8 {
							filled = 8
						}
					}
					if isDone {
						barStyled = m.theme.dim.Render(strings.Repeat(m.glyph.barOff, 8))
					} else {
						barStyled = m.theme.accent.Render(strings.Repeat(m.glyph.barOn, filled)) + m.theme.dim.Render(strings.Repeat(m.glyph.barOff, 8-filled))
					}
				} else {
					barStyled = strings.Repeat(" ", wLease)
				}

				leftStr := leaseLeft(t, m.now)
				var leftStyled string
				if leftStr != "" {
					leftStyled = padRight(trunc(leftStr, wLeft, ""), wLeft)
					if isDone {
						leftStyled = m.theme.dim.Render(leftStyled)
					}
				} else {
					leftStyled = strings.Repeat(" ", wLeft)
				}

				id7 := t.ID
				if len(id7) > 7 {
					id7 = id7[:7]
				}
				idStyled := m.theme.dim.Render(padRight(id7, wID))

				var rowBody strings.Builder
				rowBody.WriteString(statusStyled)
				rowBody.WriteString(" ")
				rowBody.WriteString(priStyled)
				rowBody.WriteString(" ")
				if wScope > 0 {
					rowBody.WriteString(scopeStyled)
					rowBody.WriteString(" ")
				}
				rowBody.WriteString(titleStyled)
				if wClaims > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(claimsStyled)
				}
				if wWorker > 0 {
					rowBody.WriteString(" ")
					rowBody.WriteString(workerStyled)
				}
				rowBody.WriteString(" ")
				rowBody.WriteString(barStyled)
				rowBody.WriteString(" ")
				rowBody.WriteString(leftStyled)
				rowBody.WriteString(" ")
				rowBody.WriteString(idStyled)

				rowW := w - 1
				if hasScrollbar {
					rowW = w - 2
				}
				bodyStr := padLine(rowBody.String(), rowW)
				if isSelected {
					bodyStr = m.theme.selected.Render(bodyStr)
				}

				tableLines[i] = padLine(cursorCell+bodyStr+scrollCell, w)
			}
		}
	}

	// 7. Detail pane (detailRows() rows)
	ruleChar := m.glyph.rule

	detailLines := make([]string, 0, dRows)
	curTask, hasTask := m.selected()

	if !hasTask {
		detailLines = append(detailLines, padLine(m.theme.rule.Render(strings.Repeat(ruleChar, w)), w))
		for r := 1; r < dRows; r++ {
			detailLines = append(detailLines, strings.Repeat(" ", w))
		}
	} else {
		// Rule row
		id7 := curTask.ID
		idRest := ""
		if len(curTask.ID) > 7 {
			id7 = curTask.ID[:7]
			idRest = curTask.ID[7:]
		}
		leftPart := m.theme.rule.Render(ruleChar+ruleChar+" ") + m.theme.dim.Render(m.glyph.hash+" ") + m.theme.bold.Render(id7) + m.theme.dim.Render(idRest) + m.theme.rule.Render(" ")

		var stGlyph string
		switch curTask.Status {
		case "pending":
			stGlyph = m.glyph.pending
		case "leased":
			stGlyph = m.glyph.leased
		case "done":
			stGlyph = m.glyph.done
		case "buried":
			stGlyph = m.glyph.buried
		default:
			stGlyph = ""
		}
		stText := stGlyph + " " + curTask.Status
		var rightContent string
		if curTask.Status == "leased" {
			lStr := leaseLeft(curTask, m.now)
			if lStr != "" {
				rightContent = "[" + stText + "] [" + lStr + "] left"
			} else {
				rightContent = "[" + stText + "]"
			}
		} else {
			rightContent = "[" + stText + "]"
		}
		rightPart := m.theme.rule.Render(" ") + m.theme.dim.Render(rightContent) + m.theme.rule.Render(" "+ruleChar+ruleChar)

		lpw := lipgloss.Width(leftPart)
		rpw := lipgloss.Width(rightPart)
		var ruleLine string
		if lpw+rpw < w {
			fillCount := w - lpw - rpw
			ruleLine = leftPart + m.theme.rule.Render(strings.Repeat(ruleChar, fillCount)) + rightPart
		} else {
			ruleLine = padLine(leftPart+" "+rightPart, w)
		}
		detailLines = append(detailLines, padLine(ruleLine, w))

		// Scope + title line
		scope, title := titleOf(curTask)
		var l1 strings.Builder
		if scope != "" {
			l1.WriteString(m.theme.scope.Render(scope) + " ")
		}
		l1.WriteString(m.theme.bold.Render(title))
		detailLines = append(detailLines, padLine(l1.String(), w))

		// Chips line
		var chips []string
		if curTask.Project != "" {
			chips = append(chips, m.glyph.folder+" "+curTask.Project)
		}
		chips = append(chips, "p "+strconv.Itoa(curTask.Priority))
		h, co := workerParts(curTask.Worker)
		if h != "" {
			chips = append(chips, m.glyph.host+" "+h)
		}
		if co != "" {
			chips = append(chips, m.glyph.branch+" "+co)
		}
		if curTask.ClaimCount > 0 {
			cw := "claim"
			if curTask.ClaimCount != 1 {
				cw = "claims"
			}
			chips = append(chips, m.glyph.refresh+" "+strconv.Itoa(curTask.ClaimCount)+" "+cw)
		}
		chipsLine := m.theme.dim.Render(strings.Join(chips, "  "))
		detailLines = append(detailLines, padLine(chipsLine, w))

		// Blank line
		detailLines = append(detailLines, strings.Repeat(" ", w))

		// Viewport content
		vpContent := m.detail.View()
		var vpLines []string
		if vpContent != "" {
			vpLines = strings.Split(vpContent, "\n")
		}
		vpMax := dRows - 4
		for r := 0; r < vpMax; r++ {
			if r < len(vpLines) {
				detailLines = append(detailLines, padLine(vpLines[r], w))
			} else {
				detailLines = append(detailLines, strings.Repeat(" ", w))
			}
		}
	}

	// 8. Footer (1 row)
	pos := 0
	if len(m.shown) > 0 {
		pos = m.cursor + 1
	}
	posStr := fmt.Sprintf("%d/%d", pos, len(m.shown))
	footRight := m.theme.dim.Render(posStr)

	var footLeft string
	if m.msg != "" {
		if strings.HasPrefix(m.msg, "error: ") {
			footLeft = m.theme.err.Render(strings.TrimPrefix(m.msg, "error: "))
		} else {
			footLeft = m.theme.accent.Render(m.msg)
		}
	} else if m.mode == modeSearch {
		footLeft = m.theme.accent.Render("/") + m.query + m.theme.accent.Render(m.glyph.caret)
	} else if m.mode == modeDetail || m.mode == modeZoom {
		items := [][2]string{
			{"j/k", "scroll"},
			{"Tab", "back"},
			{"z", "zoom"},
			{"Y", "copy"},
			{"q", "quit"},
		}
		var parts []string
		for _, it := range items {
			parts = append(parts, m.theme.accent.Render(it[0])+" "+m.theme.dim.Render(it[1]))
		}
		footLeft = strings.Join(parts, "  ")
	} else {
		items := [][2]string{
			{"j/k", "move"},
			{"0-4", "filter"},
			{"p", "project"},
			{"n", "new"},
			{"e", "edit"},
			{"+/-", "pri"},
			{"D", "delete"},
			{"Y", "copy"},
			{"z", "zoom"},
			{"q", "quit"},
		}
		var parts []string
		for _, it := range items {
			parts = append(parts, m.theme.accent.Render(it[0])+" "+m.theme.dim.Render(it[1]))
		}
		footLeft = strings.Join(parts, "  ")
	}

	flw := lipgloss.Width(footLeft)
	frw := lipgloss.Width(footRight)
	var footerLine string
	if flw+frw+1 <= w {
		spaces := w - flw - frw
		footerLine = footLeft + strings.Repeat(" ", spaces) + footRight
	} else {
		footerLine = padLine(footLeft+" "+footRight, w)
	}

	// Frame assembly
	var allLines []string
	allLines = append(allLines, headerLine)
	allLines = append(allLines, tabLine)
	allLines = append(allLines, strings.Repeat(" ", w))

	if m.mode == modeZoom {
		allLines = append(allLines, detailLines...)
	} else {
		allLines = append(allLines, colHeadLine)
		allLines = append(allLines, tableLines...)
		allLines = append(allLines, strings.Repeat(" ", w))
		allLines = append(allLines, detailLines...)
	}
	allLines = append(allLines, footerLine)

	content := strings.Join(allLines, "\n")
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
