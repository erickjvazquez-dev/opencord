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
	"sort"
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
	// ErrCategoryNotFound is returned when a category id doesn't exist in the server.
	ErrCategoryNotFound = errors.New("category not found")
	// ErrInvalidChannelKind is returned when a server channel kind is not 'public' or 'voice'.
	ErrInvalidChannelKind = errors.New("channel kind must be 'public' or 'voice'")
	// ErrUserNotFound is returned when a DM target username does not exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrCannotDMSelf is returned when a user tries to open a DM with themselves.
	ErrCannotDMSelf = errors.New("cannot DM yourself")
	// ErrNotGroupDM is returned when leaving/adding to a channel that isn't a group DM (a
	// 1:1 DM or a non-DM channel can't be "left"/added to).
	ErrNotGroupDM = errors.New("can only leave a group DM")
	// ErrAlreadyMember is returned when adding a user who is already in the group DM.
	ErrAlreadyMember = errors.New("user is already in this group DM")
	// ErrForbidden is returned when a user acts on a channel they can't access
	// (e.g. reacting to a message in a DM they're not a member of).
	ErrForbidden = errors.New("forbidden")
	// ErrServerNotFound is returned for an unknown server id.
	ErrServerNotFound = errors.New("server not found")
	// ErrInvalidInvite is returned when an invite code doesn't exist.
	ErrInvalidInvite = errors.New("invalid invite code")
	// ErrInviteExpired is returned when an invite code exists but has expired.
	ErrInviteExpired = errors.New("invite has expired")
	// ErrInviteExhausted is returned when an invite has reached its max-uses cap.
	ErrInviteExhausted = errors.New("invite has reached its maximum uses")
	// ErrBanned is returned when a banned user tries to (re)join a server.
	ErrBanned = errors.New("banned from this server")
	// ErrBlocked is returned when a user tries to open/use a DM with someone in a
	// block relationship with them (symmetric: either side having blocked the other).
	ErrBlocked = errors.New("blocked")
	// ErrTimedOut is returned when a timed-out member tries to post.
	ErrTimedOut = errors.New("timed out")
	// ErrInvalidRole is returned when a role is not one that can be assigned.
	ErrInvalidRole = errors.New("invalid role")
	// ErrInvalidPolicy is returned when a channel posting policy is not valid.
	ErrInvalidPolicy = errors.New("invalid posting policy")
	// ErrSlowMode is returned when a non-admin posts again before the channel's
	// slowmode cooldown has elapsed.
	ErrSlowMode = errors.New("slow mode active")
	// ErrInvalidSlowmode is returned when a slowmode value is out of range.
	ErrInvalidSlowmode = errors.New("invalid slowmode (0..21600 seconds)")
	// ErrGroupTooLarge is returned when a group DM would exceed the 10-member cap.
	ErrGroupTooLarge = errors.New("group DM exceeds the 10-member limit")
	// ErrInvalidColor is returned when a custom role's color is not a #RGB/#RRGGBB hex.
	ErrInvalidColor = errors.New("invalid color (want #RGB or #RRGGBB hex)")
	// ErrInvalidRoleName is returned when a custom role's name is empty or too long.
	ErrInvalidRoleName = errors.New("invalid role name (1-32 characters)")
	// ErrRoleNotFound is returned when a custom role id doesn't exist in the server.
	ErrRoleNotFound = errors.New("role not found")
	// ErrChannelNotFound is returned when a channel id doesn't exist.
	ErrChannelNotFound = errors.New("channel not found")
	// ErrInvalidThreadName is returned when a thread name is empty or too long (>100).
	ErrInvalidThreadName = errors.New("invalid thread name (1-100 characters)")
	// ErrNotThreadable is returned when trying to thread a DM or a thread (no nesting).
	ErrNotThreadable = errors.New("cannot start a thread here")
	// channelNameRe mirrors a Discord-style channel slug: lowercase, 2-32 chars.
	channelNameRe = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
	// roleColorRe matches a #RGB or #RRGGBB hex color (case-insensitive).
	roleColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

// DMUser is one of the other participants in a direct message channel.
type DMUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// DMChannel is a direct-message channel as seen by one participant: the channel
// plus the *other* members in it. Users holds every other member (one entry for a
// 1:1 DM, two or more for a group DM), username-sorted. User mirrors Users[0] and is
// retained for the 1:1 client contract (v0.6 group DMs, slice 1).
type DMChannel struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	User      DMUser    `json:"user"`
	Users     []DMUser  `json:"users"`
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
	// Online is annotated by the HTTP layer from the hub's live-connection set
	// (the store doesn't know about sockets). False = no active WS connection.
	Online bool `json:"online"`
	// Status is the member's custom status line ("" = none).
	Status string `json:"status,omitempty"`
	// StatusEmoji is an optional short emoji shown before the status ("" = none).
	StatusEmoji string `json:"statusEmoji,omitempty"`
	// PresenceState is the member's raw user-chosen availability from storage
	// (online|idle|dnd|invisible). Internal — the HTTP layer derives the visible
	// Presence from it + the live-connection set, so it is not serialized.
	PresenceState string `json:"-"`
	// Presence is the EFFECTIVE presence the HTTP layer annotates for this viewer
	// (online|idle|dnd|offline). Others see 'invisible'/disconnected as 'offline';
	// the viewer sees their own true state. "" when unannotated.
	Presence string `json:"presence,omitempty"`
	// TimeoutUntil is set while the member is timed out (muted); nil/past = not muted.
	TimeoutUntil *time.Time `json:"timeoutUntil,omitempty"`
	// Profile (v0.5): a longer "About Me" bio + short pronouns, shown on the profile
	// card. "" = none. React-escaped on render.
	About    string `json:"about,omitempty"`
	Pronouns string `json:"pronouns,omitempty"`
	// Color (v0.7) is the member's top custom-role color (#RGB/#RRGGBB), used to tint their
	// name. "" = no colored role assigned.
	Color string `json:"color,omitempty"`
	// RoleIds (v0.7) are the cosmetic role ids this member holds in the server (highest
	// position first), so the client can show per-member assignment state. Empty = none.
	RoleIds []int64 `json:"roleIds,omitempty"`
}

// Role is a custom, cosmetic, server-scoped colored role (v0.7) — distinct from the
// owner/admin/member permission tier. A member's display color is their highest-position
// assigned role's color.
type Role struct {
	ID       int64  `json:"id"`
	ServerID int64  `json:"serverId"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
	// Hoist (slice 3): show this role as its own section in the member list (Discord's
	// "Display role members separately"). Default false.
	Hoist bool `json:"hoist"`
}

// ValidChannelName reports whether name is a valid channel slug (2-32 [a-z0-9_-]).
func ValidChannelName(name string) bool { return channelNameRe.MatchString(name) }

type Message struct {
	ID        int64             `json:"id"`
	ChannelID int64             `json:"channelId"`
	UserID    int64             `json:"userId"`
	Username  string            `json:"username"`
	Body      string            `json:"body"`
	CreatedAt time.Time         `json:"createdAt"`
	EditedAt  *time.Time        `json:"editedAt,omitempty"`
	Deleted   bool              `json:"deleted,omitempty"`
	Pinned    bool              `json:"pinned,omitempty"`
	Reactions []ReactionSummary `json:"reactions,omitempty"`
	// AuthorColor (v0.7) is the author's top custom-role color in the channel's server
	// (#RGB/#RRGGBB), tinting their name. "" for DM/global channels or an uncolored author.
	AuthorColor string `json:"authorColor,omitempty"`
	// ThreadID/ThreadName (v0.8 slice 3) are the thread started FROM this message, if any, so
	// the client can show a clickable thread reference. nil/"" when the message has no thread.
	ThreadID   *int64 `json:"threadId,omitempty"`
	ThreadName string `json:"threadName,omitempty"`
	// Reply reference (Discord-style). ReplyTo is the referenced message id; the
	// author + body snippet are denormalized so history and live broadcasts render
	// the quoted preview without an extra round-trip. All three are unset when the
	// message isn't a reply.
	ReplyTo       *int64 `json:"replyTo,omitempty"`
	ReplyToAuthor string `json:"replyToAuthor,omitempty"`
	ReplyToBody   string `json:"replyToBody,omitempty"`
	// Attachments (v0.4): files/images carried by the message, served access-gated
	// from /api/attachments/{id}. Unset when the message has none.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment is a file/image on a message as seen by a client. URL is the
// access-gated serve endpoint (the client fetches it with its bearer token).
type Attachment struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

// NewAttachment is an already-stored file (bytes written to disk under StorageKey)
// awaiting its DB row, passed to SaveWithAttachments. Filename/ContentType are
// metadata only; StorageKey is the opaque on-disk name (no client input).
type NewAttachment struct {
	StorageKey  string
	Filename    string
	ContentType string
	Size        int64
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
	// CategoryID groups the channel under a server category; nil = uncategorized.
	CategoryID *int64 `json:"categoryId,omitempty"`
	// Kind distinguishes a thread ('thread') from a regular channel; "" for non-threads on
	// the wire (omitempty). ParentID is the parent channel a thread hangs off; nil otherwise.
	Kind     string `json:"kind,omitempty"`
	ParentID *int64 `json:"parentId,omitempty"`
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
	// Enforce an active timeout (temporary mute) — server-side, can't be bypassed.
	if blocked, err := s.timeoutBlocked(ctx, channelID, userID); err != nil {
		return Message{}, err
	} else if blocked {
		return Message{}, ErrTimedOut
	}
	m := Message{ChannelID: channelID, UserID: userID, Username: username, Body: body}
	if err := s.resolveReply(ctx, s.pool, channelID, &m, &replyTo); err != nil {
		return Message{}, err
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO messages (channel_id, user_id, body, reply_to) VALUES ($1, $2, $3, $4)
		   RETURNING id, created_at`,
		channelID, userID, body, replyTo,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	m.AuthorColor = s.authorColor(ctx, channelID, userID)
	return m, nil
}

