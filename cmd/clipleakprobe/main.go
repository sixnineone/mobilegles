// Command clipleakprobe reproduces the clip/transform state leak recorded as a
// pre-existing defect in MIGRATION.md ("Known defects carried over", item 1):
//
//	After a ClipRect + RestoreToCount pair, subsequent DrawText calls are
//	silently dropped and a later Translate/Rotate lands at the origin.
//
// The demo scenes avoid it by ordering the clip strictly last, which keeps the
// demo correct without exposing the bug. This probe puts the clip FIRST and
// asserts three things on the real WINDOW framebuffer:
//
//	clip  a red full-canvas rect drawn under a ClipRect band appears only in
//	      that band (this cannot be tested offscreen: RenderOffscreen calls
//	      SetClipDiscardEnabled(false) by design)
//	B     text drawn after ClipRect+RestoreToCount still renders
//	C     a Translate+Rotate rect after it still lands at its own origin
//
// Exit status is non-zero when an assertion fails, so it can gate the fix:
//
//	go run ./cmd/clipleakprobe
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/sixnineone/mobilegles"
)

const (
	W, H = 480, 320

	// The clip band (top-down); the red rect is drawn over the WHOLE canvas, so
	// any red outside the band is a clip failure.
	bandX, bandY, bandW, bandH = 10, 60, 200, 40

	// Two IDENTICAL transformed rects, one either side of the clip pair. The
	// assertion is that they come out the same - not that rotation is ideal
	// (how this renderer approximates a rotated rect is a separate question).
	ctrlCX, ctrlCY = 120, 250
	testCX, testCY = 340, 220

	// The band the TEXT is drawn under (REVIEW §7), with a big font so the glyphs
	// clearly cross both of its edges.
	textBandY = 200
)

// White-text regions (top-down x0,y0,x1,y1), before and after the clip.
var (
	boxBefore = [4]int{10, 10, 200, 42}
	boxAfter  = [4]int{10, 120, 200, 152}
)

