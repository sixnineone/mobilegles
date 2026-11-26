// roundrectprobe — does a rotated ROUNDED rect draw its true rotated shape?
//
// REVIEW §6 fixed a rotated plain rect; a rounded one still took the axis-aligned
// path because the SDF batch carries the radius and had no angle. The batch now
// rotates its quad about the centre while aLocal stays unrotated, so the shader's
// local-space rounded-box distance yields the exact rotated shape.
//
// Verification is by ink: the bounding box must be the analytic AABB of the rotated
// rect, and the AREA must be the rounded rect's own area — a bounding box or a plain
// rect would both be visibly larger.
package main

import (
	"fmt"
	"math"
	"os"

	"github.com/sixnineone/mobilegles"
)

const (
	W, H = 320, 240
	rw   = 80.0
	rh   = 40.0
	rr   = 12.0
	deg  = 20.0
)

func main() {
	if err := mobilegles.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Init: %v\n", err)
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
		canvas.ClearWithViewport(0xFF101018, 0, 0, W, H)

		p := mobilegles.NewPaint()
		p.SetStyle(int32(mobilegles.Fill))
		p.SetColor(mobilegles.RGBA(255, 255, 255, 255))
		canvas.Save()
		canvas.Translate(W/2, H/2)
		canvas.Rotate(deg)
		canvas.DrawRoundedRect(mobilegles.Rect{X: -rw / 2, Y: -rh / 2, Width: rw, Height: rh}, rr, rr, p)
		// A STROKED rotated rounded rect, in a colour the fill does not use, drawn
		// on the same frame so both are measured. The stroke is evaluated in the
		// same local space as the fill, so it must rotate with the shape: if it did
		// not, the ring would sit axis-aligned and its bbox would be the sharp AABB.
		sp := mobilegles.NewPaint()
		sp.SetStyle(int32(mobilegles.Stroke))
		sp.SetStrokeWidth(4)
		sp.SetColor(mobilegles.RGBA(0, 200, 255, 255))
		canvas.DrawRoundedRect(mobilegles.Rect{X: -rw / 2, Y: -rh / 2, Width: rw, Height: rh}, rr, rr, sp)
		canvas.Restore()

		mobilegles.FlushBatches()
		px = mobilegles.ReadScreenPixels(W, H)
		mobilegles.ReleaseRenderer()
	}

	// Bottom-up readback: index it the way clipleakprobe does.
	at := func(x, y int) uint32 { return px[(H-1-y)*W+x] }
	n, minX, minY, maxX, maxY := 0, W, H, -1, -1
	sn2, sMinX, sMinY, sMaxX, sMaxY := 0, W, H, -1, -1
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			v := at(x, y)
			isFill := (v>>16)&0xFF > 140 && (v>>8)&0xFF > 140 && v&0xFF > 140
			isStroke := (v>>16)&0xFF < 90 && (v>>8)&0xFF > 140 && v&0xFF > 200
			if !isFill && !isStroke {
				continue
			}
			if isStroke {
				sn2++
				if x < sMinX {
					sMinX = x
				}
				if x > sMaxX {
					sMaxX = x
				}
				if y < sMinY {
					sMinY = y
				}
				if y > sMaxY {
					sMaxY = y
				}
			}
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

	rad := deg * math.Pi / 180
	cs, sn := math.Abs(math.Cos(rad)), math.Abs(math.Sin(rad))
	// A rounded rect is the Minkowski sum of its INNER rect and a disc of radius
	// rr, so its rotated AABB is the rotated inner rect's AABB grown by rr on every
	// side — NOT the sharp rect's AABB, which is 7px larger here and was the first
	// thing this probe got wrong.
	iw, ih := rw-2*rr, rh-2*rr
	wantW := iw*cs + ih*sn + 2*rr
	wantH := iw*sn + ih*cs + 2*rr
	// The rounded rect's own area: the box minus the four corner cut-offs.
	wantArea := rw*rh - (4-math.Pi)*rr*rr

	fmt.Printf("ink bbox   : %dx%d  (analytic AABB of the rotated ROUNDED rect: %.0fx%.0f)\n",
		maxX-minX+1, maxY-minY+1, wantW, wantH)
	fmt.Printf("ink area   : %d px (analytic rounded area: %.0f; a plain rect would be %.0f, a bounding box %.0f)\n",
		n, wantArea, rw*rh, wantW*wantH)

	if n == 0 {
		fmt.Println("INCONCLUSIVE: nothing drawn")
		os.Exit(2)
	}
	gotW, gotH := float64(maxX-minX+1), float64(maxY-minY+1)
	if math.Abs(gotW-wantW) > 2 || math.Abs(gotH-wantH) > 2 {
		fmt.Printf("FAIL: the rotated rounded rect is not its rotated shape (bbox off by %.0f, %.0f)\n",
			gotW-wantW, gotH-wantH)
		os.Exit(1)
	}
	fmt.Printf("stroke bbox: %dx%d, %d px (must be the SAME rotated AABB: %.0fx%.0f)\n",
		sMaxX-sMinX+1, sMaxY-sMinY+1, sn2, wantW, wantH)
	if math.Abs(float64(sMaxX-sMinX+1)-wantW) > 3 || math.Abs(float64(sMaxY-sMinY+1)-wantH) > 3 {
		fmt.Println("FAIL: the rotated stroke is not on the rotated shape")
		os.Exit(1)
	}
	if sn2 == 0 {
		fmt.Println("FAIL: no stroke drawn")
		os.Exit(1)
	}
	if math.Abs(float64(n)-wantArea) > wantArea*0.06 {
		fmt.Printf("FAIL: area %d against the rounded rect's %.0f — it is a box, not a rounded rect\n", n, wantArea)
		os.Exit(1)
	}
	fmt.Println("PASS: the rotated rounded rect is its true rotated shape")
}
