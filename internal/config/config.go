// Package config loads runtime settings from the environment with safe local
// defaults so `go run ./cmd/server` works with zero setup against a local
// Postgres.
package config

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"os"
	"strconv"
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
	// TURNSecret (OPENCORD_TURN_SECRET) opts into EPHEMERAL TURN auth: when set, each
	// /voice/token mints short-lived HMAC credentials instead of the static
	// username/password (coturn's use-auth-secret / TURN REST scheme). TURNTTL is how
	// long each credential is valid (OPENCORD_TURN_TTL, default 12h).
	TURNSecret string
	TURNTTL    time.Duration
}

// IceServer is one WebRTC ICE server, shaped for the browser's
// RTCConfiguration.iceServers (`urls`, optional `username`/`credential`).
type IceServer struct {
	URLs       string `json:"urls"`
	Username   string `json:"username,omitempty"`
	Credential string `json:"credential,omitempty"`
}

// TurnCredentials makes ephemeral TURN REST-API credentials (coturn use-auth-secret):
// username is "<expiry-unix>:<userID>" and credential is base64(HMAC-SHA1(secret,
// username)). coturn re-derives the same HMAC from its shared static-auth-secret and
// rejects the username once its embedded expiry passes — so a leaked credential is
// short-lived and can't be forged without the secret (Rule C/15). SHA-1 here is the
// coturn-mandated MAC for this scheme, not a hash of anything secret.
func TurnCredentials(secret string, userID int64, expiresAt time.Time) (username, credential string) {
	username = strconv.FormatInt(expiresAt.Unix(), 10) + ":" + strconv.FormatInt(userID, 10)
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	credential = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return username, credential
}

// ICEServersForUser returns the ICE servers for mesh WebRTC: the configured STUN (if
// any) plus an optional TURN relay (if OPENCORD_TURN_URL is set). When OPENCORD_TURN_SECRET
// is set, the TURN credentials are SHORT-LIVED per-user HMAC creds (production-correct;
// a leaked cred expires) instead of the static username/password. Empty list = host/LAN
// candidates only. Credentials are returned only to authed callers (see /voice/token, Rule C).
func (c Config) ICEServersForUser(userID int64, now time.Time) []IceServer {
	out := []IceServer{}
	if c.STUNURL != "" {
		out = append(out, IceServer{URLs: c.STUNURL})
	}
	if c.TURNURL != "" {
		ts := IceServer{URLs: c.TURNURL}
		if c.TURNSecret != "" {
			ts.Username, ts.Credential = TurnCredentials(c.TURNSecret, userID, now.Add(c.TURNTTL))
		} else {
			ts.Username, ts.Credential = c.TURNUsername, c.TURNPassword
		}
		out = append(out, ts)
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
		TURNSecret:        env("OPENCORD_TURN_SECRET", ""),
		TURNTTL:           envDuration("OPENCORD_TURN_TTL", 12*time.Hour),
	}
}

// envDuration parses a Go duration (e.g. "1h", "30m") from the env, falling back to def
// on unset/invalid so a typo can't disable TURN auth silently.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