func main() {
	if err := mobilegles.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "mobilegles.Init: %v\n", err)
		os.Exit(2)
	}
	win := mobilegles.InitWindow(W, H)
	if win == nil {
		fmt.Fprintln(os.Stderr, "InitWindow returned nil")
		os.Exit(2)
	}
	defer win.Destroy()
	win.OwnsEventPump = true
	if err := mobilegles.InitRenderer(); err != nil {
		fmt.Fprintf(os.Stderr, "InitRenderer: %v\n", err)
		os.Exit(2)
	}
	mobilegles.SetScreenSize(float32(W), float32(H))

	face := mobilegles.SystemTTF()
	if f, err := mobilegles.LoadSystemFont("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"); err == nil {
		face = f
	}
	body := mobilegles.NewFont("body", 16, face)
	big := mobilegles.NewFont("big", 48, face)

	canvas := mobilegles.NewCanvas(W, H)
	var px []uint32

	for frame := 0; frame < 3; frame++ {
		win.PollEvents(func(e mobilegles.Event) {
			if e.Type == mobilegles.EventQuit {
				win.Close()
			}
		})
		win.RunFrameTasks()

		mobilegles.AcquireRenderer()
		if !win.MakeCurrent() {
			mobilegles.ReleaseRenderer()
			continue
		}
		mobilegles.ResetClipState()
		mobilegles.SetScreenSize(float32(W), float32(H))
		canvas.ClearWithViewport(0xFF0A0C18, 0, 0, W, H)

		p := mobilegles.NewPaint()
		p.SetStyle(int32(mobilegles.Fill))

		// A: text before any clip - the control.
		p.SetColor(mobilegles.RGBA(255, 255, 255, 255))
		canvas.DrawText("BEFORE", 14, 38, body, p)

		// C-control: the same translate/rotate/size as the test rect below, drawn
		// BEFORE the clip pair, so the two must be identical.
		saveC := canvas.Save()
		canvas.Translate(ctrlCX, ctrlCY)
		canvas.Rotate(20)
		p.SetColor(mobilegles.RGBA(90, 220, 120, 255))
		canvas.DrawRect(-40, -20, 80, 40, p)
		canvas.RestoreToCount(saveC)

		// The ClipRect + RestoreToCount pair the defect is about. The red rect
		// covers the whole canvas and must be clipped to the band.
		save := canvas.Save()
		canvas.ClipRect(mobilegles.Rect{X: bandX, Y: bandY, Width: bandW, Height: bandH})
		p.SetColor(mobilegles.RGBA(220, 40, 40, 255))
		canvas.DrawRect(0, 0, W, H, p)
		canvas.RestoreToCount(save)

		// B: text after the clip+restore.
		p.SetColor(mobilegles.RGBA(255, 255, 255, 255))
		canvas.DrawText("AFTER", 14, 148, body, p)

		// TEXT UNDER A CLIP (REVIEW §7): a large run straddling a second band, drawn
		// the same way the red rect above is. The red rect proves this harness clips
		// correctly, so any ink outside this band is text specifically escaping it.
		canvas.Save()
		canvas.ClipRect(mobilegles.Rect{X: 0, Y: textBandY, Width: W, Height: bandH})
		p.SetColor(mobilegles.RGBA(255, 255, 0, 255))
		canvas.DrawText("Hamburgefonstiv", 20, textBandY+bandH/2+16, big, p)
		canvas.RestoreToCount(0)

		// C: the same rect after the clip+restore.
		save2 := canvas.Save()
		canvas.Translate(testCX, testCY)
		canvas.Rotate(20)
		p.SetColor(mobilegles.RGBA(90, 220, 120, 255))
		canvas.DrawRect(-40, -20, 80, 40, p)
		canvas.RestoreToCount(save2)

		mobilegles.FlushBatches()
		px = mobilegles.ReadScreenPixels(W, H)
		win.Swap()
		mobilegles.ReleaseRenderer()
	}
	if len(px) == 0 {
		fmt.Fprintln(os.Stderr, "no pixels read back")
		os.Exit(2)
	}

	at := func(x, y int) uint32 { return px[(H-1-y)*W+x] }
	ch := func(v uint32, shift uint) int { return int((v >> shift) & 0xFF) }
	bright := func(v uint32) bool { return ch(v, 16) > 180 && ch(v, 8) > 180 && ch(v, 0) > 180 }
	red := func(v uint32) bool { return ch(v, 16) > 150 && ch(v, 8) < 90 && ch(v, 0) < 90 }
	green := func(v uint32) bool { return ch(v, 8) > 140 && ch(v, 16) < 150 && ch(v, 0) < 150 }

	count := func(box [4]int, pred func(uint32) bool) (n, minX, minY, maxX, maxY int) {
		minX, minY, maxX, maxY = W, H, -1, -1
		for y := box[1]; y < box[3]; y++ {
			for x := box[0]; x < box[2]; x++ {
				if pred(at(x, y)) {
					n++
					if x < minX {
						minX = x
					}
					if x > maxX {
						maxX = x
					}
					if y < minY {
						minY = y
					}
					if y > maxY {
						maxY = y
					}
				}
			}
		}
		return
	}

	// Dump the frame so a failure can be looked at rather than guessed about.
	if f, err := os.Create("/tmp/clipleak.png"); err == nil {
		img := image.NewRGBA(image.Rect(0, 0, W, H))
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				v := at(x, y)
				o := img.PixOffset(x, y)
				img.Pix[o+0], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = byte(v>>16), byte(v>>8), byte(v), 0xFF
			}
		}
		png.Encode(f, img)
		f.Close()
		fmt.Println("wrote /tmp/clipleak.png")
	}

	aN, _, _, _, _ := count(boxBefore, bright)
	bN, _, _, _, _ := count(boxAfter, bright)
	inN, _, _, _, _ := count([4]int{bandX, bandY, bandX + bandW, bandY + bandH}, red)
	allRed, _, _, _, _ := count([4]int{0, 0, W, H}, red)
	outN := allRed - inN
	ctrlN, ctrlMinX, ctrlMinY, ctrlMaxX, ctrlMaxY := count([4]int{0, 180, 240, H}, green)
	cN, cMinX, cMinY, cMaxX, cMaxY := count([4]int{240, 150, W, H}, green)

	yIn, yOut := 0, 0
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			v := at(x, y)
			if (v>>16)&0xFF < 120 || (v>>8)&0xFF < 120 || v&0xFF > 120 {
				continue // not the yellow
			}
			if y >= textBandY && y < textBandY+bandH {
				yIn++
			} else {
				yOut++
			}
		}
	}
	fmt.Printf("TEXT under clip  : %d px inside, %d px OUTSIDE (want >0 / 0)\n", yIn, yOut)
	fmt.Printf("clip band red px : %d inside, %d outside (want >0 / 0)\n", inN, outN)
	fmt.Printf("A text before    : %d bright px (want > 0)\n", aN)
	fmt.Printf("B text after clip: %d bright px (want > 0)\n", bN)
	fmt.Printf("control rect (pre-clip) : %d px, %dx%d bbox\n", ctrlN,
		ctrlMaxX-ctrlMinX+1, ctrlMaxY-ctrlMinY+1)
	if cN > 0 {
		fmt.Printf("C rect after clip pair  : %d px, %dx%d bbox\n", cN,
			cMaxX-cMinX+1, cMaxY-cMinY+1)
	} else {
		fmt.Println("C rect after clip pair  : 0 green px (want > 0)")
	}

	fail := false
	if inN == 0 || outN != 0 {
		fmt.Println("FAIL: the ClipRect band did not confine the draw")
		fail = true
	}
	if aN == 0 {
		fmt.Println("FAIL: the control text did not render - the probe is not exercising text")
		fail = true
	}
	if bN == 0 {
		fmt.Println("FAIL: text drawn after ClipRect+RestoreToCount did not render (defect 1: dropped text)")
		fail = true
	}
	if cN == 0 {
		fmt.Println("FAIL: the rect drawn after ClipRect+RestoreToCount did not render (defect 1: transform lost)")
		fail = true
	} else if abs((cMinX+cMaxX)/2-testCX) > 12 || abs((cMinY+cMaxY)/2-testCY) > 12 {
		fmt.Printf("FAIL: transformed rect is at %d,%d, not the requested %d,%d (defect 1: transform lost)\n",
			(cMinX+cMaxX)/2, (cMinY+cMaxY)/2, testCX, testCY)
		fail = true
	} else if abs(cN-ctrlN) > ctrlN/5 {
		fmt.Printf("FAIL: the rect after the clip pair has %d px, the identical pre-clip one %d (defect 1: transform corrupted)\n", cN, ctrlN)
		fail = true
	}
	if fail {
		os.Exit(1)
	}
	fmt.Println("PASS: clip/transform state does not leak into later draws")
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
