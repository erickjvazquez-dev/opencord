package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/config"
)

// Custom server-emoji surface (POST/GET/DELETE /api/servers/{id}/emoji + GET
// /api/emoji/{id}), hardened per Rule 15: only an admin uploads/deletes, only a
// member lists, the bytes serve to any authed user, names are validated, non-images
// and oversized uploads are rejected, names are unique per server but not across
// servers, and you can't delete another server's emoji. SKIPS without DATABASE_URL.

// emojiReq drives a multipart POST /api/servers/{id}/emoji. data == nil → no file
// part; name == "" → no name field (so the "missing name" path is exercisable too).
func emojiReq(t *testing.T, hs harness, token string, serverID int64, name, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if name != "" {
		if err := mw.WriteField("name", name); err != nil {
			t.Fatalf("write name field: %v", err)
		}
	}
	if data != nil {
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/servers/%d/emoji", serverID), &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

// emojiCount returns how many emoji a server currently has (via the store, so the
// assertion doesn't depend on the list route being member-gated correctly).
func emojiCount(t *testing.T, hs harness, serverID int64) int {
	t.Helper()
	list, err := hs.store.ListServerEmoji(context.Background(), serverID)
	if err != nil {
		t.Fatalf("list emoji: %v", err)
	}
	return len(list)
}

func TestServerEmojiIntegration(t *testing.T) {
	hs := newHarnessCfg(t, config.Config{UploadDir: t.TempDir()})
	ctx := context.Background()

	owner, ownerTok := hs.user(t)   // server owner = admin
	member, memberTok := hs.user(t) // plain member
	_, strangerTok := hs.user(t)    // non-member

	srv, err := hs.store.CreateServer(ctx, owner.ID, "Emoji Guild")
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

	listURL := fmt.Sprintf("/api/servers/%d/emoji", srv.ID)

	var emojiID int64
	t.Run("happy path: admin uploads, lists, serves, deletes", func(t *testing.T) {
		// Admin uploads a valid PNG emoji.
		up := emojiReq(t, hs, ownerTok, srv.ID, "party_parrot", "p.png", pngBytes)
		wantStatus(t, up, http.StatusCreated, "admin uploads emoji")
		var created struct {
			ID       int64  `json:"id"`
			ServerID int64  `json:"serverId"`
			Name     string `json:"name"`
		}
		if err := json.Unmarshal(up.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode created emoji: %v", err)
		}
		if created.ID == 0 || created.Name != "party_parrot" || created.ServerID != srv.ID {
			t.Fatalf("unexpected created emoji %+v", created)
		}
		emojiID = created.ID

		// It appears in the member's list.
		ls := hs.req(t, http.MethodGet, listURL, memberTok, "")
		wantStatus(t, ls, http.StatusOK, "member lists emoji")
		var list []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(ls.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(list) != 1 || list[0].ID != emojiID || list[0].Name != "party_parrot" {
			t.Fatalf("unexpected list %+v", list)
		}

		// A member can fetch the bytes with the right content type.
		dl := hs.req(t, http.MethodGet, fmt.Sprintf("/api/emoji/%d", emojiID), memberTok, "")
		wantStatus(t, dl, http.StatusOK, "member serves emoji bytes")
		if !bytes.Equal(dl.Body.Bytes(), pngBytes) {
			t.Fatalf("served emoji bytes differ from upload")
		}
		if ct := dl.Header().Get("Content-Type"); ct != "image/png" {
			t.Fatalf("content type = %q, want image/png", ct)
		}
		if got := dl.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("nosniff = %q, want nosniff", got)
		}

		// Admin deletes it → list is empty again.
		del := hs.req(t, http.MethodDelete, fmt.Sprintf("%s/%d", listURL, emojiID), ownerTok, "")
		wantStatus(t, del, http.StatusNoContent, "admin deletes emoji")
		if n := emojiCount(t, hs, srv.ID); n != 0 {
			t.Fatalf("emoji count after delete = %d, want 0", n)
		}
		// Serving the deleted emoji is now 404.
		wantStatus(t, hs.req(t, http.MethodGet, fmt.Sprintf("/api/emoji/%d", emojiID), memberTok, ""),
			http.StatusNotFound, "serve deleted emoji")
	})

	t.Run("a non-admin member cannot upload or delete", func(t *testing.T) {
		// Seed one emoji as the admin so there's something a member might try to delete.
		up := emojiReq(t, hs, ownerTok, srv.ID, "seeded", "s.png", pngBytes)
		wantStatus(t, up, http.StatusCreated, "admin seeds emoji")
		var seeded struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(up.Body.Bytes(), &seeded); err != nil {
			t.Fatalf("decode seeded: %v", err)
		}

		// Member uploads → 403, and nothing is created.
		before := emojiCount(t, hs, srv.ID)
		wantStatus(t, emojiReq(t, hs, memberTok, srv.ID, "member_made", "m.png", pngBytes),
			http.StatusForbidden, "member uploads emoji")
		if after := emojiCount(t, hs, srv.ID); after != before {
			t.Fatalf("member upload created a row: count %d → %d", before, after)
		}

		// Member deletes the seeded emoji → 403, and it still exists.
		wantStatus(t, hs.req(t, http.MethodDelete, fmt.Sprintf("%s/%d", listURL, seeded.ID), memberTok, ""),
			http.StatusForbidden, "member deletes emoji")
		if n := emojiCount(t, hs, srv.ID); n != before {
			t.Fatalf("member delete changed count: %d → %d", before, n)
		}

		// Clean up the seed so later subtests start from a known state.
		wantStatus(t, hs.req(t, http.MethodDelete, fmt.Sprintf("%s/%d", listURL, seeded.ID), ownerTok, ""),
			http.StatusNoContent, "admin cleans up seed")
	})

	t.Run("a non-member cannot list", func(t *testing.T) {
		wantStatus(t, hs.req(t, http.MethodGet, listURL, strangerTok, ""),
			http.StatusForbidden, "stranger lists emoji")
		// A non-member also can't upload (admin check fails before membership matters).
		wantStatus(t, emojiReq(t, hs, strangerTok, srv.ID, "stranger", "x.png", pngBytes),
			http.StatusForbidden, "stranger uploads emoji")
	})

	t.Run("invalid emoji names are 400", func(t *testing.T) {
		bad := map[string]string{
			"spaces+punct": "BAD NAME!",
			"empty":        "",
			"too long":     "this_name_is_definitely_way_too_long_for_an_emoji_slug", // 53 chars
			"too short":    "a",
			"uppercase":    "Party",
			"colon":        ":party:",
		}
		for what, name := range bad {
			before := emojiCount(t, hs, srv.ID)
			w := emojiReq(t, hs, ownerTok, srv.ID, name, "n.png", pngBytes)
			wantStatus(t, w, http.StatusBadRequest, "invalid name: "+what)
			if after := emojiCount(t, hs, srv.ID); after != before {
				t.Fatalf("invalid name %q created a row: %d → %d", name, before, after)
			}
		}
	})

	t.Run("oversized image is rejected, no row created", func(t *testing.T) {
		big := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, (256<<10)+1)...) // >256 KiB, sniffs png
		before := emojiCount(t, hs, srv.ID)
		w := emojiReq(t, hs, ownerTok, srv.ID, "huge", "huge.png", big)
		if w.Code/100 == 2 {
			t.Fatalf("oversized upload status = %d, want non-2xx", w.Code)
		}
		if after := emojiCount(t, hs, srv.ID); after != before {
			t.Fatalf("oversized upload created a row: %d → %d", before, after)
		}
	})

	t.Run("non-image bytes are rejected", func(t *testing.T) {
		before := emojiCount(t, hs, srv.ID)
		// %PDF sniffs as application/pdf; plain text sniffs as text/plain — both 4xx.
		for what, data := range map[string][]byte{
			"pdf":  []byte("%PDF-1.7\nnot really an image"),
			"text": []byte("just some text, not an image at all"),
		} {
			w := emojiReq(t, hs, ownerTok, srv.ID, "not_image", "x.png", data)
			if w.Code/100 != 4 {
				t.Fatalf("non-image (%s) status = %d, want 4xx", what, w.Code)
			}
		}
		if after := emojiCount(t, hs, srv.ID); after != before {
			t.Fatalf("non-image upload created a row: %d → %d", before, after)
		}
	})

	t.Run("duplicate name in same server is 409; same name in a different server is allowed", func(t *testing.T) {
		wantStatus(t, emojiReq(t, hs, ownerTok, srv.ID, "dup", "d.png", pngBytes),
			http.StatusCreated, "first dup upload")
		// Same name, same server → 409.
		wantStatus(t, emojiReq(t, hs, ownerTok, srv.ID, "dup", "d2.png", pngBytes),
			http.StatusConflict, "duplicate name same server")

		// A second server owned by the same admin may reuse the name.
		srv2, err := hs.store.CreateServer(ctx, owner.ID, "Other Guild")
		if err != nil {
			t.Fatalf("create second server: %v", err)
		}
		wantStatus(t, emojiReq(t, hs, ownerTok, srv2.ID, "dup", "d3.png", pngBytes),
			http.StatusCreated, "same name in different server")
	})

	t.Run("deleting via the wrong server id is 404 and the emoji survives", func(t *testing.T) {
		// Create an emoji in srv, then try to delete it via a different server's path.
		up := emojiReq(t, hs, ownerTok, srv.ID, "scoped", "sc.png", pngBytes)
		wantStatus(t, up, http.StatusCreated, "create scoped emoji")
		var e struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(up.Body.Bytes(), &e); err != nil {
			t.Fatalf("decode scoped: %v", err)
		}
		// Owner-owned second server so the admin check passes but the scope check fails.
		srv2, err := hs.store.CreateServer(ctx, owner.ID, "Wrong Guild")
		if err != nil {
			t.Fatalf("create wrong server: %v", err)
		}
		wantStatus(t, hs.req(t, http.MethodDelete, fmt.Sprintf("/api/servers/%d/emoji/%d", srv2.ID, e.ID), ownerTok, ""),
			http.StatusNotFound, "delete via wrong server id")
		// The emoji is still servable (it wasn't deleted).
		wantStatus(t, hs.req(t, http.MethodGet, fmt.Sprintf("/api/emoji/%d", e.ID), ownerTok, ""),
			http.StatusOK, "scoped emoji survives wrong-server delete")
	})

	t.Run("unauthenticated upload, list, serve, delete are 401", func(t *testing.T) {
		wantStatus(t, emojiReq(t, hs, "", srv.ID, "anon", "a.png", pngBytes), http.StatusUnauthorized, "anon upload")
		wantStatus(t, hs.req(t, http.MethodGet, listURL, "", ""), http.StatusUnauthorized, "anon list")
		wantStatus(t, hs.req(t, http.MethodGet, "/api/emoji/1", "", ""), http.StatusUnauthorized, "anon serve")
		wantStatus(t, hs.req(t, http.MethodDelete, listURL+"/1", "", ""), http.StatusUnauthorized, "anon delete")
	})
}
