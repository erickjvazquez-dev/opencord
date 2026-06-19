package httpapi

import "testing"

// TestInlineImageAllowlistIsRasterOnly guards a security-critical invariant (Rule 15).
// inlineImageTypes is the set of SNIFFED content types served INLINE (vs forced to a
// download) for attachments, avatars, and custom emoji. It must contain ONLY
// non-scriptable raster image types. A scriptable type here — image/svg+xml, text/html,
// text/xml, application/xhtml+xml — would let a hostile upload execute in a victim's
// session (stored XSS) the moment it's rendered inline. This fails the instant any such
// type is added, catching the mistake AT THE SOURCE (the constant), including types that
// no behavioural-test payload happens to sniff to. Don't relax it without re-proving that
// the new type can never carry script.
func TestInlineImageAllowlistIsRasterOnly(t *testing.T) {
	allowed := map[string]bool{
		"image/png":  true,
		"image/jpeg": true,
		"image/gif":  true,
		"image/webp": true,
	}
	if len(inlineImageTypes) == 0 {
		t.Fatal("inlineImageTypes is empty — images would never render inline")
	}
	for ct := range inlineImageTypes {
		if !allowed[ct] {
			t.Fatalf("inlineImageTypes contains %q — only non-scriptable raster types may be served "+
				"INLINE (png/jpeg/gif/webp). A scriptable type (svg/html/xml) is a stored-XSS hole; "+
				"serve it as a download instead.", ct)
		}
	}
}
