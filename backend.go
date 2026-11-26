package mobilegles

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Renderer is the interface that Canvas uses for all GPU operations.
// Each platform (desktop, Android, iOS) provides its own implementation.
type Renderer interface {
	// Lifecycle
	SetScreenSize(w, h float32)
	ScreenSize() (float32, float32)

	// Frame control
	Present()

	// Surface state
	ClearWithViewport(col uint32, x, y, w, h int32)
	SetViewport(x, y, w, h int32)
	// ScissorState reports the GL scissor box and whether the test is on.
	ScissorState() (x, y, w, h int32, test bool)
	// DrainErrors clears any pending GL error flags.
	DrainErrors()

	// State
	SetBlendMode(mode BlendMode)
	SetScissor(x, y, w, h int32)
	ClearScissor()

	// 2D drawing
	DrawRect(x, y, w, h float32, color Color, angle float32)
	DrawCircle(cx, cy, r float32, color Color)
	DrawOval(x, y, w, h float32, color Color)
	DrawLine(x1, y1, x2, y2 float32, color Color, strokeW float32)
	DrawTriangles(verts []float32, color Color)
	DrawTexturedQuads(verts []float32, texID uint32, color Color, isImage bool)
	// DrawColor fills the whole surface with col, composited with mode.
	DrawColor(col uint32, mode BlendMode)
	// DrawRectStyled is the shaped-rect primitive: fill or stroke, with a
	// corner radius and stroke width. It is the primitive the toolkit's rect
	// drawing actually uses, so it belongs in the contract.
	DrawRectStyled(x, y, w, h, radius, strokeW float32, color Color, style int32, angle float32)
	// Flush emits any pending batched draws, preserving strict draw order.
	Flush()

	// 3D drawing
}

// defaultRenderer is set by InitRenderer() and used by NewCanvas().
var defaultRenderer Renderer

// rendererFactory builds the process renderer. It is a variable so a second
// backend (for example Metal) can be installed before the renderer is
// initialised, without NewCanvas naming a concrete renderer.
var rendererFactory = func() Renderer { return &DesktopRenderer{} }

// SetRendererFactory installs the constructor used for the process renderer.
// Call it before InitRenderer.
func SetRendererFactory(f func() Renderer) {
	if f != nil {
		rendererFactory = f
	}
}

// NewRenderer builds the process renderer from the installed factory.
func NewRenderer() Renderer { return rendererFactory() }

// ErrNotSupported indicates a backend is not available on this platform.
type ErrNotSupported struct {
	Platform string
}

func (e *ErrNotSupported) Error() string {
	return "backend not supported on " + e.Platform
}

// appendWithClip expands src verts (oldStrides floats per vertex) into
// dst, stamping the current clip rect (minX, minY, maxX, maxY in GL
// window coords) as two extra vec2 attributes per vertex.
func appendWithClip(dst, src []float32, oldStrides int) []float32 {
	c0, c1, c2, c3 := curClipQuad()
	for i := 0; i < len(src); i += oldStrides {
		dst = append(dst, src[i:i+oldStrides]...)
		dst = append(dst, c0, c1, c2, c3)
	}
	return dst
}

// DesktopRenderer implements Renderer using the existing OpenGL globals.
type DesktopRenderer struct{}

func (d *DesktopRenderer) SetScreenSize(w, h float32) { SetScreenSize(w, h) }
func (d *DesktopRenderer) ScreenSize() (float32, float32) {
	return rectRenderer.screenWidth, rectRenderer.screenHeight
}
func (d *DesktopRenderer) Present() { flushBatches() }

// SetBlendMode selects how subsequent draws composite. The GL blend state is
// global while draws are batched, so a mode change is a batch boundary: the
// pending batch is flushed under the old mode first.
func (d *DesktopRenderer) SetBlendMode(mode BlendMode) {
	if mode == activeBlendMode {
		return
	}
	flushBatches()
	activeBlendMode = mode
}

