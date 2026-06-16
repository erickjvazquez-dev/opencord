package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
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

	has := func(ids []int64, id int64) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	unread := func() []int64 {
		ids, err := store.UnreadChannelIDs(ctx, me.ID)
		if err != nil {
			t.Fatalf("UnreadChannelIDs: %v", err)
		}
		return ids
	}

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

	statusOf := func(uid int64) string {
		ms, err := store.ListServerMembers(ctx, srv.ID)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		for _, m := range ms {
			if m.UserID == uid {
				return m.Status
			}
		}
		t.Fatalf("member %d not listed", uid)
		return ""
	}

	// Default: no status.
	if s := statusOf(member.ID); s != "" {
		t.Fatalf("default status should be empty, got %q", s)
	}
	// Set → trimmed and reflected in the member list.
	if err := store.SetUserStatus(ctx, member.ID, "  building Opencord  "); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if s := statusOf(member.ID); s != "building Opencord" {
		t.Fatalf("status = %q, want trimmed 'building Opencord'", s)
	}
	// Over-long → capped to 128 runes (Rule B), never stored unbounded.
	if err := store.SetUserStatus(ctx, member.ID, strings.Repeat("x", 500)); err != nil {
		t.Fatalf("set long: %v", err)
	}
	if n := len([]rune(statusOf(member.ID))); n != 128 {
		t.Fatalf("over-long status capped to %d runes, want 128", n)
	}
	// Whitespace clears it.
	if err := store.SetUserStatus(ctx, member.ID, "   "); err != nil {
		t.Fatalf("clear status: %v", err)
	}
	if s := statusOf(member.ID); s != "" {
		t.Fatalf("status should be cleared, got %q", s)
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
		name           string
		actor, target  int64
		want           error
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
