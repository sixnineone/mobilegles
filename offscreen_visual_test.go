//go:build visual

package mobilegles

import (
	"testing"
	"unsafe"
)

// rendererStateSnapshot captures every piece of process-global renderer state an
// offscreen pass touches. It is a struct rather than a list of comparisons so a
// new field cannot be added to one half and forgotten in the other.
type rendererStateSnapshot struct {
	screenW, screenH float32
	scissor          [4]int32
	scissorActive    bool
	scissorEmpty     bool
	clipPlanes       []ClipPlane
	clipGen          int
	clipDiscard      bool
	fbo              int32
	viewport         [4]int32
}

func captureRendererState() rendererStateSnapshot {
	s := rendererStateSnapshot{
		screenW:       screenWidth,
		screenH:       screenHeight,
		scissorActive: scissorState.active,
		scissorEmpty:  scissorState.empty,
		clipGen:       clipGen,
		clipDiscard:   clipDiscardEnabled,
		clipPlanes:    append(clipPlaneState[:0:0], clipPlaneState...),
	}
	s.scissor = [4]int32{scissorState.x, scissorState.y, scissorState.w, scissorState.h}
	glGetIntegerv(0x8CA6 /*GL_FRAMEBUFFER_BINDING*/, uintptr(unsafe.Pointer(&s.fbo)))
	glGetIntegerv(0x0BA2 /*GL_VIEWPORT*/, uintptr(unsafe.Pointer(&s.viewport[0])))
	return s
}

func (s rendererStateSnapshot) diff(o rendererStateSnapshot) []string {
	var out []string
	if s.screenW != o.screenW || s.screenH != o.screenH {
		out = append(out, "screen size")
	}
	if s.scissor != o.scissor {
		out = append(out, "scissor rect")
	}
	if s.scissorActive != o.scissorActive {
		out = append(out, "scissor active")
	}
	if s.scissorEmpty != o.scissorEmpty {
		out = append(out, "scissor empty")
	}
	if s.clipGen != o.clipGen {
		out = append(out, "clip generation")
	}
	if s.clipDiscard != o.clipDiscard {
		out = append(out, "clip discard")
	}
	if len(s.clipPlanes) != len(o.clipPlanes) {
		out = append(out, "clip plane count")
	} else {
		for i := range s.clipPlanes {
			if s.clipPlanes[i] != o.clipPlanes[i] {
				out = append(out, "clip plane table")
				break
			}
		}
	}
	if s.fbo != o.fbo {
		out = append(out, "bound framebuffer")
	}
	if s.viewport != o.viewport {
		out = append(out, "viewport")
	}
	return out
}

// TestOffscreenPassSeparatesItsState is the regression test for the fault class
// that made MEUI_NO_OFFSCREEN necessary: the offscreen pass mutates renderer state
// that every window shares.
//
// Each leak was found by hand, one at a time, and each presented as something else
// entirely — ghost text and garbled calendar numbers (pending main-frame geometry
// flushed into the cache FBO), DataTable cells vanishing (a scissor rect restored
// from the wrong coordinate space), a window rendering NOTHING because the previous
// window's offscreen pass left its clip planes behind. This asserts the whole set
// at once, so the next omission fails here instead of in an app.
//
// It also pins the ORIENTATION of a nested pass (MOBILE-PLAN.md P5.7), which is
// the same fault seen from the other end: an offscreen pass inside an offscreen
// pass is the nesting shape without needing a window manager, and if the inner
// result's orientation depends on being nested the two disagree here.
//
// Both checks share ONE window on purpose. A second SDL window in one process is a
// known GL-re-init hazard (AGENTS.md), so this is one test, not two.
func TestOffscreenPassSeparatesItsState(t *testing.T) {
	win := InitWindow(320, 240)
	if win == nil {
		t.Skip("no SDL3 window; cannot exercise a real offscreen pass")
	}
	defer win.Destroy()

	// Put the shared state into a deliberately NON-DEFAULT configuration, so a
	// pass that merely "restores to default" is caught as well as one that forgets
	// entirely.
	SetScreenSize(320, 240)
	SetClipDiscardEnabled(true)
	scissorState.active = true
	scissorState.empty = false
	scissorState.x, scissorState.y, scissorState.w, scissorState.h = 3, 5, 111, 77
	clipGen = 7
	clipPlaneState = append(clipPlaneState[:0:0], ClipPlane{NX: 0.6, NY: 0.8, D: -12})

	before := captureRendererState()

	// An asymmetric pattern — TOP half red, BOTTOM half blue — so a vertical flip
	// shows up as the colours swapping. Both passes are asked to draw the same
	// thing, so their top rows must agree.
	drawPattern := func(w, h int32) {
		c := NewCanvas(float32(w), float32(h))
		red := NewPaint()
		red.SetColor(RGBA(255, 0, 0, 255))
		c.DrawRect(0, 0, float32(w), float32(h)/2, red)
		blue := NewPaint()
		blue.SetColor(RGBA(0, 0, 255, 255))
		c.DrawRect(0, float32(h)/2, float32(w), float32(h)/2, blue)
	}

	const w, h = 32, 16
	standalone := RenderOffscreen(w, h, func() { drawPattern(w, h) })
	if standalone == nil {
		t.Fatal("standalone offscreen pass produced no pixels, so nothing was exercised")
	}

	var nested []uint32
	outer := RenderOffscreen(w, h, func() {
		drawPattern(w, h)
		nested = RenderOffscreen(w, h, func() { drawPattern(w, h) })
	})
	if outer == nil || nested == nil {
		t.Fatal("nested offscreen pass produced no pixels")
	}

	after := captureRendererState()
	if d := before.diff(after); len(d) != 0 {
		t.Errorf("the offscreen pass left shared renderer state changed: %v\n"+
			"every window draws with this state, which is why MEUI_NO_OFFSCREEN existed", d)
	}

	// Top-centre pixel of each: row 0 is the top of the returned image.
	topCentre := func(pix []uint32) uint32 { return pix[w/2] }
	if a, b := topCentre(standalone), topCentre(nested); a != b {
		t.Errorf("a nested offscreen pass is oriented differently from a standalone one: "+
			"top-centre %#08x standalone against %#08x nested (P5.7)", a, b)
	}
}
