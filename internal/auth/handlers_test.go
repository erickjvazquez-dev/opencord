package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These exercise the hostile-input rejection paths of the auth handlers (Rule B/15):
// the body is bounded (MaxBytesReader) and the credentials are validated BEFORE any
// DB call, so a nil pool is fine — every case here returns 400 without touching the
// database. (Happy-path register/login is covered by the DB integration suites.)
func TestHandleRegisterRejectsBadInput(t *testing.T) {
	s := New(nil, []byte("test-secret"), time.Hour)

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"username": "alice", `},
		{"not json at all", `definitely not json`},
		{"username too short", `{"username":"ab","password":"password123"}`},
		{"username bad chars", `{"username":"has space","password":"password123"}`},
		{"password too short", `{"username":"validname","password":"12345"}`},
		// Body larger than the 64 KiB MaxBytesReader cap must be rejected, not buffered.
		{"oversized body", `{"username":"` + strings.Repeat("a", 70000) + `","password":"password123"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(tc.body))
			s.HandleRegister(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("HandleRegister(%s) = %d, want 400 (body: %s)", tc.name, rec.Code, strings.TrimSpace(rec.Body.String()))
			}
		})
	}
}

func TestHandleLoginRejectsMalformedBody(t *testing.T) {
	s := New(nil, []byte("test-secret"), time.Hour)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":`))
	s.HandleLogin(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("HandleLogin(malformed) = %d, want 400", rec.Code)
	}
}
