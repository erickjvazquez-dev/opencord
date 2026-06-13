// Package chat persists and retrieves messages for the single global channel
// that the MVP exposes. Channels/servers come in a later milestone.
package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Message struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	Username  string    `json:"username"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Save inserts a message and returns it fully populated.
func (s *Store) Save(ctx context.Context, userID int64, username, body string) (Message, error) {
	m := Message{UserID: userID, Username: username, Body: body}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (user_id, body) VALUES ($1, $2) RETURNING id, created_at`,
		userID, body,
	).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

// Recent returns up to limit messages in chronological (oldest-first) order.
func (s *Store) Recent(ctx context.Context, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.user_id, u.username, m.body, m.created_at
		   FROM messages m JOIN users u ON u.id = m.user_id
		  ORDER BY m.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt); err != nil {
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
