package mobilegles

import (
	"testing"
)

func TestRoundedRectTessellation(t *testing.T) {
	// Replicate DrawRoundedRect's path construction
	x, y, w, h := float32(150), float32(10), float32(120), float32(60)
	rx, ry := float32(8), float32(8)
	path := NewPath()
	segs := 8
	trCX, trCY := x+w-rx, y+ry
	brCX, brCY := x+w-rx, y+h-ry
	blCX, blCY := x+rx, y+h-ry
	tlCX, tlCY := x+rx, y+ry
	path.MoveTo(Point{X: x + rx, Y: y})
	path.LineTo(Point{X: trCX, Y: y})
	for i := 1; i <= segs; i++ {
		a := (270 + float32(i)*90/float32(segs)) * 3.14159265 / 180
		path.LineTo(Point{X: trCX + rx*cosf(a), Y: trCY + ry*sinf(a)})
	}
	path.LineTo(Point{X: x + w, Y: brCY})
	for i := 1; i <= segs; i++ {
		a := float32(i) * 90 / float32(segs) * 3.14159265 / 180
		path.LineTo(Point{X: brCX + rx*cosf(a), Y: brCY + ry*sinf(a)})
	}
	path.LineTo(Point{X: blCX, Y: y + h})
	for i := 1; i <= segs; i++ {
		a := (90 + float32(i)*90/float32(segs)) * 3.14159265 / 180
		path.LineTo(Point{X: blCX + rx*cosf(a), Y: blCY + ry*sinf(a)})
	}
	path.LineTo(Point{X: x, Y: tlCY})
	for i := 1; i <= segs; i++ {
		a := (180 + float32(i)*90/float32(segs)) * 3.14159265 / 180
		path.LineTo(Point{X: tlCX + rx*cosf(a), Y: tlCY + ry*sinf(a)})
	}
	path.Close()

	t.Logf("path points: %d", len(path.points))
	contours := buildContours(path)
	if len(contours) != 1 {
		t.Fatalf("contours = %d, want 1", len(contours))
	}
	pts := contours[0].points
	t.Logf("contour points: %d", len(pts))
	if len(pts) < 3 {
		t.Fatalf("too few points")
	}
	// verify all points inside the rect bounds
	for _, p := range pts {
		if p.X < x-0.6 || p.X > x+w+0.6 || p.Y < y-0.6 || p.Y > y+h+0.6 {
			t.Errorf("point outside rect: (%v,%v)", p.X, p.Y)
		}
	}
	tris := earcut(pts)
	t.Logf("triangles: %d", len(tris))
	if len(tris) == 0 {
		t.Fatal("earcut produced no triangles")
	}
	// every triangle vertex must lie inside the rect too
	for _, tri := range tris {
		for _, p := range tri {
			if p.X < x-0.6 || p.X > x+w+0.6 || p.Y < y-0.6 || p.Y > y+h+0.6 {
				t.Errorf("triangle vertex outside rect: (%v,%v)", p.X, p.Y)
			}
		}
	}
}