// rowQuerier is the subset of *pgxpool.Pool / pgx.Tx that resolveReply needs, so
// the reply lookup can run either on the pool or inside a transaction.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// resolveReply validates + denormalizes m's reply reference against channelID using
// q. A reference that doesn't resolve to a message in THIS channel is dropped
// (*replyTo set to nil, m's reply fields left empty) so the INSERT stores no link
// and the preview stays empty (Rule B/C: a client can't leak a message it can't see
// through a reply preview). A valid one fills m.ReplyTo/Author/Body.
func (s *Store) resolveReply(ctx context.Context, q rowQuerier, channelID int64, m *Message, replyTo **int64) error {
	if *replyTo == nil {
		return nil
	}
	var refChannel int64
	var refAuthor, refBody string
	var refDeleted *time.Time
	err := q.QueryRow(ctx,
		`SELECT m.channel_id, u.username, m.body, m.deleted_at
		   FROM messages m JOIN users u ON u.id = m.user_id WHERE m.id = $1`, **replyTo).
		Scan(&refChannel, &refAuthor, &refBody, &refDeleted)
	switch {
	case errors.Is(err, pgx.ErrNoRows) || (err == nil && refChannel != channelID):
		*replyTo = nil // bogus or cross-channel — drop the reference
	case err != nil:
		return err
	default:
		m.ReplyTo = *replyTo
		m.ReplyToAuthor = refAuthor
		m.ReplyToBody = replySnippet(refBody, refDeleted)
	}
	return nil
}

// attachmentURL is the access-gated serve path for an attachment id (the client
// fetches it with its bearer token).
func attachmentURL(id int64) string {
	return "/api/attachments/" + strconv.FormatInt(id, 10)
}

// SaveWithAttachments creates a message (optionally a reply) carrying one or more
// already-stored attachments, atomically (message + attachment rows in one tx), and
// returns it fully populated. Authorization mirrors SaveReply: a read-only channel
// or active slowmode blocks the post (ErrForbidden / ErrSlowMode) and nothing is
// written. The caller has already written each file's bytes to disk under its
// StorageKey and must clean them up if this returns an error.
func (s *Store) SaveWithAttachments(ctx context.Context, channelID, userID int64, username, body string, replyTo *int64, atts []NewAttachment) (Message, error) {
	if ok, err := s.CanPostInChannel(ctx, channelID, userID); err != nil {
		return Message{}, err
	} else if !ok {
		return Message{}, ErrForbidden
	}
	if blocked, err := s.slowmodeBlocked(ctx, channelID, userID); err != nil {
		return Message{}, err
	} else if blocked {
		return Message{}, ErrSlowMode
	}
	if blocked, err := s.timeoutBlocked(ctx, channelID, userID); err != nil {
		return Message{}, err
	} else if blocked {
		return Message{}, ErrTimedOut
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback(ctx)

	m := Message{ChannelID: channelID, UserID: userID, Username: username, Body: body}
	if err := s.resolveReply(ctx, tx, channelID, &m, &replyTo); err != nil {
		return Message{}, err
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO messages (channel_id, user_id, body, reply_to) VALUES ($1, $2, $3, $4)
		   RETURNING id, created_at`,
		channelID, userID, body, replyTo,
	).Scan(&m.ID, &m.CreatedAt); err != nil {
		return Message{}, err
	}
	for _, a := range atts {
		att := Attachment{Filename: a.Filename, ContentType: a.ContentType, Size: a.Size}
		if err := tx.QueryRow(ctx,
			`INSERT INTO attachments (message_id, storage_key, filename, content_type, size)
			   VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			m.ID, a.StorageKey, a.Filename, a.ContentType, a.Size,
		).Scan(&att.ID); err != nil {
			return Message{}, err
		}
		att.URL = attachmentURL(att.ID)
		m.Attachments = append(m.Attachments, att)
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, err
	}
	m.AuthorColor = s.authorColor(ctx, channelID, userID)
	return m, nil
}

// authorColor returns userID's top custom-role color (#RGB/#RRGGBB) in the server that owns
// channelID, or "" when the channel has no server (DM/global) or the author has no colored
// role. Best-effort (a lookup error yields "" — coloring is cosmetic, never a hard failure).
func (s *Store) authorColor(ctx context.Context, channelID, userID int64) string {
	var c string
	_ = s.pool.QueryRow(ctx,
		`SELECT COALESCE((SELECT sr.color FROM member_roles mr
		          JOIN server_roles sr ON sr.id = mr.role_id
		          JOIN channels ch ON ch.id = $1
		         WHERE mr.user_id = $2 AND sr.server_id = ch.server_id
		         ORDER BY sr.position DESC, sr.id DESC LIMIT 1), '')`, channelID, userID).Scan(&c)
	return c
}

// authorColorSQL is the correlated-subquery column (aliased author_color) that read paths
// add to their message SELECT to tint each author by their top role in the channel's server.
// It assumes the message table is aliased `m` (columns m.channel_id, m.user_id).
const authorColorSQL = `COALESCE((SELECT sr.color FROM member_roles mr
	          JOIN server_roles sr ON sr.id = mr.role_id
	          JOIN channels ch ON ch.id = m.channel_id
	         WHERE mr.user_id = m.user_id AND sr.server_id = ch.server_id
	         ORDER BY sr.position DESC, sr.id DESC LIMIT 1), '')`

// AttachmentsForMessages returns attachments keyed by message id for the given ids
// (oldest row first), so history can render each message's files.
func (s *Store) AttachmentsForMessages(ctx context.Context, messageIDs []int64) (map[int64][]Attachment, error) {
	out := make(map[int64][]Attachment)
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, message_id, filename, content_type, size
		   FROM attachments WHERE message_id = ANY($1) ORDER BY id`, messageIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var a Attachment
		if err := rows.Scan(&a.ID, &mid, &a.Filename, &a.ContentType, &a.Size); err != nil {
			return nil, err
		}
		a.URL = attachmentURL(a.ID)
		out[mid] = append(out[mid], a)
	}
	return out, rows.Err()
}

// ErrNoAvatar is returned when a user hasn't uploaded an avatar.
var ErrNoAvatar = errors.New("no avatar")

// SetAvatar sets userID's avatar (opaque storage key + sniffed image type) and
// returns the PREVIOUS key (empty if none) so the caller can delete the old file on
// replace. ErrUserNotFound if the user is gone.
func (s *Store) SetAvatar(ctx context.Context, userID int64, key, contentType string) (string, error) {
	var old *string
	err := s.pool.QueryRow(ctx,
		`WITH prev AS (SELECT avatar_key FROM users WHERE id = $1)
		   UPDATE users SET avatar_key = $2, avatar_type = $3 WHERE id = $1
		   RETURNING (SELECT avatar_key FROM prev)`,
		userID, key, contentType).Scan(&old)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", err
	}
	if old != nil {
		return *old, nil
	}
	return "", nil
}

// AvatarForServe returns userID's avatar on-disk key + sniffed content type, or
// ErrNoAvatar if they haven't set one (the client then renders initials).
// ErrUserNotFound if the user doesn't exist.
func (s *Store) AvatarForServe(ctx context.Context, userID int64) (key, contentType string, err error) {
	var k, ct *string
	err = s.pool.QueryRow(ctx,
		`SELECT avatar_key, avatar_type FROM users WHERE id = $1`, userID).Scan(&k, &ct)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrUserNotFound
	}
	if err != nil {
		return "", "", err
	}
	if k == nil || *k == "" {
		return "", "", ErrNoAvatar
	}
	if ct != nil {
		contentType = *ct
	}
	return *k, contentType, nil
}

// AttachmentForServe returns an attachment's on-disk storage key + display metadata
// + the channel of the message it belongs to, for the access-gated serve handler.
// ErrMessageNotFound when the attachment (or its message) doesn't exist.
func (s *Store) AttachmentForServe(ctx context.Context, id int64) (storageKey, contentType, filename string, channelID int64, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT a.storage_key, a.content_type, a.filename, m.channel_id
		   FROM attachments a JOIN messages m ON m.id = a.message_id
		  WHERE a.id = $1`, id).Scan(&storageKey, &contentType, &filename, &channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", 0, ErrMessageNotFound
	}
	return storageKey, contentType, filename, channelID, err
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

// CanPostInChannel reports whether userID may post in channelID. False for a voice
// channel (kind='voice') — it's voice-only, so NOBODY posts text (Rule B/15: the UI
// hides the composer, but a hostile client could POST via the raw WS 'message' frame or
// the REST attachment path; this is the gate that actually rejects it). Otherwise true
// unless the channel's policy is 'admins' AND it's a server channel AND the user isn't a
// server admin (read-only / announcement channel).
func (s *Store) CanPostInChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	var policy, kind string
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT post_policy, kind, server_id FROM channels WHERE id = $1`, channelID).Scan(&policy, &kind, &serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if kind == "voice" {
		return false, nil // a voice channel takes no text posts
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

// timeoutBlocked reports whether userID is currently timed out (muted) in the server
// that channelID belongs to. False for non-server channels (global/DM have no
// timeouts) and when the member's timeout_until is NULL or in the past. The cutoff is
// evaluated server-side (now()) so a client can't fake its clock to bypass it (Rule B).
func (s *Store) timeoutBlocked(ctx context.Context, channelID, userID int64) (bool, error) {
	var blocked bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM server_members sm
		     JOIN channels c ON c.server_id = sm.server_id
		    WHERE c.id = $1 AND sm.user_id = $2
		      AND sm.timeout_until IS NOT NULL AND sm.timeout_until > now())`,
		channelID, userID).Scan(&blocked)
	return blocked, err
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
	if err != nil {
		return Message{}, err
	}
	m.AuthorColor = s.authorColor(ctx, m.ChannelID, userID)
	return m, nil
}

// ErrInvalidEmoji is returned when a reaction emoji is empty or too long.
var ErrInvalidEmoji = errors.New("invalid emoji")

// validEmoji accepts either a unicode emoji (1..16 bytes, the original rule) or a
// custom-emoji marker `custom:{id}` where {id} is a numeric server_emoji id (the
// marker is stored verbatim in reactions.emoji; the client renders it as the image
// at /api/emoji/{id}). The overall length stays bounded ≤24 (Rule B) and the marker
// format is validated server-side (rejects custom:abc, custom:, oversized). The id's
// existence is deliberately NOT checked here — a deleted emoji just renders as a
// graceful broken/empty image, which is acceptable.
func validEmoji(e string) bool {
	if len(e) < 1 || len(e) > 24 {
		return false
	}
	if rest, ok := strings.CutPrefix(e, "custom:"); ok {
		if len(rest) < 1 || len(rest) > 16 {
			return false
		}
		_, err := strconv.ParseInt(rest, 10, 64)
		return err == nil
	}
	return len(e) <= 16
}

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

