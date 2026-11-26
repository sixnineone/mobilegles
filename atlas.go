package mobilegles

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strconv"
	"sync"
	"unsafe"

	"github.com/golang/freetype/raster"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"image/color"
)

const atlasResolution = 2048

type atlasGlyph struct {
	uvX, uvY, uvW, uvH float32
	advance            float32
	bearingX, bearingY float32
	width, height      int
}

type glyphAtlas struct {
	font   *truetype.Font
	size   float32
	texID  uint32
	glyphs map[rune]*atlasGlyph

	fakeBold   bool // embolden coverage at raster time (no real bold face)
	fakeItalic bool // shear outline at raster time (no real italic face)

	mu      sync.Mutex
	dirty   bool
	dumpSeq int
	img     *image.RGBA

	cursorX, cursorY int
	rowHeight        int
	capHeight        float32 // ink height of 'H' — measured at rasterization
	fallbacks        []*truetype.Font
	uploaded         bool
}

// glyphSubstitutions maps emoji/symbols no installed outline font
// carries (color-emoji-only codepoints) to the closest renderable glyph.
var glyphSubstitutions = map[rune]rune{
	0x2705: 0x2611, // ✅ -> ☑
	0x274C: 0x2717, // ❌ -> ✗
	0x2B50: 0x2605, // ⭐ -> ★
	0x2795: '+',    // ➕ -> +
	0x2796: '-',    // ➖ -> -
	0x2757: '!',    // ❗ -> !
}

// resolveFace returns the face and RUNE to rasterize for ch: the
// primary font, then fallbacks, then a renderable substitute rune for
// color-emoji-only codepoints. The substituted rune must be what gets
// drawn — rasterizing the original would produce .notdef.
func (a *glyphAtlas) resolveFace(ch rune) (*truetype.Font, truetype.Index, rune) {
	if a.font == nil {
		// Inert atlas: no face at all. Callers treat a nil face/index as
		// "draw nothing, advance the pen" — never as a reason to crash.
		return nil, 0, ch
	}
	if idx := a.font.Index(ch); idx != 0 {
		return a.font, idx, ch
	}
	for _, fb := range a.fallbacks {
		if fb == nil {
			continue
		}
		if idx := fb.Index(ch); idx != 0 {
			return fb, idx, ch
		}
	}
	if sub, ok := glyphSubstitutions[ch]; ok {
		if idx := a.font.Index(sub); idx != 0 {
			return a.font, idx, sub
		}
		for _, fb := range a.fallbacks {
			if fb == nil {
				continue
			}
			if idx := fb.Index(sub); idx != 0 {
				return fb, idx, sub
			}
		}
	}
	return a.font, a.font.Index(ch), ch
}

