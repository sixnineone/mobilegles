package mobilegles

import "testing"

// ---- Unit Tests ----

func TestMatrixIdentity(t *testing.T) {
	m := NewMatrix()
	p := Point{X: 10, Y: 20}
	result := m.Transform(p)
	if result.X != 10 || result.Y != 20 {
		t.Errorf("Identity transform failed: got (%f,%f), want (10,20)", result.X, result.Y)
	}
}

func TestMatrixTranslate(t *testing.T) {
	m := TranslateMatrix(5, 10)
	p := Point{X: 0, Y: 0}
	result := m.Transform(p)
	if result.X != 5 || result.Y != 10 {
		t.Errorf("Translate failed: got (%f,%f), want (5,10)", result.X, result.Y)
	}
}

func TestMatrixScale(t *testing.T) {
	m := ScaleMatrix(2, 3)
	p := Point{X: 10, Y: 20}
	result := m.Transform(p)
	if result.X != 20 || result.Y != 60 {
		t.Errorf("Scale failed: got (%f,%f), want (20,60)", result.X, result.Y)
	}
}

func TestPathQuadTo(t *testing.T) {
	p := NewPath()
	p.MoveTo(Point{X: 0, Y: 0})
	p.QuadTo(Point{X: 5, Y: 5}, Point{X: 10, Y: 0})
	p.Close()
	if len(p.points) != 4 {
		t.Errorf("Path should have 4 points, got %d", len(p.points))
	}
	if p.points[3].Verb != PathClose {
		t.Errorf("Last point should be PathClose")
	}
}

func TestVec3Operations(t *testing.T) {
	a := Vec3{X: 1, Y: 2, Z: 3}
	b := Vec3{X: 4, Y: 5, Z: 6}
	if s := a.Dot(b); s != 32 {
		t.Errorf("Dot 1*4+2*5+3*6 = %f, want 32", s)
	}
	c := a.Cross(b)
	if c.X != -3 || c.Y != 6 || c.Z != -3 {
		t.Errorf("Cross (1,2,3)×(4,5,6) = (%f,%f,%f), want (-3,6,-3)", c.X, c.Y, c.Z)
	}
}

func TestMat4Perspective(t *testing.T) {
	m := Perspective(90, 1, 0.1, 100)
	if m[0] == 0 || m[5] == 0 {
		t.Errorf("Perspective matrix should have non-zero elements at [0] and [5], got %f %f", m[0], m[5])
	}
}

func TestMat4Ortho(t *testing.T) {
	m := Ortho(0, 100, 100, 0, -1, 1)
	v := m.Transform(Vec3{X: 50, Y: 50, Z: 0})
	if v.X != 0 || v.Y != 0 {
		t.Errorf("Ortho center should map to (0,0), got (%f,%f)", v.X, v.Y)
	}
}

func TestMat4Inverse(t *testing.T) {
	m := TranslateMatrix(10, 20).Multiply(RotateMatrix(45))
	// Convert 3x3 to 4x4
	var m4 Mat4
	m4[0] = m[0]
	m4[1] = m[1]
	m4[4] = m[3]
	m4[5] = m[4]
	m4[12] = m[2]
	m4[13] = m[5]
	m4[10] = 1
	m4[15] = 1
	inv := m4.Inverse()
	prod := m4.Multiply(inv)
	if prod[0] < 0.99 || prod[0] > 1.01 {
		t.Errorf("M * M^-1 should have 1 at [0], got %f", prod[0])
	}
}

// ---- Benchmarks ----

func BenchmarkMatrixMultiply(b *testing.B) {
	a := Matrix{1, 2, 3, 4, 5, 6, 7, 8, 9}
	c := Matrix{9, 8, 7, 6, 5, 4, 3, 2, 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Multiply(c)
	}
}

func BenchmarkMatrixTransform(b *testing.B) {
	m := NewMatrix()
	p := Point{X: 10, Y: 20}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Transform(p)
	}
}

func BenchmarkNewPaint(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewPaint()
	}
}

func BenchmarkColorRGBA(b *testing.B) {
	for i := 0; i < b.N; i++ {
		RGBA(255, 128, 64, 32)
	}
}

func BenchmarkGaussianWeight(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GaussianWeights(4, 1.5)
	}
}

func BenchmarkImageNewFromPNG(b *testing.B) {
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x62, 0x00, 0x00, 0x00, 0x02,
		0x00, 0x01, 0xE6, 0x00, 0x0A, 0x00, 0x00, 0x00,
		0x0D, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
		0x44, 0xAE, 0x42, 0x60, 0x82,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewImageFromPNG(pngData)
	}
}

func BenchmarkVec3Operations(b *testing.B) {
	a := Vec3{X: 1, Y: 2, Z: 3}
	c := Vec3{X: 4, Y: 5, Z: 6}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Dot(c)
		a.Cross(c)
		a.Add(c).Normalize()
	}
}

func BenchmarkMat4Multiply(b *testing.B) {
	a := Identity()
	a = a.Translate(Vec3{X: 1, Y: 2, Z: 3})
	a = a.Rotate(45, Vec3{X: 0, Y: 1, Z: 0})
	c := Perspective(60, 1.6, 0.1, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Multiply(c)
	}
}

func BenchmarkMat4Inverse(b *testing.B) {
	m := Identity()
	m = m.Translate(Vec3{X: 10, Y: 20, Z: 30})
	m = m.Rotate(30, Vec3{X: 1, Y: 1, Z: 0})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Inverse()
	}
}

func BenchmarkNewPath(b *testing.B) {
	for i := 0; i < b.N; i++ {
		p := NewPath()
		p.MoveTo(Point{X: 0, Y: 0})
		p.LineTo(Point{X: 10, Y: 0})
		p.LineTo(Point{X: 10, Y: 10})
		p.LineTo(Point{X: 0, Y: 10})
		p.Close()
	}
}
