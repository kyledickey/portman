// Package ui holds portman's look: one teal accent, one red, everything else
// plain or faint.
package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

var (
	teal    = lipgloss.Color("#14B8A6")
	crimson = lipgloss.Color("#EF4444")

	accent = lipgloss.NewStyle().Foreground(teal)
	red    = lipgloss.NewStyle().Foreground(crimson)
	faint  = lipgloss.NewStyle().Faint(true)
)

func Faint(s string) string  { return faint.Render(s) }
func Accent(s string) string { return accent.Render(s) }
func Red(s string) string    { return red.Render(s) }

// Up and Down are the status dots used wherever something can be reachable.
func Up(s string) string   { return accent.Render("●") + " " + s }
func Down(s string) string { return red.Render("○") + " " + faint.Render(s) }

// Line prints s at the indent every portman line shares.
func Line(w io.Writer, s string)      { lipgloss.Fprintln(w, "  "+s) }
func Success(w io.Writer, msg string) { Line(w, accent.Render("✓")+" "+msg) }
func Info(w io.Writer, msg string)    { Line(w, faint.Render(msg)) }

// Error prints err and, when it carries one, the hint for fixing it.
func Error(w io.Writer, err error) {
	Line(w, red.Render("✗ "+err.Error()))
	var h hinted
	if errors.As(err, &h) {
		lipgloss.Fprintln(w, "    "+faint.Render(h.hint))
	}
}

type hinted struct {
	error
	hint string
}

func (h hinted) Unwrap() error { return h.error }

// Hint attaches the next step a user should take to err.
func Hint(err error, format string, args ...any) error {
	return hinted{err, fmt.Sprintf(format, args...)}
}

// Table prints rows under a faint header, indented to match every other line.
func Table(w io.Writer, header []string, rows [][]string) {
	t := table.New().
		Headers(header...).
		Rows(rows...).
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		BorderHeader(false).BorderColumn(false).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().PaddingRight(3)
			if row == table.HeaderRow {
				return s.Faint(true)
			}
			if col == 0 {
				return s.Bold(true)
			}
			return s
		})
	Line(w, strings.ReplaceAll(t.String(), "\n", "\n  "))
}

// Theme styles huh forms as a compact list: one row per field, the focused
// row marked in the accent, no borders or button blocks.
var Theme = huh.ThemeFunc(func(isDark bool) *huh.Styles {
	t := huh.ThemeBase(isDark)
	t.FieldSeparator = lipgloss.NewStyle().SetString("\n")
	t.Focused.Base = lipgloss.NewStyle().PaddingLeft(1).
		BorderStyle(lipgloss.Border{Left: "▸"}).BorderLeft(true).BorderForeground(teal)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle().Bold(true)
	t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(crimson).SetString(" ✗")
	t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(crimson)
	t.Focused.SelectSelector = lipgloss.NewStyle()
	t.Focused.SelectedOption = accent
	t.Focused.Option = accent
	t.Focused.NextIndicator = faint.MarginLeft(1).SetString("←→")
	t.Focused.PrevIndicator = lipgloss.NewStyle()
	t.Focused.FocusedButton = accent.Bold(true).MarginRight(1)
	t.Focused.BlurredButton = faint.MarginRight(1)
	t.Focused.TextInput.Cursor = accent
	t.Focused.TextInput.Text = accent
	t.Focused.TextInput.Placeholder = faint

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.Title = lipgloss.NewStyle()
	t.Blurred.Option = lipgloss.NewStyle()
	t.Blurred.SelectedOption = lipgloss.NewStyle()
	t.Blurred.FocusedButton = lipgloss.NewStyle().MarginRight(1)
	t.Blurred.TextInput.Text = lipgloss.NewStyle()
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()
	return t
})
