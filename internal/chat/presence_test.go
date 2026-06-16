package chat_test

import (
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/chat"
)

// TestNormalizePresence: known states pass (case/space-insensitive), everything
// else — empty, garbage, an injection string — falls back to "online" (Rule B).
func TestNormalizePresence(t *testing.T) {
	cases := map[string]string{
		"online":               "online",
		"idle":                 "idle",
		"dnd":                  "dnd",
		"invisible":            "invisible",
		"  DND  ":              "dnd",
		"Invisible":            "invisible",
		"":                     "online",
		"bogus":                "online",
		"'; DROP TABLE users;": "online",
	}
	for in, want := range cases {
		if got := chat.NormalizePresence(in); got != want {
			t.Errorf("NormalizePresence(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEffectivePresence: what OTHER viewers see. A disconnected member, or one who
// chose "invisible" (even while connected), reads as offline; idle/dnd/online show
// through only when connected.
func TestEffectivePresence(t *testing.T) {
	cases := []struct {
		connected    bool
		raw          string
		wantOnline   bool
		wantPresence string
	}{
		{true, "online", true, "online"},
		{true, "idle", true, "idle"},
		{true, "dnd", true, "dnd"},
		{true, "invisible", false, "offline"}, // invisible hides even while connected
		{false, "online", false, "offline"},   // no live socket → offline
		{false, "dnd", false, "offline"},
		{true, "", true, "online"},      // empty normalizes to online
		{true, "bogus", true, "online"}, // unknown normalizes to online
	}
	for _, c := range cases {
		online, presence := chat.EffectivePresence(c.connected, c.raw)
		if online != c.wantOnline || presence != c.wantPresence {
			t.Errorf("EffectivePresence(%v, %q) = (%v, %q), want (%v, %q)",
				c.connected, c.raw, online, presence, c.wantOnline, c.wantPresence)
		}
	}
}
