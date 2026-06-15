package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/config"
)

// This suite hammers the file-attachment surface (POST /api/messages multipart +
// GET /api/attachments/{id}) the way a hostile user would (Rule 15): path-traversal
// filenames, oversized files, an HTML payload that must never render inline, and
// non-member access to both upload and download. It SKIPS without DATABASE_URL.

type uploadFile struct {
	name string
	data []byte
}

// pngBytes is a tiny buffer that sniffs as image/png (valid 8-byte signature).
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

// uploadReq drives a multipart POST /api/messages through the real router.
func uploadReq(t *testing.T, hs harness, token string, channelID int64, body string, files []uploadFile) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("channelId", strconv.FormatInt(channelID, 10))
	if body != "" {
		_ = mw.WriteField("body", body)
	}
	for _, f := range files {
		fw, err := mw.CreateFormFile("files", f.name)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(f.data); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/messages", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	hs.h.ServeHTTP(w, r)
	return w
}

func decodeMessage(t *testing.T, w *httptest.ResponseRecorder) chat.Message {
	t.Helper()
	var m chat.Message
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode message: %v (body: %s)", err, w.Body.String())
	}
	return m
}

func TestAttachmentsIntegration(t *testing.T) {
	uploadDir := t.TempDir()
	hs := newHarnessCfg(t, config.Config{UploadDir: uploadDir})
	ctx := context.Background()

	owner, ownerTok := hs.user(t)
	member, memberTok := hs.user(t)
	_, strangerTok := hs.user(t)

	general, err := hs.store.DefaultChannelID(ctx)
	if err != nil {
		t.Fatalf("default channel: %v", err)
	}

	t.Run("happy path: upload image then download it", func(t *testing.T) {
		w := uploadReq(t, hs, ownerTok, general, "look at this", []uploadFile{{"pic.png", pngBytes}})
		wantStatus(t, w, http.StatusCreated, "upload png")
		m := decodeMessage(t, w)
		if len(m.Attachments) != 1 {
			t.Fatalf("attachments = %d, want 1", len(m.Attachments))
		}
		a := m.Attachments[0]
		if a.ContentType != "image/png" {
			t.Fatalf("content type = %q, want image/png (sniffed)", a.ContentType)
		}
		if a.Filename != "pic.png" {
			t.Fatalf("filename = %q, want pic.png", a.Filename)
		}
		// Download it back — bytes must match, served inline + nosniff.
		dl := hs.req(t, http.MethodGet, a.URL, ownerTok, "")
		wantStatus(t, dl, http.StatusOK, "download png")
		if !bytes.Equal(dl.Body.Bytes(), pngBytes) {
			t.Fatalf("downloaded bytes differ from upload")
		}
		if got := dl.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("nosniff = %q, want nosniff", got)
		}
		if disp := dl.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "inline") {
			t.Fatalf("image disposition = %q, want inline", disp)
		}
	})

	t.Run("body optional when a file is attached", func(t *testing.T) {
		w := uploadReq(t, hs, ownerTok, general, "", []uploadFile{{"f.png", pngBytes}})
		wantStatus(t, w, http.StatusCreated, "upload with empty body")
	})

	t.Run("no files is a 400", func(t *testing.T) {
		w := uploadReq(t, hs, ownerTok, general, "text only", nil)
		wantStatus(t, w, http.StatusBadRequest, "upload zero files")
	})

	t.Run("unauthenticated upload is 401", func(t *testing.T) {
		w := uploadReq(t, hs, "", general, "", []uploadFile{{"f.png", pngBytes}})
		wantStatus(t, w, http.StatusUnauthorized, "upload without token")
	})

	t.Run("oversized file is rejected, nothing stored", func(t *testing.T) {
		before := countFiles(t, uploadDir)
		big := make([]byte, (8<<20)+1) // 8 MiB + 1
		w := uploadReq(t, hs, ownerTok, general, "", []uploadFile{{"big.bin", big}})
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized status = %d, want 413", w.Code)
		}
		if after := countFiles(t, uploadDir); after != before {
			t.Fatalf("oversized upload wrote a file (%d -> %d)", before, after)
		}
	})

	t.Run("path-traversal filename can't escape the upload dir", func(t *testing.T) {
		before := countFiles(t, uploadDir)
		w := uploadReq(t, hs, ownerTok, general, "", []uploadFile{
			{"../../../../etc/passwd", pngBytes},
		})
		wantStatus(t, w, http.StatusCreated, "traversal filename upload")
		m := decodeMessage(t, w)
		if m.Attachments[0].Filename != "passwd" {
			t.Fatalf("display filename = %q, want sanitized to passwd", m.Attachments[0].Filename)
		}
		// Exactly one new file, and it lives flat inside uploadDir (opaque key, no subdirs).
		if after := countFiles(t, uploadDir); after != before+1 {
			t.Fatalf("file count %d -> %d, want +1 (written inside dir)", before, after)
		}
	})

	t.Run("HTML payload is served as a download, never inline", func(t *testing.T) {
		htmlBytes := []byte("<html><body><script>alert(document.cookie)</script></body></html>")
		w := uploadReq(t, hs, ownerTok, general, "", []uploadFile{{"x.html", htmlBytes}})
		wantStatus(t, w, http.StatusCreated, "upload html")
		a := decodeMessage(t, w).Attachments[0]
		dl := hs.req(t, http.MethodGet, a.URL, ownerTok, "")
		wantStatus(t, dl, http.StatusOK, "download html")
		if disp := dl.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "attachment") {
			t.Fatalf("html disposition = %q, want attachment", disp)
		}
		if got := dl.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("nosniff = %q, want nosniff", got)
		}
	})

	t.Run("non-member can't upload to or download from a DM", func(t *testing.T) {
		// A DM between owner and member; the stranger is not a member.
		dm, err := hs.store.CreateOrGetDM(ctx, member.ID, owner.ID)
		if err != nil {
			t.Fatalf("create dm: %v", err)
		}
		// Member (in the DM) uploads fine.
		up := uploadReq(t, hs, memberTok, dm.ID, "secret", []uploadFile{{"s.png", pngBytes}})
		wantStatus(t, up, http.StatusCreated, "member upload to dm")
		a := decodeMessage(t, up).Attachments[0]
		// Stranger can't upload to the DM channel.
		su := uploadReq(t, hs, strangerTok, dm.ID, "", []uploadFile{{"s.png", pngBytes}})
		wantStatus(t, su, http.StatusForbidden, "stranger upload to dm")
		// Stranger can't download the member's DM attachment.
		sd := hs.req(t, http.MethodGet, a.URL, strangerTok, "")
		wantStatus(t, sd, http.StatusForbidden, "stranger download dm attachment")
	})

	t.Run("missing attachment is 404", func(t *testing.T) {
		w := hs.req(t, http.MethodGet, "/api/attachments/999999999", ownerTok, "")
		wantStatus(t, w, http.StatusNotFound, "download missing attachment")
	})
}

// countFiles returns how many regular files live directly under dir.
func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read upload dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}
