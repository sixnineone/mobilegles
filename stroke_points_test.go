package mobilegles

import "testing"

// A stroke is drawn as one anti-aliased quad per contour segment, and those
// quads' shared edges are anti-aliased too — so a run of collinear segments
// (a straight cable, the flat part of a curve) comes out dashed where a single
// segment is solid. strokePoints merges such a run; these are the numbers.
func TestStrokePointsMergesCollinearRuns(t *testing.T) {
	// A straight run of 100 points collapses to its two ends.
	line := make([]Point, 0, 100)
	for i := 0; i < 100; i++ {
		line = append(line, Point{X: float32(i) * 6, Y: 10})
	}
	if got := strokePoints(line); len(got) != 2 {
		t.Errorf("a straight run of %d points kept %d: %v", len(line), len(got), got)
	}

	// A run that turns keeps the corner and drops the collinear points around it.
	bent := []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}, {X: 30, Y: 10}}
	got := strokePoints(bent)
	if len(got) != 3 || got[1] != (Point{X: 20, Y: 0}) {
		t.Errorf("a turn kept %v, want the corner at (20,0)", got)
	}
	// A wobble under the tolerance is absorbed.
	wobble := []Point{{X: 0, Y: 0}, {X: 10, Y: 0.1}, {X: 20, Y: 0}}
	if got := strokePoints(wobble); len(got) != 2 {
		t.Errorf("a sub-pixel wobble kept %d points: %v", len(got), got)
	}

	// Ends are always kept, and short inputs pass through untouched.
	if got := strokePoints([]Point{{X: 1, Y: 1}}); len(got) != 1 {
		t.Errorf("a single point became %v", got)
	}
	if got := strokePoints(nil); len(got) != 0 {
		t.Errorf("an empty contour became %v", got)
	}
	ends := strokePoints([]Point{{X: 0, Y: 0}, {X: 100, Y: 0}})
	if len(ends) != 2 || ends[0].X != 0 || ends[1].X != 100 {
		t.Errorf("the endpoints moved: %v", ends)
	}

	// A real curve keeps its shape: the simplified point set must stay within a
	// quarter pixel of the original along its whole length.
	curve := make([]Point, 0, 64)
	for i := 0; i < 64; i++ {
		x := float32(i) * 4
		curve = append(curve, Point{X: x, Y: 100 - float32(i*i)/16})
	}
	simple := strokePoints(curve)
	if len(simple) >= len(curve) || len(simple) < 3 {
		t.Fatalf("a curve simplified to %d points (from %d)", len(simple), len(curve))
	}
	for _, p := range curve {
		worst := float32(1e9)
		for i := 0; i+1 < len(simple); i++ {
			if d := pointLineDistance(p, simple[i], simple[i+1]); d < worst {
				worst = d
			}
		}
		if worst > strokeTolerance+0.01 {
			t.Errorf("point %v is %.2fpx off the simplified path", p, worst)
		}
	}
}

// A stroke's geometry stays in the path's own coordinates: the primitives that
// draw it apply the canvas transform themselves. Transforming here as well put
// every stroked path at twice the canvas origin — the node graph's cables landed
// a whole content offset below their own sockets, which is what "the cables do
// not connect to their slots" was.
func TestStrokeGeometryIsLocalToTheCanvas(t *testing.T) {
	path := NewPath()
	path.MoveTo(Point{X: 10, Y: 10})
	path.LineTo(Point{X: 60, Y: 10})
	path.LineTo(Point{X: 60, Y: 60})

	segs, joins := strokeGeometry(buildContours(path))
	if len(segs) != 2 || len(joins) != 1 {
		t.Fatalf("geometry = %d segments, %d joins; want 2 and 1", len(segs), len(joins))
	}
	if segs[0][0] != (Point{X: 10, Y: 10}) || segs[1][1] != (Point{X: 60, Y: 60}) {
		t.Errorf("segments are not the path's own points: %v", segs)
	}
	if joins[0][1] != (Point{X: 60, Y: 10}) {
		t.Errorf("the join is not at the corner: %v", joins)
	}

	// The canvas transform must not appear in this geometry: a canvas moved by the
	// title bar would otherwise shift the stroke twice.
	c := NewCanvas(200, 200)
	c.Translate(0, 40)
	segs2, _ := strokeGeometry(buildContours(path))
	if len(segs2) != len(segs) {
		t.Fatalf("geometry changed with a canvas transform: %d segments", len(segs2))
	}
	for i := range segs {
		if segs2[i] != segs[i] {
			t.Errorf("segment %d moved with the canvas transform: %v -> %v", i, segs[i], segs2[i])
		}
	}
}
