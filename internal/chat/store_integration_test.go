package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/db"
)

// setup connects to the database named by DATABASE_URL, applies the schema, and
// returns a store plus a freshly-registered user. It SKIPS when DATABASE_URL is
// unset, so `go test ./...` stays green locally without a database; CI sets it
// (a postgres service) so these run there.
func setup(t *testing.T) (*chat.Store, auth.User) {
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
	u, err := auth.New(pool, []byte("test-secret"), time.Hour).
		Register(ctx, fmt.Sprintf("itest_%d", time.Now().UnixNano()), "password123")
	if err != nil {
		t.Fatalf("register test user: %v", err)
	}
	return chat.NewStore(pool), u
}

func uniqueChannel() string { return fmt.Sprintf("itest-%d", time.Now().UnixNano()) }

func TestChannelStoreIntegration(t *testing.T) {
	store, _ := setup(t)
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
	store, u := setup(t)
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

	recentA, err := store.Recent(ctx, a.ID, 50)
	if err != nil {
		t.Fatalf("recent A: %v", err)
	}
	if len(recentA) != 1 || recentA[0].Body != "in-A" || recentA[0].ChannelID != a.ID {
		t.Fatalf("channel A history wrong (isolation broken?): %+v", recentA)
	}

	recentB, err := store.Recent(ctx, b.ID, 50)
	if err != nil {
		t.Fatalf("recent B: %v", err)
	}
	if len(recentB) != 1 || recentB[0].Body != "in-B" || recentB[0].ChannelID != b.ID {
		t.Fatalf("channel B history wrong (isolation broken?): %+v", recentB)
	}
}

func TestEditMessageIntegration(t *testing.T) {
	store, u := setup(t)
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
	store, u := setup(t)
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
	recent, _ := store.Recent(ctx, ch.ID, 50)
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
