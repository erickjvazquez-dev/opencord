package httpapi

import (
	"strings"
	"testing"
)

// TestSanitizeFilename encodes the path-traversal / control-char attacks an
// uploader might put in a filename (Rule 15). The result is display metadata
// echoed to clients — never a path — but it's cleaned so it can't carry a
// traversal sequence, embedded NUL/control bytes, or render as something else.
func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// --- path traversal: only the base name may survive ---
		{"posix traversal", "../../../etc/passwd", "passwd"},
		{"windows traversal", `..\..\..\windows\system32\cmd.exe`, "cmd.exe"},
		{"mixed separators", `foo/../..\bar.txt`, "bar.txt"},
		{"absolute posix", "/etc/shadow", "shadow"},
		{"trailing dotdot", "foo/..", "file"},
		{"deep then file", "a/b/c/d/report.pdf", "report.pdf"},

		// --- the traversal tokens themselves collapse to a safe default ---
		{"bare dotdot", "..", "file"},
		{"bare dot", ".", "file"},
		{"empty", "", "file"},

		// --- control / NUL bytes are stripped (truncation & log-injection) ---
		{"embedded nul", "foo\x00bar.txt", "foobar.txt"},
		{"crlf injection", "foo\r\nbar.txt", "foobar.txt"},
		{"bell + tab", "\x07a\tb.txt", "ab.txt"},
		{"del byte", "evil\x7f.txt", "evil.txt"},
		{"only control bytes", "\x00\x01\x02", "file"},

		// --- whitespace is trimmed; a control-only name defaults ---
		{"surrounding space", "  spaced.png  ", "spaced.png"},

		// --- legitimate names (incl. unicode) are preserved ---
		{"unicode kept", "café-photo.png", "café-photo.png"},
		{"dotfile kept", ".env.example", ".env.example"},
		{"spaces inside kept", "my report final.docx", "my report final.docx"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeFilename(c.in)
			if got != c.want {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", c.in, got, c.want)
			}
			// Invariant: no result may contain a separator or control byte —
			// these are what make a name dangerous to echo or reuse.
			if strings.ContainsAny(got, `/\`) {
				t.Errorf("result %q still contains a path separator", got)
			}
			for _, r := range got {
				if r < 0x20 || r == 0x7f {
					t.Errorf("result %q still contains a control byte %#x", got, r)
				}
			}
		})
	}
}

// TestSanitizeFilenameLengthCap bounds the echoed name (a megabyte filename is
// itself an abuse vector) and keeps the meaningful tail (extension).
func TestSanitizeFilenameLengthCap(t *testing.T) {
	long := strings.Repeat("a", 500) + ".pdf"
	got := sanitizeFilename(long)
	if len(got) > 200 {
		t.Fatalf("length cap not applied: got %d bytes", len(got))
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Errorf("length cap dropped the extension tail: %q", got)
	}
}

// TestSanitizeHeaderFilename proves the Content-Disposition quoted-string can't
// be broken out of: quotes, backslashes, CR/LF and control bytes that would let
// an attacker inject a second header or escape the filename="..." quoting are
// neutralised to '_' (Rule 15 — header injection / response splitting).
func TestSanitizeHeaderFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"quote breakout", `evil".txt`, "evil_.txt"},
		{"backslash escape", `evil\.txt`, "evil_.txt"},
		{"header injection", "x\r\nSet-Cookie: a=b", "x__Set-Cookie: a=b"},
		{"bare newline", "a\nb.txt", "a_b.txt"},
		{"nul byte", "a\x00b.txt", "a_b.txt"},
		{"del byte", "a\x7fb.txt", "a_b.txt"},
		{"quote and slash", `"\`, "__"},
		{"legit untouched", "café-report (1).pdf", "café-report (1).pdf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeHeaderFilename(c.in)
			if got != c.want {
				t.Fatalf("sanitizeHeaderFilename(%q) = %q, want %q", c.in, got, c.want)
			}
			// Invariant: nothing that can break the quoted header survives.
			if strings.ContainsAny(got, "\"\\\r\n") {
				t.Errorf("result %q still contains a header-breaking char", got)
			}
		})
	}
}
