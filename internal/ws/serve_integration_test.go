package ws_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	return wsHarness{srv: srv, authsvc: authsvc, store: store}
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
	time.Sleep(400 * time.Millisecond) // let the server process the burst

	msgs, err := h.store.Recent(ctx, ch.ID, owner.ID, 100)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	saved := 0
	for _, m := range msgs {
		if strings.HasPrefix(m.Body, "flood-") {
			saved++
		}
	}
	if saved >= flood {
		t.Fatalf("rate limiter dropped nothing: saved %d of %d sent", saved, flood)
	}
	if saved < 3 {
		t.Fatalf("rate limiter dropped too much (burst should let ~%d through): saved %d", int(5), saved)
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
		Type   string          `json:"type"`
		From   int64           `json:"from"`
		Target int64           `json:"target"`
		Signal json.RawMessage `json:"signal"`
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

	// A joins voice → B sees voice-join stamped with A's id.
	if err := connA.WriteMessage(gws.TextMessage, []byte(`{"type":"voice-join"}`)); err != nil {
		t.Fatalf("A voice-join: %v", err)
	}
	if join := readUntil(connB, "voice-join"); join.From != a.ID {
		t.Fatalf("voice-join from = %d, want A (%d)", join.From, a.ID)
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
	send := func(n int) {
		for i := 0; i < n; i++ {
			if err := connA.WriteMessage(gws.TextMessage, payload); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
	}

	// Phase 1 — a legitimate setup-sized burst (well within the bucket and B's
	// buffer) must be relayed IN FULL: the guard must not false-drop real ICE.
	const legit = 25
	send(legit)
	time.Sleep(400 * time.Millisecond)
	if got := atomic.LoadInt64(&received); got < legit {
		t.Fatalf("voice guard throttled a legitimate burst: relayed %d of %d", got, legit)
	}
	atomic.StoreInt64(&received, 0)

	// Phase 2 — a flood far beyond the bucket must be bounded: most frames are
	// dropped at the source, never amplified to the channel.
	const flood = 500
	send(flood)
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
