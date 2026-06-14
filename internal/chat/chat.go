// Package chat persists and retrieves channels and messages. Messages are
// channel-scoped at the data layer (v0.2); the WS gateway still serves a single
// default room until per-channel routing lands.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrChannelExists is returned when a channel name is already taken.
	ErrChannelExists = errors.New("channel name already taken")
	// ErrUserNotFound is returned when a DM target username does not exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrCannotDMSelf is returned when a user tries to open a DM with themselves.
	ErrCannotDMSelf = errors.New("cannot DM yourself")
	// channelNameRe mirrors a Discord-style channel slug: lowercase, 2-32 chars.
	channelNameRe = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
)

// DMUser is the other participant in a direct message channel.
type DMUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// DMChannel is a direct-message channel as seen by one participant: the channel
// plus the *other* user in it.
type DMChannel struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	User      DMUser    `json:"user"`
}

type Message struct {
	ID        int64     `json:"id"`
	ChannelID int64     `json:"channelId"`
	UserID    int64     `json:"userId"`
	Username  string    `json:"username"`
	Body      string     `json:"body"`
	CreatedAt time.Time         `json:"createdAt"`
	EditedAt  *time.Time        `json:"editedAt,omitempty"`
	Deleted   bool              `json:"deleted,omitempty"`
	Reactions []ReactionSummary `json:"reactions,omitempty"`
}

// ReactionSummary aggregates one emoji on a message. Mine is true when the
// viewing user reacted with it.
type ReactionSummary struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine,omitempty"`
}

// Channel is a named room. v0.2 introduces the table behind the MVP's single
// hardcoded global channel.
type Channel struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Save inserts a message into the given channel and returns it fully populated.
func (s *Store) Save(ctx context.Context, channelID, userID int64, username, body string) (Message, error) {
	m := Message{ChannelID: channelID, UserID: userID, Username: username, Body: body}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (channel_id, user_id, body) VALUES ($1, $2, $3)
		   RETURNING id, created_at`,
		channelID, userID, body,
	).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

// ErrMessageNotFound is returned when a message doesn't exist, isn't owned by the
// caller, or was already deleted.
var ErrMessageNotFound = errors.New("message not found")

// DeleteMessage soft-deletes the caller's own message and returns a stub (id +
// channel + deleted flag) suitable for broadcasting the removal to the channel.
func (s *Store) DeleteMessage(ctx context.Context, id, userID int64) (Message, error) {
	m := Message{ID: id, Deleted: true, Body: "[deleted]"}
	err := s.pool.QueryRow(ctx,
		`UPDATE messages SET deleted_at = now()
		   WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		   RETURNING channel_id`,
		id, userID,
	).Scan(&m.ChannelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	return m, err
}

// EditMessage updates the body of the caller's own (non-deleted) message, stamps
// edited_at, and returns the updated message (username is filled by the caller).
func (s *Store) EditMessage(ctx context.Context, id, userID int64, body string) (Message, error) {
	m := Message{ID: id, UserID: userID, Body: body}
	err := s.pool.QueryRow(ctx,
		`UPDATE messages SET body = $1, edited_at = now()
		   WHERE id = $2 AND user_id = $3 AND deleted_at IS NULL
		   RETURNING channel_id, created_at, edited_at`,
		body, id, userID,
	).Scan(&m.ChannelID, &m.CreatedAt, &m.EditedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	return m, err
}

// ErrInvalidEmoji is returned when a reaction emoji is empty or too long.
var ErrInvalidEmoji = errors.New("invalid emoji")

func validEmoji(e string) bool { return len(e) >= 1 && len(e) <= 16 }

// AddReaction records a (message, user, emoji) reaction (idempotent) and returns
// the message's channel id for broadcasting. ErrMessageNotFound if the message is
// gone/deleted; ErrInvalidEmoji for a bad emoji.
func (s *Store) AddReaction(ctx context.Context, messageID, userID int64, emoji string) (int64, error) {
	if !validEmoji(emoji) {
		return 0, ErrInvalidEmoji
	}
	channelID, err := s.messageChannel(ctx, messageID, true)
	if err != nil {
		return 0, err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO reactions (message_id, user_id, emoji) VALUES ($1, $2, $3)
		   ON CONFLICT (message_id, user_id, emoji) DO NOTHING`,
		messageID, userID, emoji)
	return channelID, err
}

// RemoveReaction deletes a reaction and returns the message's channel id.
func (s *Store) RemoveReaction(ctx context.Context, messageID, userID int64, emoji string) (int64, error) {
	channelID, err := s.messageChannel(ctx, messageID, false)
	if err != nil {
		return 0, err
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
		messageID, userID, emoji)
	return channelID, err
}

