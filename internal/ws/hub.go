// Package ws implements the realtime chat gateway: a single hub fans messages
// out to connected clients over WebSocket, routed per channel — a client only
// receives messages and presence for the channel it subscribed to.
package ws

import (
	"encoding/json"
	"sort"

	"github.com/erickjvazquez-dev/opencord/internal/chat"
)

// Event is the envelope every server→client frame uses.
type Event struct {
	Type    string         `json:"type"` // history|message|message-edited|message-deleted|message-pinned|typing|presence|error|voice-join|voice-leave|voice-signal|voice-screen
	Message *chat.Message  `json:"message,omitempty"`
	History []chat.Message `json:"history,omitempty"`
	// LastReadID rides the "history" event: the viewer's read marker for this channel at
	// connect time (nil = no read row yet), so the client can draw the "New messages"
	// divider at the pre-open boundary. omitempty → nil is omitted.
	LastReadID *int64 `json:"lastReadId,omitempty"`
	Username   string `json:"username,omitempty"` // who, for "typing" / voice
	Online     int    `json:"online,omitempty"`
	Error      string `json:"error,omitempty"`
	// Voice signaling (mesh WebRTC): From is the sender; Target the intended peer
	// (clients ignore a voice-signal unless Target is them); Signal is opaque
	// WebRTC JSON (an SDP offer/answer or an ICE candidate).
	From   int64           `json:"from,omitempty"`
	Target int64           `json:"target,omitempty"`
	Signal json.RawMessage `json:"signal,omitempty"`
	// Screen share (voice-screen): On=true announces a peer started sharing and
	// StreamID carries the screen MediaStream's id (so receivers can tell the
	// screen's audio/video tracks apart from the mic). A voice-screen frame with
	// On omitted (false) means the peer stopped sharing.
	On       bool   `json:"on,omitempty"`
	StreamID string `json:"streamId,omitempty"`
	// Kind tags a voice-screen video stream as the screen ("screen", the default/
	// omitted) or the camera ("camera"), so receivers can label/mirror it correctly.
	Kind string `json:"kind,omitempty"`
	// VoiceMembers (v0.9, type "voice-presence") is the set of user ids currently in the
	// voice call on this channel, sorted; emitted whenever someone joins/leaves voice.
	VoiceMembers []int64 `json:"voiceMembers,omitempty"`
	// ServerID scopes a user-targeted event to a server (e.g. "server-removed",
	// "server-renamed"). Name carries a server's new name on "server-renamed".
	ServerID int64  `json:"serverId,omitempty"`
	Name     string `json:"name,omitempty"`
	// ChannelID scopes a channel event (e.g. "dm-membership" when a group DM's roster
	// changes, so its members refetch their DM list).
	ChannelID int64 `json:"channelId,omitempty"`
}

// targetedEvent is an Event addressed to a specific channel — used for events
// that aren't tied to a stored message (e.g. typing).
type targetedEvent struct {
	channelID int64
	event     Event
}

// evictReq tells the hub to disconnect every live socket belonging to userID that
// is subscribed to one of `channels` — used when a user loses access (e.g. kicked
// from a server) since WS access is otherwise only checked at connect time.
type evictReq struct {
	userID   int64
	channels map[int64]bool
}

// onlineReq asks the hub for the set of currently-connected user IDs (presence).
type onlineReq struct {
	reply chan map[int64]bool
}

// voiceReq asks the hub for the in-voice user ids of each of the given channels (presence
// across channels — used by the sidebar, since a client's WS only covers its active channel).
type voiceReq struct {
	channelIDs []int64
	reply      chan map[int64][]int64
}

// userMessage carries a pre-marshalled event addressed to every live socket of a
// specific user (regardless of which channel each is on).
type userMessage struct {
	userID int64
	data   []byte
}

// Hub owns the set of connected clients and serializes all mutations through a
// single goroutine (Run), so the client map needs no locking.
type Hub struct {
	store      *chat.Store
	clients    map[*Client]bool
	broadcast  chan chat.Message
	events     chan targetedEvent
	register   chan *Client
	unregister chan *Client
	evict      chan evictReq
	online     chan onlineReq
	voice      chan voiceReq
	toUser     chan userMessage
	// voiceMembers tracks who is in the voice call per channel (channelID → {userID}).
	// Mutated ONLY on the Run goroutine (like clients), so it needs no lock.
	voiceMembers map[int64]map[int64]bool
}

func NewHub(store *chat.Store) *Hub {
	return &Hub{
		store:        store,
		clients:      make(map[*Client]bool),
		broadcast:    make(chan chat.Message, 64),
		events:       make(chan targetedEvent, 64),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		evict:        make(chan evictReq),
		online:       make(chan onlineReq),
		voice:        make(chan voiceReq),
		toUser:       make(chan userMessage),
		voiceMembers: make(map[int64]map[int64]bool),
	}
}

// SendToUser delivers e to every live socket belonging to userID, regardless of which
// channel each is on. The push happens on the hub goroutine (no locks). Used for
// per-user notifications that aren't tied to a channel (e.g. "you were removed from a
// server"). Best-effort: a socket whose buffer is full is dropped (same as fan-out).
func (h *Hub) SendToUser(userID int64, e Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	h.toUser <- userMessage{userID: userID, data: data}
}

// OnlineUserIDs returns the set of user IDs with ≥1 live WS connection (presence).
// Read-only; the set is built on the hub goroutine so the client map needs no lock.
func (h *Hub) OnlineUserIDs() map[int64]bool {
	reply := make(chan map[int64]bool, 1)
	h.online <- onlineReq{reply: reply}
	return <-reply
}

