package mobilegles

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"unsafe"
)

// Point represents a 2D point (compatible with toolbox/geom.Point)
type Point struct {
	X, Y float32
}

// NewPoint creates a new Point
func NewPoint(x, y float32) Point {
	return Point{X: x, Y: y}
}

// Rect represents a rectangle (compatible with toolbox/geom.Rect)
type Rect struct {
	X, Y          float32
	Width, Height float32
}

// NewRect creates a new Rect
func NewRect(x, y, w, h float32) Rect {
	return Rect{X: x, Y: y, Width: w, Height: h}
}

// Intersects returns true if this rectangle overlaps with other.
func (r Rect) Intersects(other Rect) bool {
	return r.X < other.X+other.Width &&
		r.X+r.Width > other.X &&
		r.Y < other.Y+other.Height &&
		r.Y+r.Height > other.Y
}

// Contains returns true if the point (x, y) is inside the rectangle.
func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// Size represents width and height
type Size struct {
	Width, Height float32
}

// NewSize creates a new Size
func NewSize(w, h float32) Size {
	return Size{Width: w, Height: h}
}

// Matrix is a 3x3 affine transformation matrix (row-major)
// [sx, kx, tx, ky, sy, ty, 0, 0, 1]
type Matrix [9]float32

// NewMatrix creates an identity matrix
func NewMatrix() Matrix {
	return Matrix{1, 0, 0, 0, 1, 0, 0, 0, 1}
}

// Translate creates a translation matrix
func TranslateMatrix(tx, ty float32) Matrix {
	return Matrix{1, 0, tx, 0, 1, ty, 0, 0, 1}
}

// ScaleMatrix creates a scale matrix
func ScaleMatrix(sx, sy float32) Matrix {
	return Matrix{sx, 0, 0, 0, sy, 0, 0, 0, 1}
}

// RotateMatrix creates a rotation matrix (degrees)
func RotateMatrix(degrees float32) Matrix {
	rad := float32(degrees) * 3.14159265 / 180
	s, c := float32(math.Sin(float64(rad))), float32(math.Cos(float64(rad)))
	return Matrix{c, s, 0, -s, c, 0, 0, 0, 1}
}

// SkewMatrix creates a skew matrix (degrees for both axes)
func SkewMatrix(kx, ky float32) Matrix {
	radX := kx * 3.14159265 / 180
	radY := ky * 3.14159265 / 180
	return Matrix{1, tanf(radY), 0, tanf(radX), 1, 0, 0, 0, 1}
}

// Multiply returns this * other
func (m Matrix) Multiply(other Matrix) Matrix {
	var result Matrix
	for row := 0; row < 3; row++ {
		for col := 0; col < 3; col++ {
			result[row*3+col] = m[row*3]*other[col] +
				m[row*3+1]*other[3+col] +
				m[row*3+2]*other[6+col]
		}
	}
	return result
}

// Transform transforms a point by this matrix
func (m Matrix) Transform(pt Point) Point {
	return Point{
		X: m[0]*pt.X + m[1]*pt.Y + m[2],
		Y: m[3]*pt.X + m[4]*pt.Y + m[5],
	}
}

// Concat returns the concatenation of this matrix with another (this * other)
func (m Matrix) Concat(other Matrix) Matrix {
	return m.Multiply(other)
}

// PaintStyle defines fill/stroke style
type PaintStyle int

const (
	Fill PaintStyle = iota
	Stroke
	FillStroke
)

// GradientType defines gradient shape
type GradientType int

const (
	GradientNone GradientType = iota
	GradientLinear
	GradientRadial
	GradientConic
)

// MaxGradientStops caps a gradient's stops. The ramp is rasterised into a
// 256-wide LUT row, so more stops than that would not be resolvable anyway.
const MaxGradientStops = 16

// GradientStop is one colour stop at Offset (0..1) along the gradient.
type GradientStop struct {
	Offset float32
	Color  Color
}

// Gradient describes a multi-stop colour gradient for fills and strokes.
//
//	Linear: ramp runs from Start to End.
//	Radial: ramp runs from Center out to Radius.
//	Conic:  ramp sweeps around Center, starting at StartAngle degrees.
//
// Stops are kept sorted by Offset so sampleGradientStops can walk them.
type Gradient struct {
	Type       GradientType
	Stops      [MaxGradientStops]GradientStop
	StopCount  int
	Start, End Point
	Center     Point
	Radius     float32
	StartAngle float32
}