// MarkChannelRead advances userID's read marker in channelID to the channel's latest
// message. Idempotent (upsert). The caller is responsible for the access check.
func (s *Store) MarkChannelRead(ctx context.Context, channelID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_reads (user_id, channel_id, last_read_id)
		 VALUES ($1, $2, COALESCE((SELECT MAX(id) FROM messages WHERE channel_id = $2), 0))
		 ON CONFLICT (user_id, channel_id)
		 DO UPDATE SET last_read_id = EXCLUDED.last_read_id, updated_at = now()`,
		userID, channelID)
	return err
}

// MuteChannel mutes channelID for userID — it stops surfacing as unread (Unreads excludes
// it). Idempotent. The caller is responsible for the access check (you can only mute a
// channel you can read).
func (s *Store) MuteChannel(ctx context.Context, channelID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_mutes (user_id, channel_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, channel_id) DO NOTHING`, userID, channelID)
	return err
}

// UnmuteChannel removes userID's mute on channelID. Idempotent (a no-op if not muted).
func (s *Store) UnmuteChannel(ctx context.Context, channelID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM channel_mutes WHERE user_id = $1 AND channel_id = $2`, userID, channelID)
	return err
}

// MutedChannelIDs returns the ids of every channel userID has muted, so the client can
// render the muted state (a 🔕 dim + the right header toggle).
func (s *Store) MutedChannelIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT channel_id FROM channel_mutes WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ChannelUnread is one channel's unread state for a user: the channel is unread (it's
// only returned when it has unread messages) and Mentions counts the unread messages
// that @-mention the user (incl. @everyone/@here) — drives the red mention badge.
type ChannelUnread struct {
	ChannelID int64 `json:"id"`
	Mentions  int   `json:"mentions"`
}

// Unreads returns, for each channel userID can access that holds a non-deleted message
// newer than their read marker authored by someone else (your own sends never
// self-unread), the channel id and how many of those unread messages mention the user.
// Access is scoped exactly like CanAccessChannel (public-global OR server-member OR
// dm-member), so unread never leaks a channel you can't see.
//
// Mention match (mirrors the client's `@([A-Za-z0-9_]{2,32})` highlight, case-insensitive,
// with @everyone/@here): `@(username|everyone|here)` followed by a non-word char or end —
// the trailing boundary stops `@alice` from matching `@alice2`. Usernames are validated
// `[a-zA-Z0-9_]{3,32}` so the pattern has no regex metachars, and it's passed as a bind
// parameter (no SQL injection) — Rule B, double safety.
func (s *Store) Unreads(ctx context.Context, userID int64, username string) ([]ChannelUnread, error) {
	pattern := `@(` + username + `|everyone|here)([^a-z0-9_]|$)`
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, COUNT(*) FILTER (WHERE msg.body ~* $2) AS mentions
		   FROM channels c
		   JOIN messages msg ON msg.channel_id = c.id
		    AND msg.deleted_at IS NULL
		    AND msg.user_id <> $1
		    AND msg.id > COALESCE(
		          (SELECT last_read_id FROM channel_reads cr
		            WHERE cr.channel_id = c.id AND cr.user_id = $1), 0)
		  WHERE c.kind <> 'thread' AND (
		          (c.kind <> 'dm' AND c.server_id IS NULL)
		          OR (c.server_id IS NOT NULL AND EXISTS (
		                SELECT 1 FROM server_members sm
		                 WHERE sm.server_id = c.server_id AND sm.user_id = $1))
		          OR (c.kind = 'dm' AND EXISTS (
		                SELECT 1 FROM channel_members m
		                 WHERE m.channel_id = c.id AND m.user_id = $1))
		        )
		    -- A muted channel never surfaces as unread (sidebar dot, mention badge, tab badge).
		    AND NOT EXISTS (
		          SELECT 1 FROM channel_mutes cm
		           WHERE cm.channel_id = c.id AND cm.user_id = $1)
		  GROUP BY c.id`, userID, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChannelUnread, 0)
	for rows.Next() {
		var u ChannelUnread
		if err := rows.Scan(&u.ChannelID, &u.Mentions); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CanAccessChannel reports whether userID may read/join channelID. Public
// channels are open to everyone; DM (and future private) channels require
// membership. A non-existent channel returns (false, nil).
//
// For DM channels there is an ADDITIONAL gate (v0.5 user blocking, slice 1): in a 1:1
// DM a member is denied if they are in a block relationship with the OTHER member —
// symmetric, so it holds whichever side did the blocking. This makes block enforcement
// flow through every read/send/react path that already routes through this gate, while
// leaving server/global channel access completely unchanged. The block-deny is gated on
// a 2-member channel (v0.6 group DMs): a group member is never denied the whole channel
// just because one co-member is blocked — their messages are hidden at render instead.
func (s *Store) CanAccessChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT CASE
		          WHEN c.kind = 'dm' THEN
		            EXISTS (SELECT 1 FROM channel_members m
		                     WHERE m.channel_id = c.id AND m.user_id = $2)
		            AND NOT (
		                  (SELECT COUNT(*) FROM channel_members cm WHERE cm.channel_id = c.id) = 2
		                  AND EXISTS (
		                    SELECT 1 FROM channel_members other
		                      JOIN user_blocks b
		                        ON (b.blocker_id = $2 AND b.blocked_id = other.user_id)
		                        OR (b.blocker_id = other.user_id AND b.blocked_id = $2)
		                     WHERE other.channel_id = c.id AND other.user_id <> $2))
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
	// A block (either direction) forbids opening/reopening a DM (v0.5, symmetric).
	if blocked, err := s.IsBlocked(ctx, a, b); err != nil {
		return DMChannel{}, err
	} else if blocked {
		return DMChannel{}, ErrBlocked
	}

	// Existing DM with exactly {a, b}?
	var dm DMChannel
	dm.User = other
	dm.Users = []DMUser{other}
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

// CreateGroupDM creates a NEW group direct-message channel (kind='dm') whose members are
// the creator plus otherIDs. Like Discord, group DMs are NOT deduplicated — each call makes
// a fresh channel. otherIDs is deduped and the creator is dropped from it; the resulting
// distinct-other count must be 1..9 (total 2..10 — ErrGroupTooLarge above that, an
// ErrCannotDMSelf-style guard at zero). Exactly one other delegates to CreateOrGetDM (the
// idempotent 1:1). The creator must not be in a block relationship with ANY member (ErrBlocked,
// symmetric — you can't form a group with someone you've blocked / who blocked you). Returns
// the channel as seen by the creator (Users = the others username-sorted, User = Users[0]).
func (s *Store) CreateGroupDM(ctx context.Context, creator int64, otherIDs []int64) (DMChannel, error) {
	// Dedupe and drop the creator's own id.
	seen := make(map[int64]struct{}, len(otherIDs))
	others := make([]int64, 0, len(otherIDs))
	for _, id := range otherIDs {
		if id == creator {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		others = append(others, id)
	}
	if len(others) == 0 {
		return DMChannel{}, ErrCannotDMSelf
	}
	if len(others) > 9 {
		return DMChannel{}, ErrGroupTooLarge
	}
	if len(others) == 1 {
		return s.CreateOrGetDM(ctx, creator, others[0])
	}

	// Resolve every member (ErrUserNotFound if any is gone) and verify no block
	// relationship with the creator (symmetric) BEFORE creating anything.
	members := make([]DMUser, 0, len(others))
	for _, id := range others {
		u, err := s.LookupUserByID(ctx, id)
		if err != nil {
			return DMChannel{}, err
		}
		if blocked, err := s.IsBlocked(ctx, creator, id); err != nil {
			return DMChannel{}, err
		} else if blocked {
			return DMChannel{}, ErrBlocked
		}
		members = append(members, u)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Username < members[j].Username })

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DMChannel{}, err
	}
	defer tx.Rollback(ctx)

	var dm DMChannel
	if err := tx.QueryRow(ctx,
		`INSERT INTO channels (kind) VALUES ('dm') RETURNING id, created_at`).
		Scan(&dm.ID, &dm.CreatedAt); err != nil {
		return DMChannel{}, err
	}
	// One multi-row insert: the creator + every other member. $1 is the channel id;
	// each member id is its own placeholder (mirrors the 1:1 VALUES ($1,$2),($1,$3) style).
	ids := append([]int64{creator}, others...)
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, dm.ID)
	values := make([]string, 0, len(ids))
	for i, id := range ids {
		values = append(values, "($1, $"+strconv.Itoa(i+2)+")")
		args = append(args, id)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO channel_members (channel_id, user_id) VALUES `+strings.Join(values, ", "),
		args...); err != nil {
		return DMChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DMChannel{}, err
	}
	dm.Users = members
	dm.User = members[0]
	return dm, nil
}

// LeaveGroupDM removes userID from a GROUP DM (kind='dm' with ≥3 members). A 1:1 DM
// can't be "left" (that's a future "close DM") → ErrNotGroupDM; a non-DM channel →
// ErrNotGroupDM; a non-member → ErrUserNotFound (no existence leak, Rule B); an unknown
// channel → ErrChannelNotFound. Messages are preserved (history intact, like a server
// leave) and the group stays a group for the rest even if it drops to 2 members.
func (s *Store) LeaveGroupDM(ctx context.Context, channelID, userID int64) error {
	var kind string
	var memberCount int
	var isMember bool
	err := s.pool.QueryRow(ctx, `
		SELECT c.kind,
		       (SELECT COUNT(*) FROM channel_members WHERE channel_id = c.id),
		       EXISTS(SELECT 1 FROM channel_members WHERE channel_id = c.id AND user_id = $2)
		  FROM channels c WHERE c.id = $1`,
		channelID, userID).Scan(&kind, &memberCount, &isMember)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChannelNotFound
	}
	if err != nil {
		return err
	}
	if !isMember {
		return ErrUserNotFound // not in this channel (checked first so a non-member never leaks its kind/size)
	}
	if kind != "dm" || memberCount < 3 {
		return ErrNotGroupDM // a 1:1 DM, or a non-DM channel — only groups can be left
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM channel_members WHERE channel_id = $1 AND user_id = $2`, channelID, userID)
	return err
}

