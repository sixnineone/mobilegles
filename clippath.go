package mobilegles

import "math"

// maxClipPlanes caps how many half-planes one path clip may contribute. A
// convex outline needs one plane per edge; beyond this the clip degrades to the
// bounding rectangle (a superset, so nothing is hidden) rather than silently
// dropping edges, which would clip away content the caller asked to keep.
const maxClipPlanes = 8

// ClipPlane is one half-plane of a convex clip, in DEVICE space (GL window
// coordinates, y increasing upward). A pixel is kept when
//
//	NX*x + NY*y + D >= 0
//
// Planes are always combined by intersection.
type ClipPlane struct {
	NX, NY, D float32
}

// contains reports whether the point is on the kept side of the plane.
func (p ClipPlane) contains(x, y float32) bool {
	return p.NX*x+p.NY*y+p.D >= 0
}

// convexClipPlanes returns the half-planes of the convex hull of pts, or nil
// when pts cannot describe a clip (fewer than three distinct points, or a
// degenerate, zero-area outline).
//
// offsetNudge moves every plane outward by that many device pixels. The hull of
// a *flattened curve* is inscribed in the curve, so it is smaller than the shape
// the caller drew; the nudge makes the clip a superset again, and clipping is
// only ever allowed to show more than asked, never less. Pass 0 for straight-
// edged outlines, where the hull is exact.
func convexClipPlanes(pts []Point, offsetNudge float32) []ClipPlane {
	hull := convexHull(pts)
	if len(hull) < 3 {
		return nil
	}
	area := polygonSignedArea(hull)
	if area == 0 {
		return nil
	}
	// Positive area means counter-clockwise in the given coordinate system; the
	// interior then lies to the LEFT of each directed edge.
	ccw := area > 0
	planes := make([]ClipPlane, 0, len(hull))
	for i := range hull {
		a := hull[i]
		b := hull[(i+1)%len(hull)]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := float32(math.Hypot(float64(dx), float64(dy)))
		if l < 1e-6 {
			continue
		}
		var nx, ny float32
		if ccw {
			nx, ny = -dy/l, dx/l
		} else {
			nx, ny = dy/l, -dx/l
		}
		planes = append(planes, ClipPlane{
			NX: nx,
			NY: ny,
			D:  -(nx*a.X + ny*a.Y) + offsetNudge,
		})
	}
	if len(planes) < 3 || len(planes) > maxClipPlanes {
		return nil
	}
	return planes
}

// convexHull returns the convex hull of pts in counter-clockwise order,
// dropping duplicate and collinear points. Andrew's monotone chain, which is
// robust to the input order and to repeated vertices.
func convexHull(pts []Point) []Point {
	if len(pts) < 3 {
		return nil
	}
	sorted := make([]Point, len(pts))
	copy(sorted, pts)
	// Insertion sort by x, then y: hull inputs are a handful of points from one
	// flattened contour, so this beats sort.Slice's reflection overhead and
	// avoids pulling sort into the renderer.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			if sorted[j-1].X < sorted[j].X ||
				(sorted[j-1].X == sorted[j].X && sorted[j-1].Y <= sorted[j].Y) {
				break
			}
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	// Drop exact duplicates so the cross-product test cannot be handed a
	// zero-length edge.
	uniq := sorted[:0]
	for i, p := range sorted {
		if i == 0 || p != sorted[i-1] {
			uniq = append(uniq, p)
		}
	}
	sorted = uniq
	if len(sorted) < 3 {
		return nil
	}

	// cross is positive for a counter-clockwise turn in a y-up system.
	cross := func(o, a, b Point) float32 {
		return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
	}
	build := func(in []Point) []Point {
		out := make([]Point, 0, len(in))
		for _, p := range in {
			for len(out) >= 2 && cross(out[len(out)-2], out[len(out)-1], p) <= 0 {
				out = out[:len(out)-1]
			}
			out = append(out, p)
		}
		return out
	}
	lower := build(sorted)
	// Upper chain over the reversed input.
	rev := make([]Point, 0, len(sorted))
	for i := len(sorted) - 1; i >= 0; i-- {
		rev = append(rev, sorted[i])
	}
	upper := build(rev)
	// The last point of each chain is the first of the other.
	hull := append(lower[:len(lower)-1:len(lower)-1], upper[:len(upper)-1]...)
	if len(hull) < 3 {
		return nil
	}
	return hull
}

// polygonSignedArea returns twice the signed area of a closed polygon: positive
// for counter-clockwise winding in a y-up coordinate system.
func polygonSignedArea(pts []Point) float32 {
	if len(pts) < 3 {
		return 0
	}
	var sum float32
	for i := range pts {
		a := pts[i]
		b := pts[(i+1)%len(pts)]
		sum += a.X*b.Y - b.X*a.Y
	}
	return sum
}

// clipPlanesForContour converts one flattened contour, given in CANVAS
// coordinates (y down), into device-space clip planes. It returns nil when the
// contour cannot be expressed within maxClipPlanes.
//
// It also reports whether the clip is EXACT, meaning the caller asked for the
// region the planes describe. A concave outline is clipped to its convex hull,
// and a contour containing beziers is clipped to the hull of its flattened
// approximation: both are supersets of what was drawn, so nothing is hidden,
// but neither is what the caller asked for and both report false.
func clipPlanesForContour(canvasHeight float32, canvasPts []Point, curved bool) ([]ClipPlane, bool) {
	if len(canvasPts) < 3 {
		return nil, false
	}
	// Canvas y grows downward; device space grows upward. The height must be the
	// CANVAS' own: the global screen height belongs to whichever window last
	// called SetScreenSize, and flipping with it clips paths offset by the
	// difference - the residual short-clip after the scissor fix.
	sh := canvasHeight
	dev := make([]Point, len(canvasPts))
	for i, p := range canvasPts {
		dev[i] = Point{X: p.X, Y: sh - p.Y}
	}
	var nudge float32
	if curved {
		nudge = curveClipTolerance
	}
	planes := convexClipPlanes(dev, nudge)
	if planes == nil {
		return nil, false
	}
	// Convexity is decided on the UNnudged hull: a nudge would push every
	// outline point strictly inside and make any contour look non-convex.
	if curved {
		return planes, false
	}
	return planes, outlineIsConvex(dev, planes)
}

// outlineIsConvex reports whether every point of the outline lies on the hull's
// boundary. A point strictly inside means the hull cut a corner, so the clip is
// a strict superset of the outline.
func outlineIsConvex(pts []Point, planes []ClipPlane) bool {
	const eps = 0.01
	for _, p := range pts {
		onEdge := false
		for _, pl := range planes {
			v := pl.NX*p.X + pl.NY*p.Y + pl.D
			if v < 0 {
				v = -v
			}
			if v <= eps {
				onEdge = true
				break
			}
		}
		if !onEdge {
			return false
		}
	}
	return true
}

// curveClipTolerance is how far a curved contour's clip planes are nudged
// outward so the clip cannot cut inside the curve the caller drew. The
// flattener subdivides into 8 segments, so the sagitta of a quadratic is well
// under a pixel for any UI-scale radius; half a pixel of slack is enough and is
// invisible at the clip edge.
const curveClipTolerance = 0.5
