// Package webui embeds the built web SPA so the single Go binary can serve the
// frontend in production — no separate nginx container needed (which makes the
// cloud/one-binary deploy trivial). In dev the Vite server serves the SPA and
// proxies the API, so this handler is only exercised by the compiled binary.
//
// The real dist/ is produced by `npm --prefix web run build` and copied here at
// Docker build time; a tiny placeholder index.html is committed so `go build`
// always succeeds (CI doesn't build the web).
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded SPA: a real embedded file when the path maps to one
// (e.g. /assets/index-*.js), and index.html as the fallback for client-side routes
// (the History-API paths the React app owns).
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time; this cannot fail at runtime
	}
	fileServer := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(sub, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
			if f, err := sub.Open(p); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: index.html for "/" and any unknown client-side route.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
