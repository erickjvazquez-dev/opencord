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
	maxMessageSize = 4096  // max chat message body (Rule B)
	maxFrameSize   = 16384 // max inbound WS frame — larger than a message so SDP offers fit
	maxSignalSize  = 8192  // max voice-signal payload (SDP/ICE) — flood guard
	maxStreamID    = 128   // max voice-screen StreamID (a MediaStream.id is ~36 chars) — Rule B

	// Per-connection inbound rate limit (token bucket): burst of rateBurst frames,
	// refilling rateRefillPerSec/sec. Each inbound frame (message or typing) costs
	// one token; excess frames are dropped. Bounds a single connection's flood.
	rateBurst        = 5.0
	rateRefillPerSec = 2.0

	// Dedicated voice rate bucket. Voice signaling (mesh WebRTC) is legitimately
	// bursty — a joiner trickles many ICE candidates to each peer at call setup —
	// so it's exempt from the strict text bucket, but NOT unbounded: every voice
	// frame is fanned out to the whole channel, so an unthrottled flood is an
	// amplification DoS. This bucket is generous enough for a 2–4 peer setup burst
	// (~dozens of frames in a second) yet caps a sustained flood (Rule 15).
	voiceBurst        = 100.0
	voiceRefillPerSec = 50.0
)

// MaxConnsPerUser bounds how many concurrent WebSocket connections ONE user may hold
// (Rule B/15). The per-connection token bucket above throttles a single socket, but it's
// per-connection: without this cap a single authenticated user could open unbounded
// sockets — each costs 2 goroutines + a 32-slot send buffer + a history fetch — to
// exhaust the server, OR churn reconnects to keep getting a fresh rate-limit bucket. The
// hub evicts a user's OLDEST socket past this cap (so a legitimate reconnect after a
// network blip still connects; only stale/abusive excess is dropped). Generous enough for
// real multi-tab / multi-device use (each tab is ~1 socket) while bounding abuse.
const MaxConnsPerUser = 10

