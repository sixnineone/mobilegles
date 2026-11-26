package mobilegles

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"
)

var defaultReadFBO int32

// DefaultReadFBO reports the framebuffer binding captured at package init, which
// the offscreen helpers rebind as "the default framebuffer". It is exposed so a
// probe can confirm what it actually holds: 0 is each context's own drawable
// (per-context-correct), while any other value would be a binding captured in
// some other context and would be wrong to rebind here.
func DefaultReadFBO() int32 { return defaultReadFBO }

func init() {
	if glGetIntegerv != nil {
		glGetIntegerv(0x8CA6, uintptr(unsafe.Pointer(&defaultReadFBO)))
	}
}

func createOffscreenFBO(w, h int32) (fbo uint32, tex uint32) {
	if glGenFramebuffers == nil || glGenTextures == nil {
		// No GL context loaded (e.g. unit tests) — signal failure so
		// callers fall back to error returns instead of panicking.
		return 0, 0
	}
	glGenFramebuffers(1, unsafe.Pointer(&fbo))
	glGenTextures(1, unsafe.Pointer(&tex))

	glBindTexture(GL_TEXTURE_2D, tex)
	glTexImage2D(GL_TEXTURE_2D, 0, int32(0x1908), w, h, 0, 0x1908, GL_UNSIGNED_BYTE, nil)
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, int32(GL_CLAMP_TO_EDGE))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, int32(GL_CLAMP_TO_EDGE))

	glBindFramebuffer(GL_FRAMEBUFFER, fbo)
	glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, tex, 0)

	status := glCheckFramebufferStatus(GL_FRAMEBUFFER)
	if status != GL_FRAMEBUFFER_COMPLETE {
		glDeleteFramebuffers(1, unsafe.Pointer(&fbo))
		glDeleteTextures(1, unsafe.Pointer(&tex))
		return 0, 0
	}

	glBindTexture(GL_TEXTURE_2D, 0)
	glBindFramebuffer(GL_FRAMEBUFFER, uint32(defaultReadFBO))
	return fbo, tex
}

func deleteOffscreenFBO(fbo, tex uint32) {
	if fbo == 0 {
		return
	}
	glDeleteFramebuffers(1, unsafe.Pointer(&fbo))
	if tex != 0 {
		glDeleteTextures(1, unsafe.Pointer(&tex))
	}
}

func CreateOffscreenFBO(w, h int32) (uint32, uint32) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return createOffscreenFBO(w, h)
}

func DeleteOffscreenFBO(fbo, tex uint32) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deleteOffscreenFBO(fbo, tex)
}

func ErrOffscreenFailed() error {
	return fmt.Errorf("offscreen rendering failed")
}

