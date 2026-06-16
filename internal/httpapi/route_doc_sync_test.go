package httpapi

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/config"
	"github.com/erickjvazquez-dev/opencord/internal/ws"
)

// TestREADMEAPITableMatchesRoutes guards the README "API surface" table against
// drift (Rule 14): every route the router actually registers must be documented,
// and the table must not list a route that no longer exists. This is the automated
// form of the manual route-vs-README cross-check that caught ~35 endpoints of drift
// in tick 125 — a doc table that enumerates code is only trustworthy if it is
// diff-verified against the code. When you add/rename/remove an endpoint, update the
// README table in the same change and this test stays green.
//
// It walks the REAL chi router (not a regex over source), so it can't be fooled by
// how a route is registered. No DB: route registration never calls the store/hub, so
// zero-value deps are enough to enumerate the wiring.
func TestREADMEAPITableMatchesRoutes(t *testing.T) {
	h := New(config.Config{CORSOrigin: "*"}, &auth.Service{}, &chat.Store{}, &ws.Hub{})
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("router is %T, not chi.Routes — can't enumerate routes", h)
	}

	actual := map[string]bool{}
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		method = strings.ToUpper(method)
		// OPTIONS/HEAD are CORS/protocol plumbing, and "/*" is the SPA catch-all
		// (static assets, served by webui) — none are documented API endpoints.
		if method == "OPTIONS" || method == "HEAD" || method == "*" {
			return nil
		}
		route = strings.TrimSuffix(route, "/")
		if route == "" || route == "/*" {
			return nil
		}
		actual[method+" "+route] = true
		return nil
	}
	if err := chi.Walk(routes, walk); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(actual) == 0 {
		t.Fatal("walked 0 API routes — router wiring changed?")
	}

	readme := parseREADMEAPITable(t)

	for r := range actual {
		if !readme[r] {
			t.Errorf("route %q is registered but MISSING from the README API table (Rule 14: document it)", r)
		}
	}
	for r := range readme {
		if !actual[r] {
			t.Errorf("README API table lists %q but no such route is registered (stale doc — remove or fix it)", r)
		}
	}
}

// parseREADMEAPITable extracts METHOD+path entries from the README "API surface"
// markdown table. Paths keep the chi pattern form ({id}) and the /api prefix, so
// they compare directly to chi.Walk output; the WS row maps to GET /ws and query
// hints (?channel=…) are stripped.
func parseREADMEAPITable(t *testing.T) map[string]bool {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	methodRe := regexp.MustCompile("^`(GET|POST|PUT|PATCH|DELETE|WS)`$")
	pathRe := regexp.MustCompile("^`([^`]+)`$")
	out := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		m := methodRe.FindStringSubmatch(strings.TrimSpace(cells[1]))
		p := pathRe.FindStringSubmatch(strings.TrimSpace(cells[2]))
		if m == nil || p == nil {
			continue
		}
		method := m[1]
		if method == "WS" { // /ws is registered with r.Get
			method = "GET"
		}
		path := p[1]
		if i := strings.IndexByte(path, '?'); i >= 0 { // drop ?channel=<id>&q=… hints
			path = path[:i]
		}
		out[method+" "+strings.TrimSuffix(path, "/")] = true
	}
	if len(out) == 0 {
		t.Fatal("parsed 0 routes from the README API table — table format changed?")
	}
	return out
}
