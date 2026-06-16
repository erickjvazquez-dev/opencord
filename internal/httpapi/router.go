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
	"github.com/erickjvazquez-dev/opencord/internal/voice"
	"github.com/erickjvazquez-dev/opencord/internal/webui"
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
			// Set the CALLER's own custom status (Rule C — derived from the JWT, no
			// target id). Empty/whitespace clears it; the store trims + caps length.
			r.Put("/me/status", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				var in struct {
					Status string `json:"status"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				if err := store.SetUserStatus(r.Context(), me.ID, in.Status); err != nil {
					http.Error(w, `{"error":"could not set status"}`, http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			r.Get("/channels", chat.HandleChannels(store))
			r.Post("/channels", chat.HandleCreateChannel(store))
			// Unread indicators: the caller's accessible channels with unread messages,
			// each with a count of unread messages that @-mention them (red badge).
			r.Get("/unreads", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				chans, err := store.Unreads(r.Context(), me.ID, me.Username)
				if err != nil {
					http.Error(w, `{"error":"could not load unreads"}`, http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"channels": chans})
			})
			// Mark a channel read up to its latest message (access-gated, Rule C).
			r.Post("/channels/{id}/read", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}
				if ok, err := store.CanAccessChannel(r.Context(), id, me.ID); err != nil || !ok {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				if err := store.MarkChannelRead(r.Context(), id, me.ID); err != nil {
					http.Error(w, `{"error":"could not mark read"}`, http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			// Update a server channel — posting policy ('everyone'|'admins') and/or
			// topic. Admins only. Fields are optional (pointers): each is applied only
			// when present, so old {postPolicy} clients keep working.
			r.Patch("/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}
				var in struct {
					PostPolicy      *string `json:"postPolicy"`
					Topic           *string `json:"topic"`
					SlowmodeSeconds *int    `json:"slowmodeSeconds"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				if in.PostPolicy == nil && in.Topic == nil && in.SlowmodeSeconds == nil {
					http.Error(w, `{"error":"no fields to update"}`, http.StatusBadRequest)
					return
				}
				if in.PostPolicy != nil {
					switch err := store.SetChannelPostPolicy(r.Context(), id, me.ID, *in.PostPolicy); {
					case errors.Is(err, chat.ErrInvalidPolicy):
						http.Error(w, `{"error":"policy must be everyone or admins"}`, http.StatusBadRequest)
						return
					case errors.Is(err, chat.ErrForbidden):
						http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
						return
					case err != nil:
						http.Error(w, `{"error":"could not set policy"}`, http.StatusInternalServerError)
						return
					}
				}
				if in.Topic != nil {
					switch err := store.SetChannelTopic(r.Context(), id, me.ID, *in.Topic); {
					case errors.Is(err, chat.ErrTopicTooLong):
						http.Error(w, `{"error":"topic too long (max 1024 chars)"}`, http.StatusBadRequest)
						return
					case errors.Is(err, chat.ErrForbidden):
						http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
						return
					case err != nil:
						http.Error(w, `{"error":"could not set topic"}`, http.StatusInternalServerError)
						return
					}
				}
				if in.SlowmodeSeconds != nil {
					switch err := store.SetChannelSlowmode(r.Context(), id, me.ID, *in.SlowmodeSeconds); {
					case errors.Is(err, chat.ErrInvalidSlowmode):
						http.Error(w, `{"error":"slowmode must be 0..21600 seconds"}`, http.StatusBadRequest)
						return
					case errors.Is(err, chat.ErrForbidden):
						http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
						return
					case err != nil:
						http.Error(w, `{"error":"could not set slowmode"}`, http.StatusInternalServerError)
						return
					}
				}
				w.WriteHeader(http.StatusNoContent)
			})
			r.Get("/dms", chat.HandleListDMs(store))
			r.Post("/dms", chat.HandleCreateDM(store))
			mountServerRoutes(r, store, hub)
			r.Get("/messages", chat.HandleRecent(store))
			// Create a message carrying file/image attachments (multipart). Plain
			// text messages keep flowing over the WS; files don't fit a 4 KiB frame.
			r.Post("/messages", handleUploadMessage(cfg.UploadDir, store, hub))
			// Access-gated serve of a message attachment's bytes.
			r.Get("/attachments/{id}", handleServeAttachment(cfg.UploadDir, store))
			// Uploaded avatars: set your own (JWT-derived); serve any user's (404 → initials).
			r.Post("/avatar", handleUploadAvatar(cfg.UploadDir, store))
			r.Get("/users/{id}/avatar", handleServeAvatar(cfg.UploadDir, store))
			r.Get("/messages/search", chat.HandleSearch(store))
			r.Get("/messages/pins", chat.HandlePins(store))
			// Mint a join token for the optional LiveKit SFU (large voice calls).
			// Unconfigured → {sfu:false} so the client uses mesh (Rule A). Otherwise
			// the token is minted server-side from the verified user (Rule C), scoped
			// to room = the channel, and ONLY for members (Rule B) — a non-member
			// gets 403, never a token.
			r.Post("/voice/token", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				// ICE servers (STUN + optional TURN) for the mesh path; returned to
				// authed callers only so TURN creds don't leak (Rule C).
				ice := cfg.ICEServers()
				if cfg.SFUURL == "" {
					writeJSON(w, http.StatusOK, map[string]any{"sfu": false, "iceServers": ice})
					return
				}
				channelID, err := strconv.ParseInt(r.URL.Query().Get("channel"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}
				ok, err := store.CanAccessChannel(r.Context(), channelID, me.ID)
				if err != nil {
					http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
					return
				}
				if !ok {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				room := "opencord-ch-" + strconv.FormatInt(channelID, 10)
				tok, err := voice.MintToken(cfg.SFUSecret, cfg.SFUKey, room,
					"u"+strconv.FormatInt(me.ID, 10), me.Username, time.Hour, time.Now())
				if err != nil {
					http.Error(w, `{"error":"could not mint token"}`, http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"sfu": true, "url": cfg.SFUURL, "room": room, "token": tok, "iceServers": ice,
				})
			})
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
				if errors.Is(err, chat.ErrForbidden) {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
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
				if errors.Is(err, chat.ErrForbidden) {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				if err != nil {
					http.Error(w, `{"error":"could not remove reaction"}`, http.StatusInternalServerError)
					return
				}
				broadcastReactions(w, r, store, hub, id, chID)
			})

			// Pin / unpin a message → broadcast the change to the channel. Server
			// channels: admins only; serverless channels: any member (see store).
			setPinned := func(pinned bool) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					u, _ := auth.UserFrom(r.Context())
					id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
					if err != nil {
						http.Error(w, `{"error":"invalid message id"}`, http.StatusBadRequest)
						return
					}
					m, err := store.SetMessagePinned(r.Context(), id, u.ID, pinned)
					switch {
					case errors.Is(err, chat.ErrMessageNotFound):
						http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
					case errors.Is(err, chat.ErrForbidden):
						http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					case err != nil:
						http.Error(w, `{"error":"could not change pin"}`, http.StatusInternalServerError)
					default:
						hub.BroadcastEvent(ws.Event{Type: "message-pinned", Message: &m})
						w.WriteHeader(http.StatusNoContent)
					}
				}
			}
			r.Put("/messages/{id}/pin", setPinned(true))
			r.Delete("/messages/{id}/pin", setPinned(false))
		})
	})

	r.Get("/ws", ws.ServeWS(hub, authsvc, store))

	// Everything not matched above is the SPA: real assets when they exist, else
	// index.html (client-side routing). /api, /ws and /healthz are matched first,
	// so this only catches frontend paths.
	r.Handle("/*", webui.Handler())

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

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// serverIDParam parses the {id} path segment as a server id.
func serverIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// mountServerRoutes wires the servers/guilds REST surface (auth applied by the
// caller's group). Server channels are members-only — non-members get 403. Routes
// are registered flat to avoid chi's trailing-slash matching on a sub-router.
func mountServerRoutes(r chi.Router, store *chat.Store, hub *ws.Hub) {
	r.Get("/servers", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		servers, err := store.ListServers(r.Context(), me.ID)
		if err != nil {
			http.Error(w, `{"error":"could not load servers"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, servers)
	})
	r.Post("/servers", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" || len(name) > 64 {
			http.Error(w, `{"error":"server name must be 1-64 chars"}`, http.StatusBadRequest)
			return
		}
		srv, err := store.CreateServer(r.Context(), me.ID, name)
		if err != nil {
			http.Error(w, `{"error":"could not create server"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, srv)
	})
	// Mint an invite code for a server (members only). Joining is invite-only — the
	// old open POST /servers/{id}/join (anyone could join by guessing the id) is gone.
	r.Post("/servers/{id}/invites", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerMember(r.Context(), id, me.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		code, err := store.CreateInvite(r.Context(), id, me.ID)
		if err != nil {
			http.Error(w, `{"error":"could not create invite"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"code": code})
	})
	r.Get("/servers/{id}/channels", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerMember(r.Context(), id, me.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		chans, err := store.ListServerChannels(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"could not load channels"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, chans)
	})
	r.Post("/servers/{id}/channels", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		// Creating a channel requires admin+ (Discord default: members can't).
		if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if !chat.ValidChannelName(in.Name) {
			http.Error(w, `{"error":"channel name must be 2-32 chars of [a-z0-9_-]"}`, http.StatusBadRequest)
			return
		}
		c, err := store.CreateServerChannel(r.Context(), id, in.Name)
		if errors.Is(err, chat.ErrChannelExists) {
			http.Error(w, `{"error":"channel name already taken in this server"}`, http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not create channel"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	})
	// List a server's members with their roles (members only).
	r.Get("/servers/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerMember(r.Context(), id, me.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		members, err := store.ListServerMembers(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"could not load members"}`, http.StatusInternalServerError)
			return
		}
		// Annotate presence from the hub's live-connection set (the store doesn't
		// know about sockets). A member is online if they hold ≥1 WS connection.
		online := hub.OnlineUserIDs()
		for i := range members {
			members[i].Online = online[members[i].UserID]
		}
		writeJSON(w, http.StatusOK, members)
	})
	// Promote/demote a member (owner only): {userId, role:'admin'|'member'}.
	r.Post("/servers/{id}/roles", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			UserID int64  `json:"userId"`
			Role   string `json:"role"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		switch err := store.SetServerRole(r.Context(), id, me.ID, in.UserID, in.Role); {
		case errors.Is(err, chat.ErrInvalidRole):
			http.Error(w, `{"error":"role must be admin or member"}`, http.StatusBadRequest)
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not set role"}`, http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	// Kick a member out of a server: {userId} via the path. Owner/admin only; can't
	// kick the owner or yourself; an admin can't kick a fellow admin (store enforces).
	r.Delete("/servers/{id}/members/{userId}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		targetID, err := strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
			return
		}
		switch err := store.RemoveServerMember(r.Context(), id, me.ID, targetID); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not remove member"}`, http.StatusInternalServerError)
		default:
			// Tell the kicked user's client (any socket) it's been removed so it can
			// drop the server cleanly, THEN evict its live sockets on this server's
			// channels so it stops receiving (WS access is otherwise only checked at
			// connect). Order matters — the notice is queued before the close so it's
			// delivered (writePump flushes pending frames before closing). The notice
			// is best-effort UX; eviction is the security guarantee.
			hub.SendToUser(targetID, ws.Event{Type: "server-removed", ServerID: id})
			if chans, err := store.ListServerChannels(r.Context(), id); err == nil {
				ids := make([]int64, 0, len(chans))
				for _, c := range chans {
					ids = append(ids, c.ID)
				}
				hub.EvictUserFromChannels(targetID, ids)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	// Redeem an invite code → join its server (the only way to join). 404 on a bad code.
	r.Post("/invites/{code}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		srv, err := store.RedeemInvite(r.Context(), chi.URLParam(r, "code"), me.ID)
		if errors.Is(err, chat.ErrInvalidInvite) {
			http.Error(w, `{"error":"invalid invite code"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not redeem invite"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	})
}
