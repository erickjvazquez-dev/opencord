// Package chat persists and retrieves channels and messages. Messages are
// channel-scoped at the data layer (v0.2); the WS gateway still serves a single
// default room until per-channel routing lands.
package chat

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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
	// ErrForbidden is returned when a user acts on a channel they can't access
	// (e.g. reacting to a message in a DM they're not a member of).
	ErrForbidden = errors.New("forbidden")
	// ErrServerNotFound is returned for an unknown server id.
	ErrServerNotFound = errors.New("server not found")
	// ErrInvalidInvite is returned when an invite code doesn't exist.
	ErrInvalidInvite = errors.New("invalid invite code")
	// ErrInvalidRole is returned when a role is not one that can be assigned.
	ErrInvalidRole = errors.New("invalid role")
	// ErrInvalidPolicy is returned when a channel posting policy is not valid.
	ErrInvalidPolicy = errors.New("invalid posting policy")
	// ErrSlowMode is returned when a non-admin posts again before the channel's
	// slowmode cooldown has elapsed.
	ErrSlowMode = errors.New("slow mode active")
	// ErrInvalidSlowmode is returned when a slowmode value is out of range.
	ErrInvalidSlowmode = errors.New("invalid slowmode (0..21600 seconds)")
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

// Server is a guild grouping channels under a shared membership. Role is the
// requesting user's role in it ('owner'|'admin'|'member'), populated by the
// per-user views (ListServers, CreateServer, RedeemInvite).
type Server struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	OwnerID   int64     `json:"ownerId"`
	CreatedAt time.Time `json:"createdAt"`
	Role      string    `json:"role,omitempty"`
}

