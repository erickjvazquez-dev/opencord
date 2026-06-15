package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/config"
)

// Avatar surface (POST /api/avatar + GET /api/users/{id}/avatar), hardened per
// Rule 15: a non-image is rejected, oversized is rejected, you can only set your
// OWN avatar (JWT-derived), and auth is required. SKIPS without DATABASE_URL.

// avatarReq drives a multipart POST /api/avatar (field "file"). nil data → no file part.
func avatarReq(t *testing.T, hs harness, token, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
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
	r := httptest.NewRequest(http.MethodPost, "/api/avatar", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

func TestAvatarsIntegration(t *testing.T) {
	hs := newHarnessCfg(t, config.Config{UploadDir: t.TempDir()})
	_ = context.Background()

	alice, aliceTok := hs.user(t)
	bob, bobTok := hs.user(t)
	avatarURL := func(id int64) string { return fmt.Sprintf("/api/users/%d/avatar", id) }

	t.Run("no avatar yet → serve is 404 (client falls back to initials)", func(t *testing.T) {
		w := hs.req(t, http.MethodGet, avatarURL(alice.ID), aliceTok, "")
		wantStatus(t, w, http.StatusNotFound, "serve before upload")
	})

	t.Run("happy path: set own avatar, then anyone authed can fetch it", func(t *testing.T) {
		up := avatarReq(t, hs, aliceTok, "me.png", pngBytes)
		wantStatus(t, up, http.StatusOK, "upload avatar")
		// Bob (a different user) can fetch Alice's avatar — avatars are public in-instance.
		dl := hs.req(t, http.MethodGet, avatarURL(alice.ID), bobTok, "")
		wantStatus(t, dl, http.StatusOK, "bob fetches alice avatar")
		if !bytes.Equal(dl.Body.Bytes(), pngBytes) {
			t.Fatalf("served avatar bytes differ from upload")
		}
		if got := dl.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("nosniff = %q, want nosniff", got)
		}
		if ct := dl.Header().Get("Content-Type"); ct != "image/png" {
			t.Fatalf("content type = %q, want image/png", ct)
		}
	})

	t.Run("setting your avatar never touches another user's", func(t *testing.T) {
		// Alice has an avatar (above); Bob set none → Bob's is still 404.
		w := hs.req(t, http.MethodGet, avatarURL(bob.ID), aliceTok, "")
		wantStatus(t, w, http.StatusNotFound, "bob still has no avatar after alice uploaded")
	})

	t.Run("non-image is rejected", func(t *testing.T) {
		w := avatarReq(t, hs, bobTok, "notes.txt", []byte("just some text, not an image at all"))
		wantStatus(t, w, http.StatusBadRequest, "upload non-image avatar")
		// Bob still has no avatar.
		g := hs.req(t, http.MethodGet, avatarURL(bob.ID), bobTok, "")
		wantStatus(t, g, http.StatusNotFound, "bob avatar still unset after rejected upload")
	})

	t.Run("oversized avatar is rejected", func(t *testing.T) {
		big := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, (2<<20)+1)...) // >2 MiB, sniffs png
		w := avatarReq(t, hs, bobTok, "huge.png", big)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized status = %d, want 413", w.Code)
		}
	})

	t.Run("no file part is a 400", func(t *testing.T) {
		w := avatarReq(t, hs, bobTok, "", nil)
		wantStatus(t, w, http.StatusBadRequest, "upload with no file")
	})

	t.Run("unauthenticated upload + serve are 401", func(t *testing.T) {
		up := avatarReq(t, hs, "", "me.png", pngBytes)
		wantStatus(t, up, http.StatusUnauthorized, "upload without token")
		dl := hs.req(t, http.MethodGet, avatarURL(alice.ID), "", "")
		wantStatus(t, dl, http.StatusUnauthorized, "serve without token")
	})

	t.Run("replacing your avatar serves the new bytes", func(t *testing.T) {
		gif := append([]byte("GIF89a"), make([]byte, 32)...) // sniffs image/gif
		up := avatarReq(t, hs, aliceTok, "new.gif", gif)
		wantStatus(t, up, http.StatusOK, "replace avatar with a gif")
		dl := hs.req(t, http.MethodGet, avatarURL(alice.ID), aliceTok, "")
		wantStatus(t, dl, http.StatusOK, "fetch replaced avatar")
		if ct := dl.Header().Get("Content-Type"); ct != "image/gif" {
			t.Fatalf("content type = %q, want image/gif after replace", ct)
		}
	})
}
