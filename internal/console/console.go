package console

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/mattn/go-isatty"

	"weatherhds/internal/control"
	"weatherhds/internal/events"
)

// Options wires the console to the server.
type Options struct {
	Dispatcher *control.Dispatcher
	Broker     *Broker
	Sources    Sources
	Status     func() Status
	Stop       func() // stops the server: Ctrl+C in its own terminal
	Log        events.Logger

	SSHPort        int
	SSHDConfig     string   // the system's sshd_config, for who may log in
	SystemHostKeys []string // the machine's sshd host keys, used if readable so the fingerprint matches
	HostKeyPath    string   // otherwise a key generated here
}

// Console serves the HDS Server console in the server's terminal and over SSH.
type Console struct {
	opts Options
	auth *Authorizer
}

// Start opens the console in the server's terminal if it has one, and starts the SSH server. Both stop
// with ctx.
func Start(ctx context.Context, opts Options) *Console {
	c := &Console{opts: opts, auth: &Authorizer{ConfigPath: opts.SSHDConfig}}
	if isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd()) {
		c.runLocal(ctx)
	}
	if err := c.serveSSH(ctx); err != nil {
		opts.Log("SSH console disabled: %v", err)
	}
	return c
}

func (c *Console) runLocal(ctx context.Context) {
	c.opts.Broker.SetEcho(nil)
	scr := newScreen(c, "console", true, c.opts.Stop, "Ctrl+C stops the server", lipgloss.DefaultRenderer())
	program := tea.NewProgram(scr, tea.WithAltScreen(), tea.WithContext(ctx))
	go func() {
		_, err := program.Run()
		c.opts.Broker.SetEcho(os.Stdout) // shutdown messages print to the terminal again
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			c.opts.Log("console closed: %v", err)
		}
	}()
}

func (c *Console) serveSSH(ctx context.Context) error {
	hostKey, err := c.hostKeyOption()
	if err != nil {
		return err
	}
	server, err := wish.NewServer(
		hostKey,
		wish.WithPublicKeyAuth(func(sctx ssh.Context, key ssh.PublicKey) bool {
			allowed, reason := c.auth.Allows(sctx.User(), remoteIP(sctx.RemoteAddr()), key)
			if !allowed && sctx.Value(rejectionLogged{}) == nil {
				sctx.SetValue(rejectionLogged{}, true)
				c.opts.Log("SSH login refused for %s from %s: %s", sctx.User(), sctx.RemoteAddr(), reason)
			}
			return allowed
		}),
		wish.WithIdleTimeout(30*time.Minute),
		wish.WithMiddleware(bm.Middleware(c.sshScreen), activeterm.Middleware()),
	)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(c.opts.SSHPort))
	if err != nil {
		return err
	}
	c.opts.Log("SSH console listening on port %d", c.opts.SSHPort)
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			c.opts.Log("SSH console stopped: %v", err)
		}
	}()
	return nil
}

type rejectionLogged struct{}

// hostKeyOption uses the machine's own sshd host key when this process can read it, so clients see the
// fingerprint they already trust, else a key generated in HostKeyPath.
func (c *Console) hostKeyOption() (ssh.Option, error) {
	for _, path := range c.opts.SystemHostKeys {
		if pem, err := os.ReadFile(path); err == nil {
			return wish.WithHostKeyPEM(pem), nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.opts.HostKeyPath), 0o700); err != nil {
		return nil, err
	}
	return wish.WithHostKeyPath(c.opts.HostKeyPath), nil
}

func (c *Console) sshScreen(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
	who := "ssh:" + sess.User() + "@" + remoteIP(sess.RemoteAddr())
	c.opts.Log("SSH console opened by %s", who)
	scr := newScreen(c, who, false, func() {}, "Ctrl+C or exit disconnects", bm.MakeRenderer(sess))
	go func() {
		<-sess.Context().Done()
		scr.unsubscribe()
		c.opts.Log("SSH console closed by %s", who)
	}()
	return scr, []tea.ProgramOption{tea.WithAltScreen()}
}

// execute carries out one line typed at the prompt and returns what to show for it. Commands sent to
// the pages report through the server log, which every screen shows, so they return nothing here.
func (c *Console) execute(who, text string) ([]string, error) {
	line, err := ParseLine(text)
	if err != nil {
		return nil, err
	}
	if len(line.Words) == 0 {
		return nil, nil
	}
	switch line.Words[0] {
	case "help":
		return help(line.Words[1:]), nil
	case "feeds":
		return c.feeds(), nil
	case "clear":
		return nil, nil
	case "exit":
		if who == "console" {
			return nil, errors.New("Ctrl+C stops the server; exit only ends an SSH session")
		}
		return nil, nil
	}
	request, err := line.Request()
	if err != nil {
		return nil, err
	}
	request.From = who
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = c.opts.Dispatcher.Run(ctx, request)
	return nil, err
}

func help(words []string) []string {
	out := []string{"  [@feed] <type> <command> [target] [key=value …]   Tab completes, ↑ ↓ history, Esc closes"}
	for _, spec := range control.Specs() {
		if len(words) > 0 && spec.Type != words[0] {
			continue
		}
		out = append(out, fmt.Sprintf("  %-52s %s", spec.Usage(), spec.Help))
	}
	if len(words) == 0 {
		out = append(out,
			fmt.Sprintf("  %-52s %s", "feeds", "List the connected pages."),
			fmt.Sprintf("  %-52s %s", "clear", "Clear this screen."),
			fmt.Sprintf("  %-52s %s", "exit", "End an SSH session."),
		)
	}
	return out
}

func (c *Console) feeds() []string {
	pages := c.opts.Dispatcher.Hub().Pages()
	if len(pages) == 0 {
		return []string{"  no pages connected"}
	}
	out := make([]string, 0, len(pages))
	for _, page := range pages {
		state := strings.TrimSpace(string(page.State))
		if state == "" || state == "null" || state == "{}" {
			state = ""
		}
		out = append(out, fmt.Sprintf("  %-8s %-24s since %s  %s", page.Feed, page.Addr, page.Since.Format("Jan 2 15:04:05"), state))
	}
	return out
}

func remoteIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