// AddGroupDMMember adds targetID to a GROUP DM (kind='dm', ≥3 members). The actor must be a
// member (checked FIRST, so a non-member never leaks the channel's kind/size — Rule B); a 1:1
// DM or non-DM channel → ErrNotGroupDM (start a NEW group instead of adding to a 1:1); the
// 10-member cap → ErrGroupTooLarge; an existing member → ErrAlreadyMember; a block either way
// (symmetric) → ErrBlocked. The caller resolves the target identifier to targetID first. On
// success the target gains access immediately (a channel_members row); messages are untouched.
func (s *Store) AddGroupDMMember(ctx context.Context, channelID, actorID, targetID int64) error {
	var kind string
	var memberCount int
	var actorIsMember, targetIsMember bool
	err := s.pool.QueryRow(ctx, `
		SELECT c.kind,
		       (SELECT COUNT(*) FROM channel_members WHERE channel_id = c.id),
		       EXISTS(SELECT 1 FROM channel_members WHERE channel_id = c.id AND user_id = $2),
		       EXISTS(SELECT 1 FROM channel_members WHERE channel_id = c.id AND user_id = $3)
		  FROM channels c WHERE c.id = $1`,
		channelID, actorID, targetID).Scan(&kind, &memberCount, &actorIsMember, &targetIsMember)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChannelNotFound
	}
	if err != nil {
		return err
	}
	if !actorIsMember {
		return ErrForbidden // only a member can add to the group (checked first, no leak)
	}
	if kind != "dm" || memberCount < 3 {
		return ErrNotGroupDM // a 1:1 or non-DM channel — can't add (start a new group instead)
	}
	if memberCount >= 10 {
		return ErrGroupTooLarge
	}
	if targetIsMember {
		return ErrAlreadyMember
	}
	if blocked, err := s.IsBlocked(ctx, actorID, targetID); err != nil {
		return err
	} else if blocked {
		return ErrBlocked
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO channel_members (channel_id, user_id) VALUES ($1, $2)`, channelID, targetID)
	return err
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

// ListDMs returns userID's direct-message channels, each with its other member(s): one
// entry in Users for a 1:1 DM, two or more for a group DM (username-sorted; User mirrors
// Users[0]). A member in a block relationship with the caller (either direction) is
// filtered OUT of Users (v0.5 symmetric block); a channel left with no visible others —
// a 1:1 whose sole other is blocked — is dropped, so the sidebar never shows an
// un-openable DM. A group is never hidden for one blocked co-member (v0.6 group DMs):
// the blocked member is simply omitted from Users.
func (s *Store) ListDMs(ctx context.Context, userID int64) ([]DMChannel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.created_at, u.id, u.username
		   FROM channels c
		   JOIN channel_members me    ON me.channel_id = c.id AND me.user_id = $1
		   JOIN channel_members other ON other.channel_id = c.id AND other.user_id <> $1
		   JOIN users u ON u.id = other.user_id
		  WHERE c.kind = 'dm'
		    AND NOT EXISTS (
		          SELECT 1 FROM user_blocks b
		           WHERE (b.blocker_id = $1 AND b.blocked_id = other.user_id)
		              OR (b.blocker_id = other.user_id AND b.blocked_id = $1))
		  ORDER BY c.id, u.username`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dms := make([]DMChannel, 0)
	byID := make(map[int64]int) // channel id -> index into dms (first sighting wins ordering)
	for rows.Next() {
		var cid int64
		var created time.Time
		var ou DMUser
		if err := rows.Scan(&cid, &created, &ou.ID, &ou.Username); err != nil {
			return nil, err
		}
		if idx, ok := byID[cid]; ok {
			dms[idx].Users = append(dms[idx].Users, ou)
			continue
		}
		byID[cid] = len(dms)
		dms = append(dms, DMChannel{ID: cid, CreatedAt: created, User: ou, Users: []DMUser{ou}})
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

// TransferServerOwnership hands serverID's ownership from the current owner (actorID) to
// another member (targetID). Only the owner may transfer (ErrForbidden otherwise); the
// target must be a different existing member (ErrForbidden if it's the actor, ErrUserNotFound
// if not a member). In one transaction the target is promoted to 'owner', the old owner is
// demoted to 'admin', and servers.owner_id is updated — so there's always exactly one owner
// and the change can't half-apply.
func (s *Store) TransferServerOwnership(ctx context.Context, serverID, actorID, targetID int64) error {
	if targetID == actorID {
		return ErrForbidden // already the owner; nothing to transfer
	}
	actorRole, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if actorRole != "owner" {
		return ErrForbidden // only the owner may transfer ownership
	}
	targetRole, err := s.ServerRole(ctx, serverID, targetID)
	if err != nil {
		return err
	}
	if targetRole == "" {
		return ErrUserNotFound // target isn't a member of this server
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`UPDATE server_members SET role = 'owner' WHERE server_id = $1 AND user_id = $2`,
		serverID, targetID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE server_members SET role = 'admin' WHERE server_id = $1 AND user_id = $2`,
		serverID, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE servers SET owner_id = $2 WHERE id = $1`, serverID, targetID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveServerMember kicks targetID out of serverID. The actor must be an owner or
// admin of the server; nobody can kick the owner; the actor can't kick themselves;
// and an admin can't kick a fellow admin (only the owner can). Returns ErrForbidden
// for any of those, ErrUserNotFound if the target isn't a member. The target's
// authored messages are left intact (Discord keeps history). The caller is
// responsible for evicting the kicked user's live sockets (see Hub.EvictUserFromChannels).
func (s *Store) RemoveServerMember(ctx context.Context, serverID, actorID, targetID int64) error {
	if targetID == actorID {
		return ErrForbidden // can't kick yourself (leaving is a separate action)
	}
	actorRole, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if actorRole != "owner" && actorRole != "admin" {
		return ErrForbidden // non-members and plain members can't kick
	}
	targetRole, err := s.ServerRole(ctx, serverID, targetID)
	if err != nil {
		return err
	}
	if targetRole == "" {
		return ErrUserNotFound // target isn't a member of this server
	}
	if targetRole == "owner" {
		return ErrForbidden // nobody can kick the owner
	}
	if actorRole == "admin" && targetRole == "admin" {
		return ErrForbidden // admins can't kick fellow admins — only the owner can
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2 AND role <> 'owner'`,
		serverID, targetID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound // raced away or was the owner (defense-in-depth)
	}
	return nil
}

// LeaveServer removes userID's own membership of serverID — the voluntary counterpart
// to a kick (self-removal, which RemoveServerMember explicitly forbids). The OWNER can't
// leave (Discord rule: they must transfer ownership or delete the server first) →
// ErrForbidden. A non-member (or unknown server) → ErrServerNotFound. The user keeps their
// authored messages (history is preserved, like a kick). The caller evicts the leaver's
// own live sockets from the server's channels (see Hub.EvictUserFromChannels), so they
// stop receiving immediately.
func (s *Store) LeaveServer(ctx context.Context, serverID, userID int64) error {
	role, err := s.ServerRole(ctx, serverID, userID)
	if err != nil {
		return err
	}
	if role == "" {
		return ErrServerNotFound // not a member (don't leak whether the server exists)
	}
	if role == "owner" {
		return ErrForbidden // the owner can't leave — transfer or delete instead
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2 AND role <> 'owner'`,
		serverID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrServerNotFound // raced away between the role check and the delete
	}
	return nil
}

// maxBanReasonLen bounds a ban reason (Rule B). Discord allows up to 512.
const maxBanReasonLen = 512

// ServerBan is one banned user as shown to an admin in the bans list.
type ServerBan struct {
	UserID   int64     `json:"userId"`
	Username string    `json:"username"`
	Reason   string    `json:"reason,omitempty"`
	BannedAt time.Time `json:"bannedAt"`
}

// BanServerMember bans targetID from serverID: it removes their membership (like a kick)
// AND records a ban so they can't rejoin via an invite until unbanned. The authz mirrors
// RemoveServerMember exactly (owner/admin only; can't ban yourself, the owner, and an
// admin can't ban a fellow admin); the target must currently be a member (ErrUserNotFound
// otherwise). Removal + ban are one transaction so a user is never left half-banned. The
// reason is trimmed and bounded. The caller evicts the banned user's live sockets (see
// Hub.EvictUserFromChannels), identical to a kick.
func (s *Store) BanServerMember(ctx context.Context, serverID, actorID, targetID int64, reason string) error {
	if targetID == actorID {
		return ErrForbidden // can't ban yourself
	}
	actorRole, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if actorRole != "owner" && actorRole != "admin" {
		return ErrForbidden // non-members and plain members can't ban
	}
	targetRole, err := s.ServerRole(ctx, serverID, targetID)
	if err != nil {
		return err
	}
	if targetRole == "" {
		return ErrUserNotFound // target isn't a member of this server
	}
	if targetRole == "owner" {
		return ErrForbidden // nobody can ban the owner
	}
	if actorRole == "admin" && targetRole == "admin" {
		return ErrForbidden // admins can't ban fellow admins — only the owner can
	}
	reason = strings.TrimSpace(reason)
	if r := []rune(reason); len(r) > maxBanReasonLen {
		reason = string(r[:maxBanReasonLen])
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx,
		`DELETE FROM server_members WHERE server_id = $1 AND user_id = $2 AND role <> 'owner'`,
		serverID, targetID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound // raced away or was the owner (defense-in-depth)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO server_bans (server_id, user_id, banned_by, reason) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (server_id, user_id) DO UPDATE SET banned_by = EXCLUDED.banned_by, reason = EXCLUDED.reason, created_at = now()`,
		serverID, targetID, actorID, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UnbanServerMember lifts targetID's ban from serverID. Owner/admin only; returns
// ErrUserNotFound if the target wasn't banned. After this they may rejoin via an invite.
func (s *Store) UnbanServerMember(ctx context.Context, serverID, actorID, targetID int64) error {
	ok, err := s.IsServerAdmin(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden // non-admins can't unban
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM server_bans WHERE server_id = $1 AND user_id = $2`, serverID, targetID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound // wasn't banned
	}
	return nil
}

// IsServerBanned reports whether userID is banned from serverID.
func (s *Store) IsServerBanned(ctx context.Context, serverID, userID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM server_bans WHERE server_id = $1 AND user_id = $2)`,
		serverID, userID).Scan(&ok)
	return ok, err
}

// ListServerBans returns a server's banned users, newest ban first. Admin view; the
// handler gates on IsServerAdmin before calling.
func (s *Store) ListServerBans(ctx context.Context, serverID int64) ([]ServerBan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT b.user_id, u.username, b.reason, b.created_at
		   FROM server_bans b JOIN users u ON u.id = b.user_id
		  WHERE b.server_id = $1
		  ORDER BY b.created_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ServerBan, 0)
	for rows.Next() {
		var b ServerBan
		if err := rows.Scan(&b.UserID, &b.Username, &b.Reason, &b.BannedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BlockUser records that blockerID has blocked blockedID (v0.5, slice 1: DM-only
// enforcement). Idempotent — re-blocking is a no-op. You can't block yourself
// (ErrForbidden); the target must exist (ErrUserNotFound from the lookup). The block is
// directed in storage but enforced SYMMETRICALLY for DMs (see CanAccessChannel /
// CreateOrGetDM / ListDMs).
func (s *Store) BlockUser(ctx context.Context, blockerID, blockedID int64) error {
	if blockerID == blockedID {
		return ErrForbidden // can't block yourself
	}
	if _, err := s.LookupUserByID(ctx, blockedID); err != nil {
		return err // ErrUserNotFound for an unknown target
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1, $2)
		 ON CONFLICT (blocker_id, blocked_id) DO NOTHING`,
		blockerID, blockedID)
	return err
}

// UnblockUser lifts blockerID's block on blockedID. Returns ErrUserNotFound when there
// was no such block (the pair wasn't blocked / no such user) — mirrors the unban shape.
func (s *Store) UnblockUser(ctx context.Context, blockerID, blockedID int64) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM user_blocks WHERE blocker_id = $1 AND blocked_id = $2`,
		blockerID, blockedID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound // wasn't blocked
	}
	return nil
}

// IsBlocked reports whether users a and b are in a block relationship in EITHER
// direction (symmetric) — a blocked b OR b blocked a. This is the predicate behind DM
// enforcement.
func (s *Store) IsBlocked(ctx context.Context, a, b int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM user_blocks
		    WHERE (blocker_id = $1 AND blocked_id = $2)
		       OR (blocker_id = $2 AND blocked_id = $1))`,
		a, b).Scan(&ok)
	return ok, err
}

// ListBlocked returns the users that userID has blocked (the directed blocker→blocked
// view), newest block first, as a non-nil (possibly empty) slice.
func (s *Store) ListBlocked(ctx context.Context, userID int64) ([]DMUser, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.username
		   FROM user_blocks b JOIN users u ON u.id = b.blocked_id
		  WHERE b.blocker_id = $1
		  ORDER BY b.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DMUser, 0)
	for rows.Next() {
		var u DMUser
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// maxTimeoutDuration bounds how long a member can be timed out (Discord's max).
const maxTimeoutDuration = 28 * 24 * time.Hour

// TimeoutServerMember mutes targetID in serverID until `until`. The authz mirrors
// ban/kick exactly (owner/admin only; can't timeout yourself, the owner, and an admin
// can't timeout a fellow admin); the target must be a member (ErrUserNotFound otherwise).
// `until` is clamped server-side to (now, now+maxTimeoutDuration] so a client can't
// request a negative or absurdly long mute. Returns the effective until.
func (s *Store) TimeoutServerMember(ctx context.Context, serverID, actorID, targetID int64, until time.Time) (time.Time, error) {
	if targetID == actorID {
		return time.Time{}, ErrForbidden // can't timeout yourself
	}
	actorRole, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return time.Time{}, err
	}
	if actorRole != "owner" && actorRole != "admin" {
		return time.Time{}, ErrForbidden
	}
	targetRole, err := s.ServerRole(ctx, serverID, targetID)
	if err != nil {
		return time.Time{}, err
	}
	if targetRole == "" {
		return time.Time{}, ErrUserNotFound
	}
	if targetRole == "owner" {
		return time.Time{}, ErrForbidden // nobody can timeout the owner
	}
	if actorRole == "admin" && targetRole == "admin" {
		return time.Time{}, ErrForbidden // admins can't timeout fellow admins
	}
	now := time.Now()
	if !until.After(now) {
		return time.Time{}, ErrForbidden // a timeout must be in the future
	}
	if max := now.Add(maxTimeoutDuration); until.After(max) {
		until = max // clamp to the Discord-style ceiling
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE server_members SET timeout_until = $3
		   WHERE server_id = $1 AND user_id = $2 AND role <> 'owner'`,
		serverID, targetID, until)
	if err != nil {
		return time.Time{}, err
	}
	if ct.RowsAffected() == 0 {
		return time.Time{}, ErrUserNotFound // raced away or was the owner (defense-in-depth)
	}
	return until, nil
}

// ClearTimeout lifts targetID's timeout in serverID (sets it NULL). Owner/admin only;
// ErrUserNotFound if the target isn't a member.
func (s *Store) ClearTimeout(ctx context.Context, serverID, actorID, targetID int64) error {
	ok, err := s.IsServerAdmin(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE server_members SET timeout_until = NULL WHERE server_id = $1 AND user_id = $2`,
		serverID, targetID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrUserNotFound // not a member
	}
	return nil
}

// RenameServer renames serverID to name. The actor must be an owner or admin
// (Discord's "Manage Server"); a plain member or non-member gets ErrForbidden, an
// unknown server ErrServerNotFound. The name is assumed already trimmed/validated by
// the caller (1–64 chars, like CreateServer). Returns the updated row with the actor's
// role so the caller can echo it back.
func (s *Store) RenameServer(ctx context.Context, serverID, actorID int64, name string) (Server, error) {
	role, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return Server{}, err
	}
	if role == "" {
		// Don't leak existence to a non-member, but distinguish a real unknown server
		// so the caller can 404 rather than 403 when the server truly doesn't exist.
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1)`, serverID).Scan(&exists); err != nil {
			return Server{}, err
		}
		if !exists {
			return Server{}, ErrServerNotFound
		}
		return Server{}, ErrForbidden
	}
	if role != "owner" && role != "admin" {
		return Server{}, ErrForbidden
	}
	srv := Server{Role: role}
	err = s.pool.QueryRow(ctx,
		`UPDATE servers SET name = $1 WHERE id = $2 RETURNING id, name, owner_id, created_at`,
		name, serverID).Scan(&srv.ID, &srv.Name, &srv.OwnerID, &srv.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrServerNotFound
	}
	if err != nil {
		return Server{}, err
	}
	return srv, nil
}

// DeleteServer permanently deletes serverID. Only the server OWNER may delete it
// (destructive — an admin can't); a non-owner (incl. admin and non-member) gets
// ErrForbidden, an unknown server ErrServerNotFound. The delete is one transaction:
// the server's messages are removed first (messages.channel_id has no ON DELETE
// CASCADE, so the channel cascade would otherwise hit an FK violation), then the
// server row is deleted — its ON DELETE CASCADE FKs sweep server_members, channels
// (and their channel_reads/pins), server_invites, channel_categories, and server_bans.
// The global #general (server_id IS NULL) is never touched. The caller is responsible
// for notifying + evicting the (now ex-)members' live sockets (see Hub helpers); gather
// their ids and the channel ids BEFORE calling this, as the rows are gone afterwards.
func (s *Store) DeleteServer(ctx context.Context, serverID, actorID int64) error {
	role, err := s.ServerRole(ctx, serverID, actorID)
	if err != nil {
		return err
	}
	if role == "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1)`, serverID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrServerNotFound
		}
		return ErrForbidden
	}
	if role != "owner" {
		return ErrForbidden // only the owner deletes the server
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx,
		`DELETE FROM messages WHERE channel_id IN (SELECT id FROM channels WHERE server_id = $1)`,
		serverID); err != nil {
		return err
	}
	ct, err := tx.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrServerNotFound // raced away between the role check and the delete
	}
	return tx.Commit(ctx)
}

// ListServerMembers returns a server's members with roles, owner/admin first.
func (s *Store) ListServerMembers(ctx context.Context, serverID int64) ([]ServerMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.user_id, u.username, m.role, COALESCE(u.status, ''), COALESCE(u.status_emoji, ''),
		        COALESCE(u.presence_state, 'online'),
		        CASE WHEN m.timeout_until > now() THEN m.timeout_until END,
		        COALESCE(u.about, ''), COALESCE(u.pronouns, ''),
		        COALESCE((SELECT sr.color FROM member_roles mr
		                    JOIN server_roles sr ON sr.id = mr.role_id
		                   WHERE mr.user_id = m.user_id AND sr.server_id = m.server_id
		                   ORDER BY sr.position DESC, sr.id DESC LIMIT 1), ''),
		        COALESCE((SELECT array_agg(mr.role_id ORDER BY sr.position DESC, sr.id DESC)
		                    FROM member_roles mr JOIN server_roles sr ON sr.id = mr.role_id
		                   WHERE mr.user_id = m.user_id AND sr.server_id = m.server_id), '{}')
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
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role, &m.Status, &m.StatusEmoji, &m.PresenceState, &m.TimeoutUntil, &m.About, &m.Pronouns, &m.Color, &m.RoleIds); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Custom colored roles (v0.7) ---------------------------------------------------------

// validateRole normalizes + checks a custom role's name and color (Rule B). The trimmed
// name must be 1-32 chars; the color a #RGB/#RRGGBB hex.
func validateRole(name, color string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 32 {
		return "", "", ErrInvalidRoleName
	}
	if !roleColorRe.MatchString(color) {
		return "", "", ErrInvalidColor
	}
	return name, color, nil
}

// CreateServerRole creates a cosmetic role in serverID (admin-gated). The new role takes the
// next position (max+1) so it sits on top by default. Returns ErrForbidden if the actor isn't
// an admin/owner, ErrInvalidRoleName/ErrInvalidColor for bad input.
func (s *Store) CreateServerRole(ctx context.Context, serverID, actorID int64, name, color string, hoist bool) (Role, error) {
	if admin, err := s.IsServerAdmin(ctx, serverID, actorID); err != nil {
		return Role{}, err
	} else if !admin {
		return Role{}, ErrForbidden
	}
	name, color, err := validateRole(name, color)
	if err != nil {
		return Role{}, err
	}
	r := Role{ServerID: serverID, Name: name, Color: color, Hoist: hoist}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO server_roles (server_id, name, color, hoist, position)
		 VALUES ($1, $2, $3, $4, COALESCE((SELECT MAX(position) + 1 FROM server_roles WHERE server_id = $1), 0))
		 RETURNING id, position`, serverID, name, color, hoist).Scan(&r.ID, &r.Position)
	return r, err
}

// ListServerRoles returns serverID's cosmetic roles, highest position first.
func (s *Store) ListServerRoles(ctx context.Context, serverID int64) ([]Role, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, server_id, name, color, position, hoist FROM server_roles
		  WHERE server_id = $1 ORDER BY position DESC, id DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Role, 0)
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.ServerID, &r.Name, &r.Color, &r.Position, &r.Hoist); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateServerRole renames/recolors/re-hoists a role (admin-gated). The role must belong to
// serverID (ErrRoleNotFound otherwise).
func (s *Store) UpdateServerRole(ctx context.Context, serverID, actorID, roleID int64, name, color string, hoist bool) error {
	if admin, err := s.IsServerAdmin(ctx, serverID, actorID); err != nil {
		return err
	} else if !admin {
		return ErrForbidden
	}
	name, color, err := validateRole(name, color)
	if err != nil {
		return err
	}
	ct, err := s.pool.Exec(ctx,
		`UPDATE server_roles SET name = $3, color = $4, hoist = $5 WHERE id = $2 AND server_id = $1`,
		serverID, roleID, name, color, hoist)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

// DeleteServerRole removes a role (admin-gated); member_roles rows cascade away.
func (s *Store) DeleteServerRole(ctx context.Context, serverID, actorID, roleID int64) error {
	if admin, err := s.IsServerAdmin(ctx, serverID, actorID); err != nil {
		return err
	} else if !admin {
		return ErrForbidden
	}
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM server_roles WHERE id = $2 AND server_id = $1`, serverID, roleID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

// roleInServer reports whether roleID is a role of serverID.
func (s *Store) roleInServer(ctx context.Context, serverID, roleID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM server_roles WHERE id = $1 AND server_id = $2)`,
		roleID, serverID).Scan(&ok)
	return ok, err
}

// AssignServerRole gives targetID the role (admin-gated). The target must be a member of the
// server (ErrUserNotFound) and the role must belong to it (ErrRoleNotFound). Idempotent.
func (s *Store) AssignServerRole(ctx context.Context, serverID, actorID, targetID, roleID int64) error {
	if admin, err := s.IsServerAdmin(ctx, serverID, actorID); err != nil {
		return err
	} else if !admin {
		return ErrForbidden
	}
	if ok, err := s.roleInServer(ctx, serverID, roleID); err != nil {
		return err
	} else if !ok {
		return ErrRoleNotFound
	}
	if member, err := s.IsServerMember(ctx, serverID, targetID); err != nil {
		return err
	} else if !member {
		return ErrUserNotFound
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO member_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		targetID, roleID)
	return err
}

// UnassignServerRole removes a role from targetID (admin-gated). The role must belong to the
// server (ErrRoleNotFound); removing an unheld role is a no-op success.
func (s *Store) UnassignServerRole(ctx context.Context, serverID, actorID, targetID, roleID int64) error {
	if admin, err := s.IsServerAdmin(ctx, serverID, actorID); err != nil {
		return err
	} else if !admin {
		return ErrForbidden
	}
	if ok, err := s.roleInServer(ctx, serverID, roleID); err != nil {
		return err
	} else if !ok {
		return ErrRoleNotFound
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM member_roles WHERE user_id = $1 AND role_id = $2`, targetID, roleID)
	return err
}

// maxStatusLen bounds a user's custom status (Rule B).
const maxStatusLen = 128

// maxStatusEmojiLen bounds the status emoji (Rule B). 16 runes is generous enough
// for a ZWJ emoji sequence (e.g. a multi-person family) while staying tiny; the
// value is React-escaped on render, never validated as a "real" emoji.
const maxStatusEmojiLen = 16

// SetUserStatus sets userID's custom status line + optional status emoji: each is
// trimmed and capped (status to maxStatusLen, emoji to maxStatusEmojiLen runes);
// an empty/whitespace value clears that field (NULL). Only ever called for the
// JWT-derived caller (Rule C) — there is no target-user parameter.
func (s *Store) SetUserStatus(ctx context.Context, userID int64, status, emoji string) error {
	status = strings.TrimSpace(status)
	if r := []rune(status); len(r) > maxStatusLen {
		status = string(r[:maxStatusLen])
	}
	emoji = strings.TrimSpace(emoji)
	if r := []rune(emoji); len(r) > maxStatusEmojiLen {
		emoji = string(r[:maxStatusEmojiLen])
	}
	var sVal, eVal *string
	if status != "" {
		sVal = &status
	}
	if emoji != "" {
		eVal = &emoji
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET status = $2, status_emoji = $3 WHERE id = $1`, userID, sVal, eVal)
	return err
}

// maxAboutLen / maxPronounsLen bound the profile fields (Rule B). 190 matches Discord's
// About Me; pronouns stay short. Values are React-escaped on render, never trusted as markup.
const maxAboutLen = 190
const maxPronounsLen = 40

// SetUserProfile sets userID's About Me + pronouns: each is trimmed and rune-capped; an
// empty/whitespace value clears that field (NULL). JWT-derived caller only (Rule B/C) —
// there is no target-user parameter.
func (s *Store) SetUserProfile(ctx context.Context, userID int64, about, pronouns string) error {
	cap := func(v string, max int) *string {
		v = strings.TrimSpace(v)
		if r := []rune(v); len(r) > max {
			v = string(r[:max])
		}
		if v == "" {
			return nil
		}
		return &v
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET about = $2, pronouns = $3 WHERE id = $1`,
		userID, cap(about, maxAboutLen), cap(pronouns, maxPronounsLen))
	return err
}

// UserProfile is a user's PUBLIC profile, shown on the profile card (clicked from a
// message or the member list). Strictly public fields — no password hash, no internal
// data ever joins this. PresenceState is internal (the HTTP layer derives the effective
// Presence from it + the live-connection set, like the member list).
type UserProfile struct {
	UserID        int64  `json:"userId"`
	Username      string `json:"username"`
	About         string `json:"about,omitempty"`
	Pronouns      string `json:"pronouns,omitempty"`
	Status        string `json:"status,omitempty"`
	StatusEmoji   string `json:"statusEmoji,omitempty"`
	PresenceState string `json:"-"`
	Presence      string `json:"presence,omitempty"`
	Online        bool   `json:"online"`
}

// GetUserProfile returns userID's public profile, or ErrUserNotFound if no such user.
// Selects ONLY public columns (Rule 15 — a profile read can never leak the password hash).
func (s *Store) GetUserProfile(ctx context.Context, userID int64) (UserProfile, error) {
	var p UserProfile
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, COALESCE(about, ''), COALESCE(pronouns, ''),
		        COALESCE(status, ''), COALESCE(status_emoji, ''), COALESCE(presence_state, 'online')
		   FROM users WHERE id = $1`, userID).
		Scan(&p.UserID, &p.Username, &p.About, &p.Pronouns, &p.Status, &p.StatusEmoji, &p.PresenceState)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserProfile{}, ErrUserNotFound
	}
	return p, err
}

// NormalizePresence coerces a raw presence value to a known state, defaulting any
// empty/unknown input to "online" (so a NULL column or a hostile body is inert).
func NormalizePresence(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "idle":
		return "idle"
	case "dnd":
		return "dnd"
	case "invisible":
		return "invisible"
	default:
		return "online"
	}
}

// EffectivePresence maps a member's raw chosen state + live-connection flag to what
// OTHER viewers should see: a disconnected member, or one who chose "invisible",
// reads as "offline"; otherwise their chosen online|idle|dnd shows through. The
// viewer's OWN row is handled separately (they always see their true state).
func EffectivePresence(connected bool, raw string) (online bool, presence string) {
	raw = NormalizePresence(raw)
	if !connected || raw == "invisible" {
		return false, "offline"
	}
	return true, raw
}

// SetUserPresence sets userID's chosen presence state (online|idle|dnd|invisible),
// normalized so an unknown value falls back to "online". JWT-derived caller only
// (Rule C) — there is no target-user parameter.
func (s *Store) SetUserPresence(ctx context.Context, userID int64, state string) error {
	state = NormalizePresence(state)
	_, err := s.pool.Exec(ctx, `UPDATE users SET presence_state = $2 WHERE id = $1`, userID, state)
	return err
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

// CreateServerChannel creates a members-only channel under a server (uncategorized).
func (s *Store) CreateServerChannel(ctx context.Context, serverID int64, name string) (Channel, error) {
	return s.CreateServerChannelInCategory(ctx, serverID, name, nil)
}

// CreateServerChannelInCategory creates a members-only text channel under a server, optionally
// inside a category. A non-nil categoryID is validated to belong to serverID (Rule B — a
// client can't attach a channel to another server's category); a bad/cross-server id
// returns ErrCategoryNotFound and nothing is written.
func (s *Store) CreateServerChannelInCategory(ctx context.Context, serverID int64, name string, categoryID *int64) (Channel, error) {
	return s.CreateServerChannelOfKind(ctx, serverID, name, categoryID, "public")
}

// CreateServerChannelOfKind creates a members-only channel of the given kind under a server.
// kind is 'public' (a text channel, the default) or 'voice' (a Discord-style 🔊 channel that
// reuses the kind-agnostic per-channel voice infra); any other value returns
// ErrInvalidChannelKind and nothing is written (Rule B — defense in depth even though the route
// validates). A voice channel is just a channel row with kind='voice'; access + presence reuse
// the server membership + voiceMembers paths unchanged. The returned Channel.Kind is "" for a
// text channel (so the wire JSON stays byte-identical to a pre-voice channel) and "voice" for a
// voice channel.
func (s *Store) CreateServerChannelOfKind(ctx context.Context, serverID int64, name string, categoryID *int64, kind string) (Channel, error) {
	if kind == "" {
		kind = "public"
	}
	if kind != "public" && kind != "voice" {
		return Channel{}, ErrInvalidChannelKind
	}
	if categoryID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM channel_categories WHERE id = $1 AND server_id = $2)`,
			*categoryID, serverID).Scan(&ok); err != nil {
			return Channel{}, err
		}
		if !ok {
			return Channel{}, ErrCategoryNotFound
		}
	}
	c := Channel{Name: name, PostPolicy: "everyone", CategoryID: categoryID}
	if kind == "voice" {
		c.Kind = "voice"
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO channels (name, server_id, category_id, kind) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		name, serverID, categoryID, kind).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique within the server
			return Channel{}, ErrChannelExists
		}
		return Channel{}, err
	}
	return c, nil
}

