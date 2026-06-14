// Package config loads runtime settings from the environment with safe local
// defaults so `go run ./cmd/server` works with zero setup against a local
// Postgres.
package config

import (
	"os"
	"time"
)

type Config struct {
	Addr        string
	DatabaseURL string
	JWTSecret   []byte
	TokenTTL    time.Duration
	CORSOrigin  string
}

func Load() Config {
	// Platforms like Railway/Heroku inject $PORT and expect the app to bind it;
	// it takes precedence over OPENCORD_ADDR when present.
	addr := env("OPENCORD_ADDR", ":8080")
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	return Config{
		Addr:        addr,
		DatabaseURL: env("DATABASE_URL", "postgres://opencord:opencord@localhost:5432/opencord?sslmode=disable"),
		JWTSecret:   []byte(env("JWT_SECRET", "dev-insecure-change-me")),
		TokenTTL:    7 * 24 * time.Hour,
		CORSOrigin:  env("CORS_ORIGIN", "*"),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
