package chat

import "testing"

// Unit coverage for the Trojan-Source (CVE-2021-42574) bidi-control stripper. Fast,
// no DB — pins the EXACT character set so a future edit can't silently widen it (over-
// stripping legit Unicode) or narrow it (letting a spoofing control back in). The
// end-to-end proof through the real persistence path is TestBidiControlStrippingIntegration.
func TestStripBidiControls(t *testing.T) {
	// All 9 controls the stripper must remove, by name.
	controls := map[string]rune{
		"LRE": '‪', "RLE": '‫', "PDF": '‬', "LRO": '‭', "RLO": '‮',
		"LRI": '⁦', "RLI": '⁧', "FSI": '⁨', "PDI": '⁩',
	}
	for name, r := range controls {
		if !isBidiControl(r) {
			t.Errorf("%s (%U) should be classified as a bidi control", name, r)
		}
		got := stripBidiControls("a" + string(r) + "b")
		if got != "ab" {
			t.Errorf("%s not stripped: got %q", name, got)
		}
	}

	// Legit Unicode that MUST survive untouched — including ZWJ (U+200D, emoji glue),
	// the bidi MARKS LRM/RLM (weaker, legitimately used — not in our strip set), Arabic,
	// CJK, combining marks, and plain text.
	keep := []string{
		"",                  // empty
		"hello world",       // ascii
		"🎉",                 // emoji
		"👨‍💻",          // ZWJ profession emoji
		"مرحبا",             // Arabic (renders RTL without explicit controls)
		"你好",              // CJK
		"é",           // combining acute accent
		"a‎b‏c",   // LRM/RLM marks — intentionally preserved
	}
	for _, s := range keep {
		if got := stripBidiControls(s); got != s {
			t.Errorf("legit text altered: input %q got %q", s, got)
		}
	}

	// A body that is ONLY controls collapses to empty (degenerate spoof payload).
	if got := stripBidiControls("‮⁦⁩"); got != "" {
		t.Errorf("all-controls body should strip to empty, got %q", got)
	}
}
