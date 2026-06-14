package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The embedded SPA handler must serve index.html for "/" and fall back to it for
// unknown client-side routes (History-API routing), always as HTML. In CI the
// embedded dist is just the committed placeholder, which is enough to assert this.
func TestHandlerServesSPA(t *testing.T) {
	h := Handler()
	// (Note: /index.html is intentionally omitted — http.FileServer 301-redirects
	// it to "/" by canonicalization; the SPA is always entered at "/".)
	for _, path := range []string{"/", "/some/client/route", "/login"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Fatalf("GET %s content-type = %q, want text/html", path, ct)
		}
		if !strings.Contains(w.Body.String(), "Opencord") {
			t.Fatalf("GET %s did not serve the SPA shell (index.html)", path)
		}
	}
}