func newGlyphAtlas(f *truetype.Font, size float32, fakeBold, fakeItalic bool) *glyphAtlas {
	// A nil face is not a font that draws nothing — it is a segfault waiting for
	// the first glyph, because the atlas indexes it to resolve every rune. That
	// is not hypothetical: on a machine where no system font matched the
	// toolkit's candidate paths, every label rasterized against nil and took the
	// process down inside paint. Fall back to the face this binary carries.
	if f == nil {
		f = EmbeddedRobotoFace("Regular")
	}
	a := &glyphAtlas{
		font:       f,
		size:       size,
		glyphs:     make(map[rune]*atlasGlyph),
		img:        image.NewRGBA(image.Rect(0, 0, atlasResolution, atlasResolution)),
		dirty:      true,
		dumpSeq:    0,
		fakeBold:   fakeBold,
		fakeItalic: fakeItalic,
	}
	// Pre-rasterize the printable ASCII range: runtime text changes
	// (SetText, dynamic labels) then hit the glyph cache instead of
	// triggering mid-frame atlas re-uploads — under EGL/XWayland a
	// full glTexImage2D re-upload mid-frame hangs the pipeline.
	//
	// Skipped when even the embedded face is missing (a build that dropped the
	// embedded resources): the atlas is then inert, and getGlyph returns nil so
	// text lays out with a fixed advance and draws nothing.
	if a.font != nil {
		for r := rune(32); r <= 126; r++ {
			a.getGlyph(r)
		}
	}
	return a
}
func (a *glyphAtlas) getGlyph(ch rune) *atlasGlyph {
	a.mu.Lock()
	defer a.mu.Unlock()

	if g, ok := a.glyphs[ch]; ok {
		return g
	}

	face, idx, _ := a.resolveFace(ch)
	if face == nil {
		return nil
	}
	scale := fixed.Int26_6(a.size*64.0 + 0.5) // whole device pixels

	// Load the UNHINTED scaled outline and grid-fit it ourselves:
	// golang/freetype's HintingFull does not snap outlines the way
	// Skia's autohinter does — stems landed at fractional offsets and
	// smeared across two pixel columns (the dim duplicate every simple
	// glyph showed). snapOutline rounds flat edges and extremes to the
	// pixel grid; curves keep their natural AA.
	// HintingFull runs the font's own TrueType instructions — with our
	// own raster path (tight mask, integer offsets) the grid-fitted
	// points land crisply; the old Context path smeared them because
	// its pen/bounds math was fractional.
	// Hinting is ON by default: the font's own instructions grid-fit the outlines,
	// and with this raster path (tight mask, integer offsets) they land crisply.
	//
	// The cost is known and accepted. Hinted and unhinted outlines do not measure
	// the same — 100px against 111px for "Hello" at size 32, 43 against 48 at 14 —
	// so hinting has to be available CONSISTENTLY or text lands in different places.
	// It is not always available: DejaVuSans's own instructions crash freetype's
	// hinter (see loadGlyph), and a face that crashes is rasterized unhinted for the
	// rest of its life, which is why that decision is per FACE rather than per
	// glyph. So an app that swaps in such a font will see it measure differently
	// from a platform that can hint it — the toolkit's own theme font (the embedded
	// Roboto) hints cleanly everywhere, which is the case that matters.
	//
	// MEUI_TEXT_HINTING=0 turns it off. Measured on the parity corpus, off is the
	// closer match to Skia's autohinter (main 21.4 -> 20.8, audit7 7.6 -> 7.2), so
	// that is the lever if visual parity is the goal rather than crispness.
	hinting := font.HintingFull
	if os.Getenv("MEUI_TEXT_HINTING") == "0" || hintingUnsafe(face) {
		hinting = font.HintingNone
	}
	var gb truetype.GlyphBuf
	if err := loadGlyph(&gb, face, scale, idx, hinting); err != nil {
		return nil
	}
	// Whitespace: no outline, but the advance still matters.
	if len(gb.Points) == 0 {
		advPx := float32((gb.AdvanceWidth + 32) >> 6)
		atX, atY := a.cursorX, a.cursorY
		if atX >= atlasResolution {
			atX, atY = 0, atY+a.rowHeight
		}
		a.img.SetRGBA(atX, atY, color.RGBA{A: 255})
		a.cursorX += 2
		ag := &atlasGlyph{
			uvX:     float32(atX) / atlasResolution,
			uvY:     float32(atY) / atlasResolution,
			uvW:     1.0 / atlasResolution,
			uvH:     1.0 / atlasResolution,
			advance: advPx,
			width:   1,
			height:  1,
		}
		a.glyphs[ch] = ag
		a.dirty = true
		return ag
	}

	// Synthetic italic: shear the outline 0.2 px/px above the baseline
	// (TrueType outlines are y-up from baseline 0) — the rasterizer
	// keeps sub-pixel AA, so the slant is smooth.
	if a.fakeItalic {
		for i := range gb.Points {
			gb.Points[i].X += gb.Points[i].Y * 13107 >> 16 // ≈ ×0.20
		}
	}

	// Pixel bounds of the grid-fitted outline (26.6 → px).
	minX, minY := int(^uint(0)>>1), int(^uint(0)>>1)
	maxX, maxY := -(int(^uint(0)>>1) - 1), -(int(^uint(0)>>1) - 1)
	for _, p := range gb.Points {
		if int(p.X) < minX {
			minX = int(p.X)
		}
		if int(p.X) > maxX {
			maxX = int(p.X)
		}
		if int(p.Y) < minY {
			minY = int(p.Y)
		}
		if int(p.Y) > maxY {
			maxY = int(p.Y)
		}
	}
	x0pix, y0pix := minX>>6, minY>>6
	x1pix := (maxX + 63) >> 6
	y1pix := (maxY + 63) >> 6
	w, h := x1pix-x0pix, y1pix-y0pix
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	// Room for the synthetic-bold +0.5px embolden (may round into one
	// extra right column).
	if a.fakeBold {
		w++
	}

	// MEUI_TEXT_LCD=1: rasterize at 3x horizontal width and filter down
	// to per-channel subpixel coverage (ClearType-style).
	// Default (and MEUI_TEXT_LCD=0): grayscale coverage — the Skia /
	// Chrome Linux reference path. Subpixel fringing read as blur on
	// this display; grayscale + stem snapping matches the reference.
	lcd := os.Getenv("MEUI_TEXT_LCD") == "1"
	dst, w, h := rasterGlyphLCD(&gb, w, h, x0pix, y0pix, y1pix, lcd, a.fakeBold)

	// Synthetic bold: dilate the coverage (max over self/left/up, per
	// channel) — widens strokes by ~1px right/down.
	if a.fakeBold {
		dilateRGBA(dst)
	}

	// Cap height from the 'H' ink: the baseline sits at raster row
	// -y0pix of the coverage buffer, so cap = baselineRow - firstInkRow.
	if ch == 'H' && a.capHeight == 0 {
		baselineRow := -y0pix
	probe:
		for inkTop := 0; inkTop < h; inkTop++ {
			for x := 0; x < w; x++ {
				c := dst.RGBAAt(x, inkTop)
				if c.R > 32 || c.G > 32 || c.B > 32 {
					a.capHeight = float32(baselineRow - inkTop)
					break probe
				}
			}
		}
		if a.capHeight <= 0 {
			a.capHeight = a.size * 0.72
		}
	}

	// Atlas placement
	atX, atY := a.cursorX, a.cursorY
	if a.cursorX+w > atlasResolution {
		a.cursorX = 0
		a.cursorY += a.rowHeight
		atX, atY = a.cursorX, a.cursorY
	}
	if a.cursorY+h > atlasResolution {
		return nil
	}
	for y := 0; y < h && y+atY < atlasResolution; y++ {
		for x := 0; x < w && x+atX < atlasResolution; x++ {
			a.img.SetRGBA(atX+x, atY+y, dst.RGBAAt(x, y))
		}
	}
	a.cursorX += w + 1
	if h > a.rowHeight {
		a.rowHeight = h
	}

	// Whole-pixel advance: keeps pen origins integer across a run of
	// text, in step with DrawText's pixel snapping. Synthetic styles
	// widen the advance here — the atlas is the SINGLE metrics
	// authority, so TextWidth (which walks it) inherits the same
	// widening and measurement can never drift from drawing.
	advPx := float32((gb.AdvanceWidth + 32) >> 6)
	if a.fakeBold {
		advPx++ // dilation widens each glyph ~0.5-1px
	}
	if a.fakeItalic {
		advPx += 0.5 // shear leans glyph tops past the advance
	}

	ag := &atlasGlyph{
		uvX:      float32(atX) / atlasResolution,
		uvY:      float32(atY) / atlasResolution,
		uvW:      float32(w) / atlasResolution,
		uvH:      float32(h) / atlasResolution,
		advance:  advPx,
		bearingX: float32(x0pix),
		bearingY: float32(y1pix), // mask top, measured up from the baseline
		width:    w,
		height:   h,
	}
	a.glyphs[ch] = ag
	a.dirty = true
	return ag
}

