package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
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
