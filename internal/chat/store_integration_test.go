package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setup connects to the database named by DATABASE_URL, applies the schema, and
// returns a store plus a freshly-registered user. It SKIPS when DATABASE_URL is
// unset, so `go test ./...` stays green locally without a database; CI sets it
// (a postgres service) so these run there.
func setup(t *testing.T) (*chat.Store, *pgxpool.Pool, auth.User) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping DB integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return chat.NewStore(pool), pool, regUser(t, pool)
}

var userCounter int64

// regUser registers a fresh, uniquely-named user on the pool.
func regUser(t *testing.T, pool *pgxpool.Pool) auth.User {
	t.Helper()
	name := fmt.Sprintf("itest_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&userCounter, 1))
	u, err := auth.New(pool, []byte("test-secret"), time.Hour).
		Register(context.Background(), name, "password123")
	if err != nil {
		t.Fatalf("register test user: %v", err)
	}
	return u
}

func uniqueChannel() string { return fmt.Sprintf("itest-%d", time.Now().UnixNano()) }

func TestChannelStoreIntegration(t *testing.T) {
	store, _, _ := setup(t)
	ctx := context.Background()

	name := uniqueChannel()
	c, err := store.CreateChannel(ctx, name)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == 0 || c.Name != name {
		t.Fatalf("unexpected channel %+v", c)
	}

	if _, err := store.CreateChannel(ctx, name); !errors.Is(err, chat.ErrChannelExists) {
		t.Fatalf("duplicate create err = %v, want ErrChannelExists", err)
	}

	if ok, err := store.ChannelExists(ctx, c.ID); err != nil || !ok {
		t.Fatalf("ChannelExists(%d) = %v, %v; want true", c.ID, ok, err)
	}
	if ok, _ := store.ChannelExists(ctx, 1<<40); ok {
		t.Fatal("ChannelExists for an unknown id should be false")
	}

	list, err := store.ListChannels(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, ch := range list {
		names[ch.Name] = true
	}
	if !names["general"] || !names[name] {
		t.Fatalf("ListChannels missing general or %q: %v", name, names)
	}

	gid, err := store.DefaultChannelID(ctx)
	if err != nil || gid == 0 {
		t.Fatalf("DefaultChannelID = %d, %v; want a valid general id", gid, err)
	}
}

func TestMessageChannelScopingIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()

	a, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	if _, err := store.Save(ctx, a.ID, u.ID, u.Username, "in-A"); err != nil {
		t.Fatalf("save A: %v", err)
	}
	if _, err := store.Save(ctx, b.ID, u.ID, u.Username, "in-B"); err != nil {
		t.Fatalf("save B: %v", err)
	}

	recentA, err := store.Recent(ctx, a.ID, u.ID, 50)
	if err != nil {
		t.Fatalf("recent A: %v", err)
	}
	if len(recentA) != 1 || recentA[0].Body != "in-A" || recentA[0].ChannelID != a.ID {
		t.Fatalf("channel A history wrong (isolation broken?): %+v", recentA)
	}

	recentB, err := store.Recent(ctx, b.ID, u.ID, 50)
	if err != nil {
		t.Fatalf("recent B: %v", err)
	}
	if len(recentB) != 1 || recentB[0].Body != "in-B" || recentB[0].ChannelID != b.ID {
		t.Fatalf("channel B history wrong (isolation broken?): %+v", recentB)
	}
}

func TestEditMessageIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()
	ch, _ := store.CreateChannel(ctx, uniqueChannel())
	m, err := store.Save(ctx, ch.ID, u.ID, u.Username, "before")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	edited, err := store.EditMessage(ctx, m.ID, u.ID, "after")
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if edited.Body != "after" || edited.EditedAt == nil || edited.ChannelID != ch.ID {
		t.Fatalf("edit result wrong: %+v", edited)
	}

	// A different user must not be able to edit it.
	if _, err := store.EditMessage(ctx, m.ID, u.ID+99999, "hax"); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("non-owner edit err = %v, want ErrMessageNotFound", err)
	}

	// A deleted message is no longer editable.
	if _, err := store.DeleteMessage(ctx, m.ID, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.EditMessage(ctx, m.ID, u.ID, "zombie"); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("edit-after-delete err = %v, want ErrMessageNotFound", err)
	}
}

func TestDeleteMessageIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()
	ch, _ := store.CreateChannel(ctx, uniqueChannel())
	m, _ := store.Save(ctx, ch.ID, u.ID, u.Username, "doomed")

	// A different user must not be able to delete it.
	if _, err := store.DeleteMessage(ctx, m.ID, u.ID+99999); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("non-owner delete err = %v, want ErrMessageNotFound", err)
	}

	del, err := store.DeleteMessage(ctx, m.ID, u.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !del.Deleted || del.ChannelID != ch.ID {
		t.Fatalf("delete result wrong: %+v", del)
	}

	// Re-deleting is a no-op → not found.
	if _, err := store.DeleteMessage(ctx, m.ID, u.ID); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("re-delete err = %v, want ErrMessageNotFound", err)
	}

	// History renders it as deleted.
	recent, _ := store.Recent(ctx, ch.ID, u.ID, 50)
	var found bool
	for _, x := range recent {
		if x.ID == m.ID {
			found = true
			if !x.Deleted || x.Body != "[deleted]" {
				t.Fatalf("history not marked deleted: %+v", x)
			}
		}
	}
	if !found {
		t.Fatal("deleted message missing from history")
	}
}

func TestReactionsIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()
	ch, _ := store.CreateChannel(ctx, uniqueChannel())
	m, err := store.Save(ctx, ch.ID, u.ID, u.Username, "react to me")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	chID, err := store.AddReaction(ctx, m.ID, u.ID, "👍")
	if err != nil || chID != ch.ID {
		t.Fatalf("add reaction: chID=%d err=%v", chID, err)
	}
	// Adding the same reaction again is idempotent.
	if _, err := store.AddReaction(ctx, m.ID, u.ID, "👍"); err != nil {
		t.Fatalf("idempotent add: %v", err)
	}

	mine := reactionOf(t, store, ctx, ch.ID, u.ID, m.ID)
	if len(mine) != 1 || mine[0].Emoji != "👍" || mine[0].Count != 1 || !mine[0].Mine {
		t.Fatalf("reactor's view wrong: %+v", mine)
	}
	other := reactionOf(t, store, ctx, ch.ID, u.ID+99999, m.ID)
	if len(other) != 1 || other[0].Count != 1 || other[0].Mine {
		t.Fatalf("non-reactor's view wrong: %+v", other)
	}

	if _, err := store.RemoveReaction(ctx, m.ID, u.ID, "👍"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := reactionOf(t, store, ctx, ch.ID, u.ID, m.ID); len(got) != 0 {
		t.Fatalf("expected no reactions after remove, got %+v", got)
	}

	if _, err := store.AddReaction(ctx, m.ID, u.ID, ""); !errors.Is(err, chat.ErrInvalidEmoji) {
		t.Fatalf("empty emoji err = %v, want ErrInvalidEmoji", err)
	}
	if _, err := store.AddReaction(ctx, 1<<40, u.ID, "👍"); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("missing-message err = %v, want ErrMessageNotFound", err)
	}
}

// TestCustomEmojiReactionsIntegration mirrors TestReactionsIntegration but for a
// custom-emoji reaction, stored verbatim as the marker `custom:{id}` in the existing
// reactions.emoji column (no schema/WS change — see validEmoji). It proves: the marker
// is stored, aggregated (count 1, mine=true for the reactor), add is idempotent, remove
// works, AND validEmoji accepts a well-formed marker while rejecting a non-numeric id,
// an empty id, and an oversized one — all via the AddReaction → ErrInvalidEmoji path.
func TestCustomEmojiReactionsIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()
	ch, _ := store.CreateChannel(ctx, uniqueChannel())
	m, err := store.Save(ctx, ch.ID, u.ID, u.Username, "react with a custom emoji")
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// A plain-unicode reaction still works alongside the custom one (no regression).
	if _, err := store.AddReaction(ctx, m.ID, u.ID, "👍"); err != nil {
		t.Fatalf("unicode add: %v", err)
	}

	// Add the custom-emoji marker. The id need not reference a live emoji row — the
	// marker is decoupled by design (a deleted emoji just renders broken), so we use a
	// bare numeric id (mirrors the client sending `custom:{id}`).
	const marker = "custom:7"
	chID, err := store.AddReaction(ctx, m.ID, u.ID, marker)
	if err != nil || chID != ch.ID {
		t.Fatalf("add custom reaction: chID=%d err=%v", chID, err)
	}
	// Adding the same custom reaction again is idempotent (ON CONFLICT DO NOTHING).
	if _, err := store.AddReaction(ctx, m.ID, u.ID, marker); err != nil {
		t.Fatalf("idempotent custom add: %v", err)
	}

	// The reactor's view: the custom marker is present with count 1 and mine=true.
	mine := reactionOf(t, store, ctx, ch.ID, u.ID, m.ID)
	custom := findReaction(mine, marker)
	if custom == nil || custom.Count != 1 || !custom.Mine {
		t.Fatalf("reactor's custom view wrong: %+v (all: %+v)", custom, mine)
	}
	// A non-reactor sees the count but not mine.
	other := reactionOf(t, store, ctx, ch.ID, u.ID+99999, m.ID)
	otherCustom := findReaction(other, marker)
	if otherCustom == nil || otherCustom.Count != 1 || otherCustom.Mine {
		t.Fatalf("non-reactor's custom view wrong: %+v (all: %+v)", otherCustom, other)
	}

	// Toggling off removes only the custom marker; the unicode reaction survives.
	if _, err := store.RemoveReaction(ctx, m.ID, u.ID, marker); err != nil {
		t.Fatalf("remove custom: %v", err)
	}
	after := reactionOf(t, store, ctx, ch.ID, u.ID, m.ID)
	if findReaction(after, marker) != nil {
		t.Fatalf("custom reaction still present after remove: %+v", after)
	}
	if findReaction(after, "👍") == nil {
		t.Fatalf("unicode reaction lost when removing the custom one: %+v", after)
	}

	// validEmoji (via AddReaction) REJECTS malformed markers. Each must return
	// ErrInvalidEmoji and write nothing.
	bad := map[string]string{
		"non-numeric id":       "custom:abc",
		"empty id":             "custom:",
		"oversized numeric id": "custom:1234567890123456789012345", // 25 digits → rest>16 & total>24
	}
	for name, e := range bad {
		if _, err := store.AddReaction(ctx, m.ID, u.ID, e); !errors.Is(err, chat.ErrInvalidEmoji) {
			t.Fatalf("%s (%q): err = %v, want ErrInvalidEmoji", name, e, err)
		}
	}
}

// findReaction returns the summary for emoji in rs, or nil if absent.
func findReaction(rs []chat.ReactionSummary, emoji string) *chat.ReactionSummary {
	for i := range rs {
		if rs[i].Emoji == emoji {
			return &rs[i]
		}
	}
	return nil
}

func reactionOf(t *testing.T, store *chat.Store, ctx context.Context, channelID, viewerID, msgID int64) []chat.ReactionSummary {
	t.Helper()
	msgs, err := store.Recent(ctx, channelID, viewerID, 50)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	for _, m := range msgs {
		if m.ID == msgID {
			return m.Reactions
		}
	}
	t.Fatalf("message %d not in history", msgID)
	return nil
}

func TestDirectMessagesIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)

	// Open a DM alice↔bob; it reports bob as the other participant.
	dm, err := store.CreateOrGetDM(ctx, alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("create dm: %v", err)
	}
	if dm.ID == 0 || dm.User.ID != bob.ID || dm.User.Username != bob.Username {
		t.Fatalf("dm wrong: %+v", dm)
	}

	// Idempotent: a second open returns the same channel, not a duplicate.
	again, err := store.CreateOrGetDM(ctx, alice.ID, bob.ID)
	if err != nil || again.ID != dm.ID {
		t.Fatalf("CreateOrGetDM not idempotent: %+v (want id %d), err=%v", again, dm.ID, err)
	}
	// Order-independent: bob opening with alice resolves to the same channel.
	rev, err := store.CreateOrGetDM(ctx, bob.ID, alice.ID)
	if err != nil || rev.ID != dm.ID {
		t.Fatalf("reverse open made a different channel: %+v (want id %d)", rev, dm.ID)
	}

	// Access control — the privacy core. Members in, everyone else out.
	for _, tc := range []struct {
		who  int64
		want bool
		name string
	}{
		{alice.ID, true, "alice (member)"},
		{bob.ID, true, "bob (member)"},
		{carol.ID, false, "carol (non-member)"},
	} {
		got, err := store.CanAccessChannel(ctx, dm.ID, tc.who)
		if err != nil || got != tc.want {
			t.Fatalf("CanAccessChannel dm for %s = %v (err %v), want %v", tc.name, got, err, tc.want)
		}
	}

	// Public channels are open to everyone; unknown ids are denied.
	general, _ := store.DefaultChannelID(ctx)
	if ok, err := store.CanAccessChannel(ctx, general, carol.ID); err != nil || !ok {
		t.Fatalf("carol must access the public general channel, got %v err %v", ok, err)
	}
	if ok, _ := store.CanAccessChannel(ctx, 1<<40, alice.ID); ok {
		t.Fatal("CanAccessChannel for an unknown channel must be false")
	}

	// ListDMs is per-viewer and names the *other* participant.
	aliceDMs, err := store.ListDMs(ctx, alice.ID)
	if err != nil || !hasDM(aliceDMs, dm.ID, bob.ID) {
		t.Fatalf("alice's DMs should include the dm with bob: %+v err %v", aliceDMs, err)
	}
	bobDMs, _ := store.ListDMs(ctx, bob.ID)
	if !hasDM(bobDMs, dm.ID, alice.ID) {
		t.Fatalf("bob's DMs should include the dm with alice: %+v", bobDMs)
	}
	if carolDMs, _ := store.ListDMs(ctx, carol.ID); len(carolDMs) != 0 {
		t.Fatalf("carol should have no DMs, got %+v", carolDMs)
	}

	// DM channels never leak into the public channel list.
	publics, _ := store.ListChannels(ctx)
	for _, c := range publics {
		if c.ID == dm.ID {
			t.Fatalf("DM channel %d leaked into the public channel list", dm.ID)
		}
	}

	// Guards: self-DM and unknown user.
	if _, err := store.CreateOrGetDM(ctx, alice.ID, alice.ID); !errors.Is(err, chat.ErrCannotDMSelf) {
		t.Fatalf("self-DM err = %v, want ErrCannotDMSelf", err)
	}
	if _, err := store.LookupUserByUsername(ctx, "nobody_"+uniqueChannel()); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("unknown user err = %v, want ErrUserNotFound", err)
	}
	if u, err := store.LookupUserByUsername(ctx, bob.Username); err != nil || u.ID != bob.ID {
		t.Fatalf("lookup bob = %+v err %v", u, err)
	}

	// Identifier lookup resolves both a username and a numeric user id to the same
	// user (invite/DM by username OR id).
	if u, err := store.LookupUserByIdentifier(ctx, bob.Username); err != nil || u.ID != bob.ID {
		t.Fatalf("identifier(username) = %+v err %v, want bob", u, err)
	}
	if u, err := store.LookupUserByIdentifier(ctx, strconv.FormatInt(bob.ID, 10)); err != nil || u.ID != bob.ID {
		t.Fatalf("identifier(id) = %+v err %v, want bob", u, err)
	}
	if _, err := store.LookupUserByIdentifier(ctx, "99999999"); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("identifier(unknown id) err = %v, want ErrUserNotFound", err)
	}
}

