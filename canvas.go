// Package mobilegles provides an OpenGL ES 2.0 backend for MeUi using purego.
package mobilegles

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/golang/freetype/truetype"
)

// Canvas is a simplified drawing surface for OpenGL ES.
// This is a subset of unison.Canvas methods needed for MeUi.
type Canvas struct {
	width, height  float32
	scaleX, scaleY float32
	matrixStack    []Matrix
	currentMatrix  Matrix
	clipStack      []Rect
	saveStack      []int // clipStack length at each Save
	currentClip    Rect
	// clipPlaneStack holds snapshots of the accumulated convex path clip (the
	// last entry is current), and planeSaveStack records its depth at each
	// Save, mirroring clipStack/saveStack for the rect clip.
	clipPlaneStack [][]ClipPlane
	planeSaveStack []int
	renderer       Renderer
}

// NewCanvas creates a new canvas for the given dimensions.
func NewCanvas(width, height float32) *Canvas {
	fullRect := Rect{Width: width, Height: height}
	c := &Canvas{
		width:         width,
		height:        height,
		scaleX:        1,
		scaleY:        1,
		currentMatrix: NewMatrix(),
		currentClip:   fullRect,
		clipStack:     []Rect{fullRect},
		renderer:      defaultRenderer,
	}
	// Sync GPU scissor to full canvas rect (scissor test is always enabled).
	if glScissorGlobal != nil && rectRenderer.ready {
		setScissorTracked(0, 0, int32(width), int32(height))
	}
	// A new canvas is a new clip generation. The batch caches the clip-region set
	// it registered (clipRegGen) and stamps a per-vertex clip rect; either can
	// still describe the canvas this one replaced, and primitives are then clipped
	// at the OLD size - observed as content clipped at the previous window height
	// immediately after a resize, while the canvas, the size cache and the scissor
	// were all correct. Invalidating here makes the next append re-register against
	// the current surface.
	sdfBatch.clipRegGen = -1
	clipGen++
	return c
}

// Width returns the canvas width in pixels. Clip reset bounds derive
// from it, so a canvas must be recreated when its window resizes.
func (c *Canvas) Width() float32 { return c.width }

// Height returns the canvas height in pixels.
func (c *Canvas) Height() float32 { return c.height }

// applyScissorTopDown applies a scissor rect given in top-down canvas
// coordinates (y grows downward). GL scissor Y grows upward from the
// bottom of the render target, so the Y must be flipped. Passing canvas
// coords raw mirrored every clip vertically: content drawn near the top
// was clipped by a scissor band near the bottom and vice versa.
func applyScissorTopDown(canvasHeight, x, y, w, h float32) {
	// MEUI_SCISSOR_TRACE: a PARTIAL clip (h != canvasHeight) is what becomes a
	// per-vertex clip band in GL space. A clip whose height is a PREVIOUS canvas
	// height, applied with the current one, yields (0, canvasHeight-y-h)-(..)
	// and cuts the frame exactly at the old height - the vertical-resize fault.
	// Full-height clips are silent so the log stays readable.
	if os.Getenv("MEUI_SCISSOR_TRACE") != "" && int32(h) != int32(canvasHeight) {
		where := "?"
		if _, file, line, ok := runtime.Caller(1); ok {
			where = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		}
		fmt.Printf("[scissor] canvasH=%.0f topdown=(%.0f,%.0f %.0fx%.0f) -> gl=(%.0f,%.0f %.0fx%.0f) via %s\n",
			canvasHeight, x, y, w, h, x, canvasHeight-y-h, w, h, where)
	}
	setScissorTracked(int32(x), int32(canvasHeight-y-h), int32(w), int32(h))
}

// Save saves the current state (matrix, clip).
func (c *Canvas) Save() int {
	c.matrixStack = append(c.matrixStack, c.currentMatrix)
	// Record the clip-stack length at save time. ClipRect pushes its
	// own entry; Restore pops the clip stack back to THIS length, so a
	// Save+ClipRect+Restore sequence is a true no-op pair. (The old
	// protocol — Save pushed a clip entry, Restore popped one — leaked
	// one stack entry per clipped widget, leaving stale clips active
	// that shader-discarded every later draw outside them.)
	c.saveStack = append(c.saveStack, len(c.clipStack))
	c.planeSaveStack = append(c.planeSaveStack, len(c.clipPlaneStack))
	if os.Getenv("MEUI_CLIP_DEBUG") != "" {
		clipDepth++
		if clipDepth > 100 {
			buf := make([]byte, 2000)
			n := runtime.Stack(buf, false)
			lines := strings.Split(string(buf[:n]), "\n")
			caller := ""
			for _, l := range lines {
				if strings.Contains(l, "meui-mobile/meui/widgets") {
					caller = strings.TrimSpace(l)
					break
				}
			}
			fmt.Printf("[clipDepth LEAK] %d via %s\n", clipDepth, caller)
		}
	}
	return len(c.matrixStack)
}

var clipDepth int

// SaveAndRestore runs f inside a Save/Restore pair — the transaction
// form of Save: every state change f makes (clips, transforms) is
// rolled back when it returns, even on early paths. Prefer this over
// bare Save/Restore in widget paints; it cannot leak stack entries.
func (c *Canvas) SaveAndRestore(f func(c *Canvas)) {
	c.Save()
	defer c.Restore()
	f(c)
}

// WithClip intersects the clip with r (top-down canvas coords) for the
// duration of f, then restores the previous clip. The transaction form
// of ClipRect: drawing with a clip can no longer leak stack entries
// (the historic TextField bug — every paint pushed a clip that was
// never popped, poisoning everything painted after it).
func (c *Canvas) WithClip(r Rect, f func(c *Canvas)) {
	c.Save()
	c.ClipRect(r)
	defer c.Restore()
	f(c)
}

// ClipDepth returns the current clip-stack depth. Debug aid for
// hunting widgets that push a clip without popping it.
func (c *Canvas) ClipDepth() int { return len(c.clipStack) }

// ResetClip drops any active clip and returns the canvas to an
// unclipped state: the clip stack is cleared and the process-global
// scissor mirror (stamped into every batched vertex) is reset. Used by
// callers that draw OUTSIDE a content tree — window title bars — so a
// leaked content clip (a widget's ClipRect without a matching
// Restore) cannot discard the bar's primitives.
func (c *Canvas) ResetClip() {
	c.clipStack = c.clipStack[:0]
	c.clipPlaneStack = c.clipPlaneStack[:0]
	c.currentClip = Rect{X: 0, Y: 0, Width: c.width, Height: c.height}
	ResetClipState()
	SetClipPlanes(nil)
}

// Restore restores the state to the most recent Save: the
// matrix/paint stacks pop one entry and the clip stack pops back to
// the length recorded at that Save (removing every ClipRect pushed
// since).
func (c *Canvas) Restore() {
	if len(c.matrixStack) > 0 {
		c.currentMatrix = c.matrixStack[len(c.matrixStack)-1]
		c.matrixStack = c.matrixStack[:len(c.matrixStack)-1]
	}
	if len(c.saveStack) > 0 {
		target := c.saveStack[len(c.saveStack)-1]
		c.saveStack = c.saveStack[:len(c.saveStack)-1]
		if len(c.clipStack) > target {
			c.clipStack = c.clipStack[:target]
		}
	}
	if len(c.planeSaveStack) > 0 {
		ptarget := c.planeSaveStack[len(c.planeSaveStack)-1]
		c.planeSaveStack = c.planeSaveStack[:len(c.planeSaveStack)-1]
		if len(c.clipPlaneStack) > ptarget {
			c.clipPlaneStack = c.clipPlaneStack[:ptarget]
		}
		SetClipPlanes(c.currentClipPlanes())
	}
	if len(c.clipStack) > 0 {
		c.currentClip = c.clipStack[len(c.clipStack)-1]
	} else {
		// Stack empty: return to the canvas base clip (NOT Rect{} — an
		// empty clip here left a zero scissor clipping all later draws).
		c.currentClip = Rect{X: 0, Y: 0, Width: c.width, Height: c.height}
	}
	// Sync the scissor to the restored clip. Batched draws carry their clip
	// per vertex, so no flush is needed on clip changes.
	// NOTE: do NOT call RestoreClip() here — it pops the clip stack AGAIN
	// (double pop drained the stack every Save/Restore pair).
	c.syncClipToGL()
}

// syncClipToGL mirrors the canvas' current clip into the process-global scissor
// mirror. That mirror is what curClipQuad() stamps into every batched vertex, so
// it IS the clip for all batched drawing; leaving it stale discards those draws.
//
// Both Restore and RestoreToCount must call this. For a long time only Restore
// did, so any caller using the count form (the renderer demo, and widget paints
// that save a count) left the mirror holding the clipped rect and every draw
// after the pair vanished outside it — the "clip state leaks into later draws"
// defect. One helper, both paths, so they cannot diverge again.
func (c *Canvas) syncClipToGL() {
	if glScissorGlobal != nil && rectRenderer.ready {
		applyScissorTopDown(c.height, c.currentClip.X, c.currentClip.Y, c.currentClip.Width, c.currentClip.Height)
	}
}

// RestoreToCount restores to a specific save count.
func (c *Canvas) RestoreToCount(count int) {
	target := count - 1
	if target < 0 {
		c.currentMatrix = NewMatrix()
		c.matrixStack = c.matrixStack[:0]
		c.currentClip = Rect{X: 0, Y: 0, Width: c.width, Height: c.height}
		c.clipStack = c.clipStack[:0]
		c.saveStack = c.saveStack[:0]
		c.clipPlaneStack = c.clipPlaneStack[:0]
		c.planeSaveStack = c.planeSaveStack[:0]
		SetClipPlanes(nil)
		c.syncClipToGL()
		return
	}
	if target < len(c.matrixStack) {
		c.currentMatrix = c.matrixStack[target]
		c.matrixStack = c.matrixStack[:target]
	}
	if target < len(c.saveStack) {
		// The first save being removed recorded the clip-stack length to
		// return to; drop it and every clip pushed after that point.
		clipTarget := c.saveStack[target]
		c.saveStack = c.saveStack[:target]
		if clipTarget <= len(c.clipStack) {
			c.clipStack = c.clipStack[:clipTarget]
		}
	}
	if target < len(c.planeSaveStack) {
		planeTarget := c.planeSaveStack[target]
		c.planeSaveStack = c.planeSaveStack[:target]
		if planeTarget <= len(c.clipPlaneStack) {
			c.clipPlaneStack = c.clipPlaneStack[:planeTarget]
		}
	}
	SetClipPlanes(c.currentClipPlanes())
	if len(c.clipStack) > 0 {
		c.currentClip = c.clipStack[len(c.clipStack)-1]
	} else {
		c.currentClip = Rect{X: 0, Y: 0, Width: c.width, Height: c.height}
	}
	// Same mirror sync as Restore: without it the clipped rect stays stamped
	// into every later batched vertex and they are discarded outside it.
	c.syncClipToGL()
}

