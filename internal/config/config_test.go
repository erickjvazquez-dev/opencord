package config

import (
	"strconv"
	"testing"
	"time"
)

// clearEnv blanks every variable Load reads so a test starts from a known,
// override-free baseline. t.Setenv("") is enough: the env helper treats an empty
// value as unset, and t.Setenv restores the prior value when the test ends.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENCORD_ADDR", "PORT", "DATABASE_URL", "JWT_SECRET", "CORS_ORIGIN",
		"OPENCORD_SFU_URL", "OPENCORD_SFU_KEY", "OPENCORD_SFU_SECRET",
		"OPENCORD_STUN_URL", "OPENCORD_TURN_URL", "OPENCORD_TURN_USERNAME", "OPENCORD_TURN_PASSWORD",
		"OPENCORD_TURN_SECRET", "OPENCORD_TURN_TTL",
	} {
		t.Setenv(k, "")
	}
}

func TestICEServers(t *testing.T) {
	clearEnv(t)
	now := time.Unix(1_700_000_000, 0)

	// Default: a public STUN, no TURN (current behavior; one-command stack, Rule A).
	def := Load()
	if def.STUNURL == "" {
		t.Fatal("STUNURL should default to a public STUN")
	}
	ice := def.ICEServersForUser(7, now)
	if len(ice) != 1 || ice[0].URLs != def.STUNURL || ice[0].Username != "" {
		t.Fatalf("default ICEServers = %+v, want one STUN entry, no creds", ice)
	}

	// With STATIC TURN configured: STUN + a TURN entry carrying the static creds.
	t.Setenv("OPENCORD_TURN_URL", "turn:turn.example.com:3478")
	t.Setenv("OPENCORD_TURN_USERNAME", "u1")
	t.Setenv("OPENCORD_TURN_PASSWORD", "p1")
	c := Load()
	ice = c.ICEServersForUser(7, now)
	if len(ice) != 2 {
		t.Fatalf("ICEServers with TURN = %+v, want 2 (STUN + TURN)", ice)
	}
	turn := ice[1]
	if turn.URLs != "turn:turn.example.com:3478" || turn.Username != "u1" || turn.Credential != "p1" {
		t.Fatalf("static TURN entry = %+v, want url+username+credential", turn)
	}

	// STUN can be overridden (self-hoster's own); empty STUN + empty TURN = no servers.
	none := Config{}
	if got := none.ICEServersForUser(7, now); len(got) != 0 {
		t.Fatalf("empty config ICEServers = %+v, want none (host/LAN only)", got)
	}
}