// maxThreadNameLen bounds a thread's display name (Rule B). Threads allow spaces + caps
// (they're titles, not slugs), so they don't use ValidChannelName.
const maxThreadNameLen = 100

// CreateThread (v0.8) creates a thread (kind='thread') under parentID, copying the parent's
// server_id so the parent's access + post gates apply to the thread unchanged. The parent must
// exist and be a regular channel — NOT a DM and NOT itself a thread (no nesting). The name is
// trimmed + bounded 1..100. fromMessageID, when set, anchors the thread to a message: it is
// validated to be a non-deleted message IN the parent channel (Rule B/C — a client can't anchor
// to a message it can't see) and DROPPED (not honored) otherwise. The caller verifies the actor
// can access+post in the parent (the route does); a thread does NOT participate in name uniqueness.
func (s *Store) CreateThread(ctx context.Context, parentID int64, name string, fromMessageID *int64) (Channel, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxThreadNameLen {
		return Channel{}, ErrInvalidThreadName
	}
	var kind string
	var serverID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT kind, server_id FROM channels WHERE id = $1`, parentID).Scan(&kind, &serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, ErrChannelNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	if kind == "dm" || kind == "thread" || kind == "voice" {
		return Channel{}, ErrNotThreadable // no nesting, no DM threads, and a voice channel is voice-only
	}
	// Validate the anchor message lives (non-deleted) in THIS parent channel; drop it otherwise.
	if fromMessageID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM messages WHERE id = $1 AND channel_id = $2 AND deleted_at IS NULL)`,
			*fromMessageID, parentID).Scan(&ok); err != nil {
			return Channel{}, err
		}
		if !ok {
			fromMessageID = nil
		}
	}
	parent := parentID
	c := Channel{Name: name, PostPolicy: "everyone", Kind: "thread", ParentID: &parent}
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO channels (name, kind, parent_id, server_id, source_message_id)
		   VALUES ($1, 'thread', $2, $3, $4) RETURNING id, created_at`,
		name, parentID, serverID, fromMessageID).Scan(&c.ID, &c.CreatedAt); err != nil {
		return Channel{}, err
	}
	return c, nil
}

// ListThreads returns the threads spawned from parentID, newest first. The caller verifies
// access to the parent channel.
func (s *Store) ListThreads(ctx context.Context, parentID int64) ([]Channel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at, parent_id FROM channels
		  WHERE kind = 'thread' AND parent_id = $1 ORDER BY id DESC`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.ParentID); err != nil {
			return nil, err
		}
		c.Kind = "thread"
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChannelCategory is a named, collapsible grouping of a server's channels.
type ChannelCategory struct {
	ID        int64     `json:"id"`
	ServerID  int64     `json:"serverId"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// maxCategoryNameLen bounds a category's display name (Rule B). Categories allow spaces
// and caps (they're labels, not slugs), so they don't use ValidChannelName.
const maxCategoryNameLen = 32

// CreateChannelCategory creates a category under serverID. The caller verifies admin and
// passes an already-trimmed, non-empty, length-bounded name (the handler validates it).
func (s *Store) CreateChannelCategory(ctx context.Context, serverID int64, name string) (ChannelCategory, error) {
	c := ChannelCategory{ServerID: serverID, Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO channel_categories (server_id, name) VALUES ($1, $2) RETURNING id, created_at`,
		serverID, name).Scan(&c.ID, &c.CreatedAt)
	return c, err
}

