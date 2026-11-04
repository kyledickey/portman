package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/kdicusb/portman/internal/config"
	"github.com/kdicusb/portman/internal/ui"
)

var configCmd = &cli.Command{
	Name:  "config",
	Usage: "edit settings in the terminal",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		dir := cmd.String("home")
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return ui.Hint(errors.New("pm config needs a terminal"), "edit %s directly instead", config.File(dir))
		}
		c, err := config.Load(dir)
		if err != nil {
			return err
		}
		m := newSettings(dir, c)
		if _, err := tea.NewProgram(m, tea.WithContext(ctx)).Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			return err
		}
		if m.err != nil {
			return m.err
		}
		if m.changed {
			ui.Success(cmd.Root().Writer, "saved "+config.File(dir))
		}
		return nil
	},
}

// settings wraps the huh form so each field saves when you leave it, the way
// pm add saves at once. Inputs update their value on every keystroke, so
// saving any sooner would push half-typed values to the running portman.
type settings struct {
	form    *huh.Form
	dir     string
	c       *config.Config
	port    *string
	saved   config.Config
	hints   map[huh.Field]string
	focused huh.Field
	last    huh.Field
	changed bool
	err     error
}

func newSettings(dir string, c config.Config) *settings {
	s := &settings{dir: dir, c: &c, saved: c, hints: map[huh.Field]string{}}
	port := strconv.Itoa(c.Port)
	s.port = &port
	row := func(hint string, f huh.Field) huh.Field {
		s.hints[f] = hint
		return f
	}
	label := func(s string) string { return fmt.Sprintf("%-11s", s) }

	fields := []huh.Field{
		row("Apps live at NAME.domain. localhost needs no DNS setup.",
			huh.NewInput().Title(label("Domain")).Prompt("").Inline(true).
				Value(&s.c.Domain).Validate(check(config.ParseDomain))),
		row("Where portman listens. 80 keeps URLs free of a port.",
			huh.NewInput().Title(label("Port")).Prompt("").Inline(true).
				Value(s.port).Validate(check(config.ParsePort))),
		row("Listen on every network interface, so another machine can forward here.",
			huh.NewConfirm().Title(label("Share")).Affirmative("on").Negative("off").Inline(true).
				WithButtonAlignment(lipgloss.Left).
				Value(&s.c.Share)),
		row("Send names with no local app to portman on this host. Leave empty to turn off.",
			huh.NewInput().Title(label("Forward")).Prompt("").Placeholder("off").Inline(true).
				Value(&s.c.Forward).Validate(func(v string) error {
				if v == "" {
					return nil
				}
				_, err := config.ParseForward(v)
				return err
			})),
		row("How much portman writes to its log. See it with pm logs.",
			// huh puts a select's title on its own line, so the label rides in each option.
			huh.NewSelect[string]().Inline(true).
				Options(levelOptions(label("Log level"))...).
				Value(&s.c.LogLevel)),
	}
	s.last = fields[len(fields)-1]

	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	keys.Input.Next = key.NewBinding(key.WithKeys("enter", "tab", "down"))
	keys.Input.Prev = key.NewBinding(key.WithKeys("shift+tab", "up"))
	keys.Confirm.Next = keys.Input.Next
	keys.Confirm.Prev = keys.Input.Prev
	keys.Select.Next = keys.Input.Next
	keys.Select.Prev = keys.Input.Prev
	keys.Select.Up = key.NewBinding(key.WithDisabled())
	keys.Select.Down = key.NewBinding(key.WithDisabled())

	s.form = huh.NewForm(huh.NewGroup(fields...)).
		WithTheme(ui.Theme).WithKeyMap(keys).WithShowHelp(false)
	return s
}

func (s *settings) Init() tea.Cmd { return s.form.Init() }

func (s *settings) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Down on the last row would submit and close the form.
	if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "down" && s.form.GetFocusedField() == s.last {
		return s, nil
	}
	m, cmd := s.form.Update(msg)
	s.form = m.(*huh.Form)
	if f := s.form.GetFocusedField(); f != s.focused || s.form.State != huh.StateNormal {
		s.focused = f
		s.save()
	}
	if s.form.State != huh.StateNormal {
		return s, tea.Quit
	}
	return s, cmd
}

// save writes the form's values if they all parse and differ from the file.
func (s *settings) save() {
	n := *s.c
	var err error
	if n.Domain, err = config.ParseDomain(n.Domain); err != nil {
		return
	}
	if n.Port, err = config.ParsePort(*s.port); err != nil {
		return
	}
	if n.Forward != "" {
		if n.Forward, err = config.ParseForward(n.Forward); err != nil {
			return
		}
	}
	if n.Domain == s.saved.Domain && n.Port == s.saved.Port && n.Share == s.saved.Share &&
		n.Forward == s.saved.Forward && n.LogLevel == s.saved.LogLevel {
		return
	}
	if s.err = config.Save(s.dir, n); s.err == nil {
		s.saved, s.changed = n, true
	}
}

func (s *settings) View() tea.View {
	status := ui.Faint("changes save as you leave each row")
	if s.changed {
		status = ui.Accent("✓") + ui.Faint(" saved")
	}
	return tea.NewView("\n  " + ui.Accent("portman settings") + "\n\n" +
		s.form.View() + "\n\n" +
		"  " + ui.Faint(s.hints[s.form.GetFocusedField()]) + "\n" +
		"  " + ui.Faint("↑↓ move · ←→ change · esc done") + "   " + status + "\n")
}

func levelOptions(label string) []huh.Option[string] {
	var opts []huh.Option[string]
	for _, l := range []string{"debug", "info", "warn", "error"} {
		opts = append(opts, huh.NewOption(label+l, l))
	}
	return opts
}

// check adapts a config parser into a form validator.
func check[T any](parse func(string) (T, error)) func(string) error {
	return func(s string) error {
		_, err := parse(s)
		return err
	}
}
