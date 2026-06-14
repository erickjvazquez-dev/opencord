package ws_test

import (
	"context"
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