// snapOutline grid-fits the scaled outline points to whole pixels:
//   - on-curve points forming a horizontal run (equal Y within a
//     contour) snap their Y to the nearest pixel — horizontal bars,
//     dot tops/bottoms and crossbars become crisp;
//   - vertical runs (equal X) snap X — stems become single solid
//     columns;
//   - the glyph extremes snap as well (dot outer edges).
//
// Off-curve control points and non-extreme single points are left
// alone, so rounding cannot lump curves.
func snapOutline(gb *truetype.GlyphBuf) {
	if len(gb.Points) == 0 {
		return
	}
	pts := gb.Points
	xRuns := map[fixed.Int26_6][]int{}
	yRuns := map[fixed.Int26_6][]int{}
	e0 := 0
	for _, e1 := range gb.Ends {
		ps := pts[e0:e1]
		base := e0
		e0 = e1
		n := len(ps)
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			if ps[i].X == ps[j].X {
				xRuns[ps[i].X] = append(xRuns[ps[i].X], base+i, base+j)
			}
			if ps[i].Y == ps[j].Y {
				yRuns[ps[i].Y] = append(yRuns[ps[i].Y], base+i, base+j)
			}
		}
	}
	for _, idxs := range xRuns {
		for _, i := range idxs {
			pts[i].X = (pts[i].X + 32) & ^fixed.Int26_6(63)
		}
	}
	for v, idxs := range yRuns {
		nv := (v + 32) & ^fixed.Int26_6(63)
		for _, i := range idxs {
			pts[i].Y = nv
		}
	}
	// NOTE: glyph extremes are deliberately NOT snapped — moving curve
	// apexes by up to half a pixel distorts round letterforms. Flat
	// runs (exact-equal coordinates) are safe and cover stems, bars
	// and dot edges, which is where crispness matters most.
}

