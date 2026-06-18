package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/config"
	"github.com/erickjvazquez-dev/opencord/internal/db"
	"github.com/erickjvazquez-dev/opencord/internal/httpapi"
	"github.com/erickjvazquez-dev/opencord/internal/ws"
)

// This suite exercises the REST authorization wiring in router.go end-to-end:
// the real chi router, the JWT middleware, and the error→HTTP-status mapping for
// the roles / moderation / read-only-channel surface. The store layer is covered
// by chat/store_integration_test.go; here we assert that the HTTP layer enforces
// the same rules and returns the right codes (401/403/404/400/2xx) to a hostile
// caller (Rule B / Rule 15). It SKIPS without DATABASE_URL, like the store suite,
// so `go test ./...` stays green locally; CI provides a postgres service.

const testSecret = "router-itest-secret"

// harness bundles the live handler plus the auth service used to mint tokens.
type harness struct {
	h       http.Handler
	authsvc *auth.Service
	store   *chat.Store
}

func newHarness(t *testing.T) harness {
	return newHarnessCfg(t, config.Config{CORSOrigin: "*"})
}

// newHarnessCfg builds the live router with a caller-supplied config (e.g. to set
// the optional SFU credentials). CORSOrigin defaults to "*" if unset.
func newHarnessCfg(t *testing.T, cfg config.Config) harness {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping HTTP integration test")
	}
	if cfg.CORSOrigin == "" {
		cfg.CORSOrigin = "*"
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
	authsvc := auth.New(pool, []byte(testSecret), time.Hour)
	store := chat.NewStore(pool)
	hub := ws.NewHub(store)
	go hub.Run() // REST delete/edit handlers fan out via the hub; drain it.
	return harness{h: httpapi.New(cfg, authsvc, store, hub), authsvc: authsvc, store: store}
}

var userSeq int64

// user registers a uniquely-named account and returns it with a signed token.
func (hs harness) user(t *testing.T) (auth.User, string) {
	t.Helper()
	name := fmt.Sprintf("rt_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&userSeq, 1))
	u, err := hs.authsvc.Register(context.Background(), name, "password123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	tok, err := hs.authsvc.Issue(u)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return u, tok
}

// req drives one request through the real router. An empty token omits the header.
func (hs harness) req(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, want int, what string) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("%s: status = %d, want %d (body: %s)", what, w.Code, want, strings.TrimSpace(w.Body.String()))
	}
}

// TestRouterAuthorizationIntegration walks an attacker through the role-gated REST
// surface: unauth, non-member, non-admin, owner, and a promoted admin — asserting
// the HTTP status at every boundary. Subtests run in order; later ones rely on the
// state set by earlier ones (member promoted to admin, channel created).
func TestRouterAuthorizationIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	// Arrange a server with a non-admin member and one open channel holding a
	// message owned by the owner (built via the store; the HTTP behavior is the
	// subject under test).
	srv, err := hs.store.CreateServer(ctx, owner.ID, "Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	code, err := hs.store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := hs.store.RedeemInvite(ctx, code, member.ID); err != nil {
		t.Fatalf("redeem invite: %v", err)
	}
	openCh, err := hs.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	msg, err := hs.store.Save(ctx, openCh.ID, owner.ID, owner.Username, "hello")
	if err != nil {
		t.Fatalf("save message: %v", err)
	}

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "GET", "/api/channels", "", ""), http.StatusUnauthorized, "no token GET /channels")
		wantStatus(t, hs.req(t, "GET", "/api/auth/me", ownerTok, ""), http.StatusOK, "owner GET /auth/me")
	})

	t.Run("server channels are members-only", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/channels", srv.ID)
		wantStatus(t, hs.req(t, "GET", p, strangerTok, ""), http.StatusForbidden, "stranger lists server channels")
		wantStatus(t, hs.req(t, "GET", p, memberTok, ""), http.StatusOK, "member lists server channels")
	})

	t.Run("voice-presence is members-only", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/voice-presence", srv.ID)
		wantStatus(t, hs.req(t, "GET", p, "", ""), http.StatusUnauthorized, "no-token voice-presence")
		wantStatus(t, hs.req(t, "GET", p, strangerTok, ""), http.StatusForbidden, "stranger voice-presence")
		// A member gets 200 with a JSON object (empty when no one is in voice in this harness).
		w := hs.req(t, "GET", p, memberTok, "")
		wantStatus(t, w, http.StatusOK, "member voice-presence")
		if body := strings.TrimSpace(w.Body.String()); !strings.HasPrefix(body, "{") {
			t.Fatalf("voice-presence body = %q, want a JSON object", body)
		}
	})

	var createdChannelID int64
	t.Run("channel creation is admin-gated", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/channels", srv.ID)
		wantStatus(t, hs.req(t, "POST", p, memberTok, `{"name":"member-made"}`), http.StatusForbidden, "non-admin creates channel")
		w := hs.req(t, "POST", p, ownerTok, `{"name":"announcements"}`)
		wantStatus(t, w, http.StatusCreated, "owner creates channel")
		var c chat.Channel
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatalf("decode created channel: %v", err)
		}
		// A text channel carries no kind on the wire (omitempty); voice channels do.
		if c.Kind != "" {
			t.Fatalf("text channel kind = %q, want empty", c.Kind)
		}
		createdChannelID = c.ID
	})

	t.Run("voice channels (kind='voice')", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/channels", srv.ID)
		// A non-admin cannot create a voice channel either (admin gate precedes kind parsing).
		wantStatus(t, hs.req(t, "POST", p, memberTok, `{"name":"member-voice","kind":"voice"}`), http.StatusForbidden, "non-admin creates voice channel")
		// An invalid kind is a 400, not a silent default.
		wantStatus(t, hs.req(t, "POST", p, ownerTok, `{"name":"bad-kind","kind":"stage"}`), http.StatusBadRequest, "owner sends bogus kind")
		// The owner creates a real voice channel → 201 carrying kind='voice'.
		w := hs.req(t, "POST", p, ownerTok, `{"name":"lounge","kind":"voice"}`)
		wantStatus(t, w, http.StatusCreated, "owner creates voice channel")
		var vc chat.Channel
		if err := json.Unmarshal(w.Body.Bytes(), &vc); err != nil {
			t.Fatalf("decode created voice channel: %v", err)
		}
		if vc.Kind != "voice" {
			t.Fatalf("voice channel kind = %q, want %q", vc.Kind, "voice")
		}
		// It surfaces with kind='voice' in the members' channel list; text channels stay kind=''.
		lw := hs.req(t, "GET", p, memberTok, "")
		wantStatus(t, lw, http.StatusOK, "member lists channels incl. voice")
		var chans []chat.Channel
		if err := json.Unmarshal(lw.Body.Bytes(), &chans); err != nil {
			t.Fatalf("decode channel list: %v", err)
		}
		var sawVoice, sawText bool
		for _, ch := range chans {
			switch ch.ID {
			case vc.ID:
				sawVoice = ch.Kind == "voice"
			case openCh.ID:
				sawText = ch.Kind == ""
			}
		}
		if !sawVoice {
			t.Fatalf("voice channel %d missing or not kind='voice' in list: %+v", vc.ID, chans)
		}
		if !sawText {
			t.Fatalf("text channel %d should list with empty kind: %+v", openCh.ID, chans)
		}
	})

	t.Run("role changes are owner-only", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/roles", srv.ID)
		// A non-owner cannot promote anyone (not even via a self-promotion attempt).
		wantStatus(t, hs.req(t, "POST", p, memberTok, fmt.Sprintf(`{"userId":%d,"role":"admin"}`, member.ID)), http.StatusForbidden, "member self-promotes")
		// An invalid role is a 400, not a silent success.
		wantStatus(t, hs.req(t, "POST", p, ownerTok, fmt.Sprintf(`{"userId":%d,"role":"superadmin"}`, member.ID)), http.StatusBadRequest, "owner sets bogus role")
		// The owner legitimately promotes the member to admin (used below).
		wantStatus(t, hs.req(t, "POST", p, ownerTok, fmt.Sprintf(`{"userId":%d,"role":"admin"}`, member.ID)), http.StatusNoContent, "owner promotes member")
	})

	t.Run("read-only policy is admin-gated", func(t *testing.T) {
		if createdChannelID == 0 {
			t.Skip("no channel id from creation subtest")
		}
		p := fmt.Sprintf("/api/channels/%d", createdChannelID)
		wantStatus(t, hs.req(t, "PATCH", p, strangerTok, `{"postPolicy":"admins"}`), http.StatusForbidden, "stranger sets policy")
		wantStatus(t, hs.req(t, "PATCH", p, ownerTok, `{"postPolicy":"nope"}`), http.StatusBadRequest, "owner sets bogus policy")
		wantStatus(t, hs.req(t, "PATCH", p, ownerTok, `{"postPolicy":"admins"}`), http.StatusNoContent, "owner sets read-only")
	})

	t.Run("moderation: only author or admin deletes", func(t *testing.T) {
		p := fmt.Sprintf("/api/messages/%d", msg.ID)
		// A non-member/non-author gets 404 (existence is not leaked).
		wantStatus(t, hs.req(t, "DELETE", p, strangerTok, ""), http.StatusNotFound, "stranger deletes others' message")
		// The promoted admin may moderate (delete) the owner's message.
		wantStatus(t, hs.req(t, "DELETE", p, memberTok, ""), http.StatusNoContent, "admin moderates message")
	})

	t.Run("malformed ids are 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", "/api/channels/abc", ownerTok, `{"postPolicy":"admins"}`), http.StatusBadRequest, "non-numeric channel id")
		wantStatus(t, hs.req(t, "DELETE", "/api/messages/xyz", ownerTok, ""), http.StatusBadRequest, "non-numeric message id")
	})
}

