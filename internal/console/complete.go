package console

import (
	"slices"
	"sort"
	"strings"

	"weatherhds/internal/control"
)

// Sources gives the live values a parameter can take, by its control.Param.Source name ("feeds",
// "videos", ...). Unknown names give none.
type Sources func(name string) []string

// Complete lists what the last word of text could become, and where that word starts. Matching ignores
// case, so "pauser" offers "pauseResume".
func Complete(text string, sources Sources) (candidates []string, wordStart int) {
	words, _ := split(text)
	current := ""
	if len(words) > 0 && !strings.HasSuffix(text, " ") {
		current = words[len(words)-1]
		words = words[:len(words)-1]
	}
	wordStart = len(text) - len(current)

	// A leading @feed picks the feed, then the command starts after it.
	if len(words) == 0 && strings.HasPrefix(current, "@") {
		return matching(prefixed("@", sources("feeds")), current), wordStart
	}
	if len(words) > 0 && strings.HasPrefix(words[0], "@") {
		words = words[1:]
	}

	switch len(words) {
	case 0:
		return matching(append(types(), consoleCommands...), current), wordStart
	case 1:
		if words[0] == "help" {
			return matching(types(), current), wordStart
		}
		return matching(commandsOf(words[0]), current), wordStart
	}

	spec, ok := control.Lookup(words[0], words[1])
	if !ok {
		return nil, wordStart
	}
	// key=value: complete the value of that key.
	if key, partial, isArg := strings.Cut(current, "="); isArg {
		for _, param := range spec.Params {
			if param.Name == key {
				return matching(prefixed(key+"=", values(param, sources)), key+"="+partial), wordStart
			}
		}
		return nil, wordStart
	}
	var offered []string
	targetGiven := false
	used := map[string]bool{}
	for _, word := range words[2:] {
		if key, _, isArg := strings.Cut(word, "="); isArg {
			used[key] = true
		} else {
			targetGiven = true
		}
	}
	if spec.Target != nil && !targetGiven {
		offered = append(offered, values(*spec.Target, sources)...)
	}
	for _, param := range spec.Params {
		if !used[param.Name] {
			offered = append(offered, param.Name+"=")
		}
	}
	return matching(offered, current), wordStart
}

// CommonPrefix is the longest start all candidates share, for Tab to fill in.
func CommonPrefix(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	prefix := candidates[0]
	for _, candidate := range candidates[1:] {
		for !strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix)) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func values(param control.Param, sources Sources) []string {
	if len(param.Choices) > 0 {
		return param.Choices
	}
	if param.Source != "" {
		return sources(param.Source)
	}
	return nil
}

func types() []string {
	var names []string
	for _, spec := range control.Specs() {
		if !slices.Contains(names, spec.Type) {
			names = append(names, spec.Type)
		}
	}
	return names
}

func commandsOf(messageType string) []string {
	var names []string
	for _, spec := range control.Specs() {
		if spec.Type == messageType {
			names = append(names, spec.Command)
		}
	}
	return names
}

func prefixed(prefix string, words []string) []string {
	out := make([]string, len(words))
	for i, word := range words {
		out[i] = prefix + word
	}
	return out
}

// matching keeps the words that start with prefix, ignoring case, sorted and without repeats.
func matching(words []string, prefix string) []string {
	lowered := strings.ToLower(prefix)
	var out []string
	for _, word := range words {
		if strings.HasPrefix(strings.ToLower(word), lowered) && !slices.Contains(out, word) {
			out = append(out, word)
		}
	}
	sort.Strings(out)
	return out
}
