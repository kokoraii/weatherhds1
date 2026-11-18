// Package events is the realtime channel between the server and each WeatherHDS page: one WebSocket per
// page at /events, carrying typed messages both ways.
package events

import "encoding/json"

// Message is one message on a page's channel, in either direction.
//
//	{"type": "presentation", "command": "pause", "target": "main", "args": {...}, "id": "c-41"}
//
// A message with an id expects an answer: the page replies {"type": "ack", "id": ..., "ok": true} or
// {"type": "ack", "id": ..., "error": "..."}.
type Message struct {
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Target  string          `json:"target,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	ID      string          `json:"id,omitempty"`
	OK      bool            `json:"ok,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// TypeAck answers a message that carried an id.
const TypeAck = "ack"

// TypeHealth carries the connection's lifecycle: hello and state from the page; heartbeat, startUp,
// shutDown, backgroundSync and holidaySync from the server.
const TypeHealth = "health"

// NewMessage builds a message whose args are any JSON-encodable value.
func NewMessage(messageType, command string, args any) Message {
	message := Message{Type: messageType, Command: command}
	if args != nil {
		payload, err := json.Marshal(args)
		if err != nil {
			panic("events: unencodable args for " + messageType + " " + command + ": " + err.Error())
		}
		message.Args = payload
	}
	return message
}
