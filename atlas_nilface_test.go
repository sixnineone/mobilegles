package mobilegles

import "testing"

// TestAnAtlasWithoutAFaceStillWorks pins the crash that took TradeUI down
// inside paint (2026-11-24): a machine whose installed fonts matched none of the
// toolkit's candidate paths ended up with no face at all, and
// newGlyphAtlas(nil) dereferenced it while pre-rasterizing ASCII — a SIGSEGV on
// the first glyph of the first label.
func TestAnAtlasWithoutAFaceStillWorks(t *testing.T) {
	a := newGlyphAtlas(nil, 14, false, false) // must not panic
	if a == nil {
		t.Fatal("newGlyphAtlas(nil) returned nil")
	}
	if a.font == nil {
		t.Fatal("atlas has no face: the embedded Roboto was not used")
	}
	if g := a.getGlyph('A'); g == nil {
		t.Fatal("getGlyph('A') returned nil with the embedded face")
	}

	// The Font wrapper must not keep a nil face either: the shape walk indexes
	// it directly, so keeping nil here only moves the crash to the next draw.
	f := NewFontStyled("family-that-is-not-installed", 12, nil, false, false)
	if f.face == nil {
		t.Fatal("NewFontStyled kept a nil face")
	}
	if got := f.TextWidth("text"); got <= 0 {
		t.Fatalf("TextWidth with a substituted face = %v, want > 0", got)
	}
}
