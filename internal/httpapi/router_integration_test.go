package httpapi_test

import (
	"context"
	"encoding/json"
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
		createdChannelID = c.ID
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