// messageChannel returns a message's channel id, optionally requiring it to be
// non-deleted. ErrMessageNotFound when it doesn't exist (or is deleted when required).
func (s *Store) messageChannel(ctx context.Context, messageID int64, mustBeLive bool) (int64, error) {
	q := `SELECT channel_id FROM messages WHERE id = $1`
	if mustBeLive {
		q += ` AND deleted_at IS NULL`
	}
	var channelID int64
	if err := s.pool.QueryRow(ctx, q, messageID).Scan(&channelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrMessageNotFound
		}
		return 0, err
	}
	return channelID, nil
}

// reactionsForMessages returns reaction summaries keyed by message id. `mine` is
// true for emojis the viewer reacted with; pass viewerID 0 for count-only.
func (s *Store) reactionsForMessages(ctx context.Context, messageIDs []int64, viewerID int64) (map[int64][]ReactionSummary, error) {
	out := make(map[int64][]ReactionSummary)
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT message_id, emoji, COUNT(*), bool_or(user_id = $2)
		   FROM reactions WHERE message_id = ANY($1)
		  GROUP BY message_id, emoji
		  ORDER BY MIN(created_at)`,
		messageIDs, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var rs ReactionSummary
		if err := rows.Scan(&mid, &rs.Emoji, &rs.Count, &rs.Mine); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], rs)
	}
	return out, rows.Err()
}

// ReactionsForMessage returns reaction summaries for a single message (viewerID 0
// → count-only). Used to broadcast/return updated state after a change.
func (s *Store) ReactionsForMessage(ctx context.Context, messageID, viewerID int64) ([]ReactionSummary, error) {
	byMsg, err := s.reactionsForMessages(ctx, []int64{messageID}, viewerID)
	if err != nil {
		return nil, err
	}
	return byMsg[messageID], nil
}

// DefaultChannelID returns the id of the default `general` channel.
func (s *Store) DefaultChannelID(ctx context.Context) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM channels WHERE name = 'general'`).Scan(&id)
	return id, err
}

// ChannelExists reports whether a channel id exists.
func (s *Store) ChannelExists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channels WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}

// CanAccessChannel reports whether userID may read/join channelID. Public
// channels are open to everyone; DM (and future private) channels require
// membership. A non-existent channel returns (false, nil).
func (s *Store) CanAccessChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT c.kind <> 'dm'
		     OR EXISTS (SELECT 1 FROM channel_members m
		                 WHERE m.channel_id = c.id AND m.user_id = $2)
		   FROM channels c WHERE c.id = $1`, channelID, userID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// LookupUserByUsername resolves a DM target, returning ErrUserNotFound if absent.
func (s *Store) LookupUserByUsername(ctx context.Context, username string) (DMUser, error) {
	var u DMUser
	err := s.pool.QueryRow(ctx,
		`SELECT id, username FROM users WHERE username = $1`, username).Scan(&u.ID, &u.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return DMUser{}, ErrUserNotFound
	}
	return u, err
}

// CreateOrGetDM returns the existing two-member DM channel between users a and b,
// creating it (kind='dm', both as members) if absent. Idempotent for sequential
// callers. Returns the channel as seen by viewer `a` (the other user is `b`).
func (s *Store) CreateOrGetDM(ctx context.Context, a, b int64) (DMChannel, error) {
	if a == b {
		return DMChannel{}, ErrCannotDMSelf
	}
	other, err := s.LookupUserByID(ctx, b)
	if err != nil {
		return DMChannel{}, err
	}

	// Existing DM with exactly {a, b}?
	var dm DMChannel
	dm.User = other
	err = s.pool.QueryRow(ctx,
		`SELECT c.id, c.created_at FROM channels c
		  WHERE c.kind = 'dm'
		    AND EXISTS (SELECT 1 FROM channel_members m WHERE m.channel_id = c.id AND m.user_id = $1)
		    AND EXISTS (SELECT 1 FROM channel_members m WHERE m.channel_id = c.id AND m.user_id = $2)
		    AND (SELECT COUNT(*) FROM channel_members m WHERE m.channel_id = c.id) = 2
		  LIMIT 1`, a, b).Scan(&dm.ID, &dm.CreatedAt)
	if err == nil {
		return dm, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return DMChannel{}, err
	}

	// Create it atomically.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DMChannel{}, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx,
		`INSERT INTO channels (kind) VALUES ('dm') RETURNING id, created_at`).
		Scan(&dm.ID, &dm.CreatedAt); err != nil {
		return DMChannel{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO channel_members (channel_id, user_id) VALUES ($1, $2), ($1, $3)`,
		dm.ID, a, b); err != nil {
		return DMChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DMChannel{}, err
	}
	return dm, nil
}

// LookupUserByID resolves a user id to id+username, ErrUserNotFound if absent.
func (s *Store) LookupUserByID(ctx context.Context, id int64) (DMUser, error) {
	var u DMUser
	err := s.pool.QueryRow(ctx,
		`SELECT id, username FROM users WHERE id = $1`, id).Scan(&u.ID, &u.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return DMUser{}, ErrUserNotFound
	}
	return u, err
}

