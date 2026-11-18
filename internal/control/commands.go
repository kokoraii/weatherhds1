// Package control is the set of commands that drive WeatherHDS pages and the server: one table that the
// admin API checks requests against, the console completes from, and help is generated from.
package control

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Param is one argument of a command: its target (the word after the command) or a key=value.
type Param struct {
	Name     string
	Help     string
	Required bool
	Choices  []string // fixed values, checked and offered for completion
	Source   string   // a named list of live values offered for completion ("feeds", "videos", ...)
}

// Spec is one command of a message type.
type Spec struct {
	Type    string
	Command string
	Help    string
	Target  *Param // nil when the command takes no target
	Params  []Param
	Local   bool // carried out on the server instead of being sent to the pages
}

// Request is one command to run, from the console, the admin API or a change set.
type Request struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Target  string            `json:"target,omitempty"`
	Args    map[string]string `json:"args,omitempty"`
	Feed    string            `json:"feed,omitempty"` // one feed, or "" for all of them
	From    string            `json:"-"`              // who sent it, for the log
}

// Presentations are the page's presentations that can be targeted.
var Presentations = []string{"main", "ldl", "sidebar", "cityticker"}

var specs = []Spec{
	{Type: "reload", Command: "refresh", Help: "Reload the page."},
}

// Specs lists every command, sorted by type and command.
func Specs() []Spec {
	sorted := slices.Clone(specs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Type != sorted[j].Type {
			return sorted[i].Type < sorted[j].Type
		}
		return sorted[i].Command < sorted[j].Command
	})
	return sorted
}

// Lookup finds a command's spec.
func Lookup(messageType, command string) (Spec, bool) {
	for _, spec := range specs {
		if spec.Type == messageType && spec.Command == command {
			return spec, true
		}
	}
	return Spec{}, false
}

// register adds commands to the table. Each feature's file calls it from init.
func register(more ...Spec) {
	specs = append(specs, more...)
}

// Check reports what's wrong with a request for this command, if anything.
func (s Spec) Check(r Request) error {
	if s.Target == nil && r.Target != "" {
		return fmt.Errorf("%s %s takes no target", s.Type, s.Command)
	}
	if s.Target != nil {
		if err := s.Target.check(r.Target); err != nil {
			return err
		}
	}
	for name := range r.Args {
		if !slices.ContainsFunc(s.Params, func(p Param) bool { return p.Name == name }) {
			return fmt.Errorf("%s %s has no argument %q", s.Type, s.Command, name)
		}
	}
	for _, param := range s.Params {
		if err := param.check(r.Args[param.Name]); err != nil {
			return err
		}
	}
	return nil
}

func (p Param) check(value string) error {
	if value == "" {
		if p.Required {
			return fmt.Errorf("%s is required", p.Name)
		}
		return nil
	}
	if len(p.Choices) > 0 && !slices.Contains(p.Choices, value) {
		return fmt.Errorf("%s must be one of %s", p.Name, strings.Join(p.Choices, ", "))
	}
	return nil
}

// Usage is the command's one-line syntax, e.g. "presentation pause <presentation>".
func (s Spec) Usage() string {
	parts := []string{s.Type, s.Command}
	if s.Target != nil {
		parts = append(parts, bracket(s.Target.Name, s.Target.Required))
	}
	for _, param := range s.Params {
		parts = append(parts, bracket(param.Name+"=…", param.Required))
	}
	return strings.Join(parts, " ")
}

func bracket(text string, required bool) string {
	if required {
		return "<" + text + ">"
	}
	return "[" + text + "]"
}
