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

// Migrate applies the embedded schema.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schema)
	return err
}