// TestGroupDMIntegration exercises v0.6 group DMs (slice 1, backend): a group DM is the
// same kind='dm' channel generalized to 3+ members. It proves the N-member create/list/
// access paths AND that the existing 2-member model (idempotency, block-hiding) is intact.
func TestGroupDMIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)
	dave := regUser(t, pool)

	// A 1:1 alice↔carol created up-front, so we can later prove a block hides the 1:1 but
	// NOT the group that also contains carol.
	solo, err := store.CreateOrGetDM(ctx, alice.ID, carol.ID)
	if err != nil {
		t.Fatalf("create 1:1 alice-carol: %v", err)
	}

	// Alice starts a group with bob + carol → a 3-member channel; the response carries both
	// others (username-sorted), and User mirrors Users[0].
	g, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, carol.ID})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if g.ID == 0 || len(g.Users) != 2 {
		t.Fatalf("group wrong: %+v", g)
	}
	if g.Users[0].ID != g.User.ID {
		t.Fatalf("User must mirror Users[0]: %+v", g)
	}
	if g.Users[0].Username > g.Users[1].Username {
		t.Fatalf("Users must be username-sorted: %+v", g.Users)
	}
	if !hasOther(g.Users, bob.ID) || !hasOther(g.Users, carol.ID) {
		t.Fatalf("group should contain bob+carol: %+v", g.Users)
	}

	// Access control: all three members in; dave (non-member) out.
	for _, tc := range []struct {
		who  int64
		want bool
		name string
	}{
		{alice.ID, true, "alice"},
		{bob.ID, true, "bob"},
		{carol.ID, true, "carol"},
		{dave.ID, false, "dave (non-member)"},
	} {
		got, err := store.CanAccessChannel(ctx, g.ID, tc.who)
		if err != nil || got != tc.want {
			t.Fatalf("CanAccessChannel group for %s = %v (err %v), want %v", tc.name, got, err, tc.want)
		}
	}

	// ListDMs is per-viewer: each member sees the OTHER two, never themselves.
	bobDMs, _ := store.ListDMs(ctx, bob.ID)
	bg := findDM(bobDMs, g.ID)
	if bg == nil || len(bg.Users) != 2 || !hasOther(bg.Users, alice.ID) ||
		!hasOther(bg.Users, carol.ID) || hasOther(bg.Users, bob.ID) {
		t.Fatalf("bob's view of the group wrong: %+v", bg)
	}

	// Groups are NOT deduped — the same members again make a fresh channel (unlike a 1:1).
	g2, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, carol.ID})
	if err != nil || g2.ID == g.ID {
		t.Fatalf("group should not dedupe: g2=%+v err=%v (g.ID=%d)", g2, err, g.ID)
	}

	// Exactly one distinct other delegates to the idempotent 1:1 (incl. after dedupe/self-drop).
	d1, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID})
	if err != nil {
		t.Fatalf("1-other group: %v", err)
	}
	if d2, _ := store.CreateOrGetDM(ctx, alice.ID, bob.ID); d1.ID != d2.ID {
		t.Fatalf("1-other group must resolve to the 1:1 DM: %d vs %d", d1.ID, d2.ID)
	}
	if dd, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, bob.ID, alice.ID}); err != nil || dd.ID != d1.ID {
		t.Fatalf("dedupe/drop-self should resolve to the same 1:1: %+v err=%v", dd, err)
	}

	// Guards: zero distinct others, and the 10-member cap (creator + >9 others).
	if _, err := store.CreateGroupDM(ctx, alice.ID, []int64{alice.ID}); !errors.Is(err, chat.ErrCannotDMSelf) {
		t.Fatalf("zero-others err = %v, want ErrCannotDMSelf", err)
	}
	tooMany := make([]int64, 0, 10)
	for i := 0; i < 10; i++ {
		tooMany = append(tooMany, regUser(t, pool).ID)
	}
	if _, err := store.CreateGroupDM(ctx, alice.ID, tooMany); !errors.Is(err, chat.ErrGroupTooLarge) {
		t.Fatalf("10-other group err = %v, want ErrGroupTooLarge", err)
	}

	// A block with ANY prospective member rejects group creation (symmetric).
	if err := store.BlockUser(ctx, alice.ID, dave.ID); err != nil {
		t.Fatalf("block dave: %v", err)
	}
	if _, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, dave.ID}); !errors.Is(err, chat.ErrBlocked) {
		t.Fatalf("group incl. blocked dave err = %v, want ErrBlocked", err)
	}

	// Blocking carol AFTER the group exists must NOT hide the whole group (unlike a 1:1):
	// the group still lists with carol omitted from Users and access stays open — while the
	// pre-existing 1:1 alice↔carol IS hidden + denied (the unchanged 2-member rule).
	if err := store.BlockUser(ctx, alice.ID, carol.ID); err != nil {
		t.Fatalf("block carol: %v", err)
	}
	aliceDMs, _ := store.ListDMs(ctx, alice.ID)
	if ag := findDM(aliceDMs, g.ID); ag == nil {
		t.Fatal("group must still appear for alice after blocking one member")
	} else if hasOther(ag.Users, carol.ID) || !hasOther(ag.Users, bob.ID) {
		t.Fatalf("blocked carol must be omitted from the group's Users (bob kept): %+v", ag.Users)
	}
	if ok, err := store.CanAccessChannel(ctx, g.ID, alice.ID); err != nil || !ok {
		t.Fatalf("alice must still access the group after blocking carol: %v err %v", ok, err)
	}
	if findDM(aliceDMs, solo.ID) != nil {
		t.Fatal("the 1:1 with the blocked carol must be hidden from alice's DM list")
	}
	if ok, _ := store.CanAccessChannel(ctx, solo.ID, alice.ID); ok {
		t.Fatal("alice must be denied the 1:1 with the blocked carol")
	}
}

// TestUserBlockingIntegration exercises the v0.5 user-blocking store layer (slice 1:
// DM-only enforcement). A block is SYMMETRIC for DMs — if A blocked B OR B blocked A,
// neither can open, read, or send in their DM — and it must not touch server/global
// channel access.
func TestUserBlockingIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)

	// Can't block yourself.
	if err := store.BlockUser(ctx, alice.ID, alice.ID); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("self-block err = %v, want ErrForbidden", err)
	}
	// Blocking a non-existent user → ErrUserNotFound.
	if err := store.BlockUser(ctx, alice.ID, 1<<40); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("block unknown err = %v, want ErrUserNotFound", err)
	}

	// Pre-existing DM alice↔bob, accessible to both before any block.
	dm, err := store.CreateOrGetDM(ctx, alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("create dm: %v", err)
	}
	for _, who := range []int64{alice.ID, bob.ID} {
		if ok, err := store.CanAccessChannel(ctx, dm.ID, who); err != nil || !ok {
			t.Fatalf("before block: user %d must access dm, got %v err %v", who, ok, err)
		}
	}

	// Alice blocks bob (idempotent: a second block is a no-op, not an error).
	if err := store.BlockUser(ctx, alice.ID, bob.ID); err != nil {
		t.Fatalf("block bob: %v", err)
	}
	if err := store.BlockUser(ctx, alice.ID, bob.ID); err != nil {
		t.Fatalf("re-block bob (idempotent): %v", err)
	}

	// IsBlocked is symmetric.
	if ok, err := store.IsBlocked(ctx, alice.ID, bob.ID); err != nil || !ok {
		t.Fatalf("IsBlocked(alice,bob) = %v err %v, want true", ok, err)
	}
	if ok, err := store.IsBlocked(ctx, bob.ID, alice.ID); err != nil || !ok {
		t.Fatalf("IsBlocked(bob,alice) = %v err %v, want true (symmetric)", ok, err)
	}
	if ok, _ := store.IsBlocked(ctx, alice.ID, carol.ID); ok {
		t.Fatal("IsBlocked(alice,carol) must be false")
	}

	// CreateOrGetDM is now blocked in BOTH directions (symmetric).
	if _, err := store.CreateOrGetDM(ctx, alice.ID, bob.ID); !errors.Is(err, chat.ErrBlocked) {
		t.Fatalf("CreateOrGetDM(alice,bob) err = %v, want ErrBlocked", err)
	}
	if _, err := store.CreateOrGetDM(ctx, bob.ID, alice.ID); !errors.Is(err, chat.ErrBlocked) {
		t.Fatalf("CreateOrGetDM(bob,alice) err = %v, want ErrBlocked (symmetric)", err)
	}

	// The pre-existing DM is now inaccessible to BOTH members (symmetric read gate).
	for _, who := range []int64{alice.ID, bob.ID} {
		if ok, err := store.CanAccessChannel(ctx, dm.ID, who); err != nil || ok {
			t.Fatalf("after block: user %d must NOT access dm, got %v err %v", who, ok, err)
		}
	}

	// The blocked DM is filtered out of both members' DM lists.
	if dms, _ := store.ListDMs(ctx, alice.ID); hasDM(dms, dm.ID, bob.ID) {
		t.Fatalf("alice's DM list must exclude the blocked dm: %+v", dms)
	}
	if dms, _ := store.ListDMs(ctx, bob.ID); hasDM(dms, dm.ID, alice.ID) {
		t.Fatalf("bob's DM list must exclude the blocked dm: %+v", dms)
	}

	// ListBlocked is the directed blocker→blocked view: alice sees bob, bob sees nobody.
	blocked, err := store.ListBlocked(ctx, alice.ID)
	if err != nil || len(blocked) != 1 || blocked[0].ID != bob.ID {
		t.Fatalf("ListBlocked(alice) = %+v err %v, want [bob]", blocked, err)
	}
	if bobBlocked, _ := store.ListBlocked(ctx, bob.ID); len(bobBlocked) != 0 {
		t.Fatalf("ListBlocked(bob) = %+v, want empty (block is directed in storage)", bobBlocked)
	}

	// REGRESSION GUARD: server/global channel access is untouched by blocking. Put
	// alice + bob in a server channel; both must still access it despite the block.
	srv, err := store.CreateServer(ctx, alice.ID, "Block Regression Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := store.AddServerMember(ctx, srv.ID, bob.ID); err != nil {
		t.Fatalf("add bob to server: %v", err)
	}
	ch, err := store.CreateServerChannel(ctx, srv.ID, uniqueChannel())
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}
	for _, who := range []int64{alice.ID, bob.ID} {
		if ok, err := store.CanAccessChannel(ctx, ch.ID, who); err != nil || !ok {
			t.Fatalf("server-channel access for user %d must be unaffected by a block, got %v err %v", who, ok, err)
		}
	}
	general, _ := store.DefaultChannelID(ctx)
	if ok, err := store.CanAccessChannel(ctx, general, bob.ID); err != nil || !ok {
		t.Fatalf("global #general access must be unaffected by a block, got %v err %v", ok, err)
	}

	// Unblock restores everything: DM re-opens, both regain access, list clears.
	if err := store.UnblockUser(ctx, alice.ID, bob.ID); err != nil {
		t.Fatalf("unblock bob: %v", err)
	}
	// Unblocking a not-blocked pair → ErrUserNotFound.
	if err := store.UnblockUser(ctx, alice.ID, bob.ID); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("re-unblock err = %v, want ErrUserNotFound", err)
	}
	if ok, err := store.IsBlocked(ctx, alice.ID, bob.ID); err != nil || ok {
		t.Fatalf("after unblock IsBlocked = %v err %v, want false", ok, err)
	}
	reopened, err := store.CreateOrGetDM(ctx, alice.ID, bob.ID)
	if err != nil || reopened.ID != dm.ID {
		t.Fatalf("after unblock CreateOrGetDM = %+v err %v, want the original dm %d", reopened, err, dm.ID)
	}
	for _, who := range []int64{alice.ID, bob.ID} {
		if ok, err := store.CanAccessChannel(ctx, dm.ID, who); err != nil || !ok {
			t.Fatalf("after unblock: user %d must access dm again, got %v err %v", who, ok, err)
		}
	}
	if dms, _ := store.ListDMs(ctx, alice.ID); !hasDM(dms, dm.ID, bob.ID) {
		t.Fatalf("after unblock alice's DM list must include the dm again: %+v", dms)
	}
	if blocked, _ := store.ListBlocked(ctx, alice.ID); len(blocked) != 0 {
		t.Fatalf("after unblock ListBlocked(alice) = %+v, want empty", blocked)
	}
}

