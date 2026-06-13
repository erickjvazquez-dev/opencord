// Package ws implements the realtime chat gateway: a single hub fans messages
// out to every connected client over WebSocket. The MVP has one global channel;
// the hub abstraction is where per-channel routing will live later.
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
			h.emit(Event{Type: "presence", Online: len(h.clients)})
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.emit(Event{Type: "presence", Online: len(h.clients)})
		case m := <-h.broadcast:
			msg := m
			h.emit(Event{Type: "message", Message: &msg})
		}
	}
}

// emit serializes an event and pushes it to every client, dropping any client
// whose send buffer is full (a stuck/slow consumer).
func (h *Hub) emit(e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			close(c.send)
			delete(h.clients, c)
		}
	}
}