// ListDMs returns userID's direct-message channels, each with the other member.
func (s *Store) ListDMs(ctx context.Context, userID int64) ([]DMChannel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.created_at, u.id, u.username
		   FROM channels c
		   JOIN channel_members me    ON me.channel_id = c.id AND me.user_id = $1
		   JOIN channel_members other ON other.channel_id = c.id AND other.user_id <> $1
		   JOIN users u ON u.id = other.user_id
		  WHERE c.kind = 'dm'
		  ORDER BY c.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dms := make([]DMChannel, 0)
	for rows.Next() {
		var d DMChannel
		if err := rows.Scan(&d.ID, &d.CreatedAt, &d.User.ID, &d.User.Username); err != nil {
			return nil, err
		}
		dms = append(dms, d)
	}
	return dms, rows.Err()
}

// Recent returns up to limit messages from the given channel in chronological
// (oldest-first) order.
func (s *Store) Recent(ctx context.Context, channelID, viewerID int64, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.deleted_at, m.edited_at
		   FROM messages m JOIN users u ON u.id = m.user_id
		  WHERE m.channel_id = $1
		  ORDER BY m.id DESC LIMIT $2`, channelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		var deletedAt, editedAt *time.Time
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt, &deletedAt, &editedAt); err != nil {
			return nil, err
		}
		m.EditedAt = editedAt
		if deletedAt != nil {
			m.Deleted = true
			m.Body = "[deleted]"
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	ids := make([]int64, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	byMsg, err := s.reactionsForMessages(ctx, ids, viewerID)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		msgs[i].Reactions = byMsg[msgs[i].ID]
	}
	return msgs, nil
}

// HandleRecent serves recent history for a channel over REST. The channel is
// chosen by ?channel=<id>, defaulting to `general`.
func HandleRecent(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID, err := ChannelIDFromQuery(r, store)
		if err != nil {
			http.Error(w, `{"error":"invalid channel"}`, http.StatusBadRequest)
			return
		}
		viewer, _ := auth.UserFrom(r.Context())
		if ok, err := store.CanAccessChannel(r.Context(), channelID, viewer.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		msgs, err := store.Recent(r.Context(), channelID, viewer.ID, 50)
		if err != nil {
			http.Error(w, `{"error":"could not load messages"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	}
}

// ChannelIDFromQuery resolves ?channel=<id> against the request, defaulting to
// the `general` channel when the parameter is absent.
func ChannelIDFromQuery(r *http.Request, store *Store) (int64, error) {
	raw := r.URL.Query().Get("channel")
	if raw == "" {
		return store.DefaultChannelID(r.Context())
	}
	return strconv.ParseInt(raw, 10, 64)
}

// ListChannels returns all channels in creation order.
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at FROM channels WHERE kind = 'public' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channels := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, c)
	}
	return channels, rows.Err()
}

// HandleChannels serves the channel list over REST.
func HandleChannels(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channels, err := store.ListChannels(r.Context())
		if err != nil {
			http.Error(w, `{"error":"could not load channels"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(channels)
	}
}

// CreateChannel inserts a new channel, returning ErrChannelExists if the name is
// already taken.
func (s *Store) CreateChannel(ctx context.Context, name string) (Channel, error) {
	c := Channel{Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO channels (name) VALUES ($1) RETURNING id, created_at`, name,
	).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return Channel{}, ErrChannelExists
		}
		return Channel{}, err
	}
	return c, nil
}

// HandleCreateChannel creates a channel from a JSON {"name":"..."} body.
func HandleCreateChannel(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if !channelNameRe.MatchString(in.Name) {
			http.Error(w, `{"error":"channel name must be 2-32 chars of [a-z0-9_-]"}`, http.StatusBadRequest)
			return
		}
		c, err := store.CreateChannel(r.Context(), in.Name)
		if errors.Is(err, ErrChannelExists) {
			http.Error(w, `{"error":"channel name already taken"}`, http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not create channel"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(c)
	}
}

// HandleListDMs serves the caller's direct-message channels.
func HandleListDMs(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		dms, err := store.ListDMs(r.Context(), me.ID)
		if err != nil {
			http.Error(w, `{"error":"could not load dms"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dms)
	}
}

// HandleCreateDM opens (or returns the existing) DM with {"username":"..."}.
func HandleCreateDM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		var in struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		target, err := store.LookupUserByUsername(r.Context(), in.Username)
		if errors.Is(err, ErrUserNotFound) {
			http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not look up user"}`, http.StatusInternalServerError)
			return
		}
		dm, err := store.CreateOrGetDM(r.Context(), me.ID, target.ID)
		if errors.Is(err, ErrCannotDMSelf) {
			http.Error(w, `{"error":"cannot DM yourself"}`, http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not open dm"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dm)
	}
}