// SetStops replaces the gradient's stops, sorted by offset so the ramp is
// well defined. Fewer than two stops leaves it a flat fill of the first.
func (g *Gradient) SetStops(stops []GradientStop) {
	g.StopCount = 0
	for _, s := range stops {
		if g.StopCount >= MaxGradientStops {
			break
		}
		g.Stops[g.StopCount] = s
		g.StopCount++
	}
	// Insertion sort: a handful of stops, and it keeps the sort allocation-free.
	for i := 1; i < g.StopCount; i++ {
		for j := i; j > 0 && g.Stops[j-1].Offset > g.Stops[j].Offset; j-- {
			g.Stops[j-1], g.Stops[j] = g.Stops[j], g.Stops[j-1]
		}
	}
}

// StrokeCap defines line end style
type StrokeCap int

const (
	StrokeCapButt StrokeCap = iota
	StrokeCapRound
	StrokeCapSquare
)

// StrokeJoin defines line join style
type StrokeJoin int

const (
	StrokeJoinMiter StrokeJoin = iota
	StrokeJoinRound
	StrokeJoinBevel
)

// BlendMode defines blend modes
type BlendMode int

const (
	BlendModeNormal BlendMode = iota
	BlendModeMultiply
	BlendModeScreen
	BlendModeOverlay
	BlendModeDarken
	BlendModeLighten
	BlendModeColorDodge
	BlendModeColorBurn
	BlendModeHardLight
	BlendModeSoftLight
	BlendModeDifference
	BlendModeExclusion
	BlendModeHue
	BlendModeSaturation
	BlendModeColor
	BlendModeLuminosity
)

// PathVerb represents a path command
type PathVerb int

const (
	PathMove PathVerb = iota
	PathLine
	PathQuad
	PathCubic
	PathClose
)

// PathPt represents a point in a path along with verb
type PathPt struct {
	Verb PathVerb
	Pt   Point
}

// Path represents a path with points and verbs
type Path struct {
	points []PathPt
	closed bool
	bounds Rect
}

// NewPath creates a new empty path
func NewPath() *Path {
	return &Path{
		points: make([]PathPt, 0, 64),
		closed: false,
	}
}

// MoveTo moves to a point (starts new contour)
func (p *Path) MoveTo(pt Point) {
	p.points = append(p.points, PathPt{Verb: PathMove, Pt: pt})
}

// LineTo adds a line to the given point
func (p *Path) LineTo(pt Point) {
	p.points = append(p.points, PathPt{Verb: PathLine, Pt: pt})
}

// QuadTo adds a quadratic bezier curve
func (p *Path) QuadTo(ctrl, end Point) {
	p.points = append(p.points, PathPt{Verb: PathQuad, Pt: ctrl})
	p.points = append(p.points, PathPt{Verb: PathQuad, Pt: end})
}

// Close closes the current contour
func (p *Path) Close() {
	if len(p.points) == 0 {
		return
	}
	if p.points[len(p.points)-1].Verb != PathClose {
		p.points = append(p.points, PathPt{Verb: PathClose})
	}
}

// Reset clears the path
func (p *Path) Reset() {
	p.points = p.points[:0]
	p.closed = false
}

