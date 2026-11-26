// Command demo is a self-contained visual verification app for the
// mobilegles renderer module. It opens a single SDL3 + OpenGL window,
// drives the frame loop directly through the public mobilegles API (no
// toolkit, no shim), and paints a labelled grid exercising the
// renderer's primitive surface: fills, strokes, rounded rects, circles,
// ovals, arcs, lines, quadratic paths, transforms, clipping, gradients,
// blend modes and text at several sizes.
//
// Usage:
//
//	go run ./cmd/demo                      # interactive; Esc or the window
//	                                       # close button exits
//	go run ./cmd/demo -frames 60 -o out.png   # render 60 frames, capture, exit
//
// The -o capture is written from the default framebuffer after the
// renderer has flushed, which makes the demo usable as a smoke test on
// a machine with no interactive session.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"

	"github.com/sixnineone/mobilegles"
)

const background uint32 = 0xFF1A1A2E

func main() {
	width := flag.Int("w", 1000, "window width in pixels")
	height := flag.Int("h", 640, "window height in pixels")
	frames := flag.Int("frames", 0, "render this many frames then exit (0 = until the window is closed)")
	out := flag.String("o", "", "capture the rendered framebuffer to this PNG path")
	scene := flag.String("scene", "grid", "scene to draw: grid | text")
	fontPath := flag.String("font", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", "TTF font used for the labels")
	flag.Parse()

	if err := mobilegles.Init(); err != nil {
		log.Fatalf("mobilegles.Init: %v", err)
	}

	win := mobilegles.InitWindow(int32(*width), int32(*height))
	if win == nil {
		log.Fatal("mobilegles.InitWindow returned nil")
	}
	// The primary window drives the global SDL event pump; without this
	// no platform events reach the queue.
	win.OwnsEventPump = true
	mobilegles.SetScreenSize(float32(win.Width()), float32(win.Height()))

	if err := mobilegles.InitRenderer(); err != nil {
		log.Fatalf("mobilegles.InitRenderer: %v", err)
	}
	mobilegles.SetSwapInterval0()

	face := mobilegles.SystemTTF()
	if f, err := mobilegles.LoadSystemFont(*fontPath); err != nil {
		log.Printf("font %s unavailable (%v) — falling back to the embedded atlas", *fontPath, err)
	} else {
		face = f
	}
	titleFont := mobilegles.NewFont("demo-title", 22, face)
	bodyFont := mobilegles.NewFont("demo-body", 14, face)
	smallFont := mobilegles.NewFont("demo-small", 11, face)

	canvas := mobilegles.NewCanvas(float32(win.Width()), float32(win.Height()))

	log.Printf("mobilegles demo: %dx%d", win.Width(), win.Height())

	frame := 0
	captured := false
	for !win.ShouldClose() {
		win.PollEvents(func(e mobilegles.Event) {
			switch e.Type {
			case mobilegles.EventQuit:
				win.Close()
			case mobilegles.EventKeyDown:
				if e.Key == mobilegles.KeyEscape {
					win.Close()
				}
			}
		})
		win.RunFrameTasks()

		// One serialized frame: bind, reset global clip state, project
		// against this window, clear, draw, flush, present.
		mobilegles.AcquireRenderer()
		if !win.MakeCurrent() {
			mobilegles.ReleaseRenderer()
			continue
		}
		mobilegles.ResetClipState()
		mobilegles.SetScreenSize(float32(win.Width()), float32(win.Height()))

		canvas.ClearWithViewport(background, 0, 0, win.Width(), win.Height())
		if *scene == "text" {
			drawTextProbe(canvas, smallFont, bodyFont, titleFont)
		} else {
			drawScene(canvas, win.Width(), win.Height(), titleFont, bodyFont, smallFont, frame)
		}
		mobilegles.FlushBatches()

		// Capture after a couple of warm-up frames, before the swap.
		if *out != "" && !captured && frame >= 2 {
			savePNG(*out, mobilegles.ReadScreenPixels(win.Width(), win.Height()), win.Width(), win.Height())
			captured = true
			log.Printf("captured %s (%dx%d)", *out, win.Width(), win.Height())
		}

		win.Swap()
		mobilegles.ReleaseRenderer()

		frame++
		if *frames > 0 && frame >= *frames {
			win.Close()
		}
		if *out != "" && captured {
			win.Close()
		}
	}

	win.Destroy()
	mobilegles.Shutdown()
	log.Printf("mobilegles demo: rendered %d frames", frame)
}

// drawScene paints the verification grid. Every call below is part of
// the public mobilegles surface a toolkit or adapter is expected to use.
func drawScene(c *mobilegles.Canvas, w, h int32, title, body, small *mobilegles.Font, frame int) {
	p := mobilegles.NewPaint()
	p.SetStyle(int32(mobilegles.Fill))
	p.SetColor(mobilegles.RGBA(235, 235, 245, 255))
	c.DrawText("mobilegles renderer verification", 24, 34, title, p)

	p.SetColor(mobilegles.RGBA(150, 160, 190, 255))
	c.DrawText(fmt.Sprintf("frame %d  ·  %dx%d  ·  Esc or close to exit", frame, w, h), 24, 54, small, p)

	label := func(text string, x, y float32) {
		p.SetColor(mobilegles.RGBA(120, 200, 255, 255))
		c.DrawText(text, x, y, small, p)
	}

	// --- Fills and strokes -------------------------------------------------
	label("fill / stroke / rounded", 24, 92)
	p.SetStyle(int32(mobilegles.Fill))
	p.SetColor(mobilegles.RGBA(90, 130, 220, 255))
	c.DrawRect(24, 100, 90, 56, p)

	p.SetStyle(int32(mobilegles.Stroke))
	p.SetStrokeWidth(3)
	p.SetColor(mobilegles.RGBA(120, 220, 200, 255))
	c.DrawRect(128, 100, 90, 56, p)

	p.SetStyle(int32(mobilegles.Fill))
	p.SetColor(mobilegles.RGBA(230, 170, 90, 255))
	c.DrawRoundedRect(mobilegles.Rect{X: 232, Y: 100, Width: 90, Height: 56}, 12, 12, p)

	// --- Circles, ovals, arcs ---------------------------------------------
	label("circle / oval / arc", 24, 190)
	p.SetColor(mobilegles.RGBA(220, 110, 140, 255))
	c.DrawCircle(60, 230, 32, p)

	p.SetColor(mobilegles.RGBA(140, 210, 120, 255))
	c.DrawOval(110, 200, 84, 56, p)

	p.SetStyle(int32(mobilegles.Stroke))
	p.SetStrokeWidth(4)
	p.SetColor(mobilegles.RGBA(240, 210, 90, 255))
	c.DrawArc(240, 190, 76, 76, 0, 270, p)

	// --- Lines, dashes, paths ---------------------------------------------
	label("lines / path (quad)", 24, 300)
	p.SetStyle(int32(mobilegles.Stroke))
	p.SetStrokeWidth(2)
	for i := 0; i < 6; i++ {
		p.SetColor(mobilegles.RGBA(uint8(120+i*20), 160, 255, 255))
		y := float32(316 + i*10)
		c.DrawLine(24, y, 180, y, p)
	}

	p.SetStyle(int32(mobilegles.Fill))
	p.SetColor(mobilegles.RGBA(255, 120, 170, 220))
	wave := mobilegles.NewPath()
	wave.MoveTo(mobilegles.Point{X: 210, Y: 330})
	for i := 0; i < 6; i++ {
		x := float32(210 + i*26)
		wave.QuadTo(mobilegles.Point{X: x + 13, Y: 300}, mobilegles.Point{X: x + 26, Y: 330})
	}
	wave.LineTo(mobilegles.Point{X: 366, Y: 352})
	wave.LineTo(mobilegles.Point{X: 210, Y: 352})
	wave.Close()
	c.DrawPath(wave, p)

	// --- Transform ---------------------------------------------------------
	// NOTE (renderer defect, pre-existing in the fork): the ClipRect section
	// at the very end of this function corrupts clip and transform state for
	// later draws — text drawn after it is silently dropped, and a
	// Translate/Rotate after it lands at the origin. Reproduced here with
	// -scene text (all 14 rows render) plus this grid (every DrawText issued
	// after the clip section vanished in an earlier ordering). Left for the
	// seam/device work rather than worked around; the ordering below keeps
	// the demo correct without hiding it.
	label("transform / clip", 400, 92)
	p.SetStyle(int32(mobilegles.Fill))
	p.SetColor(mobilegles.RGBA(150, 200, 255, 220))
	tSave := c.Save()
	c.Translate(470, 150)
	c.Rotate(24)
	c.DrawRect(-45, -22, 90, 44, p)
	c.RestoreToCount(tSave)

	// --- Gradients ---------------------------------------------------------
	label("gradients", 400, 300)
	p.SetStyle(int32(mobilegles.Fill))
	// Three stops, so the demo exercises the multi-stop ramp rather than the
	// old two-colour special case.
	p.SetLinearGradient(mobilegles.Point{X: 400, Y: 310}, mobilegles.Point{X: 590, Y: 360},
		[]mobilegles.GradientStop{
			{Offset: 0, Color: mobilegles.RGBA(80, 160, 255, 255)},
			{Offset: 0.5, Color: mobilegles.RGBA(120, 255, 200, 255)},
			{Offset: 1, Color: mobilegles.RGBA(220, 90, 200, 255)},
		})
	c.DrawRect(400, 310, 190, 52, p)

	// A conic ramp, and a gradient on a stroke rather than a fill.
	p.SetConicGradient(mobilegles.Point{X: 730, Y: 336}, 0, []mobilegles.GradientStop{
		{Offset: 0, Color: mobilegles.RGBA(255, 90, 90, 255)},
		{Offset: 0.5, Color: mobilegles.RGBA(255, 220, 90, 255)},
		{Offset: 1, Color: mobilegles.RGBA(255, 90, 90, 255)},
	})
	c.DrawCircle(730, 336, 26, p)

	p.SetStyle(int32(mobilegles.Stroke))
	p.SetStrokeWidth(6)
	p.SetRadialGradient(mobilegles.Point{X: 860, Y: 336}, 30, []mobilegles.GradientStop{
		{Offset: 0, Color: mobilegles.RGBA(255, 255, 255, 255)},
		{Offset: 1, Color: mobilegles.RGBA(60, 90, 255, 255)},
	})
	c.DrawCircle(860, 336, 26, p)
	p.SetStyle(int32(mobilegles.Fill))

	// --- Text at several sizes --------------------------------------------
	label("text sizes", 640, 92)
	p.SetColor(mobilegles.RGBA(235, 235, 245, 255))
	sizes := []*mobilegles.Font{small, body, title}
	for i, f := range sizes {
		p.SetColor(mobilegles.RGBA(uint8(200+i*20), 220, 255, 255))
		c.DrawText(fmt.Sprintf("size sample %d — {}", i), 640, float32(120+i*40), f, p)
	}

	// --- Blend modes -------------------------------------------------------
	label("blend modes", 640, 300)
	p.SetStyle(int32(mobilegles.Fill))
	p.SetColor(mobilegles.RGBA(40, 40, 70, 255))
	c.DrawRect(640, 310, 200, 60, p)
	modes := []mobilegles.BlendMode{
		mobilegles.BlendModeNormal, mobilegles.BlendModeMultiply,
		mobilegles.BlendModeScreen, mobilegles.BlendModeOverlay,
	}
	for i, m := range modes {
		p.SetColor(mobilegles.RGBA(210, 120, 255, 190))
		p.SetBlendMode(m)
		c.DrawRect(float32(652+i*46), 322, 38, 36, p)
	}
	p.SetBlendMode(mobilegles.BlendModeNormal)

	// --- Frame marker ------------------------------------------------------
	p.SetColor(mobilegles.RGBA(120, 200, 255, 160))
	c.DrawRect(24, float32(h-34), float32((frame*7)%int(w-48)), 4, p)

	// --- Clipping (strictly last: see the note above) ----------------------
	save := c.Save()
	c.ClipRect(mobilegles.Rect{X: 400, Y: 190, Width: 190, Height: 90})
	for i := 0; i < 9; i++ {
		p.SetColor(mobilegles.RGBA(255, uint8(110+i*14), 150, 255))
		c.DrawRect(400, float32(190+i*11), 190, 8, p)
	}
	c.RestoreToCount(save)
}

// drawTextProbe paints a numbered column of DrawText calls in a single
// frame, with no clipping or transforms, cycling the three font sizes.
// It makes text batching directly observable: run with
// `-scene text -o text.png` and count the numbered rows. All 14 render,
// which is what isolates the grid scene's dropped text to the
// clip/transform state defect noted in drawScene.
func drawTextProbe(c *mobilegles.Canvas, small, body, title *mobilegles.Font) {
	p := mobilegles.NewPaint()
	p.SetStyle(int32(mobilegles.Fill))
	fonts := []*mobilegles.Font{small, body, title}
	for i := 0; i < 14; i++ {
		p.SetColor(mobilegles.RGBA(uint8(120+i*8), 200, 255, 255))
		c.DrawText(fmt.Sprintf("row %02d", i), 40, float32(40+i*34), fonts[i%len(fonts)], p)
	}
}

// savePixels converts the bottom-up 0xAARRGGBB framebuffer to a PNG.
func savePNG(path string, pix []uint32, w, h int32) {
	img := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
	for y := 0; y < int(h); y++ {
		row := pix[(int(h)-1-y)*int(w) : (int(h)-y)*int(w)]
		for x, px := range row {
			o := img.PixOffset(x, y)
			img.Pix[o+0] = byte(px >> 16)
			img.Pix[o+1] = byte(px >> 8)
			img.Pix[o+2] = byte(px)
			img.Pix[o+3] = 0xFF
		}
	}
	f, err := os.Create(path)
	if err != nil {
		log.Printf("capture: %v", err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Printf("capture encode: %v", err)
	}
}
