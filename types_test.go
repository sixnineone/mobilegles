package mobilegles

import "testing"

func TestMat4IdentityTransform(t *testing.T) {
	m := Identity()
	v := m.Transform(Vec3{X: 1, Y: 2, Z: 3})
	if v.X != 1 || v.Y != 2 || v.Z != 3 {
		t.Errorf("Identity transform: got (%f,%f,%f), want (1,2,3)", v.X, v.Y, v.Z)
	}
}

func TestMat4Translate(t *testing.T) {
	m := Identity().Translate(Vec3{X: 10, Y: 20, Z: 30})
	v := m.Transform(Vec3{X: 1, Y: 2, Z: 3})
	if v.X != 11 || v.Y != 22 || v.Z != 33 {
		t.Errorf("Translate: got (%f,%f,%f), want (11,22,33)", v.X, v.Y, v.Z)
	}
}

func TestMat4Scale(t *testing.T) {
	m := Identity().Scale(Vec3{X: 2, Y: 3, Z: 4})
	v := m.Transform(Vec3{X: 1, Y: 2, Z: 3})
	if v.X != 2 || v.Y != 6 || v.Z != 12 {
		t.Errorf("Scale: got (%f,%f,%f), want (2,6,12)", v.X, v.Y, v.Z)
	}
}

func TestMat4RotateX(t *testing.T) {
	m := Identity().Rotate(90, Vec3{X: 1, Y: 0, Z: 0})
	v := m.Transform(Vec3{X: 0, Y: 1, Z: 0})
	// 90° around X: (0,1,0) → (0,0,1)
	if absF32(v.X) > 0.01 || absF32(v.Y) > 0.01 || absF32(v.Z-1) > 0.01 {
		t.Errorf("RotateX: got (%f,%f,%f), want approx (0,0,1)", v.X, v.Y, v.Z)
	}
}

func TestMat4RotateY(t *testing.T) {
	m := Identity().Rotate(90, Vec3{X: 0, Y: 1, Z: 0})
	v := m.Transform(Vec3{X: 1, Y: 0, Z: 0})
	// 90° around Y: (1,0,0) → (0,0,-1)
	if absF32(v.X) > 0.01 || absF32(v.Y) > 0.01 || absF32(v.Z+1) > 0.01 {
		t.Errorf("RotateY: got (%f,%f,%f), want approx (0,0,-1)", v.X, v.Y, v.Z)
	}
}

func TestMat4RotateZ(t *testing.T) {
	m := Identity().Rotate(90, Vec3{X: 0, Y: 0, Z: 1})
	v := m.Transform(Vec3{X: 1, Y: 0, Z: 0})
	// 90° around Z: (1,0,0) → (0,1,0)
	if absF32(v.X) > 0.01 || absF32(v.Y-1) > 0.01 || absF32(v.Z) > 0.01 {
		t.Errorf("RotateZ: got (%f,%f,%f), want approx (0,1,0)", v.X, v.Y, v.Z)
	}
}

func TestMat4MultiplyOrder(t *testing.T) {
	tX := Identity().Translate(Vec3{X: 5, Y: 0, Z: 0})
	rZ := Identity().Rotate(90, Vec3{X: 0, Y: 0, Z: 1})
	// Translate then rotate
	tr := rZ.Multiply(tX)
	v := tr.Transform(Vec3{X: 0, Y: 0, Z: 0})
	// (0,0,0) translated by (5,0,0) → (5,0,0), then rotated 90° Z → (0,5,0)
	if absF32(v.X) > 0.01 || absF32(v.Y-5) > 0.01 || absF32(v.Z) > 0.01 {
		t.Errorf("TR order: got (%f,%f,%f), want approx (0,5,0)", v.X, v.Y, v.Z)
	}
}

func TestMat4PerspectiveProperties(t *testing.T) {
	m := Perspective(90, 1.6, 0.1, 100)
	// Perspective matrix should have non-zero elements in projection-dependent positions
	if m[0] <= 0 || m[5] <= 0 {
		t.Errorf("Perspective f/aspect and f should be positive; got m[0]=%f m[5]=%f", m[0], m[5])
	}
	if m[10] >= 0 {
		t.Errorf("Perspective far+near/near-far should be negative; got m[10]=%f", m[10])
	}
	// m[11] should be -1 (the homogeneous divide component)
	if m[11] != -1 {
		t.Errorf("Perspective m[11] should be -1; got %f", m[11])
	}
}

func TestMat4OrthoProperties(t *testing.T) {
	m := Ortho(0, 100, 100, 0, -1, 1)
	// Center of viewport should map to (0,0)
	v := m.Transform(Vec3{X: 50, Y: 50, Z: 0})
	if absF32(v.X) > 0.01 || absF32(v.Y) > 0.01 {
		t.Errorf("Ortho center: got (%f,%f), want (0,0)", v.X, v.Y)
	}
	// Near plane point should map within NDC
	v2 := m.Transform(Vec3{X: 100, Y: 0, Z: -1})
	// Ortho(0,100,100,0,-1,1): (100,0) → NDC (1,1) in GL Y-up convention
	if v2.X < 0.9 || v2.X > 1.1 || v2.Y < 0.9 || v2.Y > 1.1 {
		t.Errorf("Ortho far-right-top: got (%f,%f), want approx (1,1)", v2.X, v2.Y)
	}
}