// Translate translates the coordinate system.
func (c *Canvas) Translate(dx, dy float32) {
	c.currentMatrix = c.currentMatrix.Multiply(TranslateMatrix(dx, dy))
}

// Scale scales the coordinate system.
func (c *Canvas) Scale(sx, sy float32) {
	c.currentMatrix = c.currentMatrix.Multiply(ScaleMatrix(sx, sy))
}

// Rotate rotates the coordinate system (degrees).
func (c *Canvas) Rotate(degrees float32) {
	c.currentMatrix = c.currentMatrix.Multiply(RotateMatrix(degrees))
}

// Skew skews the coordinate system (kx, ky in degrees).
func (c *Canvas) Skew(kx, ky float32) {
	c.currentMatrix = c.currentMatrix.Multiply(SkewMatrix(kx, ky))
}

// GetMatrix returns the current transform matrix.
func (c *Canvas) GetMatrix() Matrix {
	return c.currentMatrix
}

// transformPoint applies the current matrix to a point and returns screen coordinates.
func (c *Canvas) transformPoint(x, y float32) (float32, float32) {
	pt := c.currentMatrix.Transform(Point{x, y})
	return pt.X, pt.Y
}

// transformRect transforms a rect (x, y, w, h) by the current matrix and returns
// the axis-aligned bounding box in screen coordinates.
//
// It transforms ALL FOUR corners, and that is the whole point: the first version
// transformed only (x,y) and (x+w,y+h), which is the AABB only while the matrix is
// axis-aligned. Under a rotation the other two corners fall outside the box those
// two span, so the result was too small — measured on an 80x40 rect at 20 degrees,
// where the two-corner box is 61x65 against a true 89x65, and at 45 degrees on a
// SQUARE it collapses to a thin bar (the two opposite corners land on a vertical
// line). That is exactly the node graph's PortDiamond, which is a square drawn under
// Rotate(45): it rendered as a sliver. Recorded as REVIEW.md section 6.
func (c *Canvas) transformRect(x, y, w, h float32) (float32, float32, float32, float32) {
	ax, ay := c.transformPoint(x, y)
	bx, by := c.transformPoint(x+w, y)
	cx, cy := c.transformPoint(x+w, y+h)
	dx, dy := c.transformPoint(x, y+h)
	minX, maxX := ax, ax
	minY, maxY := ay, ay
	for _, v := range [3][2]float32{{bx, by}, {cx, cy}, {dx, dy}} {
		if v[0] < minX {
			minX = v[0]
		}
		if v[0] > maxX {
			maxX = v[0]
		}
		if v[1] < minY {
			minY = v[1]
		}
		if v[1] > maxY {
			maxY = v[1]
		}
	}
	return minX, minY, maxX - minX, maxY - minY
}

// rectStyledGeometry maps a local rect to the geometry the SDF batch wants.
//
// Axis-aligned: the plain transformed box, as before. ROTATED: the shape cannot be
// described by a top-left and a size at all — rotating a rect about its corner is
// not a rect — so the centre and the two EDGE VECTORS are used instead, which give
// the true size and the angle. The batch rotates its quad by that angle about the
// centre while keeping aLocal unrotated, so the shader's local-space rounded-box
// distance produces the exact rotated shape, stroke and edge ramp included.
func (c *Canvas) rectStyledGeometry(x, y, w, h float32) (gx, gy, gw, gh, angle float32) {
	q := c.rectCorners(x, y, w, h)
	ex, ey := q[1][0]-q[0][0], q[1][1]-q[0][1] // the local +x edge, transformed
	fx, fy := q[3][0]-q[0][0], q[3][1]-q[0][1] // the local +y edge, transformed
	gw = sqrtf(ex*ex + ey*ey)
	gh = sqrtf(fx*fx + fy*fy)
	angle = atan2f(ey, ex)
	cx := (q[0][0] + q[2][0]) * 0.5
	cy := (q[0][1] + q[2][1]) * 0.5
	return cx - gw*0.5, cy - gh*0.5, gw, gh, angle
}

// matrixRotated reports whether the current matrix rotates or skews, i.e. whether a
// rect drawn through it is no longer axis-aligned.
//
// The off-diagonal terms are m[1] and m[3]. Matrix is a row-major [9]float32 —
// X = m[0]*x + m[1]*y + m[2], Y = m[3]*x + m[4]*y + m[5] — NOT the column-major Mat4
// layout, where the same role is played by m[4] and m[1]. Reading m[4] here returns
// the Y SCALE, which is 1 for every ordinary scene, so the predicate was true
// everywhere and every rect in every scene took the rotated path; that showed up as
// the parity corpus's only pixel-perfect scene, test_bigrect, moving off 0.0.
func (c *Canvas) matrixRotated() bool {
	const eps = 1e-4
	m := c.currentMatrix
	return absf(m[1]) > eps || absf(m[3]) > eps
}

// rectCorners transforms a rect's FOUR corners individually and returns them in
// order: top-left, top-right, bottom-right, bottom-left.
func (c *Canvas) rectCorners(x, y, w, h float32) [4][2]float32 {
	var out [4][2]float32
	out[0][0], out[0][1] = c.transformPoint(x, y)
	out[1][0], out[1][1] = c.transformPoint(x+w, y)
	out[2][0], out[2][1] = c.transformPoint(x+w, y+h)
	out[3][0], out[3][1] = c.transformPoint(x, y+h)
	return out
}

// TransformRect is the public version of transformRect, used by meuishim.
func (c *Canvas) TransformRect(x, y, w, h float32) (float32, float32, float32, float32) {
	return c.transformRect(x, y, w, h)
}

// ClipRect sets the clipping rectangle using scissor test.
func (c *Canvas) ClipRect(r Rect) {
	// Intersect with the current clip. Always intersect (the base clip is
	// the full canvas rect): a replace shortcut on a zero rect would let
	// an EMPTY clip be reopened by the next ClipRect, leaking draws the
	// intersection had excluded.
	cx := max(c.currentClip.X, r.X)
	cy := max(c.currentClip.Y, r.Y)
	cw := min(c.currentClip.X+c.currentClip.Width, r.X+r.Width) - cx
	ch := min(c.currentClip.Y+c.currentClip.Height, r.Y+r.Height) - cy
	if cw > 0 && ch > 0 {
		c.currentClip = Rect{X: cx, Y: cy, Width: cw, Height: ch}
	} else {
		// Empty clip: stays empty through further intersections, and
		// applyScissorTopDown below gives the mirror a zero-area rect so
		// every stamped draw is discarded (NOT "no clip").
		c.currentClip = Rect{X: 0, Y: 0, Width: 0, Height: 0}
	}
	c.clipStack = append(c.clipStack, c.currentClip)

	// Apply scissor test
	if glScissorGlobal != nil && rectRenderer.ready {
		// Batched draws carry their clip per vertex — no flush needed.
		applyScissorTopDown(c.height, c.currentClip.X, c.currentClip.Y, c.currentClip.Width, c.currentClip.Height)
		if os.Getenv("MEUI_LAYOUT_DEBUG") != "" && c.renderer != nil {
			bx, by, bw, bh, test := c.renderer.ScissorState()
			fmt.Printf("[clip] set=(%.0f,%.0f %.0fx%.0f) glBox=(%d,%d %dx%d) test=%v screenH=%.0f\n",
				c.currentClip.X, c.currentClip.Y, c.currentClip.Width, c.currentClip.Height,
				bx, by, bw, bh, test, rectRenderer.screenHeight)
		}
		if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
			buf := make([]byte, 2000)
			n := runtime.Stack(buf, false)
			lines := strings.Split(string(buf[:n]), "\n")
			caller := ""
			for _, l := range lines {
				if strings.Contains(l, "meui-mobile/meui") {
					caller = strings.TrimSpace(l)
					break
				}
			}
			fmt.Printf("[clip] set=(%.0f,%.0f %.0fx%.0f) via %s\n", c.currentClip.X, c.currentClip.Y, c.currentClip.Width, c.currentClip.Height, caller)
		}
	}
}

// currentClipPlanes returns the accumulated path clip, or nil.
func (c *Canvas) currentClipPlanes() []ClipPlane {
	if len(c.clipPlaneStack) == 0 {
		return nil
	}
	return c.clipPlaneStack[len(c.clipPlaneStack)-1]
}

// ClipPath intersects the clip with the interior of path, transformed by the
// current matrix.
//
// It reports whether the clip is EXACT. Clipping is applied as per-vertex
// half-planes, which express any convex outline; a concave outline is clipped
// to its convex hull, and an outline needing more than maxClipPlanes edges is
// clipped to its bounding rectangle instead. Both fallbacks are supersets of
// the requested region, so content the caller asked to keep is never hidden —
// the clip can only ever show a little more than asked, never less — but both
// report false.
func (c *Canvas) ClipPath(path *Path) bool {
	if c == nil || path == nil || len(path.points) == 0 {
		return false
	}
	contours := buildContours(path)
	if len(contours) == 0 {
		return false
	}
	// A path with several contours needs set operations on regions, which
	// half-planes cannot express; the largest outline is the one that bounds
	// the rest closely enough to be safe.
	best := contours[0]
	for _, ct := range contours[1:] {
		if len(ct.points) > len(best.points) {
			best = ct
		}
	}
	dev := make([]Point, len(best.points))
	for i, p := range best.points {
		x, y := c.transformPoint(p.X, p.Y)
		dev[i] = Point{X: x, Y: y}
	}
	planes, exact := clipPlanesForContour(c.height, dev, pathHasCurve(path))
	combined := planes
	if planes != nil {
		prev := c.currentClipPlanes()
		combined = make([]ClipPlane, 0, len(prev)+len(planes))
		combined = append(combined, prev...)
		combined = append(combined, planes...)
		if len(combined) > maxClipPlanes {
			combined = nil
		}
	}
	if combined == nil {
		// Not expressible: intersect with the bounding rectangle instead. A
		// superset of the path, so nothing the caller drew is cut away.
		r := path.Bounds()
		x, y, w, h := c.transformRect(r.X, r.Y, r.Width, r.Height)
		c.ClipRect(Rect{X: x, Y: y, Width: w, Height: h})
		return false
	}
	c.clipPlaneStack = append(c.clipPlaneStack, combined)
	SetClipPlanes(combined)
	return exact
}