// VoiceMembersFor returns each channel's in-voice user ids (only channels with someone in
// voice appear in the result). Built on the hub goroutine, so the map needs no lock.
func (h *Hub) VoiceMembersFor(channelIDs []int64) map[int64][]int64 {
	reply := make(chan map[int64][]int64, 1)
	h.voice <- voiceReq{channelIDs: channelIDs, reply: reply}
	return <-reply
}

// EvictUserFromChannels disconnects every live socket belonging to userID that is
// subscribed to one of channelIDs. Used when a user loses realtime access (e.g. is
// kicked from a server) — without this an already-open socket would keep streaming
// the channel, since access is only checked at connect (ServeWS → CanAccessChannel).
// Pure in-memory; the actual disconnect runs on the hub goroutine.
func (h *Hub) EvictUserFromChannels(userID int64, channelIDs []int64) {
	if len(channelIDs) == 0 {
		return
	}
	set := make(map[int64]bool, len(channelIDs))
	for _, id := range channelIDs {
		set[id] = true
	}
	h.evict <- evictReq{userID: userID, channels: set}
}

// BroadcastEvent fans an already-built event out to its channel (derived from the
// embedded message). Used by REST handlers (e.g. delete/edit).
func (h *Hub) BroadcastEvent(e Event) {
	if e.Message != nil {
		h.events <- targetedEvent{e.Message.ChannelID, e}
	}
}

// BroadcastToChannel fans an event out to a specific channel, for events with no
// stored message (e.g. typing).
func (h *Hub) BroadcastToChannel(channelID int64, e Event) {
	h.events <- targetedEvent{channelID, e}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
			h.emitToChannel(c.channelID, Event{Type: "presence", Online: h.countInChannel(c.channelID)})
			// Tell the new client about any call already in progress on this channel.
			if len(h.voiceMembers[c.channelID]) > 0 {
				h.emitVoicePresence(c.channelID)
			}
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.done)
				h.emitToChannel(c.channelID, Event{Type: "presence", Online: h.countInChannel(c.channelID)})
				h.setVoiceMember(c.channelID, c.user.ID, false) // a disconnect leaves voice
			}
		case m := <-h.broadcast:
			msg := m
			h.emitToChannel(msg.ChannelID, Event{Type: "message", Message: &msg})
		case te := <-h.events:
			h.emitToChannel(te.channelID, te.event)
			// Voice-join/leave additionally update the channel's voice-presence set.
			switch te.event.Type {
			case "voice-join":
				h.setVoiceMember(te.channelID, te.event.From, true)
			case "voice-leave":
				h.setVoiceMember(te.channelID, te.event.From, false)
			}
		case ev := <-h.evict:
			// Collect first (don't mutate the map while detecting matches), then drop
			// each — mirrors the unregister/emitToChannel drop: delete + close(done),
			// guarded so `done` is closed at most once. writePump then sends a Close
			// frame and readPump's deferred unregister becomes a safe no-op.
			var hit []*Client
			for c := range h.clients {
				if c.user.ID == ev.userID && ev.channels[c.channelID] {
					hit = append(hit, c)
				}
			}
			for _, c := range hit {
				if _, ok := h.clients[c]; ok {
					delete(h.clients, c)
					close(c.done)
					h.emitToChannel(c.channelID, Event{Type: "presence", Online: h.countInChannel(c.channelID)})
					h.setVoiceMember(c.channelID, c.user.ID, false) // eviction leaves voice
				}
			}
		case req := <-h.online:
			set := make(map[int64]bool, len(h.clients))
			for c := range h.clients {
				set[c.user.ID] = true
			}
			req.reply <- set
		case req := <-h.voice:
			out := make(map[int64][]int64)
			for _, cid := range req.channelIDs {
				if set := h.voiceMembers[cid]; len(set) > 0 {
					ids := make([]int64, 0, len(set))
					for id := range set {
						ids = append(ids, id)
					}
					sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
					out[cid] = ids
				}
			}
			req.reply <- out
		case um := <-h.toUser:
			for c := range h.clients {
				if c.user.ID != um.userID {
					continue
				}
				select {
				case c.send <- um.data:
				default:
					// Stuck consumer: drop it (same policy as emitToChannel).
					close(c.done)
					delete(h.clients, c)
				}
			}
		}
	}
}

// setVoiceMember adds/removes userID to channelID's voice set and broadcasts the new
// voice-presence when it actually changed. MUST run on the hub goroutine (mutates the map).
func (h *Hub) setVoiceMember(channelID, userID int64, joined bool) {
	set := h.voiceMembers[channelID]
	if joined {
		if set == nil {
			set = make(map[int64]bool)
			h.voiceMembers[channelID] = set
		}
		if set[userID] {
			return // already in voice — no change
		}
		set[userID] = true
	} else {
		if set == nil || !set[userID] {
			return // wasn't in voice — no change
		}
		delete(set, userID)
		if len(set) == 0 {
			delete(h.voiceMembers, channelID)
		}
	}
	h.emitVoicePresence(channelID)
}

// emitVoicePresence broadcasts the current voice-call roster of channelID to that channel.
func (h *Hub) emitVoicePresence(channelID int64) {
	set := h.voiceMembers[channelID]
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	h.emitToChannel(channelID, Event{Type: "voice-presence", VoiceMembers: ids})
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
// Runs only on the hub goroutine, so closing `done` here is serialized with the
// unregister path — `done` is closed at most once per client.
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
			// Stuck consumer: drop it. Signal via done, never close(send) —
			// other goroutines write to send and would panic.
			close(c.done)
			delete(h.clients, c)
		}
	}
}