// CheckOrigin is permissive because in production the browser talks to the
// gateway through the same-origin nginx proxy; tighten this if you expose the
// gateway cross-origin.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	user       auth.User
	channelID  int64
	// seq is a hub-assigned monotonic registration order, used to evict a user's OLDEST
	// socket first when they exceed MaxConnsPerUser. Set + read only on the hub goroutine.
	seq        int64
	rateTokens float64
	rateLast   time.Time
	// Separate token bucket for voice signaling frames (see voiceBurst).
	voiceTokens float64
	voiceLast   time.Time
	// Closed (once, by the hub) when this client is dropped. `send` is never
	// closed — ServeWS, readPump, and the hub all write to it, so closing it
	// under them would panic ("send on closed channel"); producers select on
	// `done` to bail instead, and writePump exits when it's closed.
	done chan struct{}
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
		// DM (and future private) channels are members-only — refuse the upgrade for
		// anyone who can't access this channel before we read or stream anything.
		if ok, err := store.CanAccessChannel(r.Context(), channelID, user.ID); err != nil || !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already wrote an error response
		}

		c := &Client{
			hub: hub, conn: conn, send: make(chan []byte, 32), user: user, channelID: channelID,
			rateTokens: rateBurst, rateLast: time.Now(),
			voiceTokens: voiceBurst, voiceLast: time.Now(),
			done: make(chan struct{}),
		}
		hub.register <- c

		if msgs, err := store.Recent(r.Context(), channelID, user.ID, 50); err == nil {
			// Capture the read marker BEFORE the client marks the channel read, so the
			// "New messages" divider anchors at the pre-open boundary (nil on first visit).
			lastRead, _ := store.LastReadID(r.Context(), channelID, user.ID)
			if data, err := json.Marshal(Event{Type: "history", History: msgs, LastReadID: lastRead}); err == nil {
				// Safe send: the hub may drop us (closing done) before the pumps
				// drain; never a raw send, which could race a channel close.
				c.sendSafe(data)
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

	c.conn.SetReadLimit(maxFrameSize)
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
			Type     string          `json:"type"`
			Body     string          `json:"body"`
			ReplyTo  *int64          `json:"replyTo"`
			Target   int64           `json:"target"`
			Signal   json.RawMessage `json:"signal"`
			On       bool            `json:"on"`
			StreamID string          `json:"streamId"`
			Kind     string          `json:"kind"`
		}
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		// Voice signaling (mesh WebRTC) is relayed verbatim, stamped with the sender.
		// It's exempt from the strict text bucket — ICE exchange is legitimately
		// bursty — but throttled by its own, more generous voice bucket so a flood
		// (each frame is fanned out to the whole channel) can't be amplified into a
		// DoS. The signal payload is also size-bounded (Rule 15).
		switch in.Type {
		case "voice-join", "voice-leave", "voice-signal", "voice-screen":
			nowV := time.Now()
			c.voiceTokens = min(voiceBurst, c.voiceTokens+nowV.Sub(c.voiceLast).Seconds()*voiceRefillPerSec)
			c.voiceLast = nowV
			if c.voiceTokens < 1 {
				continue // over the voice budget — drop (flood guard)
			}
			c.voiceTokens--
			if in.Type == "voice-signal" {
				if len(in.Signal) == 0 || len(in.Signal) > maxSignalSize {
					continue
				}
				c.hub.BroadcastToChannel(c.channelID, Event{
					Type: "voice-signal", From: c.user.ID, Username: c.user.Username,
					Target: in.Target, Signal: in.Signal,
				})
				continue
			}
			if in.Type == "voice-screen" {
				// Announce a start/stop of a shared video (screen OR camera). StreamID
				// lets receivers tell the video's tracks from the mic; Kind tags it as
				// the screen or the camera. Bound StreamID and whitelist Kind (Rule B —
				// an unknown kind is hostile/garbage, so drop the frame); carry both
				// only when starting.
				if len(in.StreamID) > maxStreamID {
					continue
				}
				if in.On && in.Kind != "" && in.Kind != "screen" && in.Kind != "camera" {
					continue
				}
				ev := Event{Type: "voice-screen", From: c.user.ID, Username: c.user.Username, On: in.On}
				if in.On {
					ev.StreamID = in.StreamID
					ev.Kind = in.Kind
				}
				c.hub.BroadcastToChannel(c.channelID, ev)
				continue
			}
			c.hub.BroadcastToChannel(c.channelID, Event{Type: in.Type, From: c.user.ID, Username: c.user.Username})
			continue
		}
		// Per-connection rate limit (token bucket): drop non-voice frames over budget.
		now := time.Now()
		c.rateTokens = min(rateBurst, c.rateTokens+now.Sub(c.rateLast).Seconds()*rateRefillPerSec)
		c.rateLast = now
		if c.rateTokens < 1 {
			continue
		}
		c.rateTokens--
		if in.Type == "typing" {
			// Ephemeral: relay to the channel, never persisted.
			c.hub.BroadcastToChannel(c.channelID, Event{Type: "typing", Username: c.user.Username})
			continue
		}
		body := strings.TrimSpace(in.Body)
		if body == "" || len(body) > maxMessageSize {
			continue
		}
		msg, err := store.SaveReply(context.Background(), c.channelID, c.user.ID, c.user.Username, body, in.ReplyTo)
		if errors.Is(err, chat.ErrForbidden) {
			// Read-only channel: tell the sender instead of silently dropping.
			if data, e := json.Marshal(Event{Type: "error", Error: "you can't post in this channel"}); e == nil {
				c.sendSafe(data)
			}
			continue
		}
		if errors.Is(err, chat.ErrSlowMode) {
			// Slowmode: tell the sender to wait, rather than silently dropping.
			if data, e := json.Marshal(Event{Type: "error", Error: "slow mode is on — wait before posting again"}); e == nil {
				c.sendSafe(data)
			}
			continue
		}
		if errors.Is(err, chat.ErrTimedOut) {
			// Timed out (muted): tell the sender instead of silently dropping.
			if data, e := json.Marshal(Event{Type: "error", Error: "you're timed out and can't post right now"}); e == nil {
				c.sendSafe(data)
			}
			continue
		}
		if err != nil {
			continue
		}
		c.hub.broadcast <- msg
	}
}

// sendSafe queues data to the client without ever risking a "send on closed
// channel" panic: if the hub has already dropped this client (done closed), it
// bails. `send` is intentionally never closed; `done` is the closure signal.
func (c *Client) sendSafe(data []byte) {
	select {
	case c.send <- data:
	case <-c.done:
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
		case data := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-c.done: // hub dropped this client
			// Flush any already-queued frames (e.g. a final "server-removed" notice
			// queued just before eviction) before sending the Close — otherwise the
			// select above could pick Close ahead of a pending frame and lose it.
			for {
				select {
				case data := <-c.send:
					_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
					if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return
					}
				default:
					_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
					_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
