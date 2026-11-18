package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	heartbeatPeriod = 15 * time.Second
	ackTimeout      = 2 * time.Second
	shutdownFlush   = time.Second
)

// Logger writes one line to the server log.
type Logger func(format string, args ...any)

// Extension adds a message type's per-page behaviour (music, for one). It runs for each page as it
// connects and returns the handler for that page's messages of the type, and what to do when it goes.
type Extension func(page *Page) (handle func(Message), close func())

// Hub keeps every connected page and routes messages between them and the server.
type Hub struct {
	log      Logger
	version  string
	bootID   string
	started  time.Time
	upgrader websocket.Upgrader
	writers  sync.WaitGroup

	mu         sync.Mutex
	pages      map[*Page]struct{}
	extensions map[string]Extension
	greeters   []func(*Page)
	waiters    map[string]*waiter
	nextID     uint64
	closing    bool
}

// Result says how pages answered a command.
type Result struct {
	Feeds  []string `json:"feeds"`            // the feed of each page the command went to
	Acked  int      `json:"acked"`            // pages that carried it out
	Errors []string `json:"errors,omitempty"` // "sd: unsupported command"
	Silent int      `json:"silent"`           // pages that didn't answer in time
}

type waiter struct {
	pending map[*Page]struct{}
	result  Result
	done    chan struct{}
}

// NewHub makes a hub for a server running this config version.
func NewHub(version string, log Logger) *Hub {
	bootID := make([]byte, 6)
	_, _ = rand.Read(bootID)
	return &Hub{
		log:     log,
		version: version,
		bootID:  hex.EncodeToString(bootID),
		started: time.Now(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     sameOrigin,
		},
		pages:      make(map[*Page]struct{}),
		extensions: make(map[string]Extension),
		waiters:    make(map[string]*waiter),
	}
}

// Extend registers the handler for one message type. Call before serving.
func (h *Hub) Extend(messageType string, extension Extension) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.extensions[messageType] = extension
}

// OnConnect adds something every new page is sent once it connects, after startUp: the current
// background, say. Call before serving.
func (h *Hub) OnConnect(greet func(*Page)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.greeters = append(h.greeters, greet)
}

// ServeHTTP upgrades a page's request to its event channel and serves it until it disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log("event channel upgrade from %s failed: %v", r.RemoteAddr, err)
		return
	}
	page := &Page{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, sendQueueLength),
		Addr:     r.RemoteAddr,
		Since:    time.Now(),
		handlers: make(map[string]func(Message)),
	}

	h.mu.Lock()
	if h.closing {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.pages[page] = struct{}{}
	extensions := make(map[string]Extension, len(h.extensions))
	for messageType, extension := range h.extensions {
		extensions[messageType] = extension
	}
	greeters := h.greeters
	h.writers.Add(1)
	h.mu.Unlock()

	page.Send(NewMessage(TypeHealth, "startUp", h.startUpArgs()))
	for _, greet := range greeters {
		greet(page)
	}
	for messageType, extension := range extensions {
		handle, closer := extension(page)
		page.mu.Lock()
		page.handlers[messageType] = handle
		page.closers = append(page.closers, closer)
		page.mu.Unlock()
	}
	go func() {
		defer h.writers.Done()
		page.writePump()
	}()
	page.readPump()
}

func (h *Hub) startUpArgs() map[string]any {
	return map[string]any{"boot": h.bootID, "version": h.version, "started": h.started}
}

func (h *Hub) receive(page *Page, message Message) {
	switch message.Type {
	case TypeAck:
		h.resolve(page, message)
	case TypeHealth:
		h.receiveHealth(page, message)
	default:
		page.mu.Lock()
		handle := page.handlers[message.Type]
		page.mu.Unlock()
		if handle == nil {
			page.Send(Message{Type: TypeHealth, Command: "error", Error: "unsupported message type " + message.Type})
			return
		}
		handle(message)
	}
}

// receiveHealth takes the page's hello (its feed and presentation states), later state reports, and
// lines the page wants in the server log (a video that failed after its command was acked, say).
func (h *Hub) receiveHealth(page *Page, message Message) {
	var args struct {
		Feed          string          `json:"feed"`
		Version       string          `json:"version"`
		Presentations json.RawMessage `json:"presentations"`
		Text          string          `json:"text"`
	}
	_ = json.Unmarshal(message.Args, &args)
	switch message.Command {
	case "hello":
		page.mu.Lock()
		page.feed = args.Feed
		page.state = args.Presentations
		page.mu.Unlock()
		h.log("page connected: feed %s from %s (page version %s)", page.Feed(), page.Addr, args.Version)
	case "state":
		page.mu.Lock()
		page.state = args.Presentations
		page.mu.Unlock()
	case "log":
		h.log("page %s: %s", page.Feed(), args.Text)
	}
}

