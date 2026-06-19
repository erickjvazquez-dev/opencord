package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/db"
	"github.com/erickjvazquez-dev/opencord/internal/ws"
	gws "github.com/gorilla/websocket"
)

// The WebSocket gateway is the realtime data path and the most attacker-exposed
// surface (Rule B). ServeWS enforces auth (401) and per-channel access (403)
// BEFORE upgrading the connection, so a hostile client can't read or stream a
// channel it has no rights to. The store's CanAccessChannel is unit-tested; this
// suite proves the *gateway* wires it correctly — rejecting at the handshake and
// admitting a member through to history. SKIPS without DATABASE_URL (CI provides
// a postgres service), matching the store/httpapi integration suites.

const wsTestSecret = "ws-itest-secret"

type wsHarness struct {
	srv     *httptest.Server
	authsvc *auth.Service
	store   *chat.Store
	hub     *ws.Hub
}

func newWSHarness(t *testing.T) wsHarness {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping WS integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	authsvc := auth.New(pool, []byte(wsTestSecret), time.Hour)
	store := chat.NewStore(pool)
	hub := ws.NewHub(store)
	go hub.Run() // register/history fan-out is drained by Run; the success dial needs it.
	srv := httptest.NewServer(ws.ServeWS(hub, authsvc, store))
	t.Cleanup(srv.Close)
	return wsHarness{srv: srv, authsvc: authsvc, store: store, hub: hub}
}

var wsUserSeq int64

func (h wsHarness) user(t *testing.T) (auth.User, string) {
	t.Helper()
	name := fmt.Sprintf("ws_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&wsUserSeq, 1))
	u, err := h.authsvc.Register(context.Background(), name, "password123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tok, err := h.authsvc.Issue(u)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return u, tok
}

// getStatus issues a plain (non-WebSocket) GET and returns the status code. The
// pre-upgrade rejection paths (401/403/400) answer with normal HTTP, so this is
// enough to assert them without a handshake.
func getStatus(t *testing.T, base, query string) int {
	t.Helper()
	req, err := http.NewRequest("GET", base+query, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// dial attempts a real WebSocket handshake against the harness; returns the
// connection (nil on failure) and the HTTP status the server answered with.
func (h wsHarness) dial(t *testing.T, query string) (*gws.Conn, int) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(h.srv.URL, "http") + query
	d := gws.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, resp, err := d.Dial(u, nil)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if err != nil && conn == nil {
		return nil, status
	}
	return conn, status
}

func TestServeWSAccessControlIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()

	owner, _ := h.user(t)
	member, memberTok := h.user(t)
	_, strangerTok := h.user(t)

	srv, err := h.store.CreateServer(ctx, owner.ID, "WS Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	code, err := h.store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := h.store.RedeemInvite(ctx, code, member.ID); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	ch, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	chQ := fmt.Sprintf("?channel=%d", ch.ID)

	t.Run("no token is 401", func(t *testing.T) {
		if got := getStatus(t, h.srv.URL, chQ); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})

	t.Run("garbage token is 401", func(t *testing.T) {
		if got := getStatus(t, h.srv.URL, chQ+"&token=not-a-jwt"); got != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", got)
		}
	})

	t.Run("malformed channel id is 400", func(t *testing.T) {
		if got := getStatus(t, h.srv.URL, "?channel=abc&token="+memberTok); got != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", got)
		}
	})

	t.Run("non-member is 403 (no leak before upgrade)", func(t *testing.T) {
		if got := getStatus(t, h.srv.URL, chQ+"&token="+strangerTok); got != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", got)
		}
	})

	t.Run("non-member handshake refused 403", func(t *testing.T) {
		conn, status := h.dial(t, chQ+"&token="+strangerTok)
		if conn != nil {
			conn.Close()
			t.Fatal("stranger upgraded to a channel they can't access")
		}
		if status != http.StatusForbidden {
			t.Fatalf("handshake status = %d, want 403", status)
		}
	})

	t.Run("member connects (token via query) and receives history", func(t *testing.T) {
		conn, _ := h.dial(t, chQ+"&token="+memberTok)
		if conn == nil {
			t.Fatal("member failed to upgrade to a channel they belong to")
		}
		defer conn.Close()
		// History is queued unconditionally on connect; presence may interleave, so
		// read a few frames until we see the history event.
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		sawHistory := false
		for i := 0; i < 5 && !sawHistory; i++ {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			if strings.Contains(string(data), `"type":"history"`) {
				sawHistory = true
			}
		}
		if !sawHistory {
			t.Fatal("member did not receive the history event after connecting")
		}
	})
}