func TestMat4InverseRoundTrip(t *testing.T) {
	m := Identity()
	m = m.Translate(Vec3{X: 10, Y: 20, Z: 30})
	m = m.Rotate(45, Vec3{X: 1, Y: 1, Z: 0})
	m = m.Scale(Vec3{X: 2, Y: 2, Z: 2})
	inv := m.Inverse()
	prod := m.Multiply(inv)
	for i := 0; i < 16; i++ {
		expected := float32(0)
		if i%5 == 0 {
			expected = 1
		}
		if absF32(prod[i]-expected) > 0.01 {
			t.Errorf("Inverse round-trip failed at [%d]: got %f, want %f", i, prod[i], expected)
		}
	}
}

func TestMat4LookAt(t *testing.T) {
	eye := Vec3{X: 0, Y: 0, Z: 5}
	center := Vec3{X: 0, Y: 0, Z: 0}
	up := Vec3{X: 0, Y: 1, Z: 0}
	m := LookAt(eye, center, up)
	// Camera at (0,0,5) looking at origin.
	// Origin should be at (0,0,-5) relative to camera, along -Z in view space.
	v := m.Transform(center)
	if absF32(v.X) > 0.01 || absF32(v.Y) > 0.01 || absF32(v.Z+5) > 0.01 {
		t.Errorf("LookAt origin: got (%f,%f,%f), want approx (0,0,-5)", v.X, v.Y, v.Z)
	}
}

func TestPathBounds(t *testing.T) {
	p := NewPath()
	p.MoveTo(Point{X: 10, Y: 20})
	p.LineTo(Point{X: 100, Y: 200})
	p.Close()
	b := p.Bounds()
	if b.X != 10 || b.Y != 20 || b.Width != 90 || b.Height != 180 {
		t.Errorf("Path bounds: got (%f,%f,%f,%f), want (10,20,90,180)", b.X, b.Y, b.Width, b.Height)
	}
}

func TestVec3AddSub(t *testing.T) {
	a := Vec3{X: 1, Y: 2, Z: 3}
	b := Vec3{X: 4, Y: 5, Z: 6}
	s := a.Add(b)
	if s.X != 5 || s.Y != 7 || s.Z != 9 {
		t.Errorf("Add: got (%f,%f,%f), want (5,7,9)", s.X, s.Y, s.Z)
	}
	d := a.Sub(b)
	if d.X != -3 || d.Y != -3 || d.Z != -3 {
		t.Errorf("Sub: got (%f,%f,%f), want (-3,-3,-3)", d.X, d.Y, d.Z)
	}
}

func TestVec3CrossProduct(t *testing.T) {
	// Right-hand rule: X × Y = Z
	x := Vec3{X: 1, Y: 0, Z: 0}
	y := Vec3{X: 0, Y: 1, Z: 0}
	z := x.Cross(y)
	if absF32(z.X) > 0.01 || absF32(z.Y) > 0.01 || absF32(z.Z-1) > 0.01 {
		t.Errorf("X×Y: got (%f,%f,%f), want (0,0,1)", z.X, z.Y, z.Z)
	}
}

func TestVec3Normalize(t *testing.T) {
	v := Vec3{X: 3, Y: 4, Z: 0}.Normalize()
	l := v.Length()
	if absF32(l-1) > 0.0001 {
		t.Errorf("Normalized length: got %f, want 1", l)
	}
	if absF32(v.X-0.6) > 0.0001 || absF32(v.Y-0.8) > 0.0001 || absF32(v.Z) > 0.0001 {
		t.Errorf("Normalized: got (%f,%f,%f), want approx (0.6,0.8,0)", v.X, v.Y, v.Z)
	}
}

func TestVec3Negate(t *testing.T) {
	v := Vec3{X: 1, Y: -2, Z: 3}.Negate()
	if v.X != -1 || v.Y != 2 || v.Z != -3 {
		t.Errorf("Negate: got (%f,%f,%f), want (-1,2,-3)", v.X, v.Y, v.Z)
	}
}

func TestQuickRejectRect(t *testing.T) {
	canvas := NewCanvas(800, 600)
	r := Rect{X: 100, Y: 100, Width: 200, Height: 200}
	if canvas.QuickRejectRect(r) {
		t.Error("Rect inside clip should not reject")
	}
}

func TestFontDescriptor(t *testing.T) {
	f := &Font{family: "Test", size: 14}
	d := f.Descriptor()
	if d.Family != "Test" || d.Size != 14 {
		t.Errorf("Descriptor: got (%s,%f), want (Test,14)", d.Family, d.Size)
	}
}

func TestGaussianWeights(t *testing.T) {
	w := GaussianWeights(2, 1.5)
	if len(w) != 5 {
		t.Errorf("Weights length: got %d, want 5", len(w))
	}
	var sum float32
	for _, v := range w {
		sum += v
	}
	if absF32(sum-1) > 0.01 {
		t.Errorf("Weights sum: got %f, want 1", sum)
	}
	// Center weight should be the largest
	if w[2] <= w[0] || w[2] <= w[4] {
		t.Errorf("Center weight should be largest: got %f with neighbors %f,%f", w[2], w[0], w[4])
	}
}

func absF32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
