// Package httpapi wires the HTTP surface: health, the REST auth API, and the
// WebSocket upgrade endpoint.
package httpapi

import (
	"net/http"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/config"
	"github.com/erickjvazquez-dev/opencord/internal/ws"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Version is reported by /healthz and bumped per release.
const Version = "0.1.0"

func New(cfg config.Config, authsvc *auth.Service, store *chat.Store, hub *ws.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.CORSOrigin},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + Version + `"}`))
	})

	// REST API. A request timeout guards these handlers but is deliberately
	// NOT applied to the long-lived /ws connection below.
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Timeout(15 * time.Second))
		r.Post("/auth/register", authsvc.HandleRegister)
		r.Post("/auth/login", authsvc.HandleLogin)
		r.Group(func(r chi.Router) {
			r.Use(authsvc.Middleware)
			r.Get("/auth/me", authsvc.HandleMe)
			r.Get("/channels", chat.HandleChannels(store))
			r.Get("/messages", chat.HandleRecent(store))
		})
	})

	r.Get("/ws", ws.ServeWS(hub, authsvc, store))

	return r
}
