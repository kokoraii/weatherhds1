package console

import (
	"fmt"
	"strings"

	"weatherhds/internal/control"
)

// Console-only commands, handled by the console itself rather than the control table.
var consoleCommands = []string{"clear", "exit", "feeds", "help"}

// Line is one command line typed at the prompt:
//
//	[@feed] <type> <command> [target] [key=value ...]
//	presentation pause main
//	@sd video playFile promo.mp4 mode=squeezeBack
type Line struct {
	Feed  string   // from a leading @feed; "" means every feed
	Words []string // the rest
}

// ParseLine splits a command line into words. Double quotes keep spaces inside a word.
func ParseLine(text string) (Line, error) {
	words, open := split(text)
	if open {
		return Line{}, fmt.Errorf("unclosed quote")
	}
	line := Line{}
	if len(words) > 0 && strings.HasPrefix(words[0], "@") {
		line.Feed = strings.TrimPrefix(words[0], "@")
		words = words[1:]
	}
	line.Words = words
	return line, nil
}

// Request turns the line into a control request. The first two words are the type and command; a word
// without "=" after them is the target, and the rest are key=value arguments. A web address is always a
// target, whatever "=" its query has.
func (l Line) Request() (control.Request, error) {
	if len(l.Words) < 2 {
		return control.Request{}, fmt.Errorf("type a command, e.g. presentation pause main (help lists them)")
	}
	request := control.Request{Type: l.Words[0], Command: l.Words[1], Feed: l.Feed}
	spec, ok := control.Lookup(request.Type, request.Command)
	if !ok {
		return control.Request{}, fmt.Errorf("unknown command %s %s (help lists them)", request.Type, request.Command)
	}
	for _, word := range l.Words[2:] {
		key, value, isArg := strings.Cut(word, "=")
		isArg = isArg && !strings.Contains(word, "://")
		switch {
		case isArg:
			if request.Args == nil {
				request.Args = make(map[string]string)
			}
			request.Args[key] = value
		case spec.Target != nil && request.Target == "":
			request.Target = word
		default:
			return control.Request{}, fmt.Errorf("unexpected %q; usage: %s", word, spec.Usage())
		}
	}
	return request, nil
}

// split breaks text into words on spaces, keeping quoted runs together. open reports an unclosed quote.
func split(text string) (words []string, open bool) {
	var word strings.Builder
	inWord, quoted := false, false
	for _, r := range text {
		switch {
		case r == '"':
			quoted = !quoted
			inWord = true
		case r == ' ' && !quoted:
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, quoted
}
