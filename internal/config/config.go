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
	// UploadDir is where message attachments are written on local disk (Rule A:
	// no external object store). Default `data/uploads` (gitignored). A self-hoster
	// mounts a volume here for persistence; container filesystems are ephemeral.
	UploadDir string
	// Mesh-WebRTC ICE. STUNURL defaults to a public STUN (current behavior); a
	// self-hoster can override it with their own. TURN* are optional — set them to
	// run a relay (e.g. coturn) so hostile/symmetric NATs can connect. All optional
	// (Rule A: the one-command stack needs none).
	STUNURL      string
	TURNURL      string
	TURNUsername string
	TURNPassword string
}

// IceServer is one WebRTC ICE server, shaped for the browser's
// RTCConfiguration.iceServers (`urls`, optional `username`/`credential`).
type IceServer struct {
	URLs       string `json:"urls"`
	Username   string `json:"username,omitempty"`
	Credential string `json:"credential,omitempty"`
}

// ICEServers returns the ICE servers for mesh WebRTC: the configured STUN (if any) plus
// an optional TURN relay (if OPENCORD_TURN_URL is set). Empty list = host/LAN candidates
// only. TURN credentials are returned only to authenticated callers (see /voice/token).
func (c Config) ICEServers() []IceServer {
	out := []IceServer{}
	if c.STUNURL != "" {
		out = append(out, IceServer{URLs: c.STUNURL})
	}
	if c.TURNURL != "" {
		out = append(out, IceServer{URLs: c.TURNURL, Username: c.TURNUsername, Credential: c.TURNPassword})
	}
	return out
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
		UploadDir:         env("OPENCORD_UPLOAD_DIR", "data/uploads"),
		STUNURL:           env("OPENCORD_STUN_URL", "stun:stun.l.google.com:19302"),
		TURNURL:           env("OPENCORD_TURN_URL", ""),
		TURNUsername:      env("OPENCORD_TURN_USERNAME", ""),
		TURNPassword:      env("OPENCORD_TURN_PASSWORD", ""),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
