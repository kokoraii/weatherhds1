package console

import (
	"slices"
	"testing"
)

func TestParseLineReadsFeedTargetAndArgs(t *testing.T) {
	line, err := ParseLine(`@sd reload refresh`)
	if err != nil {
		t.Fatal(err)
	}
	request, err := line.Request()
	if err != nil {
		t.Fatal(err)
	}
	if request.Feed != "sd" || request.Type != "reload" || request.Command != "refresh" {
		t.Fatalf("unexpected request %+v", request)
	}
}

func TestQuotesKeepSpaces(t *testing.T) {
	words, open := split(`video playFile "my promo.mp4" mode=squeezeBack`)
	if open || !slices.Equal(words, []string{"video", "playFile", "my promo.mp4", "mode=squeezeBack"}) {
		t.Fatalf("got %q (open %v)", words, open)
	}
	if _, err := ParseLine(`video playFile "unfinished`); err == nil {
		t.Fatal("an unclosed quote should be an error")
	}
}

func TestRequestRejectsWhatTheCommandDoesntTake(t *testing.T) {
	line, _ := ParseLine("reload refresh main")
	if _, err := line.Request(); err == nil {
		t.Fatal("reload refresh takes no target")
	}
	line, _ = ParseLine("reload")
	if _, err := line.Request(); err == nil {
		t.Fatal("a type alone isn't a command")
	}
}

func TestCompleteWalksTypeThenCommandThenFeed(t *testing.T) {
	sources := func(name string) []string {
		if name == "feeds" {
			return []string{"hd", "sd"}
		}
		return nil
	}
	cases := []struct {
		text  string
		want  []string
		start int
	}{
		{"rel", []string{"reload"}, 0},
		{"he", []string{"help"}, 0},
		{"reload ", []string{"refresh"}, 7},
		{"reload RE", []string{"refresh"}, 7},
		{"@", []string{"@hd", "@sd"}, 0},
		{"@sd rel", []string{"reload"}, 4},
		{"help re", []string{"reload"}, 5},
	}
	for _, c := range cases {
		got, start := Complete(c.text, sources)
		if !slices.Equal(got, c.want) || start != c.start {
			t.Errorf("Complete(%q) = %q at %d, want %q at %d", c.text, got, start, c.want, c.start)
		}
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := CommonPrefix([]string{"pause", "pauseResume"}); got != "pause" {
		t.Fatalf("got %q", got)
	}
	if got := CommonPrefix([]string{"start", "stop"}); got != "st" {
		t.Fatalf("got %q", got)
	}
}
