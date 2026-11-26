package mobilegles

import "testing"

// recordingRenderer stands in for the GL renderer so a stroke's submission can be
// inspected without a window: it counts forced flushes and remembers the device
// coordinates of the triangles it is handed.
type recordingRenderer struct {
	Renderer
	flushes int
	minX    float32
	minY    float32
	maxX    float32
	maxY    float32
	seen    bool
}

func (r *recordingRenderer) SetBlendMode(mode BlendMode)                    {}
func (r *recordingRenderer) Flush()                                         { r.flushes++ }
func (r *recordingRenderer) SetScissor(x, y, w, h int32)                    {}
func (r *recordingRenderer) ClearScissor()                                  {}
func (r *recordingRenderer) SetScreenSize(w, h float32)                     {}
func (r *recordingRenderer) ScreenSize() (float32, float32)                 { return 200, 200 }
func (r *recordingRenderer) Present()                                       {}
func (r *recordingRenderer) ClearWithViewport(col uint32, x, y, w, h int32) {}
func (r *recordingRenderer) SetViewport(x, y, w, h int32)                   {}
func (r *recordingRenderer) ScissorState() (int32, int32, int32, int32, bool) {
	return 0, 0, 200, 200, true
}
func (r *recordingRenderer) DrainErrors()                                                  {}
func (r *recordingRenderer) DrawRect(x, y, w, h float32, color Color, angle float32)       {}
func (r *recordingRenderer) DrawCircle(cx, cy, radius float32, color Color)                {}
func (r *recordingRenderer) DrawOval(x, y, w, h float32, color Color)                      {}
func (r *recordingRenderer) DrawLine(x1, y1, x2, y2 float32, color Color, strokeW float32) {}
func (r *recordingRenderer) DrawTexturedQuads(verts []float32, texID uint32, color Color, isImage bool) {
}
func (r *recordingRenderer) DrawColor(col uint32, mode BlendMode) {}

func (r *recordingRenderer) note(x, y float32) {
	if !r.seen {
		r.minX, r.minY, r.maxX, r.maxY, r.seen = x, y, x, y, true
		return
	}
	if x < r.minX {
		r.minX = x
	}
	if y < r.minY {
		r.minY = y
	}
	if x > r.maxX {
		r.maxX = x
	}
	if y > r.maxY {
		r.maxY = y
	}
}

func (r *recordingRenderer) DrawTriangles(verts []float32, color Color) {
	for i := 0; i+8 < len(verts); i += 9 {
		r.note(verts[i], verts[i+1])
	}
}

func (r *recordingRenderer) DrawRectStyled(x, y, w, h, radius, strokeW float32, color Color, style int32, angle float32) {
	r.note(x, y)
	r.note(x+w, y+h)
}

// A stroke's joins ride the unified batch. They used to force a flush per
// triangle — hundreds of draw calls a frame for the node graph's cables, which is
// what made dragging sluggish — and they used to skip the canvas transform, which
// put flecks of cable colour on the canvas instead of a join on its stroke.
func TestStrokeJoinsBatchAndFollowTheCanvasTransform(t *testing.T) {
	// The stroke walk reads the renderer's readiness and the batch's; forcing both
	// (as sdf_stride_test does) exercises it headlessly with a stub renderer, so
	// no GL is touched.
	prevRenderer := rectRenderer
	prevReady := sdfBatch.ready
	prevQuads := quadIBOMaxQuads
	defer func() {
		rectRenderer = prevRenderer
		sdfBatch.ready = prevReady
		quadIBOMaxQuads = prevQuads
	}()
	rectRenderer = &RectRenderer{screenWidth: 200, screenHeight: 200, ready: true}
	quadIBOMaxQuads = 1 << 20
	sdfBatch.ready = true

	rec := &recordingRenderer{}
	c := NewCanvas(200, 200)
	c.renderer = rec
	c.Translate(0, 40)

	p := NewPaint()
	p.SetStyle(int32(Stroke))
	p.SetStrokeWidth(6)
	path := NewPath()
	path.MoveTo(Point{X: 20, Y: 20})
	path.LineTo(Point{X: 80, Y: 20})
	path.LineTo(Point{X: 80, Y: 80})
	c.DrawPath(path, p)

	if rec.flushes != 0 {
		t.Errorf("a stroke forced %d flushes; the joins and segments batch into one draw call", rec.flushes)
	}
	if !rec.seen {
		t.Fatal("the stroke submitted nothing")
	}
	// Local y runs 20..80, so a translated canvas must place some of it near
	// 120 — and nothing below y=40.
	if rec.maxY < 100 {
		t.Errorf("the stroke's geometry stops at y=%.1f: it was not transformed", rec.maxY)
	}
	if rec.minY < 30 {
		t.Errorf("geometry at y=%.1f is above the canvas translation: something skipped the transform", rec.minY)
	}
}