// ServerMember is a participant in a server with their role.
type ServerMember struct {
	UserID   int64  `json:"userId"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// ValidChannelName reports whether name is a valid channel slug (2-32 [a-z0-9_-]).
func ValidChannelName(name string) bool { return channelNameRe.MatchString(name) }

type Message struct {
	ID        int64     `json:"id"`
	ChannelID int64     `json:"channelId"`
	UserID    int64     `json:"userId"`
	Username  string    `json:"username"`
	Body      string     `json:"body"`
	CreatedAt time.Time         `json:"createdAt"`
	EditedAt  *time.Time        `json:"editedAt,omitempty"`
	Deleted   bool              `json:"deleted,omitempty"`
	Pinned    bool              `json:"pinned,omitempty"`
	Reactions []ReactionSummary `json:"reactions,omitempty"`
	// Reply reference (Discord-style). ReplyTo is the referenced message id; the
	// author + body snippet are denormalized so history and live broadcasts render
	// the quoted preview without an extra round-trip. All three are unset when the
	// message isn't a reply.
	ReplyTo       *int64 `json:"replyTo,omitempty"`
	ReplyToAuthor string `json:"replyToAuthor,omitempty"`
	ReplyToBody   string `json:"replyToBody,omitempty"`
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
	// PostPolicy is 'everyone' or 'admins' (read-only). Populated for server channels.
	PostPolicy string `json:"postPolicy,omitempty"`
	// Topic is a short channel description shown in the header (server channels).
	Topic string `json:"topic,omitempty"`
	// SlowmodeSeconds is the per-channel post cooldown for non-admins (0 = off).
	SlowmodeSeconds int `json:"slowmodeSeconds,omitempty"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Save inserts a message into the given channel and returns it fully populated.
// It's the no-reply path; SaveReply carries the optional reply reference.
func (s *Store) Save(ctx context.Context, channelID, userID int64, username, body string) (Message, error) {
	return s.SaveReply(ctx, channelID, userID, username, body, nil)
}

// SaveReply inserts a message that optionally references an earlier message
// (replyTo) and returns it fully populated. Rule B/C: the reference is validated
// to exist AND to live in the SAME channel — a bogus or cross-channel replyTo is
// dropped (the message posts without a reply) rather than honored, so a client can
// never make a message it can't see leak through a reply preview.
func (s *Store) SaveReply(ctx context.Context, channelID, userID int64, username, body string, replyTo *int64) (Message, error) {
	// Enforce a read-only ('admins') channel — defense in depth, every caller is gated.
	if ok, err := s.CanPostInChannel(ctx, channelID, userID); err != nil {
		return Message{}, err
	} else if !ok {
		return Message{}, ErrForbidden
	}
	// Enforce slowmode (non-admins only) — server-side, can't be bypassed by a client.
	if blocked, err := s.slowmodeBlocked(ctx, channelID, userID); err != nil {
		return Message{}, err
	} else if blocked {
		return Message{}, ErrSlowMode
	}
	m := Message{ChannelID: channelID, UserID: userID, Username: username, Body: body}
	// Validate + denormalize the reply reference. A reference that doesn't resolve
	// to a message in THIS channel is dropped (replyTo := nil) so the INSERT stores
	// no link and the preview stays empty.
	if replyTo != nil {
		var refChannel int64
		var refAuthor, refBody string
		var refDeleted *time.Time
		err := s.pool.QueryRow(ctx,
			`SELECT m.channel_id, u.username, m.body, m.deleted_at
			   FROM messages m JOIN users u ON u.id = m.user_id WHERE m.id = $1`, *replyTo).
			Scan(&refChannel, &refAuthor, &refBody, &refDeleted)
		switch {
		case errors.Is(err, pgx.ErrNoRows) || (err == nil && refChannel != channelID):
			replyTo = nil // bogus or cross-channel — drop the reference
		case err != nil:
			return Message{}, err
		default:
			m.ReplyTo = replyTo
			m.ReplyToAuthor = refAuthor
			m.ReplyToBody = replySnippet(refBody, refDeleted)
		}
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (channel_id, user_id, body, reply_to) VALUES ($1, $2, $3, $4)
		   RETURNING id, created_at`,
		channelID, userID, body, replyTo,
	).Scan(&m.ID, &m.CreatedAt)
	return m, err
}

// replySnippet renders the short preview of a replied-to message: "[deleted]" if the
// target was soft-deleted, else its body truncated to ~80 runes (rune-safe so a
// multibyte char is never split).
func replySnippet(body string, deletedAt *time.Time) string {
	if deletedAt != nil {
		return "[deleted]"
	}
	r := []rune(body)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return body
}

// CanPostInChannel reports whether userID may post in channelID. True unless the
// channel's policy is 'admins' AND it's a server channel AND the user isn't a server
// admin (read-only / announcement channel).
func (s *Store) CanPostInChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	var policy string
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT post_policy, server_id FROM channels WHERE id = $1`, channelID).Scan(&policy, &serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if policy != "admins" || serverID == nil {
		return true, nil
	}
	return s.IsServerAdmin(ctx, *serverID, userID)
}

// slowmodeBlocked reports whether userID must wait before posting in channelID due
// to slowmode. False when slowmode is off, the user is a server admin, or it's their
// first message. The cooldown is measured server-side (now() - created_at) so a
// client can't fake its clock to bypass it (Rule B).
func (s *Store) slowmodeBlocked(ctx context.Context, channelID, userID int64) (bool, error) {
	var slowmode int
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT slowmode_seconds, server_id FROM channels WHERE id = $1`, channelID).Scan(&slowmode, &serverID)
	if err != nil {
		return false, err
	}
	if slowmode <= 0 {
		return false, nil
	}
	// Admins/owner are exempt (mirrors read-only).
	if serverID != nil {
		if admin, err := s.IsServerAdmin(ctx, *serverID, userID); err != nil {
			return false, err
		} else if admin {
			return false, nil
		}
	}
	var blocked bool
	err = s.pool.QueryRow(ctx,
		`SELECT (now() - created_at) < make_interval(secs => $3)
		   FROM messages WHERE channel_id = $1 AND user_id = $2
		   ORDER BY created_at DESC LIMIT 1`,
		channelID, userID, slowmode).Scan(&blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // first message in this channel
	}
	if err != nil {
		return false, err
	}
	return blocked, nil
}

// SetChannelSlowmode sets a server channel's post cooldown (0..21600s; 0 = off).
// Server admins only; ErrInvalidSlowmode for out-of-range; ErrForbidden otherwise.
func (s *Store) SetChannelSlowmode(ctx context.Context, channelID, actorID int64, seconds int) error {
	if seconds < 0 || seconds > 21600 {
		return ErrInvalidSlowmode
	}
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT server_id FROM channels WHERE id = $1`, channelID).Scan(&serverID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && serverID == nil) {
		return ErrForbidden // unknown channel or not a server channel
	}
	if err != nil {
		return err
	}
	if ok, err := s.IsServerAdmin(ctx, *serverID, actorID); err != nil {
		return err
	} else if !ok {
		return ErrForbidden
	}
	_, err = s.pool.Exec(ctx, `UPDATE channels SET slowmode_seconds = $2 WHERE id = $1`, channelID, seconds)
	return err
}

// SetChannelPostPolicy sets a server channel's posting policy ('everyone'|'admins').
// Server admins only; ErrInvalidPolicy for a bad value; ErrForbidden otherwise.
func (s *Store) SetChannelPostPolicy(ctx context.Context, channelID, actorID int64, policy string) error {
	if policy != "everyone" && policy != "admins" {
		return ErrInvalidPolicy
	}
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT server_id FROM channels WHERE id = $1`, channelID).Scan(&serverID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && serverID == nil) {
		return ErrForbidden // unknown channel or not a server channel
	}
	if err != nil {
		return err
	}
	if ok, err := s.IsServerAdmin(ctx, *serverID, actorID); err != nil {
		return err
	} else if !ok {
		return ErrForbidden
	}
	_, err = s.pool.Exec(ctx, `UPDATE channels SET post_policy = $2 WHERE id = $1`, channelID, policy)
	return err
}

// maxTopicLen bounds a channel topic (Rule B: bound every stored value).
const maxTopicLen = 1024

// ErrTopicTooLong is returned when a channel topic exceeds maxTopicLen.
var ErrTopicTooLong = errors.New("channel topic too long")

// SetChannelTopic sets a server channel's topic (header description). Server
// admins only; ErrTopicTooLong for an over-long value; ErrForbidden for a
// non-admin, an unknown channel, or a non-server channel.
func (s *Store) SetChannelTopic(ctx context.Context, channelID, actorID int64, topic string) error {
	if len(topic) > maxTopicLen {
		return ErrTopicTooLong
	}
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT server_id FROM channels WHERE id = $1`, channelID).Scan(&serverID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && serverID == nil) {
		return ErrForbidden // unknown channel or not a server channel
	}
	if err != nil {
		return err
	}
	if ok, err := s.IsServerAdmin(ctx, *serverID, actorID); err != nil {
		return err
	} else if !ok {
		return ErrForbidden
	}
	_, err = s.pool.Exec(ctx, `UPDATE channels SET topic = $2 WHERE id = $1`, channelID, topic)
	return err
}