// Bounds returns the path bounds
func (p *Path) Bounds() Rect {
	if len(p.points) == 0 {
		return Rect{}
	}
	minX, minY := float32(0), float32(0)
	maxX, maxY := float32(0), float32(0)
	first := true
	for _, pt := range p.points {
		if pt.Verb == PathClose {
			continue
		}
		if first {
			minX, minY = pt.Pt.X, pt.Pt.Y
			maxX, maxY = minX, minY
			first = false
			continue
		}
		if pt.Pt.X < minX {
			minX = pt.Pt.X
		}
		if pt.Pt.Y < minY {
			minY = pt.Pt.Y
		}
		if pt.Pt.X > maxX {
			maxX = pt.Pt.X
		}
		if pt.Pt.Y > maxY {
			maxY = pt.Pt.Y
		}
	}
	if first {
		return Rect{}
	}
	return Rect{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

// Vec3 is a 3D vector/point
type Vec3 struct {
	X, Y, Z float32
}

// Mat4 is a 4x4 column-major matrix for 3D transforms.
// Layout: [col0, col1, col2, col3] where each col is 4 float32s.
// Index: row r, col c = m[c*4+r]
type Mat4 [16]float32

// Identity returns a 4x4 identity matrix
func Identity() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// Perspective creates a perspective projection matrix (fov in degrees)
func Perspective(fov, aspect, near, far float32) Mat4 {
	f := float32(1.0 / math.Tan(float64(fov*math.Pi/360)))
	nf := float32(1.0 / (near - far))
	return Mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, (far + near) * nf, -1,
		0, 0, 2 * far * near * nf, 0,
	}
}

// Ortho creates an orthographic projection matrix
func Ortho(left, right, bottom, top, near, far float32) Mat4 {
	rl := float32(1.0 / (right - left))
	tb := float32(1.0 / (top - bottom))
	fn := float32(1.0 / (far - near))
	return Mat4{
		2 * rl, 0, 0, 0,
		0, 2 * tb, 0, 0,
		0, 0, -2 * fn, 0,
		-(right + left) * rl, -(top + bottom) * tb, -(far + near) * fn, 1,
	}
}

// LookAt creates a view matrix (camera looking at target)
func LookAt(eye, center, up Vec3) Mat4 {
	fwd := center.Sub(eye).Normalize()
	right := fwd.Cross(up.Normalize())
	u := right.Cross(fwd)
	return Mat4{
		right.X, u.X, -fwd.X, 0,
		right.Y, u.Y, -fwd.Y, 0,
		right.Z, u.Z, -fwd.Z, 0,
		-right.Dot(eye), -u.Dot(eye), fwd.Dot(eye), 1,
	}
}

// Multiply returns the matrix product a * b
func (m Mat4) Multiply(b Mat4) Mat4 {
	var r Mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			sum := float32(0)
			for k := 0; k < 4; k++ {
				sum += m[k*4+row] * b[col*4+k]
			}
			r[col*4+row] = sum
		}
	}
	return r
}

// Translate returns m * translation(tx, ty, tz)
func (m Mat4) Translate(v Vec3) Mat4 {
	t := Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		v.X, v.Y, v.Z, 1,
	}
	return m.Multiply(t)
}

// Scale returns m * scale(sx, sy, sz)
func (m Mat4) Scale(v Vec3) Mat4 {
	s := Mat4{
		v.X, 0, 0, 0,
		0, v.Y, 0, 0,
		0, 0, v.Z, 0,
		0, 0, 0, 1,
	}
	return m.Multiply(s)
}

// Rotate returns m * rotation(angle, axis) — angle in degrees, axis normalized
func (m Mat4) Rotate(angle float32, axis Vec3) Mat4 {
	rad := angle * math.Pi / 180
	c := float32(math.Cos(float64(rad)))
	s := float32(math.Sin(float64(rad)))
	t := 1 - c
	x, y, z := axis.X, axis.Y, axis.Z
	r := Mat4{
		t*x*x + c, t*x*y + s*z, t*x*z - s*y, 0,
		t*x*y - s*z, t*y*y + c, t*y*z + s*x, 0,
		t*x*z + s*y, t*y*z - s*x, t*z*z + c, 0,
		0, 0, 0, 1,
	}
	return m.Multiply(r)
}

// Transform applies the matrix to a Vec3 (treats as position, w=1)
func (m Mat4) Transform(v Vec3) Vec3 {
	x := m[0]*v.X + m[4]*v.Y + m[8]*v.Z + m[12]
	y := m[1]*v.X + m[5]*v.Y + m[9]*v.Z + m[13]
	z := m[2]*v.X + m[6]*v.Y + m[10]*v.Z + m[14]
	return Vec3{X: x, Y: y, Z: z}
}