// TestRouterVoiceICEIntegration verifies POST /voice/token returns the configured ICE
// servers (STUN + optional TURN with creds) for the mesh path, and is auth-gated so TURN
// credentials never reach an anonymous caller.
func TestRouterVoiceICEIntegration(t *testing.T) {
	hs := newHarnessCfg(t, config.Config{
		CORSOrigin:   "*",
		STUNURL:      "stun:stun.example.com:3478",
		TURNURL:      "turn:turn.example.com:3478",
		TURNUsername: "u1",
		TURNPassword: "p1",
	})
	_, tok := hs.user(t)

	// Unauthenticated → 401 (TURN creds must not leak).
	wantStatus(t, hs.req(t, "POST", "/api/voice/token?channel=1", "", ""), http.StatusUnauthorized, "unauth voice token")

	// Authed mesh response carries STUN + TURN (the channel isn't checked in the
	// no-SFU path, so any channel id is fine here).
	w := hs.req(t, "POST", "/api/voice/token?channel=1", tok, "")
	wantStatus(t, w, http.StatusOK, "voice token")
	var resp struct {
		SFU        bool               `json:"sfu"`
		IceServers []config.IceServer `json:"iceServers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SFU {
		t.Fatal("sfu should be false (no SFU configured)")
	}
	if len(resp.IceServers) != 2 {
		t.Fatalf("iceServers = %+v, want STUN + TURN", resp.IceServers)
	}
	turn := resp.IceServers[1]
	if turn.URLs != "turn:turn.example.com:3478" || turn.Username != "u1" || turn.Credential != "p1" {
		t.Fatalf("TURN entry = %+v, want url+username+credential", turn)
	}
}

// TestRouterUnreadIntegration covers GET /unreads + POST /channels/{id}/read: auth
// required, access-gating on mark-read, and the unread→read→unread lifecycle over HTTP.
func TestRouterUnreadIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	_, myTok := hs.user(t)
	other, _ := hs.user(t)

	// A members-only server channel that the caller is NOT in (for the access-gate check).
	srv, err := hs.store.CreateServer(ctx, other.ID, "Unread Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	secret, err := hs.store.CreateServerChannel(ctx, srv.ID, "secret")
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}
	// A public channel both can use.
	pub, err := hs.store.CreateChannel(ctx, "rt-unread-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err != nil {
		t.Fatalf("create public channel: %v", err)
	}

	unreadChannels := func() []chat.ChannelUnread {
		w := hs.req(t, "GET", "/api/unreads", myTok, "")
		wantStatus(t, w, http.StatusOK, "GET unreads")
		var resp struct {
			Channels []chat.ChannelUnread `json:"channels"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode unreads: %v", err)
		}
		return resp.Channels
	}
	has := func(_ []int64, id int64) bool {
		for _, c := range unreadChannels() {
			if c.ChannelID == id {
				return true
			}
		}
		return false
	}
	unreadIDs := func() []int64 { return nil } // legacy no-op; has() recomputes

	wantStatus(t, hs.req(t, "GET", "/api/unreads", "", ""), http.StatusUnauthorized, "unauth GET unreads")

	if _, err := hs.store.Save(ctx, pub.ID, other.ID, other.Username, "hello"); err != nil {
		t.Fatalf("other posts: %v", err)
	}
	if !has(unreadIDs(), pub.ID) {
		t.Fatal("public channel should be unread after another user posts")
	}
	// Mark read over HTTP → no longer unread.
	wantStatus(t, hs.req(t, "POST", fmt.Sprintf("/api/channels/%d/read", pub.ID), myTok, ""), http.StatusNoContent, "mark read")
	if has(unreadIDs(), pub.ID) {
		t.Fatal("public channel should not be unread after POST /read")
	}
	// Access gate: marking a channel you can't access is 403, and it never appears unread.
	wantStatus(t, hs.req(t, "POST", fmt.Sprintf("/api/channels/%d/read", secret.ID), myTok, ""), http.StatusForbidden, "mark read on inaccessible channel")
	wantStatus(t, hs.req(t, "POST", "/api/channels/abc/read", myTok, ""), http.StatusBadRequest, "mark read bad id")
	if _, err := hs.store.Save(ctx, secret.ID, other.ID, other.Username, "secret"); err != nil {
		t.Fatalf("post in secret: %v", err)
	}
	if has(unreadIDs(), secret.ID) {
		t.Fatal("an inaccessible channel must never surface in unreads")
	}
}

// TestRouterStatusIntegration covers PUT /me/status: auth required, the caller's own
// status is set (Rule C — derived from the JWT), and it surfaces in the member list.
func TestRouterStatusIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	owner, ownerTok := hs.user(t)
	srv, err := hs.store.CreateServer(ctx, owner.ID, "Status Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	membersPath := fmt.Sprintf("/api/servers/%d/members", srv.ID)

	statusOf := func(uid int64) string {
		w := hs.req(t, "GET", membersPath, ownerTok, "")
		wantStatus(t, w, http.StatusOK, "list members")
		var ms []chat.ServerMember
		if err := json.Unmarshal(w.Body.Bytes(), &ms); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		for _, m := range ms {
			if m.UserID == uid {
				return m.Status
			}
		}
		t.Fatalf("owner not listed")
		return ""
	}

	wantStatus(t, hs.req(t, "PUT", "/api/me/status", "", `{"status":"hi"}`), http.StatusUnauthorized, "unauth set status")
	wantStatus(t, hs.req(t, "PUT", "/api/me/status", ownerTok, `{"status":"  on a call  "}`), http.StatusNoContent, "owner sets status")
	if s := statusOf(owner.ID); s != "on a call" {
		t.Fatalf("member-list status = %q, want trimmed 'on a call'", s)
	}
	wantStatus(t, hs.req(t, "PUT", "/api/me/status", ownerTok, `{"status":""}`), http.StatusNoContent, "owner clears status")
	if s := statusOf(owner.ID); s != "" {
		t.Fatalf("status should be cleared, got %q", s)
	}

	// --- adversarial / Rule 15: hostile bodies are rejected with 400 (never a 500 or a
	// stored value), and the length bounds hold end-to-end through the handler. This is
	// the handler-level class that produced the iter-97 over-long-password 500. ---
	emojiOf := func(uid int64) string {
		w := hs.req(t, "GET", membersPath, ownerTok, "")
		wantStatus(t, w, http.StatusOK, "list members (emoji)")
		var ms []chat.ServerMember
		if err := json.Unmarshal(w.Body.Bytes(), &ms); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		for _, m := range ms {
			if m.UserID == uid {
				return m.StatusEmoji
			}
		}
		t.Fatalf("owner not listed")
		return ""
	}
	wantStatus(t, hs.req(t, "PUT", "/api/me/status", ownerTok, `{bad json`),
		http.StatusBadRequest, "malformed status body -> 400")
	wantStatus(t, hs.req(t, "PUT", "/api/me/status", ownerTok, `{"status":"`+strings.Repeat("x", 5000)+`"}`),
		http.StatusBadRequest, "oversized status body (>4KiB) -> 400")
	if s := statusOf(owner.ID); s != "" {
		t.Fatalf("status must stay cleared after rejected hostile bodies, got %q", s)
	}
	// A long-but-under-the-byte-cap status is accepted and capped to 128 runes via HTTP.
	wantStatus(t, hs.req(t, "PUT", "/api/me/status", ownerTok, `{"status":"`+strings.Repeat("x", 200)+`"}`),
		http.StatusNoContent, "over-long status accepted")
	if s := statusOf(owner.ID); len([]rune(s)) != 128 {
		t.Fatalf("over-long status = %d runes via HTTP, want capped to 128", len([]rune(s)))
	}
	// statusEmoji is set + capped to 16 runes through the same endpoint.
	wantStatus(t, hs.req(t, "PUT", "/api/me/status", ownerTok, `{"status":"hi","statusEmoji":"`+strings.Repeat("😀", 40)+`"}`),
		http.StatusNoContent, "set + cap emoji")
	if e := emojiOf(owner.ID); len([]rune(e)) != 16 {
		t.Fatalf("over-long emoji = %d runes via HTTP, want capped to 16", len([]rune(e)))
	}
}

// TestRouterKickMemberIntegration walks the kick endpoint (DELETE
// /servers/{id}/members/{userId}) through the HTTP layer: unauth, the authz matrix,
// malformed ids, and the success path — asserting the status the store→HTTP mapping
// returns to a hostile caller (Rule B/15).
func TestRouterKickMemberIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	admin, adminTok := hs.user(t)
	member, _ := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Kick Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := hs.store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	if err := hs.store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	memberPath := fmt.Sprintf("/api/servers/%d/members/%d", srv.ID, member.ID)

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", memberPath, "", ""), http.StatusUnauthorized, "unauth kick")
	})
	t.Run("malformed ids are 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", fmt.Sprintf("/api/servers/abc/members/%d", member.ID), ownerTok, ""), http.StatusBadRequest, "bad server id")
		wantStatus(t, hs.req(t, "DELETE", fmt.Sprintf("/api/servers/%d/members/xyz", srv.ID), ownerTok, ""), http.StatusBadRequest, "bad user id")
	})
	t.Run("non-member/non-admin can't kick", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", memberPath, strangerTok, ""), http.StatusForbidden, "stranger kicks")
	})
	t.Run("can't kick the owner", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/members/%d", srv.ID, owner.ID)
		wantStatus(t, hs.req(t, "DELETE", p, adminTok, ""), http.StatusForbidden, "admin kicks owner")
	})
	t.Run("kicking a non-member target is 404", func(t *testing.T) {
		p := fmt.Sprintf("/api/servers/%d/members/%d", srv.ID, owner.ID+99999)
		wantStatus(t, hs.req(t, "DELETE", p, ownerTok, ""), http.StatusNotFound, "kick non-member")
	})
	t.Run("admin kicks a member → 204 and they lose access", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", memberPath, adminTok, ""), http.StatusNoContent, "admin kicks member")
		if ok, _ := hs.store.IsServerMember(ctx, srv.ID, member.ID); ok {
			t.Fatal("kicked member should no longer be a server member")
		}
		// Kicking the same (now non-member) again is a 404.
		wantStatus(t, hs.req(t, "DELETE", memberPath, adminTok, ""), http.StatusNotFound, "re-kick non-member")
	})
}

