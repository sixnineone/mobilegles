package mobilegles

import (
	"sync/atomic"
)

// appendShape appends one shape (6 rect verts or a circle fan) to the
// shape batch with the current clip stamped per vertex. shapeType:
// 0 = rect (verts built from ±0.5 corners), 1 = circle/oval (fan).
func appendShape(center, half [2]float32, angle float32, color Color, shapeType int, buildVerts func() []float32) {
	flushSDFBatch() // strict draw-order: SDF shapes submitted earlier go first
	if triBatch.dirty || textBatch.dirty {
		flushBatches() // strict draw-order
	}
	atomic.AddInt64(&statPrimitives, 1)
	c0, c1, c2, c3 := curClipQuad()
	r := float32((color>>16)&0xFF) / 255
	g := float32((color>>8)&0xFF) / 255
	b := float32(color&0xFF) / 255
	a := float32((color>>24)&0xFF) / 255
	verts := buildVerts()
	for i := 0; i < len(verts); i += 2 {
		shapeBatch.verts = append(shapeBatch.verts,
			center[0], center[1],
			verts[i], verts[i+1],
			half[0], half[1],
			angle,
			r, g, b, a,
			float32(shapeType),
			c0, c1, c2, c3,
		)
	}
	shapeBatch.dirty = true
}

func rectCorners() []float32 {
	return rectCornerScratch[:]
}

var rectCornerScratch = [12]float32{
	-0.5, -0.5, 0.5, -0.5, -0.5, 0.5,
	-0.5, 0.5, 0.5, -0.5, 0.5, 0.5,
}

// drawRectGL draws a filled rectangle — batched (attribute-driven shape
// shader; identical SDF/AA math as the old immediate draw).
func drawRectGL(x, y, w, h float32, color Color, angle float32) {
	if !rectRenderer.ready {
		return
	}
	appendShape([2]float32{x + w/2, y + h/2}, [2]float32{w / 2, h / 2}, angle, color, 0, rectCorners)
}

// circleFanScratch builds a 40-segment fan (120 verts) as local coords on
// the unit rim — matches the old circleVBO geometry exactly.
var circleFanScratch [120 * 2]float32

func circleFan() []float32 {
	const segs = 40
	for i := 0; i < segs; i++ {
		a0 := float32(i) / segs * 2 * 3.14159265
		a1 := float32(i+1) / segs * 2 * 3.14159265
		base := i * 6
		circleFanScratch[base+0] = 0
		circleFanScratch[base+1] = 0
		circleFanScratch[base+2] = 0.5 * cosf(a0)
		circleFanScratch[base+3] = 0.5 * sinf(a0)
		circleFanScratch[base+4] = 0.5 * cosf(a1)
		circleFanScratch[base+5] = 0.5 * sinf(a1)
	}
	return circleFanScratch[:]
}

// drawCircleGL draws a filled circle — batched.
func drawCircleGL(cx, cy, radius float32, color Color) {
	if !rectRenderer.ready || radius <= 0 {
		return
	}
	appendShape([2]float32{cx, cy}, [2]float32{radius, radius}, 0, color, 1, circleFan)
}

// drawOvalGL draws a filled oval — batched (fan aspect via aHalf).
func drawOvalGL(x, y, w, h float32, color Color) {
	if !rectRenderer.ready {
		return
	}
	appendShape([2]float32{x + w/2, y + h/2}, [2]float32{w / 2, h / 2}, 0, color, 1, circleFan)
}

// drawLineGL draws a line from (x1,y1) to (x2,y2) as a rotated quad — batched.
func drawLineGL(x1, y1, x2, y2 float32, color Color, strokeW float32) {
	if !rectRenderer.ready {
		return
	}
	dx := x2 - x1
	dy := y2 - y1
	length := sqrtf(dx*dx + dy*dy)
	if length < 0.1 {
		return
	}
	angle := atan2f(dy, dx)

	appendShape([2]float32{x1 + dx/2, y1 + dy/2}, [2]float32{length / 2, strokeW / 2}, angle, color, 0, rectCorners)
}

// drawMeshGL renders a 3D mesh with MVP transform and lighting.
