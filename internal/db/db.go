// Package db owns the Postgres connection pool and the embedded schema. The
// schema is idempotent (CREATE TABLE IF NOT EXISTS) and applied on every boot,
// so a fresh database needs no separate migration step for the MVP.
package db

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	// The schema apply takes brief ACCESS EXCLUSIVE locks (ALTER TABLE ... ADD
	// COLUMN on FK-referenced tables like users). The advisory lock serializes it
	// against OTHER migrations, but NOT against concurrent DML — the many parallel
	// `go test ./...` packages, or a sibling instance still serving traffic during a
	// rolling deploy. To never make that DML a deadlock victim, the migration sets a
	// `lock_timeout` BELOW the 1s deadlock-detection threshold: its DDL aborts its
	// own lock wait (SQLSTATE 55P03) before any deadlock is detected, so it always
	// yields. The schema is fully idempotent, so we just retry until a lock window
	// opens (and also retry a real 40P01, belt-and-suspenders).
	var execErr error
	for attempt := 0; attempt < 8; attempt++ {
		if execErr = applySchema(ctx, conn); execErr == nil {
			return nil
		}
		var pgErr *pgconn.PgError
		if !errors.As(execErr, &pgErr) || (pgErr.Code != "55P03" && pgErr.Code != "40P01") {
			return execErr // not transient lock contention — a real error
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(75*(attempt+1)) * time.Millisecond):
		}
	}
	return execErr
}

// applySchema runs the whole schema in one transaction with a short lock_timeout, so
// a contended ALTER yields (55P03) instead of deadlocking concurrent DML. SET LOCAL
// scopes the timeout to this transaction; it never leaks back to the pooled conn.
func applySchema(ctx context.Context, conn *pgxpool.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '500ms'"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, schema); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