// DeleteChannelCategory removes categoryID from serverID. The caller verifies admin. The
// `AND server_id` clause means you can't delete another server's category (Rule B). The
// category's channels survive — the FK's ON DELETE SET NULL makes them uncategorized.
// Returns ErrCategoryNotFound if no such category exists in the server.
func (s *Store) DeleteChannelCategory(ctx context.Context, serverID, categoryID int64) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM channel_categories WHERE id = $1 AND server_id = $2`, categoryID, serverID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

// ListChannelCategories returns serverID's categories, oldest first.
func (s *Store) ListChannelCategories(ctx context.Context, serverID int64) ([]ChannelCategory, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, server_id, name, created_at FROM channel_categories WHERE server_id = $1 ORDER BY id`,
		serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChannelCategory, 0)
	for rows.Next() {
		var c ChannelCategory
		if err := rows.Scan(&c.ID, &c.ServerID, &c.Name, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListServerChannels returns the channels under serverID, oldest first. The kind column is
// surfaced so the client can tell a 🔊 voice channel apart from a text one, but 'public' is
// normalized to "" so a text channel's JSON stays byte-identical to a pre-voice channel
// (omitempty drops it); only a voice channel carries "kind":"voice" on the wire.
func (s *Store) ListServerChannels(ctx context.Context, serverID int64) ([]Channel, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, created_at, post_policy, topic, slowmode_seconds, category_id, kind FROM channels
		  WHERE server_id = $1 AND kind <> 'thread' ORDER BY id`,
		serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Channel, 0)
	for rows.Next() {
		var c Channel
		var kind string
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.PostPolicy, &c.Topic, &c.SlowmodeSeconds, &c.CategoryID, &kind); err != nil {
			return nil, err
		}
		if kind != "public" {
			c.Kind = kind
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
// inviteTTL is how long a new invite stays valid (Discord's default). Enforced at
// redeem; legacy invites with a NULL expires_at never expire.
const inviteTTL = 7 * 24 * time.Hour

func (s *Store) CreateInvite(ctx context.Context, serverID, userID int64) (string, error) {
	return s.createInvite(ctx, serverID, userID, nil)
}

// CreateInviteWithMaxUses is CreateInvite with an optional join cap: maxUses nil = unlimited
// (same as CreateInvite); a non-nil maxUses (>0) admits at most that many members before the
// code is exhausted. The cap is enforced + the use counted atomically at redeem.
func (s *Store) CreateInviteWithMaxUses(ctx context.Context, serverID, userID int64, maxUses *int) (string, error) {
	return s.createInvite(ctx, serverID, userID, maxUses)
}

func (s *Store) createInvite(ctx context.Context, serverID, userID int64, maxUses *int) (string, error) {
	expiresAt := time.Now().Add(inviteTTL)
	for attempt := 0; attempt < 5; attempt++ {
		code, err := inviteCode()
		if err != nil {
			return "", err
		}
		_, err = s.pool.Exec(ctx,
			`INSERT INTO server_invites (code, server_id, created_by, expires_at, max_uses) VALUES ($1, $2, $3, $4, $5)`,
			code, serverID, userID, expiresAt, maxUses)
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
	var expiresAt *time.Time
	var maxUses *int
	var uses int
	err := s.pool.QueryRow(ctx,
		`SELECT s.id, s.name, s.owner_id, s.created_at, i.expires_at, i.max_uses, i.uses
		   FROM server_invites i JOIN servers s ON s.id = i.server_id
		  WHERE i.code = $1`, code).Scan(&srv.ID, &srv.Name, &srv.OwnerID, &srv.CreatedAt, &expiresAt, &maxUses, &uses)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrInvalidInvite
	}
	if err != nil {
		return Server{}, err
	}
	// Expiry is enforced server-side (a stale client can't bypass it). NULL = never.
	if expiresAt != nil && time.Now().After(*expiresAt) {
		return Server{}, ErrInviteExpired
	}
	// A banned user can't rejoin even with a valid code — enforced server-side (Rule B/C).
	if banned, err := s.IsServerBanned(ctx, srv.ID, userID); err != nil {
		return Server{}, err
	} else if banned {
		return Server{}, ErrBanned
	}
	// Already a member? Rejoining is a no-op and must NOT consume a use — Discord doesn't
	// count an existing member re-opening the link (and keeps legacy re-redeems idempotent).
	if member, err := s.IsServerMember(ctx, srv.ID, userID); err != nil {
		return Server{}, err
	} else if member {
		srv.Role = "member"
		return srv, nil
	}
	// Fast-path the clean "already exhausted" error; the real guard is the conditional
	// UPDATE below (max_uses NULL = unlimited), which is race-safe under concurrent joins.
	if maxUses != nil && uses >= *maxUses {
		return Server{}, ErrInviteExhausted
	}
	// Consume a use and add the member in one tx: if the join fails, the use is rolled
	// back; if a concurrent redeem took the last slot, our guarded UPDATE affects 0 rows.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Server{}, err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx,
		`UPDATE server_invites SET uses = uses + 1
		  WHERE code = $1 AND (max_uses IS NULL OR uses < max_uses)`, code)
	if err != nil {
		return Server{}, err
	}
	if ct.RowsAffected() == 0 {
		return Server{}, ErrInviteExhausted
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		srv.ID, userID); err != nil {
		return Server{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Server{}, err
	}
	srv.Role = "member"
	return srv, nil
}

// Invite describes an active (unexpired) invite for management/listing. CreatorName is
// the joined username of whoever minted it; ExpiresAt is nil for legacy never-expire codes.
type Invite struct {
	Code        string     `json:"code"`
	CreatedBy   int64      `json:"createdBy"`
	CreatorName string     `json:"creatorName"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MaxUses     *int       `json:"maxUses,omitempty"` // nil = unlimited
	Uses        int        `json:"uses"`
}

// ListInvites returns a server's active (unexpired) invite codes, newest first, joined
// with the creator's username. Expired codes are filtered out — they're already dead at
// redeem; this is the admin-facing "what can someone join with right now" view. The caller
// must verify the viewer is an admin (mirrors ListServerBans).
func (s *Store) ListInvites(ctx context.Context, serverID int64) ([]Invite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT i.code, i.created_by, u.username, i.created_at, i.expires_at, i.max_uses, i.uses
		   FROM server_invites i JOIN users u ON u.id = i.created_by
		  WHERE i.server_id = $1 AND (i.expires_at IS NULL OR i.expires_at > now())
		        AND (i.max_uses IS NULL OR i.uses < i.max_uses)
		  ORDER BY i.created_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Invite, 0)
	for rows.Next() {
		var iv Invite
		if err := rows.Scan(&iv.Code, &iv.CreatedBy, &iv.CreatorName, &iv.CreatedAt, &iv.ExpiresAt, &iv.MaxUses, &iv.Uses); err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

// RevokeInvite deletes one invite code from serverID so it can no longer be redeemed.
// The `AND server_id = $2` is the Rule-B cross-server guard: an admin of server A cannot
// revoke server B's code via A's path. ErrInvalidInvite when no matching row exists
// (unknown code, already revoked, or a code from another server). The caller verifies admin.
func (s *Store) RevokeInvite(ctx context.Context, serverID int64, code string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM server_invites WHERE code = $1 AND server_id = $2`, code, serverID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrInvalidInvite
	}
	return nil
}

// Recent returns up to limit messages from the given channel in chronological
// (oldest-first) order.
func (s *Store) Recent(ctx context.Context, channelID, viewerID int64, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.deleted_at, m.edited_at, m.pinned,
		        m.reply_to, ru.username, r.body, r.deleted_at, `+authorColorSQL+`, t.id, COALESCE(t.name, '')
		   FROM messages m JOIN users u ON u.id = m.user_id
		   LEFT JOIN messages r ON r.id = m.reply_to
		   LEFT JOIN users ru ON ru.id = r.user_id
		   LEFT JOIN channels t ON t.source_message_id = m.id AND t.kind = 'thread'
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
			&replyTo, &replyAuthor, &replyBody, &replyDeleted, &m.AuthorColor, &m.ThreadID, &m.ThreadName); err != nil {
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
	byAtt, err := s.AttachmentsForMessages(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		msgs[i].Reactions = byMsg[msgs[i].ID]
		msgs[i].Attachments = byAtt[msgs[i].ID]
	}
	return msgs, nil
}

// PinnedMessages returns the channel's pinned (non-deleted) messages, oldest first.
// Access is gated by the caller (HandlePins) before this runs.
func (s *Store) PinnedMessages(ctx context.Context, channelID int64) ([]Message, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.edited_at, `+authorColorSQL+`, t.id, COALESCE(t.name, '')
		   FROM messages m JOIN users u ON u.id = m.user_id
		   LEFT JOIN channels t ON t.source_message_id = m.id AND t.kind = 'thread'
		  WHERE m.channel_id = $1 AND m.pinned = true AND m.deleted_at IS NULL
		  ORDER BY m.id`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt, &m.EditedAt, &m.AuthorColor, &m.ThreadID, &m.ThreadName); err != nil {
			return nil, err
		}
		m.Pinned = true
		out = append(out, m)
	}
	return out, rows.Err()
}

// searchFilters is a parsed search query: free text plus Discord-style operators.
type searchFilters struct {
	text     string     // remaining free text (LIKE-matched)
	from     string     // from:<username> — author filter (case-insensitive exact)
	hasLink  bool       // has:link  — body contains a URL
	hasImage bool       // has:image — has an image attachment
	hasFile  bool       // has:file  — has a non-image attachment
	before   *time.Time // before:<YYYY-MM-DD> — created strictly before that day (UTC)
	after    *time.Time // after:<YYYY-MM-DD>  — created strictly after that day (UTC)
}

// parseSearchDate parses a `YYYY-MM-DD` search-operator date as UTC midnight.
// A malformed value (bad format, impossible date) returns ok=false so the caller
// keeps the token as plain free text — a hostile `before:`/`after:` never errors
// the search (Rule B: bad input is inert, not fatal). time.Parse is strict, so an
// injection like `'; DROP TABLE` fails the layout and falls through to free text.
func parseSearchDate(s string) (time.Time, bool) {
	d, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

// parseSearchQuery splits a query into free text and operators. `from:X`,
// `has:link|image|file`, and `before:`/`after:<date>` become filters; any other
// token (incl. an unknown has:value or a malformed date) stays as free text, so
// `from:alice deploy` = alice's messages containing "deploy".
func parseSearchQuery(q string) searchFilters {
	var f searchFilters
	var text []string
	for _, tok := range strings.Fields(q) {
		switch lower := strings.ToLower(tok); {
		case strings.HasPrefix(lower, "from:") && len(tok) > len("from:"):
			f.from = tok[len("from:"):]
		case lower == "has:link":
			f.hasLink = true
		case lower == "has:image":
			f.hasImage = true
		case lower == "has:file":
			f.hasFile = true
		case strings.HasPrefix(lower, "before:") && len(tok) > len("before:"):
			if d, ok := parseSearchDate(tok[len("before:"):]); ok {
				f.before = &d // created_at < midnight(d) — the named day is excluded
			} else {
				text = append(text, tok)
			}
		case strings.HasPrefix(lower, "after:") && len(tok) > len("after:"):
			if d, ok := parseSearchDate(tok[len("after:"):]); ok {
				end := d.AddDate(0, 0, 1) // created_at >= start of the next day
				f.after = &end
			} else {
				text = append(text, tok)
			}
		default:
			text = append(text, tok)
		}
	}
	f.text = strings.Join(text, " ")
	return f
}

// SearchMessages returns up to limit non-deleted messages in channelID matching query,
// in chronological order. Query supports free text (LIKE-wildcard-escaped so '%' is
// literal — can't turn into match-all) plus operators: from:<user>, has:link, has:image,
// has:file, before:<YYYY-MM-DD>, after:<YYYY-MM-DD>. The SQL is built dynamically but
// every value is a bind parameter (no SQL injection, Rule B); operator fragments are
// fixed SQL.
func (s *Store) SearchMessages(ctx context.Context, channelID int64, query string, limit int) ([]Message, error) {
	f := parseSearchQuery(query)
	conds := []string{"m.channel_id = $1", "m.deleted_at IS NULL"}
	args := []any{channelID}
	add := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if f.text != "" {
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.text)
		conds = append(conds, "m.body ILIKE '%' || "+add(esc)+" || '%'")
	}
	if f.from != "" {
		conds = append(conds, "lower(u.username) = lower("+add(f.from)+")")
	}
	if f.hasLink {
		conds = append(conds, "m.body ~* 'https?://'")
	}
	if f.hasImage {
		conds = append(conds, "EXISTS (SELECT 1 FROM attachments a WHERE a.message_id = m.id AND a.content_type LIKE 'image/%')")
	}
	if f.hasFile {
		conds = append(conds, "EXISTS (SELECT 1 FROM attachments a WHERE a.message_id = m.id AND a.content_type NOT LIKE 'image/%')")
	}
	if f.before != nil {
		conds = append(conds, "m.created_at < "+add(*f.before))
	}
	if f.after != nil {
		conds = append(conds, "m.created_at >= "+add(*f.after))
	}
	sql := `SELECT m.id, m.channel_id, m.user_id, u.username, m.body, m.created_at, m.edited_at, ` + authorColorSQL + `, t.id, COALESCE(t.name, '')
		   FROM messages m JOIN users u ON u.id = m.user_id
		   LEFT JOIN channels t ON t.source_message_id = m.id AND t.kind = 'thread'
		  WHERE ` + strings.Join(conds, " AND ") + `
		  ORDER BY m.id DESC LIMIT ` + add(limit)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	msgs := make([]Message, 0, limit)
	for rows.Next() {
		var m Message
		var editedAt *time.Time
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt, &editedAt, &m.AuthorColor, &m.ThreadID, &m.ThreadName); err != nil {
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
		if errors.Is(err, ErrBlocked) {
			// A block (either direction) forbids opening the DM (v0.5, symmetric). 403
			// without distinguishing who blocked whom (no information leak, Rule 15).
			http.Error(w, `{"error":"cannot open a DM with this user"}`, http.StatusForbidden)
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

// HandleCreateGroupDM creates a group direct message from {"identifiers":[...]} — a list
// of usernames or numeric user ids (the same identifier form HandleCreateDM accepts). The
// caller is always a member; 2..9 distinct others form a group (one other delegates to the
// idempotent 1:1). Maps not-found→404, a block relationship with any member→403, and the
// 10-member cap / empty list→400. Returns the DMChannel (with Users) as seen by the creator.
func HandleCreateGroupDM(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := auth.UserFrom(r.Context())
		var in struct {
			Identifiers []string `json:"identifiers"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}
		if len(in.Identifiers) == 0 {
			http.Error(w, `{"error":"a group DM needs at least one other member"}`, http.StatusBadRequest)
			return
		}
		// 10-member cap (creator + 9) — reject early before any DB work (Rule B: bound input).
		if len(in.Identifiers) > 9 {
			http.Error(w, `{"error":"group DM exceeds the 10-member limit"}`, http.StatusBadRequest)
			return
		}
		ids := make([]int64, 0, len(in.Identifiers))
		for _, ident := range in.Identifiers {
			u, err := store.LookupUserByIdentifier(r.Context(), ident)
			if errors.Is(err, ErrUserNotFound) {
				http.Error(w, `{"error":"no user found for that username or id"}`, http.StatusNotFound)
				return
			}
			if err != nil {
				http.Error(w, `{"error":"could not look up user"}`, http.StatusInternalServerError)
				return
			}
			ids = append(ids, u.ID)
		}
		dm, err := store.CreateGroupDM(r.Context(), me.ID, ids)
		if errors.Is(err, ErrCannotDMSelf) {
			http.Error(w, `{"error":"a group DM needs at least one other member"}`, http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrGroupTooLarge) {
			http.Error(w, `{"error":"group DM exceeds the 10-member limit"}`, http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrBlocked) {
			// A block (either direction) with any member forbids the group (v0.5/v0.6,
			// symmetric). 403 without naming who blocked whom (no information leak, Rule 15).
			http.Error(w, `{"error":"cannot start a group with one of these users"}`, http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, `{"error":"could not create group dm"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(dm)
	}
}
