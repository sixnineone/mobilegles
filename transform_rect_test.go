package mobilegles

import (
	"math"
	"testing"
)

// TestTransformRectUsesAllFourCorners is the regression test for REVIEW.md §6: a rect
// under a rotation was transformed by its (x,y) and (x+w,y+h) corners only, which is
// its bounding box ONLY while the matrix is axis-aligned. The other two corners fall
// outside that box as soon as there is a rotation, so the extent came out too small —
// and at 45 degrees on a SQUARE the two opposite corners land on a vertical line and
// it collapses to a sliver, which is the node graph's PortDiamond (a square drawn
// under Rotate(45)).
//
// It is headless on purpose: transformRect is pure matrix arithmetic, so this runs in
// the default gate rather than only under -tags visual.
func TestTransformRectUsesAllFourCorners(t *testing.T) {
	const w, h = 80.0, 40.0

	for _, deg := range []float64{20, 45, 90} {
		c := NewCanvas(400, 400)
		c.Translate(200, 200)
		c.Rotate(float32(deg))
		_, _, gotW, gotH := c.TransformRect(-w/2, -h/2, w, h)

		// The analytic bounding box of the rotated rect: the sum of the absolute
		// projections of its two edge vectors onto each axis.
		rad := deg * math.Pi / 180
		cs, sn := math.Abs(math.Cos(rad)), math.Abs(math.Sin(rad))
		wantW := float32(w*cs + h*sn)
		wantH := float32(w*sn + h*cs)

		// A rotated rect's AABB is at least as large as the rect in both axes; the
		// two-corner version is strictly smaller whenever the rotation is neither 0
		// nor 90 degrees.
		if math.Abs(float64(gotW-wantW)) > 0.5 || math.Abs(float64(gotH-wantH)) > 0.5 {
			t.Errorf("at %v degrees the transformed extent is %.1fx%.1f, want %.1fx%.1f — "+
				"the AABB must span all FOUR corners, not just (x,y) and (x+w,y+h)",
				deg, gotW, gotH, wantW, wantH)
		}
	}
}

// TestTransformRectLeavesAxisAlignedRectsAlone guards the other direction: the fix
// must not move a rect that is not rotated, because every ordinary rect in every
// scene goes through it.
func TestTransformRectLeavesAxisAlignedRectsAlone(t *testing.T) {
	c := NewCanvas(400, 400)
	c.Translate(30, 40)
	x, y, w, h := c.TransformRect(10, 20, 80, 40)
	if x != 40 || y != 60 || w != 80 || h != 40 {
		t.Errorf("an unrotated rect moved: got (%.0f,%.0f %.0fx%.0f), want (40,60 80x40)", x, y, w, h)
	}
}