// pathHasCurve reports whether any segment is a bezier, whose flattened
// outline is inscribed in the curve and therefore needs an outward nudge to
// stay a superset of what was drawn.
func pathHasCurve(path *Path) bool {
	for _, pt := range path.points {
		if pt.Verb == PathQuad || pt.Verb == PathCubic {
			return true
		}
	}
	return false
}

// ClipBounds returns the current clip bounds.
func (c *Canvas) ClipBounds() Rect {
	return c.currentClip
}

// IsClipEmpty returns true if the current clip is empty.
func (c *Canvas) IsClipEmpty() bool {
	return c.currentClip.Width <= 0 || c.currentClip.Height <= 0
}

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// Clear clears the canvas with a color.
func (c *Canvas) Clear(col uint32) {
	c.ClearWithViewport(col, 0, 0, int32(c.width), int32(c.height))
}

// ClearWithViewport clears the canvas with a color and sets viewport
func (c *Canvas) ClearWithViewport(col uint32, x, y, w, h int32) {
	// MEUI_CLEAR_CALLER: the level above the renderer's clear. The renderer's own
	// caller was always canvas.go (this function), so the identity that matters is
	// who calls THIS: the frame loop (once per frame, as the code reads) or
	// Canvas.Clear (which anything may call).
	if os.Getenv("MEUI_CLEAR_CALLER") != "" {
		if _, file, line, ok := runtime.Caller(1); ok {
			fmt.Printf("[clearcaller2] %s:%d\n", filepath.Base(file), line)
		}
	}
	if c.renderer != nil {
		c.renderer.ClearWithViewport(col, x, y, w, h)
	}

	// Reset the canvas clip model per frame — otherwise the stale clip from
	// the previous frame intersects every new ClipRect, shrinking widgets
	// into invisibility (the GL scissor is reset by the renderer, so the
	// model must match).
	c.currentClip = Rect{X: float32(x), Y: float32(y), Width: float32(w), Height: float32(h)}

	if glScissorGlobal != nil {
		setScissorTracked(0, 0, int32(c.width), int32(c.height))
	}
	// Record the surface for the unclipped per-vertex clip stamp (curClipQuad).
	clipCanvasW, clipCanvasH = c.width, c.height
}

// applyBlend makes the renderer's blend state match the paint, so the paint's
// blend mode actually affects drawing. The renderer flushes pending batches
// when the mode changes, so a mode change is a batch boundary.
func (c *Canvas) applyBlend(p *Paint) {
	if c.renderer != nil && p != nil {
		c.renderer.SetBlendMode(p.blendMode)
	}
	if p != nil {
		// All of these are stamped into each batched vertex, so a batch may
		// mix paints without a flush.
		SetAntialiasPreference(p.antialias)
		SetGlobalAlphaPreference(p.GlobalAlpha())
		c.publishGradient(p)
	}
}

// publishGradient transforms the paint's gradient geometry into device space and
// hands it to the batch.
//
// A radial gradient keeps a scalar radius, so a non-uniform scale makes it an
// ellipse the shader cannot represent; the radius takes the scale along the
// gradient's own x axis, which is exact for uniform scales (the normal case).
func (c *Canvas) publishGradient(p *Paint) {
	g := p.gradient
	if g.Type == GradientNone || g.StopCount == 0 {
		SetCurrentGradient(Gradient{})
		return
	}
	// The shader evaluates the ramp against gl_FragCoord, which is in GL
	// window space with y growing UP, while the canvas grows down. Flip on the
	// way in, exactly like the clip planes.
	// c.height, not the global screen height: the global belongs to whichever
	// window last called SetScreenSize, so flipping with it offsets gradients (and
	// anything else flipped this way) by the difference between the two windows.
	sh := c.height
	flip := func(x, y float32) (float32, float32) {
		x, y = c.transformPoint(x, y)
		return x, sh - y
	}
	mg := g
	mg.Start.X, mg.Start.Y = flip(g.Start.X, g.Start.Y)
	mg.End.X, mg.End.Y = flip(g.End.X, g.End.Y)
	mg.Center.X, mg.Center.Y = flip(g.Center.X, g.Center.Y)
	ex, ey := flip(g.Center.X+g.Radius, g.Center.Y)
	mg.Radius = sqrtf((ex-mg.Center.X)*(ex-mg.Center.X) + (ey-mg.Center.Y)*(ey-mg.Center.Y))
	SetCurrentGradient(mg)
}

var textScratch []float32

var debugTextCount int

// DrawText renders text using a bitmap font atlas.
func (c *Canvas) DrawText(text string, x, y float32, font *Font, p *Paint) {
	c.applyBlend(p)
	if font == nil || font.face == nil || font.atlas == nil || p == nil || len(text) == 0 || c.renderer == nil {
		if os.Getenv("MEUI_LAYOUT_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[drawText skip] font=%v face=%v atlas=%v p=%v len=%d renderer=%v\n",
				font != nil, font == nil || font.face != nil, font == nil || font.atlas != nil, p != nil, len(text), c.renderer != nil)
		}
		return
	}
	x, y = c.transformPoint(x, y)

	// Pixel-snap the pen origin. Hinted advances are already integer,
	// so an integer origin keeps EVERY glyph quad integer-aligned —
	// GL_LINEAR then samples texels 1:1 instead of resampling them
	// into half-pixel gradients (the residual softness after hinting).
	// DrawTextWithSubPixel remains available for fractional layouts.
	x = float32(math.Round(float64(x)))
	y = float32(math.Round(float64(y)))

	// MEUI_BTN_DEBUG: where does the bar-button text land, and what
	// clip will be stamped onto its quads?
	if os.Getenv("MEUI_BTN_DEBUG") != "" && text == "New Window" {
		minX, minY, maxX, maxY := curClipQuad()
		fmt.Printf("[btn-drawtext] transformed=(%.1f,%.1f) clip=(%.0f,%.0f)-(%.0f,%.0f)\n", x, y, minX, minY, maxX, maxY)
	}

	font.atlas.upload()
	texID := font.atlas.texID

	// Reuse scratch buffer — grow if needed (8 floats per vertex, 4 per glyph)
	maxVerts := len(text) * 32
	if cap(textScratch) < maxVerts {
		textScratch = make([]float32, 0, maxVerts*2)
	}
	verts := textScratch[:0]
	tc := p.color
	tr := float32((tc>>16)&0xFF) / 255
	tg := float32((tc>>8)&0xFF) / 255
	tb := float32(tc&0xFF) / 255
	ta := float32((tc>>24)&0xFF) / 255

	// The shaping walk is shared with TextWidth — measurement and
	// drawing cannot disagree (see shapeWalk).
	font.shapeWalk(text, float64(x), float64(y), float64(font.LineHeight()),
		func(r rune, penX, penY float64, g *atlasGlyph) {
			if os.Getenv("MEUI_DEBUG_TEXT") != "" && debugTextCount < 60 {
				debugTextCount++
				fmt.Printf("[glyph] %q size=%.0f atlas=%d uv=(%.4f,%.4f %.4f,%.4f) adv=%.1f pen=%.1f\n",
					r, font.size, font.atlas.texID, g.uvX, g.uvY, g.uvW, g.uvH, g.advance, penX)
			}
			gx := float32(penX) + g.bearingX
			gy := float32(penY) - g.bearingY
			gw := float32(g.width)
			gh := float32(g.height)
			u0, v0 := g.uvX, g.uvY
			u1, v1 := g.uvX+g.uvW, g.uvY+g.uvH

			verts = append(verts,
				gx, gy, u0, v0, tr, tg, tb, ta,
				gx+gw, gy, u1, v0, tr, tg, tb, ta,
				gx, gy+gh, u0, v1, tr, tg, tb, ta,
				gx+gw, gy+gh, u1, v1, tr, tg, tb, ta,
			)
		})

	if len(verts) > 0 {
		// Glyphs rasterized by THIS call (getGlyph above) marked the atlas
		// dirty AFTER the upload at the top of this function. Re-upload so
		// the texture is current when the batch flushes — otherwise glyphs
		// first seen in the last DrawText of a frame render as empty quads.
		font.atlas.upload()
		c.renderer.DrawTexturedQuads(verts, texID, p.color, false)
	}
}

// DrawSimpleString draws simple text using the system font
func (c *Canvas) DrawSimpleString(text string, x, y float32, p *Paint) {
	c.applyBlend(p)
	if p == nil {
		return
	}
	c.DrawText(text, x, y, SystemFont(), p)
}

// Color is ARGB packed color
type Color uint32

// RGB creates a Color from RGB values
func RGB(r, g, b uint8) Color {
	return Color(0xFF000000 | (Color(r) << 16) | (Color(g) << 8) | Color(b))
}

// RGBA creates a Color from RGBA values
func RGBA(r, g, b, a uint8) Color {
	return Color((Color(a) << 24) | (Color(r) << 16) | (Color(g) << 8) | Color(b))
}

// Paint returns a Paint for this Color. Here to satisfy the Ink
// interface in the shim so a Color can be used wherever an Ink is
// expected.
func (c Color) Paint(_ *Canvas, _ Rect, style int32) *Paint {
	paint := NewPaint()
	paint.SetStyle(style)
	paint.SetColor(c)
	return paint
}

// Paint holds drawing style (subset of unison.Paint)
type Paint struct {
	color      Color
	style      PaintStyle
	strokeW    float32
	strokeCap  StrokeCap
	strokeJoin StrokeJoin
	// miterLimit caps the miter length (a multiple of the half stroke
	// width) before a miter join degrades to a bevel; 4 is the
	// conventional default.
	miterLimit float32
	// antialias asks the SDF pipeline for an analytic edge ramp. Off
	// yields hard, pixel-aligned edges.
	antialias bool
	// globalAlpha multiplies the alpha of everything this paint draws,
	// on top of the colour's own alpha. 1 = no modulation.
	globalAlpha float32
	blendMode   BlendMode
	gradient    Gradient
}