func ReadOffscreenPixels(fbo uint32, w, h int32) []uint32 {
	if fbo == 0 {
		return nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return readOffscreenPixels(fbo, w, h)
}

func readOffscreenPixels(fbo uint32, w, h int32) []uint32 {
	glBindFramebuffer(GL_FRAMEBUFFER, fbo)
	pixels := make([]uint32, w*h)
	glReadPixels(0, 0, w, h, 0x1908, GL_UNSIGNED_BYTE, unsafe.Pointer(&pixels[0]))
	glBindFramebuffer(GL_FRAMEBUFFER, uint32(defaultReadFBO))

	for i, px := range pixels {
		r := (px >> 0) & 0xFF
		g := (px >> 8) & 0xFF
		b := (px >> 16) & 0xFF
		a := (px >> 24) & 0xFF
		// Content stays PREMULTIPLIED: the FBO composites onto transparent
		// black, so color channels already carry alpha. Textures sampled by
		// the text/image batch are composited with (ONE, ONE_MINUS_SRC_ALPHA),
		// which expects exactly this. (Un-premultiplying here lost precision
		// and made cached text render dim.)
		pixels[i] = a<<24 | r<<16 | g<<8 | b
	}
	return pixels
}

// ReadScreenPixels reads the default framebuffer (back buffer) as
// 0xAARRGGBB pixels, bottom-up as GL delivers them. Call while the GL
// context is current and after FlushBatches() (e.g. inside the draw
// callback, before the swap).
func ReadScreenPixels(w, h int32) []uint32 {
	if w <= 0 || h <= 0 {
		return nil
	}
	pixels := make([]uint32, w*h)
	glReadPixels(0, 0, w, h, 0x1908, GL_UNSIGNED_BYTE, unsafe.Pointer(&pixels[0]))
	for i, px := range pixels {
		r := (px >> 0) & 0xFF
		g := (px >> 8) & 0xFF
		b := (px >> 16) & 0xFF
		a := (px >> 24) & 0xFF
		pixels[i] = a<<24 | r<<16 | g<<8 | b
	}
	return pixels
}

func RenderOffscreen(w, h int32, renderFn func()) []uint32 {
	runtime.LockOSThread()

	// Drain any batches pending for the MAIN framebuffer first. If we
	// bind the cache FBO with pending main-frame geometry, the flush
	// after renderFn rasterizes that geometry into the cached image —
	// foreign UI fragments appear as ghost text/shapes inside every
	// cached widget (TextArea ghosts, Calendar "garbled" numbers).
	flushBatches()

	// No GL context (unit tests, headless): signal failure the same way
	// createOffscreenFBO would, before touching any GL entry point.
	if glGenFramebuffers == nil || glGetIntegerv == nil {
		runtime.UnlockOSThread()
		return nil
	}

	// Capture the CURRENT framebuffer BEFORE creating the offscreen one:
	// createOffscreenFBO rebinds to the default framebuffer, so a NESTED
	// offscreen pass (e.g. lazily-rasterized SVG icons drawn inside a
	// cached widget's render callback) would otherwise save the default
	// FBO as "previous" and restore it — orphaning the parent cache FBO
	// and leaking every later draw to the main framebuffer (partial
	// cached widgets: calendar header only, ghost content).
	var prevFBO int32
	glGetIntegerv(0x8CA6, uintptr(unsafe.Pointer(&prevFBO)))

	fbo, tex := createOffscreenFBO(w, h)
	if fbo == 0 {
		runtime.UnlockOSThread()
		return nil
	}

	prevScreenW := screenWidth
	prevScreenH := screenHeight
	var prevViewport [4]int32
	glGetIntegerv(0x0BA2, uintptr(unsafe.Pointer(&prevViewport[0])))

	// Restore the CLIP MIRROR, not a GL query. Inside a nested offscreen
	// pass the GL scissor box holds the OUTER pass's coordinates (and a
	// flush can enable the test), so a GL-queried restore "succeeds" with
	// a rect from the wrong coordinate space — later sends then stamp
	// that stale rect and discard/leak the wrong fragments (DataTable
	// cell text vanishing, ghost rows).
	prevState := scissorState
	prevClipDiscard := clipDiscardEnabled
	// The per-vertex clip PLANE table is process-global and is NOT part of the
	// per-frame ResetClipState (which only clears the scissor mirror). The
	// offscreen pass draws content that sets clip planes, so without saving and
	// restoring the table those planes survive into the next window's frame:
	// its batched primitives stamp a range that points at the chart's planes and
	// get clipped away, so that window renders nothing while the window that did
	// the offscreen render looks fine. clipPlaneState[:0:0] copies without
	// naming the element type.
	prevPlanes := append(clipPlaneState[:0:0], clipPlaneState...)
	prevGen := clipGen

	SetScreenSize(float32(w), float32(h))
	setViewport(0, 0, w, h)
	glBindFramebuffer(GL_FRAMEBUFFER, fbo)

	// The offscreen render must NOT be clipped by the main canvas's scissor:
	// NewCanvas (called inside renderFn) re-programs the scissor to the
	// offscreen size, and if left behind it clips all subsequent main-canvas
	// draws to that size. Disable scissor for the offscreen pass.
	glDisable(0x0C11 /*GL_SCISSOR_TEST*/)
	scissorState.active = false  // per-vertex clip: full target during offscreen
	SetClipDiscardEnabled(false) // offscreen target is unclipped

	glClearColor(0, 0, 0, 0)
	glClear(GL_COLOR_BUFFER_BIT)

	// The thread stays locked across renderFn: GL contexts are per-thread, so
	// unlocking here lets the goroutine migrate and every GL call after the
	// re-lock (the flush, the pixel read, the FBO unbind, and the screen-size,
	// viewport and scissor restores) can then run on a thread where this
	// window's context is not current — leaving the FBO bound and the globals
	// at the offscreen values for every window painted after it.
	if renderFn != nil {
		renderFn()
	}
	if os.Getenv("MEUI_LAYOUT_DEBUG") != "" {
		var test int32
		var box [4]int32
		glGetIntegerv(0x0C11, uintptr(unsafe.Pointer(&test)))
		glGetIntegerv(0x0C10, uintptr(unsafe.Pointer(&box[0])))
		fmt.Printf("[offscreen] pre-read flush: scissorTest=%d box=(%d,%d %dx%d) fbo=%d\n",
			test, box[0], box[1], box[2], box[3], fbo)
	}
	flushBatches()

	pixels := readOffscreenPixels(fbo, w, h)

	deleteOffscreenFBO(fbo, tex)

	glBindFramebuffer(GL_FRAMEBUFFER, uint32(prevFBO))
	SetClipDiscardEnabled(prevClipDiscard)
	SetScreenSize(prevScreenW, prevScreenH)
	setViewport(prevViewport[0], prevViewport[1], prevViewport[2], prevViewport[3])
	scissorState = prevState
	clipPlaneState = prevPlanes
	clipGen = prevGen
	// Guarded like EVERY other call site of glScissorGlobal (backend_api.go,
	// canvas.go, renderer.go). It is assigned during renderer INIT, not by the GL
	// loader, so an offscreen pass running before the renderer is ready reached this
	// line and segfaulted — and the entry check cannot catch it, because that tests
	// glGenFramebuffers and glGetIntegerv, which ARE loaded by then. Found by
	// TestOffscreenPassSeparatesItsState, the first caller to run a pass outside a
	// frame.
	if glScissorGlobal != nil {
		glScissorGlobal(prevState.x, prevState.y, prevState.w, prevState.h)
	}
	if prevState.active && !prevState.empty {
		glEnable(0x0C11 /*GL_SCISSOR_TEST*/)
	} else {
		glDisable(0x0C11 /*GL_SCISSOR_TEST*/)
	}

	DumpRenderState("offscreen:restored")

	runtime.UnlockOSThread()
	return pixels
}