// TestServeWSRateLimitIntegration is the abuse-protection guard (Rule 15): a single
// connection that floods the gateway must be throttled by the per-connection token
// bucket (burst rateBurst, +rateRefillPerSec/s) so most of a rapid burst is dropped
// before it is persisted. Without the limiter every frame would be saved.
func TestServeWSRateLimitIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()
	owner, ownerTok := h.user(t)
	srv, err := h.store.CreateServer(ctx, owner.ID, "Flood Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}

	conn, status := h.dial(t, fmt.Sprintf("?channel=%d&token=%s", ch.ID, ownerTok))
	if conn == nil {
		t.Fatalf("dial failed (status %d)", status)
	}
	defer conn.Close()
	// Drain server->client frames so the server's writePump never blocks on us.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Flood far beyond the burst budget as fast as the socket allows.
	const flood = 30
	for i := 0; i < flood; i++ {
		if err := conn.WriteMessage(gws.TextMessage, []byte(fmt.Sprintf(`{"body":"flood-%d"}`, i))); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	// Poll until the burst has landed (a loaded machine may need well over 400ms to
	// process the flood), then assert throttling dropped the rest. Polling the lower
	// bound de-flakes without weakening the check: a broken limiter would let all
	// `flood` through and the upper-bound assert below would still catch it.
	saved := 0
	for i := 0; i < 40; i++ { // ~4s max
		msgs, err := h.store.Recent(ctx, ch.ID, owner.ID, 100)
		if err != nil {
			t.Fatalf("recent: %v", err)
		}
		saved = 0
		for _, m := range msgs {
			if strings.HasPrefix(m.Body, "flood-") {
				saved++
			}
		}
		if saved >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if saved >= flood {
		t.Fatalf("rate limiter dropped nothing: saved %d of %d sent", saved, flood)
	}
	if saved < 3 {
		t.Fatalf("rate limiter dropped too much (burst should let ~5 through): saved %d", saved)
	}
	t.Logf("rate limiter: %d of %d flooded messages persisted, rest throttled", saved, flood)
}

// TestServeWSVoiceSignalingIntegration verifies the mesh-voice signaling relay: the
// server stamps the sender and rebroadcasts voice-join / voice-signal to the channel,
// carrying from/target/signal so peers can establish WebRTC connections.
func TestServeWSVoiceSignalingIntegration(t *testing.T) {
	h := newWSHarness(t)
	a, aTok := h.user(t)
	b, bTok := h.user(t)

	// Both connect to the default (global) channel.
	connA, sA := h.dial(t, "?token="+aTok)
	if connA == nil {
		t.Fatalf("dial A failed (status %d)", sA)
	}
	defer connA.Close()
	connB, sB := h.dial(t, "?token="+bTok)
	if connB == nil {
		t.Fatalf("dial B failed (status %d)", sB)
	}
	defer connB.Close()

	type frame struct {
		Type         string          `json:"type"`
		From         int64           `json:"from"`
		Target       int64           `json:"target"`
		Signal       json.RawMessage `json:"signal"`
		On           bool            `json:"on"`
		StreamID     string          `json:"streamId"`
		Kind         string          `json:"kind"`
		VoiceMembers []int64         `json:"voiceMembers"`
	}
	// readUntil reads frames on conn (with a deadline) until one of typ arrives.
	readUntil := func(conn *gws.Conn, typ string) frame {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("waiting for %q: %v", typ, err)
			}
			var f frame
			if json.Unmarshal(data, &f) == nil && f.Type == typ {
				return f
			}
		}
	}

	contains := func(ids []int64, want int64) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}

	// A joins voice → B sees voice-join stamped with A's id, AND a voice-presence listing A.
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-join"}`)); err != nil {
		t.Fatalf("A voice-join: %v", err)
	}
	if join := readUntil(connB, "voice-join"); join.From != a.ID {
		t.Fatalf("voice-join from = %d, want A (%d)", join.From, a.ID)
	}
	if vp := readUntil(connB, "voice-presence"); !contains(vp.VoiceMembers, a.ID) {
		t.Fatalf("voice-presence after A joins = %v, want to include A (%d)", vp.VoiceMembers, a.ID)
	}
	// The HTTP-facing query (used by the cross-channel sidebar, slice 2) sees A too.
	if dch, err := h.store.DefaultChannelID(context.Background()); err == nil {
		if vm := h.hub.VoiceMembersFor([]int64{dch}); !contains(vm[dch], a.ID) {
			t.Fatalf("VoiceMembersFor[%d] = %v, want to include A (%d)", dch, vm[dch], a.ID)
		}
	}
	// A leaves voice → voice-presence drops A.
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-leave"}`)); err != nil {
		t.Fatalf("A voice-leave: %v", err)
	}
	if vp := readUntil(connB, "voice-presence"); contains(vp.VoiceMembers, a.ID) {
		t.Fatalf("voice-presence after A leaves = %v, must NOT include A (%d)", vp.VoiceMembers, a.ID)
	}

	// A sends a directed offer to B → B sees voice-signal from A, target B, signal intact.
	if err := connA.WriteMessage(gws.TextMessage, []byte(fmt.Sprintf(`{"type":"voice-signal","target":%d,"signal":{"sdp":"v=0"}}`, b.ID))); err != nil {
		t.Fatalf("A voice-signal: %v", err)
	}
	sig := readUntil(connB, "voice-signal")
	if sig.From != a.ID || sig.Target != b.ID {
		t.Fatalf("voice-signal from=%d target=%d, want from=%d target=%d", sig.From, sig.Target, a.ID, b.ID)
	}
	if !strings.Contains(string(sig.Signal), "v=0") {
		t.Fatalf("voice-signal payload not relayed intact: %s", sig.Signal)
	}

	// A starts screen sharing → B sees voice-screen on=true from A, carrying the
	// screen MediaStream id (so B can tell the screen's tracks from the mic) and the
	// kind ("screen").
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-screen","on":true,"streamId":"screen-stream-123","kind":"screen"}`)); err != nil {
		t.Fatalf("A voice-screen on: %v", err)
	}
	scr := readUntil(connB, "voice-screen")
	if scr.From != a.ID || !scr.On || scr.StreamID != "screen-stream-123" || scr.Kind != "screen" {
		t.Fatalf("voice-screen on: from=%d on=%v streamId=%q kind=%q, want from=%d on=true streamId=screen-stream-123 kind=screen",
			scr.From, scr.On, scr.StreamID, scr.Kind, a.ID)
	}

	// A turns the camera on → B sees voice-screen on=true with kind="camera" (video
	// calling reuses the same frame; the kind tags it as the camera, not the screen).
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-screen","on":true,"streamId":"cam-stream-9","kind":"camera"}`)); err != nil {
		t.Fatalf("A voice-screen camera on: %v", err)
	}
	cam := readUntil(connB, "voice-screen")
	if cam.From != a.ID || !cam.On || cam.StreamID != "cam-stream-9" || cam.Kind != "camera" {
		t.Fatalf("voice-screen camera: from=%d on=%v streamId=%q kind=%q, want from=%d on=true streamId=cam-stream-9 kind=camera",
			cam.From, cam.On, cam.StreamID, cam.Kind, a.ID)
	}

	// Rule B: a hostile/unknown kind must NOT be relayed — the frame is dropped. We
	// prove it by sending a garbage-kind frame, then a valid one, and asserting B's
	// next voice-screen is the VALID one (the garbage never arrived).
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-screen","on":true,"streamId":"evil","kind":"<script>"}`)); err != nil {
		t.Fatalf("A voice-screen bad kind: %v", err)
	}
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-screen","on":true,"streamId":"good-stream","kind":"screen"}`)); err != nil {
		t.Fatalf("A voice-screen good after bad: %v", err)
	}
	good := readUntil(connB, "voice-screen")
	if good.StreamID != "good-stream" || good.Kind != "screen" {
		t.Fatalf("hostile kind leaked: got streamId=%q kind=%q, want the garbage frame dropped and streamId=good-stream kind=screen",
			good.StreamID, good.Kind)
	}

	// A stops sharing → B sees voice-screen on=false (and no stream id / kind leaks).
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-screen","on":false}`)); err != nil {
		t.Fatalf("A voice-screen off: %v", err)
	}
	off := readUntil(connB, "voice-screen")
	if off.From != a.ID || off.On || off.StreamID != "" || off.Kind != "" {
		t.Fatalf("voice-screen off: from=%d on=%v streamId=%q kind=%q, want from=%d on=false empty streamId+kind",
			off.From, off.On, off.StreamID, off.Kind, a.ID)
	}
}

