package mobilegles

import (
	"os"
	"runtime"
	"unsafe"
)

// Gradient LUT atlas.
//
// Stops cannot travel per vertex (an arbitrary number of them) and cannot live
// in uniforms (one batch mixes many gradients), so each distinct gradient is
// rasterised into one row of a shared 256-wide RGBA texture and the shader
// samples it with the interpolated position along the gradient. Rows are cached
// by content and persist across frames; when the atlas fills, the pending batch
// is flushed (so nothing still references a row) and the atlas restarts.
const (
	gradientLUTWidth  = 256
	gradientLUTHeight = 64
)

type gradientAtlasData struct {
	tex   uint32
	pix   []byte
	rows  int
	dirty bool
	ready bool
	// keys caches the row for a gradient's content so the same gradient drawn
	// many times does not duplicate rows.
	keys [gradientLUTHeight]uint64
}

var gradientAtlas gradientAtlasData

// gradientKey hashes the parts of a gradient that affect its LUT.
func gradientKey(g *Gradient) uint64 {
	if g == nil || g.StopCount == 0 {
		return 0
	}
	// FNV-1a over the kind and the stops. Geometry is deliberately excluded:
	// the ramp depends only on the stops, and the geometry is per vertex.
	h := uint64(14695981039346656037)
	mix := func(b uint64) {
		h ^= b
		h *= 1099511628211
	}
	mix(uint64(g.Type))
	for i := 0; i < g.StopCount; i++ {
		mix(uint64(g.Stops[i].Color))
		bits := float32bits(g.Stops[i].Offset)
		mix(uint64(bits))
	}
	return h
}

func float32bits(f float32) uint32 {
	return *(*uint32)(unsafe.Pointer(&f))
}

// gradientRowFor returns the atlas row holding g's ramp, rasterising it on first
// use. It returns -1 when there is no gradient to draw.
func gradientRowFor(g *Gradient) int {
	if g == nil || g.Type == GradientNone || g.StopCount == 0 {
		return -1
	}
	key := gradientKey(g)
	for i := 0; i < gradientAtlas.rows; i++ {
		if gradientAtlas.keys[i] == key {
			return i
		}
	}
	if gradientAtlas.rows >= gradientLUTHeight {
		// The atlas is full. Flush first: batched vertices may still reference
		// existing rows, and clearing them underneath would corrupt that draw.
		flushSDFBatch()
		gradientAtlas.rows = 0
	}
	row := gradientAtlas.rows
	gradientAtlas.rows++
	gradientAtlas.keys[row] = key
	gradientAtlas.rasteriseRow(row, g)
	return row
}

// rasteriseRow writes the gradient's ramp into one atlas row.
func (a *gradientAtlasData) rasteriseRow(row int, g *Gradient) {
	if a.pix == nil {
		a.pix = make([]byte, gradientLUTWidth*gradientLUTHeight*4)
	}
	for x := 0; x < gradientLUTWidth; x++ {
		c := sampleGradientStops(g, float32(x)/float32(gradientLUTWidth-1))
		o := (row*gradientLUTWidth + x) * 4
		a.pix[o+0] = byte((c >> 16) & 0xFF)
		a.pix[o+1] = byte((c >> 8) & 0xFF)
		a.pix[o+2] = byte(c & 0xFF)
		a.pix[o+3] = byte((c >> 24) & 0xFF)
	}
	a.dirty = true
}

// sampleGradientStops linearly interpolates the stops at t (already clamped to
// 0..1). Stops must be sorted by offset; the setters guarantee that.
func sampleGradientStops(g *Gradient, t float32) Color {
	n := g.StopCount
	if n == 0 {
		return 0
	}
	if t <= g.Stops[0].Offset {
		return g.Stops[0].Color
	}
	for i := 1; i < n; i++ {
		if t <= g.Stops[i].Offset {
			a, b := g.Stops[i-1], g.Stops[i]
			span := b.Offset - a.Offset
			if span <= 0 {
				return b.Color
			}
			f := (t - a.Offset) / span
			return lerpColor(a.Color, b.Color, f)
		}
	}
	return g.Stops[n-1].Color
}

