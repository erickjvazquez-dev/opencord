package httpapi

import (
	"errors"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/go-chi/chi/v5"
)

// maxEmojiBytes bounds a custom-emoji upload (Rule B). Emoji are tiny images.
const maxEmojiBytes = 256 << 10 // 256 KiB

// handleUploadServerEmoji handles POST /api/servers/{id}/emoji (multipart): the caller
// (already admin-gated by the route) uploads a named image emoji for the server. Form
// fields: name (a [a-z0-9_]{2,32} slug) + file. Image-only (sniffed must be in the
// inline allowlist — no SVG/script), ≤256 KiB. The bytes go to disk under an opaque
// key (no client filename → no traversal); a duplicate name is a 409. Mirrors the
// avatar upload (Rule C: caller derived from the JWT).
func handleUploadServerEmoji(uploadDir string, store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFrom(r.Context())
		serverID, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		// Bound the whole request before touching it (Rule B): the file + the small
		// name field, plus a little multipart framing slack.
		r.Body = http.MaxBytesReader(w, r.Body, maxEmojiBytes+4096)
		if err := r.ParseMultipartForm(maxEmojiBytes); err != nil {
			http.Error(w, `{"error":"invalid or oversized upload (max 256 KiB)"}`, http.StatusRequestEntityTooLarge)
			return
		}
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}

		name := strings.TrimSpace(r.FormValue("name"))
		if !chat.ValidEmojiName(name) {
			http.Error(w, `{"error":"emoji name must be 2-32 chars of [a-z0-9_]"}`, http.StatusBadRequest)
			return
		}

		var fh *multipart.FileHeader
		if r.MultipartForm != nil && len(r.MultipartForm.File["file"]) > 0 {
			fh = r.MultipartForm.File["file"][0]
		}
		if fh == nil {
			http.Error(w, `{"error":"no file"}`, http.StatusBadRequest)
			return
		}
		if fh.Size > maxEmojiBytes {
			http.Error(w, `{"error":"emoji exceeds the 256 KiB limit"}`, http.StatusRequestEntityTooLarge)
			return
		}
		if err := os.MkdirAll(uploadDir, 0o755); err != nil {
			http.Error(w, `{"error":"storage unavailable"}`, http.StatusInternalServerError)
			return
		}
		key, err := storageKey()
		if err != nil {
			http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
			return
		}
		dst := filepath.Join(uploadDir, key)
		ct, _, err := saveUpload(fh, dst)
		if err != nil {
			http.Error(w, `{"error":"could not store emoji"}`, http.StatusInternalServerError)
			return
		}
		// Must sniff as an allowlisted image — reject anything else (no scripts/SVG).
		if !inlineImageTypes[ct] {
			_ = os.Remove(dst)
			http.Error(w, `{"error":"emoji must be a png, jpeg, gif, or webp image"}`, http.StatusBadRequest)
			return
		}
		e, err := store.CreateServerEmoji(r.Context(), serverID, name, key, ct, u.ID)
		if errors.Is(err, chat.ErrEmojiExists) {
			_ = os.Remove(dst)
			http.Error(w, `{"error":"emoji name already taken in this server"}`, http.StatusConflict)
			return
		}
		if err != nil {
			_ = os.Remove(dst)
			http.Error(w, `{"error":"could not create emoji"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, e)
	}
}

// handleListServerEmoji handles GET /api/servers/{id}/emoji (member-gated by the
// route): returns the server's custom emoji as JSON (always an array).
func handleListServerEmoji(store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		list, err := store.ListServerEmoji(r.Context(), serverID)
		if err != nil {
			http.Error(w, `{"error":"could not load emoji"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// handleServeEmoji handles GET /api/emoji/{id}: serves an emoji's image bytes to any
// authenticated user. Emoji are renderable wherever a message is shown (and a message
// may quote/cross-reference another channel), so emoji are public within the instance
// like avatars — not access-gated per server. Always served with nosniff + inline.
func handleServeEmoji(uploadDir string, store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid emoji id"}`, http.StatusBadRequest)
			return
		}
		key, ct, err := store.ServerEmojiForServe(r.Context(), id)
		if errors.Is(err, chat.ErrEmojiNotFound) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
			return
		}
		// The key is server-generated hex; Base it defensively anyway so a path can
		// never escape the upload dir even if the DB were tampered with.
		f, err := os.Open(filepath.Join(uploadDir, filepath.Base(key)))
		if err != nil {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("Content-Disposition", "inline")
		http.ServeContent(w, r, key, info.ModTime(), f)
	}
}

// handleDeleteServerEmoji handles DELETE /api/servers/{id}/emoji/{emojiId} (admin-gated
// by the route): removes the emoji row (scoped by server_id so a foreign emoji can't be
// deleted — 404) and best-effort removes its on-disk file. 204 on success.
func handleDeleteServerEmoji(uploadDir string, store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverID, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		emojiID, err := strconv.ParseInt(chi.URLParam(r, "emojiId"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid emoji id"}`, http.StatusBadRequest)
			return
		}
		// Resolve the on-disk key first (scoped delete then makes the authorization
		// the source of truth) so we can clean up the file after a successful delete.
		key, _, keyErr := store.ServerEmojiForServe(r.Context(), emojiID)
		switch err := store.DeleteServerEmoji(r.Context(), serverID, emojiID); {
		case errors.Is(err, chat.ErrEmojiNotFound):
			http.Error(w, `{"error":"emoji not found"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not delete emoji"}`, http.StatusInternalServerError)
		default:
			if keyErr == nil && key != "" {
				_ = os.Remove(filepath.Join(uploadDir, filepath.Base(key)))
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}