// ErrMessageNotFound is returned when a message doesn't exist, isn't owned by the
// caller, or was already deleted.
var ErrMessageNotFound = errors.New("message not found")

// DeleteMessage soft-deletes the caller's own message and returns a stub (id +
// channel + deleted flag) suitable for broadcasting the removal to the channel.
func (s *Store) DeleteMessage(ctx context.Context, id, userID int64) (Message, error) {
	// Resolve the live message's channel, author, and (if any) server.
	var channelID, authorID int64
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT m.channel_id, m.user_id, c.server_id
		   FROM messages m JOIN channels c ON c.id = m.channel_id
		  WHERE m.id = $1 AND m.deleted_at IS NULL`, id).Scan(&channelID, &authorID, &serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, err
	}
	// Allowed if the author, or an admin of the channel's server (moderation).
	if userID != authorID {
		allowed := false
		if serverID != nil {
			if allowed, err = s.IsServerAdmin(ctx, *serverID, userID); err != nil {
				return Message{}, err
			}
		}
		if !allowed {
			return Message{}, ErrMessageNotFound
		}
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE messages SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id); err != nil {
		return Message{}, err
	}
	return Message{ID: id, ChannelID: channelID, Deleted: true, Body: "[deleted]"}, nil
}

// SetMessagePinned pins or unpins a (non-deleted) message and returns a stub
// (id + channel + pinned) for broadcasting the change. Authorization mirrors
// moderation: in a server channel only server admins may pin; in a serverless
// channel (global, DM) any member who can access it may. ErrMessageNotFound if the
// message is gone; ErrForbidden if the actor isn't allowed.
func (s *Store) SetMessagePinned(ctx context.Context, id, actorID int64, pinned bool) (Message, error) {
	var channelID int64
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT m.channel_id, c.server_id
		   FROM messages m JOIN channels c ON c.id = m.channel_id
		  WHERE m.id = $1 AND m.deleted_at IS NULL`, id).Scan(&channelID, &serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, ErrMessageNotFound
	}
	if err != nil {
		return Message{}, err
	}
	if serverID != nil {
		if ok, err := s.IsServerAdmin(ctx, *serverID, actorID); err != nil {
			return Message{}, err
		} else if !ok {
			return Message{}, ErrForbidden
		}
	} else {
		if ok, err := s.CanAccessChannel(ctx, channelID, actorID); err != nil {
			return Message{}, err
		} else if !ok {
			return Message{}, ErrForbidden
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE messages SET pinned = $2 WHERE id = $1`, id, pinned); err != nil {
		return Message{}, err
	}
	return Message{ID: id, ChannelID: channelID, Pinned: pinned}, nil
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
	if err := s.requireChannelAccess(ctx, channelID, userID); err != nil {
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
	if err := s.requireChannelAccess(ctx, channelID, userID); err != nil {
		return 0, err
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
		messageID, userID, emoji)
	return channelID, err
}

// requireChannelAccess returns ErrForbidden if userID can't access channelID.
func (s *Store) requireChannelAccess(ctx context.Context, channelID, userID int64) error {
	ok, err := s.CanAccessChannel(ctx, channelID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
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
	// The default room is the GLOBAL general; per-server channels may also be named
	// "general", so scope to server_id IS NULL to keep this unambiguous.
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM channels WHERE name = 'general' AND server_id IS NULL`).Scan(&id)
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
		`SELECT CASE
		          WHEN c.kind = 'dm' THEN
		            EXISTS (SELECT 1 FROM channel_members m
		                     WHERE m.channel_id = c.id AND m.user_id = $2)
		          WHEN c.server_id IS NOT NULL THEN
		            EXISTS (SELECT 1 FROM server_members sm
		                     WHERE sm.server_id = c.server_id AND sm.user_id = $2)
		          ELSE TRUE
		        END
		   FROM channels c WHERE c.id = $1`, channelID, userID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// LookupUserByIdentifier resolves a DM/invite target from a free-form identifier:
// an all-digit string is treated as a user id, otherwise as a username. (Email
// lookup will slot in here once accounts store an email — a one-line WHERE email=$1
// branch; until then an email simply won't match a username and returns not-found.)
func (s *Store) LookupUserByIdentifier(ctx context.Context, ident string) (DMUser, error) {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return DMUser{}, ErrUserNotFound
	}
	if id, err := strconv.ParseInt(ident, 10, 64); err == nil {
		return s.LookupUserByID(ctx, id)
	}
	return s.LookupUserByUsername(ctx, ident)
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

// CreateServer creates a server owned by ownerID and adds the owner as its first
// member, atomically.
func (s *Store) CreateServer(ctx context.Context, ownerID int64, name string) (Server, error) {
	srv := Server{Name: name, OwnerID: ownerID}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx,
		`INSERT INTO servers (name, owner_id) VALUES ($1, $2) RETURNING id, created_at`,
		name, ownerID).Scan(&srv.ID, &srv.CreatedAt); err != nil {
		return Server{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, role) VALUES ($1, $2, 'owner')`,
		srv.ID, ownerID); err != nil {
		return Server{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Server{}, err
	}
	srv.Role = "owner"
	return srv, nil
}

// ListServers returns the servers userID is a member of.
func (s *Store) ListServers(ctx context.Context, userID int64) ([]Server, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT s.id, s.name, s.owner_id, s.created_at, m.role
		   FROM servers s
		   JOIN server_members m ON m.server_id = s.id AND m.user_id = $1
		  ORDER BY s.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Server, 0)
	for rows.Next() {
		var srv Server
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.OwnerID, &srv.CreatedAt, &srv.Role); err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

// ServerRole returns userID's role in serverID ('owner'|'admin'|'member'), or ""
// if they are not a member.
func (s *Store) ServerRole(ctx context.Context, serverID, userID int64) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM server_members WHERE server_id = $1 AND user_id = $2`,
		serverID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// IsServerAdmin reports whether userID is an owner or admin of serverID.
func (s *Store) IsServerAdmin(ctx context.Context, serverID, userID int64) (bool, error) {
	role, err := s.ServerRole(ctx, serverID, userID)
	if err != nil {
		return false, err
	}
	return role == "owner" || role == "admin", nil
}

// SetServerRole lets the server OWNER set targetID's role to 'admin' or 'member'.
// Only the owner may change roles; the role must be valid; an owner row is never
// changed. Returns ErrForbidden (actor not owner / target is self), ErrInvalidRole,
// or ErrUserNotFound (target not a member).
func (s *Store) SetServerRole(ctx context.Context, serverID, actorID, targetID int64, role string) error {
	if role != "admin" && role != "member" {
		return ErrInvalidRole
	}
	actorRole, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if actorRole != "owner" {
		return ErrForbidden
	}
	if targetID == actorID {
		return ErrForbidden // the owner can't change their own role
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE server_members SET role = $3
		   WHERE server_id = $1 AND user_id = $2 AND role <> 'owner'`,
		serverID, targetID, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound // target isn't a member of this server
	}
	return nil
}

// ListServerMembers returns a server's members with roles, owner/admin first.
func (s *Store) ListServerMembers(ctx context.Context, serverID int64) ([]ServerMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.user_id, u.username, m.role
		   FROM server_members m JOIN users u ON u.id = m.user_id
		  WHERE m.server_id = $1
		  ORDER BY (m.role = 'owner') DESC, (m.role = 'admin') DESC, u.username`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ServerMember, 0)
	for rows.Next() {
		var m ServerMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsServerMember reports whether userID belongs to serverID.
func (s *Store) IsServerMember(ctx context.Context, serverID, userID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID).Scan(&ok)
	return ok, err
}

// AddServerMember adds userID to serverID (idempotent), ErrServerNotFound if absent.
func (s *Store) AddServerMember(ctx context.Context, serverID, userID int64) error {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1)`, serverID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrServerNotFound
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		serverID, userID)
	return err
}

// CreateServerChannel creates a members-only channel under a server.
func (s *Store) CreateServerChannel(ctx context.Context, serverID int64, name string) (Channel, error) {
	c := Channel{Name: name, PostPolicy: "everyone"}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO channels (name, server_id) VALUES ($1, $2) RETURNING id, created_at`,
		name, serverID).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique within the server
			return Channel{}, ErrChannelExists
		}
		return Channel{}, err
	}
	return c, nil
}

