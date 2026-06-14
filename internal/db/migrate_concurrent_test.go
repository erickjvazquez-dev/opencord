package db_test

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/db"
)

// TestMigrateConcurrent guards against the catalog race that `go test ./...`
// exposed once integration tests lived in multiple packages: several processes
// calling Migrate at once on a fresh database raced in `CREATE TABLE/INDEX IF NOT
// EXISTS`, colliding on pg_class with a duplicate-key error (SQLSTATE 23505).
// Migrate now takes a Postgres advisory lock so concurrent callers serialize.
// This drives that path directly — N goroutines released from a barrier so they
// all hit the schema at the same instant — and asserts every one succeeds.
// SKIPS without DATABASE_URL, like the other integration suites.
func TestMigrateConcurrent(t *testing.T) {
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

	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release all goroutines together to maximize overlap
			errs <- db.Migrate(ctx, pool)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Migrate failed: %v", err)
		}
	}
}