// TestRouterLeaveServerIntegration walks the leave endpoint (POST /servers/{id}/leave)
// through the HTTP layer: unauth, the owner-can't-leave rule, a non-member 404, and the
// member success path (after which they lose channel access).
func TestRouterLeaveServerIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Leave Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := hs.store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ch, err := hs.store.CreateServerChannel(ctx, srv.ID, "leave-chan")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	path := fmt.Sprintf("/api/servers/%d/leave", srv.ID)

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, "", ""), http.StatusUnauthorized, "unauth leave")
	})
	t.Run("malformed id is 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", "/api/servers/abc/leave", memberTok, ""), http.StatusBadRequest, "bad server id")
	})
	t.Run("non-member leaving is 404", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, strangerTok, ""), http.StatusNotFound, "stranger leaves")
	})
	t.Run("owner can't leave → 403", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, ownerTok, ""), http.StatusForbidden, "owner leaves")
		if ok, _ := hs.store.IsServerMember(ctx, srv.ID, owner.ID); !ok {
			t.Fatal("owner should still be a member after the rejected leave")
		}
	})
	t.Run("member leaves → 204 and loses access", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, memberTok, ""), http.StatusNoContent, "member leaves")
		if ok, _ := hs.store.IsServerMember(ctx, srv.ID, member.ID); ok {
			t.Fatal("member who left should no longer be a member")
		}
		if ok, _ := hs.store.CanAccessChannel(ctx, ch.ID, member.ID); ok {
			t.Fatal("member who left should lose channel access")
		}
		// Leaving again is a 404.
		wantStatus(t, hs.req(t, "POST", path, memberTok, ""), http.StatusNotFound, "re-leave non-member")
	})
}

// TestRouterTransferOwnershipIntegration walks POST /servers/{id}/transfer through the
// HTTP layer: unauth, the owner-only rule, an invalid target, and the success path
// (after which the actor is demoted to admin and the target owns the server).
func TestRouterTransferOwnershipIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Transfer Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := hs.store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	path := fmt.Sprintf("/api/servers/%d/transfer", srv.ID)
	body := func(uid int64) string { return fmt.Sprintf(`{"userId":%d}`, uid) }

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, "", body(member.ID)), http.StatusUnauthorized, "unauth transfer")
	})
	t.Run("malformed id is 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", "/api/servers/abc/transfer", ownerTok, body(member.ID)), http.StatusBadRequest, "bad server id")
	})
	t.Run("non-owner can't transfer", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, memberTok, body(owner.ID)), http.StatusForbidden, "member transfers")
		wantStatus(t, hs.req(t, "POST", path, strangerTok, body(member.ID)), http.StatusForbidden, "stranger transfers")
	})
	t.Run("transfer to a non-member is 404", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, ownerTok, body(owner.ID+99999)), http.StatusNotFound, "transfer to non-member")
	})
	t.Run("owner transfers to a member → 204 and roles swap", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", path, ownerTok, body(member.ID)), http.StatusNoContent, "owner transfers")
		if r, _ := hs.store.ServerRole(ctx, srv.ID, member.ID); r != "owner" {
			t.Fatalf("target should be owner, got %q", r)
		}
		if r, _ := hs.store.ServerRole(ctx, srv.ID, owner.ID); r != "admin" {
			t.Fatalf("old owner should be admin, got %q", r)
		}
		// The ex-owner (now admin) can no longer transfer.
		wantStatus(t, hs.req(t, "POST", path, ownerTok, body(owner.ID)), http.StatusForbidden, "ex-owner transfers")
	})
}

// TestRouterServerSettingsIntegration walks the server rename (PATCH /servers/{id}) and
// delete (DELETE /servers/{id}) endpoints through the HTTP layer: unauth, the authz
// matrix (rename = admin+, delete = owner-only), body validation, malformed ids, and the
// success paths — asserting the store→HTTP status mapping a hostile caller sees (Rule B/15).
func TestRouterServerSettingsIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	admin, adminTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Settings Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := hs.store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	if err := hs.store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	path := fmt.Sprintf("/api/servers/%d", srv.ID)

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", path, "", `{"name":"X"}`), http.StatusUnauthorized, "unauth rename")
		wantStatus(t, hs.req(t, "DELETE", path, "", ""), http.StatusUnauthorized, "unauth delete")
	})
	t.Run("malformed / invalid input", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", "/api/servers/abc", ownerTok, `{"name":"X"}`), http.StatusBadRequest, "bad server id")
		wantStatus(t, hs.req(t, "PATCH", path, ownerTok, `{"name":"   "}`), http.StatusBadRequest, "blank name")
		wantStatus(t, hs.req(t, "PATCH", path, ownerTok, `{"name":"`+strings.Repeat("x", 65)+`"}`), http.StatusBadRequest, "over-long name")
	})
	t.Run("rename authz: member/stranger forbidden", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", path, memberTok, `{"name":"Hijacked"}`), http.StatusForbidden, "member renames")
		wantStatus(t, hs.req(t, "PATCH", path, strangerTok, `{"name":"Hijacked"}`), http.StatusForbidden, "stranger renames")
	})
	t.Run("delete authz: admin/member/stranger forbidden", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", path, adminTok, ""), http.StatusForbidden, "admin deletes")
		wantStatus(t, hs.req(t, "DELETE", path, memberTok, ""), http.StatusForbidden, "member deletes")
		wantStatus(t, hs.req(t, "DELETE", path, strangerTok, ""), http.StatusForbidden, "stranger deletes")
		// Still present after every rejected delete.
		if servers, _ := hs.store.ListServers(ctx, owner.ID); len(servers) != 1 {
			t.Fatalf("server should survive rejected deletes: %+v", servers)
		}
	})
	t.Run("admin can rename → 200", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", path, adminTok, `{"name":"Admin Renamed"}`), http.StatusOK, "admin renames")
		if servers, _ := hs.store.ListServers(ctx, owner.ID); len(servers) != 1 || servers[0].Name != "Admin Renamed" {
			t.Fatalf("rename should land: %+v", servers)
		}
	})
	t.Run("owner deletes → 204 then gone", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", path, ownerTok, ""), http.StatusNoContent, "owner deletes")
		if servers, _ := hs.store.ListServers(ctx, owner.ID); len(servers) != 0 {
			t.Fatalf("server should be gone: %+v", servers)
		}
		// Operating on the now-deleted server is a 404.
		wantStatus(t, hs.req(t, "PATCH", path, ownerTok, `{"name":"Zombie"}`), http.StatusNotFound, "rename deleted server")
		wantStatus(t, hs.req(t, "DELETE", path, ownerTok, ""), http.StatusNotFound, "re-delete server")
	})
}

// TestRouterBanMemberIntegration walks the ban/unban endpoints through the HTTP layer:
// unauth, the authz matrix (mirrors kick), malformed ids, the success path, and the
// security guarantee that a banned user can't redeem a *valid* invite until unbanned
// (Rule B/15).
func TestRouterBanMemberIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	admin, adminTok := hs.user(t)
	admin2, _ := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Ban Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, admin2, member} {
		if err := hs.store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	for _, a := range []auth.User{admin, admin2} {
		if err := hs.store.SetServerRole(ctx, srv.ID, owner.ID, a.ID, "admin"); err != nil {
			t.Fatalf("promote admin: %v", err)
		}
	}
	bansPath := fmt.Sprintf("/api/servers/%d/bans", srv.ID)
	banBody := func(uid int64) string { return fmt.Sprintf(`{"userId":%d,"reason":"spam"}`, uid) }

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", bansPath, "", banBody(member.ID)), http.StatusUnauthorized, "unauth ban")
	})
	t.Run("malformed server id is 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", "/api/servers/abc/bans", ownerTok, banBody(member.ID)), http.StatusBadRequest, "bad server id")
	})
	t.Run("non-member/non-admin can't ban", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", bansPath, strangerTok, banBody(member.ID)), http.StatusForbidden, "stranger bans")
		wantStatus(t, hs.req(t, "POST", bansPath, memberTok, banBody(admin.ID)), http.StatusForbidden, "member bans")
	})
	t.Run("can't ban the owner", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", bansPath, adminTok, banBody(owner.ID)), http.StatusForbidden, "admin bans owner")
	})
	t.Run("admin can't ban a fellow admin", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", bansPath, adminTok, banBody(admin2.ID)), http.StatusForbidden, "admin bans admin")
	})
	t.Run("can't ban yourself", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", bansPath, adminTok, banBody(admin.ID)), http.StatusForbidden, "self ban")
	})
	t.Run("banning a non-member is 404", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", bansPath, ownerTok, banBody(owner.ID+99999)), http.StatusNotFound, "ban non-member")
	})
	t.Run("non-admin can't list bans", func(t *testing.T) {
		wantStatus(t, hs.req(t, "GET", bansPath, strangerTok, ""), http.StatusForbidden, "stranger lists bans")
	})
	t.Run("admin bans a member → 204, loses access, can't rejoin a valid invite, then unban restores", func(t *testing.T) {
		// Mint a valid invite the member could otherwise rejoin with.
		code, err := hs.store.CreateInvite(ctx, srv.ID, owner.ID)
		if err != nil {
			t.Fatalf("invite: %v", err)
		}
		// Ban → 204, membership gone, ban recorded.
		wantStatus(t, hs.req(t, "POST", bansPath, adminTok, banBody(member.ID)), http.StatusNoContent, "admin bans member")
		if ok, _ := hs.store.IsServerMember(ctx, srv.ID, member.ID); ok {
			t.Fatal("banned member should no longer be a server member")
		}
		if ok, _ := hs.store.IsServerBanned(ctx, srv.ID, member.ID); !ok {
			t.Fatal("banned member should be recorded in server_bans")
		}
		// A banned user can't redeem a still-valid invite — 403.
		wantStatus(t, hs.req(t, "POST", fmt.Sprintf("/api/invites/%s", code), memberTok, ""), http.StatusForbidden, "banned redeems valid invite")
		// The ban shows up in the admin bans list.
		listResp := hs.req(t, "GET", bansPath, adminTok, "")
		wantStatus(t, listResp, http.StatusOK, "admin lists bans")
		if !strings.Contains(listResp.Body.String(), fmt.Sprintf(`"userId":%d`, member.ID)) {
			t.Fatalf("bans list should contain the banned user; got %s", listResp.Body.String())
		}
		// Unban → 204; now the same valid invite admits them again.
		unbanPath := fmt.Sprintf("/api/servers/%d/bans/%d", srv.ID, member.ID)
		wantStatus(t, hs.req(t, "DELETE", unbanPath, adminTok, ""), http.StatusNoContent, "admin unbans member")
		wantStatus(t, hs.req(t, "POST", fmt.Sprintf("/api/invites/%s", code), memberTok, ""), http.StatusOK, "unbanned redeems invite")
		if ok, _ := hs.store.IsServerMember(ctx, srv.ID, member.ID); !ok {
			t.Fatal("unbanned member should be able to rejoin")
		}
		// Unbanning a not-banned user is 404.
		wantStatus(t, hs.req(t, "DELETE", unbanPath, adminTok, ""), http.StatusNotFound, "unban non-banned")
	})
}

