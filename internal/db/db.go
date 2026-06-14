// Package db owns the Postgres connection pool and the embedded schema. The
// schema is idempotent (CREATE TABLE IF NOT EXISTS) and applied on every boot,
// so a fresh database needs no separate migration step for the MVP.
package db

import (
	"context"
	_ "embed"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

// Connect opens a pool and waits (up to ~30s) for Postgres to accept
// connections — Compose may start the server before the DB is ready.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	var pingErr error
	for i := 0; i < 30; i++ {
		if pingErr = pool.Ping(ctx); pingErr == nil {
			return pool, nil
		}
		time.Sleep(time.Second)
	}
	pool.Close()
	return nil, pingErr
}

// migrateLockKey is an arbitrary, app-scoped key for the advisory lock that
// serializes Migrate across connections.
const migrateLockKey int64 = 0x6F70656E63 // "openc"

// Migrate applies the embedded schema. It first takes a session-level Postgres
// advisory lock so concurrent callers serialize: `CREATE TABLE/INDEX IF NOT
// EXISTS` is NOT atomic against simultaneous creation, so two unsynchronized
// migrations (e.g. parallel `go test ./...` packages, or multiple instances
// booting at once) can collide in the catalog with a pg_class duplicate-key
// error (SQLSTATE 23505). The lock makes the schema apply exactly once at a time;
// each serialized run is idempotent (the IF-NOT-EXISTS statements no-op).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		return err
	}
	defer func() {
		// Release on a fresh context so a cancelled ctx can't strand the lock.
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrateLockKey)
	}()
	_, err = conn.Exec(ctx, schema)
	return err
}
