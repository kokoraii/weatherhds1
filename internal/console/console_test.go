package console

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"weatherhds/internal/control"
	"weatherhds/internal/events"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// lockedBuffer collects what the console draws.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) contains(text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.buf.String(), text)
}

func waitFor(t *testing.T, out *lockedBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if out.contains(text) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	out.mu.Lock()
	defer out.mu.Unlock()
	t.Fatalf("never saw %q; screen so far:\n%s", text, out.buf.String())
}

func TestSSHConsoleLetsListedKeysInAndRunsCommands(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip("no current user:", err)
	}
	username := me.Username
	if i := strings.LastIndexAny(username, `\`); i >= 0 { // Windows: DOMAIN\name
		username = username[i+1:]
	}

	dir := t.TempDir()
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := gossh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keys := filepath.Join(dir, "authorized_keys")
	writeFile(t, keys, string(gossh.MarshalAuthorizedKey(signer.PublicKey())))
	writeFile(t, filepath.Join(dir, "sshd_config"), "AuthorizedKeysFile "+filepath.ToSlash(keys)+"\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := events.NewHub("test", func(string, ...any) {})
	var logs lockedBuffer
	broker := NewBroker(nil)
	port := freePort(t)
	Start(ctx, Options{
		Dispatcher: control.NewDispatcher(hub, func(string, ...any) {}),
		Broker:     broker,
		Sources:    func(string) []string { return nil },
		Status:     func() Status { return Status{Version: "test"} },
		Stop:       func() {},
		Log: func(format string, args ...any) {
			_, _ = io.WriteString(&logs, format+"\n")
		},
		SSHPort:     port,
		SSHDConfig:  filepath.Join(dir, "sshd_config"),
		HostKeyPath: filepath.Join(dir, "host_ed25519_key"),
	})

	dial := func(signer gossh.Signer) (*gossh.Client, error) {
		return gossh.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &gossh.ClientConfig{
			User:            username,
			Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
	}

	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	strangerSigner, _ := gossh.NewSignerFromKey(stranger)
	if client, err := dial(strangerSigner); err == nil {
		client.Close()
		t.Fatal("an unlisted key got in")
	} else if !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("expected an authentication failure, got %v", err)
	}

	client, err := dial(signer)
	if err != nil {
		t.Fatalf("a listed key was refused: %v", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.RequestPty("xterm-256color", 30, 120, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	var screen lockedBuffer
	session.Stdout = &screen
	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Shell(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, &screen, "HDS Server test")
	_, _ = stdin.Write([]byte("/"))
	waitFor(t, &screen, promptText)
	_, _ = stdin.Write([]byte("help\r"))
	waitFor(t, &screen, "reload refresh")
}
