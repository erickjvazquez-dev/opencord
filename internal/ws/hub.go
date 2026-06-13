// Package ws implements the realtime chat gateway: a single hub fans messages
// out to connected clients over WebSocket, routed per channel — a client only
// receives messages and presence for the channel it subscribed to.
package ws

import (
	"encoding/json"

	"github.com/erickjvazquez-dev/opencord/internal/chat"
)

// Event is the envelope every server→client frame uses.
type Event struct {
	Type    string         `json:"type"` // "history" | "message" | "presence" | "error"
	Message *chat.Message  `json:"message,omitempty"`
	History []chat.Message `json:"history,omitempty"`
	Online  int            `json:"online,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// Hub owns the set of connected clients and serializes all mutations through a
// single goroutine (Run), so the client map needs no locking.
type Hub struct {
	store      *chat.Store
	clients    map[*Client]bool
	broadcast  chan chat.Message
	register   chan *Client
	unregister chan *Client
}

func NewHub(store *chat.Store) *Hub {
	return &Hub{
		store:      store,
		clients:    make(map[*Client]bool),
		broadcast:  make(chan chat.Message, 64),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
			h.emitToChannel(c.channelID, Event{Type: "presence", Online: h.countInChannel(c.channelID)})
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
				h.emitToChannel(c.channelID, Event{Type: "presence", Online: h.countInChannel(c.channelID)})
			}
		case m := <-h.broadcast:
			msg := m
			h.emitToChannel(msg.ChannelID, Event{Type: "message", Message: &msg})
		}
	}
}

// countInChannel returns how many connected clients are subscribed to channelID.
func (h *Hub) countInChannel(channelID int64) int {
	n := 0
	for c := range h.clients {
		if c.channelID == channelID {
			n++
		}
	}
	return n
}

// emitToChannel serializes an event and pushes it to every client subscribed to
// channelID, dropping any client whose send buffer is full (a stuck consumer).
func (h *Hub) emitToChannel(channelID int64, e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	for c := range h.clients {
		if c.channelID != channelID {
			continue
		}
		select {
		case c.send <- data:
		default:
			close(c.send)
			delete(h.clients, c)
		}
	}
}