// NewPaint creates a new Paint
func NewPaint() *Paint {
	return &Paint{
		color:       0xFFFFFFFF,
		style:       Fill,
		strokeW:     1,
		strokeCap:   StrokeCapButt,
		strokeJoin:  StrokeJoinMiter,
		miterLimit:  4,
		antialias:   true,
		globalAlpha: 1,
		blendMode:   BlendModeNormal,
	}
}

// SetGradient replaces the paint's gradient wholesale.
func (p *Paint) SetGradient(g Gradient) { p.gradient = g }

// SetLinearGradient sets a multi-stop linear gradient running start→end.
func (p *Paint) SetLinearGradient(start, end Point, stops []GradientStop) {
	p.gradient.Type = GradientLinear
	p.gradient.Start = start
	p.gradient.End = end
	p.gradient.SetStops(stops)
}

// SetRadialGradient sets a multi-stop radial gradient centred at center.
func (p *Paint) SetRadialGradient(center Point, radius float32, stops []GradientStop) {
	p.gradient.Type = GradientRadial
	p.gradient.Center = center
	p.gradient.Radius = radius
	p.gradient.SetStops(stops)
}

// SetConicGradient sets a multi-stop conic gradient sweeping around center from
// startAngle degrees.
func (p *Paint) SetConicGradient(center Point, startAngle float32, stops []GradientStop) {
	p.gradient.Type = GradientConic
	p.gradient.Center = center
	p.gradient.StartAngle = startAngle
	p.gradient.SetStops(stops)
}

// ClearGradient removes any gradient from this paint.
func (p *Paint) ClearGradient() {
	p.gradient.Type = GradientNone
}

// Gradient returns the paint's gradient.
func (p *Paint) Gradient() Gradient {
	return p.gradient
}

// SetColor sets the paint color
func (p *Paint) SetColor(c Color) {
	p.color = c
}

// SetStyle sets the paint style
func (p *Paint) SetStyle(style int32) {
	p.style = PaintStyle(style)
}

// SetStrokeWidth sets the stroke width
func (p *Paint) SetStrokeWidth(w float32) {
	p.strokeW = w
}

// Color returns the paint color
func (p *Paint) Color() Color {
	return p.color
}

// StrokeWidth returns the stroke width
func (p *Paint) StrokeWidth() float32 {
	return p.strokeW
}

// Style returns the paint style
func (p *Paint) Style() PaintStyle {
	return p.style
}

// SetStrokeCap sets the stroke cap
func (p *Paint) SetStrokeCap(cap StrokeCap) {
	p.strokeCap = cap
}

// StrokeCap returns the stroke cap
func (p *Paint) StrokeCap() StrokeCap {
	return p.strokeCap
}

// SetStrokeJoin sets the stroke join
func (p *Paint) SetStrokeJoin(join StrokeJoin) {
	p.strokeJoin = join
}

// StrokeJoin returns the stroke join
func (p *Paint) StrokeJoin() StrokeJoin {
	return p.strokeJoin
}

// SetMiterLimit sets the miter limit: the longest miter allowed, as a
// multiple of half the stroke width, before a miter join falls back to a
// bevel. Values <= 0 mean the conventional default of 4.
func (p *Paint) SetMiterLimit(limit float32) {
	p.miterLimit = limit
}

// MiterLimit returns the miter limit (4 when unset).
func (p *Paint) MiterLimit() float32 {
	if p == nil || p.miterLimit <= 0 {
		return 4
	}
	return p.miterLimit
}

// SetAntialias turns the SDF pipeline's analytic edge ramp on or off.
func (p *Paint) SetAntialias(on bool) {
	p.antialias = on
}

// Antialias reports the antialiasing preference (on by default).
func (p *Paint) Antialias() bool {
	if p == nil {
		return true
	}
	return p.antialias
}

// SetGlobalAlpha sets the alpha multiplier applied to everything this paint
// draws, on top of the colour's own alpha. Values outside 0..1 are clamped;
// 1 means no modulation.
func (p *Paint) SetGlobalAlpha(a float32) {
	p.globalAlpha = a
}

// GlobalAlpha returns the alpha multiplier (1 when unset).
func (p *Paint) GlobalAlpha() float32 {
	if p == nil || p.globalAlpha < 0 {
		return 1
	}
	if p.globalAlpha == 0 {
		return 0
	}
	if p.globalAlpha > 1 {
		return 1
	}
	return p.globalAlpha
}

// SetBlendMode sets the blend mode
func (p *Paint) SetBlendMode(mode BlendMode) {
	p.blendMode = mode
}

// BlendMode returns the blend mode
func (p *Paint) BlendMode() BlendMode {
	return p.blendMode
}

// Font holds font data for GPU-accelerated text rendering
type Font struct {
	family string
	size   float32
	face   *truetype.Font
	atlas  *glyphAtlas

	// Synthetic style flags (mirror the atlas): bold widens each glyph
	// ~1px, so advances and measurements grow to match.
	fakeBold   bool
	fakeItalic bool
}

// NewFont creates a font with the given truetype face and size.
// Atlas cache keyed by face + size + style to reuse atlases across fonts.
var atlasCache = make(map[string]*glyphAtlas)

func atlasCacheKey(face *truetype.Font, size float32, fakeBold, fakeItalic bool) string {
	return fmt.Sprintf("%p:%.1f:%t%t", face, size, fakeBold, fakeItalic)
}

// NewFont delegates to NewFontStyled with no synthetic styles.
func NewFont(family string, size float32, face *truetype.Font) *Font {
	return NewFontStyled(family, size, face, false, false)
}

// NewFontStyled creates a font, optionally with synthetic bold/italic
// (rasterized emboldening/shear for faces lacking real companions —
// Skia-style synthesis). Styled variants get their own atlas.
func NewFontStyled(family string, size float32, face *truetype.Font, fakeBold, fakeItalic bool) *Font {
	// A Font with no face cannot be shaped: the walk indexes f.face for every
	// rune and the atlas rasterizes from it. A caller that passes nil (a theme
	// whose family is not installed, an app that never loaded one) would
	// otherwise reach the atlas with nil and crash inside paint. Substitute the
	// face this binary carries — the same one the mobile fallback uses.
	if face == nil {
		face = EmbeddedRobotoFace("Regular")
	}
	key := atlasCacheKey(face, size, fakeBold, fakeItalic)
	atlas, ok := atlasCache[key]
	if !ok {
		atlas = newGlyphAtlas(face, size, fakeBold, fakeItalic)
		// Symbol fallback: glyphs missing from the primary face try the
		// system font (DejaVu carries ✓ ☑ ⚠ and friends; Roboto doesn't).
		if systemTTF != nil && systemTTF != face {
			atlas.fallbacks = append(atlas.fallbacks, systemTTF)
		}
		atlasCache[key] = atlas
	}
	return &Font{
		family:     family,
		size:       size,
		face:       face,
		atlas:      atlas,
		fakeBold:   fakeBold,
		fakeItalic: fakeItalic,
	}
}

// SystemFont returns the default system font
func SystemFont() *Font {
	if systemTTF == nil {
		return nil
	}
	return NewFont("system", fontSize, systemTTF)
}

// Baseline returns the baseline that vertically centers text optically.
// Widget code centers with (h - LineHeight)/2 + Baseline; using the font
// bbox ascent here (bounds.Max.Y ≈ 1.23em — accent headroom over ALL
// glyphs) pushed every centered label ~3.5px below the box center. The
// correct value is the line-box midpoint plus half the cap height, with
// cap height measured from the rasterized 'H' glyph.
func (f *Font) Baseline() float32 {
	if f.face == nil {
		return f.size * 0.96
	}
	lh := f.LineHeight()
	cap := f.size * 0.72
	if f.atlas != nil {
		f.atlas.getGlyph('H') // rasterizes on demand, measures capHeight
		if f.atlas.capHeight > 0 {
			cap = f.atlas.capHeight
		}
	}
	return lh/2 + cap/2
}

// LineHeight returns the font line height
func (f *Font) LineHeight() float32 {
	return f.size * 1.2
}

// TextWidth returns the width of text in pixels using actual glyph metrics
// shapeWalk is the SINGLE text shaping pass: it walks text exactly as
// it is rendered (kern + whole-pixel atlas advance per glyph, newline
// resets the pen to startX and drops lineH, variation selectors and
// unknown glyphs skipped/fallback) and calls fn for every positioned
// glyph. Drawing, measurement and hit-testing all consume this walk —
// they cannot disagree.
func (f *Font) shapeWalk(text string, startX, startY, lineH float64, fn func(r rune, penX, penY float64, g *atlasGlyph)) {
	if f.face == nil {
		return
	}
	scale := fontSizeInPixels(f.size)
	dx, dy := startX, startY
	prev, hasPrev := truetype.Index(0), false
	for _, r := range text {
		if r == '\n' {
			// Newline: pen returns to startX and drops one line;
			// kern state is kept (DrawText parity).
			dx = startX
			dy += lineH
			continue
		}
		// Variation selectors (VS15/VS16) are invisible formatting.
		if r == 0xFE0E || r == 0xFE0F {
			continue
		}
		idx := f.face.Index(r)
		if hasPrev {
			dx += float64(f.face.Kern(scale, prev, idx)) / 64.0
		}
		g := f.atlas.getGlyph(r)
		if g == nil {
			// Unknown glyph: fixed fallback advance, no ink (the
			// caller emits nothing for it).
			dx += 5
			prev, hasPrev = idx, true
			continue
		}
		fn(r, dx, dy, g)
		dx += float64(g.advance)
		prev, hasPrev = idx, true
	}
}

// TextWidth measures with the SAME engine that draws: the glyph atlas.
// It mirrors DrawText's pen walk exactly (kern + whole-pixel atlas
// advance per glyph, same skips and same nil fallback) so measurement
// can never drift from rendering — synthetic styles included, since
// their advance adjustments live in the atlas itself. Returns the
// widest line.
func (f *Font) TextWidth(text string) float32 {
	if f.face == nil {
		return float32(len(text)) * f.size * 0.6
	}
	w := 0.0
	f.shapeWalk(text, 0, 0, 0, func(_ rune, penX, _ float64, g *atlasGlyph) {
		if e := penX + float64(g.advance); e > w {
			w = e
		}
	})
	out := float32(w)
	// Synthetic styles' ink overhangs the final advance (~0.5px bold
	// dilation, ~1.6px italic shear lean at the top). Layout width
	// includes a trailing clearance so text FOLLOWING a styled run —
	// the next span, table cell, whatever — never overlaps it. Living
	// here (not in callers) keeps PreferredDimensions, paintRichText
	// and linkAt all measuring identically.
	if f.fakeBold || f.fakeItalic {
		out++
	}
	return out
}

