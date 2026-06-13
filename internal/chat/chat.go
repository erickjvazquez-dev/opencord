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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrChannelExists is returned when a channel name is already taken.
	ErrChannelExists = errors.New("channel name already taken")
	// channelNameRe mirrors a Discord-style channel slug: lowercase, 2-32 chars.
	channelNameRe = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
)

type Message struct {
	ID        int64     `json:"id"`
	ChannelID int64     `json:"channelId"`
	UserID    int64     `json:"userId"`
	Username  string    `json:"username"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"createdAt"`
	EditedAt  *time.Time `json:"editedAt,omitempty"`
	Deleted   bool       `json:"deleted,omitempty"`
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

// Recent returns up to limit messages from the given channel in chronological
// (oldest-first) order.
func (s *Store) Recent(ctx context.Context, channelID int64, limit int) ([]Message, error) {
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
		msgs, err := store.Recent(r.Context(), channelID, 50)
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
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at FROM channels ORDER BY id`)
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