// TestRouterBlockUserIntegration walks the v0.5 user-blocking endpoints through the
// real HTTP layer (Rule B/C — identity from the JWT, never the body): unauth → 401,
// block → 204, the block shows in GET /me/blocks, self-block → 400, unknown user → 404,
// unblock → 204, re-unblock → 404. It also confirms a normal (un-blocked) DM still opens
// and that a block then forbids opening the DM (403) — DM-only enforcement, slice 1.
func TestRouterBlockUserIntegration(t *testing.T) {
	hs := newHarness(t)

	alice, aliceTok := hs.user(t)
	bob, _ := hs.user(t)
	carol, _ := hs.user(t)

	blockPath := func(uid int64) string { return fmt.Sprintf("/api/users/%d/block", uid) }

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", blockPath(bob.ID), "", ""), http.StatusUnauthorized, "unauth block")
		wantStatus(t, hs.req(t, "GET", "/api/me/blocks", "", ""), http.StatusUnauthorized, "unauth list")
		wantStatus(t, hs.req(t, "DELETE", blockPath(bob.ID), "", ""), http.StatusUnauthorized, "unauth unblock")
	})
	t.Run("malformed user id is 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", "/api/users/abc/block", aliceTok, ""), http.StatusBadRequest, "bad user id")
	})
	t.Run("can't block yourself", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", blockPath(alice.ID), aliceTok, ""), http.StatusBadRequest, "self block")
	})
	t.Run("blocking an unknown user is 404", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", blockPath(alice.ID+999999), aliceTok, ""), http.StatusNotFound, "block unknown")
	})

	// A normal (un-blocked) DM with carol opens fine — proves no regression.
	t.Run("normal DM opens before any block", func(t *testing.T) {
		body := fmt.Sprintf(`{"username":%q}`, carol.Username)
		wantStatus(t, hs.req(t, "POST", "/api/dms", aliceTok, body), http.StatusOK, "open dm with carol")
	})

	t.Run("block → 204, listed, then DM is forbidden, then unblock restores", func(t *testing.T) {
		// Block bob → 204 (idempotent: a second block is also 204).
		wantStatus(t, hs.req(t, "POST", blockPath(bob.ID), aliceTok, ""), http.StatusNoContent, "block bob")
		wantStatus(t, hs.req(t, "POST", blockPath(bob.ID), aliceTok, ""), http.StatusNoContent, "re-block bob")

		// GET /me/blocks shows bob.
		listResp := hs.req(t, "GET", "/api/me/blocks", aliceTok, "")
		wantStatus(t, listResp, http.StatusOK, "list blocks")
		if !strings.Contains(listResp.Body.String(), fmt.Sprintf(`"id":%d`, bob.ID)) {
			t.Fatalf("blocks list should contain bob; got %s", listResp.Body.String())
		}

		// Opening a DM with a blocked user is forbidden (403) — DM-only enforcement.
		body := fmt.Sprintf(`{"username":%q}`, bob.Username)
		wantStatus(t, hs.req(t, "POST", "/api/dms", aliceTok, body), http.StatusForbidden, "open dm with blocked bob")

		// Unblock → 204; the DM opens again.
		wantStatus(t, hs.req(t, "DELETE", blockPath(bob.ID), aliceTok, ""), http.StatusNoContent, "unblock bob")
		wantStatus(t, hs.req(t, "POST", "/api/dms", aliceTok, body), http.StatusOK, "open dm with unblocked bob")

		// Unblocking a not-blocked user is 404.
		wantStatus(t, hs.req(t, "DELETE", blockPath(bob.ID), aliceTok, ""), http.StatusNotFound, "re-unblock bob")
	})
}

// TestRouterInviteManagementIntegration walks the invite list/revoke endpoints through
// the HTTP layer: listing + revoking are admin-gated (a stranger/member gets 403, not the
// list and not a revoke), revoke is scoped by server_id so an admin of one server can't
// revoke another server's code (Rule B cross-server guard), a revoked code no longer
// redeems (404), and re-revoking an unknown code is 404. Minting stays members-only.
func TestRouterInviteManagementIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, _ := hs.user(t)
	admin, adminTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, joinerTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Invite Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := hs.store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	if err := hs.store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	invitesPath := fmt.Sprintf("/api/servers/%d/invites", srv.ID)

	t.Run("listing is admin-gated", func(t *testing.T) {
		wantStatus(t, hs.req(t, "GET", invitesPath, "", ""), http.StatusUnauthorized, "unauth list")
		wantStatus(t, hs.req(t, "GET", invitesPath, strangerTok, ""), http.StatusForbidden, "stranger lists")
		wantStatus(t, hs.req(t, "GET", invitesPath, memberTok, ""), http.StatusForbidden, "member lists")
		wantStatus(t, hs.req(t, "GET", invitesPath, adminTok, ""), http.StatusOK, "admin lists")
	})

	// A plain member can still mint (members-only POST is unchanged); the new code
	// shows up in the admin list. Grab the code for the revoke checks below.
	mint := hs.req(t, "POST", invitesPath, memberTok, "")
	wantStatus(t, mint, http.StatusCreated, "member mints invite")
	var minted struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(mint.Body.Bytes(), &minted); err != nil || minted.Code == "" {
		t.Fatalf("mint returned no code: %v (body %s)", err, mint.Body.String())
	}

	t.Run("minted code appears in the admin list", func(t *testing.T) {
		list := hs.req(t, "GET", invitesPath, adminTok, "")
		wantStatus(t, list, http.StatusOK, "admin lists after mint")
		if !strings.Contains(list.Body.String(), fmt.Sprintf(`"code":%q`, minted.Code)) {
			t.Fatalf("invites list should contain the minted code; got %s", list.Body.String())
		}
	})

	revokePath := fmt.Sprintf("%s/%s", invitesPath, minted.Code)
	t.Run("revoke is admin-gated", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", revokePath, "", ""), http.StatusUnauthorized, "unauth revoke")
		wantStatus(t, hs.req(t, "DELETE", revokePath, strangerTok, ""), http.StatusForbidden, "stranger revokes")
		wantStatus(t, hs.req(t, "DELETE", revokePath, memberTok, ""), http.StatusForbidden, "member revokes")
	})

	t.Run("cross-server revoke is blocked (Rule B) and the foreign code still works", func(t *testing.T) {
		// A second server owned by the same owner, with its own invite code.
		srvB, err := hs.store.CreateServer(ctx, owner.ID, "Other Guild")
		if err != nil {
			t.Fatalf("create server B: %v", err)
		}
		codeB, err := hs.store.CreateInvite(ctx, srvB.ID, owner.ID)
		if err != nil {
			t.Fatalf("invite B: %v", err)
		}
		// Admin of A tries to revoke B's code via A's path → 404 (scoped by server_id).
		crossPath := fmt.Sprintf("/api/servers/%d/invites/%s", srv.ID, codeB)
		wantStatus(t, hs.req(t, "DELETE", crossPath, adminTok, ""), http.StatusNotFound, "cross-server revoke")
		// Proof it wasn't deleted: the code still admits a joiner to server B.
		wantStatus(t, hs.req(t, "POST", fmt.Sprintf("/api/invites/%s", codeB), joinerTok, ""), http.StatusOK, "B's code still redeems")
	})

	t.Run("admin revokes → 204, code stops redeeming, re-revoke is 404", func(t *testing.T) {
		wantStatus(t, hs.req(t, "DELETE", revokePath, adminTok, ""), http.StatusNoContent, "admin revokes")
		// The revoked code no longer admits anyone.
		wantStatus(t, hs.req(t, "POST", fmt.Sprintf("/api/invites/%s", minted.Code), joinerTok, ""), http.StatusNotFound, "revoked code redeem")
		// It's gone from the list, and revoking it again is 404.
		list := hs.req(t, "GET", invitesPath, adminTok, "")
		if strings.Contains(list.Body.String(), fmt.Sprintf(`"code":%q`, minted.Code)) {
			t.Fatalf("revoked code should be gone from the list; got %s", list.Body.String())
		}
		wantStatus(t, hs.req(t, "DELETE", revokePath, adminTok, ""), http.StatusNotFound, "re-revoke unknown")
	})
}

