package httpapi_test

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
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping HTTP integration test")
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
	cfg := config.Config{CORSOrigin: "*"}
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