// Inverse returns the inverse of a 4x4 matrix using Gauss-Jordan elimination
func (m Mat4) Inverse() Mat4 {
	// Augmented matrix [m | I]
	a := [16]float32(m)
	var inv [16]float32
	for i := 0; i < 16; i++ {
		if i%5 == 0 {
			inv[i] = 1
		}
	}
	for col := 0; col < 4; col++ {
		// Find pivot (largest value in this column)
		pivot := col
		for row := col + 1; row < 4; row++ {
			if abs32(a[col*4+row]) > abs32(a[col*4+pivot]) {
				pivot = row
			}
		}
		if a[col*4+pivot] == 0 {
			return Identity()
		}
		// Swap rows
		if pivot != col {
			for c := 0; c < 4; c++ {
				a[c*4+col], a[c*4+pivot] = a[c*4+pivot], a[c*4+col]
				inv[c*4+col], inv[c*4+pivot] = inv[c*4+pivot], inv[c*4+col]
			}
		}
		// Scale pivot row
		scale := a[col*4+col]
		for c := 0; c < 4; c++ {
			a[c*4+col] /= scale
			inv[c*4+col] /= scale
		}
		// Eliminate other rows
		for row := 0; row < 4; row++ {
			if row == col {
				continue
			}
			factor := a[col*4+row]
			for c := 0; c < 4; c++ {
				a[c*4+row] -= factor * a[c*4+col]
				inv[c*4+row] -= factor * inv[c*4+col]
			}
		}
	}
	return Mat4(inv)
}

// Vec3 operations

func (v Vec3) Sub(o Vec3) Vec3      { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Add(o Vec3) Vec3      { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Scale(s float32) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }
func (v Vec3) Dot(o Vec3) float32   { return v.X*o.X + v.Y*o.Y + v.Z*o.Z }
func (v Vec3) Cross(o Vec3) Vec3 {
	return Vec3{v.Y*o.Z - v.Z*o.Y, v.Z*o.X - v.X*o.Z, v.X*o.Y - v.Y*o.X}
}
func (v Vec3) Length() float32 { return sqrtf(v.X*v.X + v.Y*v.Y + v.Z*v.Z) }
func (v Vec3) Normalize() Vec3 {
	l := v.Length()
	if l == 0 {
		return v
	}
	return Vec3{v.X / l, v.Y / l, v.Z / l}
}
func (v Vec3) Negate() Vec3 { return Vec3{-v.X, -v.Y, -v.Z} }

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// tanf computes tangent using Taylor series approximation
func tanf(x float32) float32 {
	if x > 1.57 {
		x = 1.57
	}
	if x < -1.57 {
		x = -1.57
	}
	s := x - x*x*x/3 + x*x*x*x*x*2/15
	c := 1 - x*x/2 + x*x*x*x/24
	if c == 0 {
		return 0
	}
	return s / c
}

// Image represents an image with texture
type Image struct {
	width, height int32
	texture       uint32
	pixels        []uint32
	// premultiplied reports whether pixels[] color channels are already
	// multiplied by alpha. Readbacks from offscreen FBOs (RenderOffscreen)
	// produce premultiplied content; decoded PNGs are straight alpha and
	// are premultiplied at upload time.
	premultiplied bool
	// dirty marks pixels[] as changed since the last Upload, so the next
	// draw re-uploads the texture. Writing into an image's pixel buffer
	// (meui's SetPixel/PutImage) sets it; the GL work stays at draw time,
	// where the context is current.
	dirty bool
}

// SetPixels replaces the pixel buffer, for a caller that re-renders into the same
// surface. The buffer must be the same size as the image: this image keeps one
// texture, and a different extent would be uploaded under the old shape.
func (img *Image) SetPixels(pix []uint32) {
	if img == nil || int32(len(pix)) != img.width*img.height {
		return
	}
	img.pixels = pix
	img.premultiplied = true // the seam's buffers are premultiplied by construction
	img.dirty = true
}

// MarkPixelsDirty flags the CPU pixel buffer as modified so the next draw
// re-uploads the texture. Safe to call off the GL thread.
func (img *Image) MarkPixelsDirty() {
	if img != nil {
		img.dirty = true
	}
}

// Dirty reports whether the texture is behind the pixel buffer.
func (img *Image) Dirty() bool {
	return img != nil && img.dirty
}

// NewImage creates a new image from raw ARGB data. Pixels are assumed
// premultiplied (the case for framebuffer readbacks); use
// NewImageFromPNG for straight-alpha sources.
func NewImage(rgba []uint32, w, h int32) *Image {
	return &Image{width: w, height: h, pixels: rgba, premultiplied: true}
}

// LogicalSize returns the image's dimensions as a Size. Used by meui
// for layout.
func (i *Image) LogicalSize() Size {
	if i == nil {
		return NewSize(0, 0)
	}
	return NewSize(float32(i.width), float32(i.height))
}

// NewImageFromPNG decodes a PNG from bytes
func NewImageFromPNG(data []byte) (*Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	w := int32(bounds.Dx())
	h := int32(bounds.Dy())

	pixels := make([]uint32, w*h)
	for y := 0; y < int(h); y++ {
		for x := 0; x < int(w); x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			pixels[y*int(w)+x] = uint32(a>>8)<<24 | uint32(r>>8)<<16 | uint32(g>>8)<<8 | uint32(b>>8)
		}
	}

	return &Image{width: w, height: h, pixels: pixels}, nil
}

