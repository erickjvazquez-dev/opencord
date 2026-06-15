// Command server is the Opencord backend: a single Go binary that serves the
// REST auth API and the WebSocket chat gateway. Run it with a reachable
// Postgres (DATABASE_URL) and it self-migrates on boot.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
	"github.com/erickjvazquez-dev/opencord/internal/chat"
	"github.com/erickjvazquez-dev/opencord/internal/config"
	"github.com/erickjvazquez-dev/opencord/internal/db"
	"github.com/erickjvazquez-dev/opencord/internal/httpapi"
	"github.com/erickjvazquez-dev/opencord/internal/ws"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // optional .env for local dev; ignored if absent

	cfg := config.Load()
	if cfg.InsecureJWTSecret {
		log.Println("WARNING: JWT_SECRET is unset — using the insecure development default. " +
			"Anyone can forge auth tokens. Set JWT_SECRET to a long random value before " +
			"exposing this server (Rule C).")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	authsvc := auth.New(pool, cfg.JWTSecret, cfg.TokenTTL)
	store := chat.NewStore(pool)
	hub := ws.NewHub(store)
	go hub.Run()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(cfg, authsvc, store, hub),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("opencord server listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
