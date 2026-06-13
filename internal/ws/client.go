package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// CheckOrigin is permissive because in production the browser talks to the
// gateway through the same-origin nginx proxy; tighten this if you expose the
// gateway cross-origin.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	user      auth.User
	channelID int64
}

var errUnknownChannel = errors.New("unknown channel")

// ServeWS authenticates the upgrade request (token via ?token=), resolves the
// target channel (?channel=<id>, default `general`), registers the client,
// replays that channel's recent history, and starts the read/write pumps.
func ServeWS(hub *Hub, authsvc *auth.Service, store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := authsvc.Parse(auth.TokenFromRequest(r))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		channelID, err := resolveChannel(r, store)
		if err != nil {
			http.Error(w, "invalid channel", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already wrote an error response
		}

		c := &Client{hub: hub, conn: conn, send: make(chan []byte, 32), user: user, channelID: channelID}
		hub.register <- c

		if msgs, err := store.Recent(r.Context(), channelID, 50); err == nil {
			if data, err := json.Marshal(Event{Type: "history", History: msgs}); err == nil {
				c.send <- data
			}
		}

		go c.writePump()
		go c.readPump(store)
	}
}

// resolveChannel returns the channel id from ?channel=<id>, or the default
// `general` channel when absent. It errors on a malformed or unknown id.
func resolveChannel(r *http.Request, store *chat.Store) (int64, error) {
	raw := r.URL.Query().Get("channel")
	if raw == "" {
		return store.DefaultChannelID(r.Context())
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	ok, err := store.ChannelExists(r.Context(), id)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errUnknownChannel
	}
	return id, nil
}

// readPump reads inbound frames, persists each as a message, and hands it to
// the hub for broadcast. It owns connection teardown.
func (c *Client) readPump(store *chat.Store) {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var in struct {
			Type string `json:"type"`
			Body string `json:"body"`
		}
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		if in.Type == "typing" {
			// Ephemeral: relay to the channel, never persisted.
			c.hub.BroadcastToChannel(c.channelID, Event{Type: "typing", Username: c.user.Username})
			continue
		}
		body := strings.TrimSpace(in.Body)
		if body == "" || len(body) > maxMessageSize {
			continue
		}
		msg, err := store.Save(context.Background(), c.channelID, c.user.ID, c.user.Username, body)
		if err != nil {
			continue
		}
		c.hub.broadcast <- msg
	}
}

// writePump drains the send channel and keeps the connection alive with pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case data, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok { // hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