// A non-member must not be able to react (or un-react) to a message in a DM they
// can't access — reactions take a message id, which is guessable, so the read gate
// alone isn't enough (Rule 15). Members and public-channel reactions stay open.
func TestDMReactionAccessControlIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)

	dm, err := store.CreateOrGetDM(ctx, alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("create dm: %v", err)
	}
	m, err := store.Save(ctx, dm.ID, alice.ID, alice.Username, "secret dm message")
	if err != nil {
		t.Fatalf("save dm message: %v", err)
	}

	// A member may react.
	if _, err := store.AddReaction(ctx, m.ID, bob.ID, "👍"); err != nil {
		t.Fatalf("member should be able to react in a DM: %v", err)
	}
	// A non-member must be blocked from adding or removing a reaction.
	if _, err := store.AddReaction(ctx, m.ID, carol.ID, "👍"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-member AddReaction err = %v, want ErrForbidden", err)
	}
	if _, err := store.RemoveReaction(ctx, m.ID, carol.ID, "👍"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-member RemoveReaction err = %v, want ErrForbidden", err)
	}

	// Public-channel reactions stay unrestricted (no regression).
	ch, _ := store.CreateChannel(ctx, uniqueChannel())
	pm, _ := store.Save(ctx, ch.ID, alice.ID, alice.Username, "public message")
	if _, err := store.AddReaction(ctx, pm.ID, carol.ID, "👍"); err != nil {
		t.Fatalf("anyone should be able to react in a public channel: %v", err)
	}
}

func TestServersIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	outsider := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "My Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if srv.ID == 0 || srv.OwnerID != owner.ID || srv.Name != "My Guild" {
		t.Fatalf("server wrong: %+v", srv)
	}
	// The owner is auto-joined; an outsider is not.
	if ok, _ := store.IsServerMember(ctx, srv.ID, owner.ID); !ok {
		t.Fatal("owner should be a member of their own server")
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, outsider.ID); ok {
		t.Fatal("outsider should not be a member")
	}

	// A channel under the server is members-only.
	ch, err := store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, owner.ID); !ok {
		t.Fatal("member should access the server channel")
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, outsider.ID); ok {
		t.Fatal("outsider must NOT access the server channel")
	}

	// Server channels never leak into the global public list; ListServerChannels has it.
	publics, _ := store.ListChannels(ctx)
	for _, c := range publics {
		if c.ID == ch.ID {
			t.Fatalf("server channel %d leaked into the global channel list", ch.ID)
		}
	}
	if sc, _ := store.ListServerChannels(ctx, srv.ID); len(sc) != 1 || sc[0].ID != ch.ID {
		t.Fatalf("ListServerChannels wrong: %+v", sc)
	}

	// ListServers is per-member.
	if ms, _ := store.ListServers(ctx, owner.ID); !hasServer(ms, srv.ID) {
		t.Fatalf("owner's servers should include %d", srv.ID)
	}
	if ms, _ := store.ListServers(ctx, outsider.ID); hasServer(ms, srv.ID) {
		t.Fatal("outsider should not see the server")
	}

	// Joining grants access; join is idempotent; unknown server → not found.
	if err := store.AddServerMember(ctx, srv.ID, outsider.ID); err != nil {
		t.Fatalf("join: %v", err)
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, outsider.ID); !ok {
		t.Fatal("after joining, the outsider should access the server channel")
	}
	if err := store.AddServerMember(ctx, srv.ID, outsider.ID); err != nil {
		t.Fatalf("idempotent join: %v", err)
	}
	if err := store.AddServerMember(ctx, 1<<40, owner.ID); !errors.Is(err, chat.ErrServerNotFound) {
		t.Fatalf("join unknown server err = %v, want ErrServerNotFound", err)
	}

	// Per-server channel naming: a second server can also have its own #general,
	// but a duplicate within ONE server conflicts, and the global #general survives.
	srv2, _ := store.CreateServer(ctx, owner.ID, "Another")
	if _, err := store.CreateServerChannel(ctx, srv2.ID, "general"); err != nil {
		t.Fatalf("a second server should allow its own #general: %v", err)
	}
	if _, err := store.CreateServerChannel(ctx, srv.ID, "general"); !errors.Is(err, chat.ErrChannelExists) {
		t.Fatalf("duplicate channel in one server err = %v, want ErrChannelExists", err)
	}
	if gid, err := store.DefaultChannelID(ctx); err != nil || gid == 0 {
		t.Fatalf("global #general should still resolve unambiguously: id=%d err=%v", gid, err)
	}
}

func TestServerInvitesIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	outsider := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Invite Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, _ := store.CreateServerChannel(ctx, srv.ID, "general")

	// Before redeeming, the outsider cannot access the server's channel.
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, outsider.ID); ok {
		t.Fatal("outsider should not access the server channel before redeeming an invite")
	}

	// A member mints an invite code.
	code, err := store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil || len(code) < 6 {
		t.Fatalf("create invite: code=%q err=%v", code, err)
	}

	// An unknown code is rejected — you can't join by guessing.
	if _, err := store.RedeemInvite(ctx, "not-a-real-code", outsider.ID); !errors.Is(err, chat.ErrInvalidInvite) {
		t.Fatalf("redeem bad code err = %v, want ErrInvalidInvite", err)
	}

	// Redeeming the real code joins the outsider, who can then access the channel.
	joined, err := store.RedeemInvite(ctx, code, outsider.ID)
	if err != nil || joined.ID != srv.ID {
		t.Fatalf("redeem: server=%+v err=%v (want id %d)", joined, err, srv.ID)
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, outsider.ID); !ok {
		t.Fatal("after redeeming the invite, the outsider should access the server channel")
	}
	// Redeeming again is idempotent (membership insert is ON CONFLICT DO NOTHING).
	if _, err := store.RedeemInvite(ctx, code, outsider.ID); err != nil {
		t.Fatalf("re-redeem should be idempotent: %v", err)
	}

	// Expiry (Rule 15): a fresh invite carries a future expiry and works; once expired it
	// is rejected. Force the expiry into the past to simulate the 7-day window elapsing.
	stranger := regUser(t, pool)
	expCode, err := store.CreateInvite(ctx, srv.ID, owner.ID)
	if err != nil {
		t.Fatalf("create invite to expire: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE server_invites SET expires_at = now() - interval '1 hour' WHERE code = $1`, expCode); err != nil {
		t.Fatalf("force-expire: %v", err)
	}
	if _, err := store.RedeemInvite(ctx, expCode, stranger.ID); !errors.Is(err, chat.ErrInviteExpired) {
		t.Fatalf("redeem expired invite err = %v, want ErrInviteExpired", err)
	}
	// The stranger must NOT have been admitted by the expired invite.
	if ok, _ := store.IsServerMember(ctx, srv.ID, stranger.ID); ok {
		t.Fatal("an expired invite must not admit the user")
	}
}

