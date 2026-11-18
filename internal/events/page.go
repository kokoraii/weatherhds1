package events

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait       = 10 * time.Second
	pongWait        = 60 * time.Second
	pingPeriod      = 45 * time.Second
	maxMessageBytes = 64 * 1024
	sendQueueLength = 32
)

// Page is one connected WeatherHDS page.
type Page struct {
	hub   *Hub
	conn  *websocket.Conn
	send  chan []byte
	Addr  string
	Since time.Time

	mu       sync.Mutex
	feed     string
	state    json.RawMessage // what the page last reported about its presentations
	handlers map[string]func(Message)
	closers  []func()
	once     sync.Once
}

// PageInfo is a snapshot of a page for listings.
type PageInfo struct {
	Feed  string          `json:"feed"`
	Addr  string          `json:"addr"`
	Since time.Time       `json:"since"`
	State json.RawMessage `json:"state,omitempty"`
}

// Feed is the name the page gave in its hello ("sd", "hd", ...), or "default" before then.
func (p *Page) Feed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.feed == "" {
		return "default"
	}
	return p.feed
}

func (p *Page) info() PageInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return PageInfo{Feed: p.feed, Addr: p.Addr, Since: p.Since, State: p.state}
}

// Send queues a message for the page. A page that can't keep up is disconnected; it reconnects.
func (p *Page) Send(message Message) {
	payload, err := json.Marshal(message)
	if err != nil {
		panic("events: unencodable message: " + err.Error())
	}
	defer func() { _ = recover() }() // the queue may close between the check and the send
	select {
	case p.send <- payload:
	default:
		p.hub.log("page %s (%s) fell behind; disconnecting", p.Feed(), p.Addr)
		p.close()
	}
}

func (p *Page) readPump() {
	defer p.hub.remove(p)
	p.conn.SetReadLimit(maxMessageBytes)
	_ = p.conn.SetReadDeadline(time.Now().Add(pongWait))
	p.conn.SetPongHandler(func(string) error {
		return p.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, payload, err := p.conn.ReadMessage()
		if err != nil {
			return
		}
		var message Message
		if err := json.Unmarshal(payload, &message); err != nil {
			p.Send(Message{Type: TypeHealth, Command: "error", Error: "malformed message"})
			continue
		}
		p.hub.receive(p, message)
	}
}

func (p *Page) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = p.conn.Close()
	}()
	for {
		select {
		case payload, ok := <-p.send:
			_ = p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = p.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := p.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := p.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// close ends the page's extensions and its connection. Queued messages are still written first.
func (p *Page) close() {
	p.once.Do(func() {
		p.mu.Lock()
		closers := p.closers
		p.mu.Unlock()
		for _, closer := range closers {
			closer()
		}
		close(p.send)
	})
}
