package mobilegles

// GL state cache + batch upload helpers (RENDERER_OPTIMIZATION.md O7/O8).
// The GL API is immediate: consecutive batch flushes previously re-issued
// identical program binds, buffer binds and attribute-enable calls. This
// cache deduplicates them. All program/buffer/attrib changes in the renderer
// must go through these helpers (or invalidate appropriately).

import (
	"unsafe"
)

// GL constant used by the indexed batch path.
const GL_UNSIGNED_SHORT = 0x1403

// cachedProgram lives in renderer.go (pre-existing state cache); the rest
// are new (O7).
var (
	cachedVBO     uint32 = 0xFFFFFFFF
	cachedAttribs uint32 // bit i = vertex attrib array i enabled
	cachedIBO     uint32 = 0xFFFFFFFF
)

func cacheUseProgram(p uint32) {
	if cachedProgram != p {
		glUseProgram(p)
		cachedProgram = p
	}
}

func cacheBindVBO(b uint32) {
	if cachedVBO != b {
		glBindBuffer(GL_ARRAY_BUFFER, b)
		cachedVBO = b
	}
}

func cacheBindElemIBO(b uint32) {
	if cachedIBO != b {
		glBindBuffer(GL_ELEMENT_ARRAY_BUFFER, b)
		cachedIBO = b
	}
}

func cacheEnableAttrib(loc uint32) {
	if loc >= 32 {
		glEnableVertexAttribArray(loc)
		return
	}
	mask := uint32(1) << loc
	if cachedAttribs&mask == 0 {
		glEnableVertexAttribArray(uint32(loc))
		cachedAttribs |= mask
	}
}

// syncAttribMask enables exactly the attribute arrays in mask and disables
// the rest (locations 0..7). Programs with different attribute sets share
// the global attrib-enable state — without this, leftover enabled arrays
// from a previous batch corrupt or fault later draws.
func syncAttribMask(mask uint32) {
	for i := uint32(0); i < 16; i++ {
		m := uint32(1) << i
		want := mask&m != 0
		have := cachedAttribs&m != 0
		if want && !have {
			glEnableVertexAttribArray(i)
			cachedAttribs |= m
		} else if !want && have {
			glDisableVertexAttribArray(i)
			cachedAttribs &^= m
		}
	}
}

// uploadBatchData streams batch vertex data into a VBO. Grows the storage
// when the batch outgrows it. Call with the VBO already bound via
// cacheBindVBO.
//
// The buffer is shared by every window (one process-global name, contexts in
// one share group), so its storage also has to survive the previous window's
// draw still being in flight on the GPU. Writing straight into that storage
// with glBufferSubData is only synchronised against the context that queued
// the draw — across contexts it is a race, and the GPU then reads vertices
// that are half this window's data and half the next window's. The symptom is
// partial frames: individual cells and rect edges collapse (a quad whose
// vertices mix two batches has no area) and the clear colour shows through
// where they should have been. It needs several windows to appear at all,
// and it hits the FIRST N-2 of N, because those are the frames whose vertex
// data sits in the buffer longest before the GPU consumes it.
//
// The fix is to orphan the storage before writing: glBufferData(NULL) hands
// back fresh storage, and the driver is then free to keep old storage alive
// until the draws referencing it have completed.
func uploadBatchData(vbo uint32, cap *int32, data []float32) {
	need := int32(len(data)) * 4
	if need <= 0 {
		return
	}
	if *cap < need {
		newCap := *cap
		if newCap < 1<<20 {
			newCap = 1 << 20
		}
		for newCap < need {
			newCap *= 2
		}
		*cap = newCap
	}
	// EXPERIMENT (P5fg): one exact-size upload instead of orphan-to-capacity plus a
	// sub-data write. The array traced by every instrument is correct and the draw's
	// range matches it, so the only unverified link is what the buffer actually holds;
	// this changes that path in the smallest way possible. Revert if the rate does not
	// move, since capacity reuse is the more common idiom.
	glBufferData(GL_ARRAY_BUFFER, need, unsafe.Pointer(&data[0]), GL_STREAM_DRAW)
}

// Shared quad index buffer (O5): pattern [0,1,2, 1,3,2] per quad for
// uint16 indices. Covers maxQuadIndices/6 quads per draw.
var quadIBO uint32
var quadIBOMaxQuads int32

const maxQuadIndices = 65535 // uint16 index limit

func buildQuadIndexBuffer(maxQuads int32) {
	quadIBOMaxQuads = maxQuads
	glGenBuffers(1, unsafe.Pointer(&quadIBO))
	cacheBindElemIBO(quadIBO)
	indices := make([]uint16, maxQuads*6)
	for i := int32(0); i < maxQuads; i++ {
		b := i * 4
		o := i * 6
		indices[o+0] = uint16(b)
		indices[o+1] = uint16(b + 1)
		indices[o+2] = uint16(b + 2)
		indices[o+3] = uint16(b + 1)
		indices[o+4] = uint16(b + 3)
		indices[o+5] = uint16(b + 2)
	}
	glBufferData(GL_ELEMENT_ARRAY_BUFFER, int32(len(indices))*2, unsafe.Pointer(&indices[0]), GL_STATIC_DRAW)
}

// InvalidateGLStateCache resets every cached GL binding after a context
// switch. GL state is PER-CONTEXT: the caches describe the previous
// context — a flush after MakeCurrent(otherCtx) would skip re-binding
// program/VBO/IBO/attribs and draw with this context's defaults
// (null attrib pointers → driver SIGSEGV).
func InvalidateGLStateCache() {
	cachedProgram = 0xFFFFFFFF
	cachedVBO = 0xFFFFFFFF
	cachedIBO = 0xFFFFFFFF
	cachedAttribs = 0
	// glUseProgram(0) etc. is NOT issued: the fresh context is already
	// at defaults, matching the reset cache.
}
