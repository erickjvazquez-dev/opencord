package chat

import (
	"encoding/base64"
	"regexp"
	"testing"
)

// inviteCodeRe is the RawURLEncoding alphabet: URL/path-safe, no padding. A code
// with '+', '/' or '=' would break when embedded in an invite URL.
var inviteCodeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8}$`)

// TestInviteCodeFormat proves a minted code is exactly 8 URL-safe base64 chars
// (6 random bytes, RawURLEncoding) and decodes back to 6 bytes — so it can be
// dropped into a link/path without escaping.
func TestInviteCodeFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, err := inviteCode()
		if err != nil {
			t.Fatalf("inviteCode() err=%v", err)
		}
		if !inviteCodeRe.MatchString(code) {
			t.Fatalf("code %q not 8 URL-safe base64 chars", code)
		}
		raw, err := base64.RawURLEncoding.DecodeString(code)
		if err != nil {
			t.Fatalf("code %q does not decode as RawURLEncoding: %v", code, err)
		}
		if len(raw) != 6 {
			t.Fatalf("code %q decodes to %d bytes, want 6 (48 bits entropy)", code, len(raw))
		}
	}
}

// TestInviteCodeUniqueness is the guardrail against a regression that swaps the
// CSPRNG for a constant/weak/sequential generator: 48 bits of entropy must yield
// no collisions across a large batch. A dupe here means invite codes became
// guessable/predictable (Rule 15 — an attacker could mint or forge a code).
func TestInviteCodeUniqueness(t *testing.T) {
	const n = 5000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		code, err := inviteCode()
		if err != nil {
			t.Fatalf("inviteCode() err=%v", err)
		}
		if _, dup := seen[code]; dup {
			t.Fatalf("collision after %d codes: %q — generator is not random enough", i, code)
		}
		seen[code] = struct{}{}
	}
}
