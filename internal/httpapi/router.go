// Package httpapi wires the HTTP surface: health, the REST auth API, and the
// WebSocket upgrade endpoint.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
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
					Status      string `json:"status"`
					StatusEmoji string `json:"statusEmoji"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				if err := store.SetUserStatus(r.Context(), me.ID, in.Status, in.StatusEmoji); err != nil {
					http.Error(w, `{"error":"could not set status"}`, http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			// Set the CALLER's own profile — About Me + pronouns (trimmed + capped in the
			// store; empty clears). Rule B/C — bounded body, JWT-derived id, no target user.
			r.Put("/me/profile", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				var in struct {
					About    string `json:"about"`
					Pronouns string `json:"pronouns"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				if err := store.SetUserProfile(r.Context(), me.ID, in.About, in.Pronouns); err != nil {
					http.Error(w, `{"error":"could not set profile"}`, http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			// Set the CALLER's own presence state (online|idle|dnd|invisible). An
			// unknown value normalizes to "online" (Rule B). Rule C — JWT-derived id.
			r.Put("/me/presence", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				var in struct {
					Presence string `json:"presence"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				if err := store.SetUserPresence(r.Context(), me.ID, in.Presence); err != nil {
					http.Error(w, `{"error":"could not set presence"}`, http.StatusInternalServerError)
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
			// Mute / unmute a channel for myself — a muted channel stops surfacing as
			// unread (sidebar dot, mention badge, tab badge). Access-gated (Rule B/C: you
			// can only mute a channel you can read; identity from the JWT, never the body).
			r.Post("/channels/{id}/mute", func(w http.ResponseWriter, r *http.Request) {
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
				if err := store.MuteChannel(r.Context(), id, me.ID); err != nil {
					http.Error(w, `{"error":"could not mute"}`, http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			r.Delete("/channels/{id}/mute", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}
				if err := store.UnmuteChannel(r.Context(), id, me.ID); err != nil {
					http.Error(w, `{"error":"could not unmute"}`, http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			// The channels I've muted (so the client renders the muted dim + toggle state).
			r.Get("/muted-channels", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				ids, err := store.MutedChannelIDs(r.Context(), me.ID)
				if err != nil {
					http.Error(w, `{"error":"could not load muted channels"}`, http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"channels": ids})
			})
			// A user's PUBLIC profile (About Me, pronouns, status, presence) — for the profile
			// card. Any authed user can view any user's public profile (Discord-style); the store
			// selects only public fields (Rule 15). 404 for a missing user.
			r.Get("/users/{id}/profile", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
					return
				}
				p, err := store.GetUserProfile(r.Context(), id)
				if errors.Is(err, chat.ErrUserNotFound) {
					http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
					return
				}
				if err != nil {
					http.Error(w, `{"error":"server error"}`, http.StatusInternalServerError)
					return
				}
				// Effective presence: the viewer sees their OWN true state; others see an
				// invisible/disconnected user as offline (mirrors the member list).
				connected := hub.OnlineUserIDs()[p.UserID]
				if p.UserID == me.ID {
					p.Online, p.Presence = connected, chat.NormalizePresence(p.PresenceState)
				} else {
					p.Online, p.Presence = chat.EffectivePresence(connected, p.PresenceState)
				}
				writeJSON(w, http.StatusOK, p)
			})
			// Block a user (v0.5, slice 1): symmetric DM enforcement. Identity is the
			// JWT caller (Rule B/C — never the body); the target is the path id. 400 if
			// you try to block yourself, 404 for an unknown user, 204 on success
			// (idempotent — re-blocking is also 204).
			r.Post("/users/{id}/block", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
					return
				}
				switch err := store.BlockUser(r.Context(), me.ID, id); {
				case errors.Is(err, chat.ErrForbidden):
					http.Error(w, `{"error":"cannot block yourself"}`, http.StatusBadRequest)
				case errors.Is(err, chat.ErrUserNotFound):
					http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				case err != nil:
					http.Error(w, `{"error":"could not block user"}`, http.StatusInternalServerError)
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			})
			// Unblock a user. 404 if they weren't blocked, 204 on success.
			r.Delete("/users/{id}/block", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid user id"}`, http.StatusBadRequest)
					return
				}
				switch err := store.UnblockUser(r.Context(), me.ID, id); {
				case errors.Is(err, chat.ErrUserNotFound):
					http.Error(w, `{"error":"user is not blocked"}`, http.StatusNotFound)
				case err != nil:
					http.Error(w, `{"error":"could not unblock user"}`, http.StatusInternalServerError)
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			})
			// The users I've blocked (so the client can render + manage the block list).
			r.Get("/me/blocks", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				blocked, err := store.ListBlocked(r.Context(), me.ID)
				if err != nil {
					http.Error(w, `{"error":"could not load blocks"}`, http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, blocked)
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
			// Threads (v0.8): a thread is a sub-conversation off a parent channel. Both routes
			// gate on access to the PARENT (which transitively gates the thread, since a thread
			// inherits the parent's server_id). List threads / start a thread {name}.
			r.Get("/channels/{id}/threads", func(w http.ResponseWriter, r *http.Request) {
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
				threads, err := store.ListThreads(r.Context(), id)
				if err != nil {
					http.Error(w, `{"error":"could not load threads"}`, http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusOK, threads)
			})
			r.Post("/channels/{id}/threads", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
				if err != nil {
					http.Error(w, `{"error":"invalid channel id"}`, http.StatusBadRequest)
					return
				}
				// Must be able to access AND post in the parent to start a thread there.
				if ok, err := store.CanAccessChannel(r.Context(), id, me.ID); err != nil || !ok {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				if ok, err := store.CanPostInChannel(r.Context(), id, me.ID); err != nil || !ok {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				var in struct {
					Name string `json:"name"`
				}
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
					http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
					return
				}
				thread, err := store.CreateThread(r.Context(), id, in.Name)
				switch {
				case errors.Is(err, chat.ErrInvalidThreadName):
					http.Error(w, `{"error":"thread name must be 1-100 characters"}`, http.StatusBadRequest)
					return
				case errors.Is(err, chat.ErrNotThreadable):
					http.Error(w, `{"error":"cannot start a thread on this channel"}`, http.StatusBadRequest)
					return
				case errors.Is(err, chat.ErrChannelNotFound):
					http.Error(w, `{"error":"channel not found"}`, http.StatusNotFound)
					return
				case err != nil:
					http.Error(w, `{"error":"could not create thread"}`, http.StatusInternalServerError)
					return
				}
				writeJSON(w, http.StatusCreated, thread)
			})
			r.Get("/dms", chat.HandleListDMs(store))
			r.Post("/dms", chat.HandleCreateDM(store))
			r.Post("/dms/group", chat.HandleCreateGroupDM(store))
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
			// Custom server emoji. Upload/delete are admin-gated; list is member-gated;
			// serving the bytes is open to any authed user (emoji are public in-instance,
			// like avatars). The handlers take only (uploadDir, store), so the member/admin
			// access checks live here in the route closures (like the channel routes).
			r.Post("/servers/{id}/emoji", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := serverIDParam(r)
				if err != nil {
					http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
					return
				}
				if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil || !ok {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				handleUploadServerEmoji(cfg.UploadDir, store)(w, r)
			})
			r.Get("/servers/{id}/emoji", func(w http.ResponseWriter, r *http.Request) {
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
				handleListServerEmoji(store)(w, r)
			})
			r.Delete("/servers/{id}/emoji/{emojiId}", func(w http.ResponseWriter, r *http.Request) {
				me, _ := auth.UserFrom(r.Context())
				id, err := serverIDParam(r)
				if err != nil {
					http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
					return
				}
				if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil || !ok {
					http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
					return
				}
				handleDeleteServerEmoji(cfg.UploadDir, store)(w, r)
			})
			// Serve an emoji's bytes (any authed user; emoji are public in-instance).
			r.Get("/emoji/{id}", handleServeEmoji(cfg.UploadDir, store))
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
				ice := cfg.ICEServersForUser(me.ID, time.Now())
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
	// Rename a server (owner/admin — Discord's "Manage Server"). Live-relabels every
	// member's sidebar via a "server-renamed" push.
	r.Patch("/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
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
		srv, err := store.RenameServer(r.Context(), id, me.ID, name)
		switch {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrServerNotFound):
			http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not rename server"}`, http.StatusInternalServerError)
		default:
			// Tell every member so their sidebar relabels live (best-effort UX).
			if members, err := store.ListServerMembers(r.Context(), id); err == nil {
				for _, m := range members {
					hub.SendToUser(m.UserID, ws.Event{Type: "server-renamed", ServerID: id, Name: srv.Name})
				}
			}
			writeJSON(w, http.StatusOK, srv)
		}
	})
	// Delete a server (owner only — destructive). Cascades away its channels, members,
	// messages, invites, categories, and bans; the global #general is untouched. Every
	// member is pushed "server-removed" + evicted from the (now-deleted) channels so
	// their client drops the server live, identical to a kick/ban.
	r.Delete("/servers/{id}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		// Gather members + channel ids BEFORE the delete — they're gone afterwards.
		members, _ := store.ListServerMembers(r.Context(), id)
		chans, _ := store.ListServerChannels(r.Context(), id)
		switch err := store.DeleteServer(r.Context(), id, me.ID); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrServerNotFound):
			http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not delete server"}`, http.StatusInternalServerError)
		default:
			ids := make([]int64, 0, len(chans))
			for _, c := range chans {
				ids = append(ids, c.ID)
			}
			for _, m := range members {
				if m.UserID == me.ID {
					continue // the actor deleted it — their own HTTP response cleans up (no "you were removed" notice)
				}
				hub.SendToUser(m.UserID, ws.Event{Type: "server-removed", ServerID: id})
				hub.EvictUserFromChannels(m.UserID, ids)
			}
			w.WriteHeader(http.StatusNoContent)
		}
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
		// Body is optional: legacy clients send none (→ unlimited). An empty body decodes
		// to io.EOF (fine); only genuinely malformed JSON is a 400.
		var in struct {
			MaxUses *int `json:"maxUses"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if in.MaxUses != nil && (*in.MaxUses < 1 || *in.MaxUses > 1000) {
			http.Error(w, `{"error":"maxUses must be between 1 and 1000"}`, http.StatusBadRequest)
			return
		}
		code, err := store.CreateInviteWithMaxUses(r.Context(), id, me.ID, in.MaxUses)
		if err != nil {
			http.Error(w, `{"error":"could not create invite"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"code": code})
	})
	// List a server's active invite codes (admin-gated, like bans — a non-admin gets 403,
	// not the list). This is the "what can someone join with right now" management view.
	r.Get("/servers/{id}/invites", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil {
			http.Error(w, `{"error":"could not list invites"}`, http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		invites, err := store.ListInvites(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"could not list invites"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, invites)
	})
	// Revoke an invite code so it can no longer be redeemed (admin-gated). The code is in
	// the path; the store scopes the delete by server_id (Rule B — no cross-server revoke).
	r.Delete("/servers/{id}/invites/{code}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		switch err := store.RevokeInvite(r.Context(), id, chi.URLParam(r, "code")); {
		case errors.Is(err, chat.ErrInvalidInvite):
			http.Error(w, `{"error":"invite not found"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not revoke invite"}`, http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
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
			Name       string `json:"name"`
			CategoryID *int64 `json:"categoryId"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if !chat.ValidChannelName(in.Name) {
			http.Error(w, `{"error":"channel name must be 2-32 chars of [a-z0-9_-]"}`, http.StatusBadRequest)
			return
		}
		c, err := store.CreateServerChannelInCategory(r.Context(), id, in.Name, in.CategoryID)
		switch {
		case errors.Is(err, chat.ErrChannelExists):
			http.Error(w, `{"error":"channel name already taken in this server"}`, http.StatusConflict)
			return
		case errors.Is(err, chat.ErrCategoryNotFound):
			http.Error(w, `{"error":"category not found in this server"}`, http.StatusBadRequest)
			return
		case err != nil:
			http.Error(w, `{"error":"could not create channel"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	})
	// List a server's channel categories (members). Categories are display-only groupings.
	r.Get("/servers/{id}/categories", func(w http.ResponseWriter, r *http.Request) {
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
		cats, err := store.ListChannelCategories(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"could not load categories"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, cats)
	})
	// Create a channel category (admin-gated, like creating a channel). Name is a label
	// (spaces/caps allowed), trimmed and bounded.
	r.Post("/servers/{id}/categories", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
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
		name := strings.TrimSpace(in.Name)
		if name == "" || len([]rune(name)) > 32 {
			http.Error(w, `{"error":"category name must be 1-32 characters"}`, http.StatusBadRequest)
			return
		}
		c, err := store.CreateChannelCategory(r.Context(), id, name)
		if err != nil {
			http.Error(w, `{"error":"could not create category"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	})
	// Delete a category (admin-gated). Its channels survive (become uncategorized).
	r.Delete("/servers/{id}/categories/{catId}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		catID, err := strconv.ParseInt(chi.URLParam(r, "catId"), 10, 64)
		if err != nil {
			http.Error(w, `{"error":"invalid category id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		switch err := store.DeleteChannelCategory(r.Context(), id, catID); {
		case errors.Is(err, chat.ErrCategoryNotFound):
			http.Error(w, `{"error":"category not found"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not delete category"}`, http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
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
		// know about sockets) combined with each member's chosen presence state.
		// Others see a disconnected or "invisible" member as offline; the viewer
		// always sees their OWN true chosen state (so the picker reflects it).
		online := hub.OnlineUserIDs()
		for i := range members {
			connected := online[members[i].UserID]
			if members[i].UserID == me.ID {
				members[i].Online = connected
				members[i].Presence = chat.NormalizePresence(members[i].PresenceState)
				continue
			}
			members[i].Online, members[i].Presence = chat.EffectivePresence(connected, members[i].PresenceState)
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
	// Transfer server ownership to another member: {userId} in the body. Owner only;
	// the target must be a different existing member. The old owner becomes an admin.
	r.Post("/servers/{id}/transfer", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			UserID int64 `json:"userId"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		switch err := store.TransferServerOwnership(r.Context(), id, me.ID, in.UserID); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"only the owner can transfer ownership"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not transfer ownership"}`, http.StatusInternalServerError)
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
	// Leave a server (voluntary self-removal — the counterpart to join). Any non-owner
	// member may leave; the owner can't (they must delete/transfer first). The leaver's
	// own live sockets on the server's channels are evicted so they stop receiving.
	r.Post("/servers/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		// Gather the channel ids BEFORE leaving (still readable; access is lost after).
		chans, _ := store.ListServerChannels(r.Context(), id)
		switch err := store.LeaveServer(r.Context(), id, me.ID); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"the owner cannot leave — delete or transfer the server instead"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrServerNotFound):
			http.Error(w, `{"error":"you are not a member of this server"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not leave server"}`, http.StatusInternalServerError)
		default:
			ids := make([]int64, 0, len(chans))
			for _, c := range chans {
				ids = append(ids, c.ID)
			}
			hub.EvictUserFromChannels(me.ID, ids)
			w.WriteHeader(http.StatusNoContent)
		}
	})

	// --- Custom colored roles (v0.7): cosmetic, separate from the owner/admin/member
	// permission tier. Live under /custom-roles (POST /roles is the permission setter).
	// Mutations are admin-gated in the store; map ErrForbidden→403, bad input→400, ErrRoleNotFound
	// / ErrUserNotFound→404.
	mapRoleErr := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"only an admin can manage roles"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrInvalidColor):
			http.Error(w, `{"error":"color must be a #RGB or #RRGGBB hex"}`, http.StatusBadRequest)
		case errors.Is(err, chat.ErrInvalidRoleName):
			http.Error(w, `{"error":"role name must be 1-32 characters"}`, http.StatusBadRequest)
		case errors.Is(err, chat.ErrRoleNotFound):
			http.Error(w, `{"error":"role not found"}`, http.StatusNotFound)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		default:
			http.Error(w, `{"error":"could not update role"}`, http.StatusInternalServerError)
		}
	}
	roleIDParam := func(r *http.Request) (int64, error) {
		return strconv.ParseInt(chi.URLParam(r, "roleId"), 10, 64)
	}
	// List a server's custom roles (members only).
	r.Get("/servers/{id}/custom-roles", func(w http.ResponseWriter, r *http.Request) {
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
		roles, err := store.ListServerRoles(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"could not load roles"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, roles)
	})
	// Create a custom role: {name, color} (admin only).
	r.Post("/servers/{id}/custom-roles", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		role, err := store.CreateServerRole(r.Context(), id, me.ID, in.Name, in.Color)
		if err != nil {
			mapRoleErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, role)
	})
	// Edit a custom role: {name, color} (admin only).
	r.Patch("/servers/{id}/custom-roles/{roleId}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		roleID, err := roleIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid role id"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if err := store.UpdateServerRole(r.Context(), id, me.ID, roleID, in.Name, in.Color); err != nil {
			mapRoleErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Delete a custom role (admin only); assignments cascade away.
	r.Delete("/servers/{id}/custom-roles/{roleId}", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		roleID, err := roleIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid role id"}`, http.StatusBadRequest)
			return
		}
		if err := store.DeleteServerRole(r.Context(), id, me.ID, roleID); err != nil {
			mapRoleErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Assign a custom role to a member (admin only).
	r.Put("/servers/{id}/members/{userId}/custom-roles/{roleId}", func(w http.ResponseWriter, r *http.Request) {
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
		roleID, err := roleIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid role id"}`, http.StatusBadRequest)
			return
		}
		if err := store.AssignServerRole(r.Context(), id, me.ID, targetID, roleID); err != nil {
			mapRoleErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Unassign a custom role from a member (admin only).
	r.Delete("/servers/{id}/members/{userId}/custom-roles/{roleId}", func(w http.ResponseWriter, r *http.Request) {
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
		roleID, err := roleIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid role id"}`, http.StatusBadRequest)
			return
		}
		if err := store.UnassignServerRole(r.Context(), id, me.ID, targetID, roleID); err != nil {
			mapRoleErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Ban a member: {userId, reason?} in the body. Owner/admin only; same authz as kick
	// (can't ban the owner/yourself; an admin can't ban a fellow admin — store enforces).
	// Removes them AND blocks rejoining until unbanned; evicts their live sockets.
	r.Post("/servers/{id}/bans", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			UserID int64  `json:"userId"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		switch err := store.BanServerMember(r.Context(), id, me.ID, in.UserID, in.Reason); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not ban member"}`, http.StatusInternalServerError)
		default:
			// Identical to kick: notify then evict so the banned client drops the server live.
			hub.SendToUser(in.UserID, ws.Event{Type: "server-removed", ServerID: id})
			if chans, err := store.ListServerChannels(r.Context(), id); err == nil {
				ids := make([]int64, 0, len(chans))
				for _, c := range chans {
					ids = append(ids, c.ID)
				}
				hub.EvictUserFromChannels(in.UserID, ids)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	// Unban a member: {userId} via the path. Owner/admin only.
	r.Delete("/servers/{id}/bans/{userId}", func(w http.ResponseWriter, r *http.Request) {
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
		switch err := store.UnbanServerMember(r.Context(), id, me.ID, targetID); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not banned"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not unban member"}`, http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	// List a server's bans (admin-gated). A non-admin gets 403, not the list.
	r.Get("/servers/{id}/bans", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		if ok, err := store.IsServerAdmin(r.Context(), id, me.ID); err != nil {
			http.Error(w, `{"error":"could not list bans"}`, http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		bans, err := store.ListServerBans(r.Context(), id)
		if err != nil {
			http.Error(w, `{"error":"could not list bans"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, bans)
	})
	// Timeout (temporary mute) a member: {userId, durationSeconds} in the body. Owner/admin
	// only; same authz as kick/ban. The duration is clamped server-side. Returns {until}.
	r.Post("/servers/{id}/timeouts", func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		id, err := serverIDParam(r)
		if err != nil {
			http.Error(w, `{"error":"invalid server id"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			UserID          int64 `json:"userId"`
			DurationSeconds int64 `json:"durationSeconds"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if in.DurationSeconds <= 0 {
			http.Error(w, `{"error":"durationSeconds must be positive"}`, http.StatusBadRequest)
			return
		}
		until := time.Now().Add(time.Duration(in.DurationSeconds) * time.Second)
		switch eff, err := store.TimeoutServerMember(r.Context(), id, me.ID, in.UserID, until); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not time out member"}`, http.StatusInternalServerError)
		default:
			writeJSON(w, http.StatusOK, map[string]any{"until": eff})
		}
	})
	// Clear a member's timeout (unmute early): {userId} via the path. Owner/admin only.
	r.Delete("/servers/{id}/timeouts/{userId}", func(w http.ResponseWriter, r *http.Request) {
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
		switch err := store.ClearTimeout(r.Context(), id, me.ID, targetID); {
		case errors.Is(err, chat.ErrForbidden):
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case errors.Is(err, chat.ErrUserNotFound):
			http.Error(w, `{"error":"user is not a member"}`, http.StatusNotFound)
		case err != nil:
			http.Error(w, `{"error":"could not clear timeout"}`, http.StatusInternalServerError)
		default:
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
		if errors.Is(err, chat.ErrInviteExpired) {
			http.Error(w, `{"error":"this invite has expired"}`, http.StatusNotFound)
			return
		}
		if errors.Is(err, chat.ErrInviteExhausted) {
			http.Error(w, `{"error":"this invite has reached its maximum uses"}`, http.StatusNotFound)
			return
		}
		if errors.Is(err, chat.ErrBanned) {
			http.Error(w, `{"error":"you are banned from this server"}`, http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not redeem invite"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, srv)
	})
}
