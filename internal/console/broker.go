// Package console is the HDS Server console: the server log with an "HDS Server>" command prompt, in the
// server's own terminal and over SSH.
package console

import (
	"bytes"
	"io"
	"sync"
)

const (
	keepLines         = 2000
	subscriberBacklog = 512
)

// Broker collects the server log. It keeps the latest lines for screens that open later and passes each
// new line to every open screen. When no local console has the terminal, it also echoes there, as the
// server always printed before.
type Broker struct {
	mu          sync.Mutex
	lines       []string
	partial     []byte
	subscribers map[chan string]struct{}
	echo        io.Writer
}

// NewBroker echoes to w (normally os.Stdout) until a local console takes the terminal.
func NewBroker(w io.Writer) *Broker {
	return &Broker{subscribers: make(map[chan string]struct{}), echo: w}
}

// Write takes log output; complete lines are kept and passed on.
func (b *Broker) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.echo != nil {
		_, _ = b.echo.Write(p)
	}
	b.partial = append(b.partial, p...)
	for {
		end := bytes.IndexByte(b.partial, '\n')
		if end < 0 {
			break
		}
		line := string(bytes.TrimRight(b.partial[:end], "\r"))
		b.partial = b.partial[end+1:]
		b.lines = append(b.lines, line)
		if len(b.lines) > keepLines {
			b.lines = b.lines[len(b.lines)-keepLines:]
		}
		for subscriber := range b.subscribers {
			select {
			case subscriber <- line:
			default: // a screen that can't keep up misses lines rather than stalling the server
			}
		}
	}
	return len(p), nil
}

// Subscribe returns the kept lines and a channel of the lines that follow. Call cancel when done.
func (b *Broker) Subscribe() (backlog []string, lines <-chan string, cancel func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := make(chan string, subscriberBacklog)
	b.subscribers[channel] = struct{}{}
	backlog = append([]string(nil), b.lines...)
	return backlog, channel, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subscribers[channel]; ok {
			delete(b.subscribers, channel)
			close(channel)
		}
	}
}

// SetEcho changes where log output is echoed: nil while a local console owns the terminal.
func (b *Broker) SetEcho(w io.Writer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.echo = w
}