// FontDescriptor describes a font
type FontDescriptor struct {
	Family string
	Size   float32
}

// Descriptor returns the font descriptor
func (f *Font) Descriptor() FontDescriptor {
	return FontDescriptor{
		Family: f.family,
		Size:   f.size,
	}
}

// Size returns the font size in points
func (f *Font) Size() float32 { return f.size }

// Face returns the underlying truetype font face
func (f *Font) Face() *truetype.Font { return f.face }

// Family returns the font family name
func (f *Font) Family() string { return f.family }

// DrawRect draws a rectangle at (x, y) with given width and height.
// Triangulated into 2 triangles and accumulated in the frame batch.
func (c *Canvas) DrawRect(x, y, w, h float32, p *Paint) {
	// MEUI_CANVAS_MATRIX_TRACE: the transform in force when a rect is submitted.
	// currentMatrix is the one stateful store in the stack never logged - an
	// unbalanced Translate (the shim's Translate(0, bar), or a Save/Restore pair
	// in Window.Paint) would offset every coordinate appended after it, by the bar
	// or by a size-derived amount, with every value measured elsewhere still
	// correct. The rect printed is pre-transform.
	if os.Getenv("MEUI_CANVAS_MATRIX_TRACE") != "" {
		m := c.currentMatrix
		fmt.Printf("[cmtx] translate=(%.0f,%.0f) rect=(%.0f,%.0f %.0fx%.0f)\n", m[2], m[5], x, y, w, h)
		// The canvas object itself: if these read the OLD size while the window is the
		// new one, the failing frame is drawing into a stale canvas (P5ez).
		fmt.Printf("[cdims] canvas=%.0fx%.0f clip=(%.0f,%.0f %.0fx%.0f)\n",
			c.width, c.height, c.currentClip.X, c.currentClip.Y, c.currentClip.Width, c.currentClip.Height)
	}
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready || w <= 0 || h <= 0 {
		return
	}
	if p == nil {
		p = NewPaint()
	}
	// Fills and strokes both go through the instanced SDF batch — including
	// gradient ones, which the batch carries per vertex: one draw call per
	// frame instead of ~10 GL calls per rect (RENDERER_OPTIMIZATION.md O2/O3).
	if c.matrixRotated() {
		// The SDF batch rotates its quad about the centre, so a rotated rect is an
		// exact rotated rect — with the radius, the stroke and the analytic edge
		// ramp, none of which the triangle fallback could carry. (That fallback is
		// gone: it was the fix for REVIEW §6 and this supersedes it.)
		gx, gy, gw, gh, ang := c.rectStyledGeometry(x, y, w, h)
		c.renderer.DrawRectStyled(gx, gy, gw, gh, 0, p.strokeW, p.color, int32(p.style), ang)
		return
	}
	x, y, w, h = c.transformRect(x, y, w, h)
	c.renderer.DrawRectStyled(x, y, w, h, 0, p.strokeW, p.color, int32(p.style), 0)
}

// emitQuadTriangles emits two triangles covering the four given (already
// transformed) corners: 0-1-2 and 0-2-3.
func emitQuadTriangles(c *Canvas, q [4][2]float32, color Color) {
	writeTriVert(triVertScratch[:], 0, q[0][0], q[0][1], 1, 0, 0, color)
	writeTriVert(triVertScratch[:], 9, q[1][0], q[1][1], 0, 1, 0, color)
	writeTriVert(triVertScratch[:], 18, q[2][0], q[2][1], 0, 0, 1, color)
	c.renderer.DrawTriangles(triVertScratch[:], color)
	writeTriVert(triVertScratch[:], 0, q[0][0], q[0][1], 1, 0, 0, color)
	writeTriVert(triVertScratch[:], 9, q[2][0], q[2][1], 0, 1, 0, color)
	writeTriVert(triVertScratch[:], 18, q[3][0], q[3][1], 0, 0, 1, color)
	c.renderer.DrawTriangles(triVertScratch[:], color)
}

// emitRectTriangles adds two triangles representing the rect to the triangle batch.
func emitRectTriangles(c *Canvas, x, y, w, h float32, color Color) {
	x, y, w, h = c.transformRect(x, y, w, h)
	emitQuadTriangles(c, [4][2]float32{
		{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h},
	}, color)
}

// DrawLine draws a line from (x1, y1) to (x2, y2)
func (c *Canvas) DrawLine(x1, y1, x2, y2 float32, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready {
		return
	}
	x1, y1 = c.transformPoint(x1, y1)
	x2, y2 = c.transformPoint(x2, y2)
	dx := x2 - x1
	dy := y2 - y1
	length := sqrtf(dx*dx + dy*dy)
	if length != length || p.strokeW != p.strokeW {
		// NaN coordinates — trace the source
		println("[NaN LINE]", x1, y1, x2, y2, "strokeW:", p.strokeW)
		return
	}
	if length < 0.1 || p.strokeW <= 0 {
		return
	}
	// 1px axis-aligned lines snap to the pixel grid. A line centered on
	// an integer coordinate splits across two pixels at half coverage —
	// the same soft-2px defect the SDF stroke had (bar borders,
	// separators, rules). Snapped quads cover exactly one pixel row or
	// column at full coverage.
	if p.strokeW <= 1 {
		// Route through the SDF fill instead of the triangle batch: the
		// tri shader's barycentric AA treats each quad's internal
		// diagonal as an edge, so a 1px quad only reaches ~0.5 coverage
		// per pixel (the faint 2px line defect). The SDF fill covers a
		// pixel-aligned 1px rect exactly.
		if dy == 0 {
			y := float32(math.Floor(float64(y1)))
			c.renderer.DrawRectStyled(x1, y, x2-x1, 1, 0, 0, p.color, 0, 0)
			return
		}
		if dx == 0 {
			x := float32(math.Floor(float64(x1)))
			top, bot := y1, y2
			if bot < top {
				top, bot = bot, top
			}
			c.renderer.DrawRectStyled(x, float32(math.Floor(float64(top))), 1, bot-top, 0, 0, p.color, 0, 0)
			return
		}
	}
	// Line caps. A butt cap ends exactly at the endpoint (the quad below).
	// Square extends the line by half the stroke width; round extends it and
	// adds a disc at each end. The 1px pixel-snapped path above returns early
	// and stays butt: it draws rules and separators where caps are moot.
	endX1, endY1, endX2, endY2 := x1, y1, x2, y2
	capHalf := p.strokeW / 2
	switch p.strokeCap {
	case StrokeCapSquare, StrokeCapRound:
		ux, uy := dx/length, dy/length
		x1 -= ux * capHalf
		y1 -= uy * capHalf
		x2 += ux * capHalf
		y2 += uy * capHalf
	}
	// Compute perpendicular offset for line width
	nx := -dy / length * p.strokeW / 2
	ny := dx / length * p.strokeW / 2
	// 4 corners of the line quad
	ax, ay := x1+nx, y1+ny
	bx, by := x1-nx, y1-ny
	cx, cy := x2+nx, y2+ny
	dx2, dy2 := x2-nx, y2-ny
	// Emit 2 triangles (6 verts) through triangle batch
	writeTriVert(triVertScratch[:], 0, ax, ay, 1, 0, 0, p.color)
	writeTriVert(triVertScratch[:], 9, cx, cy, 0, 1, 0, p.color)
	writeTriVert(triVertScratch[:], 18, bx, by, 0, 0, 1, p.color)
	c.renderer.DrawTriangles(triVertScratch[:], p.color)
	writeTriVert(triVertScratch[:], 0, cx, cy, 1, 0, 0, p.color)
	writeTriVert(triVertScratch[:], 9, dx2, dy2, 0, 1, 0, p.color)
	writeTriVert(triVertScratch[:], 18, bx, by, 0, 0, 1, p.color)
	c.renderer.DrawTriangles(triVertScratch[:], p.color)

	// Round caps are discs centred on the (un-extended) endpoints.
	if p.strokeCap == StrokeCapRound {
		c.DrawCircle(endX1, endY1, capHalf, p)
		c.DrawCircle(endX2, endY2, capHalf, p)
	}
}

// strokeJoinAt draws the join at an interior vertex b between the segments
// (a→b) and (b→c). The toolkit never sets a join style today, so the default
// (miter) preserves the existing segment-overlap look.
func (c *Canvas) strokeJoinAt(a, b, cp Point, p *Paint) {
	d1x, d1y := b.X-a.X, b.Y-a.Y
	d2x, d2y := cp.X-b.X, cp.Y-b.Y
	l1 := sqrtf(d1x*d1x + d1y*d1y)
	l2 := sqrtf(d2x*d2x + d2y*d2y)
	if l1 == 0 || l2 == 0 {
		return
	}
	// Unit normals to each segment.
	n1x, n1y := -d1y/l1, d1x/l1
	n2x, n2y := -d2y/l2, d2x/l2
	h := p.strokeW / 2
	if h <= 0 {
		return
	}
	// The outer side of the turn. For direction vectors d1→d2, the outer side is
	// the -n side when the cross product is positive and the +n side otherwise;
	// getting this backwards draws the join on the inner side, where it hides
	// under the segments and leaves the outer notch unfilled.
	sgn := float32(-1)
	if d1x*d2y-d1y*d2x < 0 {
		sgn = 1
	}
	bevel := func() {
		p1 := Point{X: b.X + sgn*n1x*h, Y: b.Y + sgn*n1y*h}
		p2 := Point{X: b.X + sgn*n2x*h, Y: b.Y + sgn*n2y*h}
		c.DrawTriangleFilled(b, p1, p2, p)
	}
	switch p.strokeJoin {
	case StrokeJoinRound:
		c.DrawCircle(b.X, b.Y, h, p)
	case StrokeJoinBevel:
		bevel()
	default: // StrokeJoinMiter
		bx, by := n1x+n2x, n1y+n2y
		bl := sqrtf(bx*bx + by*by)
		if bl < 1e-6 {
			// Segments double back: a round join is the only sane shape.
			c.DrawCircle(b.X, b.Y, h, p)
			return
		}
		// Miter length = h / cos(θ/2), compared against the join's miter
		// limit (a multiple of the half width). Past the limit the miter
		// would spike, so it degrades to a bevel.
		cosHalf := bl / 2
		t := h / cosHalf
		limit := p.miterLimit
		if limit <= 0 {
			limit = 4
		}
		if t > h*limit {
			bevel()
			return
		}
		tipX, tipY := b.X+sgn*bx/bl*t, b.Y+sgn*by/bl*t
		c.DrawTriangleFilled(b,
			Point{X: b.X + sgn*n1x*h, Y: b.Y + sgn*n1y*h},
			Point{X: tipX, Y: tipY}, p)
		c.DrawTriangleFilled(b,
			Point{X: tipX, Y: tipY},
			Point{X: b.X + sgn*n2x*h, Y: b.Y + sgn*n2y*h}, p)
	}
}

