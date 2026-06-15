package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/ws"
	"github.com/go-chi/chi/v5"
)

// Attachment limits (Rule B: bound every inbound payload).
const (
	maxAttachmentsPerMessage = 10
	maxAttachmentBytes       = 8 << 20  // 8 MiB per file
	maxUploadBytes           = 40 << 20 // 40 MiB per request (body + all files)
	maxAttachmentBody        = 4096     // optional message body cap (matches the WS cap)
)

// inlineImageTypes are the sniffed content types served inline; everything else is
// served as a download (Content-Disposition: attachment + nosniff) so an uploaded
// .html / SVG can never execute script in our origin.
var inlineImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// storageKey returns a 32-hex-char opaque on-disk name (16 bytes of crypto/rand).
// The client's filename is NEVER used as a path, so path traversal is impossible.
func storageKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// handleUploadMessage handles POST /api/messages (multipart/form-data): the one path
// that creates a message carrying attachments. Form fields: channelId (required),
// body (optional, ≤4096), replyTo (optional), files (1..10). It derives the user
// from the JWT (Rule C), access-gates the channel (Rule B), writes each file to disk
// under an opaque key, then creates the message + attachment rows atomically and
// broadcasts the finished message over the hub like any other message.
func handleUploadMessage(uploadDir string, store *chat.Store, hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFrom(r.Context())
		// Bound the whole request before touching it (Rule B).
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			http.Error(w, `{"error":"invalid or oversized upload (max 40 MiB)"}`, http.StatusRequestEntityTooLarge)
			return
		}
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }() // drop any temp spill files
		}

		channelID, err := strconv.ParseInt(r.FormValue("channelId"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
			return
		}
		// Access gate up front (defense in depth; the store also enforces post policy).
		if ok, err := store.CanAccessChannel(r.Context(), channelID, u.ID); err != nil {
			http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		body := strings.TrimSpace(r.FormValue("body"))
		if len(body) > maxAttachmentBody {
			http.Error(w, `{"error":"message body too long"}`, http.StatusBadRequest)
			return
		}
		var replyTo *int64
		if rt := r.FormValue("replyTo"); rt != "" {
			if id, err := strconv.ParseInt(rt, 10, 64); err == nil {
				replyTo = &id
			}
		}

		var files []*multipart.FileHeader
		if r.MultipartForm != nil {
			files = r.MultipartForm.File["files"]
		}
		if len(files) == 0 {
			http.Error(w, `{"error":"no files attached"}`, http.StatusBadRequest)
			return
		}
		if len(files) > maxAttachmentsPerMessage {
			http.Error(w, `{"error":"too many files (max 10)"}`, http.StatusBadRequest)
			return
		}

		if err := os.MkdirAll(uploadDir, 0o755); err != nil {
			http.Error(w, `{"error":"storage unavailable"}`, http.StatusInternalServerError)
			return
		}

		// Write each file to disk (sniffing its real content type), tracking written
		// paths so a later failure cleans up — no orphan files or rows.
		var written []string
		cleanup := func() {
			for _, p := range written {
				_ = os.Remove(p)
			}
		}
		atts := make([]chat.NewAttachment, 0, len(files))
		for _, fh := range files {
			if fh.Size > maxAttachmentBytes {
				cleanup()
				http.Error(w, `{"error":"a file exceeds the 8 MiB limit"}`, http.StatusRequestEntityTooLarge)
				return
			}
			key, err := storageKey()
			if err != nil {
				cleanup()
				http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
				return
			}
			dst := filepath.Join(uploadDir, key)
			ct, size, err := saveUpload(fh, dst)
			if err != nil {
				cleanup()
				http.Error(w, `{"error":"could not store file"}`, http.StatusInternalServerError)
				return
			}
			written = append(written, dst)
			atts = append(atts, chat.NewAttachment{
				StorageKey:  key,
				Filename:    sanitizeFilename(fh.Filename),
				ContentType: ct,
				Size:        size,
			})
		}

		m, err := store.SaveWithAttachments(r.Context(), channelID, u.ID, u.Username, body, replyTo, atts)
		switch {
		case errors.Is(err, chat.ErrForbidden):
			cleanup()
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		case errors.Is(err, chat.ErrSlowMode):
			cleanup()
			http.Error(w, `{"error":"slow mode active"}`, http.StatusTooManyRequests)
			return
		case err != nil:
			cleanup()
			http.Error(w, `{"error":"could not send message"}`, http.StatusInternalServerError)
			return
		}
		hub.BroadcastEvent(ws.Event{Type: "message", Message: &m})
		writeJSON(w, http.StatusCreated, m)
	}
}

// saveUpload streams a multipart file to dst, returning the SNIFFED content type
// (http.DetectContentType on the leading bytes — never the client's claimed type)
// and the number of bytes written. On any error the partial dst file is removed.
func saveUpload(fh *multipart.FileHeader, dst string) (string, int64, error) {
	src, err := fh.Open()
	if err != nil {
		return "", 0, err
	}
	defer src.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	ct := http.DetectContentType(head[:n])
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}

	// O_EXCL: the key is unique, so this never clobbers an existing file.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", 0, err
	}
	written, err := io.Copy(out, src)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", 0, err
	}
	return ct, written, nil
}

// handleServeAttachment handles GET /api/attachments/{id}: the access-gated file
// serve. It resolves the attachment → its message's channel and runs
// CanAccessChannel (Rule B/C) — a non-member gets 403, never the bytes. The file is
// served with its sniffed content type + nosniff, inline only for the image
// allowlist (else as a download).
func handleServeAttachment(uploadDir string, store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFrom(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid attachment id"}`, http.StatusBadRequest)
			return
		}
		key, ct, filename, channelID, err := store.AttachmentForServe(r.Context(), id)
		if errors.Is(err, chat.ErrMessageNotFound) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
			return
		}
		if ok, err := store.CanAccessChannel(r.Context(), channelID, u.ID); err != nil {
			http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
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
		disp := "attachment"
		if inlineImageTypes[ct] {
			disp = "inline"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", disp+`; filename="`+sanitizeHeaderFilename(filename)+`"`)
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, filename, info.ModTime(), f)
	}
}

// sanitizeFilename reduces a client filename to a safe display string: base name
// only, no control/path characters, bounded length. It is metadata only (never a
// path), but it is echoed to clients, so it's cleaned. Empty → "file".
func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name
}

// sanitizeHeaderFilename strips characters that could break out of the quoted
// Content-Disposition filename (header injection / quote escape).
func sanitizeHeaderFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
}