// ListServerChannels returns the channels under serverID, oldest first.
func (s *Store) ListServerChannels(ctx context.Context, serverID int64) ([]Channel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at, post_policy, topic, slowmode_seconds FROM channels WHERE server_id = $1 ORDER BY id`,
		serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.PostPolicy, &c.Topic, &c.SlowmodeSeconds); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// inviteCode returns 8 url-safe random characters (6 bytes of crypto/rand).
func inviteCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateInvite mints a unique invite code for serverID created by userID. The
// caller must verify membership. Retries on the (astronomically rare) collision.
func (s *Store) CreateInvite(ctx context.Context, serverID, userID int64) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		code, err := inviteCode()
		if err != nil {
			return "", err
		}
		_, err = s.pool.Exec(ctx,
			`INSERT INTO server_invites (code, server_id, created_by) VALUES ($1, $2, $3)`,
			code, serverID, userID)
		if err == nil {
			return code, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // code collision — try again
			continue
		}
		return "", err
	}
	return "", errors.New("could not allocate a unique invite code")
}

// RedeemInvite joins userID to the server the code belongs to and returns that
// server. ErrInvalidInvite if the code is unknown.
func (s *Store) RedeemInvite(ctx context.Context, code string, userID int64) (Server, error) {
	var srv Server
	err := s.pool.QueryRow(ctx,
		`SELECT s.id, s.name, s.owner_id, s.created_at
		   FROM server_invites i JOIN servers s ON s.id = i.server_id
		  WHERE i.code = $1`, code).Scan(&srv.ID, &srv.Name, &srv.OwnerID, &srv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrInvalidInvite
	}
	if err != nil {
		return Server{}, err
	}
	if err := s.AddServerMember(ctx, srv.ID, userID); err != nil {
		return Server{}, err
	}
	srv.Role = "member"
	return srv, nil
}

// Recent returns up to limit messages from the given channel in chronological
// (oldest-first) order.
func (s *Store) Recent(ctx context.Context, channelID, viewerID int64, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.deleted_at, m.edited_at, m.pinned,
		        m.reply_to, ru.username, r.body, r.deleted_at
		   FROM messages m JOIN users u ON u.id = m.user_id
		   LEFT JOIN messages r ON r.id = m.reply_to
		   LEFT JOIN users ru ON ru.id = r.user_id
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
		var replyTo *int64
		var replyAuthor, replyBody *string
		var replyDeleted *time.Time
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt, &deletedAt, &editedAt, &m.Pinned,
			&replyTo, &replyAuthor, &replyBody, &replyDeleted); err != nil {
			return nil, err
		}
		m.EditedAt = editedAt
		if deletedAt != nil {
			m.Deleted = true
			m.Body = "[deleted]"
		}
		// reply_to points at a non-deleted row only at post time; the target may be
		// soft-deleted later, in which case replyAuthor is still present and the
		// snippet renders "[deleted]".
		if replyTo != nil && replyAuthor != nil {
			m.ReplyTo = replyTo
			m.ReplyToAuthor = *replyAuthor
			rb := ""
			if replyBody != nil {
				rb = *replyBody
			}
			m.ReplyToBody = replySnippet(rb, replyDeleted)
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

// PinnedMessages returns the channel's pinned (non-deleted) messages, oldest first.
// Access is gated by the caller (HandlePins) before this runs.
func (s *Store) PinnedMessages(ctx context.Context, channelID int64) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.edited_at
		   FROM messages m JOIN users u ON u.id = m.user_id
		  WHERE m.channel_id = $1 AND m.pinned = true AND m.deleted_at IS NULL
		  ORDER BY m.id`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt, &m.EditedAt); err != nil {
			return nil, err
		}
		m.Pinned = true
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchMessages returns up to limit non-deleted messages in channelID whose body
// contains query (case-insensitive), in chronological order. The query's LIKE
// wildcards are escaped so it matches literally — a user typing '%' can't turn the
// search into match-all.
func (s *Store) SearchMessages(ctx context.Context, channelID int64, query string, limit int) ([]Message, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.edited_at
		   FROM messages m JOIN users u ON u.id = m.user_id
		  WHERE m.channel_id = $1 AND m.deleted_at IS NULL
		    AND m.body ILIKE '%' || $2 || '%'
		  ORDER BY m.id DESC LIMIT $3`, channelID, esc, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		var editedAt *time.Time
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt, &editedAt); err != nil {
			return nil, err
		}
		m.EditedAt = editedAt
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

// HandleSearch serves message search within a channel: ?channel=<id>&q=<text>.
func HandleSearch(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID, err := ChannelIDFromQuery(r, store)
		if err != nil {
			http.Error(w, `{"error":"invalid channel"}`, http.StatusBadRequest)
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" || len(query) > 200 {
			http.Error(w, `{"error":"q must be 1-200 chars"}`, http.StatusBadRequest)
			return
		}
		viewer, _ := auth.UserFrom(r.Context())
		if ok, err := store.CanAccessChannel(r.Context(), channelID, viewer.ID); err != nil || !ok {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		msgs, err := store.SearchMessages(r.Context(), channelID, query, 50)
		if err != nil {
			http.Error(w, `{"error":"could not search"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msgs)
	}
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

// HandlePins serves a channel's pinned messages (access-gated), for the pins panel.
func HandlePins(store *Store) http.HandlerFunc {
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
		msgs, err := store.PinnedMessages(r.Context(), channelID)
		if err != nil {
			http.Error(w, `{"error":"could not load pins"}`, http.StatusInternalServerError)
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
		`SELECT id, name, created_at FROM channels
		  WHERE kind = 'public' AND server_id IS NULL ORDER BY id`)
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

// HandleCreateDM opens (or returns the existing) DM with a target identified by
// {"identifier":"..."} — a username or numeric user id (email once accounts store
// it). Accepts the legacy {"username":"..."} field too for backward compatibility.
func HandleCreateDM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		var in struct {
			Username   string `json:"username"`
			Identifier string `json:"identifier"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		ident := in.Identifier
		if ident == "" {
			ident = in.Username
		}
		target, err := store.LookupUserByIdentifier(r.Context(), ident)
		if errors.Is(err, ErrUserNotFound) {
			http.Error(w, `{"error":"no user found for that username or id"}`, http.StatusNotFound)
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
