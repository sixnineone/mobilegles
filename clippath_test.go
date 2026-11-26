package mobilegles

import (
	"math"
	"testing"
)

// hull corners of a unit square, in various input orders, must all reduce to
// the same four-edge clip regardless of winding.
func TestConvexClipPlanes_SquareFromAnyOrder(t *testing.T) {
	orders := [][]Point{
		{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		{{X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}, {X: 0, Y: 0}},
		{{X: 10, Y: 10}, {X: 0, Y: 0}, {X: 10, Y: 0}, {X: 0, Y: 10}},
	}
	for i, pts := range orders {
		planes := convexClipPlanes(pts, 0)
		if len(planes) != 4 {
			t.Fatalf("order %d: got %d planes, want 4", i, len(planes))
		}
		// Inside points must satisfy every plane; outside points must fail at
		// least one. This is the property the fragment shader relies on, and it
		// is independent of how the winding was detected.
		for _, in := range []Point{{X: 5, Y: 5}, {X: 0.5, Y: 0.5}, {X: 9.5, Y: 9.5}} {
			for j, pl := range planes {
				if !pl.contains(in.X, in.Y) {
					t.Errorf("order %d: inside point %+v rejected by plane %d (%+v)", i, in, j, pl)
				}
			}
		}
		for _, out := range []Point{{X: -0.5, Y: 5}, {X: 5, Y: -0.5}, {X: 10.5, Y: 5}, {X: 5, Y: 10.5}} {
			kept := true
			for _, pl := range planes {
				if !pl.contains(out.X, out.Y) {
					kept = false
					break
				}
			}
			if kept {
				t.Errorf("order %d: outside point %+v survived every plane", i, out)
			}
		}
	}
}

func TestConvexClipPlanes_Triangle(t *testing.T) {
	planes := convexClipPlanes([]Point{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 0, Y: 20}}, 0)
	if len(planes) != 3 {
		t.Fatalf("got %d planes, want 3", len(planes))
	}
	if !allContain(planes, 2, 2) {
		t.Error("interior point (2,2) rejected by a triangle clip")
	}
	// (15,15) is outside the hypotenuse x+y<=20.
	if allContain(planes, 15, 15) {
		t.Error("exterior point (15,15) survived the triangle clip")
	}
}

// A concave outline must never produce planes that cut inside it: the hull is a
// superset, so every point of the original outline stays inside the clip.
func TestConvexClipPlanes_ConcaveHullIsASuperset(t *testing.T) {
	// An arrowhead / chevron: concave at the notch.
	pts := []Point{
		{X: 0, Y: 0}, {X: 10, Y: 5}, {X: 20, Y: 0},
		{X: 20, Y: 20}, {X: 10, Y: 15}, {X: 0, Y: 20},
	}
	planes := convexClipPlanes(pts, 0)
	if len(planes) == 0 {
		t.Fatal("concave outline produced no clip at all")
	}
	for _, p := range pts {
		if !allContain(planes, p.X, p.Y) {
			t.Errorf("outline vertex %+v fell outside the clip built from the outline", p)
		}
	}
}

// The outward nudge exists so a flattened curve's inscribed hull cannot cut
// inside the curve. It must widen the clip, never narrow it.
func TestConvexClipPlanes_NudgeWidensOnly(t *testing.T) {
	pts := []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	tight := convexClipPlanes(pts, 0)
	wide := convexClipPlanes(pts, 0.5)
	if len(tight) != 4 || len(wide) != 4 {
		t.Fatalf("plane counts = %d / %d, want 4 / 4", len(tight), len(wide))
	}
	// A point just outside the tight clip but within the nudge must be kept by
	// the wide one and rejected by the tight one.
	px, py := float32(-0.25), float32(5)
	if allContain(tight, px, py) {
		t.Error("tight clip kept a point 0.25px outside it")
	}
	if !allContain(wide, px, py) {
		t.Error("nudged clip rejected a point inside the nudge")
	}
	// And it must not have moved the far side inward.
	if !allContain(wide, 10.4, 5) || allContain(tight, 10.4, 5) {
		t.Error("nudge changed the wrong side of the boundary")
	}
}

func TestConvexClipPlanes_DegenerateInputs(t *testing.T) {
	cases := map[string][]Point{
		"empty":            nil,
		"one point":        {{X: 1, Y: 1}},
		"two points":       {{X: 0, Y: 0}, {X: 1, Y: 1}},
		"collinear":        {{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}},
		"duplicate points": {{X: 1, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 1}},
	}
	for name, pts := range cases {
		if got := convexClipPlanes(pts, 0); got != nil {
			t.Errorf("%s: got %d planes, want nil (no clip)", name, len(got))
		}
	}
}

// Collinear points on an edge must not each become a plane, or a rectangle
// whose edges were built from many line segments would blow past the cap and
// silently degrade to a bounding box.
func TestConvexHull_DropsCollinearPoints(t *testing.T) {
	pts := []Point{
		{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 10, Y: 0},
		{X: 10, Y: 10}, {X: 0, Y: 10},
	}
	hull := convexHull(pts)
	if len(hull) != 4 {
		t.Fatalf("hull has %d vertices, want 4 (collinear midpoint dropped): %+v", len(hull), hull)
	}
	if got := convexClipPlanes(pts, 0); len(got) != 4 {
		t.Errorf("got %d planes, want 4", len(got))
	}
}

// Too many edges must fail loudly (nil) so the caller falls back to a
// conservative rectangle, rather than emitting a truncated, in-cutting clip.
func TestConvexClipPlanes_TooManyEdgesReturnsNil(t *testing.T) {
	var pts []Point
	const n = maxClipPlanes + 2
	for i := 0; i < n; i++ {
		a := 2 * math.Pi * float64(i) / float64(n)
		pts = append(pts, Point{X: float32(50 * math.Cos(a)), Y: float32(50 * math.Sin(a))})
	}
	if got := convexClipPlanes(pts, 0); got != nil {
		t.Errorf("got %d planes for a %d-gon, want nil so the caller falls back", len(got), n)
	}
}

func TestPolygonSignedArea_Winding(t *testing.T) {
	ccw := []Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
	if polygonSignedArea(ccw) <= 0 {
		t.Error("counter-clockwise square reported non-positive area")
	}
	cw := []Point{{X: 0, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 0}}
	if polygonSignedArea(cw) >= 0 {
		t.Error("clockwise square reported non-negative area")
	}
}

func allContain(planes []ClipPlane, x, y float32) bool {
	for _, p := range planes {
		if !p.contains(x, y) {
			return false
		}
	}
	return true
}