// Upload creates or updates the GPU texture for this image.
// Must be called from the GL thread (inside LockOSThread). With no GL context
// loaded (headless unit tests) it is a no-op, leaving texture == 0 so callers
// fall back instead of panicking.
func (img *Image) Upload() {
	if len(img.pixels) == 0 {
		return
	}
	if glGenTextures == nil || glBindTexture == nil || glTexImage2D == nil {
		return
	}
	if img.texture == 0 {
		glGenTextures(1, unsafe.Pointer(&img.texture))
	}
	glBindTexture(GL_TEXTURE_2D, img.texture)
	// Convert ARGB pixels to RGBA bytes for GL. Straight-alpha sources
	// (decoded PNGs) are premultiplied here so every texture the text/
	// image batch samples is premultiplied — the batch composites with
	// (ONE, ONE_MINUS_SRC_ALPHA).
	if !img.premultiplied {
		for i, px := range img.pixels {
			a := (px >> 24) & 0xFF
			r := ((px >> 16) & 0xFF) * a / 255
			g := ((px >> 8) & 0xFF) * a / 255
			b := (px & 0xFF) * a / 255
			img.pixels[i] = a<<24 | r<<16 | g<<8 | b
		}
		img.premultiplied = true
	}
	rgba := make([]byte, img.width*img.height*4)
	for i, px := range img.pixels {
		rgba[i*4+0] = byte((px >> 16) & 0xFF) // R
		rgba[i*4+1] = byte((px >> 8) & 0xFF)  // G
		rgba[i*4+2] = byte(px & 0xFF)         // B
		rgba[i*4+3] = byte((px >> 24) & 0xFF) // A
	}
	glTexImage2D(GL_TEXTURE_2D, 0, int32(0x1908), img.width, img.height, 0, 0x1908, GL_UNSIGNED_BYTE, unsafe.Pointer(&rgba[0]))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, int32(GL_CLAMP_TO_EDGE))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, int32(GL_CLAMP_TO_EDGE))
	img.dirty = false
}

// TextureID returns the GPU texture handle (0 = not uploaded).
func (img *Image) TextureID() uint32 {
	return img.texture
}

// Width returns image width
func (img *Image) Width() int32 { return img.width }

// Height returns image height
func (img *Image) Height() int32 { return img.height }

// Pixels returns the decoded pixel data
func (img *Image) Pixels() []uint32 { return img.pixels }

// ToPNG encodes the image as a PNG and returns the bytes.
// compressionLevel: 0 = default, 1-9 = higher compression.
func (img *Image) ToPNG(compressionLevel int) ([]byte, error) {
	if img == nil || len(img.pixels) == 0 {
		return nil, fmt.Errorf("mobilegles: empty image")
	}
	w := int(img.width)
	h := int(img.height)
	rgba := make([]uint8, w*h*4)
	for i, px := range img.pixels {
		rgba[i*4+0] = uint8((px >> 16) & 0xFF)
		rgba[i*4+1] = uint8((px >> 8) & 0xFF)
		rgba[i*4+2] = uint8(px & 0xFF)
		rgba[i*4+3] = uint8((px >> 24) & 0xFF)
	}
	imgRGBA := image.NewRGBA(image.Rect(0, 0, w, h))
	imgRGBA.Pix = rgba

	var buf bytes.Buffer
	enc := &png.Encoder{}
	if compressionLevel > 0 && compressionLevel <= 9 {
		enc.CompressionLevel = png.CompressionLevel(compressionLevel)
	}
	if err := enc.Encode(&buf, imgRGBA); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
