package chat

import (
	"testing"
	"time"
)

// TestParseSearchQuery covers the Discord-style search-operator parser. The
// query is attacker-controlled (Rule B): operators must be recognised exactly,
// and anything malformed must fall through to inert free text rather than
// error or change the query's shape.
func TestParseSearchQuery(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		wantText string
		check    func(t *testing.T, f searchFilters)
	}{
		{
			name:     "plain text only",
			query:    "deploy the thing",
			wantText: "deploy the thing",
			check:    func(t *testing.T, f searchFilters) {},
		},
		{
			name:     "from operator strips key keeps value case",
			query:    "From:Alice deploy",
			wantText: "deploy",
			check: func(t *testing.T, f searchFilters) {
				if f.from != "Alice" {
					t.Errorf("from = %q, want %q (key lowercased, value preserved)", f.from, "Alice")
				}
			},
		},
		{
			name:     "has flags are case-insensitive",
			query:    "HAS:link Has:Image has:file",
			wantText: "",
			check: func(t *testing.T, f searchFilters) {
				if !f.hasLink || !f.hasImage || !f.hasFile {
					t.Errorf("has flags = link:%v image:%v file:%v, want all true", f.hasLink, f.hasImage, f.hasFile)
				}
			},
		},
		{
			name:     "before and after parse to dates",
			query:    "before:2026-01-01 after:2025-12-25 hi",
			wantText: "hi",
			check: func(t *testing.T, f searchFilters) {
				if f.before == nil || f.after == nil {
					t.Fatalf("before=%v after=%v, want both set", f.before, f.after)
				}
				// after: is exclusive-of-the-named-day → start of the NEXT day.
				if f.after.Day() != 26 {
					t.Errorf("after.Day = %d, want 26 (named day excluded)", f.after.Day())
				}
			},
		},
		{
			name:     "everything at once",
			query:    "from:bob has:image before:2026-06-01 release notes",
			wantText: "release notes",
			check: func(t *testing.T, f searchFilters) {
				if f.from != "bob" || !f.hasImage || f.before == nil {
					t.Errorf("combined parse wrong: %+v", f)
				}
			},
		},

		// --- adversarial / malformed: must degrade to free text, never break ---
		{
			name:     "sql injection in before stays free text",
			query:    "before:'; DROP TABLE messages;--",
			wantText: "before:'; DROP TABLE messages;--",
			check: func(t *testing.T, f searchFilters) {
				if f.before != nil {
					t.Errorf("injection parsed as a date: %v", f.before)
				}
			},
		},
		{
			name:     "impossible date stays free text",
			query:    "after:2026-13-99",
			wantText: "after:2026-13-99",
			check: func(t *testing.T, f searchFilters) {
				if f.after != nil {
					t.Errorf("impossible date accepted: %v", f.after)
				}
			},
		},
		{
			name:     "unknown has value is free text",
			query:    "has:cookies",
			wantText: "has:cookies",
			check: func(t *testing.T, f searchFilters) {
				if f.hasLink || f.hasImage || f.hasFile {
					t.Errorf("unknown has:value set a flag: %+v", f)
				}
			},
		},
		{
			name:     "bare operator keys have no value so stay free text",
			query:    "from: before: after:",
			wantText: "from: before: after:",
			check: func(t *testing.T, f searchFilters) {
				if f.from != "" || f.before != nil || f.after != nil {
					t.Errorf("bare operator key consumed: %+v", f)
				}
			},
		},
		{
			name:     "empty query",
			query:    "",
			wantText: "",
			check:    func(t *testing.T, f searchFilters) {},
		},
		{
			name:     "wildcards survive as literal text (escaped downstream)",
			query:    "100%_done",
			wantText: "100%_done",
			check:    func(t *testing.T, f searchFilters) {},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := parseSearchQuery(c.query)
			if f.text != c.wantText {
				t.Errorf("text = %q, want %q", f.text, c.wantText)
			}
			c.check(t, f)
		})
	}
}

// TestParseSearchDate proves the date operator is strict: only a real
// YYYY-MM-DD parses; anything else (bad format, impossible date, injection,
// empty) returns ok=false so the token reverts to inert free text (Rule B —
// hostile input is non-fatal).
func TestParseSearchDate(t *testing.T) {
	good := []string{"2026-01-01", "2025-12-31", "2000-02-29"} // leap day is valid
	for _, s := range good {
		d, ok := parseSearchDate(s)
		if !ok {
			t.Errorf("parseSearchDate(%q) ok=false, want a valid date", s)
			continue
		}
		// midnight UTC, no local-tz drift
		if d.Location() != time.UTC || d.Hour() != 0 || d.Minute() != 0 {
			t.Errorf("parseSearchDate(%q) = %v, want UTC midnight", s, d)
		}
	}
	bad := []string{
		"",
		"2026-13-01",            // month 13
		"2026-02-30",            // impossible day
		"2025-02-29",            // not a leap year
		"01-01-2026",            // wrong layout
		"2026/01/01",            // wrong separator
		"2026-1-1",              // unpadded
		"'; DROP TABLE x;--",    // injection
		"2026-01-01T00:00:00Z",  // datetime, not a bare date
		"99999999",              // garbage
	}
	for _, s := range bad {
		if _, ok := parseSearchDate(s); ok {
			t.Errorf("parseSearchDate(%q) ok=true, want rejected", s)
		}
	}
}
