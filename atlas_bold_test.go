package mobilegles

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/golang/freetype/truetype"
)

func TestSyntheticBoldGlyphMask(t *testing.T) {
	f := EmbeddedRobotoFace("Regular")
	if f == nil {
		t.Skip("no embedded roboto")
	}
	a := newGlyphAtlas(f, 11, true, false)
	g := a.getGlyph('B')
	if g == nil {
		t.Fatal("no glyph")
	}
	// dump the mask region + 3px margin
	m := 3
	img := image.NewRGBA(image.Rect(0, 0, g.width+m*2, g.height+m*2))
	for y := 0; y < g.height+m*2; y++ {
		for x := 0; x < g.width+m*2; x++ {
			ax, ay := x-m, y-m
			var v byte
			if ax >= 0 && ay >= 0 && ax < g.width && ay < g.height {
				v = a.img.RGBAAt(int(g.uvX*2048)+ax, int(g.uvY*2048)+ay).R
			}
			gray := byte(255 - v)
			o := (y*img.Stride + x*4)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = gray, gray, gray, 255
		}
	}
	fout, _ := os.Create("/tmp/opencode/maskB.png")
	png.Encode(fout, img)
	fout.Close()
	t.Logf("mask %dx%d dumped", g.width, g.height)
}

// TestSyntheticStyleAdvances pins that synthetic bold/italic glyphs
// carry WIDER advances than the regular face — the atlas is the single
// metrics authority (TextWidth walks it), so if this regresses, styled
// runs measure with regular metrics and following text overlaps.
func TestSyntheticStyleAdvances(t *testing.T) {
	f := EmbeddedRobotoFace("Regular")
	if f == nil {
		t.Skip("no embedded roboto")
	}
	reg := newGlyphAtlas(f, 11, false, false)
	bold := newGlyphAtlas(f, 11, true, false)
	ital := newGlyphAtlas(f, 11, false, true)
	both := newGlyphAtlas(f, 11, true, true)
	for _, r := range []rune{'B', 'w', 'd', 'k'} {
		ra, rb := reg.getGlyph(r), bold.getGlyph(r)
		ri, rbi := ital.getGlyph(r), both.getGlyph(r)
		if ra == nil || rb == nil || ri == nil || rbi == nil {
			t.Fatalf("missing glyph %q", r)
		}
		if rb.advance <= ra.advance {
			t.Errorf("%q bold advance %v <= regular %v", r, rb.advance, ra.advance)
		}
		if ri.advance <= ra.advance {
			t.Errorf("%q italic advance %v <= regular %v", r, ri.advance, ra.advance)
		}
		if rbi.advance <= rb.advance {
			t.Errorf("%q bold+italic advance %v <= bold %v", r, rbi.advance, rb.advance)
		}
	}
}

// TestTextWidthMatchesAtlas pins measure == draw: TextWidth must equal
// the exact pen walk DrawText performs (kern + atlas advance per glyph
// + synthetic trailing clearance).
func TestTextWidthMatchesAtlas(t *testing.T) {
	f := EmbeddedRobotoFace("Regular")
	font := NewFontStyled("test", 8, f, true, false)
	text := "4567"
	w := font.TextWidth(text)
	var pen float64
	prev, hasPrev := truetype.Index(0), false
	scale := fontSizeInPixels(8)
	for _, r := range text {
		idx := f.Index(r)
		if hasPrev {
			pen += float64(f.Kern(scale, prev, idx)) / 64.0
		}
		g := font.atlas.getGlyph(r)
		pen += float64(g.advance)
		prev, hasPrev = idx, true
	}
	pen += 1 // trailing overhang clearance
	t.Logf("TextWidth=%.2f pen-walk=%.2f", w, pen)
	if float64(w) < pen-0.01 || float64(w) > pen+0.01 {
		t.Errorf("TextWidth %.2f != pen walk %.2f", w, pen)
	}
	if w < 19 {
		t.Errorf("bold width %v suspiciously small (per-glyph widening missing?)", w)
	}
}
