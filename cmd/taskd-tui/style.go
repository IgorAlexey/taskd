package main

import (
	"charm.land/lipgloss/v2"
)

type glyphs struct {
	pending  string
	leased   string
	done     string
	buried   string
	refresh  string
	branch   string
	host     string
	server   string
	dot      string
	db       string
	folder   string
	hash     string
	pillL    string
	pillR    string
	cursor   string
	barOn    string
	barOff   string
	track    string
	thumb    string
	ellipsis string
	rule     string
	caret    string
	sort     string
}

var nerdGlyphs = glyphs{
	pending:  "\uf10c",
	leased:   "\uf023",
	done:     "\uf00c",
	buried:   "\uf1c6",
	refresh:  "\uf021",
	branch:   "\ue0a0",
	host:     "\uf109",
	server:   "\uf233",
	dot:      "\uf111",
	db:       "\uf1c0",
	folder:   "\uf07b",
	hash:     "\uf292",
	pillL:    "\ue0b6",
	pillR:    "\ue0b4",
	cursor:   "▎",
	barOn:    "━",
	barOff:   "━",
	track:    "│",
	thumb:    "┃",
	ellipsis: "…",
	rule:     "─",
	caret:    "▏",
	sort:     "▼",
}

var asciiGlyphs = glyphs{
	pending:  "o",
	leased:   "*",
	done:     "v",
	buried:   "_",
	refresh:  "~",
	branch:   "&",
	host:     "@",
	server:   "#",
	dot:      "*",
	db:       "db",
	folder:   "/",
	hash:     "#",
	pillL:    "[",
	pillR:    "]",
	cursor:   ">",
	barOn:    "=",
	barOff:   "-",
	track:    "|",
	thumb:    "#",
	ellipsis: "...",
	rule:     "-",
	caret:    "_",
	sort:     "*",
}

type theme struct {
	accent     lipgloss.Style
	accentPill lipgloss.Style
	dim        lipgloss.Style
	scope      lipgloss.Style
	ok         lipgloss.Style
	err        lipgloss.Style
	warn       lipgloss.Style
	bold       lipgloss.Style
	selected   lipgloss.Style
	code       lipgloss.Style
	rule       lipgloss.Style
	tabActive  lipgloss.Style
	tabKey     lipgloss.Style
	heading    lipgloss.Style
}

func newTheme(dark bool) theme {
	if dark {
		accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5A54B"))
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		return theme{
			accent:     accent,
			accentPill: lipgloss.NewStyle().Background(lipgloss.Color("#E5A54B")).Foreground(lipgloss.Color("#1E1E1E")).Bold(true),
			dim:        dim,
			scope:      lipgloss.NewStyle().Foreground(lipgloss.Color("#8FA3BF")),
			ok:         lipgloss.NewStyle().Foreground(lipgloss.Color("#7BC275")),
			err:        lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75")),
			warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("#E5A54B")),
			bold:       lipgloss.NewStyle().Bold(true),
			selected:   lipgloss.NewStyle().Background(lipgloss.Color("#2A2F3A")),
			code:       lipgloss.NewStyle().Background(lipgloss.Color("#2A2F3A")),
			rule:       dim,
			tabActive:  lipgloss.NewStyle().Background(lipgloss.Color("#E5A54B")).Foreground(lipgloss.Color("#1E1E1E")).Bold(true),
			tabKey:     accent,
			heading:    dim,
		}
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#B45309"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563"))
	return theme{
		accent:     accent,
		accentPill: lipgloss.NewStyle().Background(lipgloss.Color("#B45309")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		dim:        dim,
		scope:      lipgloss.NewStyle().Foreground(lipgloss.Color("#2B6CB0")),
		ok:         lipgloss.NewStyle().Foreground(lipgloss.Color("#15803D")),
		err:        lipgloss.NewStyle().Foreground(lipgloss.Color("#DC2626")),
		warn:       lipgloss.NewStyle().Foreground(lipgloss.Color("#B45309")),
		bold:       lipgloss.NewStyle().Bold(true),
		selected:   lipgloss.NewStyle().Background(lipgloss.Color("#E5E7EB")),
		code:       lipgloss.NewStyle().Background(lipgloss.Color("#E5E7EB")).Foreground(lipgloss.Color("#1F2937")),
		rule:       dim,
		tabActive:  lipgloss.NewStyle().Background(lipgloss.Color("#B45309")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		tabKey:     accent,
		heading:    dim,
	}
}