func (h *Hub) remove(page *Page) {
	h.mu.Lock()
	_, present := h.pages[page]
	delete(h.pages, page)
	for _, w := range h.waiters {
		if _, waiting := w.pending[page]; waiting {
			delete(w.pending, page)
			w.result.Silent++
			if len(w.pending) == 0 {
				close(w.done)
			}
		}
	}
	h.mu.Unlock()
	page.close()
	if present {
		h.log("page disconnected: feed %s from %s", page.Feed(), page.Addr)
	}
}

// targets lists the pages of a feed, or every page for "".
func (h *Hub) targets(feed string) []*Page {
	h.mu.Lock()
	defer h.mu.Unlock()
	pages := make([]*Page, 0, len(h.pages))
	for page := range h.pages {
		if feed == "" || page.Feed() == feed {
			pages = append(pages, page)
		}
	}
	return pages
}

// Broadcast sends a message to every page, without waiting for answers.
func (h *Hub) Broadcast(message Message) {
	for _, page := range h.targets("") {
		page.Send(message)
	}
}

// Command sends a message to every page of a feed ("" for all) and waits up to two seconds for each
// to say whether it carried it out.
func (h *Hub) Command(ctx context.Context, message Message, feed string) Result {
	pages := h.targets(feed)
	w := &waiter{pending: make(map[*Page]struct{}, len(pages)), done: make(chan struct{})}
	for _, page := range pages {
		w.pending[page] = struct{}{}
		w.result.Feeds = append(w.result.Feeds, page.Feed())
	}
	sort.Strings(w.result.Feeds)
	if len(pages) == 0 {
		return w.result
	}

	h.mu.Lock()
	h.nextID++
	message.ID = "c-" + strconv.FormatUint(h.nextID, 10)
	h.waiters[message.ID] = w
	h.mu.Unlock()

	for _, page := range pages {
		page.Send(message)
	}
	timer := time.NewTimer(ackTimeout)
	defer timer.Stop()
	select {
	case <-w.done:
	case <-timer.C:
	case <-ctx.Done():
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.waiters, message.ID)
	result := w.result
	result.Silent += len(w.pending)
	return result
}

func (h *Hub) resolve(page *Page, ack Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.waiters[ack.ID]
	if w == nil {
		return
	}
	if _, waiting := w.pending[page]; !waiting {
		return
	}
	delete(w.pending, page)
	if ack.Error != "" {
		w.result.Errors = append(w.result.Errors, fmt.Sprintf("%s: %s", page.Feed(), ack.Error))
	} else {
		w.result.Acked++
	}
	if len(w.pending) == 0 {
		close(w.done)
	}
}

// Pages lists the connected pages, oldest first.
func (h *Hub) Pages() []PageInfo {
	pages := h.targets("")
	infos := make([]PageInfo, 0, len(pages))
	for _, page := range pages {
		infos = append(infos, page.info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Since.Before(infos[j].Since) })
	return infos
}

// Run sends the routine heartbeat until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(heartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.Broadcast(NewMessage(TypeHealth, "heartbeat", map[string]any{
				"boot":   h.bootID,
				"uptime": int(time.Since(h.started).Seconds()),
			}))
		}
	}
}

// Shutdown tells every page the server is going away, closes their channels once the message is out,
// and refuses new ones.
func (h *Hub) Shutdown(reason string) {
	h.mu.Lock()
	h.closing = true
	h.mu.Unlock()
	h.Broadcast(NewMessage(TypeHealth, "shutDown", map[string]any{"reason": reason}))
	for _, page := range h.targets("") {
		page.close()
	}
	flushed := make(chan struct{})
	go func() {
		h.writers.Wait()
		close(flushed)
	}()
	select {
	case <-flushed:
	case <-time.After(shutdownFlush):
	}
}

// sameOrigin accepts pages served by this server, and non-browser clients that send no Origin.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "http://"+r.Host || origin == "https://"+r.Host
}