// DrawLineRotated draws a rotated line starting at (x, y)
func (c *Canvas) DrawLineRotated(x, y, length, thickness, angle float32, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready {
		return
	}
	rad := angle * 3.14159265 / 180
	c.DrawLine(x, y, x+length*cosf(rad), y+length*sinf(rad), p)
}

// QuickRejectRect returns true if the rect is entirely outside the current clip.
func (c *Canvas) QuickRejectRect(rect Rect) bool {
	clip := c.currentClip
	if clip.Width <= 0 || clip.Height <= 0 {
		return false
	}
	if rect.X+rect.Width < clip.X || rect.X > clip.X+clip.Width {
		return true
	}
	if rect.Y+rect.Height < clip.Y || rect.Y > clip.Y+clip.Height {
		return true
	}
	return false
}

// QuickRejectPath returns true if the path bounds are entirely outside the current clip.
// DrawPoints renders a batch of points as filled circles.
func (c *Canvas) DrawPoints(verts []float32, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready || len(verts) < 3 {
		return
	}
	for i := 0; i+2 < len(verts); i += 3 {
		emitCircleTriangles(c, verts[i], verts[i+1], verts[i+2], verts[i+2], p.color)
	}
}

// DrawCircle draws a filled circle at (cx, cy) with radius r.
func (c *Canvas) DrawCircle(cx, cy, r float32, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready || r <= 0 {
		return
	}
	if p == nil {
		p = NewPaint()
	}
	// Transform the bounding box ONCE (transformRect applies the full
	// current matrix). The previous code transformed the center and then
	// transformed the top-left of the already-transformed center again —
	// under a parent translate the circle landed at twice the offset.
	if c.matrixRotated() {
		// A rotated rounded rect: the SDF batch rotates the quad and the shader
		// evaluates its rounded-box distance in the unrotated local frame, so the
		// corners stay circular and the ramp stays analytic.
		gx, gy, gw, gh, ang := c.rectStyledGeometry(cx-r, cy-r, 2*r, 2*r)
		radius := gw * 0.5
		if gh*0.5 < radius {
			radius = gh * 0.5
		}
		c.renderer.DrawRectStyled(gx, gy, gw, gh, radius, p.strokeW, p.color, int32(p.style), ang)
		return
	}
	x, y, w, h := c.transformRect(cx-r, cy-r, 2*r, 2*r)
	radius := w * 0.5
	if h*0.5 < radius {
		radius = h * 0.5
	}
	c.renderer.DrawRectStyled(x, y, w, h, radius, p.strokeW, p.color, int32(p.style), 0)
}

// DrawOval draws an oval (ellipse) within the given bounds
func (c *Canvas) DrawOval(x, y, w, h float32, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready || w <= 0 || h <= 0 {
		return
	}
	if p == nil {
		p = NewPaint()
	}
	x, y, w, h = c.transformRect(x, y, w, h)
	radius := w * 0.5
	if h*0.5 < radius {
		radius = h * 0.5
	}
	c.renderer.DrawRectStyled(x, y, w, h, radius, p.strokeW, p.color, int32(p.style), 0)
}

const circleSegs = 32

// emitCircleTriangles emits a filled circle/ellipse as a triangle fan through the batch.
func emitCircleTriangles(c *Canvas, cx, cy, rx, ry float32, color Color) {
	for i := 0; i < circleSegs; i++ {
		a1 := float64(i) * 2 * 3.14159265 / float64(circleSegs)
		a2 := float64(i+1) * 2 * 3.14159265 / float64(circleSegs)
		writeTriVert(triVertScratch[:], 0, cx, cy, 1, 0, 0, color)
		writeTriVert(triVertScratch[:], 9, cx+rx*float32(math.Cos(a1)), cy+ry*float32(math.Sin(a1)), 0, 1, 0, color)
		writeTriVert(triVertScratch[:], 18, cx+rx*float32(math.Cos(a2)), cy+ry*float32(math.Sin(a2)), 0, 0, 1, color)
		c.renderer.DrawTriangles(triVertScratch[:], color)
	}
}

// DrawArc draws an arc within the given oval bounds
func (c *Canvas) DrawArc(x, y, w, h, startAngle, sweepAngle float32, p *Paint) {
	c.applyBlend(p)
	if w <= 0 || h <= 0 {
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if !rectRenderer.ready {
		return
	}

	// Draw arc as a series of lines (angles in degrees)
	segments := 32
	for i := 0; i < segments; i++ {
		t := float32(i) / float32(segments)
		angle := (startAngle + sweepAngle*t) * 3.14159265 / 180 // Convert to radians
		cosA := cosf(angle)
		sinA := sinf(angle)

		t2 := float32(i+1) / float32(segments)
		angle2 := (startAngle + sweepAngle*t2) * 3.14159265 / 180
		cosA2 := cosf(angle2)
		sinA2 := sinf(angle2)

		x1 := x + w/2 + (w/2)*cosA
		y1 := y + h/2 + (h/2)*sinA
		x2 := x + w/2 + (w/2)*cosA2
		y2 := y + h/2 + (h/2)*sinA2

		c.DrawLine(x1, y1, x2, y2, p)
	}
}

// DrawColor fills the entire canvas with a color using the specified blend mode.
func (c *Canvas) DrawColor(col uint32, mode BlendMode) {
	if c.renderer == nil {
		return
	}
	c.renderer.DrawColor(col, mode)
}

// contourVertices holds a single contour's flattened vertices
type contourVertices struct {
	points []Point
}

// DrawPath draws a path by splitting into contours, flattening curves,
// triangulating each contour with earcut, and submitting triangles to the GPU.
func (c *Canvas) DrawPath(path *Path, p *Paint) {
	c.applyBlend(p)
	if path == nil || len(path.points) == 0 {
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if !rectRenderer.ready {
		return
	}

	// O6: cache the tessellation in LOCAL (untransformed) space keyed by the
	// path geometry. At draw time, transform each cached vertex and stamp the
	// paint color — far cheaper than re-running earcut per frame.
	key := pathCacheKey(path)
	entry := pathCacheLookup(key)
	if os.Getenv("MEUI_NO_PATHCACHE") != "" {
		entry = nil // bisect: disable cache
	}
	var contours []contourVertices
	if p.style == Stroke || p.style == FillStroke {
		// Strokes walk LOCAL contour points. Every primitive that draws a piece
		// of the stroke — DrawLine for a segment, DrawCircle for a join or a cap
		// — applies the canvas transform itself, so transforming here as well
		// put every stroked path at twice the canvas origin: a cable in a view
		// offset by the window's title bar landed a whole bar below its own
		// sockets, and a circle-marked join landed at the untransformed point.
		contours = buildContours(path)
	}
	if entry == nil {
		contours := buildContours(path)
		localTris := make([]float32, 0, 256)
		var minX, minY, maxX, maxY float32
		boundsSet := false
		for _, ct := range contours {
			if len(ct.points) < 3 {
				continue
			}
			for _, tri := range earcut(ct.points) {
				for _, pt := range tri {
					localTris = append(localTris, pt.X, pt.Y, 1, 0, 0)
					if !boundsSet {
						minX, minY, maxX, maxY = pt.X, pt.Y, pt.X, pt.Y
						boundsSet = true
					} else {
						if pt.X < minX {
							minX = pt.X
						}
						if pt.Y < minY {
							minY = pt.Y
						}
						if pt.X > maxX {
							maxX = pt.X
						}
						if pt.Y > maxY {
							maxY = pt.Y
						}
					}
				}
			}
		}
		if !boundsSet {
			return
		}
		entry = &pathCacheEntry{
			tris:   localTris,
			bounds: Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY},
		}
		pathCacheInsert(key, entry)
	}

	if p.style == Fill || p.style == FillStroke {
		pr := float32((p.color>>16)&0xFF) / 255
		pg := float32((p.color>>8)&0xFF) / 255
		pb := float32(p.color&0xFF) / 255
		pa := float32((p.color>>24)&0xFF) / 255

		allTris := make([]float32, 0, len(entry.tris)/5*9)
		var minX, minY, maxX, maxY float32
		boundsSet := false
		for i := 0; i+4 < len(entry.tris); i += 5 {
			tx, ty := c.transformPoint(entry.tris[i], entry.tris[i+1])
			// NaN/garbage guard: one bad vertex rasterizes as a full-screen
			// triangle (NaN skips the min/max bounds checks).
			if tx != tx || ty != ty || tx > 1e6 || ty > 1e6 || tx < -1e6 || ty < -1e6 {
				if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
					fmt.Printf("[path NaN/garbage vertex] (%v, %v)\n", tx, ty)
				}
				continue
			}
			allTris = append(allTris, tx, ty, entry.tris[i+2], entry.tris[i+3], entry.tris[i+4], pr, pg, pb, pa)
			if !boundsSet {
				minX, minY, maxX, maxY = tx, ty, tx, ty
				boundsSet = true
			} else {
				if tx < minX {
					minX = tx
				}
				if ty < minY {
					minY = ty
				}
				if tx > maxX {
					maxX = tx
				}
				if ty > maxY {
					maxY = ty
				}
			}
		}
		if boundsSet {
			if pf, ok := c.renderer.(interface {
				DrawPathFill(tris []float32, color Color, bounds Rect)
			}); ok {
				pf.DrawPathFill(allTris, p.color, Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY})
			} else {
				// Fallback: per-triangle batched path (legacy backends).
				// Local vertices: DrawTriangleFilled transforms them itself.
				for i := 0; i+4 < len(entry.tris); i += 15 {
					c.DrawTriangleFilled(
						Point{X: entry.tris[i], Y: entry.tris[i+1]},
						Point{X: entry.tris[i+5], Y: entry.tris[i+6]},
						Point{X: entry.tris[i+10], Y: entry.tris[i+11]},
						p,
					)
				}
			}
		}
	}

	if p.style == Stroke || p.style == FillStroke {
		segs, joins := strokeGeometry(contours)
		for _, seg := range segs {
			c.DrawLine(seg[0].X, seg[0].Y, seg[1].X, seg[1].Y, p)
		}
		// Joins cover the interior vertices; the segment ends keep the paint's caps.
		for _, j := range joins {
			c.strokeJoinAt(j[0], j[1], j[2], p)
		}
	}
}

