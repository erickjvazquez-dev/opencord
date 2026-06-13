// Package httpapi wires the HTTP surface: health, the REST auth API, and the
// WebSocket upgrade endpoint.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
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
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
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
			r.Post("/channels", chat.HandleCreateChannel(store))
			r.Get("/messages", chat.HandleRecent(store))
			// Delete one's own message (soft delete) → broadcast the removal to the channel.
			r.Delete("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
				u, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid message id"}`, http.StatusBadRequest)
					return
				}
				m, err := store.DeleteMessage(r.Context(), id, u.ID)
				if errors.Is(err, chat.ErrMessageNotFound) {
					http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
					return
				}
				if err != nil {
					http.Error(w, `{"error":"could not delete message"}`, http.StatusInternalServerError)
					return
				}
				hub.BroadcastEvent(ws.Event{Type: "message-deleted", Message: &m})
				w.WriteHeader(http.StatusNoContent)
			})
			// Edit one's own message → broadcast the updated message to the channel.
			r.Patch("/messages/{id}", func(w http.ResponseWriter, r *http.Request) {
				u, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid message id"}`, http.StatusBadRequest)
					return
				}
				var in struct {
					Body string `json:"body"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				body := strings.TrimSpace(in.Body)
				if body == "" {
					http.Error(w, `{"error":"message body required"}`, http.StatusBadRequest)
					return
				}
				m, err := store.EditMessage(r.Context(), id, u.ID, body)
				if errors.Is(err, chat.ErrMessageNotFound) {
					http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
					return
				}
				if err != nil {
					http.Error(w, `{"error":"could not edit message"}`, http.StatusInternalServerError)
					return
				}
				m.Username = u.Username
				hub.BroadcastEvent(ws.Event{Type: "message-edited", Message: &m})
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(m)
			})
			// React to a message (add emoji) → broadcast updated counts to the channel.
			r.Put("/messages/{id}/reactions", func(w http.ResponseWriter, r *http.Request) {
				u, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid message id"}`, http.StatusBadRequest)
					return
				}
				var in struct {
					Emoji string `json:"emoji"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				chID, err := store.AddReaction(r.Context(), id, u.ID, in.Emoji)
				if errors.Is(err, chat.ErrInvalidEmoji) {
					http.Error(w, `{"error":"invalid emoji"}`, http.StatusBadRequest)
					return
				}
				if errors.Is(err, chat.ErrMessageNotFound) {
					http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
					return
				}
				if err != nil {
					http.Error(w, `{"error":"could not add reaction"}`, http.StatusInternalServerError)
					return
				}
				broadcastReactions(w, r, store, hub, id, chID)
			})
			// Remove your reaction (emoji) from a message.
			r.Delete("/messages/{id}/reactions/{emoji}", func(w http.ResponseWriter, r *http.Request) {
				u, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid message id"}`, http.StatusBadRequest)
					return
				}
				chID, err := store.RemoveReaction(r.Context(), id, u.ID, chi.URLParam(r, "emoji"))
				if errors.Is(err, chat.ErrMessageNotFound) {
					http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
					return
				}
				if err != nil {
					http.Error(w, `{"error":"could not remove reaction"}`, http.StatusInternalServerError)
					return
				}
				broadcastReactions(w, r, store, hub, id, chID)
			})
		})
	})

	r.Get("/ws", ws.ServeWS(hub, authsvc, store))

	return r
}

// broadcastReactions recomputes a message's reaction summary (count-only),
// broadcasts it to the channel as a "reaction" event, and returns it to the caller.
func broadcastReactions(w http.ResponseWriter, r *http.Request, store *chat.Store, hub *ws.Hub, msgID, channelID int64) {
	sums, err := store.ReactionsForMessage(r.Context(), msgID, 0)
	if err != nil {
		http.Error(w, `{"error":"could not load reactions"}`, http.StatusInternalServerError)
		return
	}
	hub.BroadcastEvent(ws.Event{Type: "reaction", Message: &chat.Message{ID: msgID, ChannelID: channelID, Reactions: sums}})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"messageId": msgID, "reactions": sums})
}