// lerpColor blends two 0xAARRGGBB colours channel-wise, including alpha.
func lerpColor(a, b Color, f float32) Color {
	if f <= 0 {
		return a
	}
	if f >= 1 {
		return b
	}
	mix := func(shift uint) uint32 {
		ca := float32((a >> shift) & 0xFF)
		cb := float32((b >> shift) & 0xFF)
		return uint32(ca+(cb-ca)*f+0.5) & 0xFF
	}
	return Color(mix(24)<<24 | mix(16)<<16 | mix(8)<<8 | mix(0))
}

// ensureGradientAtlasTexture creates the GL texture and its sampler state on
// first use, so a gradient primitive has a texture id to name as its sampler
// unit at emit time. The pixels are uploaded later, at flush. Safe to call
// without a GL context: it stays texture-less and the primitive falls back.
func ensureGradientAtlasTexture() {
	if gradientAtlas.tex != 0 || glGenTextures == nil || glBindTexture == nil {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	glGenTextures(1, unsafe.Pointer(&gradientAtlas.tex))
	if gradientAtlas.tex == 0 {
		return
	}
	glBindTexture(GL_TEXTURE_2D, gradientAtlas.tex)
	glTexImage2D(GL_TEXTURE_2D, 0, int32(0x1908), gradientLUTWidth, gradientLUTHeight,
		0, 0x1908, GL_UNSIGNED_BYTE, nil)
	// Linear across the ramp; v is sampled exactly at a texel centre, so rows
	// cannot bleed into each other despite the vertical linear filter.
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, int32(GL_CLAMP_TO_EDGE))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, int32(GL_CLAMP_TO_EDGE))
}

// uploadGradientAtlasPixels refreshes the atlas pixels. Must run on the GL
// thread; a no-op without a context (headless tests), like Image.Upload.
func uploadGradientAtlasPixels() {
	if gradientAtlas.pix == nil || !gradientAtlas.dirty || gradientAtlas.tex == 0 {
		return
	}
	if glBindTexture == nil || glTexImage2D == nil {
		return
	}
	glBindTexture(GL_TEXTURE_2D, gradientAtlas.tex)
	glTexImage2D(GL_TEXTURE_2D, 0, int32(0x1908), gradientLUTWidth, gradientLUTHeight,
		0, 0x1908, GL_UNSIGNED_BYTE, unsafe.Pointer(&gradientAtlas.pix[0]))
	gradientAtlas.dirty = false
	gradientAtlas.ready = true
	if os.Getenv("MEUI_GRADIENT_DEBUG") != "" {
		println("[gradient] uploaded atlas rows=", gradientAtlas.rows, "tex=", gradientAtlas.tex)
	}
}

// gradVertex is the per-vertex gradient state: two vec4 attributes plus the
// sampler unit holding the LUT atlas.
type gradVertex struct {
	a, b [4]float32
	unit float32
}

// sdfGradientVertex builds the gradient attributes for the primitive about to
// be emitted. A zero kind means "solid colour": the shader then uses vColor.
func sdfGradientVertex() gradVertex {
	if currentGradient.Type == GradientNone || currentGradient.StopCount == 0 {
		return gradVertex{}
	}
	row := gradientRowFor(&currentGradient)
	ensureGradientAtlasTexture()
	if gradientAtlas.tex == 0 || row < 0 {
		return gradVertex{}
	}
	g := currentGradient
	// a = (kind, anchor, scale) where the anchor is the linear start or the
	// radial/conic centre, and the scale is the radial radius or the conic
	// start angle. b = (linear end, lutV, unused).
	gv := gradVertex{
		a: [4]float32{float32(g.Type), g.Start.X, g.Start.Y, 0},
		b: [4]float32{g.End.X, g.End.Y, gradientRowV(row), 0},
	}
	switch g.Type {
	case GradientRadial:
		gv.a = [4]float32{float32(g.Type), g.Center.X, g.Center.Y, g.Radius}
		gv.b = [4]float32{0, 0, gradientRowV(row), 0}
	case GradientConic:
		gv.a = [4]float32{float32(g.Type), g.Center.X, g.Center.Y, g.StartAngle}
		gv.b = [4]float32{0, 0, gradientRowV(row), 0}
	}
	return gradVertex{
		a: gv.a,
		b: gv.b,
		// Reusing the shape modes' texture unit slot: a gradient shape samples
		// only the LUT, so it needs no other texture.
		unit: sdfBatchTexUnit(gradientAtlas.tex),
	}
}

// gradientRowV returns the v coordinate at the centre of an atlas row.
func gradientRowV(row int) float32 {
	return (float32(row) + 0.5) / float32(gradientLUTHeight)
}