// Ephemeral TURN: with OPENCORD_TURN_SECRET set, /voice/token must hand out SHORT-LIVED
// HMAC creds (coturn use-auth-secret) instead of the static password — the username
// embeds the expiry + user id and the credential is the HMAC re-derivable only with the
// secret. Proves the scheme is deterministic, user-scoped, time-bounded, and unforgeable.
func TestEphemeralTurnCredentials(t *testing.T) {
	clearEnv(t)
	now := time.Unix(1_700_000_000, 0)
	t.Setenv("OPENCORD_TURN_URL", "turn:turn.example.com:3478")
	t.Setenv("OPENCORD_TURN_USERNAME", "static-u")
	t.Setenv("OPENCORD_TURN_PASSWORD", "static-p")
	t.Setenv("OPENCORD_TURN_SECRET", "shared-coturn-secret")
	t.Setenv("OPENCORD_TURN_TTL", "1h")
	c := Load()
	if c.TURNTTL != time.Hour {
		t.Fatalf("TURNTTL = %v, want 1h", c.TURNTTL)
	}

	ice := c.ICEServersForUser(42, now)
	if len(ice) != 2 {
		t.Fatalf("ICEServers = %+v, want STUN + TURN", ice)
	}
	turn := ice[1]
	// The secret must NOT be used as the static password — ephemeral creds replace it.
	if turn.Credential == "static-p" || turn.Username == "static-u" {
		t.Fatal("with OPENCORD_TURN_SECRET set, the STATIC username/password must not be used")
	}
	// username = "<expiry-unix>:<userID>"; expiry = now + TTL.
	wantUser := strconv.FormatInt(now.Add(time.Hour).Unix(), 10) + ":42"
	if turn.Username != wantUser {
		t.Fatalf("ephemeral username = %q, want %q", turn.Username, wantUser)
	}
	// credential = base64(HMAC-SHA1(secret, username)) — deterministic + re-derivable.
	wantUser2, wantCred := TurnCredentials("shared-coturn-secret", 42, now.Add(time.Hour))
	if wantUser2 != turn.Username || wantCred != turn.Credential {
		t.Fatalf("credential mismatch: got (%q,%q), want (%q,%q)", turn.Username, turn.Credential, wantUser2, wantCred)
	}
	// Unforgeable: the WRONG secret yields a different credential for the same username.
	if _, forged := TurnCredentials("wrong-secret", 42, now.Add(time.Hour)); forged == turn.Credential {
		t.Fatal("an attacker without the secret produced a matching credential — HMAC is not protecting it")
	}
	// User-scoped + time-bounded: a different user OR a different expiry → different creds.
	if _, other := TurnCredentials("shared-coturn-secret", 99, now.Add(time.Hour)); other == turn.Credential {
		t.Fatal("different user produced the same credential — creds are not user-scoped")
	}
	if _, later := TurnCredentials("shared-coturn-secret", 42, now.Add(2*time.Hour)); later == turn.Credential {
		t.Fatal("different expiry produced the same credential — creds are not time-bounded")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	c := Load()
	if c.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", c.Addr)
	}
	if c.DatabaseURL == "" {
		t.Error("DatabaseURL should fall back to the local default, got empty")
	}
	if string(c.JWTSecret) != DevJWTSecret {
		t.Errorf("JWTSecret = %q, want the dev default", c.JWTSecret)
	}
	if !c.InsecureJWTSecret {
		t.Error("InsecureJWTSecret should be true when JWT_SECRET is unset")
	}
	if c.TokenTTL != 7*24*time.Hour {
		t.Errorf("TokenTTL = %v, want 168h", c.TokenTTL)
	}
	if c.CORSOrigin != "*" {
		t.Errorf("CORSOrigin = %q, want *", c.CORSOrigin)
	}
	if c.SFUURL != "" || c.SFUKey != "" || c.SFUSecret != "" {
		t.Error("SFU settings must be empty by default (mesh-only one-command stack, Rule A)")
	}
}

func TestPortTakesPrecedenceOverAddr(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENCORD_ADDR", ":9000")
	t.Setenv("PORT", "4000")
	if got := Load().Addr; got != ":4000" {
		t.Errorf("Addr = %q, want :4000 ($PORT must win over OPENCORD_ADDR)", got)
	}
}

func TestOpencordAddrUsedWhenNoPort(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENCORD_ADDR", ":7777")
	if got := Load().Addr; got != ":7777" {
		t.Errorf("Addr = %q, want :7777", got)
	}
}

func TestJWTSecretOverrideClearsInsecureFlag(t *testing.T) {
	clearEnv(t)
	t.Setenv("JWT_SECRET", "a-properly-long-random-production-secret")
	c := Load()
	if string(c.JWTSecret) != "a-properly-long-random-production-secret" {
		t.Errorf("JWTSecret = %q, want the overridden value", c.JWTSecret)
	}
	if c.InsecureJWTSecret {
		t.Error("InsecureJWTSecret should be false once JWT_SECRET is overridden")
	}
}

// Setting JWT_SECRET *to* the dev default explicitly is still insecure and must be
// flagged — the warning is about the value, not just whether the var is set.
func TestExplicitDevSecretIsStillFlagged(t *testing.T) {
	clearEnv(t)
	t.Setenv("JWT_SECRET", DevJWTSecret)
	if !Load().InsecureJWTSecret {
		t.Error("InsecureJWTSecret should be true when JWT_SECRET == the dev default")
	}
}

func TestSFUOptIn(t *testing.T) {
	clearEnv(t)
	t.Setenv("OPENCORD_SFU_URL", "wss://livekit.example")
	t.Setenv("OPENCORD_SFU_KEY", "key")
	t.Setenv("OPENCORD_SFU_SECRET", "secret")
	c := Load()
	if c.SFUURL != "wss://livekit.example" || c.SFUKey != "key" || c.SFUSecret != "secret" {
		t.Errorf("SFU opt-in not honored: %+v", c)
	}
}

func TestEnvFallback(t *testing.T) {
	clearEnv(t)
	if got := env("OPENCORD_ADDR", ":8080"); got != ":8080" {
		t.Errorf("env fallback = %q, want default :8080", got)
	}
	t.Setenv("OPENCORD_ADDR", ":1234")
	if got := env("OPENCORD_ADDR", ":8080"); got != ":1234" {
		t.Errorf("env = %q, want the set value :1234", got)
	}
}
