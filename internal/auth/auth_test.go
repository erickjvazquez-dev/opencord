package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueParseRoundTrip(t *testing.T) {
	s := New(nil, []byte("test-secret"), time.Hour)
	want := User{ID: 42, Username: "alice"}

	tok, err := s.Issue(want)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	got, err := s.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, want)
	}
}

func TestParseRejectsWrongSecret(t *testing.T) {
	issuer := New(nil, []byte("secret-a"), time.Hour)
	verifier := New(nil, []byte("secret-b"), time.Hour)

	tok, _ := issuer.Issue(User{ID: 1, Username: "bob"})
	if _, err := verifier.Parse(tok); err == nil {
		t.Fatal("expected verification to fail with a different signing secret")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	s := New(nil, []byte("secret"), -time.Minute) // already expired
	tok, _ := s.Issue(User{ID: 1, Username: "carol"})
	if _, err := s.Parse(tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

// TestParseRejectsAlgNone is the Rule-15 lock on the classic JWT bypass: an attacker
// forges an UNSIGNED (alg=none) token with arbitrary claims (any user id), or one signed
// with a non-HMAC method, hoping the verifier trusts the token's own alg header. Parse
// MUST reject both — its keyfunc requires an HMAC signing method, so a future "simplify the
// keyfunc" refactor that dropped that check would fail this test loudly instead of silently
// minting an auth bypass.
func TestParseRejectsAlgNone(t *testing.T) {
	s := New(nil, []byte("secret"), time.Hour)
	claims := Claims{
		Username: "attacker",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "999", // an arbitrary victim/admin id the attacker did not earn
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	// alg=none: a syntactically valid, UNSIGNED token. The lib requires the special
	// UnsafeAllowNoneSignatureType key just to MINT it; the verifier must still refuse it.
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("forge alg=none token: %v", err)
	}
	if u, err := s.Parse(none); err == nil {
		t.Fatalf("alg=none token MUST be rejected, but Parse accepted it as %+v", u)
	}
	// Malformed and empty tokens are also rejected (no panic, just an error).
	for _, bad := range []string{"not.a.jwt", "", "a.b.c", "....."} {
		if _, err := s.Parse(bad); err == nil {
			t.Fatalf("malformed token %q must be rejected", bad)
		}
	}
}

func TestUsernameRegex(t *testing.T) {
	valid := []string{"abc", "user_1", "A1_b2", "________"}
	invalid := []string{"ab", "has space", "no-dash", "", "thisUsernameIsWayTooLongToBeValidForOpencord"}
	for _, v := range valid {
		if !usernameRe.MatchString(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	for _, v := range invalid {
		if usernameRe.MatchString(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}