// ── LCD subpixel coverage ───────────────────────────────────────────

// ── LCD subpixel coverage ───────────────────────────────────────────

// lcdFilter is FreeType's FT_LCD_FILTER_LIGHT: {0, 85, 86, 85, 0}/256.
var lcdFilter = [5]float64{0, 85, 86, 85, 0}

// rasterGlyphLCD rasterizes the grid-fitted outline into an RGBA
// coverage mask for the text atlas.
//
// lcd=true: the outline is rasterized at 3x horizontal resolution
// (ClearType-style) and reduced with the LCD FIR filter, producing
// per-channel subpixel coverage in R/G/B, then run through the
// gamma/contrast preblend LUT per channel.
//
// lcd=false: plain grayscale coverage replicated to RGB.
func rasterGlyphLCD(gb *truetype.GlyphBuf, w, h, x0pix, y0pix, y1pix int, lcd, fakeBold bool) (*image.RGBA, int, int) {
	if !lcd {
		g := image.NewAlpha(image.Rect(0, 0, w, h))
		rasterGlyphOutline(gb, g, x0pix, y0pix, y1pix, 1, 0)
		if fakeBold {
			// Synthetic bold: a second pass at +0.5px unions with the
			// first (over-compositor) — widens strokes ~0.5px without
			// the 1px integer smear that fused letters at 8px.
			rasterGlyphOutline(gb, g, x0pix, y0pix, y1pix, 1, 32)
		}
		textPreblendOnce.Do(textPreblendBuild)
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for i := range g.Pix {
			cov := textPreblendLUT[g.Pix[i]]
			j := i * 4
			out.Pix[j], out.Pix[j+1], out.Pix[j+2], out.Pix[j+3] = cov, cov, cov, 255
		}
		return out, w, h
	}

	w3 := w * 3
	g3 := image.NewAlpha(image.Rect(0, 0, w3, h))
	rasterGlyphOutline(gb, g3, x0pix*3, y0pix, y1pix, 3, 0)
	if fakeBold {
		// +0.5px final = 1.5px at the 3x raster (96 in 26.6 units).
		rasterGlyphOutline(gb, g3, x0pix*3, y0pix, y1pix, 3, 96)
	}

	textPreblendOnce.Do(textPreblendBuild)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	raw := make([]uint8, w3)

	for y := 0; y < h; y++ {
		for x := 0; x < w3; x++ {
			raw[x] = g3.AlphaAt(x, y).A
		}
		for x := 0; x < w; x++ {
			o := out.Pix[y*out.Stride+x*4:]
			for c := 0; c < 3; c++ {
				var sum float64
				for t := -2; t <= 2; t++ {
					s := 3*x + c + t
					if s < 0 {
						s = 0
					}
					if s >= w3 {
						s = w3 - 1
					}
					sum += lcdFilter[t+2] * float64(raw[s])
				}
				cov := uint8(clampf(sum/255.0, 0, 1)*255 + 0.5)
				// gamma/contrast preblend per channel
				cov = textPreblendLUT[cov]
				o[c] = cov
			}
			o[3] = 255
		}
	}
	return out, w, h
}

func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// dilateRGBA widens the coverage mask: each pixel becomes the max of
// itself, its left and its upper neighbor (per channel). Alpha stays
// 255 — the atlas stores per-channel coverage. Gives synthetic bold
// glyphs ~1px heavier strokes.
func dilateRGBA(img *image.RGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	src := make([]byte, len(img.Pix))
	copy(src, img.Pix)
	at := func(x, y int) int {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		return (y*w + x) * 4
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := (y*w + x) * 4
			l := at(x-1, y)
			u := at(x, y-1)
			for c := 0; c < 3; c++ {
				v := src[o+c]
				if src[l+c] > v {
					v = src[l+c]
				}
				if src[u+c] > v {
					v = src[u+c]
				}
				img.Pix[o+c] = v
			}
		}
	}
}

