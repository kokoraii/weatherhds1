package console

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	promptText   = "HDS Server> "
	footerHeight = 2
	maxHistory   = 200
)

// Status is the bar across the top of the console.
type Status struct {
	Version string
	Uptime  time.Duration
	Feeds   []string // connected pages, by feed
	Track   string   // music playing, or ""
}

type styles struct {
	header, hint, suggestion, echo, output, failure lipgloss.Style
}

// newStyles is the console's look. Bubble Tea fits the colours to each terminal (local or SSH) itself.
func newStyles() styles {
	return styles{
		header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#1f5a8a")).Padding(0, 1),
		hint:       lipgloss.NewStyle().Foreground(lipgloss.Color("#8a96a3")),
		suggestion: lipgloss.NewStyle().Foreground(lipgloss.Color("#7fc4ff")),
		echo:       lipgloss.NewStyle().Foreground(lipgloss.Color("#7fc4ff")),
		output:     lipgloss.NewStyle().Foreground(lipgloss.Color("#d8dee6")),
		failure:    lipgloss.NewStyle().Foreground(lipgloss.Color("#ff7a6b")),
	}
}

// screen is one console: the log with a prompt that opens on "/". The server's terminal and each SSH
// session get their own.
type screen struct {
	console  *Console
	who      string // for the log: "console" or "ssh:rai@192.168.2.10"
	local    bool   // the server's own terminal, rather than an SSH session
	quit     func() // Ctrl+C: stops the server in its own terminal, ends an SSH session
	quitHint string
	styles   styles

	lines       []string
	logs        <-chan string
	unsubscribe func()
	view        viewport.Model
	input       textinput.Model
	prompting   bool
	suggestions []string
	history     []string
	historyAt   int
	status      Status
	width       int
}

type logLine string
type statusTick struct{}
type commandDone struct {
	output []string
	err    error
	clear  bool
	exit   bool
}

func newScreen(c *Console, who string, local bool, quit func(), quitHint string) *screen {
	backlog, logs, unsubscribe := c.opts.Broker.Subscribe()
	input := textinput.New()
	input.Prompt = promptText
	inputStyles := input.Styles()
	inputStyles.Cursor.Blink = false // nothing repaints while idle
	input.SetStyles(inputStyles)
	s := &screen{
		console:     c,
		who:         who,
		local:       local,
		quit:        quit,
		quitHint:    quitHint,
		styles:      newStyles(),
		lines:       backlog,
		logs:        logs,
		unsubscribe: unsubscribe,
		view:        viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		input:       input,
		status:      c.opts.Status(),
	}
	return s
}

func (s *screen) Init() tea.Cmd {
	return tea.Batch(s.waitForLog(), tickStatus())
}

func (s *screen) waitForLog() tea.Cmd {
	return func() tea.Msg {
		line, ok := <-s.logs
		if !ok {
			return nil
		}
		return logLine(line)
	}
}

func tickStatus() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return statusTick{} })
}

func (s *screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.view.SetWidth(msg.Width)
		s.view.SetHeight(max(1, msg.Height-1-footerHeight))
		s.input.SetWidth(max(10, msg.Width-len(promptText)-1))
		s.refresh(true)
		return s, nil
	case logLine:
		s.append(string(msg))
		return s, s.waitForLog()
	case statusTick:
		s.status = s.console.opts.Status()
		return s, tickStatus()
	case commandDone:
		if msg.exit {
			s.unsubscribe()
			return s, tea.Quit
		}
		if msg.clear {
			s.lines = nil
			s.refresh(true)
		}
		for _, line := range msg.output {
			s.append(s.styles.output.Render(line))
		}
		if msg.err != nil {
			s.append(s.styles.failure.Render("  " + msg.err.Error()))
		}
		return s, nil
	case tea.KeyPressMsg:
		return s.key(msg)
	}
	return s, nil
}