// ClearWithViewport clears the surface and sets the viewport. Clears must never
// be scissored: batch flushes leave the scissor enabled, and on a fresh context
// the scissor box is stale, which would leave the rest of the buffer showing
// uninitialized surface garbage.
func (d *DesktopRenderer) ClearWithViewport(col uint32, x, y, w, h int32) {
	DumpRenderState("frame:clear")
	// MEUI_CLEAR_CALLER: who reaches this clear. P5el counted 1036 clears per
	// swap while the frame loop clears once, so the caller identity decides which
	// observation is wrong - the loop's or my instrument's.
	if os.Getenv("MEUI_CLEAR_CALLER") != "" {
		if _, file, line, ok := runtime.Caller(1); ok {
			fmt.Printf("[clearcaller] %s:%d rect=(%d,%d %dx%d)\n", filepath.Base(file), line, x, y, w, h)
		}
	}
	for glGetError() != 0 {
	}
	r := float32((col>>16)&0xFF) / 255
	g := float32((col>>8)&0xFF) / 255
	b := float32(col&0xFF) / 255
	a := float32((col>>24)&0xFF) / 255
	// MEUI_CLEAR_TRACE: every clear with its rect. Read against the swap trace,
	// which is the frame boundary: one clear per presented frame at the current
	// size is normal; a second clear, or one at the previous size arriving after
	// the content draw, would erase the region below the old height - the stuck
	// signature, with geometry, clips, viewport and swap all correct.
	if os.Getenv("MEUI_CLEAR_TRACE") != "" {
		fmt.Printf("[clear] rect=(%d,%d %dx%d)\n", x, y, w, h)
	}
	glClearColor(r, g, b, a)
	setViewport(x, y, w, h)
	glDisable(GL_SCISSOR_TEST)
	glClear(0x00004000 | GL_DEPTH_BUFFER_BIT | GL_STENCIL_BUFFER_BIT) // color + depth + stencil
}

func (d *DesktopRenderer) SetViewport(x, y, w, h int32) { setViewport(x, y, w, h) }

func (d *DesktopRenderer) ScissorState() (int32, int32, int32, int32, bool) {
	var box [4]int32
	var test int32
	glGetIntegerv(0x0C10 /*GL_SCISSOR_BOX*/, uintptr(unsafe.Pointer(&box[0])))
	glGetIntegerv(0x0C11 /*GL_SCISSOR_TEST*/, uintptr(unsafe.Pointer(&test)))
	return box[0], box[1], box[2], box[3], test != 0
}

func (d *DesktopRenderer) DrainErrors() {
	for glGetError() != 0 {
	}
}

// DrawColor fills the whole surface with col using the given blend mode. It is
// the legacy immediate path (the rect program), kept behind the interface so
// Canvas owns no GL state of its own.
func (d *DesktopRenderer) DrawColor(col uint32, mode BlendMode) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !rectRenderer.ready {
		return
	}
	red := float32((col>>16)&0xFF) / 255
	green := float32((col>>8)&0xFF) / 255
	blue := float32(col&0xFF) / 255
	alpha := float32((col>>24)&0xFF) / 255
	cacheUseProgram(rectRenderer.program)
	cacheBindVBO(rectRenderer.vbo)
	glVertexAttribPointer(rectRenderer.posAttrLoc, 2, GL_FLOAT, 0, 0, unsafe.Pointer(nil))
	cacheEnableAttrib(rectRenderer.posAttrLoc)
	glUniform1i(rectRenderer.blendModeLoc, int32(mode))
	glUniform1i(rectRenderer.shapeTypeLoc, shapeTypeRect)
	glUniform4f(rectRenderer.colorLoc, red, green, blue, alpha)
	glUniform2f(rectRenderer.posLoc, 0, 0)
	glUniform2f(rectRenderer.sizeLoc, 1, 1)
	glUniform1f(rectRenderer.angleLoc, 0)
	glUniform2f(rectRenderer.screenSizeLoc, 1, 1)
	glDrawArrays(GL_TRIANGLES, 0, 6)
}
func (d *DesktopRenderer) SetScissor(x, y, w, h int32) { setScissorTracked(x, y, w, h) }
func (d *DesktopRenderer) ClearScissor() {
	setScissorTracked(0, 0, int32(screenWidth), int32(screenHeight))
}
func (d *DesktopRenderer) DrawRectStyled(x, y, w, h, radius, strokeW float32, color Color, style int32, angle float32) {
	SDFBatchRect(x, y, w, h, radius, color, style, strokeW, angle)
}

func (d *DesktopRenderer) Flush() { flushSDFBatch() }