// rasterGlyphOutline rasterizes the (grid-fitted) outline into dst.// rasterGlyphOutline rasterizes the (grid-fitted) outline into dst.
// The outline is y-up; dst is y-down with row 0 = y0pix.
func rasterGlyphOutline(gb *truetype.GlyphBuf, dst *image.Alpha, x0pix, y0pix, y1pix, xscale int, extraX26 fixed.Int26_6) {
	r := raster.NewRasterizer(dst.Bounds().Dx(), dst.Bounds().Dy())
	// Raster rows are y-down; the outline is y-up. dy = y1pix<<6 maps
	// the glyph's top (maxY) to row 0 and the baseline to the last row.
	dx := fixed.Int26_6(-x0pix<<6) + extraX26
	dy := fixed.Int26_6(y1pix << 6)
	e0 := 0
	for _, e1 := range gb.Ends {
		drawContourSnapped(r, gb.Points[e0:e1], dx, dy, xscale)
		e0 = e1
	}
	r.Rasterize(raster.NewAlphaSrcPainter(dst))
}

// drawContourSnapped replays one contour into the rasterizer,
// handling TrueType's quadratic Béziers (two consecutive off-curve
// points imply an on-curve midpoint). Ported from golang/freetype's
// Context.drawContour so the rasterization semantics match exactly.
func drawContourSnapped(r *raster.Rasterizer, ps []truetype.Point, dx, dy fixed.Int26_6, xscale int) {
	if len(ps) == 0 {
		return
	}
	start := fixed.Point26_6{X: dx + ps[0].X*fixed.Int26_6(xscale), Y: dy - ps[0].Y}
	others := []truetype.Point(nil)
	if ps[0].Flags&0x01 != 0 {
		others = ps[1:]
	} else {
		last := fixed.Point26_6{X: dx + ps[len(ps)-1].X*fixed.Int26_6(xscale), Y: dy - ps[len(ps)-1].Y}
		if ps[len(ps)-1].Flags&0x01 != 0 {
			start = last
			others = ps[:len(ps)-1]
		} else {
			start = fixed.Point26_6{X: (start.X + last.X) / 2, Y: (start.Y + last.Y) / 2}
			others = ps
		}
	}
	r.Start(start)
	q0, on0 := start, true
	for _, p := range others {
		q := fixed.Point26_6{X: dx + p.X*fixed.Int26_6(xscale), Y: dy - p.Y}
		on := p.Flags&0x01 != 0
		if on {
			if on0 {
				r.Add1(q)
			} else {
				r.Add2(q0, q)
			}
		} else {
			if on0 {
				// No-op.
			} else {
				mid := fixed.Point26_6{X: (q0.X + q.X) / 2, Y: (q0.Y + q.Y) / 2}
				r.Add2(q0, mid)
			}
		}
		q0, on0 = q, on
	}
	if on0 {
		r.Add1(start)
	} else {
		r.Add2(q0, start)
	}
}

func (a *glyphAtlas) upload() {
	if !a.dirty {
		return
	}
	if dir := os.Getenv("MEUI_DUMP_ATLAS"); dir != "" {
		a.dumpSeq++
		dumpAtlas(a, dir, a.dumpSeq)
	}
	glBindTexture(GL_TEXTURE_2D, a.texID)
	if a.texID == 0 || !a.uploaded {
		glGenTextures(1, unsafe.Pointer(&a.texID))
		glBindTexture(GL_TEXTURE_2D, a.texID)
		glTexImage2D(GL_TEXTURE_2D, 0, int32(GL_RGBA), atlasResolution, atlasResolution, 0, GL_RGBA, GL_UNSIGNED_BYTE, unsafe.Pointer(&a.img.Pix[0]))
		glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, int32(GL_LINEAR))
		glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, int32(GL_LINEAR))
		glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, int32(GL_CLAMP_TO_EDGE))
		glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, int32(GL_CLAMP_TO_EDGE))
		a.uploaded = true
	} else {
		// Incremental update: only the changed region (or full image)
		// via TexSubImage — no storage re-allocation mid-frame.
		glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, atlasResolution, atlasResolution, GL_RGBA, GL_UNSIGNED_BYTE, unsafe.Pointer(&a.img.Pix[0]))
	}
	a.dirty = false
}

// dumpAtlas writes the atlas alpha image to a PNG (inverted so glyphs are
// dark-on-white) for debugging glyph corruption. One file per size.
func dumpAtlas(a *glyphAtlas, dir string, seq int) {
	b := a.img.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			al := a.img.RGBAAt(x, y).R
			// alpha 255 = glyph pixel; show black on white
			v := 255 - int(al)
			o := img.Pix[y*img.Stride+x*4:]
			o[0], o[1], o[2], o[3] = byte(v), byte(v), byte(v), 255
		}
	}
	os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("%s/atlas_%.0f_%d.png", dir, a.size, seq)
	atlasDumpCount++
	f, err := os.Create(name)
	if err == nil {
		png.Encode(f, img)
		f.Close()
	}
}

