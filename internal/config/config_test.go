package config

import (
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
	} {
		t.Setenv(k, "")
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