// unifiedBatch reports whether DrawTriangles batches into the unified SDF stream
// — which preserves submission order, so a caller does not have to flush first —
// rather than into the legacy tri batch.
func unifiedBatch() bool {
	return sdfBatch.ready && os.Getenv("MEUI_SKIP_SDF") != "1" && os.Getenv("MEUI_SKIP_TRI") != "1"
}

func (d *DesktopRenderer) DrawRect(x, y, w, h float32, color Color, angle float32) {
	drawRectGL(x, y, w, h, color, angle)
}
func (d *DesktopRenderer) DrawCircle(cx, cy, radius float32, color Color) {
	drawCircleGL(cx, cy, radius, color)
}
func (d *DesktopRenderer) DrawOval(x, y, w, h float32, color Color) {
	drawOvalGL(x, y, w, h, color)
}
func (d *DesktopRenderer) DrawLine(x1, y1, x2, y2 float32, color Color, strokeW float32) {
	drawLineGL(x1, y1, x2, y2, color, strokeW)
}
func (d *DesktopRenderer) DrawTriangles(verts []float32, color Color) {
	// Unified batch: barycentric triangles ride the SDF stream as mode 3,
	// so path/icon triangles interleave with shapes and text in one draw.
	if unifiedBatch() {
		SDFBatchTriangles(verts)
		return
	}
	flushSDFBatch() // strict draw-order: any batched SDF shapes go first
	if shapeBatch.dirty || textBatch.dirty {
		flushBatches() // strict draw-order: pending shapes/text go first
	}
	// color is carried per-vertex — no flush needed on color changes (O4)
	atomic.AddInt64(&statPrimitives, 1)
	triBatch.verts = appendWithClip(triBatch.verts, verts, 9)
	triBatch.dirty = true
}

// DrawPathFill fills a tessellated path using stencil-then-cover: triangles
// increment the stencil (no color writes), then the path's bounding quad is
// drawn where the stencil is non-zero. This gives exact, seam-free coverage —
// adjacent tessellation triangles cannot double-blend or leave cracks, which
// per-triangle alpha blending cannot guarantee.
// Call on the GL thread. Vertices are pre-transformed (x,y,bary ×5 floats).
var pathFillCap int32

