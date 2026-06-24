package chat

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateRole pins the role-create/update input contract: a role name is
// attacker-controlled and rendered to every member, so it's trimmed, stripped
// of Trojan-Source bidi controls, and length-bounded by RUNES (not bytes); the
// color must be a real #RGB / #RRGGBB hex. Bad input returns a typed error, not
// a stored bad value (Rule B).
func TestValidateRole(t *testing.T) {
	valid := []struct {
		name, color string
		wantName    string
	}{
		{"Admin", "#fff", "Admin"},
		{"Mod", "#FF00aa", "Mod"},               // 6-hex, mixed case
		{"  spaced  ", "#000", "spaced"},        // trimmed
		{"日本語ロール", "#abcdef", "日本語ロール"},        // unicode preserved
		{strings.Repeat("x", 32), "#123", strings.Repeat("x", 32)}, // exactly 32 runes ok
		{strings.Repeat("界", 32), "#123", strings.Repeat("界", 32)}, // 32 multibyte runes ok (rune-bounded, not byte)
	}
	for _, c := range valid {
		gotName, gotColor, err := validateRole(c.name, c.color)
		if err != nil {
			t.Errorf("validateRole(%q,%q) err=%v, want ok", c.name, c.color, err)
			continue
		}
		if gotName != c.wantName {
			t.Errorf("validateRole(%q,%q) name=%q, want %q", c.name, c.color, gotName, c.wantName)
		}
		if gotColor != c.color {
			t.Errorf("validateRole(%q,%q) color=%q, want unchanged", c.name, c.color, gotColor)
		}
	}

	// Bidi controls are stripped BEFORE the empty/length check: a spoofed name
	// keeps its visible text, and a name that is ONLY controls collapses to
	// empty → rejected (can't store an invisible/blank role, Rule 15).
	t.Run("bidi stripped, visible text kept", func(t *testing.T) {
		n, _, err := validateRole("ev‮il", "#fff")
		if err != nil {
			t.Fatalf("err=%v, want ok", err)
		}
		if n != "evil" {
			t.Errorf("name=%q, want %q (bidi removed)", n, "evil")
		}
	})
	t.Run("name of only bidi controls is rejected", func(t *testing.T) {
		if _, _, err := validateRole("‮‭⁦", "#fff"); !errors.Is(err, ErrInvalidRoleName) {
			t.Errorf("err=%v, want ErrInvalidRoleName", err)
		}
	})

	badName := []string{
		"",                      // empty
		"   ",                   // whitespace only → trimmed empty
		strings.Repeat("x", 33), // 33 runes, over the cap
	}
	for _, n := range badName {
		if _, _, err := validateRole(n, "#fff"); !errors.Is(err, ErrInvalidRoleName) {
			t.Errorf("validateRole(%q) err=%v, want ErrInvalidRoleName", n, err)
		}
	}

	badColor := []string{
		"",            // empty
		"fff",         // missing #
		"#ff",         // 2 hex
		"#ffff",       // 4 hex
		"#fffffff",    // 7 hex
		"#gggggg",     // non-hex digits
		"#fff ",       // trailing space (regex is anchored, color is not trimmed)
		" #fff",       // leading space
		"red",         // name, not hex
		"#fff;DROP",   // injection-ish suffix
		"rgb(0,0,0)",  // css function
	}
	for _, c := range badColor {
		if _, _, err := validateRole("Role", c); !errors.Is(err, ErrInvalidColor) {
			t.Errorf("validateRole(name,%q) err=%v, want ErrInvalidColor", c, err)
		}
	}
}
