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
	"time"

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
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
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

// Save inserts a message into the default `general` channel and returns it fully
// populated. Per-channel targeting (a channelID parameter) lands in a later slice
// once the WS gateway and client are channel-aware; the column is wired now.
func (s *Store) Save(ctx context.Context, userID int64, username, body string) (Message, error) {
	m := Message{UserID: userID, Username: username, Body: body}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (user_id, body, channel_id)
		      VALUES ($1, $2, (SELECT id FROM channels WHERE name = 'general'))
		   RETURNING id, created_at, channel_id`,
		userID, body,
	).Scan(&m.ID, &m.CreatedAt, &m.ChannelID)
	return m, err
}

// Recent returns up to limit messages in chronological (oldest-first) order.
func (s *Store) Recent(ctx context.Context, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at
		   FROM messages m JOIN users u ON u.id = m.user_id
		  ORDER BY m.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
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

// HandleRecent serves recent history over REST (handy for clients that aren't
// connected to the WebSocket yet).
func HandleRecent(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		msgs, err := store.Recent(r.Context(), 50)
		if err != nil {
			http.Error(w, `{"error":"could not load messages"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	}
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
