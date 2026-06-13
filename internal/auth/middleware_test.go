package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenFromRequest(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*http.Request)
		want  string
	}{
		{"bearer header", func(r *http.Request) { r.Header.Set("Authorization", "Bearer abc.def.ghi") }, "abc.def.ghi"},
		{"query param (websocket path)", func(r *http.Request) { r.URL.RawQuery = "token=xyz.123" }, "xyz.123"},
		{"no token", func(r *http.Request) {}, ""},
		{"non-bearer scheme ignored", func(r *http.Request) { r.Header.Set("Authorization", "Basic zzz") }, ""},
		{"header beats query", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer H")
			r.URL.RawQuery = "token=Q"
		}, "H"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws", nil)
			c.setup(r)
			if got := TokenFromRequest(r); got != c.want {
				t.Fatalf("TokenFromRequest = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	svc := New(nil, []byte("secret"), time.Hour) // pool unused on the auth path
	valid, _ := svc.Issue(User{ID: 7, Username: "dave"})

	var seen User
	var reached bool
	protected := svc.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = UserFrom(r.Context())
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("rejects missing token with 401", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if reached {
			t.Fatal("handler must not run without a valid token")
		}
	})

	t.Run("rejects garbage token with 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		r.Header.Set("Authorization", "Bearer not-a-jwt")
		protected.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("accepts valid token and exposes the user", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		r.Header.Set("Authorization", "Bearer "+valid)
		protected.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !reached {
			t.Fatal("handler must run with a valid token")
		}
		if seen.ID != 7 || seen.Username != "dave" {
			t.Fatalf("UserFrom = %+v, want {ID:7 Username:dave}", seen)
		}
	})
}
