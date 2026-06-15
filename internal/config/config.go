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
	// Optional OSS SFU (LiveKit) for large voice calls. All empty by default →
	// voice stays mesh (Rule A: the one-command stack needs no SFU). A self-hoster
	// opts into scale by running LiveKit and setting these three.
	SFUURL    string
	SFUKey    string
	SFUSecret string
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
		SFUURL:      env("OPENCORD_SFU_URL", ""),
		SFUKey:      env("OPENCORD_SFU_KEY", ""),
		SFUSecret:   env("OPENCORD_SFU_SECRET", ""),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