func (d *DesktopRenderer) DrawPathFill(tris []float32, color Color, bounds Rect) {
	if !rectRenderer.ready || len(tris) == 0 {
		return
	}
	if os.Getenv("MEUI_FLUSH_DEBUG") != "" && (bounds.Width > 400 || bounds.Height > 400 || bounds.X < -200 || bounds.Y < -200) {
		fmt.Printf("[pathfill] HUGE bounds=(%.0f,%.0f %.0fx%.0f) tris=%d color=%08x\n",
			bounds.X, bounds.Y, bounds.Width, bounds.Height, len(tris)/9, color)
	}
	// Preserve draw order against previously batched geometry.
	flushBatches()

	cacheUseProgram(rectRenderer.triProgram)
	// Path verts are 9-float (pos2 bary3 color4); the tri pipeline is
	// 13-float (adds per-vertex clip). Expand with a wide-open clip —
	// without it, clip attribs 3/4 read garbage from the raw layout and
	// the clip discard eats the path fill.
	stride := int32(52)
	expanded := appendWithClip(nil, tris, 9)
	cacheBindVBO(rectRenderer.triVBO)
	cacheEnableAttrib(uint32(rectRenderer.triPosAttrLoc))
	glVertexAttribPointer(uint32(rectRenderer.triPosAttrLoc), 2, GL_FLOAT, 0, stride, unsafe.Pointer(nil))
	cacheEnableAttrib(uint32(rectRenderer.triBaryAttrLoc))
	glVertexAttribPointer(uint32(rectRenderer.triBaryAttrLoc), 3, GL_FLOAT, 0, stride, ptrOffset(8))
	cacheEnableAttrib(uint32(rectRenderer.triColorAttrLoc))
	glVertexAttribPointer(uint32(rectRenderer.triColorAttrLoc), 4, GL_FLOAT, 0, stride, ptrOffset(20))
	glVertexAttribPointer(uint32(rectRenderer.triClipMinLoc), 2, GL_FLOAT, 0, stride, ptrOffset(36))
	glVertexAttribPointer(uint32(rectRenderer.triClipMaxLoc), 2, GL_FLOAT, 0, stride, ptrOffset(44))
	glUniform2f(rectRenderer.triScreenSizeLoc, rectRenderer.screenWidth, rectRenderer.screenHeight)

	// Stencil counting pass — no color writes, no blending.
	glColorMask(0, 0, 0, 0)
	glEnable(GL_STENCIL_TEST)
	glStencilFunc(GL_ALWAYS, 1, 0xFFFFFFFF)
	glStencilOp(GL_KEEP, GL_KEEP, GL_INCR)
	uploadBatchData(rectRenderer.triVBO, &pathFillCap, expanded)
	glDrawArrays(GL_TRIANGLES, 0, int32(len(expanded)/13))

	// Cover pass — fill where the winding count is non-zero. The cover also
	// RESETS the stencil to zero as it draws (its quad covers the whole path
	// bbox), so the next path starts from a clean slate without a clear.
	glColorMask(1, 1, 1, 1)
	glStencilFunc(GL_NOTEQUAL, 0, 0xFFFFFFFF)
	glStencilOp(GL_ZERO, GL_ZERO, GL_ZERO)
	bx, by := bounds.X, bounds.Y
	bw, bh := bounds.Width, bounds.Height
	r := float32((color>>16)&0xFF) / 255
	g := float32((color>>8)&0xFF) / 255
	b := float32(color&0xFF) / 255
	a := float32((color>>24)&0xFF) / 255
	// 6 vertices x 13 floats (pos2, bary3, color4, clip4) — bary (2,2,2)
	// keeps the cover fragment solid (pixel-distance AA treats it as deep
	// interior). Clip is wide open: the cover must not self-discard.
	cover := []float32{
		bx, by, 2, 2, 2, r, g, b, a, -9999, -9999, 9999, 9999,
		bx + bw, by, 2, 2, 2, r, g, b, a, -9999, -9999, 9999, 9999,
		bx, by + bh, 2, 2, 2, r, g, b, a, -9999, -9999, 9999, 9999,
		bx, by + bh, 2, 2, 2, r, g, b, a, -9999, -9999, 9999, 9999,
		bx + bw, by, 2, 2, 2, r, g, b, a, -9999, -9999, 9999, 9999,
		bx + bw, by + bh, 2, 2, 2, r, g, b, a, -9999, -9999, 9999, 9999,
	}
	uploadBatchData(rectRenderer.triVBO, &pathFillCap, cover)
	glDrawArrays(GL_TRIANGLES, 0, 6)

	glDisable(GL_STENCIL_TEST)
	// Reset the stencil for this clip region (scissor is active and covers
	// at least the path bounds), so subsequent paths count from zero.
	glStencilMask(0xFFFFFFFF)
	glClear(GL_STENCIL_BUFFER_BIT)
}
func (d *DesktopRenderer) DrawTexturedQuads(verts []float32, texID uint32, color Color, isImage bool) {
	// Unified batch: glyphs and images ride the SDF vertex stream with a
	// per-vertex mode, so shape↔text alternation no longer flushes the
	// frame apart (O2/O4 follow-up; measured 236 draws/frame → 1-3).
	// LCD subpixel glyphs and SDF-disabled debugging keep the old
	// separate batch path (their per-channel blend differs).
	if sdfBatch.ready && os.Getenv("MEUI_SKIP_SDF") != "1" && os.Getenv("MEUI_TEXT_LCD") != "1" {
		if !isImage && os.Getenv("MEUI_SKIP_TEXT") == "1" {
			return
		}
		SDFBatchTexturedQuads(verts, texID, isImage)
		return
	}
	flushSDFBatch() // strict draw-order
	if shapeBatch.dirty || triBatch.dirty {
		flushBatches() // strict draw-order: pending shapes/triangles go first
	}
	newQuads := len(verts) / 32
	if textBatch.quads+newQuads > int(quadIBOMaxQuads) {
		flushBatches() // index buffer full — start a fresh batch
	}
	flushKey := !textBatch.dirty || textBatch.texID != texID || textBatch.isImage != isImage
	if flushKey {
		flushBatches()
	}
	atomic.AddInt64(&statPrimitives, 1)
	textBatch.texID = texID
	textBatch.verts = appendWithClip(textBatch.verts, verts, 8)
	textBatch.quads += newQuads
	textBatch.dirty = true
	textBatch.isImage = isImage
}