var atlasDumpCount int

// ── Text coverage preblend ──────────────────────────────────────────

var (
	textPreblendOnce sync.Once
	textPreblendLUT  [256]uint8
)

// textPreblend builds the coverage LUT once from the env knobs:
//
//	MEUI_TEXT_GAMMA    (default 1.5)  midtone boost exponent — >1 makes
//	                                   light-on-dark text bolder/brighter
//	MEUI_TEXT_CONTRAST (default 0.12) expands coverage away from 0.5,
//	                                   sharpening the AA ramp
func textPreblendBuild() {
	// Neutral by default: the LCD subpixel filter already delivers
	// crisp edges — the old 1.5/0.12 defaults doubled the ink weight
	// ("boxy"). Tune explicitly if a display needs it.
	// Defaults tuned for light-on-dark UI text (the classic Skia
	// preblend region): midtones darken so stems read solid instead of
	// washed. MEUI_TEXT_GAMMA / MEUI_TEXT_CONTRAST override.
	gamma := 1.15
	if s := os.Getenv("MEUI_TEXT_GAMMA"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 1.0 {
			gamma = f
		}
	}
	contrast := 0.08
	if s := os.Getenv("MEUI_TEXT_CONTRAST"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 0 && f <= 1 {
			contrast = f
		}
	}
	inv := 1.0 / gamma
	for i := 0; i < 256; i++ {
		cov := float64(i) / 255.0
		a := math.Pow(cov, inv)
		a = (a-0.5)*(1+contrast) + 0.5
		if a < 0 {
			a = 0
		} else if a > 1 {
			a = 1
		}
		textPreblendLUT[i] = uint8(a*255 + 0.5)
	}
}

// applyTextPreblend maps the glyph coverage through the preblend LUT.
func applyTextPreblend(dst *image.Alpha) {
	textPreblendOnce.Do(textPreblendBuild)
	for i := range dst.Pix {
		dst.Pix[i] = textPreblendLUT[dst.Pix[i]]
	}
}

// loadGlyph loads a scaled outline, falling back to UNHINTED when the font's own
// instructions crash the hinter.
//
// golang/freetype's hinter divides by the dot product of the freedom and
// projection vectors without checking it for zero (hint.go, mulDiv called from
// move), and a real font can drive it to zero. DejaVuSans does, and the panic took
// down the WHOLE PROCESS rather than one glyph — which is how this was found: a
// font swap made the path reachable for the first time.
//
// Falling back per glyph rather than disabling hinting is deliberate. The
// toolkit grid-fits outlines itself (snapOutline) and HintingFull is the chosen
// default for crispness, so turning it off globally would change every glyph in
// every app to fix one font's bytecode. An unhinted outline for that one glyph is
// exactly what the caller already gets when hinting is off.
func loadGlyph(gb *truetype.GlyphBuf, face *truetype.Font, scale fixed.Int26_6, idx truetype.Index, hinting font.Hinting) (err error) {
	if hinting == font.HintingNone {
		return gb.Load(face, scale, idx, hinting)
	}
	defer func() {
		if r := recover(); r != nil {
			// One glyph is enough to condemn the whole face: see hintingUnsafe.
			hintingUnsafeFaces.Store(face, true)
			err = gb.Load(face, scale, idx, font.HintingNone)
		}
	}()
	return gb.Load(face, scale, idx, hinting)
}

// hintingUnsafeFaces records faces whose own instructions have crashed the
// hinter. It is per FACE, not per glyph, and that is the whole point.
//
// A per-glyph fallback was tried first and made things WORSE: it left some glyphs
// hinted and some not, which is worse than either consistent choice — the
// text-heavy parity deltas went from 6.2 to 15.6 on one scene. Once a face has
// crashed once, every glyph of it is rasterized the same way.
var hintingUnsafeFaces sync.Map // *truetype.Font -> bool

// hintingUnsafe reports whether this face's instructions are known to crash the
// hinter.
func hintingUnsafe(face *truetype.Font) bool {
	if face == nil {
		return false
	}
	v, ok := hintingUnsafeFaces.Load(face)
	return ok && v.(bool)
}
