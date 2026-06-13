package auth

import (
	"testing"
	"time"
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