// TestRouterInviteMaxUsesIntegration proves the max-uses cap end-to-end: the POST body
// validates the bound (1–1000), a capped code admits exactly its limit then 404s as
// exhausted, an unlimited code keeps admitting, and a member re-redeeming does NOT burn a
// use. The cap is enforced + counted server-side (a stale client can't bypass it, Rule B).
func TestRouterInviteMaxUsesIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	srv, err := hs.store.CreateServer(ctx, owner.ID, "MaxUses Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	invitesPath := fmt.Sprintf("/api/servers/%d/invites", srv.ID)

	t.Run("maxUses body is bounded 1..1000", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", invitesPath, ownerTok, `{"maxUses":0}`), http.StatusBadRequest, "maxUses 0")
		wantStatus(t, hs.req(t, "POST", invitesPath, ownerTok, `{"maxUses":-3}`), http.StatusBadRequest, "maxUses negative")
		wantStatus(t, hs.req(t, "POST", invitesPath, ownerTok, `{"maxUses":1001}`), http.StatusBadRequest, "maxUses too big")
		wantStatus(t, hs.req(t, "POST", invitesPath, ownerTok, `not json`), http.StatusBadRequest, "malformed body")
		// An empty body is fine (legacy clients) → unlimited.
		wantStatus(t, hs.req(t, "POST", invitesPath, ownerTok, ""), http.StatusCreated, "empty body → unlimited")
	})

	t.Run("a maxUses=1 code admits one member then is exhausted", func(t *testing.T) {
		mint := hs.req(t, "POST", invitesPath, ownerTok, `{"maxUses":1}`)
		wantStatus(t, mint, http.StatusCreated, "mint capped invite")
		var minted struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(mint.Body.Bytes(), &minted); err != nil || minted.Code == "" {
			t.Fatalf("no code: %v (%s)", err, mint.Body.String())
		}
		redeem := fmt.Sprintf("/api/invites/%s", minted.Code)
		_, firstTok := hs.user(t)
		_, secondTok := hs.user(t)
		// First join consumes the only use → 200.
		wantStatus(t, hs.req(t, "POST", redeem, firstTok, ""), http.StatusOK, "first join (uses the slot)")
		// The same member re-redeeming must NOT consume a use (idempotent rejoin).
		wantStatus(t, hs.req(t, "POST", redeem, firstTok, ""), http.StatusOK, "re-redeem by a member is a no-op")
		// A different user now finds it exhausted → 404.
		wantStatus(t, hs.req(t, "POST", redeem, secondTok, ""), http.StatusNotFound, "second join is exhausted")
		// An exhausted code drops out of the admin active list.
		list := hs.req(t, "GET", invitesPath, ownerTok, "")
		if strings.Contains(list.Body.String(), fmt.Sprintf(`"code":%q`, minted.Code)) {
			t.Fatalf("exhausted code should not be listed as active; got %s", list.Body.String())
		}
	})

	t.Run("an unlimited code keeps admitting; the list reports the running uses count", func(t *testing.T) {
		mint := hs.req(t, "POST", invitesPath, ownerTok, `{}`) // {} → unlimited
		wantStatus(t, mint, http.StatusCreated, "mint unlimited invite")
		var minted struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(mint.Body.Bytes(), &minted)
		redeem := fmt.Sprintf("/api/invites/%s", minted.Code)
		for i := 0; i < 3; i++ {
			_, tok := hs.user(t)
			wantStatus(t, hs.req(t, "POST", redeem, tok, ""), http.StatusOK, fmt.Sprintf("unlimited join %d", i+1))
		}
		list := hs.req(t, "GET", invitesPath, ownerTok, "")
		if !strings.Contains(list.Body.String(), fmt.Sprintf(`"code":%q`, minted.Code)) ||
			!strings.Contains(list.Body.String(), `"uses":3`) {
			t.Fatalf("unlimited code should list with uses:3; got %s", list.Body.String())
		}
	})
}

