// Package config loads runtime settings from the environment with safe local
// defaults so `go run ./cmd/server` works with zero setup against a local
// Postgres.
package config

import (
	"os"
	"time"
)

// DevJWTSecret is the insecure default used so `go run`/`docker compose up` work
// with zero setup locally. Running with it once exposed means anyone can forge a
// JWT, so Load flags it (InsecureJWTSecret) and main warns loudly (Rule C).
const DevJWTSecret = "dev-insecure-change-me"

type Config struct {
	Addr        string
	DatabaseURL string
	JWTSecret   []byte
	TokenTTL    time.Duration
	CORSOrigin  string
	// True when JWTSecret is the insecure dev default (env unset or set to it).
	// main.go warns; auth still works so local dev stays zero-config (Rule A).
	InsecureJWTSecret bool
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
	secret := env("JWT_SECRET", DevJWTSecret)
	return Config{
		Addr:              addr,
		DatabaseURL:       env("DATABASE_URL", "postgres://opencord:opencord@localhost:5432/opencord?sslmode=disable"),
		JWTSecret:         []byte(secret),
		InsecureJWTSecret: secret == DevJWTSecret,
		TokenTTL:          7 * 24 * time.Hour,
		CORSOrigin:        env("CORS_ORIGIN", "*"),
		SFUURL:            env("OPENCORD_SFU_URL", ""),
		SFUKey:            env("OPENCORD_SFU_KEY", ""),
		SFUSecret:         env("OPENCORD_SFU_SECRET", ""),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