// strokeGeometry turns a stroked path's contours into the segments and joins to
// draw, in the path's OWN (local) coordinates. Each piece is drawn by a primitive
// that applies the canvas transform itself — DrawLine for a segment, DrawCircle
// for a join or a cap — so this must not transform them too: doing that put every
// stroked path at twice the canvas origin (a cable in a view offset by the
// window's title bar landed a whole bar below its own sockets, while the joins,
// drawn through DrawCircle, landed somewhere else again).
func strokeGeometry(contours []contourVertices) (segs [][2]Point, joins [][3]Point) {
	for _, ct := range contours {
		pts := strokePoints(ct.points)
		for i := 0; i < len(pts)-1; i++ {
			segs = append(segs, [2]Point{pts[i], pts[i+1]})
		}
		for i := 1; i < len(pts)-1; i++ {
			joins = append(joins, [3]Point{pts[i-1], pts[i], pts[i+1]})
		}
	}
	return segs, joins
}

// strokeTolerance is how far a point may sit off the straight line through the
// run around it and still be absorbed by that line, in pixels. Half a pixel is
// still under what the edge's own anti-aliasing covers, and every point merged
// away is one fewer shared edge for the renderer to anti-alias — a curve with
// fewer, longer segments is a smoother cable.
const strokeTolerance = 0.5

