package mobilegles

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// GLX drawable size: the one resize-related quantity this investigation never
// measured.
//
// Every value the draw path reads has been verified correct in a confirmed
// failing run (layout, paint rect, scissor, per-vertex clip, stencil, clip
// planes, viewport, projection, submitted geometry, upload), while the pixels
// are wrong. There is exactly one thing those measurements share: they all
// describe what the app *thinks* the surface is. SDL's cached window size says
// 520x620 and glReadPixels is asked for 520x620 rows - but neither says how big
// the GLX drawable that glViewport/glClear/glDrawElements actually rasterise
// into is. If the surface is still 520x380 after a vertical resize, GL clips
// the draw to the old surface with no error, glReadPixels of the missing rows
// is undefined, and every value in the frame is correct while the output is a
// body cut at the old height - the exact symptom.
//
// This is the same "value from one source, surface from another" defect class as
// the four process-global flips fixed earlier, which is why it is worth a
// dedicated instrument rather than another look at the sizes SDL reports.
//
// The query is server-side and independent of SDL: glXGetCurrentDisplay and
// glXGetCurrentDrawable come from libGL, and glXQueryDrawable asks the X server
// for the drawable's own width/height. No GL error, no SDL cache, no pixel read.

const (
	glxWidth  = 0x801D // GLX_WIDTH
	glxHeight = 0x801E // GLX_HEIGHT
)

var (
	glxOnce sync.Once
	glxOK   bool

	glXGetCurrentDisplay  func() uintptr
	glXGetCurrentDrawable func() uintptr
	glXQueryDrawable      func(dpy, draw uintptr, attr int32, value unsafe.Pointer) int32
)

// loadGLX binds the three GLX symbols lazily. libGL is already open (SDL uses
// it for the context); its GLX entry points are what a GLX context was created
// through. A symbol that is missing leaves glxOK false and the probe reports
// "unavailable" rather than a zero size, so an absent instrument cannot be read
// as a measurement.
func loadGLX() bool {
	glxOnce.Do(func() {
		if libGL == 0 {
			return
		}
		dpySym, errDpy := purego.Dlsym(libGL, "glXGetCurrentDisplay")
		drwSym, errDrw := purego.Dlsym(libGL, "glXGetCurrentDrawable")
		qrySym, errQry := purego.Dlsym(libGL, "glXQueryDrawable")
		if errDpy != nil || errDrw != nil || errQry != nil {
			return
		}
		if dpySym == 0 || drwSym == 0 || qrySym == 0 {
			return
		}
		purego.RegisterFunc(&glXGetCurrentDisplay, dpySym)
		purego.RegisterFunc(&glXGetCurrentDrawable, drwSym)
		purego.RegisterFunc(&glXQueryDrawable, qrySym)
		glxOK = true
	})
	return glxOK
}

// ProbeFullClear paints the ENTIRE claimed surface (viewport w x h, scissor and
// stencil off) a distinctive colour, then reads one pixel near the top row and
// one near the bottom. If a 520x620 window's back buffer is really 520x380, the
// clear fills only the bottom 380 rows and the read at y=610 is outside it. This
// separates "the surface is smaller than the window" from "the surface is fine
// and the fragments are being rejected", which no existing instrument could tell
// apart because every one of them reads through the same glReadPixels.
func ProbeFullClear(w, h int32) (top, bottom uint32, ok bool) {
	if glClearColor == nil || glClear == nil || glReadPixels == nil {
		return 0, 0, false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	glDisable(GL_SCISSOR_TEST)
	glDisable(0x0B90) // GL_STENCIL_TEST
	setViewport(0, 0, w, h)
	glClearColor(1, 0, 1, 1) // magenta
	glClear(GL_COLOR_BUFFER_BIT)
	var t, b [4]uint8
	glReadPixels(0, h-10, 1, 1, 0x1908, GL_UNSIGNED_BYTE, unsafe.Pointer(&t[0]))
	glReadPixels(0, 10, 1, 1, 0x1908, GL_UNSIGNED_BYTE, unsafe.Pointer(&b[0]))
	top = uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2])
	bottom = uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
	return top, bottom, true
}

// GLXDrawableSize reports the size of the GLX drawable current on this thread,
// as the X server sees it. ok is false when the probe is unavailable (no GLX
// symbol, no current context, or the server refuses the query) - never silently
// (0,0,true).
func GLXDrawableSize() (int32, int32, bool) {
	if !loadGLX() {
		return 0, 0, false
	}
	dpy := glXGetCurrentDisplay()
	draw := glXGetCurrentDrawable()
	if dpy == 0 || draw == 0 {
		return 0, 0, false
	}
	var w, h uint32
	if glXQueryDrawable(dpy, draw, glxWidth, unsafe.Pointer(&w)) == 0 {
		return 0, 0, false
	}
	if glXQueryDrawable(dpy, draw, glxHeight, unsafe.Pointer(&h)) == 0 {
		return 0, 0, false
	}
	return int32(w), int32(h), true
}