// TestInviteMaxUsesConcurrencyIntegration proves the atomic max-uses guard under real
// contention: when many users redeem a capped code simultaneously, EXACTLY max_uses joins
// succeed and the rest get ErrInviteExhausted — no overshoot, no double-count. A sequential
// test can't catch the off-by-one race the guarded `UPDATE ... WHERE uses < max_uses` +
// transaction exist to prevent (Rule 15). All goroutines are released at once to maximize
// the window. Run with -race to also assert no data races.
func TestInviteMaxUsesConcurrencyIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()

	srv, err := store.CreateServer(ctx, owner.ID, "Concurrency Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	const limit = 5
	const racers = 25
	maxUses := limit
	code, err := store.CreateInviteWithMaxUses(ctx, srv.ID, owner.ID, &maxUses)
	if err != nil {
		t.Fatalf("create capped invite: %v", err)
	}

	// Register the racers up front (sequentially) so the concurrent section is ONLY the redeem.
	users := make([]auth.User, racers)
	for i := range users {
		users[i] = regUser(t, pool)
	}

	var okCount, exhaustedCount, otherCount int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, u := range users {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			<-start // release all at once to maximize contention on the last slot
			switch _, err := store.RedeemInvite(ctx, code, uid); {
			case err == nil:
				atomic.AddInt64(&okCount, 1)
			case errors.Is(err, chat.ErrInviteExhausted):
				atomic.AddInt64(&exhaustedCount, 1)
			default:
				atomic.AddInt64(&otherCount, 1)
				t.Errorf("unexpected redeem error: %v", err)
			}
		}(u.ID)
	}
	close(start)
	wg.Wait()

	if otherCount != 0 {
		t.Fatalf("got %d unexpected redeem errors", otherCount)
	}
	if okCount != limit {
		t.Fatalf("exactly %d redeems should succeed, got %d (exhausted=%d)", limit, okCount, exhaustedCount)
	}
	if exhaustedCount != racers-limit {
		t.Fatalf("expected %d exhausted, got %d", racers-limit, exhaustedCount)
	}
	// The stored counter must equal the cap exactly — the guard never overshoots.
	var uses, maxStored int
	if err := pool.QueryRow(ctx,
		`SELECT uses, max_uses FROM server_invites WHERE code = $1`, code).Scan(&uses, &maxStored); err != nil {
		t.Fatalf("read uses: %v", err)
	}
	if uses != limit || maxStored != limit {
		t.Fatalf("stored uses=%d max=%d, want %d/%d", uses, maxStored, limit, limit)
	}
	// And exactly `limit` distinct members were admitted (owner excluded).
	var members int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM server_members WHERE server_id = $1 AND user_id <> $2`, srv.ID, owner.ID).Scan(&members); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if members != limit {
		t.Fatalf("expected %d admitted members, got %d", limit, members)
	}
}

func TestSearchMessagesIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()
	ch, _ := store.CreateChannel(ctx, uniqueChannel())
	mustSave := func(body string) chat.Message {
		m, err := store.Save(ctx, ch.ID, u.ID, u.Username, body)
		if err != nil {
			t.Fatalf("save %q: %v", body, err)
		}
		return m
	}
	mustSave("the quick brown fox")
	mustSave("QUICK silver")
	mustSave("nothing relevant")
	mustSave("100% sure")
	doomed := mustSave("quick deleted")
	if _, err := store.DeleteMessage(ctx, doomed.ID, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Case-insensitive substring match; the deleted and non-matching ones are excluded.
	res, err := store.SearchMessages(ctx, ch.ID, "quick", 50)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	bodies := map[string]bool{}
	for _, m := range res {
		bodies[m.Body] = true
	}
	if !bodies["the quick brown fox"] || !bodies["QUICK silver"] {
		t.Fatalf("expected case-insensitive matches, got %v", bodies)
	}
	if bodies["quick deleted"] {
		t.Fatal("a deleted message must not appear in search results")
	}
	if bodies["nothing relevant"] {
		t.Fatal("a non-matching message should not appear")
	}

	// A '%' query matches LITERALLY (only the message containing '%'), not everything —
	// the LIKE wildcards are escaped (Rule B).
	pct, err := store.SearchMessages(ctx, ch.ID, "%", 50)
	if err != nil {
		t.Fatalf("search %%: %v", err)
	}
	if len(pct) != 1 || pct[0].Body != "100% sure" {
		t.Fatalf("'%%' should match only the literal-%% message, got %+v", pct)
	}
}

func TestSearchOperatorsIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	ch, _ := store.CreateChannel(ctx, uniqueChannel())

	if _, err := store.Save(ctx, ch.ID, alice.ID, alice.Username, "alice plain message"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := store.Save(ctx, ch.ID, bob.ID, bob.Username, "bob plain message"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := store.Save(ctx, ch.ID, alice.ID, alice.Username, "check this https://example.com out"); err != nil {
		t.Fatalf("save link: %v", err)
	}
	if _, err := store.SaveWithAttachments(ctx, ch.ID, bob.ID, bob.Username, "here is a pic", nil,
		[]chat.NewAttachment{{StorageKey: "k1", Filename: "p.png", ContentType: "image/png", Size: 10}}); err != nil {
		t.Fatalf("save image: %v", err)
	}
	if _, err := store.SaveWithAttachments(ctx, ch.ID, bob.ID, bob.Username, "here is a doc", nil,
		[]chat.NewAttachment{{StorageKey: "k2", Filename: "d.pdf", ContentType: "application/pdf", Size: 20}}); err != nil {
		t.Fatalf("save file: %v", err)
	}

	search := func(q string) []string {
		res, err := store.SearchMessages(ctx, ch.ID, q, 50)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		out := make([]string, 0, len(res))
		for _, m := range res {
			out = append(out, m.Body)
		}
		return out
	}
	has := func(bodies []string, body string) bool {
		for _, b := range bodies {
			if b == body {
				return true
			}
		}
		return false
	}

	// from: filters by author (case-insensitive), independent of body text.
	fromBob := search("from:" + bob.Username)
	if !has(fromBob, "bob plain message") || has(fromBob, "alice plain message") {
		t.Fatalf("from:%s should return only bob's messages, got %v", bob.Username, fromBob)
	}
	// from: is case-insensitive.
	if got := search("from:" + strings.ToUpper(bob.Username)); !has(got, "bob plain message") {
		t.Fatalf("from: should be case-insensitive, got %v", got)
	}
	// from: + free text — author AND body.
	if got := search("from:" + bob.Username + " pic"); !has(got, "here is a pic") || has(got, "bob plain message") {
		t.Fatalf("from:bob pic should match only bob's 'pic' message, got %v", got)
	}
	// has:link
	if got := search("has:link"); !has(got, "check this https://example.com out") || has(got, "alice plain message") {
		t.Fatalf("has:link should match only the URL message, got %v", got)
	}
	// has:image — only the image-attachment message.
	if got := search("has:image"); !has(got, "here is a pic") || has(got, "here is a doc") {
		t.Fatalf("has:image should match only the image message, got %v", got)
	}
	// has:file — only the non-image attachment message.
	if got := search("has:file"); !has(got, "here is a doc") || has(got, "here is a pic") {
		t.Fatalf("has:file should match only the non-image attachment message, got %v", got)
	}
	// Injection attempt in from: is inert (bind param) — no rows, no error.
	if got := search("from:' OR '1'='1"); len(got) != 0 {
		t.Fatalf("an injection in from: must match nothing, got %v", got)
	}
}

// TestSearchDateOperatorsIntegration covers the before:/after: date operators:
// day-exclusive bounds, combining into a window, and that a malformed or hostile
// date is treated as inert free text (never errors, never injects — Rule B/15).
func TestSearchDateOperatorsIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	ch, _ := store.CreateChannel(ctx, uniqueChannel())

	// Save three messages, then backdate each to a distinct day so the date
	// operators have something to slice (created_at defaults to now()).
	mk := func(body, day string) {
		m, err := store.Save(ctx, ch.ID, alice.ID, alice.Username, body)
		if err != nil {
			t.Fatalf("save %q: %v", body, err)
		}
		ts, err := time.ParseInLocation("2006-01-02", day, time.UTC)
		if err != nil {
			t.Fatalf("bad test day %q: %v", day, err)
		}
		// noon UTC so the row sits squarely inside its day, away from boundaries.
		if _, err := pool.Exec(ctx, `UPDATE messages SET created_at = $1 WHERE id = $2`,
			ts.Add(12*time.Hour), m.ID); err != nil {
			t.Fatalf("backdate %q: %v", body, err)
		}
	}
	mk("old message", "2020-01-01")
	mk("mid message", "2022-06-15")
	mk("new message", "2024-12-31")

	search := func(q string) []string {
		res, err := store.SearchMessages(ctx, ch.ID, q, 50)
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		out := make([]string, 0, len(res))
		for _, m := range res {
			out = append(out, m.Body)
		}
		return out
	}
	has := func(bodies []string, body string) bool {
		for _, b := range bodies {
			if b == body {
				return true
			}
		}
		return false
	}

	// before: keeps everything strictly before the named day.
	if got := search("before:2023-01-01"); !has(got, "old message") || !has(got, "mid message") || has(got, "new message") {
		t.Fatalf("before:2023-01-01 => %v", got)
	}
	// after: keeps everything strictly after the named day.
	if got := search("after:2023-01-01"); has(got, "old message") || has(got, "mid message") || !has(got, "new message") {
		t.Fatalf("after:2023-01-01 => %v", got)
	}
	// The named day itself is excluded by both bounds (day-exclusive semantics).
	if got := search("before:2022-06-15"); !has(got, "old message") || has(got, "mid message") || has(got, "new message") {
		t.Fatalf("before: boundary should exclude the named day, got %v", got)
	}
	if got := search("after:2022-06-15"); has(got, "old message") || has(got, "mid message") || !has(got, "new message") {
		t.Fatalf("after: boundary should exclude the named day, got %v", got)
	}
	// before: + after: combine into a window.
	if got := search("after:2021-01-01 before:2024-01-01"); !has(got, "mid message") || has(got, "old message") || has(got, "new message") {
		t.Fatalf("windowed search => %v", got)
	}
	// before: + free text — date bound AND body match.
	if got := search("before:2023-01-01 mid"); !has(got, "mid message") || has(got, "old message") {
		t.Fatalf("before:+text should match only the in-window 'mid' message, got %v", got)
	}
	// A malformed date is inert: treated as free text (no body contains it → empty), no error.
	if got := search("before:not-a-date"); len(got) != 0 {
		t.Fatalf("malformed before: must be free text matching nothing here, got %v", got)
	}
	// An injection inside a date operator is inert (strict parse → free text, no rows).
	if got := search("after:'; DROP TABLE messages;--"); len(got) != 0 {
		t.Fatalf("injection in after: must match nothing, got %v", got)
	}
	// Sanity: the table survived the injection attempt — a plain search still works.
	if got := search("message"); len(got) != 3 {
		t.Fatalf("after the injection probe all 3 messages must still be searchable, got %v", got)
	}
}

// memberColor pulls a member's surfaced top-role color from ListServerMembers.
func memberColor(t *testing.T, store *chat.Store, serverID, userID int64) string {
	t.Helper()
	members, err := store.ListServerMembers(context.Background(), serverID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members {
		if m.UserID == userID {
			return m.Color
		}
	}
	t.Fatalf("member %d not found in member list", userID)
	return ""
}

// memberRoleIDs pulls a member's surfaced cosmetic role ids from ListServerMembers.
func memberRoleIDs(t *testing.T, store *chat.Store, serverID, userID int64) []int64 {
	t.Helper()
	members, err := store.ListServerMembers(context.Background(), serverID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members {
		if m.UserID == userID {
			return m.RoleIds
		}
	}
	t.Fatalf("member %d not found in member list", userID)
	return nil
}

// TestServerCustomRolesIntegration exercises v0.7 custom colored roles (slice 1, backend):
// admin-gated CRUD + assignment, the top-role color surfaced in ListServerMembers, and the
// owner/admin/member permission tier left completely untouched.
func TestServerCustomRolesIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Color Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Mutations are admin-gated; a plain member can't create a role.
	if _, err := store.CreateServerRole(ctx, srv.ID, member.ID, "Mod", "#ff0000", false); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("member create role err = %v, want ErrForbidden", err)
	}
	// Input validation (admin, but bad data).
	if _, err := store.CreateServerRole(ctx, srv.ID, owner.ID, "Mod", "red", false); !errors.Is(err, chat.ErrInvalidColor) {
		t.Fatalf("bad color err = %v, want ErrInvalidColor", err)
	}
	if _, err := store.CreateServerRole(ctx, srv.ID, owner.ID, "   ", "#fff", false); !errors.Is(err, chat.ErrInvalidRoleName) {
		t.Fatalf("blank name err = %v, want ErrInvalidRoleName", err)
	}

	// Owner creates two roles; positions auto-increment so the second sits on top.
	low, err := store.CreateServerRole(ctx, srv.ID, owner.ID, "Member+", "#3498db", false)
	if err != nil {
		t.Fatalf("create low role: %v", err)
	}
	high, err := store.CreateServerRole(ctx, srv.ID, owner.ID, "Moderator", "#e91e63", false)
	if err != nil {
		t.Fatalf("create high role: %v", err)
	}
	if high.Position <= low.Position {
		t.Fatalf("second role should have a higher position: low=%d high=%d", low.Position, high.Position)
	}

	// List returns both, highest position first.
	roles, err := store.ListServerRoles(ctx, srv.ID)
	if err != nil || len(roles) != 2 || roles[0].ID != high.ID {
		t.Fatalf("ListServerRoles = %+v err %v (want high first)", roles, err)
	}

	// Assign is admin-gated; a non-member target is rejected.
	if err := store.AssignServerRole(ctx, srv.ID, member.ID, member.ID, low.ID); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("member assign err = %v, want ErrForbidden", err)
	}
	if err := store.AssignServerRole(ctx, srv.ID, owner.ID, stranger.ID, low.ID); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("assign non-member err = %v, want ErrUserNotFound", err)
	}

	// Assign the LOW role → the member's surfaced color becomes the low color.
	if err := store.AssignServerRole(ctx, srv.ID, owner.ID, member.ID, low.ID); err != nil {
		t.Fatalf("assign low: %v", err)
	}
	if c := memberColor(t, store, srv.ID, member.ID); c != low.Color {
		t.Fatalf("after low assign, color = %q, want %q", c, low.Color)
	}
	// Also assign the HIGH role → the top (highest-position) role wins the color.
	if err := store.AssignServerRole(ctx, srv.ID, owner.ID, member.ID, high.ID); err != nil {
		t.Fatalf("assign high: %v", err)
	}
	if c := memberColor(t, store, srv.ID, member.ID); c != high.Color {
		t.Fatalf("with both roles, color = %q, want top %q", c, high.Color)
	}
	// roleIds are surfaced for the client, highest position first.
	if ids := memberRoleIDs(t, store, srv.ID, member.ID); len(ids) != 2 || ids[0] != high.ID || ids[1] != low.ID {
		t.Fatalf("member roleIds = %v, want [%d %d] (top first)", ids, high.ID, low.ID)
	}

	// Hoist (slice 3) round-trips through create + update + list.
	hoisted, err := store.CreateServerRole(ctx, srv.ID, owner.ID, "Staff", "#f0b232", true)
	if err != nil || !hoisted.Hoist {
		t.Fatalf("create hoisted role: %+v err %v", hoisted, err)
	}
	roleByID := func(id int64) chat.Role {
		rs, _ := store.ListServerRoles(ctx, srv.ID)
		for _, r := range rs {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("role %d not listed", id)
		return chat.Role{}
	}
	if !roleByID(hoisted.ID).Hoist {
		t.Fatal("listed role should report hoist=true")
	}
	if err := store.UpdateServerRole(ctx, srv.ID, owner.ID, hoisted.ID, "Staff", "#f0b232", false); err != nil {
		t.Fatalf("un-hoist: %v", err)
	}
	if roleByID(hoisted.ID).Hoist {
		t.Fatal("role should report hoist=false after update")
	}

	// Recolor the top role → reflected in the member's color.
	if err := store.UpdateServerRole(ctx, srv.ID, owner.ID, high.ID, "Moderator", "#00ff00", false); err != nil {
		t.Fatalf("update role: %v", err)
	}
	if c := memberColor(t, store, srv.ID, member.ID); c != "#00ff00" {
		t.Fatalf("after recolor, color = %q, want #00ff00", c)
	}

	// Delete the top role → its assignment cascades, color falls back to the low role.
	if err := store.DeleteServerRole(ctx, srv.ID, owner.ID, high.ID); err != nil {
		t.Fatalf("delete role: %v", err)
	}
	if c := memberColor(t, store, srv.ID, member.ID); c != low.Color {
		t.Fatalf("after deleting the top role, color = %q, want low %q", c, low.Color)
	}

	// An unknown / cross-server role id is ErrRoleNotFound on update/delete/assign.
	if err := store.UpdateServerRole(ctx, srv.ID, owner.ID, 1<<40, "X", "#fff", false); !errors.Is(err, chat.ErrRoleNotFound) {
		t.Fatalf("update unknown role err = %v, want ErrRoleNotFound", err)
	}

	// The permission tier is untouched by any of this.
	if role, _ := store.ServerRole(ctx, srv.ID, member.ID); role != "member" {
		t.Fatalf("custom roles must not change the permission tier: member role = %q", role)
	}
}

// TestMessageAuthorColorIntegration proves v0.7 message-author coloring: a message in a
// server channel carries the author's top-role color (both the live Save path and Recent),
// reflecting CURRENT role assignment; a message in a non-server (DM) channel carries none.
func TestMessageAuthorColorIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()

	srv, err := store.CreateServer(ctx, owner.ID, "Color Msg Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	ch, err := store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	role, err := store.CreateServerRole(ctx, srv.ID, owner.ID, "Mod", "#e67e22", false)
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	// Before assignment: a posted message has no author color.
	m1, err := store.Save(ctx, ch.ID, owner.ID, owner.Username, "hi uncolored")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if m1.AuthorColor != "" {
		t.Fatalf("uncolored author should have no color, got %q", m1.AuthorColor)
	}

	// After assignment: the live Save path AND Recent both carry the role color.
	if err := store.AssignServerRole(ctx, srv.ID, owner.ID, owner.ID, role.ID); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	m2, err := store.Save(ctx, ch.ID, owner.ID, owner.Username, "hi colored")
	if err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if m2.AuthorColor != role.Color {
		t.Fatalf("live Save author color = %q, want %q", m2.AuthorColor, role.Color)
	}
	recent, err := store.Recent(ctx, ch.ID, owner.ID, 50)
	if err != nil || len(recent) == 0 {
		t.Fatalf("recent: %v (n=%d)", err, len(recent))
	}
	if last := recent[len(recent)-1]; last.AuthorColor != role.Color {
		t.Fatalf("Recent latest author color = %q, want %q", last.AuthorColor, role.Color)
	}

	// A DM (non-server) channel message carries no author color.
	bob := regUser(t, pool)
	dm, err := store.CreateOrGetDM(ctx, owner.ID, bob.ID)
	if err != nil {
		t.Fatalf("dm: %v", err)
	}
	dmMsg, err := store.Save(ctx, dm.ID, owner.ID, owner.Username, "dm hi")
	if err != nil {
		t.Fatalf("dm save: %v", err)
	}
	if dmMsg.AuthorColor != "" {
		t.Fatalf("DM message should have no author color, got %q", dmMsg.AuthorColor)
	}
}

func TestServerRolesIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Role Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Roles after creation/join: creator=owner, joiner=member.
	if role, _ := store.ServerRole(ctx, srv.ID, owner.ID); role != "owner" {
		t.Fatalf("creator role = %q, want owner", role)
	}
	if role, _ := store.ServerRole(ctx, srv.ID, member.ID); role != "member" {
		t.Fatalf("joiner role = %q, want member", role)
	}
	if ok, _ := store.IsServerAdmin(ctx, srv.ID, owner.ID); !ok {
		t.Fatal("owner should be admin")
	}
	if ok, _ := store.IsServerAdmin(ctx, srv.ID, member.ID); ok {
		t.Fatal("a plain member should not be admin")
	}

	// Only the owner can change roles; role must be valid; owner can't self-change;
	// target must be a member.
	if err := store.SetServerRole(ctx, srv.ID, member.ID, owner.ID, "member"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-owner SetServerRole err = %v, want ErrForbidden", err)
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, member.ID, "superadmin"); !errors.Is(err, chat.ErrInvalidRole) {
		t.Fatalf("invalid role err = %v, want ErrInvalidRole", err)
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, owner.ID, "member"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("owner self-change err = %v, want ErrForbidden", err)
	}
	stranger := regUser(t, pool)
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, stranger.ID, "admin"); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("promote non-member err = %v, want ErrUserNotFound", err)
	}

	// Owner promotes the member to admin → they become an admin.
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, member.ID, "admin"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if role, _ := store.ServerRole(ctx, srv.ID, member.ID); role != "admin" {
		t.Fatalf("promoted role = %q, want admin", role)
	}
	if ok, _ := store.IsServerAdmin(ctx, srv.ID, member.ID); !ok {
		t.Fatal("promoted member should be admin")
	}

	// ListServerMembers reflects the roles, owner first.
	ms, err := store.ListServerMembers(ctx, srv.ID)
	if err != nil || len(ms) != 2 {
		t.Fatalf("ListServerMembers = %+v err %v (want 2 members)", ms, err)
	}
	if ms[0].Role != "owner" || ms[0].UserID != owner.ID {
		t.Fatalf("first listed member should be the owner: %+v", ms[0])
	}
	var sawAdmin bool
	for _, m := range ms {
		if m.UserID == member.ID && m.Role == "admin" {
			sawAdmin = true
		}
	}
	if !sawAdmin {
		t.Fatalf("the promoted member should be listed as admin: %+v", ms)
	}

	// ListServers carries the requesting user's own role (drives the moderation UI).
	if sl, _ := store.ListServers(ctx, owner.ID); len(sl) == 0 || sl[0].Role != "owner" {
		t.Fatalf("ListServers should report the owner's role: %+v", sl)
	}
	if sl, _ := store.ListServers(ctx, member.ID); len(sl) == 0 || sl[0].Role != "admin" {
		t.Fatalf("ListServers should report the member's (promoted) role: %+v", sl)
	}
}

func TestUnreadChannelsIntegration(t *testing.T) {
	store, pool, me := setup(t)
	ctx := context.Background()
	other := regUser(t, pool)

	// helpers over Unreads: is a channel unread, and how many mentions it has for `me`.
	unreads := func() []chat.ChannelUnread {
		u, err := store.Unreads(ctx, me.ID, me.Username)
		if err != nil {
			t.Fatalf("Unreads: %v", err)
		}
		return u
	}
	has := func(_ []int64, id int64) bool { // signature kept; ids arg ignored
		for _, u := range unreads() {
			if u.ChannelID == id {
				return true
			}
		}
		return false
	}
	mentions := func(id int64) int {
		for _, u := range unreads() {
			if u.ChannelID == id {
				return u.Mentions
			}
		}
		return -1 // not unread
	}
	unread := func() []int64 { return nil } // legacy no-op (has() recomputes)

	ch, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	// Empty channel → not unread.
	if has(unread(), ch.ID) {
		t.Fatal("a channel with no messages should not be unread")
	}
	// Someone else posts → unread for me.
	if _, err := store.Save(ctx, ch.ID, other.ID, other.Username, "hi"); err != nil {
		t.Fatalf("other posts: %v", err)
	}
	if !has(unread(), ch.ID) {
		t.Fatal("a channel with a new message from someone else should be unread")
	}
	// My OWN message never self-unreads (fresh channel only I post in).
	mine, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if _, err := store.Save(ctx, mine.ID, me.ID, me.Username, "mine"); err != nil {
		t.Fatalf("self post: %v", err)
	}
	if has(unread(), mine.ID) {
		t.Fatal("my own message should not mark the channel unread for me")
	}
	// Mark read → no longer unread; a newer message → unread again.
	if err := store.MarkChannelRead(ctx, ch.ID, me.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if has(unread(), ch.ID) {
		t.Fatal("after marking read the channel should not be unread")
	}
	if _, err := store.Save(ctx, ch.ID, other.ID, other.Username, "again"); err != nil {
		t.Fatalf("other posts again: %v", err)
	}
	if !has(unread(), ch.ID) {
		t.Fatal("a message after the read marker should make the channel unread again")
	}
	// Access scoping: a server channel I'm NOT a member of is never unread for me.
	srv, err := store.CreateServer(ctx, other.ID, "Other Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	sch, err := store.CreateServerChannel(ctx, srv.ID, "secret")
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}
	if _, err := store.Save(ctx, sch.ID, other.ID, other.Username, "secret"); err != nil {
		t.Fatalf("post in server channel: %v", err)
	}
	if has(unread(), sch.ID) {
		t.Fatal("a server channel I can't access must never surface as unread (access scoping)")
	}

	// Mention counting (red badge). Fresh channel; mark it read so only new posts count.
	mc, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create mention channel: %v", err)
	}
	if err := store.MarkChannelRead(ctx, mc.ID, me.ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	post := func(body string) {
		if _, err := store.Save(ctx, mc.ID, other.ID, other.Username, body); err != nil {
			t.Fatalf("post %q: %v", body, err)
		}
	}
	post("hello @" + me.Username + " how are you")    // a real mention of me
	post("hey @" + me.Username + "extra not a match") // @me+extra → different token, NOT me
	post("ping @everyone please")                     // @everyone counts
	post("just a normal message with no ping")        // no mention
	if got := mentions(mc.ID); got != 2 {
		t.Fatalf("mention count = %d, want 2 (one @me + one @everyone; @me+suffix excluded)", got)
	}
	// My OWN message mentioning someone doesn't badge me.
	mc2, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create mention channel 2: %v", err)
	}
	if _, err := store.Save(ctx, mc2.ID, me.ID, me.Username, "@everyone from me"); err != nil {
		t.Fatalf("self mention post: %v", err)
	}
	if mentions(mc2.ID) != -1 {
		t.Fatal("my own @everyone must not badge me (channel shouldn't even be unread for me)")
	}
}

// Per-channel mute: a muted channel drops out of Unreads (so its sidebar dot/mention/tab
// badge all vanish), is per-user, idempotent, and reversible.
func TestChannelMuteIntegration(t *testing.T) {
	store, pool, me := setup(t)
	ctx := context.Background()
	other := regUser(t, pool)

	isUnread := func(id int64) bool {
		u, err := store.Unreads(ctx, me.ID, me.Username)
		if err != nil {
			t.Fatalf("Unreads: %v", err)
		}
		for _, c := range u {
			if c.ChannelID == id {
				return true
			}
		}
		return false
	}
	muted := func(uid int64) map[int64]bool {
		ids, err := store.MutedChannelIDs(ctx, uid)
		if err != nil {
			t.Fatalf("MutedChannelIDs: %v", err)
		}
		m := map[int64]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}

	ch, err := store.CreateChannel(ctx, uniqueChannel())
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if _, err := store.Save(ctx, ch.ID, other.ID, other.Username, "noisy"); err != nil {
		t.Fatalf("other posts: %v", err)
	}
	if !isUnread(ch.ID) {
		t.Fatal("precondition: the channel should be unread before muting")
	}

	// Mute → no longer surfaces as unread, and shows in my muted list.
	if err := store.MuteChannel(ctx, ch.ID, me.ID); err != nil {
		t.Fatalf("MuteChannel: %v", err)
	}
	if isUnread(ch.ID) {
		t.Fatal("a muted channel must NOT surface as unread")
	}
	if !muted(me.ID)[ch.ID] {
		t.Fatal("MutedChannelIDs should list the muted channel")
	}
	// Idempotent: muting again is a no-op (no error, still exactly one mute).
	if err := store.MuteChannel(ctx, ch.ID, me.ID); err != nil {
		t.Fatalf("MuteChannel (idempotent): %v", err)
	}
	// Per-user: `other` did not mute it, so it's not in THEIR muted list.
	if muted(other.ID)[ch.ID] {
		t.Fatal("mute must be per-user — other's list must not include my mute")
	}

	// Unmute → it surfaces as unread again and leaves my muted list.
	if err := store.UnmuteChannel(ctx, ch.ID, me.ID); err != nil {
		t.Fatalf("UnmuteChannel: %v", err)
	}
	if !isUnread(ch.ID) {
		t.Fatal("after unmuting, the channel should be unread again")
	}
	if muted(me.ID)[ch.ID] {
		t.Fatal("after unmuting, the channel must leave the muted list")
	}
}

// Profile (About Me + pronouns): set, cap, clear, and surface through ListServerMembers.
func TestUserProfileIntegration(t *testing.T) {
	store, _, owner := setup(t)
	ctx := context.Background()

	srv, err := store.CreateServer(ctx, owner.ID, "Profile Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	memberAbout := func() (about, pronouns string) {
		ms, err := store.ListServerMembers(ctx, srv.ID)
		if err != nil {
			t.Fatalf("ListServerMembers: %v", err)
		}
		for _, m := range ms {
			if m.UserID == owner.ID {
				return m.About, m.Pronouns
			}
		}
		t.Fatal("owner not in member list")
		return "", ""
	}

	// Default: empty.
	if a, p := memberAbout(); a != "" || p != "" {
		t.Fatalf("fresh profile = (%q,%q), want empty", a, p)
	}
	// Set → surfaces, trimmed.
	if err := store.SetUserProfile(ctx, owner.ID, "  Building Opencord  ", " they/them "); err != nil {
		t.Fatalf("SetUserProfile: %v", err)
	}
	if a, p := memberAbout(); a != "Building Opencord" || p != "they/them" {
		t.Fatalf("after set = (%q,%q), want trimmed about+pronouns", a, p)
	}
	// Cap: an over-long about is truncated to 190 runes (Discord's About Me cap); pronouns to 40.
	const wantAboutCap, wantPronCap = 190, 40
	longAbout := strings.Repeat("x", wantAboutCap+50)
	longPron := strings.Repeat("y", wantPronCap+20)
	if err := store.SetUserProfile(ctx, owner.ID, longAbout, longPron); err != nil {
		t.Fatalf("SetUserProfile (long): %v", err)
	}
	if a, p := memberAbout(); len([]rune(a)) != wantAboutCap || len([]rune(p)) != wantPronCap {
		t.Fatalf("caps not enforced: about=%d (want %d), pronouns=%d (want %d)",
			len([]rune(a)), wantAboutCap, len([]rune(p)), wantPronCap)
	}
	// Clear: empty/whitespace clears back to none.
	if err := store.SetUserProfile(ctx, owner.ID, "   ", ""); err != nil {
		t.Fatalf("SetUserProfile (clear): %v", err)
	}
	if a, p := memberAbout(); a != "" || p != "" {
		t.Fatalf("after clear = (%q,%q), want empty", a, p)
	}

	// GetUserProfile (the public profile fetch behind the profile card): returns the set
	// values, and a missing user is ErrUserNotFound (so the endpoint 404s, not 500s).
	if err := store.SetUserProfile(ctx, owner.ID, "hello world", "she/her"); err != nil {
		t.Fatalf("SetUserProfile: %v", err)
	}
	prof, err := store.GetUserProfile(ctx, owner.ID)
	if err != nil {
		t.Fatalf("GetUserProfile: %v", err)
	}
	if prof.UserID != owner.ID || prof.Username != owner.Username ||
		prof.About != "hello world" || prof.Pronouns != "she/her" {
		t.Fatalf("GetUserProfile = %+v, want owner's public profile", prof)
	}
	if _, err := store.GetUserProfile(ctx, 999_999_999); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("GetUserProfile(missing) err = %v, want ErrUserNotFound", err)
	}
}

func TestUserStatusIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)
	srv, err := store.CreateServer(ctx, owner.ID, "Status Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	memberOf := func(uid int64) chat.ServerMember {
		ms, err := store.ListServerMembers(ctx, srv.ID)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		for _, m := range ms {
			if m.UserID == uid {
				return m
			}
		}
		t.Fatalf("member %d not listed", uid)
		return chat.ServerMember{}
	}

	// Default: no status, no emoji.
	if m := memberOf(member.ID); m.Status != "" || m.StatusEmoji != "" {
		t.Fatalf("default status/emoji should be empty, got %q / %q", m.Status, m.StatusEmoji)
	}
	// Set → trimmed and reflected in the member list (status + emoji).
	if err := store.SetUserStatus(ctx, member.ID, "  building Opencord  ", "  🚀  "); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if m := memberOf(member.ID); m.Status != "building Opencord" || m.StatusEmoji != "🚀" {
		t.Fatalf("status/emoji = %q / %q, want trimmed 'building Opencord' / '🚀'", m.Status, m.StatusEmoji)
	}
	// Over-long → status capped to 128 runes, emoji capped to 16 runes (Rule B).
	if err := store.SetUserStatus(ctx, member.ID, strings.Repeat("x", 500), strings.Repeat("😀", 99)); err != nil {
		t.Fatalf("set long: %v", err)
	}
	if m := memberOf(member.ID); len([]rune(m.Status)) != 128 || len([]rune(m.StatusEmoji)) != 16 {
		t.Fatalf("over-long capped to status=%d emoji=%d runes, want 128 / 16",
			len([]rune(m.Status)), len([]rune(m.StatusEmoji)))
	}
	// Emoji can be set alone (no status line).
	if err := store.SetUserStatus(ctx, member.ID, "", "🎮"); err != nil {
		t.Fatalf("set emoji only: %v", err)
	}
	if m := memberOf(member.ID); m.Status != "" || m.StatusEmoji != "🎮" {
		t.Fatalf("emoji-only set => status %q emoji %q, want '' / '🎮'", m.Status, m.StatusEmoji)
	}
	// Whitespace clears both.
	if err := store.SetUserStatus(ctx, member.ID, "   ", "   "); err != nil {
		t.Fatalf("clear status: %v", err)
	}
	if m := memberOf(member.ID); m.Status != "" || m.StatusEmoji != "" {
		t.Fatalf("status/emoji should be cleared, got %q / %q", m.Status, m.StatusEmoji)
	}
}

func TestRemoveServerMemberIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	admin := regUser(t, pool)
	admin2 := regUser(t, pool)
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Kick Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, admin2, member} {
		if err := store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member %d: %v", u.ID, err)
		}
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, admin2.ID, "admin"); err != nil {
		t.Fatalf("promote admin2: %v", err)
	}
	ch, err := store.CreateServerChannel(ctx, srv.ID, "kick-chan")
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}

	// --- forbidden / adversarial cases: none of these may remove anyone (Rule 15) ---
	cases := []struct {
		name          string
		actor, target int64
		want          error
	}{
		{"plain member can't kick", member.ID, admin.ID, chat.ErrForbidden},
		{"non-member can't kick", stranger.ID, member.ID, chat.ErrForbidden},
		{"nobody can kick the owner", admin.ID, owner.ID, chat.ErrForbidden},
		{"can't kick yourself", admin.ID, admin.ID, chat.ErrForbidden},
		{"an admin can't kick a fellow admin", admin.ID, admin2.ID, chat.ErrForbidden},
		{"kicking a non-member target → not found", owner.ID, stranger.ID, chat.ErrUserNotFound},
	}
	for _, c := range cases {
		if err := store.RemoveServerMember(ctx, srv.ID, c.actor, c.target); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Guard: every member above is still a member (no rejected call removed anyone).
	for _, u := range []auth.User{admin, admin2, member} {
		if ok, _ := store.IsServerMember(ctx, srv.ID, u.ID); !ok {
			t.Fatalf("user %d should still be a member after the rejected kicks", u.ID)
		}
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, member.ID); !ok {
		t.Fatal("member should still access the server channel after the rejected kicks")
	}

	// --- happy paths ---
	// An admin CAN kick a plain member, who then loses membership AND channel access.
	if err := store.RemoveServerMember(ctx, srv.ID, admin.ID, member.ID); err != nil {
		t.Fatalf("admin kicking member: %v", err)
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, member.ID); ok {
		t.Fatal("kicked member should no longer be a server member")
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, member.ID); ok {
		t.Fatal("kicked member should lose access to the server's channels")
	}
	// The owner CAN kick an admin.
	if err := store.RemoveServerMember(ctx, srv.ID, owner.ID, admin2.ID); err != nil {
		t.Fatalf("owner kicking admin: %v", err)
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, admin2.ID); ok {
		t.Fatal("owner-kicked admin should no longer be a member")
	}
}

// TestLeaveServerIntegration proves voluntary self-removal: any non-owner member may
// leave (losing membership + channel access), the owner may NOT (must delete/transfer),
// and a non-member / unknown server is a 404-class error.
func TestLeaveServerIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	admin := regUser(t, pool)
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Leave Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member %d: %v", u.ID, err)
		}
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	ch, err := store.CreateServerChannel(ctx, srv.ID, "leave-chan")
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}

	// --- adversarial / edge cases ---
	if err := store.LeaveServer(ctx, srv.ID, owner.ID); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("owner leaving should be forbidden, got %v", err)
	}
	if err := store.LeaveServer(ctx, srv.ID, stranger.ID); !errors.Is(err, chat.ErrServerNotFound) {
		t.Fatalf("non-member leaving should be not-found, got %v", err)
	}
	if err := store.LeaveServer(ctx, srv.ID+99999, member.ID); !errors.Is(err, chat.ErrServerNotFound) {
		t.Fatalf("leaving an unknown server should be not-found, got %v", err)
	}
	// Guards: nobody left after the rejected calls.
	if ok, _ := store.IsServerMember(ctx, srv.ID, owner.ID); !ok {
		t.Fatal("owner should still be a member after the rejected leave")
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, member.ID); !ok {
		t.Fatal("member should still be a member after the rejected stranger/unknown leaves")
	}

	// --- happy paths: a plain member and an admin can both leave ---
	if err := store.LeaveServer(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("member leaving: %v", err)
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, member.ID); ok {
		t.Fatal("a member who left should no longer be a member")
	}
	if ok, _ := store.CanAccessChannel(ctx, ch.ID, member.ID); ok {
		t.Fatal("a member who left should lose access to the server's channels")
	}
	if err := store.LeaveServer(ctx, srv.ID, admin.ID); err != nil {
		t.Fatalf("admin leaving: %v", err)
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, admin.ID); ok {
		t.Fatal("an admin who left should no longer be a member")
	}
	// Leaving again (now a non-member) is a 404-class error.
	if err := store.LeaveServer(ctx, srv.ID, member.ID); !errors.Is(err, chat.ErrServerNotFound) {
		t.Fatalf("re-leaving as a non-member should be not-found, got %v", err)
	}
}

// TestRenameServerIntegration proves the rename authz matrix: admin+ may rename, a
// plain member / non-member may not, and an unknown server 404s — and that a successful
// rename actually lands in the stored row.
func TestRenameServerIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	admin := regUser(t, pool)
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Original Name")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member %d: %v", u.ID, err)
		}
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	// --- adversarial: none of these may rename (Rule 15) ---
	for _, c := range []struct {
		name  string
		actor int64
		srvID int64
		want  error
	}{
		{"plain member can't rename", member.ID, srv.ID, chat.ErrForbidden},
		{"non-member can't rename", stranger.ID, srv.ID, chat.ErrForbidden},
		{"unknown server → not found", owner.ID, srv.ID + 99999, chat.ErrServerNotFound},
	} {
		if _, err := store.RenameServer(ctx, c.srvID, c.actor, "Hijacked"); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Guard: the name is untouched after the rejected renames.
	if servers, _ := store.ListServers(ctx, owner.ID); len(servers) != 1 || servers[0].Name != "Original Name" {
		t.Fatalf("name should be unchanged after rejected renames: %+v", servers)
	}

	// --- happy paths: owner and admin can both rename ---
	if got, err := store.RenameServer(ctx, srv.ID, owner.ID, "Owner Renamed"); err != nil || got.Name != "Owner Renamed" {
		t.Fatalf("owner rename: got %+v err %v", got, err)
	}
	if got, err := store.RenameServer(ctx, srv.ID, admin.ID, "Admin Renamed"); err != nil || got.Name != "Admin Renamed" {
		t.Fatalf("admin rename: got %+v err %v", got, err)
	}
	if servers, _ := store.ListServers(ctx, member.ID); len(servers) != 1 || servers[0].Name != "Admin Renamed" {
		t.Fatalf("member should see the renamed server: %+v", servers)
	}
}

// TestDeleteServerIntegration proves delete is owner-only and cascades correctly:
// an admin/member/non-member can't delete; the owner's delete removes the server, its
// members, channels, and messages, while leaving the global #general (and its messages)
// intact. A successful delete also proves the messages-then-cascade tx ran — without it
// the channel cascade would hit the messages FK and the delete would error.
func TestDeleteServerIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	admin := regUser(t, pool)
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	// A message in the global #general must survive the server delete.
	publics, err := store.ListChannels(ctx)
	if err != nil || len(publics) == 0 {
		t.Fatalf("list global channels: %v (%d)", err, len(publics))
	}
	generalID := publics[0].ID
	for _, c := range publics {
		if c.Name == "general" {
			generalID = c.ID
		}
	}
	if _, err := store.Save(ctx, generalID, owner.ID, owner.Username, "global survives"); err != nil {
		t.Fatalf("save global message: %v", err)
	}

	srv, err := store.CreateServer(ctx, owner.ID, "Doomed Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member %d: %v", u.ID, err)
		}
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	ch, err := store.CreateServerChannel(ctx, srv.ID, "doomed-chan")
	if err != nil {
		t.Fatalf("create server channel: %v", err)
	}
	// Messages in the server channel are the reason the tx must delete messages before
	// the channel cascade (messages.channel_id has no ON DELETE CASCADE).
	for _, body := range []string{"msg one", "msg two", "msg three"} {
		if _, err := store.Save(ctx, ch.ID, member.ID, member.Username, body); err != nil {
			t.Fatalf("save server message: %v", err)
		}
	}

	// --- adversarial: only the owner may delete (Rule 15) ---
	for _, c := range []struct {
		name  string
		actor int64
		srvID int64
		want  error
	}{
		{"admin can't delete", admin.ID, srv.ID, chat.ErrForbidden},
		{"plain member can't delete", member.ID, srv.ID, chat.ErrForbidden},
		{"non-member can't delete", stranger.ID, srv.ID, chat.ErrForbidden},
		{"unknown server → not found", owner.ID, srv.ID + 99999, chat.ErrServerNotFound},
	} {
		if err := store.DeleteServer(ctx, c.srvID, c.actor); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Guard: the server still exists after the rejected deletes.
	if servers, _ := store.ListServers(ctx, owner.ID); len(servers) != 1 {
		t.Fatalf("server should survive rejected deletes: %+v", servers)
	}

	// --- happy path: the owner deletes it, cascading members/channels/messages ---
	if err := store.DeleteServer(ctx, srv.ID, owner.ID); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if servers, _ := store.ListServers(ctx, owner.ID); len(servers) != 0 {
		t.Fatalf("server should be gone for the owner: %+v", servers)
	}
	if ok, _ := store.IsServerMember(ctx, srv.ID, member.ID); ok {
		t.Fatal("members should be gone after the server delete")
	}
	if chans, _ := store.ListServerChannels(ctx, srv.ID); len(chans) != 0 {
		t.Fatalf("server channels should be gone: %+v", chans)
	}
	// Messages in the deleted channel are gone (proves the cascade tx ran).
	var msgCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE channel_id = $1`, ch.ID).Scan(&msgCount); err != nil {
		t.Fatalf("count server messages: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("server channel messages should be deleted, found %d", msgCount)
	}
	// The global #general and its message are untouched.
	var globalCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE channel_id = $1`, generalID).Scan(&globalCount); err != nil {
		t.Fatalf("count global messages: %v", err)
	}
	if globalCount == 0 {
		t.Fatal("global #general message must survive the server delete")
	}
}

// TestTransferServerOwnershipIntegration proves only the owner may transfer, only to a
// different existing member, and that a successful transfer swaps the roles (new owner,
// old owner → admin) and updates servers.owner_id.
func TestTransferServerOwnershipIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	admin := regUser(t, pool)
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Transfer Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	for _, u := range []auth.User{admin, member} {
		if err := store.AddServerMember(ctx, srv.ID, u.ID); err != nil {
			t.Fatalf("add member %d: %v", u.ID, err)
		}
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, admin.ID, "admin"); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	// --- adversarial cases ---
	for _, c := range []struct {
		name          string
		actor, target int64
		want          error
	}{
		{"admin can't transfer", admin.ID, member.ID, chat.ErrForbidden},
		{"member can't transfer", member.ID, admin.ID, chat.ErrForbidden},
		{"non-member can't transfer", stranger.ID, member.ID, chat.ErrForbidden},
		{"owner can't transfer to self", owner.ID, owner.ID, chat.ErrForbidden},
		{"owner can't transfer to a non-member", owner.ID, stranger.ID, chat.ErrUserNotFound},
	} {
		if err := store.TransferServerOwnership(ctx, srv.ID, c.actor, c.target); !errors.Is(err, c.want) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	// Guard: ownership unchanged after the rejected transfers.
	if r, _ := store.ServerRole(ctx, srv.ID, owner.ID); r != "owner" {
		t.Fatalf("owner should still be owner after rejected transfers, got %q", r)
	}

	// --- happy path: owner → member ---
	if err := store.TransferServerOwnership(ctx, srv.ID, owner.ID, member.ID); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if r, _ := store.ServerRole(ctx, srv.ID, member.ID); r != "owner" {
		t.Fatalf("target should now be owner, got %q", r)
	}
	if r, _ := store.ServerRole(ctx, srv.ID, owner.ID); r != "admin" {
		t.Fatalf("old owner should now be admin, got %q", r)
	}
	// servers.owner_id reflects the new owner (exposed as Server.OwnerID).
	srvs, _ := store.ListServers(ctx, member.ID)
	if len(srvs) != 1 || srvs[0].OwnerID != member.ID {
		t.Fatalf("servers.owner_id should be the new owner: %+v", srvs)
	}
	// The old owner (now an admin) can no longer transfer; the new owner can transfer back.
	if err := store.TransferServerOwnership(ctx, srv.ID, owner.ID, admin.ID); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("ex-owner (admin) should not be able to transfer, got %v", err)
	}
	if err := store.TransferServerOwnership(ctx, srv.ID, member.ID, owner.ID); err != nil {
		t.Fatalf("new owner transferring back: %v", err)
	}
	if r, _ := store.ServerRole(ctx, srv.ID, owner.ID); r != "owner" {
		t.Fatalf("ownership should have transferred back, got %q", r)
	}
}

func TestMessageModerationIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, _ := store.CreateServer(ctx, owner.ID, "Mod Guild")
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ch, _ := store.CreateServerChannel(ctx, srv.ID, "general")

	// The member posts; a non-admin (a non-member stranger) can't delete it.
	m, err := store.Save(ctx, ch.ID, member.ID, member.Username, "member message")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := store.DeleteMessage(ctx, m.ID, stranger.ID); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("non-admin delete err = %v, want ErrMessageNotFound", err)
	}
	// The owner (admin) CAN delete the member's message (moderation).
	if del, err := store.DeleteMessage(ctx, m.ID, owner.ID); err != nil || !del.Deleted || del.ChannelID != ch.ID {
		t.Fatalf("owner moderation delete failed: %+v err %v", del, err)
	}

	// A plain member can't moderate the owner's message — until promoted to admin.
	om, _ := store.Save(ctx, ch.ID, owner.ID, owner.Username, "owner message")
	if _, err := store.DeleteMessage(ctx, om.ID, member.ID); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("plain-member moderation err = %v, want ErrMessageNotFound", err)
	}
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, member.ID, "admin"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := store.DeleteMessage(ctx, om.ID, member.ID); err != nil {
		t.Fatalf("promoted admin should be able to moderate: %v", err)
	}

	// In a public (serverless) channel there is no moderation: a non-author can't delete.
	pub, _ := store.CreateChannel(ctx, uniqueChannel())
	pm, _ := store.Save(ctx, pub.ID, member.ID, member.Username, "public message")
	if _, err := store.DeleteMessage(ctx, pm.ID, owner.ID); !errors.Is(err, chat.ErrMessageNotFound) {
		t.Fatalf("public-channel non-author delete err = %v, want ErrMessageNotFound", err)
	}
	if _, err := store.DeleteMessage(ctx, pm.ID, member.ID); err != nil {
		t.Fatalf("author should delete their own public message: %v", err)
	}
}

func TestChannelPostPolicyIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)

	srv, _ := store.CreateServer(ctx, owner.ID, "Policy Guild")
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ch, _ := store.CreateServerChannel(ctx, srv.ID, "general")

	// Default 'everyone': a member can post.
	if _, err := store.Save(ctx, ch.ID, member.ID, member.Username, "hi"); err != nil {
		t.Fatalf("member should post in an 'everyone' channel: %v", err)
	}

	// Invalid policy + non-admin can't set the policy.
	if err := store.SetChannelPostPolicy(ctx, ch.ID, owner.ID, "nope"); !errors.Is(err, chat.ErrInvalidPolicy) {
		t.Fatalf("invalid policy err = %v, want ErrInvalidPolicy", err)
	}
	if err := store.SetChannelPostPolicy(ctx, ch.ID, member.ID, "admins"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-admin SetChannelPostPolicy err = %v, want ErrForbidden", err)
	}

	// Owner makes it admin-only (read-only for members).
	if err := store.SetChannelPostPolicy(ctx, ch.ID, owner.ID, "admins"); err != nil {
		t.Fatalf("owner set policy: %v", err)
	}
	// ListServerChannels reflects the policy (drives the read-only UI badge/composer).
	if sc, _ := store.ListServerChannels(ctx, srv.ID); len(sc) == 0 || sc[0].PostPolicy != "admins" {
		t.Fatalf("ListServerChannels should report the channel's post policy: %+v", sc)
	}
	if ok, _ := store.CanPostInChannel(ctx, ch.ID, member.ID); ok {
		t.Fatal("member should NOT be able to post in an admins-only channel")
	}
	if ok, _ := store.CanPostInChannel(ctx, ch.ID, owner.ID); !ok {
		t.Fatal("owner should be able to post in an admins-only channel")
	}
	if _, err := store.Save(ctx, ch.ID, member.ID, member.Username, "blocked"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("member Save in admins-only channel err = %v, want ErrForbidden", err)
	}
	if _, err := store.Save(ctx, ch.ID, owner.ID, owner.Username, "allowed"); err != nil {
		t.Fatalf("owner Save in admins-only channel: %v", err)
	}
	// Promote the member → they can post again.
	if err := store.SetServerRole(ctx, srv.ID, owner.ID, member.ID, "admin"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := store.Save(ctx, ch.ID, member.ID, member.Username, "now-ok"); err != nil {
		t.Fatalf("promoted admin should post in admins-only channel: %v", err)
	}

	// Public (serverless) channels always allow, and a policy can't be set on them.
	pub, _ := store.CreateChannel(ctx, uniqueChannel())
	if ok, _ := store.CanPostInChannel(ctx, pub.ID, member.ID); !ok {
		t.Fatal("anyone should post in a public channel")
	}
	if err := store.SetChannelPostPolicy(ctx, pub.ID, owner.ID, "admins"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("setting a policy on a public channel err = %v, want ErrForbidden", err)
	}
}

// TestChannelSlowmodeIntegration covers the per-channel post cooldown (SPEC
// "Slowmode"): a non-admin is throttled to one message per window, admins are
// exempt, the cooldown expires, and only admins can set it (Rule B / Rule 15).
func TestChannelSlowmodeIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)

	srv, _ := store.CreateServer(ctx, owner.ID, "Slow Guild")
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ch, _ := store.CreateServerChannel(ctx, srv.ID, "general")

	// Only admins set slowmode; out-of-range is rejected.
	if err := store.SetChannelSlowmode(ctx, ch.ID, member.ID, 30); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-admin SetChannelSlowmode = %v, want ErrForbidden", err)
	}
	if err := store.SetChannelSlowmode(ctx, ch.ID, owner.ID, 99999); !errors.Is(err, chat.ErrInvalidSlowmode) {
		t.Fatalf("out-of-range slowmode = %v, want ErrInvalidSlowmode", err)
	}

	// Owner sets a long cooldown; it shows up on the channel.
	if err := store.SetChannelSlowmode(ctx, ch.ID, owner.ID, 3600); err != nil {
		t.Fatalf("owner set slowmode: %v", err)
	}
	if sc, _ := store.ListServerChannels(ctx, srv.ID); len(sc) == 0 || sc[0].SlowmodeSeconds != 3600 {
		t.Fatalf("ListServerChannels should report slowmode: %+v", sc)
	}

	// Member: first message ok, immediate second blocked.
	if _, err := store.Save(ctx, ch.ID, member.ID, member.Username, "first"); err != nil {
		t.Fatalf("member first message: %v", err)
	}
	if _, err := store.Save(ctx, ch.ID, member.ID, member.Username, "too soon"); !errors.Is(err, chat.ErrSlowMode) {
		t.Fatalf("member rapid repost = %v, want ErrSlowMode", err)
	}
	// Admins are exempt — owner posts twice in a row.
	if _, err := store.Save(ctx, ch.ID, owner.ID, owner.Username, "a"); err != nil {
		t.Fatalf("owner post 1: %v", err)
	}
	if _, err := store.Save(ctx, ch.ID, owner.ID, owner.Username, "b"); err != nil {
		t.Fatalf("owner (admin) should be exempt from slowmode: %v", err)
	}

	// Cooldown expiry: a fresh channel with a 1s window, then wait it out.
	ch2, _ := store.CreateServerChannel(ctx, srv.ID, "quick")
	if err := store.SetChannelSlowmode(ctx, ch2.ID, owner.ID, 1); err != nil {
		t.Fatalf("set 1s slowmode: %v", err)
	}
	if _, err := store.Save(ctx, ch2.ID, member.ID, member.Username, "one"); err != nil {
		t.Fatalf("member first in ch2: %v", err)
	}
	if _, err := store.Save(ctx, ch2.ID, member.ID, member.Username, "fast"); !errors.Is(err, chat.ErrSlowMode) {
		t.Fatalf("member rapid repost in ch2 = %v, want ErrSlowMode", err)
	}
	// Wait comfortably past the 1s window. A tight margin (e.g. 1100ms) is flaky:
	// the cooldown is measured server-side as now()-created_at, so DB round-trips and
	// scheduling jitter under load can leave <1s actually elapsed; 1600ms is robust.
	time.Sleep(1600 * time.Millisecond)
	if _, err := store.Save(ctx, ch2.ID, member.ID, member.Username, "after cooldown"); err != nil {
		t.Fatalf("member should post after the cooldown elapses: %v", err)
	}

	// Slowmode is a server-channel setting — not settable on a public channel.
	pub, _ := store.CreateChannel(ctx, uniqueChannel())
	if err := store.SetChannelSlowmode(ctx, pub.ID, owner.ID, 5); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("slowmode on public channel = %v, want ErrForbidden", err)
	}
}

func hasServer(servers []chat.Server, id int64) bool {
	for _, s := range servers {
		if s.ID == id {
			return true
		}
	}
	return false
}

func hasDM(dms []chat.DMChannel, channelID, otherUserID int64) bool {
	for _, d := range dms {
		if d.ID == channelID && d.User.ID == otherUserID {
			return true
		}
	}
	return false
}

// findDM returns the listed DM channel with the given id, or nil. hasOther reports whether
// userID appears among a DM's other-member list (Users). Used by the group-DM tests.
func findDM(dms []chat.DMChannel, channelID int64) *chat.DMChannel {
	for i := range dms {
		if dms[i].ID == channelID {
			return &dms[i]
		}
	}
	return nil
}

func hasOther(users []chat.DMUser, userID int64) bool {
	for _, u := range users {
		if u.ID == userID {
			return true
		}
	}
	return false
}

// TestReplyIntegration covers message replies (references): a same-channel reply
// carries the denormalized preview (returned + in history); a cross-channel or
// nonexistent reference is DROPPED, not honored (Rule B/C — a client can't make a
// message it can't see leak through a reply preview); a soft-deleted target renders
// "[deleted]" in the preview.
func TestReplyIntegration(t *testing.T) {
	store, _, u := setup(t)
	ctx := context.Background()
	a, _ := store.CreateChannel(ctx, uniqueChannel())
	b, _ := store.CreateChannel(ctx, uniqueChannel())

	target, err := store.Save(ctx, a.ID, u.ID, u.Username, "the original")
	if err != nil {
		t.Fatalf("save target: %v", err)
	}

	// 1. Same-channel reply → preview populated on the returned message.
	reply, err := store.SaveReply(ctx, a.ID, u.ID, u.Username, "a reply", &target.ID)
	if err != nil {
		t.Fatalf("save reply: %v", err)
	}
	if reply.ReplyTo == nil || *reply.ReplyTo != target.ID ||
		reply.ReplyToAuthor != u.Username || reply.ReplyToBody != "the original" {
		t.Fatalf("same-channel reply preview wrong: %+v", reply)
	}

	// …and in history (Recent), via the LEFT JOIN.
	hist, err := store.Recent(ctx, a.ID, u.ID, 50)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	var got *chat.Message
	for i := range hist {
		if hist[i].ID == reply.ID {
			got = &hist[i]
		}
	}
	if got == nil || got.ReplyTo == nil || *got.ReplyTo != target.ID ||
		got.ReplyToAuthor != u.Username || got.ReplyToBody != "the original" {
		t.Fatalf("history reply preview wrong: %+v", got)
	}

	// 2. ADVERSARIAL: cross-channel reference (post in B, reference a message in A)
	//    must be dropped — never leak A's message into B.
	cross, err := store.SaveReply(ctx, b.ID, u.ID, u.Username, "cross", &target.ID)
	if err != nil {
		t.Fatalf("save cross-channel reply: %v", err)
	}
	if cross.ReplyTo != nil || cross.ReplyToAuthor != "" || cross.ReplyToBody != "" {
		t.Fatalf("cross-channel reference should be dropped, got: %+v", cross)
	}

	// 3. ADVERSARIAL: nonexistent target id must be dropped, not error.
	bogus := int64(99999999)
	none, err := store.SaveReply(ctx, a.ID, u.ID, u.Username, "to nowhere", &bogus)
	if err != nil {
		t.Fatalf("save bogus reply: %v", err)
	}
	if none.ReplyTo != nil {
		t.Fatalf("bogus reference should be dropped, got: %+v", none)
	}

	// 4. A soft-deleted target renders "[deleted]" in the preview.
	if _, err := store.DeleteMessage(ctx, target.ID, u.ID); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	hist2, err := store.Recent(ctx, a.ID, u.ID, 50)
	if err != nil {
		t.Fatalf("recent after delete: %v", err)
	}
	for i := range hist2 {
		if hist2[i].ID == reply.ID {
			if hist2[i].ReplyTo == nil || hist2[i].ReplyToBody != "[deleted]" {
				t.Fatalf("deleted-target preview should be [deleted], got: %+v", hist2[i])
			}
		}
	}
}

// TestThreadsIntegration exercises v0.8 threads (slice 1, backend): a thread is a kind='thread'
// channel under a parent that INHERITS the parent's access, stays out of the channel lists, and
// reuses the message infra; threading a DM or a thread (nesting) is rejected.
func TestThreadsIntegration(t *testing.T) {
	store, pool, owner := setup(t)
	ctx := context.Background()
	member := regUser(t, pool)
	stranger := regUser(t, pool)

	srv, err := store.CreateServer(ctx, owner.ID, "Thread Guild")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	if err := store.AddServerMember(ctx, srv.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	parent, err := store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("channel: %v", err)
	}

	th, err := store.CreateThread(ctx, parent.ID, "Release Planning", nil)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	if th.Kind != "thread" || th.ParentID == nil || *th.ParentID != parent.ID {
		t.Fatalf("thread wrong: %+v", th)
	}

	// Access is INHERITED from the parent's server: members in, stranger out.
	for _, tc := range []struct {
		who  int64
		want bool
		name string
	}{
		{owner.ID, true, "owner"},
		{member.ID, true, "member"},
		{stranger.ID, false, "stranger (non-member)"},
	} {
		got, err := store.CanAccessChannel(ctx, th.ID, tc.who)
		if err != nil || got != tc.want {
			t.Fatalf("CanAccessChannel thread for %s = %v (err %v), want %v", tc.name, got, err, tc.want)
		}
	}

	// The thread does NOT leak into the server's channel list or the public list.
	chans, _ := store.ListServerChannels(ctx, srv.ID)
	for _, c := range chans {
		if c.ID == th.ID {
			t.Fatalf("thread %d leaked into ListServerChannels", th.ID)
		}
	}
	publics, _ := store.ListChannels(ctx)
	for _, c := range publics {
		if c.ID == th.ID {
			t.Fatalf("thread %d leaked into ListChannels", th.ID)
		}
	}

	// A second thread can SHARE the same name (threads don't enforce name uniqueness).
	th2, err := store.CreateThread(ctx, parent.ID, "Release Planning", nil)
	if err != nil {
		t.Fatalf("duplicate-name thread should be allowed: %v", err)
	}
	threads, err := store.ListThreads(ctx, parent.ID)
	if err != nil || len(threads) != 2 || threads[0].ID != th2.ID {
		t.Fatalf("ListThreads = %+v err %v (want 2, newest first)", threads, err)
	}

	// Messages reuse the channel infra: posting in the thread + Recent works.
	if _, err := store.Save(ctx, th.ID, member.ID, member.Username, "in the thread"); err != nil {
		t.Fatalf("post in thread: %v", err)
	}
	if recent, err := store.Recent(ctx, th.ID, member.ID, 50); err != nil || len(recent) != 1 || recent[0].Body != "in the thread" {
		t.Fatalf("thread Recent = %+v err %v", recent, err)
	}

	// Message-anchored threads (slice 3): a thread created fromMessageID records the anchor,
	// and the source message's ThreadID/ThreadName surface in the parent channel's Recent.
	src, err := store.Save(ctx, parent.ID, owner.ID, owner.Username, "let's discuss this")
	if err != nil {
		t.Fatalf("save source msg: %v", err)
	}
	anchored, err := store.CreateThread(ctx, parent.ID, "Discussion", &src.ID)
	if err != nil {
		t.Fatalf("anchored thread: %v", err)
	}
	parentRecent, err := store.Recent(ctx, parent.ID, owner.ID, 50)
	if err != nil {
		t.Fatalf("parent recent: %v", err)
	}
	var found bool
	for _, m := range parentRecent {
		if m.ID == src.ID {
			found = true
			if m.ThreadID == nil || *m.ThreadID != anchored.ID || m.ThreadName != "Discussion" {
				t.Fatalf("source message thread anchor wrong: threadId=%v name=%q (want %d/Discussion)", m.ThreadID, m.ThreadName, anchored.ID)
			}
		}
	}
	if !found {
		t.Fatal("source message missing from parent Recent")
	}
	// An anchor to a message NOT in the parent channel is dropped (the thread is created
	// unanchored, so no message carries it) — Rule B/C, can't anchor to a foreign message.
	foreign, _ := store.Save(ctx, th.ID, member.ID, member.Username, "foreign") // in the thread, not the parent
	if dropped, err := store.CreateThread(ctx, parent.ID, "No Anchor", &foreign.ID); err != nil {
		t.Fatalf("create with foreign anchor: %v", err)
	} else {
		again, _ := store.Recent(ctx, parent.ID, owner.ID, 50)
		for _, m := range again {
			if m.ThreadID != nil && *m.ThreadID == dropped.ID {
				t.Fatalf("a cross-channel anchor must be DROPPED, but message %d carries thread %d", m.ID, dropped.ID)
			}
		}
	}

	// Guards: name validation, threading a DM, nesting a thread, unknown parent.
	if _, err := store.CreateThread(ctx, parent.ID, "  ", nil); !errors.Is(err, chat.ErrInvalidThreadName) {
		t.Fatalf("blank name err = %v, want ErrInvalidThreadName", err)
	}
	if _, err := store.CreateThread(ctx, parent.ID, strings.Repeat("a", 101), nil); !errors.Is(err, chat.ErrInvalidThreadName) {
		t.Fatalf("overlong name err = %v, want ErrInvalidThreadName", err)
	}
	bob := regUser(t, pool)
	dm, _ := store.CreateOrGetDM(ctx, owner.ID, bob.ID)
	if _, err := store.CreateThread(ctx, dm.ID, "no dms", nil); !errors.Is(err, chat.ErrNotThreadable) {
		t.Fatalf("thread on a DM err = %v, want ErrNotThreadable", err)
	}
	if _, err := store.CreateThread(ctx, th.ID, "no nesting", nil); !errors.Is(err, chat.ErrNotThreadable) {
		t.Fatalf("nested thread err = %v, want ErrNotThreadable", err)
	}
	if _, err := store.CreateThread(ctx, 1<<40, "ghost", nil); !errors.Is(err, chat.ErrChannelNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrChannelNotFound", err)
	}
}

// TestServerVoiceChannelIntegration verifies dedicated voice channels (kind='voice') at the
// store layer: CreateServerChannelOfKind creates a voice channel that surfaces with kind='voice'
// in ListServerChannels while text channels stay kind=” (byte-identical wire shape), and the
// kind argument is validated (defense in depth, Rule B — the route validates too).
func TestServerVoiceChannelIntegration(t *testing.T) {
	store, _, owner := setup(t)
	ctx := context.Background()

	srv, err := store.CreateServer(ctx, owner.ID, "Voice Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	// An invalid kind is rejected and writes nothing.
	if _, err := store.CreateServerChannelOfKind(ctx, srv.ID, "stage-chan", nil, "stage"); !errors.Is(err, chat.ErrInvalidChannelKind) {
		t.Fatalf("bogus kind err = %v, want ErrInvalidChannelKind", err)
	}

	// A text channel (default kind) and a voice channel coexist under the server.
	text, err := store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("create text channel: %v", err)
	}
	if text.Kind != "" {
		t.Fatalf("text channel kind = %q, want empty", text.Kind)
	}
	voice, err := store.CreateServerChannelOfKind(ctx, srv.ID, "lounge", nil, "voice")
	if err != nil {
		t.Fatalf("create voice channel: %v", err)
	}
	if voice.Kind != "voice" {
		t.Fatalf("voice channel kind = %q, want %q", voice.Kind, "voice")
	}

	// ListServerChannels surfaces each kind correctly (voice carries it; text omits it).
	chans, err := store.ListServerChannels(ctx, srv.ID)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	var sawVoice, sawText bool
	for _, c := range chans {
		switch c.ID {
		case voice.ID:
			sawVoice = c.Kind == "voice"
		case text.ID:
			sawText = c.Kind == ""
		}
	}
	if !sawVoice {
		t.Fatalf("voice channel %d missing or not kind='voice': %+v", voice.ID, chans)
	}
	if !sawText {
		t.Fatalf("text channel %d should list with empty kind: %+v", text.ID, chans)
	}
}

// TestVoiceChannelRejectsMessages is the Rule-15 regression guard for the voice-channel
// surface: a voice channel (kind='voice') is voice-only, so the message-post path must
// REJECT text posts — even though the UI hides the composer, a hostile client could POST
// to it via the raw WS 'message' frame or the REST attachment path. Both go through
// CanPostInChannel, so rejecting there closes both. A text channel still accepts posts.
func TestVoiceChannelRejectsMessages(t *testing.T) {
	store, _, owner := setup(t)
	ctx := context.Background()

	srv, err := store.CreateServer(ctx, owner.ID, "Voice Post Guild")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	text, err := store.CreateServerChannel(ctx, srv.ID, "general")
	if err != nil {
		t.Fatalf("create text channel: %v", err)
	}
	voice, err := store.CreateServerChannelOfKind(ctx, srv.ID, "lounge", nil, "voice")
	if err != nil {
		t.Fatalf("create voice channel: %v", err)
	}

	// Legit use: a text channel still accepts posts.
	if _, err := store.Save(ctx, text.ID, owner.ID, owner.Username, "hi"); err != nil {
		t.Fatalf("posting to a text channel should work: %v", err)
	}
	// Adversarial: posting to a voice channel is rejected (defense in depth — the gate, not
	// just the hidden composer). Covers the raw-WS message frame + REST attachment paths.
	if _, err := store.Save(ctx, voice.ID, owner.ID, owner.Username, "sneaky text in a voice channel"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("Save to a voice channel = %v, want ErrForbidden", err)
	}
	if _, err := store.SaveWithAttachments(ctx, voice.ID, owner.ID, owner.Username, "x", nil, nil); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("SaveWithAttachments to a voice channel = %v, want ErrForbidden", err)
	}
	// And the gate itself reports no-post for a voice channel.
	if ok, err := store.CanPostInChannel(ctx, voice.ID, owner.ID); err != nil || ok {
		t.Fatalf("CanPostInChannel(voice) = %v,%v want false,nil", ok, err)
	}
	// A voice channel is voice-only: you can't start a thread off it either (no messages to
	// anchor, no text) — a text channel still threads fine.
	if _, err := store.CreateThread(ctx, voice.ID, "ghost thread", nil); !errors.Is(err, chat.ErrNotThreadable) {
		t.Fatalf("CreateThread on a voice channel = %v, want ErrNotThreadable", err)
	}
	if _, err := store.CreateThread(ctx, text.ID, "real thread", nil); err != nil {
		t.Fatalf("CreateThread on a text channel should work: %v", err)
	}
}

// TestLeaveGroupDMIntegration verifies a member can leave a GROUP DM (≥3 members) — the
// row is removed, ListDMs drops it for the leaver but keeps it for the others — while a
// 1:1 DM can't be left, a non-member can't leave, and an unknown channel 404s (Rule B).
func TestLeaveGroupDMIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)
	dave := regUser(t, pool)

	// A group DM among alice, bob, carol (creator + 2 others = 3 members).
	grp, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, carol.ID})
	if err != nil {
		t.Fatalf("create group DM: %v", err)
	}

	// Adversarial (before leaving): a non-member can't leave (no existence leak).
	if err := store.LeaveGroupDM(ctx, grp.ID, dave.ID); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("non-member leave = %v, want ErrUserNotFound", err)
	}
	// A 1:1 DM can't be left.
	oneToOne, err := store.CreateOrGetDM(ctx, alice.ID, dave.ID)
	if err != nil {
		t.Fatalf("create 1:1 DM: %v", err)
	}
	if err := store.LeaveGroupDM(ctx, oneToOne.ID, alice.ID); !errors.Is(err, chat.ErrNotGroupDM) {
		t.Fatalf("leave 1:1 DM = %v, want ErrNotGroupDM", err)
	}
	// An unknown channel 404s.
	if err := store.LeaveGroupDM(ctx, 1<<40, alice.ID); !errors.Is(err, chat.ErrChannelNotFound) {
		t.Fatalf("leave unknown channel = %v, want ErrChannelNotFound", err)
	}

	// Bob leaves the group.
	if err := store.LeaveGroupDM(ctx, grp.ID, bob.ID); err != nil {
		t.Fatalf("bob leave group: %v", err)
	}
	// The group no longer appears in bob's DM list...
	bobDMs, _ := store.ListDMs(ctx, bob.ID)
	for _, d := range bobDMs {
		if d.ID == grp.ID {
			t.Fatalf("left group still in bob's DM list: %+v", bobDMs)
		}
	}
	// ...but it stays for alice + carol (the remaining members).
	aliceHasGroup := false
	for _, d := range mustListDMs(t, store, alice.ID) {
		if d.ID == grp.ID {
			aliceHasGroup = true
		}
	}
	if !aliceHasGroup {
		t.Fatalf("group vanished for alice after bob left")
	}
	// Bob can't leave again (no longer a member).
	if err := store.LeaveGroupDM(ctx, grp.ID, bob.ID); !errors.Is(err, chat.ErrUserNotFound) {
		t.Fatalf("re-leave = %v, want ErrUserNotFound", err)
	}
}

func mustListDMs(t *testing.T, store *chat.Store, userID int64) []chat.DMChannel {
	t.Helper()
	dms, err := store.ListDMs(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListDMs: %v", err)
	}
	return dms
}

// TestAddGroupDMMemberIntegration verifies adding a member to a GROUP DM: the new member's
// ListDMs includes the group and the roster grows; with the full adversarial matrix (non-member
// actor, 1:1, already-member, block, 10-member cap).
func TestAddGroupDMMemberIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)
	dave := regUser(t, pool)
	stranger := regUser(t, pool)

	grp, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, carol.ID})
	if err != nil {
		t.Fatalf("create group DM: %v", err)
	}

	// Adversarial: a non-member can't add (checked first → ErrForbidden, no leak).
	if err := store.AddGroupDMMember(ctx, grp.ID, stranger.ID, dave.ID); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-member add = %v, want ErrForbidden", err)
	}
	// A 1:1 DM can't be added to (start a new group instead).
	oneToOne, _ := store.CreateOrGetDM(ctx, alice.ID, stranger.ID)
	if err := store.AddGroupDMMember(ctx, oneToOne.ID, alice.ID, dave.ID); !errors.Is(err, chat.ErrNotGroupDM) {
		t.Fatalf("add to 1:1 = %v, want ErrNotGroupDM", err)
	}
	// Already a member → ErrAlreadyMember.
	if err := store.AddGroupDMMember(ctx, grp.ID, alice.ID, bob.ID); !errors.Is(err, chat.ErrAlreadyMember) {
		t.Fatalf("add existing = %v, want ErrAlreadyMember", err)
	}

	// A member (alice) adds dave → dave's ListDMs now includes the group.
	if err := store.AddGroupDMMember(ctx, grp.ID, alice.ID, dave.ID); err != nil {
		t.Fatalf("add dave: %v", err)
	}
	daveHasGroup := false
	for _, d := range mustListDMs(t, store, dave.ID) {
		if d.ID == grp.ID {
			daveHasGroup = true
			// dave sees alice, bob, carol as the others (roster grew to 4 total).
			if len(d.Users) != 3 {
				t.Fatalf("dave's group others = %d, want 3", len(d.Users))
			}
		}
	}
	if !daveHasGroup {
		t.Fatalf("added member dave doesn't see the group: %+v", mustListDMs(t, store, dave.ID))
	}

	// Block: alice blocks carol... actually carol blocks a new user; verify a blocked add is refused.
	blockee := regUser(t, pool)
	if err := store.BlockUser(ctx, alice.ID, blockee.ID); err != nil {
		t.Fatalf("block: %v", err)
	}
	if err := store.AddGroupDMMember(ctx, grp.ID, alice.ID, blockee.ID); !errors.Is(err, chat.ErrBlocked) {
		t.Fatalf("add blocked = %v, want ErrBlocked", err)
	}

	// Cap: fill to 10 members, then the 11th add is ErrGroupTooLarge. The group has 4
	// (alice,bob,carol,dave); add 6 more to reach 10.
	for i := 0; i < 6; i++ {
		u := regUser(t, pool)
		if err := store.AddGroupDMMember(ctx, grp.ID, alice.ID, u.ID); err != nil {
			t.Fatalf("fill add %d: %v", i, err)
		}
	}
	over := regUser(t, pool)
	if err := store.AddGroupDMMember(ctx, grp.ID, alice.ID, over.ID); !errors.Is(err, chat.ErrGroupTooLarge) {
		t.Fatalf("add over cap = %v, want ErrGroupTooLarge", err)
	}
}

// TestRenameGroupDMIntegration verifies naming a GROUP DM: set → ListDMs returns the name,
// clear (empty) → NULL/"" → falls back to members; adversarial (non-member, 1:1, >100 chars).
func TestRenameGroupDMIntegration(t *testing.T) {
	store, pool, alice := setup(t)
	ctx := context.Background()
	bob := regUser(t, pool)
	carol := regUser(t, pool)
	stranger := regUser(t, pool)

	grp, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, carol.ID})
	if err != nil {
		t.Fatalf("create group DM: %v", err)
	}
	nameOf := func(userID int64) string {
		for _, d := range mustListDMs(t, store, userID) {
			if d.ID == grp.ID {
				return d.Name
			}
		}
		t.Fatalf("group not in %d's DM list", userID)
		return ""
	}

	// Adversarial first: a non-member can't rename (no leak); a 1:1 can't be named; too-long rejected.
	if err := store.RenameGroupDM(ctx, grp.ID, stranger.ID, "Hax"); !errors.Is(err, chat.ErrForbidden) {
		t.Fatalf("non-member rename = %v, want ErrForbidden", err)
	}
	oneToOne, _ := store.CreateOrGetDM(ctx, alice.ID, stranger.ID)
	if err := store.RenameGroupDM(ctx, oneToOne.ID, alice.ID, "Nope"); !errors.Is(err, chat.ErrNotGroupDM) {
		t.Fatalf("rename 1:1 = %v, want ErrNotGroupDM", err)
	}
	if err := store.RenameGroupDM(ctx, grp.ID, alice.ID, strings.Repeat("x", 101)); !errors.Is(err, chat.ErrInvalidGroupName) {
		t.Fatalf("overlong rename = %v, want ErrInvalidGroupName", err)
	}

	// A member names it → every member's ListDMs shows the name.
	if err := store.RenameGroupDM(ctx, grp.ID, bob.ID, "Weekend Trip"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if n := nameOf(alice.ID); n != "Weekend Trip" {
		t.Fatalf("alice sees group name %q, want %q", n, "Weekend Trip")
	}
	if n := nameOf(carol.ID); n != "Weekend Trip" {
		t.Fatalf("carol sees group name %q, want %q", n, "Weekend Trip")
	}
	// Clearing (empty/whitespace) removes the name → back to "" (member-list title).
	if err := store.RenameGroupDM(ctx, grp.ID, alice.ID, "   "); err != nil {
		t.Fatalf("clear name: %v", err)
	}
	if n := nameOf(alice.ID); n != "" {
		t.Fatalf("after clear, name = %q, want empty", n)
	}
	// Group names are NOT unique: a second group can take the same name (the schema excludes
	// kind='dm' from name uniqueness). This would error if the unique index still applied.
	grp2, err := store.CreateGroupDM(ctx, alice.ID, []int64{bob.ID, carol.ID})
	if err != nil {
		t.Fatalf("create second group: %v", err)
	}
	if err := store.RenameGroupDM(ctx, grp.ID, alice.ID, "Shared Name"); err != nil {
		t.Fatalf("name group 1: %v", err)
	}
	if err := store.RenameGroupDM(ctx, grp2.ID, alice.ID, "Shared Name"); err != nil {
		t.Fatalf("two groups should be allowed the same name, got: %v", err)
	}
}