// strokePoints reduces a flattened contour to the points a stroke actually turns
// at. The renderer draws each segment as its own anti-aliased quad, and those
// quads' shared edges are anti-aliased too — so a run of collinear segments (a
// straight cable, the flat part of a curve) comes out as a dashed line where a
// single segment is solid, and a dense curve shows a seam at every step. Merging
// the run costs nothing visually: the points dropped are within a quarter of a
// pixel of the line that replaces them.
func strokePoints(points []Point) []Point {
	if len(points) < 3 {
		return points
	}
	keep := make([]bool, len(points))
	keep[0] = true
	keep[len(points)-1] = true
	var split func(lo, hi int)
	split = func(lo, hi int) {
		if hi-lo < 2 {
			return
		}
		worst, at := float32(-1), -1
		for i := lo + 1; i < hi; i++ {
			if d := pointLineDistance(points[i], points[lo], points[hi]); d > worst {
				worst, at = d, i
			}
		}
		if worst <= strokeTolerance || at < 0 {
			return
		}
		keep[at] = true
		split(lo, at)
		split(at, hi)
	}
	split(0, len(points)-1)

	out := make([]Point, 0, len(points))
	for i, p := range points {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// pointLineDistance is the distance from p to the line through a and b.
func pointLineDistance(p, a, b Point) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	length := sqrtf(dx*dx + dy*dy)
	if length < 1e-6 {
		return sqrtf((p.X-a.X)*(p.X-a.X) + (p.Y-a.Y)*(p.Y-a.Y))
	}
	return absf((p.X-a.X)*dy-(p.Y-a.Y)*dx) / length
}

// ---- O6: path tessellation cache ----

type pathCacheEntry struct {
	tris   []float32 // 5 floats/vertex: x, y, bary3 — LOCAL (untransformed) space
	bounds Rect      // local-space bounds
}

var pathCache = make(map[uint64]*pathCacheEntry)
var pathCacheOrder []uint64

const pathCacheMax = 512

func pathCacheKey(path *Path) uint64 {
	// FNV-1a over the raw path program (verb + coordinates)
	h := uint64(14695981039346656037)
	for _, pt := range path.points {
		h ^= uint64(pt.Verb)
		h *= 1099511628211
		h ^= uint64(math.Float32bits(pt.Pt.X))
		h *= 1099511628211
		h ^= uint64(math.Float32bits(pt.Pt.Y))
		h *= 1099511628211
	}
	return h
}

func pathCacheLookup(key uint64) *pathCacheEntry {
	e := pathCache[key]
	if e != nil {
		// refresh LRU-ish position
		for i, k := range pathCacheOrder {
			if k == key {
				pathCacheOrder = append(pathCacheOrder[:i], pathCacheOrder[i+1:]...)
				break
			}
		}
		pathCacheOrder = append(pathCacheOrder, key)
	}
	return e
}

func pathCacheInsert(key uint64, e *pathCacheEntry) {
	if len(pathCacheOrder) >= pathCacheMax {
		oldest := pathCacheOrder[0]
		pathCacheOrder = pathCacheOrder[1:]
		delete(pathCache, oldest)
	}
	pathCache[key] = e
	pathCacheOrder = append(pathCacheOrder, key)
}

// buildContours extracts individual contours from a path, flattening
// quadratic bezier curves into line segments, and closing each contour.
func buildContours(path *Path) []contourVertices {
	var contours []contourVertices
	var current []Point
	var contourStart Point
	var prevPt Point
	var inContour bool
	hasClose := false

	i := 0
	for i < len(path.points) {
		pt := path.points[i]
		switch pt.Verb {
		case PathMove:
			if inContour && len(current) >= 2 {
				if hasClose || len(current) >= 3 {
					contours = append(contours, contourVertices{points: current})
				}
			}
			contourStart = pt.Pt
			prevPt = pt.Pt
			inContour = true
			hasClose = false
			current = []Point{pt.Pt}
			i++
		case PathLine:
			current = append(current, pt.Pt)
			prevPt = pt.Pt
			i++
		case PathQuad:
			// QuadTo stores [control, end] as two consecutive PathQuad entries.
			// Only process when the next point is also QuadTo (we're at the control).
			if i+1 < len(path.points) && path.points[i+1].Verb == PathQuad {
				ctrlPt := pt.Pt
				endPt := path.points[i+1].Pt
				for s := 1; s <= 8; s++ {
					t := float32(s) / 8.0
					mt := 1 - t
					px := mt*mt*prevPt.X + 2*mt*t*ctrlPt.X + t*t*endPt.X
					py := mt*mt*prevPt.Y + 2*mt*t*ctrlPt.Y + t*t*endPt.Y
					current = append(current, Point{X: px, Y: py})
				}
				prevPt = endPt
				i += 2 // skip control and end
			} else {
				i++
			}
		case PathClose:
			hasClose = true
			if len(current) >= 2 {
				current = append(current, contourStart)
			}
			i++
		default:
			i++
		}
	}

	if inContour && len(current) >= 2 {
		if hasClose || len(current) >= 3 {
			contours = append(contours, contourVertices{points: current})
		}
	}

	return contours
}

// crossArea returns twice the signed area of triangle (a,b,c).
// In Y-down coordinates, positive means clockwise turn at b.
func crossArea(a, b, c Point) float32 {
	return (b.X-a.X)*(c.Y-a.Y) - (c.X-a.X)*(b.Y-a.Y)
}

// pointInTriangle returns true if p is inside or on triangle (a,b,c).
func pointInTriangle(p, a, b, c Point) bool {
	d1 := crossArea(p, a, b)
	d2 := crossArea(p, b, c)
	d3 := crossArea(p, c, a)
	hasNeg := d1 < -0.001 || d2 < -0.001 || d3 < -0.001
	hasPos := d1 > 0.001 || d2 > 0.001 || d3 > 0.001
	return !(hasNeg && hasPos)
}

// earcut performs ear-clipping triangulation on a simple polygon (concave or convex).
// Returns a list of triangles for GPU submission.
func earcut(vertices []Point) [][3]Point {
	n := len(vertices)
	if n < 3 {
		return nil
	}
	if n == 3 {
		return [][3]Point{{vertices[0], vertices[1], vertices[2]}}
	}

	// Determine winding: signed area
	var area float32
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		area += vertices[i].X * vertices[j].Y
		area -= vertices[j].X * vertices[i].Y
	}
	cw := area >= 0

	remaining := make([]Point, n)
	copy(remaining, vertices)

	var tris [][3]Point
	maxIter := n * n // safety limit
	for len(remaining) > 3 && maxIter > 0 {
		maxIter--
		clipped := false
		for i := 0; i < len(remaining); i++ {
			rlen := len(remaining)
			prev := remaining[(i+rlen-1)%rlen]
			curr := remaining[i]
			next := remaining[(i+1)%rlen]

			cross := crossArea(prev, curr, next)
			if cw && cross <= 0 {
				continue
			}
			if !cw && cross >= 0 {
				continue
			}

			ear := true
			for j := 0; j < rlen; j++ {
				if j == i || j == (i+rlen-1)%rlen || j == (i+1)%rlen {
					continue
				}
				if pointInTriangle(remaining[j], prev, curr, next) {
					ear = false
					break
				}
			}
			if !ear {
				continue
			}

			tris = append(tris, [3]Point{prev, curr, next})
			remaining = append(remaining[:i], remaining[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			// Fallback: fan from first vertex
			for i := 1; i < len(remaining)-1; i++ {
				tris = append(tris, [3]Point{remaining[0], remaining[i], remaining[i+1]})
			}
			break
		}
	}

	if len(remaining) == 3 {
		tris = append(tris, [3]Point{remaining[0], remaining[1], remaining[2]})
	}

	return tris
}

// Package-level scratch buffers to avoid per-call allocations in hot paths.
var triVertScratch [27]float32 // 3 verts x (pos2 + bary3 + color4)

// DrawTriangleFilled draws a filled triangle using screen-space vertices
// with barycentric edge anti-aliasing. Accumulates into frame batch.
// writeTriVert writes one triangle vertex (pos2, bary3, color4 = 9 floats).
func writeTriVert(dst []float32, i int, x, y, b1, b2, b3 float32, col Color) {
	dst[i] = x
	dst[i+1] = y
	dst[i+2] = b1
	dst[i+3] = b2
	dst[i+4] = b3
	dst[i+5] = float32((col>>16)&0xFF) / 255
	dst[i+6] = float32((col>>8)&0xFF) / 255
	dst[i+7] = float32(col&0xFF) / 255
	dst[i+8] = float32((col>>24)&0xFF) / 255
}

// DrawTriangleFilled fills one triangle. Like every other Canvas entry point it
// takes LOCAL coordinates and applies the canvas transform itself — the stroke
// walk's joins pass the same local points it hands to DrawLine, and a triangle
// that skipped the transform landed a whole view offset away from its own stroke
// (visible as flecks of cable colour on the canvas).
func (c *Canvas) DrawTriangleFilled(p1, p2, p3 Point, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready {
		return
	}
	p1.X, p1.Y = c.transformPoint(p1.X, p1.Y)
	p2.X, p2.Y = c.transformPoint(p2.X, p2.Y)
	p3.X, p3.Y = c.transformPoint(p3.X, p3.Y)
	// Draw order is preserved *inside* the unified batch, so a triangle that
	// lands there needs no flush; only the legacy tri batch does. Without this a
	// stroke join (one or two triangles per vertex) flushed the whole batch per
	// triangle — the node graph's cables, drawn in three layers, cost a few
	// hundred draw calls a frame and made dragging them sluggish.
	if !unifiedBatch() {
		c.renderer.Flush()
	}
	writeTriVert(triVertScratch[:], 0, p1.X, p1.Y, 1, 0, 0, p.color)
	writeTriVert(triVertScratch[:], 9, p2.X, p2.Y, 0, 1, 0, p.color)
	writeTriVert(triVertScratch[:], 18, p3.X, p3.Y, 0, 0, 1, p.color)
	c.renderer.DrawTriangles(triVertScratch[:], p.color)
}

func absf(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func sqrtf(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}

// ptrOffset converts a byte offset to unsafe.Pointer for GL vertex attrib pointers.
//
// GL's API takes the offset of an attribute inside a bound VBO as a POINTER: the
// address IS the offset, and there is no integer form (glVertexAttribFormat with a
// real integer offset needs GL 4.3/ES 3.1, which this renderer does not target).
// So the value produced here is deliberately not a real pointer, and any pointer
// arithmetic reaching it is flagged:
//
//	go vet:  possible misuse of unsafe.Pointer
//	-race:   fatal error: checkptr: pointer arithmetic computed bad pointer value
//	         at ptrOffset -> flushSDFBatch
//
// That checkptr abort, found by the resize probe under -race (now behaviour's resize visual test), is
// the deterministic form of the P5dc intermittent crash. nocheckptr is the
// sanctioned escape hatch for exactly this case - a value that must be an integer
// but is passed as a pointer. The alternatives are worse: a base-relative address
// would be the wrong offset for GL, and a per-attribute VBO split would change the
// vertex path for no correctness gain.
//
//go:nocheckptr
func ptrOffset(offset int) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer(nil), offset)
}

// ptrOffsetBytes is ptrOffset for an offset already held as a uintptr - the
// chunked glDrawElements index offset. Same reasoning and same escape hatch.
//
//go:nocheckptr
func ptrOffsetBytes(offset uintptr) unsafe.Pointer {
	return unsafe.Pointer(offset)
}

func sinf(x float32) float32 {
	return float32(math.Sin(float64(x)))
}

func cosf(x float32) float32 {
	return float32(math.Cos(float64(x)))
}

func atan2f(y, x float32) float32 {
	return float32(math.Atan2(float64(y), float64(x)))
}

// DrawImage draws an image at the given position
// DrawImage draws an image at the given position (no scaling).
func (c *Canvas) DrawImage(img *Image, x, y float32, p *Paint) {
	c.applyBlend(p)
	if img == nil {
		return
	}
	c.DrawImageInRect(img, Rect{X: x, Y: y, Width: float32(img.width), Height: float32(img.height)}, p)
}

// DrawImageInRect draws an image scaled to fit the given rectangle.
// Uploads the image as a GL_RGBA texture and renders via textured quads.
func (c *Canvas) DrawImageInRect(img *Image, rect Rect, p *Paint) {
	c.applyBlend(p)
	if img == nil || len(img.pixels) == 0 || c.renderer == nil {
		rect.X, rect.Y, rect.Width, rect.Height = c.transformRect(rect.X, rect.Y, rect.Width, rect.Height)
		c.DrawRect(rect.X, rect.Y, rect.Width, rect.Height, p)
		return
	}
	if p == nil {
		p = NewPaint()
	}
	if !ensureImageTexture(img) {
		rect.X, rect.Y, rect.Width, rect.Height = c.transformRect(rect.X, rect.Y, rect.Width, rect.Height)
		c.DrawRect(rect.X, rect.Y, rect.Width, rect.Height, p)
		return
	}
	rx, ry, rw, rh := c.transformRect(rect.X, rect.Y, rect.Width, rect.Height)
	emitTexturedQuad(c, rx, ry, rw, rh, 0, 1, 1, 0, img.texture, p.color)
}

// DrawImageNine draws a 9-patch image. The source image is divided into 9 regions
// by centerRect (in pixel coordinates). The four corners are drawn at their natural
// size, edges stretch along one axis, and the center stretches in both directions.
func (c *Canvas) DrawImageNine(img *Image, centerRect Rect, dest Rect, p *Paint) {
	c.applyBlend(p)
	dest.X, dest.Y, dest.Width, dest.Height = c.transformRect(dest.X, dest.Y, dest.Width, dest.Height)
	if img == nil || len(img.pixels) == 0 || c.renderer == nil {
		c.DrawRect(dest.X, dest.Y, dest.Width, dest.Height, p)
		return
	}
	if !ensureImageTexture(img) {
		c.DrawRect(dest.X, dest.Y, dest.Width, dest.Height, p)
		return
	}

	iw := float32(img.width)
	ih := float32(img.height)
	l := centerRect.X
	t := centerRect.Y
	r := centerRect.X + centerRect.Width
	b := centerRect.Y + centerRect.Height
	uL, uT := l/iw, t/ih
	uR, uB := r/iw, b/ih

	dx, dy, dw, dh := dest.X, dest.Y, dest.Width, dest.Height
	sxL, sxT := l, t
	sxR := iw - r
	sxB := ih - b

	// Column widths: left=srcL, right=srcR, center=destW-srcL-srcR
	cw := [3]float32{sxL, dw - sxL - sxR, sxR}
	ch := [3]float32{sxT, dh - sxT - sxB, sxB}
	ux := [3]float32{0, uL, uR}
	uy := [3]float32{uB, uT, 0}
	vw := [3]float32{uL, uR - uL, 1 - uR}
	vh := [3]float32{uB, uT - uB, 1 - uT}

	xp := dx
	for xi := 0; xi < 3; xi++ {
		if cw[xi] <= 0 {
			continue
		}
		yp := dy
		for yi := 0; yi < 3; yi++ {
			if ch[yi] <= 0 {
				yp += ch[yi]
				continue
			}
			emitTexturedQuad(c, xp, yp, cw[xi], ch[yi], ux[xi], uy[yi]+vh[yi], ux[xi]+vw[xi], uy[yi], img.texture, p.color)
			yp += ch[yi]
		}
		xp += cw[xi]
	}
}

// ensureImageTexture uploads the image pixels to a GPU texture if not already
// done, and re-uploads when the pixel buffer was written to since the last
// upload (Image.MarkPixelsDirty).
func ensureImageTexture(img *Image) bool {
	if img.texture == 0 || img.dirty {
		runtime.LockOSThread()
		img.Upload()
		runtime.UnlockOSThread()
	}
	return img.texture != 0
}

// emitTexturedQuad emits a textured quad via the renderer.
// (u0,v0) is the UV for the quad's top-left corner, (u1,v1) for the
// bottom-right. Callers pass v flipped (v0=1, v1=0) when the texture
// rows are stored bottom-up (GL readbacks), which yields an upright image.
func emitTexturedQuad(c *Canvas, x, y, w, h, u0, v0, u1, v1 float32, texID uint32, color Color) {
	r := float32((color>>16)&0xFF) / 255
	g := float32((color>>8)&0xFF) / 255
	b := float32(color&0xFF) / 255
	a := float32((color>>24)&0xFF) / 255
	verts := []float32{
		x, y, u0, v0, r, g, b, a, // TL
		x + w, y, u1, v0, r, g, b, a, // TR
		x, y + h, u0, v1, r, g, b, a, // BL
		x + w, y + h, u1, v1, r, g, b, a, // BR
	}
	c.renderer.DrawTexturedQuads(verts, texID, color, true)
}

// DropShadow draws a blurred drop shadow using the FBO pipeline.
// The shadow shape is rendered offset, then blurred, composited back to screen.
// Call between BeginBlur/EndBlur for the blurred background shape.
// Uses the 2-pass separable Gaussian blur for smooth edges.
func (c *Canvas) DropShadow(rect Rect, p *Paint, offsetX, offsetY, radius float32) {
	c.applyBlend(p)
	if blurRenderer == nil || !blurRenderer.ready {
		return
	}

	// Start offscreen capture
	BeginBlur()

	// Draw the shadow shape offset
	p.SetColor(RGBA(0, 0, 0, 128))
	p.SetStyle(int32(Fill))
	c.DrawRect(rect.X+offsetX, rect.Y+offsetY, rect.Width, rect.Height, p)

	// Blur the shadow and composite to screen
	EndBlur(radius / 10)
}

// DrawRoundedRect draws a rectangle with rounded corners
// DrawRoundedRect draws a rounded rectangle using a path with corner arcs.
// Reuses the existing path rendering pipeline for proper edge AA.
func (c *Canvas) DrawRoundedRect(rect Rect, radiusX, radiusY float32, p *Paint) {
	c.applyBlend(p)
	if c.renderer == nil || !rectRenderer.ready {
		return
	}
	if p == nil {
		p = NewPaint()
	}
	w, h := rect.Width, rect.Height
	if w <= 0 || h <= 0 {
		return
	}
	radius := radiusX
	if radiusY < radius {
		radius = radiusY
	}
	if c.matrixRotated() {
		// A rotated rounded rect: the batch rotates its quad about the centre while
		// aLocal stays unrotated, so the shader's local-space rounded-box distance
		// gives the exact rotated shape — circular corners and the analytic ramp
		// intact. Without this it fell through to transformRect, which is the AABB.
		gx, gy, gw, gh, ang := c.rectStyledGeometry(rect.X, rect.Y, w, h)
		c.renderer.DrawRectStyled(gx, gy, gw, gh, radius, p.strokeW, p.color, int32(p.style), ang)
		return
	}
	tx, ty, tw, th := c.transformRect(rect.X, rect.Y, w, h)
	c.renderer.DrawRectStyled(tx, ty, tw, th, radius, p.strokeW, p.color, int32(p.style), 0)
}
