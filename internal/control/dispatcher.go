package control

import (
	"context"
	"fmt"
	"strings"

	"weatherhds/internal/events"
)

// LocalHandler carries out a command on the server and says what it did. One that goes on to the pages
// returns ToPages's outcome.
type LocalHandler func(ctx context.Context, r Request) (Outcome, error)

// Outcome is what running a command produced.
type Outcome struct {
	Message string         `json:"message"`
	Result  *events.Result `json:"result,omitempty"` // for commands sent to pages
}

// Dispatcher runs commands: server-side ones through their handlers, the rest by sending them to pages.
type Dispatcher struct {
	hub   *events.Hub
	log   events.Logger
	local map[string]LocalHandler
}

// NewDispatcher sends page commands through hub.
func NewDispatcher(hub *events.Hub, log events.Logger) *Dispatcher {
	return &Dispatcher{hub: hub, log: log, local: make(map[string]LocalHandler)}
}

// Handle sets the handler for a server-side command. Its spec must be marked Local.
func (d *Dispatcher) Handle(messageType, command string, handler LocalHandler) {
	spec, ok := Lookup(messageType, command)
	if !ok || !spec.Local {
		panic("control: no local command " + messageType + " " + command)
	}
	d.local[messageType+" "+command] = handler
}

// Hub is the channel the dispatcher sends through.
func (d *Dispatcher) Hub() *events.Hub {
	return d.hub
}

// Run checks a request against the command table and carries it out.
func (d *Dispatcher) Run(ctx context.Context, r Request) (Outcome, error) {
	spec, ok := Lookup(r.Type, r.Command)
	if !ok {
		return Outcome{}, fmt.Errorf("unknown command %q %q", r.Type, r.Command)
	}
	if err := spec.Check(r); err != nil {
		return Outcome{}, err
	}

	var outcome Outcome
	if spec.Local {
		handler := d.local[r.Type+" "+r.Command]
		if handler == nil {
			return Outcome{}, fmt.Errorf("%s %s isn't available on this server", r.Type, r.Command)
		}
		local, err := handler(ctx, r)
		if err != nil {
			d.log("%s ran %s: %v", who(r), spec.Usage(), err)
			return Outcome{}, err
		}
		outcome = local
	} else {
		outcome = d.ToPages(ctx, r)
	}
	d.log("%s ran %s %s%s: %s", who(r), r.Type, r.Command, describeTarget(r), outcome.Message)
	return outcome, nil
}

// ToPages sends a request to the pages as it is, for a server-side handler that has checked or rewritten
// it (video playFile turns a file name into its URL).
func (d *Dispatcher) ToPages(ctx context.Context, r Request) Outcome {
	message := events.NewMessage(r.Type, r.Command, r.Args)
	message.Target = r.Target
	result := d.hub.Command(ctx, message, r.Feed)
	return Outcome{Message: Summarize(result, r.Feed), Result: &result}
}

// Summarize says how pages answered, e.g. "done on 2/2 pages" or "no pages connected".
func Summarize(result events.Result, feed string) string {
	total := len(result.Feeds)
	if total == 0 {
		if feed != "" {
			return "no pages on feed " + feed
		}
		return "no pages connected"
	}
	text := fmt.Sprintf("done on %d/%d pages", result.Acked, total)
	if len(result.Errors) > 0 {
		text += "; " + strings.Join(result.Errors, "; ")
	}
	if result.Silent > 0 {
		text += fmt.Sprintf("; %d didn't answer", result.Silent)
	}
	return text
}

func who(r Request) string {
	if r.From == "" {
		return "someone"
	}
	return r.From
}

func describeTarget(r Request) string {
	text := ""
	if r.Target != "" {
		text += " " + r.Target
	}
	for key, value := range r.Args {
		text += " " + key + "=" + value
	}
	if r.Feed != "" {
		text += " on feed " + r.Feed
	}
	return text
}
