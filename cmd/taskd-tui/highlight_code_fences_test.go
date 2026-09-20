package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestHighlightCodeFences(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	th := m.theme

	t.Run("PreservesFencesWithoutEmptyArtifacts", func(t *testing.T) {
		body := "Why: explanation\n```sh\necho \"hello world\"\n```\nDone when: test passes"
		rendered := m.renderBody(task{Body: body})
		plain := ansi.Strip(rendered)

		if !strings.Contains(plain, "```sh") {
			t.Fatalf("expected code fence ```sh preserved in rendered body, got:\n%s", plain)
		}
		if !strings.Contains(plain, "echo \"hello world\"") {
			t.Fatalf("expected code block content preserved, got:\n%s", plain)
		}
		lines := strings.Split(plain, "\n")
		var foundClosingFence bool
		for _, l := range lines {
			if strings.TrimSpace(l) == "```" {
				foundClosingFence = true
				break
			}
		}
		if !foundClosingFence {
			t.Fatalf("expected closing fence ``` preserved, got:\n%s", plain)
		}
		if strings.Contains(plain, " `` ") || strings.Contains(plain, "``\n") {
			t.Fatalf("unexpected empty inline backtick artifact in rendered body:\n%s", plain)
		}
	})

	t.Run("PreservesFencesWithBackticksInside", func(t *testing.T) {
		body := "Struct definition:\n```go\ntype Item struct {\n\tName string `json:\"name\"`\n}\n```\nProse after with `inline_var` here."
		rendered := m.renderBody(task{Body: body})
		plain := ansi.Strip(rendered)

		if !strings.Contains(plain, "```go") {
			t.Fatalf("expected code fence ```go preserved, got:\n%s", plain)
		}
		if !strings.Contains(plain, "`json:\"name\"`") {
			t.Fatalf("expected backtick tag preserved inside code fence, got:\n%s", plain)
		}
		if !strings.Contains(plain, "inline_var") {
			t.Fatalf("expected inline_var preserved, got:\n%s", plain)
		}

		codeStyledInline := th.code.Render("inline_var")
		if !strings.Contains(rendered, codeStyledInline) {
			t.Fatalf("expected inline_var to be styled with code style, got:\n%s", rendered)
		}

		proseSnippet := "Prose after with "
		if strings.Contains(rendered, th.code.Render(proseSnippet)) {
			t.Fatalf("prose should not be styled with code style (parity inverted):\n%s", rendered)
		}
	})

	t.Run("MultipleFencesNoInvertedParity", func(t *testing.T) {
		body := "First:\n```sh\ncmd1\n```\nMiddle text with `sample` inline.\n```sh\ncmd2\n```\nTrailing prose."
		rendered := m.renderBody(task{Body: body})
		plain := ansi.Strip(rendered)

		if !strings.Contains(plain, "cmd1") || !strings.Contains(plain, "cmd2") {
			t.Fatalf("expected both command blocks preserved, got:\n%s", plain)
		}
		if strings.Count(plain, "```sh") != 2 {
			t.Fatalf("expected two ```sh fences, got count %d in:\n%s", strings.Count(plain, "```sh"), plain)
		}

		middleProse := "Middle text with "
		if strings.Contains(rendered, th.code.Render(middleProse)) {
			t.Fatalf("middle prose was inverted and highlighted as code:\n%s", rendered)
		}
		trailingProse := "Trailing prose."
		if strings.Contains(rendered, th.code.Render(trailingProse)) {
			t.Fatalf("trailing prose was inverted and highlighted as code:\n%s", rendered)
		}
		sampleStyled := th.code.Render("sample")
		if !strings.Contains(rendered, sampleStyled) {
			t.Fatalf("expected inline code `sample` to be styled:\n%s", rendered)
		}
	})

	t.Run("InlineTripleBackticksNotBlockFence", func(t *testing.T) {
		body := "Use ``` in inline prose.\nNext line."
		rendered := m.renderBody(task{Body: body})
		plain := ansi.Strip(rendered)
		if !strings.Contains(plain, "Use ``` in inline prose.") {
			t.Fatalf("expected inline backticks preserved without becoming block fence:\n%s", plain)
		}
		if !strings.Contains(plain, "Next line.") {
			t.Fatalf("expected next line preserved:\n%s", plain)
		}
	})
}

func TestHighlightCodeFencesTabIndent(t *testing.T) {
	m := newModel(config{refresh: time.Hour}, nil)
	body := "prose\n\t```\nnot a block fence\n\t```"
	rendered := m.renderBody(task{Body: body})
	if strings.Contains(rendered, m.theme.code.Render("not a block fence")) {
		t.Fatalf("expected tab-indented block not to be highlighted as fence code:\n%s", rendered)
	}
}
