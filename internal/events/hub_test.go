package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testPage is a fake WeatherHDS page connected to a hub.
type testPage struct {
	t    *testing.T
	conn *websocket.Conn
}

func connect(t *testing.T, server *httptest.Server, feed string) *testPage {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	page := &testPage{t: t, conn: conn}
	if got := page.next(); got.Type != TypeHealth || got.Command != "startUp" {
		t.Fatalf("expected startUp first, got %+v", got)
	}
	page.write(NewMessage(TypeHealth, "hello", map[string]any{"feed": feed}))
	return page
}

func (p *testPage) write(message Message) {
	p.t.Helper()
	if err := p.conn.WriteJSON(message); err != nil {
		p.t.Fatalf("write: %v", err)
	}
}

func (p *testPage) next() Message {
	p.t.Helper()
	_ = p.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var message Message
	if err := p.conn.ReadJSON(&message); err != nil {
		p.t.Fatalf("read: %v", err)
	}
	return message
}

// answer reads the next message and acks it, with an error if one is given.
func (p *testPage) answer(errorText string) Message {
	p.t.Helper()
	message := p.next()
	p.write(Message{Type: TypeAck, ID: message.ID, OK: errorText == "", Error: errorText})
	return message
}

func newTestHub(t *testing.T) (*Hub, *httptest.Server) {
	hub := NewHub("1.0", func(string, ...any) {})
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	return hub, server
}

// waitForFeeds waits until the hub has seen a hello from each named feed.
func waitForFeeds(t *testing.T, hub *Hub, feeds ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		seen := map[string]int{}
		for _, info := range hub.Pages() {
			seen[info.Feed]++
		}
		ready := true
		for _, feed := range feeds {
			if seen[feed] == 0 {
				ready = false
			}
		}
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("feeds %v never said hello; have %+v", feeds, hub.Pages())
}

func TestCommandGoesOnlyToTheTargetFeed(t *testing.T) {
	hub, server := newTestHub(t)
	sd := connect(t, server, "sd")
	hd := connect(t, server, "hd")
	waitForFeeds(t, hub, "sd", "hd")

	done := make(chan Result)
	go func() { done <- hub.Command(context.Background(), Message{Type: "reload", Command: "refresh"}, "sd") }()
	if got := sd.answer(""); got.Type != "reload" || got.ID == "" {
		t.Fatalf("sd got %+v", got)
	}
	result := <-done
	if result.Acked != 1 || len(result.Feeds) != 1 || result.Feeds[0] != "sd" {
		t.Fatalf("unexpected result %+v", result)
	}

	// hd saw nothing: the next thing it reads is this broadcast.
	hub.Broadcast(Message{Type: "health", Command: "heartbeat"})
	if got := hd.next(); got.Command != "heartbeat" {
		t.Fatalf("hd should only see the broadcast, got %+v", got)
	}
}

func TestCommandReportsErrorsAndSilentPages(t *testing.T) {
	hub, server := newTestHub(t)
	failing := connect(t, server, "sd")
	_ = connect(t, server, "hd") // never answers
	waitForFeeds(t, hub, "sd", "hd")

	done := make(chan Result)
	go func() { done <- hub.Command(context.Background(), Message{Type: "video", Command: "playFile"}, "") }()
	failing.answer("no such file")
	result := <-done
	if result.Acked != 0 || result.Silent != 1 || len(result.Errors) != 1 || result.Errors[0] != "sd: no such file" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestExtensionsGetTheirMessagesAndAreClosed(t *testing.T) {
	hub := NewHub("1.0", func(string, ...any) {})
	received := make(chan Message, 1)
	closed := make(chan struct{})
	hub.Extend("music", func(page *Page) (func(Message), func()) {
		page.Send(NewMessage("music", "state", map[string]string{"state": "EMPTY"}))
		return func(m Message) { received <- m }, func() { close(closed) }
	})
	server := httptest.NewServer(hub)
	defer server.Close()

	page := connect(t, server, "sd")
	if got := page.next(); got.Type != "music" || got.Command != "state" {
		t.Fatalf("expected the extension's state, got %+v", got)
	}
	page.write(NewMessage("music", "offer", map[string]string{"sdp": "x"}))
	select {
	case got := <-received:
		if got.Command != "offer" {
			t.Fatalf("extension got %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("extension never got its message")
	}
	_ = page.conn.Close()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("extension was not closed when the page went")
	}
}

func TestUnknownTypeIsReported(t *testing.T) {
	_, server := newTestHub(t)
	page := connect(t, server, "sd")
	page.write(Message{Type: "nonsense"})
	if got := page.next(); got.Command != "error" || !strings.Contains(got.Error, "nonsense") {
		t.Fatalf("expected an error, got %+v", got)
	}
}

func TestShutdownTellsPagesAndClosesThem(t *testing.T) {
	hub, server := newTestHub(t)
	page := connect(t, server, "sd")
	waitForFeeds(t, hub, "sd")
	hub.Shutdown("test")
	got := page.next()
	var args struct{ Reason string }
	_ = json.Unmarshal(got.Args, &args)
	if got.Command != "shutDown" || args.Reason != "test" {
		t.Fatalf("expected shutDown, got %+v", got)
	}
	_ = page.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := page.conn.ReadMessage(); err == nil {
		t.Fatal("expected the channel to close after shutDown")
	}
}

func TestSameOrigin(t *testing.T) {
	request := &http.Request{Host: "weather.local:8080", Header: make(http.Header)}
	if !sameOrigin(request) {
		t.Fatal("empty Origin should be allowed for non-browser clients")
	}
	request.Header.Set("Origin", "https://weather.local:8080")
	if !sameOrigin(request) {
		t.Fatal("matching origin should be allowed")
	}
	request.Header.Set("Origin", "http://evil.local:8080")
	if sameOrigin(request) {
		t.Fatal("mismatched origin should be rejected")
	}
}
