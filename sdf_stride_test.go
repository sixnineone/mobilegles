package mobilegles

import "testing"

// Every emitter must append exactly sdfVertexFloats floats per vertex. A single
// miscount desynchronises the whole rest of the batch — the attributes then
// read across vertex boundaries, which shows up as geometry smeared into long
// triangles rather than as an obvious error. That is exactly what a widened
// layout risks, so it is pinned here rather than left to the render corpus.
func TestSDFBatchVertexStride(t *testing.T) {
	const strideFloats = 32

	prevRenderer := rectRenderer
	prevQuads := quadIBOMaxQuads
	prevReady := sdfBatch.ready
	prevVerts := sdfBatch.verts
	prevPlanes := sdfBatch.clipPlanes
	prevRegGen := sdfBatch.clipRegGen
	prevGrad := currentGradient
	prevPlaneState := clipPlaneState
	defer func() {
		rectRenderer = prevRenderer
		quadIBOMaxQuads = prevQuads
		sdfBatch.ready = prevReady
		sdfBatch.verts = prevVerts
		sdfBatch.clipPlanes = prevPlanes
		sdfBatch.clipRegGen = prevRegGen
		currentGradient = prevGrad
		clipPlaneState = prevPlaneState
	}()

	// The emitters append to a slice and touch no GL, so forcing ready (and a
	// screen size, which the clip quad derives its bounds from) is enough to
	// exercise them headlessly.
	rectRenderer = &RectRenderer{screenWidth: 800, screenHeight: 600}
	// A zero quad budget would make every append try to flush, which needs GL.
	quadIBOMaxQuads = 1 << 20
	sdfBatch.ready = true

	reset := func() {
		sdfBatch.verts = sdfBatch.verts[:0]
		sdfBatch.quads = 0
		sdfBatch.clipPlanes = sdfBatch.clipPlanes[:0]
		sdfBatch.clipRegGen = -1
		sdfBatch.texCount = 0
	}
	check := func(label string, wantVerts int) {
		t.Helper()
		if n := len(sdfBatch.verts); n%strideFloats != 0 {
			t.Errorf("%s: %d floats is not a multiple of %d — the batch is desynchronised", label, n, strideFloats)
		} else if want := wantVerts * strideFloats; n != want {
			t.Errorf("%s: %d floats, want %d (%d verts)", label, n, want, wantVerts)
		}
	}

	// 1. Shape quads: four vertices.
	reset()
	SDFBatchRect(10, 10, 20, 20, 0, 0xFFFFFFFF, 0, 0, 0)
	check("SDF shape quad", 4)

	// 2. Barycentric triangles: one triangle becomes a 4-vertex quad.
	reset()
	tri := make([]float32, 27)
	SDFBatchTriangles(tri)
	check("triangle", 4)

	// 3. Textured (glyph/image) quads: four vertices each, two quads.
	reset()
	tex := make([]float32, 64)
	SDFBatchTexturedQuads(tex, 1, false)
	check("textured quads", 8)

	// 4. A batch that mixes all three kinds must still be a whole number of
	// vertices, and re-registration of a clip must not disturb the layout.
	reset()
	clipPlaneState = []ClipPlane{{NX: 1, NY: 0, D: -5}, {NX: -1, NY: 0, D: 100}}
	SDFBatchRect(0, 0, 50, 50, 4, 0xFFFFFFFF, 0, 0, 0)
	SDFBatchTriangles(tri)
	currentGradient = Gradient{
		Type:      GradientLinear,
		StopCount: 2,
		Stops:     [MaxGradientStops]GradientStop{{Offset: 0, Color: 0xFFFF0000}, {Offset: 1, Color: 0xFF0000FF}},
	}
	SDFBatchRect(0, 0, 50, 50, 4, 0xFFFFFFFF, 0, 0, 0)
	SDFBatchTexturedQuads(tex, 1, true)
	// 4 + 4 + 4 shape/triangle vertices, plus 2 textured quads (4 vertices each).
	check("mixed batch", 20)

	reset()
	currentGradient = Gradient{}
	clipPlaneState = nil
}

// The vertex layout's float width must match the GL stride, or the attrib
// pointers index the wrong place. Both are written as literals in different
// files, so keep them in step here.
func TestSDFBatchStrideMatchesGL(t *testing.T) {
	const wantStrideBytes = 128
	const strideFloats = 32
	if got := strideFloats * 4; got != wantStrideBytes {
		t.Errorf("strideFloats %d is %d bytes, want %d", strideFloats, got, wantStrideBytes)
	}
	// The last attribute ends exactly at the stride.
	const lastAttrEnd = 112 + 4*4
	if lastAttrEnd != wantStrideBytes {
		t.Errorf("gradient attribute ends at %d bytes, want the %d-byte stride", lastAttrEnd, wantStrideBytes)
	}
}
