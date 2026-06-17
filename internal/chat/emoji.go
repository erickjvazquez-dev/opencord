package chat

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrEmojiExists is returned when an emoji name is already taken in the server.
	ErrEmojiExists = errors.New("emoji name already taken")
	// ErrEmojiNotFound is returned when an emoji id doesn't exist (in the scope queried).
	ErrEmojiNotFound = errors.New("emoji not found")
	// emojiNameRe is a Discord-style custom-emoji slug: lowercase letters, digits, and
	// underscores, 2-32 chars (no leading colon — the client wraps it in :name:).
	emojiNameRe = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)
)

// ServerEmoji is a custom image emoji belonging to a server. The on-disk key and
// sniffed content type are internal (served via /api/emoji/{id}), so they aren't
// serialized — the client renders the emoji from the serve URL it builds from the id.
type ServerEmoji struct {
	ID        int64     `json:"id"`
	ServerID  int64     `json:"serverId"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"createdBy"`
	CreatedAt time.Time `json:"createdAt"`
}

// ValidEmojiName reports whether name is a valid custom-emoji slug (2-32 [a-z0-9_]).
func ValidEmojiName(name string) bool { return emojiNameRe.MatchString(name) }

// CreateServerEmoji inserts a custom emoji into serverID (the bytes have already been
// written to disk under key, content type sniffed by the caller) and returns it fully
// populated. A duplicate name within the same server maps to ErrEmojiExists (the
// unique index is the source of truth, so concurrent inserts can't both win).
func (s *Store) CreateServerEmoji(ctx context.Context, serverID int64, name, key, contentType string, createdBy int64) (ServerEmoji, error) {
	e := ServerEmoji{ServerID: serverID, Name: name, CreatedBy: createdBy}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO server_emoji (server_id, name, emoji_key, content_type, created_by)
		   VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		serverID, name, key, contentType, createdBy,
	).Scan(&e.ID, &e.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ServerEmoji{}, ErrEmojiExists
	}
	if err != nil {
		return ServerEmoji{}, err
	}
	return e, nil
}

// ListServerEmoji returns serverID's custom emoji ordered by name. Returns a non-nil
// empty slice when the server has none, so the JSON is `[]` rather than `null`.
func (s *Store) ListServerEmoji(ctx context.Context, serverID int64) ([]ServerEmoji, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, server_id, name, created_by, created_at
		   FROM server_emoji WHERE server_id = $1 ORDER BY name`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ServerEmoji, 0)
	for rows.Next() {
		var e ServerEmoji
		if err := rows.Scan(&e.ID, &e.ServerID, &e.Name, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ServerEmojiForServe returns an emoji's on-disk key + sniffed content type for the
// serve handler. ErrEmojiNotFound when the id doesn't exist.
func (s *Store) ServerEmojiForServe(ctx context.Context, emojiID int64) (key, contentType string, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT emoji_key, content_type FROM server_emoji WHERE id = $1`, emojiID).
		Scan(&key, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrEmojiNotFound
	}
	if err != nil {
		return "", "", err
	}
	return key, contentType, nil
}

// DeleteServerEmoji removes emojiID from serverID. Scoping the delete by server_id
// means an admin of one server can't delete another server's emoji (a wrong/foreign
// server id affects 0 rows → ErrEmojiNotFound, Rule B). ErrEmojiNotFound when the
// emoji doesn't exist in this server.
func (s *Store) DeleteServerEmoji(ctx context.Context, serverID, emojiID int64) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM server_emoji WHERE id = $1 AND server_id = $2`, emojiID, serverID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrEmojiNotFound
	}
	return nil
}
