package mobilegles

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang/freetype/truetype"
)

// systemFaceWith returns a TrueType face installed on this machine that carries
// has and does NOT carry lacks, or nil when none does.
//
// The walk does not assume a distribution's layout: Debian nests faces in a
// per-family directory (/usr/share/fonts/truetype/dejavu/…), openSUSE puts them
// straight in /usr/share/fonts/truetype. The old test hardcoded the Debian path
// and so failed on every other distribution.
func systemFaceWith(has, lacks rune) *truetype.Font {
	var found *truetype.Font
	for _, dir := range []string{"/usr/share/fonts", "/usr/local/share/fonts"} {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".ttf") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			face, err := truetype.Parse(data)
			if err != nil || face.Index(has) == 0 || face.Index(lacks) != 0 {
				return nil
			}
			found = face
			return fs.SkipAll
		})
		if found != nil {
			return found
		}
	}
	return found
}

func TestAtlasEmojiFallback(t *testing.T) {
	primary := EmbeddedRobotoFace("Light")
	if primary == nil {
		t.Fatal("embedded Roboto Light missing")
	}
	// The fallback must be a real system face carrying the SUBSTITUTE (☑) but
	// not the emoji (✅) — otherwise the fallback draws the emoji and the
	// substitution branch this test is about is never reached.
	fallback := systemFaceWith(0x2611, 0x2705)
	if fallback == nil {
		t.Skip("no installed TrueType face carries ☑ (0x2611) without ✅ (0x2705)")
	}
	a := newGlyphAtlas(primary, 14, false, false)
	a.fallbacks = append(a.fallbacks, fallback)

	_, idx, r2 := a.resolveFace(0x2705) // ✅
	if idx == 0 {
		t.Fatal("✅ did not resolve through fallback/substitution")
	}
	if r2 != 0x2611 {
		t.Fatalf("expected substitution to ☑, got %q", r2)
	}
	if g := a.getGlyph(0x2705); g == nil {
		t.Fatal("getGlyph returned nil for ✅")
	}
}