func (s *screen) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		s.unsubscribe()
		s.quit()
		return s, tea.Quit
	}
	if !s.prompting {
		if msg.String() == "/" {
			s.prompting = true
			s.input.SetValue("")
			s.historyAt = len(s.history)
			s.suggest()
			return s, s.input.Focus()
		}
		var cmd tea.Cmd
		s.view, cmd = s.view.Update(msg) // ↑ ↓ PgUp PgDn scroll the log
		return s, cmd
	}

	switch msg.String() {
	case "esc":
		s.closePrompt()
		return s, nil
	case "enter":
		line := strings.TrimSpace(s.input.Value())
		s.closePrompt()
		if line == "" {
			return s, nil
		}
		s.remember(line)
		s.append(s.styles.echo.Render(promptText + line))
		return s, s.run(line)
	case "tab":
		s.complete()
		return s, nil
	case "up", "down":
		s.browseHistory(msg.String() == "up")
		return s, nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	s.suggest()
	return s, cmd
}

func (s *screen) run(line string) tea.Cmd {
	return func() tea.Msg {
		output, err := s.console.execute(s.who, line)
		return commandDone{
			output: output,
			err:    err,
			clear:  line == "clear",
			exit:   line == "exit" && !s.local,
		}
	}
}

func (s *screen) closePrompt() {
	s.prompting = false
	s.suggestions = nil
	s.input.Blur()
}

// complete fills in what Tab can: the single candidate, or what all candidates share.
func (s *screen) complete() {
	text := s.input.Value()
	candidates, start := Complete(text, s.console.opts.Sources)
	if len(candidates) == 0 {
		return
	}
	fill := CommonPrefix(candidates)
	if len(candidates) == 1 && !strings.HasSuffix(fill, "=") {
		fill += " "
	}
	if len(fill) >= len(text)-start {
		s.input.SetValue(text[:start] + fill)
		s.input.CursorEnd()
	}
	s.suggest()
}

func (s *screen) suggest() {
	s.suggestions, _ = Complete(s.input.Value(), s.console.opts.Sources)
}

func (s *screen) remember(line string) {
	if len(s.history) == 0 || s.history[len(s.history)-1] != line {
		s.history = append(s.history, line)
	}
	if len(s.history) > maxHistory {
		s.history = s.history[len(s.history)-maxHistory:]
	}
}

func (s *screen) browseHistory(back bool) {
	if back && s.historyAt > 0 {
		s.historyAt--
	} else if !back && s.historyAt < len(s.history) {
		s.historyAt++
	}
	if s.historyAt < len(s.history) {
		s.input.SetValue(s.history[s.historyAt])
	} else {
		s.input.SetValue("")
	}
	s.input.CursorEnd()
	s.suggest()
}

func (s *screen) append(line string) {
	follow := s.view.AtBottom()
	s.lines = append(s.lines, line)
	if len(s.lines) > keepLines {
		s.lines = s.lines[len(s.lines)-keepLines:]
	}
	s.refresh(follow)
}

// refresh redraws the log, staying at the bottom if the user hadn't scrolled up.
func (s *screen) refresh(follow bool) {
	s.view.SetContent(strings.Join(s.lines, "\n"))
	if follow {
		s.view.GotoBottom()
	}
}

func (s *screen) View() tea.View {
	st := s.status
	feeds := "none"
	if len(st.Feeds) > 0 {
		feeds = strings.Join(st.Feeds, ", ")
	}
	header := fmt.Sprintf("HDS Server %s · up %s · pages: %s", st.Version, formatUptime(st.Uptime), feeds)
	if st.Track != "" {
		header += " · ♪ " + st.Track
	}
	header = s.styles.header.Width(s.width).MaxHeight(1).Render(header)

	var footer string
	if s.prompting {
		footer = s.input.View() + "\n" + s.styles.suggestion.MaxWidth(s.width).Render(strings.Join(s.suggestions, "  "))
	} else {
		footer = s.styles.hint.Render("/ for commands · ↑ ↓ PgUp PgDn scroll · "+s.quitHint) + "\n"
	}
	view := tea.NewView(header + "\n" + s.view.View() + "\n" + footer)
	view.AltScreen = true
	return view
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, int(d.Hours())%24)
	}
	return d.String()
}