// TestRouterTimeoutMemberIntegration walks the timeout/clear endpoints through the HTTP
// layer (authz matrix mirrors ban) and proves the user-visible guarantee: a timed-out
// member's post is rejected server-side (ErrTimedOut) until the timeout is cleared, and
// the mute surfaces in the member list. Duration is clamped server-side (Rule B/15).
func TestRouterTimeoutMemberIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	admin, adminTok := hs.user(t)
	admin2, _ := hs.user(t)
	member, _ := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Timeout Router Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, admin2, member} {
		if err := hs.store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
	for _, a := range []auth.User{admin, admin2} {
		if err := hs.store.SetServerRole(ctx, srv.ID, owner.ID, a.ID, "admin"); err != nil {
			t.Fatalf("promote admin: %v", err)
		}
	}
	ch, err := hs.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	toPath := fmt.Sprintf("/api/servers/%d/timeouts", srv.ID)
	toBody := func(uid, secs int64) string {
		return fmt.Sprintf(`{"userId":%d,"durationSeconds":%d}`, uid, secs)
	}

	t.Run("auth required", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", toPath, "", toBody(member.ID, 600)), http.StatusUnauthorized, "unauth timeout")
	})
	t.Run("malformed server id is 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", "/api/servers/abc/timeouts", ownerTok, toBody(member.ID, 600)), http.StatusBadRequest, "bad server id")
	})
	t.Run("non-positive duration is 400", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", toPath, ownerTok, toBody(member.ID, 0)), http.StatusBadRequest, "zero duration")
		wantStatus(t, hs.req(t, "POST", toPath, ownerTok, toBody(member.ID, -5)), http.StatusBadRequest, "negative duration")
	})
	t.Run("non-member/non-admin can't time out", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", toPath, strangerTok, toBody(member.ID, 600)), http.StatusForbidden, "stranger times out")
	})
	t.Run("can't time out the owner / a fellow admin / yourself", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", toPath, adminTok, toBody(owner.ID, 600)), http.StatusForbidden, "admin times out owner")
		wantStatus(t, hs.req(t, "POST", toPath, adminTok, toBody(admin2.ID, 600)), http.StatusForbidden, "admin times out admin")
		wantStatus(t, hs.req(t, "POST", toPath, adminTok, toBody(admin.ID, 600)), http.StatusForbidden, "self timeout")
	})
	t.Run("timing out a non-member is 404", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", toPath, ownerTok, toBody(owner.ID+99999, 600)), http.StatusNotFound, "timeout non-member")
	})
	t.Run("admin times out a member → muted server-side until cleared", func(t *testing.T) {
		// Before the timeout the member can post.
		if _, err := hs.store.SaveReply(ctx, ch.ID, member.ID, member.Username, "before", nil); err != nil {
			t.Fatalf("member should be able to post before timeout: %v", err)
		}
		// Timeout → 200; the member's post is now rejected at the store guard.
		wantStatus(t, hs.req(t, "POST", toPath, adminTok, toBody(member.ID, 3600)), http.StatusOK, "admin times out member")
		if _, err := hs.store.SaveReply(ctx, ch.ID, member.ID, member.Username, "during", nil); !errors.Is(err, chat.ErrTimedOut) {
			t.Fatalf("timed-out member's post should be ErrTimedOut, got %v", err)
		}
		// The mute surfaces in the member list.
		members, err := hs.store.ListServerMembers(ctx, srv.ID)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		var muted bool
		for _, m := range members {
			if m.UserID == member.ID && m.TimeoutUntil != nil {
				muted = true
			}
		}
		if !muted {
			t.Fatal("the member list should report the muted member's timeoutUntil")
		}
		// Clear → 204; the member can post again.
		clearPath := fmt.Sprintf("/api/servers/%d/timeouts/%d", srv.ID, member.ID)
		wantStatus(t, hs.req(t, "DELETE", clearPath, adminTok, ""), http.StatusNoContent, "admin clears timeout")
		if _, err := hs.store.SaveReply(ctx, ch.ID, member.ID, member.Username, "after", nil); err != nil {
			t.Fatalf("member should be able to post after the timeout is cleared: %v", err)
		}
		// Clearing a non-member is 404.
		wantStatus(t, hs.req(t, "DELETE", fmt.Sprintf("/api/servers/%d/timeouts/%d", srv.ID, owner.ID+99999), adminTok, ""), http.StatusNotFound, "clear non-member")
	})
	t.Run("duration is clamped to the server-side ceiling", func(t *testing.T) {
		// Ask for ~100 years; the effective until must be within ~28 days.
		resp := hs.req(t, "POST", toPath, ownerTok, toBody(member.ID, 100*365*24*3600))
		wantStatus(t, resp, http.StatusOK, "huge duration accepted + clamped")
		var out struct {
			Until time.Time `json:"until"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode until: %v (body %s)", err, resp.Body.String())
		}
		if out.Until.After(time.Now().Add(29 * 24 * time.Hour)) {
			t.Fatalf("timeout should be clamped to ~28d, got %s", out.Until)
		}
	})
}

// TestRouterChannelCategoriesIntegration walks the category endpoints + the
// create-channel-in-category path: authz (create is admin, list is member), name
// validation, that a created channel carries its categoryId, and the Rule-B guard that a
// channel can't be attached to ANOTHER server's category (ErrCategoryNotFound → 400).
func TestRouterChannelCategoriesIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Category Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := hs.store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	// A second server owned by someone else, to test the cross-server category guard.
	other, otherTok := hs.user(t)
	srv2, err := hs.store.CreateServer(ctx, other.ID, "Other Guild")
	if err != nil {
		t.Fatalf("create server2: %v", err)
	}
	otherCat, err := hs.store.CreateChannelCategory(ctx, srv2.ID, "Other Cat")
	if err != nil {
		t.Fatalf("create other category: %v", err)
	}
	_ = otherTok

	catsPath := fmt.Sprintf("/api/servers/%d/categories", srv.ID)
	chansPath := fmt.Sprintf("/api/servers/%d/channels", srv.ID)

	t.Run("create category authz", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", catsPath, "", `{"name":"Text"}`), http.StatusUnauthorized, "unauth create")
		wantStatus(t, hs.req(t, "POST", catsPath, strangerTok, `{"name":"Text"}`), http.StatusForbidden, "stranger create")
		wantStatus(t, hs.req(t, "POST", catsPath, memberTok, `{"name":"Text"}`), http.StatusForbidden, "member create")
	})
	t.Run("category name validation", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", catsPath, ownerTok, `{"name":"   "}`), http.StatusBadRequest, "blank name")
		tooLong := fmt.Sprintf(`{"name":%q}`, strings.Repeat("x", 33))
		wantStatus(t, hs.req(t, "POST", catsPath, ownerTok, tooLong), http.StatusBadRequest, "too-long name")
	})
	t.Run("list categories: member ok, stranger 403", func(t *testing.T) {
		wantStatus(t, hs.req(t, "GET", catsPath, strangerTok, ""), http.StatusForbidden, "stranger lists")
		wantStatus(t, hs.req(t, "GET", catsPath, memberTok, ""), http.StatusOK, "member lists")
	})
	t.Run("admin creates a category → channel in it carries the categoryId", func(t *testing.T) {
		// Create the category (display label with a space + caps is allowed).
		catResp := hs.req(t, "POST", catsPath, ownerTok, `{"name":"Text Channels"}`)
		wantStatus(t, catResp, http.StatusCreated, "owner creates category")
		var cat struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(catResp.Body.Bytes(), &cat); err != nil {
			t.Fatalf("decode category: %v", err)
		}
		if cat.ID == 0 || cat.Name != "Text Channels" {
			t.Fatalf("unexpected category: %+v", cat)
		}
		// Create a channel inside it; the response carries categoryId.
		chResp := hs.req(t, "POST", chansPath, ownerTok, fmt.Sprintf(`{"name":"general","categoryId":%d}`, cat.ID))
		wantStatus(t, chResp, http.StatusCreated, "create channel in category")
		var ch struct {
			ID         int64  `json:"id"`
			CategoryID *int64 `json:"categoryId"`
		}
		if err := json.Unmarshal(chResp.Body.Bytes(), &ch); err != nil {
			t.Fatalf("decode channel: %v", err)
		}
		if ch.CategoryID == nil || *ch.CategoryID != cat.ID {
			t.Fatalf("channel should carry categoryId %d, got %+v", cat.ID, ch.CategoryID)
		}
		// And ListServerChannels reflects it.
		chans, err := hs.store.ListServerChannels(ctx, srv.ID)
		if err != nil {
			t.Fatalf("list channels: %v", err)
		}
		var found bool
		for _, c := range chans {
			if c.ID == ch.ID && c.CategoryID != nil && *c.CategoryID == cat.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("ListServerChannels should report the channel's category_id")
		}
	})
	t.Run("can't attach a channel to another server's category (Rule B)", func(t *testing.T) {
		body := fmt.Sprintf(`{"name":"sneaky","categoryId":%d}`, otherCat.ID)
		wantStatus(t, hs.req(t, "POST", chansPath, ownerTok, body), http.StatusBadRequest, "cross-server category")
		// The channel must not have been created.
		chans, _ := hs.store.ListServerChannels(ctx, srv.ID)
		for _, c := range chans {
			if c.Name == "sneaky" {
				t.Fatal("a channel with a cross-server category must not be created")
			}
		}
	})
	t.Run("delete category: authz + the channel survives as uncategorized", func(t *testing.T) {
		cat, err := hs.store.CreateChannelCategory(ctx, srv.ID, "Doomed")
		if err != nil {
			t.Fatalf("create category: %v", err)
		}
		ch, err := hs.store.CreateServerChannelInCategory(ctx, srv.ID, "keepme", &cat.ID)
		if err != nil {
			t.Fatalf("create channel in category: %v", err)
		}
		delPath := fmt.Sprintf("/api/servers/%d/categories/%d", srv.ID, cat.ID)
		// Non-admins can't delete.
		wantStatus(t, hs.req(t, "DELETE", delPath, "", ""), http.StatusUnauthorized, "unauth delete")
		wantStatus(t, hs.req(t, "DELETE", delPath, memberTok, ""), http.StatusForbidden, "member delete")
		// Can't delete another server's category via this server's path (Rule B → 404).
		wantStatus(t, hs.req(t, "DELETE", fmt.Sprintf("/api/servers/%d/categories/%d", srv.ID, otherCat.ID), ownerTok, ""), http.StatusNotFound, "cross-server delete")
		// Admin deletes it → 204; the channel survives but is now uncategorized.
		wantStatus(t, hs.req(t, "DELETE", delPath, ownerTok, ""), http.StatusNoContent, "admin deletes category")
		chans, err := hs.store.ListServerChannels(ctx, srv.ID)
		if err != nil {
			t.Fatalf("list channels: %v", err)
		}
		var found, stillCategorized bool
		for _, c := range chans {
			if c.ID == ch.ID {
				found = true
				stillCategorized = c.CategoryID != nil
			}
		}
		if !found {
			t.Fatal("the channel should still exist after its category is deleted")
		}
		if stillCategorized {
			t.Fatal("the channel should be uncategorized (category_id NULL) after the category is deleted")
		}
		// Deleting it again is a 404.
		wantStatus(t, hs.req(t, "DELETE", delPath, ownerTok, ""), http.StatusNotFound, "re-delete category")
	})
}

// TestRouterMessageEndpointsIntegration covers the edit + reaction REST endpoints,
// which carry their own authorization (edit is author-only; reactions are gated by
// channel access) and map store errors to HTTP status. The message under test lives
// in a members-only server channel so the access gate is exercised.
func TestRouterMessageEndpointsIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Msg Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	code, err := hs.store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := hs.store.RedeemInvite(ctx, code, member.ID); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	ch, err := hs.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	msg, err := hs.store.Save(ctx, ch.ID, owner.ID, owner.Username, "original")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	msgPath := fmt.Sprintf("/api/messages/%d", msg.ID)

	t.Run("edit is author-only", func(t *testing.T) {
		// A member who isn't the author can't edit it — 404 (no existence leak).
		wantStatus(t, hs.req(t, "PATCH", msgPath, memberTok, `{"body":"hacked"}`), http.StatusNotFound, "non-author edits")
		// Empty body is rejected.
		wantStatus(t, hs.req(t, "PATCH", msgPath, ownerTok, `{"body":"   "}`), http.StatusBadRequest, "empty body")
		// The author edits successfully and the new body comes back.
		w := hs.req(t, "PATCH", msgPath, ownerTok, `{"body":"edited by owner"}`)
		wantStatus(t, w, http.StatusOK, "author edits own message")
		var got chat.Message
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode edited message: %v", err)
		}
		if got.Body != "edited by owner" {
			t.Fatalf("edited body = %q, want %q", got.Body, "edited by owner")
		}
	})

	t.Run("reactions are channel-access gated", func(t *testing.T) {
		rxPath := msgPath + "/reactions"
		// A non-member of the server can't react to its channel's message — 403.
		wantStatus(t, hs.req(t, "PUT", rxPath, strangerTok, `{"emoji":"👍"}`), http.StatusForbidden, "non-member reacts")
		// An over-long emoji is rejected.
		wantStatus(t, hs.req(t, "PUT", rxPath, memberTok, `{"emoji":"x_way_too_long_emoji"}`), http.StatusBadRequest, "invalid emoji")
		// A member reacts, then removes it.
		wantStatus(t, hs.req(t, "PUT", rxPath, memberTok, `{"emoji":"👍"}`), http.StatusOK, "member adds reaction")
		del := fmt.Sprintf("%s/%s", rxPath, url.PathEscape("👍"))
		wantStatus(t, hs.req(t, "DELETE", del, memberTok, ""), http.StatusOK, "member removes reaction")
	})
}

// TestRouterSPARoutingIntegration guards the single-binary deploy wiring: the SPA
// catch-all serves index.html for "/" and unknown client routes, but must NOT
// shadow the API — an unknown /api path is still a 404 (JSON-ish), not the SPA —
// and /healthz still returns its JSON.
func TestRouterSPARoutingIntegration(t *testing.T) {
	hs := newHarness(t)

	isHTML := func(w *httptest.ResponseRecorder) bool {
		return strings.Contains(w.Header().Get("Content-Type"), "text/html")
	}

	// "/" and unknown client routes → the SPA shell (HTML).
	for _, path := range []string{"/", "/login", "/channels/123"} {
		w := hs.req(t, "GET", path, "", "")
		wantStatus(t, w, http.StatusOK, "SPA "+path)
		if !isHTML(w) {
			t.Fatalf("GET %s should serve the SPA (text/html), got %q", path, w.Header().Get("Content-Type"))
		}
	}

	// The catch-all must NOT swallow the API: an unknown /api route is a 404, not
	// the SPA HTML. (Regression guard for the /* mount added with the embed.)
	w := hs.req(t, "GET", "/api/definitely-not-a-route", "", "")
	wantStatus(t, w, http.StatusNotFound, "unknown /api path")
	if isHTML(w) {
		t.Fatal("unknown /api path served the SPA shell — catch-all is shadowing the API")
	}

	// /healthz is still the health endpoint, not the SPA.
	wh := hs.req(t, "GET", "/healthz", "", "")
	wantStatus(t, wh, http.StatusOK, "healthz")
	if !strings.Contains(wh.Body.String(), `"status":"ok"`) {
		t.Fatalf("/healthz body = %q, want health JSON", strings.TrimSpace(wh.Body.String()))
	}
}

// TestRouterChannelTopicIntegration covers PATCH /channels/{id} for the topic field:
// admin-gated, length-bounded, backward-compatible with the postPolicy-only request,
// and the topic is returned in the server channel list.
func TestRouterChannelTopicIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Topic Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	code, err := hs.store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := hs.store.RedeemInvite(ctx, code, member.ID); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	ch, err := hs.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	p := fmt.Sprintf("/api/channels/%d", ch.ID)

	t.Run("admin sets topic", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", p, ownerTok, `{"topic":"Welcome to general"}`), http.StatusNoContent, "owner sets topic")
	})
	t.Run("non-admin cannot set topic", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", p, memberTok, `{"topic":"hax"}`), http.StatusForbidden, "member sets topic")
	})
	t.Run("over-long topic is rejected", func(t *testing.T) {
		long := `{"topic":"` + strings.Repeat("x", 1025) + `"}`
		wantStatus(t, hs.req(t, "PATCH", p, ownerTok, long), http.StatusBadRequest, "1025-char topic")
	})
	t.Run("empty patch is rejected", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", p, ownerTok, `{}`), http.StatusBadRequest, "no fields")
	})
	t.Run("postPolicy still works (backward compat)", func(t *testing.T) {
		wantStatus(t, hs.req(t, "PATCH", p, ownerTok, `{"postPolicy":"admins"}`), http.StatusNoContent, "owner sets policy")
	})
	t.Run("topic is returned in the channel list", func(t *testing.T) {
		w := hs.req(t, "GET", fmt.Sprintf("/api/servers/%d/channels", srv.ID), ownerTok, "")
		wantStatus(t, w, http.StatusOK, "list server channels")
		var chans []chat.Channel
		if err := json.Unmarshal(w.Body.Bytes(), &chans); err != nil {
			t.Fatalf("decode channels: %v", err)
		}
		var found *chat.Channel
		for i := range chans {
			if chans[i].ID == ch.ID {
				found = &chans[i]
			}
		}
		if found == nil {
			t.Fatalf("channel %d not in list", ch.ID)
		}
		if found.Topic != "Welcome to general" {
			t.Fatalf("topic = %q, want %q", found.Topic, "Welcome to general")
		}
	})
}

// TestRouterPinIntegration covers PUT/DELETE /messages/{id}/pin in a server channel:
// only admins may pin there, and the pinned flag round-trips into the message list.
func TestRouterPinIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Pin Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	code, err := hs.store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if _, err := hs.store.RedeemInvite(ctx, code, member.ID); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	ch, err := hs.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	msg, err := hs.store.Save(ctx, ch.ID, owner.ID, owner.Username, "pin me")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	p := fmt.Sprintf("/api/messages/%d/pin", msg.ID)

	pinnedNow := func() bool {
		t.Helper()
		msgs, err := hs.store.Recent(ctx, ch.ID, owner.ID, 50)
		if err != nil {
			t.Fatalf("recent: %v", err)
		}
		for _, m := range msgs {
			if m.ID == msg.ID {
				return m.Pinned
			}
		}
		t.Fatalf("message %d not found in recent", msg.ID)
		return false
	}

	wantStatus(t, hs.req(t, "PUT", "/api/messages/abc/pin", ownerTok, ""), http.StatusBadRequest, "bad message id")
	wantStatus(t, hs.req(t, "PUT", p, memberTok, ""), http.StatusForbidden, "non-admin pins in server channel")
	if pinnedNow() {
		t.Fatal("message pinned after a forbidden attempt")
	}
	wantStatus(t, hs.req(t, "PUT", p, ownerTok, ""), http.StatusNoContent, "admin pins")
	if !pinnedNow() {
		t.Fatal("message not pinned after admin PUT")
	}
	wantStatus(t, hs.req(t, "DELETE", p, ownerTok, ""), http.StatusNoContent, "admin unpins")
	if pinnedNow() {
		t.Fatal("message still pinned after admin DELETE")
	}
}

// TestRouterPinsListIntegration covers GET /messages/pins: it returns only the
// channel's pinned messages, and is access-gated to channel members.
func TestRouterPinsListIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	_, strangerTok := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Pins List Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := hs.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	pinned, err := hs.store.Save(ctx, ch.ID, owner.ID, owner.Username, "pinned one")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := hs.store.Save(ctx, ch.ID, owner.ID, owner.Username, "not pinned"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := hs.store.SetMessagePinned(ctx, pinned.ID, owner.ID, true); err != nil {
		t.Fatalf("pin: %v", err)
	}

	path := fmt.Sprintf("/api/messages/pins?channel=%d", ch.ID)

	// A non-member can't read the channel's pins.
	wantStatus(t, hs.req(t, "GET", path, strangerTok, ""), http.StatusForbidden, "stranger reads pins")

	// The owner gets exactly the pinned message.
	w := hs.req(t, "GET", path, ownerTok, "")
	wantStatus(t, w, http.StatusOK, "owner reads pins")
	var pins []chat.Message
	if err := json.Unmarshal(w.Body.Bytes(), &pins); err != nil {
		t.Fatalf("decode pins: %v", err)
	}
	if len(pins) != 1 {
		t.Fatalf("got %d pins, want 1", len(pins))
	}
	if pins[0].ID != pinned.ID || !pins[0].Pinned {
		t.Fatalf("pins[0] = {id:%d pinned:%v}, want {id:%d pinned:true}", pins[0].ID, pins[0].Pinned, pinned.ID)
	}
}

// TestVoiceTokenIntegration covers the optional-SFU token endpoint (SPEC "mesh →
// OSS SFU"): unconfigured → {sfu:false} (client uses mesh); configured → a member
// gets a room-scoped LiveKit token, while a non-member gets 403 and NO token, and
// an unauthenticated caller gets 401 (Rule B / Rule 15 — never mint for a hostile
// or non-member caller).
func TestVoiceTokenIntegration(t *testing.T) {
	// 1) SFU unconfigured → sfu:false, no token.
	hs := newHarness(t)
	_, anyTok := hs.user(t)
	w := hs.req(t, "POST", "/api/voice/token?channel=1", anyTok, "")
	wantStatus(t, w, http.StatusOK, "unconfigured voice token")
	if !strings.Contains(w.Body.String(), `"sfu":false`) || strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("unconfigured: want sfu:false and no token, got %s", strings.TrimSpace(w.Body.String()))
	}

	// 2) SFU configured.
	const apiKey, apiSecret = "APItest", "sfu-secret-supersecret"
	hs2 := newHarnessCfg(t, config.Config{
		SFUURL: "wss://sfu.example", SFUKey: apiKey, SFUSecret: apiSecret,
	})
	ctx := context.Background()
	owner, ownerTok := hs2.user(t)
	_, outsiderTok := hs2.user(t)

	// A members-only server channel: owner is a member, outsider is not.
	srv, err := hs2.store.CreateServer(ctx, owner.ID, "Voice Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := hs2.store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	path := "/api/voice/token?channel=" + strconv.FormatInt(ch.ID, 10)

	// Unauthenticated → 401 (auth middleware).
	wantStatus(t, hs2.req(t, "POST", path, "", ""), http.StatusUnauthorized, "voice token unauth")

	// Non-member → 403 and NO token leaked.
	w403 := hs2.req(t, "POST", path, outsiderTok, "")
	wantStatus(t, w403, http.StatusForbidden, "voice token non-member")
	if strings.Contains(w403.Body.String(), "token") {
		t.Fatalf("non-member leaked a token: %s", strings.TrimSpace(w403.Body.String()))
	}

	// Member → a valid LiveKit token scoped to room = the channel.
	wOK := hs2.req(t, "POST", path, ownerTok, "")
	wantStatus(t, wOK, http.StatusOK, "voice token member")
	var resp struct {
		SFU   bool   `json:"sfu"`
		URL   string `json:"url"`
		Room  string `json:"room"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(wOK.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode token resp: %v", err)
	}
	wantRoom := "opencord-ch-" + strconv.FormatInt(ch.ID, 10)
	if !resp.SFU || resp.URL != "wss://sfu.example" || resp.Room != wantRoom || resp.Token == "" {
		t.Fatalf("member token resp = %+v (want sfu, url, room=%s, token)", resp, wantRoom)
	}
	// The token must verify under the SFU secret (server-minted, not forgeable) and
	// carry the channel room grant + this user's identity.
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(resp.Token, &claims, func(*jwt.Token) (any, error) {
		return []byte(apiSecret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("minted token failed to verify: %v", err)
	}
	if claims["iss"] != apiKey || claims["sub"] != "u"+strconv.FormatInt(owner.ID, 10) {
		t.Fatalf("token claims iss/sub wrong: %v / %v", claims["iss"], claims["sub"])
	}
	if video, _ := claims["video"].(map[string]any); video["room"] != wantRoom {
		t.Fatalf("token room grant = %v, want %v", video["room"], wantRoom)
	}
}

// TestPresenceEndpointIntegration drives the real PUT /me/presence + the member-list
// presence annotation: the caller always sees their OWN chosen state (incl. invisible),
// a disconnected other member reads as offline, an unknown value normalizes to online,
// and an unauthenticated set is rejected.
func TestPresenceEndpointIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	owner, ownerTok := hs.user(t)
	member, _ := hs.user(t)

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Presence Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := hs.store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// presenceOf fetches the member list AS ownerTok and returns the named user's
	// effective presence string (and online flag) as the owner sees it.
	presenceOf := func(uid int64) (string, bool) {
		w := hs.req(t, "GET", fmt.Sprintf("/api/servers/%d/members", srv.ID), ownerTok, "")
		wantStatus(t, w, http.StatusOK, "GET members")
		var ms []chat.ServerMember
		if err := json.Unmarshal(w.Body.Bytes(), &ms); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		for _, m := range ms {
			if m.UserID == uid {
				return m.Presence, m.Online
			}
		}
		t.Fatalf("member %d not in list", uid)
		return "", false
	}

	// Default: the owner's own row reads "online".
	if p, _ := presenceOf(owner.ID); p != "online" {
		t.Fatalf("default presence = %q, want online", p)
	}
	// A disconnected other member reads as offline to the owner (no live socket here).
	if p, on := presenceOf(member.ID); p != "offline" || on {
		t.Fatalf("disconnected member = (%q, online=%v), want (offline, false)", p, on)
	}
	// The caller sees their own chosen idle/dnd/invisible state (self special-case).
	for _, st := range []string{"dnd", "idle", "invisible"} {
		wantStatus(t, hs.req(t, "PUT", "/api/me/presence", ownerTok, fmt.Sprintf(`{"presence":%q}`, st)),
			http.StatusNoContent, "set presence "+st)
		if p, _ := presenceOf(owner.ID); p != st {
			t.Fatalf("after setting %q, own presence = %q, want %q", st, p, st)
		}
	}
	// An unknown value normalizes to online (Rule B — inert, not an error).
	wantStatus(t, hs.req(t, "PUT", "/api/me/presence", ownerTok, `{"presence":"bogus"}`),
		http.StatusNoContent, "set bogus presence")
	if p, _ := presenceOf(owner.ID); p != "online" {
		t.Fatalf("bogus presence should normalize to online, got %q", p)
	}
	// Unauthenticated set is rejected.
	wantStatus(t, hs.req(t, "PUT", "/api/me/presence", "", `{"presence":"dnd"}`),
		http.StatusUnauthorized, "unauth set presence")

	// Adversarial (Rule 15): malformed JSON and an oversized body (> the 4 KiB cap) are
	// 400s, never 500; a rejected body leaves the caller's presence unchanged. The last
	// accepted set above was the bogus->online normalization, so it must still read online.
	wantStatus(t, hs.req(t, "PUT", "/api/me/presence", ownerTok, `{bad`),
		http.StatusBadRequest, "malformed presence body -> 400")
	wantStatus(t, hs.req(t, "PUT", "/api/me/presence", ownerTok, `{"presence":"`+strings.Repeat("x", 5000)+`"}`),
		http.StatusBadRequest, "oversized presence body (>4KiB) -> 400")
	if p, _ := presenceOf(owner.ID); p != "online" {
		t.Fatalf("presence must be unchanged (online) after rejected hostile bodies, got %q", p)
	}
}

// TestCustomRolesAuthorizationIntegration is the route-level security proof for v0.7 custom
// colored roles (Rule 15): every mutation is admin-gated, cross-server role ids are rejected
// (IDOR), a non-member can't be assigned, and hostile input (bad color, oversized name/body,
// malformed id) is refused — encoding the adversarial probes so they can never silently regress.
func TestCustomRolesAuthorizationIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	ownerA, aTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)
	ownerB, bTok := hs.user(t)

	srvA, err := hs.store.CreateServer(ctx, ownerA.ID, "Roles A")
	if err != nil {
		t.Fatalf("create srvA: %v", err)
	}
	srvB, err := hs.store.CreateServer(ctx, ownerB.ID, "Roles B")
	if err != nil {
		t.Fatalf("create srvB: %v", err)
	}
	code, _ := hs.store.CreateInvite(ctx, srvA.ID, ownerA.ID)
	if _, err := hs.store.RedeemInvite(ctx, code, member.ID); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	roleA, err := hs.store.CreateServerRole(ctx, srvA.ID, ownerA.ID, "RoleA", "#3498db", false)
	if err != nil {
		t.Fatalf("create roleA: %v", err)
	}
	rolesA := fmt.Sprintf("/api/servers/%d/custom-roles", srvA.ID)

	t.Run("create is admin-gated", func(t *testing.T) {
		body := `{"name":"X","color":"#fff"}`
		wantStatus(t, hs.req(t, "POST", rolesA, "", body), http.StatusUnauthorized, "no-token create role")
		wantStatus(t, hs.req(t, "POST", rolesA, memberTok, body), http.StatusForbidden, "member creates role")
		wantStatus(t, hs.req(t, "POST", rolesA, strangerTok, body), http.StatusForbidden, "stranger creates role")
		wantStatus(t, hs.req(t, "GET", rolesA, strangerTok, ""), http.StatusForbidden, "stranger lists roles")
		wantStatus(t, hs.req(t, "GET", rolesA, memberTok, ""), http.StatusOK, "member lists roles")
	})

	t.Run("input validation", func(t *testing.T) {
		wantStatus(t, hs.req(t, "POST", rolesA, aTok, `{"name":"X","color":"red"}`), http.StatusBadRequest, "bad hex color")
		wantStatus(t, hs.req(t, "POST", rolesA, aTok, `{"name":"X","color":"#fff;background:url(x)"}`), http.StatusBadRequest, "CSS-injection color")
		wantStatus(t, hs.req(t, "POST", rolesA, aTok, `{"name":"`+strings.Repeat("a", 40)+`","color":"#fff"}`), http.StatusBadRequest, "oversized name")
		wantStatus(t, hs.req(t, "POST", rolesA, aTok, `{"name":"   ","color":"#fff"}`), http.StatusBadRequest, "blank name")
		wantStatus(t, hs.req(t, "DELETE", rolesA+"/abc", aTok, ""), http.StatusBadRequest, "malformed role id")
		wantStatus(t, hs.req(t, "POST", rolesA, aTok, `{"name":"X","color":"#fff","pad":"`+strings.Repeat("A", 1<<13)+`"}`), http.StatusBadRequest, "oversized body")
	})

	t.Run("cross-server role id is rejected (IDOR)", func(t *testing.T) {
		// ownerB is an admin of srvB but uses srvB's path with srvA's role id — must 404,
		// never tamper with another server's role.
		rolesB := fmt.Sprintf("/api/servers/%d/custom-roles/%d", srvB.ID, roleA.ID)
		wantStatus(t, hs.req(t, "PATCH", rolesB, bTok, `{"name":"pwned","color":"#000"}`), http.StatusNotFound, "B edits A's role via srvB path")
		wantStatus(t, hs.req(t, "DELETE", rolesB, bTok, ""), http.StatusNotFound, "B deletes A's role via srvB path")
		assignB := fmt.Sprintf("/api/servers/%d/members/%d/custom-roles/%d", srvB.ID, ownerB.ID, roleA.ID)
		wantStatus(t, hs.req(t, "PUT", assignB, bTok, ""), http.StatusNotFound, "B assigns A's role via srvB path")
	})

	t.Run("assign requires admin + a member target", func(t *testing.T) {
		assign := fmt.Sprintf("/api/servers/%d/members/%d/custom-roles/%d", srvA.ID, member.ID, roleA.ID)
		wantStatus(t, hs.req(t, "PUT", assign, memberTok, ""), http.StatusForbidden, "non-admin assigns")
		// A stranger (non-member of srvA) can't be assigned a srvA role.
		stranger, _ := hs.user(t)
		assignStranger := fmt.Sprintf("/api/servers/%d/members/%d/custom-roles/%d", srvA.ID, stranger.ID, roleA.ID)
		wantStatus(t, hs.req(t, "PUT", assignStranger, aTok, ""), http.StatusNotFound, "assign to a non-member")
		// The legit path works (the admin assigns the role to the member).
		wantStatus(t, hs.req(t, "PUT", assign, aTok, ""), http.StatusNoContent, "admin assigns to a member")
	})
}

// TestGroupDMAuthorizationIntegration is the route-level security proof for v0.6 group DMs
// (Rule 15): auth required, the member cap can't be bypassed, hostile/oversized/blocked input
// is refused, and a non-member can neither read nor (via upload) post to a group channel.
func TestGroupDMAuthorizationIntegration(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	a, aTok := hs.user(t)
	b, _ := hs.user(t)
	c, _ := hs.user(t)
	_, strangerTok := hs.user(t)
	blocked, _ := hs.user(t)

	groupPath := "/api/dms/group"

	t.Run("auth + input validation", func(t *testing.T) {
		ids := fmt.Sprintf(`{"identifiers":["%s"]}`, b.Username)
		wantStatus(t, hs.req(t, "POST", groupPath, "", ids), http.StatusUnauthorized, "no-token create group")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, `{"identifiers":[]}`), http.StatusBadRequest, "empty identifiers")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, `{"identifiers":["a","b","c","d","e","f","g","h","i","j","k"]}`), http.StatusBadRequest, "11 identifiers (cap bypass)")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, `{"identifiers":[`), http.StatusBadRequest, "malformed JSON")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, `{"identifiers":["`+strings.Repeat("A", 1<<14)+`"]}`), http.StatusBadRequest, "oversized body")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, fmt.Sprintf(`{"identifiers":["%s"]}`, a.Username)), http.StatusBadRequest, "self-only group")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, `{"identifiers":["' OR 1=1--"]}`), http.StatusNotFound, "injection-ish identifier -> not found, not 500")
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, fmt.Sprintf(`{"identifiers":["%s","ghost_nobody_xyz"]}`, b.Username)), http.StatusNotFound, "nonexistent member")
	})

	t.Run("a block forbids the group", func(t *testing.T) {
		if err := hs.store.BlockUser(ctx, a.ID, blocked.ID); err != nil {
			t.Fatalf("block: %v", err)
		}
		body := fmt.Sprintf(`{"identifiers":["%s","%s"]}`, b.Username, blocked.Username)
		wantStatus(t, hs.req(t, "POST", groupPath, aTok, body), http.StatusForbidden, "group including a blocked user")
	})

	t.Run("a non-member cannot read a group channel", func(t *testing.T) {
		grp, err := hs.store.CreateGroupDM(ctx, a.ID, []int64{b.ID, c.ID})
		if err != nil {
			t.Fatalf("create group: %v", err)
		}
		readPath := fmt.Sprintf("/api/messages?channel=%d", grp.ID)
		wantStatus(t, hs.req(t, "GET", readPath, strangerTok, ""), http.StatusForbidden, "non-member reads group history")
	})
}
