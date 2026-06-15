package db_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/db"
)

// TestMigrateUnderConcurrentDML reproduces the deadlock class that adding an
// `ALTER TABLE users ADD COLUMN` to the schema exposed: a migration's DDL takes an
// ACCESS EXCLUSIVE lock on the FK-referenced `users` table while other connections
// run DML on tables that reference it (here, INSERT INTO messages), and the two
// race to a deadlock (SQLSTATE 40P01) — sometimes killing the innocent DML, which
// is not retried. Migrate now sets a short lock_timeout so its DDL yields (55P03)
// instead of ever deadlocking the DML. This drives migrators and writers at the
// same instant and asserts NOTHING errors. SKIPS without DATABASE_URL.
func TestMigrateUnderConcurrentDML(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping DB integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("base migrate: %v", err)
	}

	// Seed an FK target: a user + the general channel (always present after migrate).
	var userID, channelID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`,
		fmt.Sprintf("dml_%d", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM channels WHERE name = 'general' AND server_id IS NULL`).Scan(&channelID); err != nil {
		t.Fatalf("general channel: %v", err)
	}

	const writers, migrators, iters = 6, 3, 25
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, (writers+migrators)*iters)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iters; i++ {
				if _, err := pool.Exec(ctx,
					`INSERT INTO messages (channel_id, user_id, body) VALUES ($1, $2, 'load')`,
					channelID, userID); err != nil {
					errs <- fmt.Errorf("writer insert: %w", err)
				}
			}
		}()
	}
	for m := 0; m < migrators; m++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iters; i++ {
				if err := db.Migrate(ctx, pool); err != nil {
					errs <- fmt.Errorf("migrate: %w", err)
				}
			}
		}()
	}

	close(start) // release everyone at once to maximize DDL/DML overlap
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate/DML failed (deadlock regression): %v", err)
		}
	}
}