// TestServeWSVoiceFloodGuard is the voice abuse-protection guard (Rule 15). Voice
// signaling is exempt from the strict text bucket (ICE is legitimately bursty), but
// must NOT be unbounded: every voice frame is fanned out to the whole channel, so an
// unthrottled flood is an amplification DoS. A connection that floods voice-signal
// frames must be capped by the dedicated voice bucket (burst voiceBurst,
// +voiceRefillPerSec/s) — most of a rapid flood is dropped before rebroadcast, while
// a legitimate setup burst still gets through.
func TestServeWSVoiceFloodGuard(t *testing.T) {
	h := newWSHarness(t)
	_, aTok := h.user(t)
	b, bTok := h.user(t)

	connA, sA := h.dial(t, "?token="+aTok)
	if connA == nil {
		t.Fatalf("dial A failed (status %d)", sA)
	}
	defer connA.Close()
	connB, sB := h.dial(t, "?token="+bTok)
	if connB == nil {
		t.Fatalf("dial B failed (status %d)", sB)
	}
	defer connB.Close()

	// A drains too: the relay fans each voice frame back to the whole channel
	// (sender included), so if A never reads, its own echoes fill its buffer and
	// the hub drops it — which would end the flood early and skew the measurement.
	go func() {
		for {
			if _, _, err := connA.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// B drains continuously (so the hub never blocks on a full send buffer) and
	// counts the voice-signals it actually receives = those that passed A's bucket.
	var received int64
	go func() {
		for {
			_, data, err := connB.ReadMessage()
			if err != nil {
				return
			}
			var f struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &f) == nil && f.Type == "voice-signal" {
				atomic.AddInt64(&received, 1)
			}
		}
	}()

	payload := []byte(fmt.Sprintf(`{"type":"voice-signal","target":%d,"signal":{"c":"x"}}`, b.ID))
	// send writes n frames from A. `fatalOnErr` is true only for the legit burst:
	// there a write failure is a real regression (the guard must not drop/close a
	// legitimate client). During a flood it's NOT — the server defending itself is
	// the guard working: the voice bucket drops most frames, and the hub may still
	// evict a client that floods faster than it drains its own fanned-back echoes
	// (a TCP reset / close mid-flood). Treating that as fatal made the test flaky
	// (it failed at a different frame each run). So in the flood phase we stop on the
	// first write error and proceed to measure what B actually received — which only
	// strengthens the "flood is bounded" assertion.
	send := func(n int, fatalOnErr bool) {
		for i := 0; i < n; i++ {
			if err := connA.WriteMessage(gws.TextMessage, payload); err != nil {
				if fatalOnErr {
					t.Fatalf("write %d: %v", i, err)
				}
				return
			}
		}
	}

	// Phase 1 — a legitimate setup-sized burst (well within the bucket and B's
	// buffer) must be relayed IN FULL: the guard must not false-drop real ICE.
	const legit = 25
	send(legit, true)
	// Poll until the legit burst is fully relayed (a loaded machine may need >400ms to
	// process it) — the guard must not false-drop real ICE. Same de-flake as the
	// rate-limit test: a fixed sleep here false-reds the gate under concurrent load.
	relayed := int64(0)
	for i := 0; i < 40; i++ {
		relayed = atomic.LoadInt64(&received)
		if relayed >= legit {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if relayed < legit {
		t.Fatalf("voice guard throttled a legitimate burst: relayed %d of %d", relayed, legit)
	}
	atomic.StoreInt64(&received, 0)

	// Phase 2 — a flood far beyond the bucket must be bounded: most frames are
	// dropped at the source, never amplified to the channel.
	const flood = 500
	send(flood, false)
	time.Sleep(500 * time.Millisecond)
	got := atomic.LoadInt64(&received)
	if got >= flood {
		t.Fatalf("voice flood guard dropped nothing: relayed %d of %d sent", got, flood)
	}
	if got > flood/2 {
		t.Fatalf("voice flood guard too leaky: relayed %d of %d sent", got, flood)
	}
	t.Logf("voice flood guard: legit burst relayed in full; flood of %d bounded to %d", flood, got)
}

// TestServeWSHostileFrameHandling is the inbound-frame adversarial guard (Rule B/15):
// a VALID, authenticated connection that sends hostile MESSAGE frames must never crash
// the gateway or persist the hostile content — and the connection must stay usable
// afterwards. Covers non-JSON garbage, a type-confused field, empty/whitespace/oversized
// bodies, and a hostile replyTo (bogus / negative id) on the reply path. Frames are paced
// ~600ms apart so the per-connection rate bucket (burst 5, +2/s) refills and each frame is
// actually PROCESSED by readPump — isolating frame-handling from rate limiting.
func TestServeWSHostileFrameHandling(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()
	owner, ownerTok := h.user(t)
	srv, err := h.store.CreateServer(ctx, owner.ID, "Hostile Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}

	conn, status := h.dial(t, fmt.Sprintf("?channel=%d&token=%s", ch.ID, ownerTok))
	if conn == nil {
		t.Fatalf("dial failed (status %d)", status)
	}
	defer conn.Close()
	// Drain server->client frames so the writePump never blocks on us.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// > maxMessageSize (4096, a private const in package ws) — must be dropped at the
	// size bound, never persisted.
	oversized := strings.Repeat("A", 5000)
	send := func(frame string) {
		t.Helper()
		if err := conn.WriteMessage(gws.TextMessage, []byte(frame)); err != nil {
			t.Fatalf("a hostile frame closed the connection (DoS): %v", err)
		}
		time.Sleep(600 * time.Millisecond) // let the rate bucket refill (+2/s)
	}

	// Battery of hostile frames — none may crash the connection or persist content.
	send(`this is not json at all }{`)        // unparseable → dropped (pre-rate-limit)
	send(`{"body":12345}`)                     // type confusion (number for string) → unmarshal fails → dropped
	send(`{"type":"message","body":""}`)       // empty body → dropped
	send(`{"body":"   \t  "}`)                  // whitespace-only → trims to empty → dropped
	send(`{"body":"` + oversized + `"}`)        // oversized → dropped at the size bound
	// Legit body + hostile replyTo: the message MUST persist, but the bogus/negative
	// reference must be DROPPED (Rule B), leaving ReplyTo nil.
	send(`{"body":"ok-bogus-reply","replyTo":999999999}`)
	send(`{"body":"ok-neg-reply","replyTo":-1}`)
	// Final legit message: if it lands, the connection survived the whole battery.
	send(`{"body":"final-legit"}`)

	// Poll for the final legit message to persist (under load the last frame may take a
	// moment) — once it's there, every earlier frame's fate is settled too, since it was
	// the last thing sent. Avoids a fixed-wait false-red in the gate.
	byBody := map[string]chat.Message{}
	for i := 0; i < 40; i++ {
		msgs, err := h.store.Recent(ctx, ch.ID, owner.ID, 200)
		if err != nil {
			t.Fatalf("recent: %v", err)
		}
		byBody = map[string]chat.Message{}
		for _, m := range msgs {
			byBody[m.Body] = m
		}
		if _, ok := byBody["final-legit"]; ok {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 1. Connection survived the battery → the final legit message landed.
	if _, ok := byBody["final-legit"]; !ok {
		t.Fatal("connection did not survive the hostile battery (final legit message never persisted)")
	}
	// 2. No hostile content persisted.
	if _, ok := byBody[oversized]; ok {
		t.Fatal("oversized body (> maxMessageSize) was persisted — size bound bypassed")
	}
	for _, banned := range []string{"", "12345", `this is not json at all }{`} {
		if _, ok := byBody[banned]; ok {
			t.Fatalf("hostile frame content was persisted: %q", banned)
		}
	}
	// 3. The legit-body / hostile-replyTo messages persisted but with NO reply ref.
	for _, body := range []string{"ok-bogus-reply", "ok-neg-reply"} {
		m, ok := byBody[body]
		if !ok {
			t.Fatalf("%q (legit body, bad replyTo) should have persisted", body)
		}
		if m.ReplyTo != nil {
			t.Fatalf("%q kept a hostile reply reference (replyTo=%v) — should be dropped", body, *m.ReplyTo)
		}
	}
	t.Log("hostile-frame guard: connection survived the battery; no hostile content persisted; bad replyTo dropped")
}

// TestServeWSFrameSizeLimitIntegration proves the TRANSPORT-level read limit: a single inbound
// frame larger than maxFrameSize (16384) is rejected by SetReadLimit, which makes the server stop
// reading and CLOSE the connection (gorilla sends a 1009 "message too big" close). This is a
// DISTINCT defense from the app-level maxMessageSize (4096) body check exercised above: a 5 KiB
// BODY is silently dropped while the connection survives, but a >16 KiB FRAME must kill the
// connection — so a hostile client cannot stream an unbounded frame to exhaust server memory
// (Rule B / Rule 15: bound every inbound payload, reject early).
func TestServeWSFrameSizeLimitIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()
	owner, ownerTok := h.user(t)
	srv, err := h.store.CreateServer(ctx, owner.ID, "Frame Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}

	conn, status := h.dial(t, fmt.Sprintf("?channel=%d&token=%s", ch.ID, ownerTok))
	if conn == nil {
		t.Fatalf("dial failed (status %d)", status)
	}
	defer conn.Close()

	// One frame well over maxFrameSize (16384). The write may succeed (it's buffered to the
	// socket) even though the server reads only up to the limit before closing.
	huge := strings.Repeat("A", 20000)
	if err := conn.WriteMessage(gws.TextMessage, []byte(`{"type":"message","body":"`+huge+`"}`)); err != nil {
		t.Logf("oversized frame surfaced the close on write (also a valid rejection): %v", err)
	}

	// The server must close the connection in response: read until we get an error (the 1009
	// close, or EOF). Initial history/presence frames read cleanly first; the close follows.
	// Crucially, distinguish a real CLOSE (server enforced the limit) from a read TIMEOUT (the
	// connection stayed OPEN and our own deadline fired) — only the close proves the guard. A
	// timeout = FAIL, otherwise this test would falsely pass if SetReadLimit were removed.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var readErr error
	for i := 0; i < 200; i++ {
		if _, _, err := conn.ReadMessage(); err != nil {
			readErr = err
			break
		}
	}
	if readErr == nil {
		t.Fatal("connection never errored after a >maxFrameSize frame — read limit not enforced")
	}
	if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
		t.Fatalf("connection stayed OPEN after a >maxFrameSize frame (read timed out, no close) — SetReadLimit not enforced; a hostile client could stream an unbounded frame: %v", readErr)
	}

	// And the oversized content was never persisted (it was never a complete, parseable message).
	msgs, err := h.store.Recent(ctx, ch.ID, owner.ID, 50)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	for _, m := range msgs {
		if m.Body == huge {
			t.Fatal("oversized-frame content was persisted — frame size bound bypassed")
		}
	}
	t.Log("frame-size guard: a >16 KiB frame closed the connection; nothing persisted")
}

// wsWaitForBody reads frames from conn until it sees a message with the given body
// or `within` elapses. Returns false on any read error (closed/timeout) — useful
// both to assert receipt and (negatively) non-receipt.
func wsWaitForBody(t *testing.T, conn *gws.Conn, body string, within time.Duration) bool {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(within))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return false
		}
		if strings.Contains(string(data), `"body":"`+body+`"`) {
			return true
		}
	}
}

// TestServeWSEvictOnKickIntegration is the security proof for kicking (Rule 15). WS
// channel access is checked only at connect (ServeWS→CanAccessChannel), so an already
// open socket would keep streaming the channel after the user loses membership. The
// kick path (store.RemoveServerMember + Hub.EvictUserFromChannels) must terminate the
// kicked user's live socket so they immediately stop receiving — while a still
// connected owner is unaffected.
func TestServeWSEvictOnKickIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()

	owner, ownerTok := h.user(t)
	member, memberTok := h.user(t)

	srv, err := h.store.CreateServer(ctx, owner.ID, "Evict Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := h.store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ch, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	chQ := fmt.Sprintf("?channel=%d&token=", ch.ID)

	ownerConn, _ := h.dial(t, chQ+ownerTok)
	if ownerConn == nil {
		t.Fatal("owner failed to connect")
	}
	defer ownerConn.Close()
	memberConn, _ := h.dial(t, chQ+memberTok)
	if memberConn == nil {
		t.Fatal("member failed to connect")
	}
	defer memberConn.Close()

	// Sanity: while still a member, the member receives the owner's message live.
	if err := ownerConn.WriteMessage(gws.TextMessage, []byte(`{"body":"before kick"}`)); err != nil {
		t.Fatalf("owner write: %v", err)
	}
	if !wsWaitForBody(t, memberConn, "before kick", 4*time.Second) {
		t.Fatal("member should receive the owner's message BEFORE being kicked")
	}

	// Notify THEN evict — exactly what the DELETE route does. The notice must be
	// delivered before the socket closes (writePump flushes pending frames on close).
	if err := h.store.RemoveServerMember(ctx, srv.ID, owner.ID, member.ID); err != nil {
		t.Fatalf("kick: %v", err)
	}
	h.hub.SendToUser(member.ID, ws.Event{Type: "server-removed", ServerID: srv.ID})
	h.hub.EvictUserFromChannels(member.ID, []int64{ch.ID})

	// The member's socket must (a) receive the "server-removed" notice carrying the
	// serverId, THEN (b) be CLOSED by eviction. Read frames: a close error proves
	// eviction; a timeout would mean it stayed open (false-positive guard).
	_ = memberConn.SetReadDeadline(time.Now().Add(4 * time.Second))
	gotNotice, evicted := false, false
	for i := 0; i < 40; i++ {
		_, data, err := memberConn.ReadMessage()
		if err == nil {
			if strings.Contains(string(data), `"server-removed"`) {
				gotNotice = true
				if !strings.Contains(string(data), fmt.Sprintf(`"serverId":%d`, srv.ID)) {
					t.Fatalf("server-removed notice missing serverId %d: %s", srv.ID, data)
				}
			}
			continue // keep reading until the socket closes
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			break // timed out with the socket still open → NOT evicted
		}
		evicted = true // a non-timeout read error = the server closed our socket
		break
	}
	if !gotNotice {
		t.Fatal("kicked member should receive a server-removed notice before the socket closes")
	}
	if !evicted {
		t.Fatal("kicked member's socket should be closed by eviction, but it stayed open (leak)")
	}

	// The channel still works for the owner (eviction was targeted, not a teardown).
	if err := ownerConn.WriteMessage(gws.TextMessage, []byte(`{"body":"after kick"}`)); err != nil {
		t.Fatalf("owner write 2: %v", err)
	}
	if !wsWaitForBody(t, ownerConn, "after kick", 4*time.Second) {
		t.Fatal("the owner — still connected — should receive a message posted after the kick")
	}
	t.Log("kick eviction: member's live socket closed; owner unaffected")
}

// TestHubOnlineUserIDsIntegration proves the presence query: a user is reported
// online iff they hold a live WS connection. Drives a real dial/close and polls the
// hub set (register/unregister land asynchronously on the hub goroutine).
func TestHubOnlineUserIDsIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()
	owner, ownerTok := h.user(t)
	srv, err := h.store.CreateServer(ctx, owner.ID, "Presence Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}

	// Offline before connecting.
	if h.hub.OnlineUserIDs()[owner.ID] {
		t.Fatal("user should be offline before connecting")
	}

	conn, _ := h.dial(t, fmt.Sprintf("?channel=%d&token=%s", ch.ID, ownerTok))
	if conn == nil {
		t.Fatal("dial failed")
	}
	online := false
	for i := 0; i < 40 && !online; i++ {
		online = h.hub.OnlineUserIDs()[owner.ID]
		if !online {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !online {
		t.Fatal("user should be online after connecting")
	}

	_ = conn.Close()
	offline := false
	for i := 0; i < 60 && !offline; i++ {
		offline = !h.hub.OnlineUserIDs()[owner.ID]
		if !offline {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !offline {
		t.Fatal("user should be offline after disconnecting")
	}
}

// TestServeWSGroupDMFanoutIntegration proves v0.6 group-DM realtime: a message sent in a
// group DM fans out to ALL its members over their live sockets (the per-channel hub makes
// this "free" — and "free" is exactly what silently regresses), while a non-member's
// handshake to the group channel is refused (WS access control holds for groups, Rule 15).
func TestServeWSGroupDMFanoutIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()
	a, aTok := h.user(t)
	b, bTok := h.user(t)
	c, cTok := h.user(t)
	_, strangerTok := h.user(t)

	g, err := h.store.CreateGroupDM(ctx, a.ID, []int64{b.ID, c.ID})
	if err != nil {
		t.Fatalf("create group dm: %v", err)
	}
	chQ := fmt.Sprintf("?channel=%d&token=", g.ID)

	connA, _ := h.dial(t, chQ+aTok)
	if connA == nil {
		t.Fatal("A (member) failed to connect to the group")
	}
	defer connA.Close()
	connB, _ := h.dial(t, chQ+bTok)
	if connB == nil {
		t.Fatal("B (member) failed to connect to the group")
	}
	defer connB.Close()
	connC, _ := h.dial(t, chQ+cTok)
	if connC == nil {
		t.Fatal("C (member) failed to connect to the group")
	}
	defer connC.Close()

	// A non-member is refused the group channel handshake (403) — group access control.
	if conn, status := h.dial(t, chQ+strangerTok); conn != nil {
		conn.Close()
		t.Fatal("a non-member upgraded to a group DM channel they can't access")
	} else if status != http.StatusForbidden {
		t.Fatalf("non-member group handshake = %d, want 403", status)
	}

	// A sends ONE message; BOTH other members must receive it live (fanout to all N).
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"body":"hi group"}`)); err != nil {
		t.Fatalf("A write: %v", err)
	}
	if !wsWaitForBody(t, connB, "hi group", 4*time.Second) {
		t.Fatal("member B should receive A's group message")
	}
	if !wsWaitForBody(t, connC, "hi group", 4*time.Second) {
		t.Fatal("member C should receive A's group message")
	}
}

// TestServeWSThreadFanoutIntegration proves v0.8 thread realtime: a message in a thread fans
// out to all the parent server's members over their live sockets (threads reuse the
// channel-id-scoped hub), while a non-member of the parent server is refused the thread
// handshake (access is inherited from the parent, Rule 15).
func TestServeWSThreadFanoutIntegration(t *testing.T) {
	h := newWSHarness(t)
	ctx := context.Background()
	owner, ownerTok := h.user(t)
	b, bTok := h.user(t)
	c, cTok := h.user(t)
	_, strangerTok := h.user(t)

	srv, err := h.store.CreateServer(ctx, owner.ID, "Thread Realtime")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	if err := h.store.AddServerMember(ctx, srv.ID, b.ID); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := h.store.AddServerMember(ctx, srv.ID, c.ID); err != nil {
		t.Fatalf("add c: %v", err)
	}
	parent, err := h.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	thread, err := h.store.CreateThread(ctx, parent.ID, "Realtime Thread", nil)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	chQ := fmt.Sprintf("?channel=%d&token=", thread.ID)

	connA, _ := h.dial(t, chQ+ownerTok)
	if connA == nil {
		t.Fatal("owner failed to connect to the thread")
	}
	defer connA.Close()
	connB, _ := h.dial(t, chQ+bTok)
	if connB == nil {
		t.Fatal("member B failed to connect to the thread")
	}
	defer connB.Close()
	connC, _ := h.dial(t, chQ+cTok)
	if connC == nil {
		t.Fatal("member C failed to connect to the thread")
	}
	defer connC.Close()

	// A non-member of the parent server is refused the thread handshake (inherited access).
	if conn, status := h.dial(t, chQ+strangerTok); conn != nil {
		conn.Close()
		t.Fatal("a non-member upgraded to a thread they can't access")
	} else if status != http.StatusForbidden {
		t.Fatalf("non-member thread handshake = %d, want 403", status)
	}

	// A posts in the thread; B and C both receive it live.
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"body":"in the thread"}`)); err != nil {
		t.Fatalf("A write: %v", err)
	}
	if !wsWaitForBody(t, connB, "in the thread", 4*time.Second) {
		t.Fatal("member B should receive A's thread message")
	}
	if !wsWaitForBody(t, connC, "in the thread", 4*time.Second) {
		t.Fatal("member C should receive A's thread message")
	}
}
