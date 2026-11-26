package mobilegles

// SDF batched primitives: one shader draws rects, rounded rects, circles and
// ovals from per-vertex attributes (see shaders/sdf_*.glsl). This replaces
// per-primitive immediate draws (~10 GL calls each) with a single accumulate +
// one draw call per frame/batch. See RENDERER_OPTIMIZATION.md O2/O3.

import (
	"fmt"
	"github.com/ebitengine/purego"
	"math"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"unsafe"
)

// Frame statistics (O1): call-count visibility for measurement-driven work.
// Counters are atomic; the GL thread writes, stats readers poll.
var (
	statPrimitives int64 // geometry primitives submitted (rects, tris, quads, lines)
	statDrawCalls  int64 // glDrawArrays/DrawElements/Draw* invocations
	statFlushes    int64 // batch flush operations
)

// FrameStats is a snapshot of renderer activity counters.
type FrameStats struct {
	Primitives int64
	DrawCalls  int64
	Flushes    int64
}

// GetFrameStats returns and resets the counters.
func GetFrameStats() FrameStats {
	return FrameStats{
		Primitives: atomic.SwapInt64(&statPrimitives, 0),
		DrawCalls:  atomic.SwapInt64(&statDrawCalls, 0),
		Flushes:    atomic.SwapInt64(&statFlushes, 0),
	}
}

func statsFrameLoopEnabled() bool { return os.Getenv("MEUI_FRAME_STATS") == "1" }

// sdfErasePass switches the next flush to erase blending
// (ZERO, ONE_MINUS_SRC_ALPHA) for the rounded-window corner pass.
var sdfErasePass bool

// EraseRoundedCorners zeroes the framebuffer alpha outside the rounded
// rect (x, y, w, h) at the given radius. topOnly extends the shape
// below the window so only the top corners round. Call after all
// content for the frame has been batched; the pending batches are
// flushed first, then the erase quad is drawn with erase blending.
func EraseRoundedCorners(x, y, w, h, radius float32, topOnly bool) {
	if !sdfBatch.ready || radius <= 0 || w <= 0 || h <= 0 {
		return
	}
	flushBatches() // content first — the erase pass must be last
	eh := h
	if topOnly {
		eh = h + radius // bottom rounding falls outside the framebuffer
	}
	SDFBatchRect(x, y, w, eh, radius, 0xFFFFFFFF, 3, 0, 0)
	sdfErasePass = true
	flushSDFBatch()
	sdfErasePass = false
}

// sdfMaxTextures bounds the distinct textures a single unified batch can
// sample (GL 2.1 guarantees at least 16 fragment texture units). When a
// new texture arrives with the table full, the batch flushes and
// restarts — so the frame costs one draw per N textures instead of one
// per text/shape alternation.
const sdfMaxTextures = 16

// MaxTexturesPerBatch reports how many distinct textures one unified batch can
// sample before it flushes and starts a new sampler-unit table.
func MaxTexturesPerBatch() int { return sdfMaxTextures }

// sdfMaxClipPlanes is the size of the batch's uniform clip-plane table. A frame
// that uses more than this across one batch flushes and starts a new table,
// exactly as the sampler-unit table does.
const sdfMaxClipPlanes = 24

type sdfBatchData struct {
	program       uint32
	vbo           uint32
	vboCap        int32
	quads         int
	posLoc        int32
	localLoc      int32
	halfLoc       int32
	radiusLoc     int32
	styleLoc      int32
	strokeWLoc    int32
	colorLoc      int32
	clipMinLoc    int32
	clipMaxLoc    int32
	clipEnLoc     int32
	uvLoc         int32
	modeLoc       int32
	texUnitLoc    int32
	aaLoc         int32
	clipRangeLoc  int32
	gradALoc      int32
	gradBLoc      int32
	clipPlaneLocs [sdfMaxClipPlanes]int32
	screenSizeLoc int32
	texLocs       [sdfMaxTextures]int32
	texIDs        [sdfMaxTextures]uint32
	texCount      int
	// clipPlanes is this batch's uniform table of clip half-planes (3 floats
	// each); clipReg* records which generation of the current clip is already
	// in it, so a set is registered once no matter how many primitives use it.
	clipPlanes   []float32
	clipRegGen   int
	clipRegBase  int
	clipRegCount int
	verts        []float32
	dirty        bool
	ready        bool
}

var sdfBatch sdfBatchData

func initSDFRenderer() {
	if glCreateShader == nil || glCreateProgram == nil {
		return
	}
	vs := glCreateShader(GL_VERTEX_SHADER)
	fs := glCreateShader(GL_FRAGMENT_SHADER)

	vsSrc := glesVertexShader(sdfSrcVS) + "\x00"
	fsSrc := glesFragmentShader(sdfSrcFS) + "\x00"

	vsB := []byte(vsSrc)
	fsB := []byte(fsSrc)
	var vsPtr, fsPtr uintptr
	if len(vsB) > 0 {
		vsPtr = uintptr(unsafe.Pointer(&vsB[0]))
	}
	if len(fsB) > 0 {
		fsPtr = uintptr(unsafe.Pointer(&fsB[0]))
	}
	vLen := int32(len(vsSrc) - 1)
	fLen := int32(len(fsSrc) - 1)

	glShaderSource(vs, 1, uintptr(unsafe.Pointer(&vsPtr)), unsafe.Pointer(&vLen))
	glCompileShader(vs)
	glShaderSource(fs, 1, uintptr(unsafe.Pointer(&fsPtr)), unsafe.Pointer(&fLen))
	glCompileShader(fs)

	sdfBatch.program = glCreateProgram()
	glAttachShader(sdfBatch.program, vs)
	glAttachShader(sdfBatch.program, fs)
	// Bind attribute locations explicitly BEFORE linking — the
	// glGetAttribLocation global misbehaves in this environment (returns 0
	// for every name), so we sidestep the query entirely.
	glBindAttribLocation(sdfBatch.program, 0, uintptr(unsafe.Pointer(&[]byte("aPos\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 1, uintptr(unsafe.Pointer(&[]byte("aLocal\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 2, uintptr(unsafe.Pointer(&[]byte("aHalf\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 3, uintptr(unsafe.Pointer(&[]byte("aRadius\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 4, uintptr(unsafe.Pointer(&[]byte("aStyle\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 5, uintptr(unsafe.Pointer(&[]byte("aStrokeW\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 6, uintptr(unsafe.Pointer(&[]byte("aColor\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 7, uintptr(unsafe.Pointer(&[]byte("aClipMin\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 8, uintptr(unsafe.Pointer(&[]byte("aClipMax\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 9, uintptr(unsafe.Pointer(&[]byte("aUv\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 10, uintptr(unsafe.Pointer(&[]byte("aMode\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 11, uintptr(unsafe.Pointer(&[]byte("aTexUnit\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 12, uintptr(unsafe.Pointer(&[]byte("aAA\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 13, uintptr(unsafe.Pointer(&[]byte("aClipRange\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 14, uintptr(unsafe.Pointer(&[]byte("aGradA\x00")[0])))
	glBindAttribLocation(sdfBatch.program, 15, uintptr(unsafe.Pointer(&[]byte("aGradB\x00")[0])))

	glLinkProgram(sdfBatch.program)

	var linkOK int32
	glGetProgramiv(sdfBatch.program, 0x8B82 /*GL_LINK_STATUS*/, uintptr(unsafe.Pointer(&linkOK)))
	if linkOK == 0 {
		fmt.Println("[SDF LINK FAILED] — check shader source")
	}
	// Locations are bound explicitly above (0..4) — the glGetAttribLocation
	// global misbehaves in this environment (returns 0 for every name), so
	// we use the bound constants directly.
	sdfBatch.posLoc = 0
	sdfBatch.localLoc = 1
	sdfBatch.halfLoc = 2
	sdfBatch.radiusLoc = 3
	sdfBatch.styleLoc = 4
	sdfBatch.strokeWLoc = 5
	sdfBatch.colorLoc = 6
	sdfBatch.clipMinLoc = 7
	sdfBatch.clipMaxLoc = 8
	sdfBatch.uvLoc = 9
	sdfBatch.modeLoc = 10
	sdfBatch.texUnitLoc = 11
	sdfBatch.aaLoc = 12
	sdfBatch.clipRangeLoc = 13
	sdfBatch.gradALoc = 14
	sdfBatch.gradBLoc = 15
	sdfBatch.screenSizeLoc = glGetUniformLocation(sdfBatch.program, uintptr(unsafe.Pointer(&[]byte("uScreenSize\x00")[0])))
	sdfBatch.clipEnLoc = glGetUniformLocation(sdfBatch.program, uintptr(unsafe.Pointer(&[]byte("uClipEnabled\x00")[0])))
	// The plane array's elements are addressed individually: glUniform3f is the
	// binding this codebase has (no glUniform3fv), and 24 calls per flush only
	// happen for frames that actually use a path clip.
	for i := 0; i < sdfMaxClipPlanes; i++ {
		name := fmt.Sprintf("uClipPlane[%d]\x00", i)
		sdfBatch.clipPlaneLocs[i] = glGetUniformLocation(sdfBatch.program, uintptr(unsafe.Pointer(&[]byte(name)[0])))
	}
	// Sampler units are fixed for the program's lifetime: uTexN ↔ unit N.
	cacheUseProgram(sdfBatch.program)
	for i := 0; i < sdfMaxTextures; i++ {
		name := fmt.Sprintf("uTex%d\x00", i)
		sdfBatch.texLocs[i] = glGetUniformLocation(sdfBatch.program, uintptr(unsafe.Pointer(&[]byte(name)[0])))
		glUniform1i(sdfBatch.texLocs[i], int32(i))
	}
	if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
		fmt.Printf("[initSDF] program=%d posLoc=%d localLoc=%d halfLoc=%d radiusLoc=%d colorLoc=%d screenSizeLoc=%d\n",
			sdfBatch.program, sdfBatch.posLoc, sdfBatch.localLoc, sdfBatch.halfLoc, sdfBatch.radiusLoc, sdfBatch.colorLoc, sdfBatch.screenSizeLoc)
	}

	glGenBuffers(1, unsafe.Pointer(&sdfBatch.vbo))
	sdfBatch.verts = make([]float32, 0, 4096)
	sdfBatch.ready = true

	if quadIBO == 0 {
		buildQuadIndexBuffer(16384)
	}

	glDeleteShader(vs)
	glDeleteShader(fs)
}

// flushSDFBatch draws all accumulated SDF quads in one call.
// The SDF batch accumulates vertices across many draws in a frame, freezing
// per-vertex state (position, clip) at APPEND time while the projection, viewport
// and uniforms are read at FLUSH time. If a batch spans a change of the surface
// size, vertices appended under one size are drawn under another: the coordinates
// are lost in the seam between the frozen and the global halves of the state.
// These record the size in force when the batch started, so a spanning batch can
// name itself instead of being inferred.
var sdfBatchStartW, sdfBatchStartH float32

func sdfStampBatchStart() {
	if sdfBatch.quads == 0 && rectRenderer != nil {
		sdfBatchStartW, sdfBatchStartH = rectRenderer.screenWidth, rectRenderer.screenHeight
	}
}

func sdfReportBatchSpan() {
	if os.Getenv("MEUI_BATCH_TRACE") == "" || sdfBatchStartW <= 0 || rectRenderer == nil {
		return
	}
	if sdfBatchStartW != rectRenderer.screenWidth || sdfBatchStartH != rectRenderer.screenHeight {
		fmt.Printf("[batch] SPANS resize %.0fx%.0f -> %.0fx%.0f quads=%d\n",
			sdfBatchStartW, sdfBatchStartH, rectRenderer.screenWidth, rectRenderer.screenHeight, sdfBatch.quads)
	}
}

func flushSDFBatch() {
	// sdfPostResize counts flushes after the surface grew past the probe window, so
	// the sample gates can skip the first frames. Earlier instruments fired on the
	// first post-resize frame and described that frame, not the failing one (P5fd).
	if rectRenderer != nil && rectRenderer.screenHeight >= 600 {
		sdfPostResize++
	}
	if !sdfBatch.dirty || len(sdfBatch.verts) == 0 {
		return
	}
	if os.Getenv("MEUI_SKIP_SDF") == "1" {
		sdfBatch.verts = sdfBatch.verts[:0]
		sdfBatch.quads = 0
		sdfBatch.dirty = false
		sdfBatch.texCount = 0
		sdfBatch.clipPlanes = sdfBatch.clipPlanes[:0]
		sdfBatch.clipRegGen = -1
		return
	}
	runtime.LockOSThread()
	// MEUI_PRIM_DEBUG: dump the batch's first primitive (pos + clip).
	if os.Getenv("MEUI_PRIM_DEBUG") == "1" && len(sdfBatch.verts) >= 17 {
		v := sdfBatch.verts
		fmt.Printf("[prim] sdf quads=%d first=(%.1f,%.1f) color=(%.2f,%.2f,%.2f,%.2f) clip=(%.0f,%.0f)-(%.0f,%.0f)\n",
			sdfBatch.quads, v[0], v[1], v[9], v[10], v[11], v[12], v[13], v[14], v[15], v[16])
	}
	// MEUI_BODY_DEBUG: the quad that actually carries the probe's body colour.
	// The trace above prints only the FIRST primitive, which is a header panel, so
	// it has never shown the body's own extents - the one object not yet measured
	// in the resize investigation. Colours are pure in the probe, so the body can
	// be found by colour: red (1,0,0).
	if os.Getenv("MEUI_BODY_DEBUG") != "" {
		const stride = 32
		v := sdfBatch.verts
		for q := 0; q+4*stride <= len(v); q += 4 * stride {
			if v[q+9] > 0.9 && v[q+10] < 0.1 && v[q+11] < 0.1 && v[q+12] > 0.5 {
				cy, hy := v[q+1], v[q+5]
				// The clip attributes of THIS quad matter: every clip check in the
				// investigation measured the first primitive (a header panel), not
				// the body's, which is the same wrong-object error as before.
				fmt.Printf("[body] pos=(%.1f,%.1f) half=(%.1f,%.1f) span_y=%.1f..%.1f (%.1f tall) clip=(%.0f,%.0f)-(%.0f,%.0f)\n",
					v[q], cy, v[q+4], hy, cy-hy, cy+hy, 2*hy, v[q+13], v[q+14], v[q+15], v[q+16])
				// MEUI_VERTEX_DUMP: every float of the body quad, not the fields I chose to
				// read. Everything compared so far was selected by me; if the shortfall is a
				// fragment-stage effect, the difference is in the twenty floats never looked at.
				if os.Getenv("MEUI_VERTEX_DUMP") != "" && sdfVertexDumps < 4 && sdfPostResize > 200 && rectRenderer != nil && rectRenderer.screenHeight >= 600 {
					sdfVertexDumps++
					fmt.Printf("[vertex] ")
					for k := 0; k < 32; k++ {
						fmt.Printf("%.3f ", v[q+k])
					}
					fmt.Printf("\n")
				}
				// aClipRange indexes the clip-plane table in a uniform: the one input never
				// read per vertex, and the only remaining route to a fragment rejection.
				fmt.Printf("[bodyclip] range=(%.0f,%.0f) planes=%d gen=%d\n",
					v[q+22], v[q+23], len(clipPlaneState), clipGen)
				// ALL FOUR corners, not just the TL one. Every geometry check in this
				// investigation read only vertex 0 (the top-left, whose aPos.y is correct),
				// so a quad whose BOTTOM vertices carry the old height - a quad 328 px tall
				// while aHalf says 544 - would have passed every check while rasterising
				// short. aHalf is a varying (SDF distance only); the corners are what the
				// rasteriser actually covers.
				fmt.Printf("[body4] TL.y=%.1f TR.y=%.1f BL.y=%.1f BR.y=%.1f | TL.x=%.1f BR.x=%.1f | half=(%.1f,%.1f)\n",
					v[q+1], v[q+stride+1], v[q+2*stride+1], v[q+3*stride+1],
					v[q+0], v[q+3*stride+0], v[q+4], v[q+5])
				// Per-corner CLIP. The clip is a varying compared against gl_FragCoord
				// (bottom-up) while it is stamped in top-down canvas space; if the four
				// corners disagree, the varying interpolates and the shader discards the
				// lower part - a vertical-only cut with geometry and value prints correct.
				fmt.Printf("[body4clip] TL=(%.0f,%.0f)-(%.0f,%.0f) TR=(%.0f,%.0f) BL=(%.0f,%.0f) BR=(%.0f,%.0f)\n",
					v[q+13], v[q+14], v[q+15], v[q+16],
					v[q+stride+13], v[q+stride+16],
					v[q+2*stride+13], v[q+2*stride+16],
					v[q+3*stride+13], v[q+3*stride+16])
				// The VBO readback for THIS quad. P5ge compared only the batch's first
				// vertex (a header panel); if the upload is faithful for vertex 0 but not
				// for the body's corners, every Go-side print in this investigation would
				// be right while the shader's clip varying is the old height - the exact
				// shape of a fragment clip that cuts the lower part.
				if os.Getenv("MEUI_VBO_BODY") != "" {
					var bb [128]float32
					if readBackBatch(q*4, 512, unsafe.Pointer(&bb[0])) {
						fmt.Printf("[bodyvbo] array clip=(%.0f,%.0f)-(%.0f,%.0f) TL.y=%.1f BL.y=%.1f | buffer clip=(%.0f,%.0f)-(%.0f,%.0f) TL.y=%.1f BL.y=%.1f\n",
							v[q+13], v[q+14], v[q+15], v[q+16], v[q+1], v[q+2*stride+1],
							bb[13], bb[14], bb[15], bb[16], bb[1], bb[2*stride+1])
					} else {
						fmt.Printf("[bodyvbo] unavailable\n")
					}
				}
			}
		}
	}
	glDisable(GL_SCISSOR_TEST) // clip is per-vertex
	// The SDF batch carries its clip PER VERTEX, so neither the scissor nor the
	// stencil may interfere with it. The scissor was already disabled here; the
	// stencil was not - and path fills leave non-zero stencil marks wherever the
	// cover pass and the final clear did not reach (the cover only resets the
	// path's bbox, and the clear is scissored), so a batch drawn while the test is
	// still enabled is stencil-rejected in exactly those regions. That cuts content
	// at whatever rectangle the stale marks occupy - a previous window height, for
	// instance - while the geometry, the clips and every measured value are correct.
	glDisable(0x0B90 /*GL_STENCIL_TEST*/)
	// The gradient LUT atlas must be resident before any unit pointing at it is
	// sampled. Uploading here keeps all GL work at flush time.
	uploadGradientAtlasPixels()
	cacheUseProgram(sdfBatch.program)
	stride := int32(128) // 32 floats: pos2 local2 half2 radius1 style1 strokeW1 color4 clipMin2 clipMax2 uv2 mode1 texUnit1 aa1 clipRange2 gradA4 gradB4
	cacheBindVBO(sdfBatch.vbo)
	uploadBatchData(sdfBatch.vbo, &sdfBatch.vboCap, sdfBatch.verts)

	syncAttribMask(0xFFFF) // attribs 0..15: pos..clipMax, uv, mode, texUnit, aa, clipRange, gradA, gradB
	glVertexAttribPointer(uint32(sdfBatch.posLoc), 2, GL_FLOAT, 0, stride, unsafe.Pointer(nil))
	glVertexAttribPointer(uint32(sdfBatch.localLoc), 2, GL_FLOAT, 0, stride, ptrOffset(8))
	glVertexAttribPointer(uint32(sdfBatch.halfLoc), 2, GL_FLOAT, 0, stride, ptrOffset(16))
	glVertexAttribPointer(uint32(sdfBatch.radiusLoc), 1, GL_FLOAT, 0, stride, ptrOffset(24))
	glVertexAttribPointer(uint32(sdfBatch.styleLoc), 1, GL_FLOAT, 0, stride, ptrOffset(28))
	glVertexAttribPointer(uint32(sdfBatch.strokeWLoc), 1, GL_FLOAT, 0, stride, ptrOffset(32))
	glVertexAttribPointer(uint32(sdfBatch.colorLoc), 4, GL_FLOAT, 0, stride, ptrOffset(36))
	glVertexAttribPointer(uint32(sdfBatch.clipMinLoc), 2, GL_FLOAT, 0, stride, ptrOffset(52))
	glVertexAttribPointer(uint32(sdfBatch.clipMaxLoc), 2, GL_FLOAT, 0, stride, ptrOffset(60))
	glVertexAttribPointer(uint32(sdfBatch.uvLoc), 2, GL_FLOAT, 0, stride, ptrOffset(68))
	glVertexAttribPointer(uint32(sdfBatch.modeLoc), 1, GL_FLOAT, 0, stride, ptrOffset(76))
	glVertexAttribPointer(uint32(sdfBatch.texUnitLoc), 1, GL_FLOAT, 0, stride, ptrOffset(80))
	glVertexAttribPointer(uint32(sdfBatch.aaLoc), 1, GL_FLOAT, 0, stride, ptrOffset(84))
	glVertexAttribPointer(uint32(sdfBatch.clipRangeLoc), 2, GL_FLOAT, 0, stride, ptrOffset(88))
	glVertexAttribPointer(uint32(sdfBatch.gradALoc), 4, GL_FLOAT, 0, stride, ptrOffset(96))
	glVertexAttribPointer(uint32(sdfBatch.gradBLoc), 4, GL_FLOAT, 0, stride, ptrOffset(112))

	glUniform2f(sdfBatch.screenSizeLoc, rectRenderer.screenWidth, rectRenderer.screenHeight)
	// Per-vertex clip is gated on uClipEnabled: without this the unified
	// batch ignored every canvas clip (ScrollPane viewports, TextArea
	// scroll areas and list rows leaked past their bounds).
	if sdfBatch.clipEnLoc >= 0 {
		// MEUI_NO_CLIP_SDF=1: disable the fragment clip for THIS pipeline only.
		// MEUI_NO_CLIP also stops the shape and text pipelines clipping, so it
		// cannot say which of the three rejects a fragment; this can.
		if os.Getenv("MEUI_NO_CLIP_SDF") != "" {
			glUniform1f(sdfBatch.clipEnLoc, 0)
		} else if os.Getenv("MEUI_CLIP_DEBUG") != "" {
			// 2.0: the shader paints the clip values it holds instead of discarding.
			glUniform1f(sdfBatch.clipEnLoc, 2)
		} else if os.Getenv("MEUI_FRAGCOORD_DEBUG") != "" {
			// 3.0: the shader paints gl_FragCoord for every fragment.
			glUniform1f(sdfBatch.clipEnLoc, 3)
		} else if os.Getenv("MEUI_CLIP_VERDICT") != "" {
			// 4.0: the shader paints the clip test's verdict (red = reject).
			glUniform1f(sdfBatch.clipEnLoc, 4)
		} else {
			glUniform1f(sdfBatch.clipEnLoc, clipEnabledUniform())
		}
	}
	// Upload this batch's plane table. Unused slots are never read (the shader
	// stops at the per-vertex count), but zero them so a stale array from a
	// previous batch cannot leak into a slot a count reaches by accident.
	for i := 0; i < sdfMaxClipPlanes; i++ {
		if sdfBatch.clipPlaneLocs[i] < 0 {
			continue
		}
		var nx, ny, d float32
		if i*3+2 < len(sdfBatch.clipPlanes) {
			nx, ny, d = sdfBatch.clipPlanes[i*3], sdfBatch.clipPlanes[i*3+1], sdfBatch.clipPlanes[i*3+2]
		} else {
			// A no-op plane: 0*x + 0*y + 1 >= 0 keeps everything.
			d = 1
		}
		glUniform3f(sdfBatch.clipPlaneLocs[i], nx, ny, d)
	}

	// Bind this batch's distinct textures to their assigned units.
	for i := 0; i < sdfBatch.texCount; i++ {
		glActiveTexture(0x84C0 /*GL_TEXTURE0*/ + uint32(i))
		glBindTexture(GL_TEXTURE_2D, sdfBatch.texIDs[i])
	}
	if sdfBatch.texCount > 0 {
		glActiveTexture(0x84C0 /*GL_TEXTURE0*/)
	}

	if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
		// GL_COLOR_WRITEMASK: whether a correct draw has any effect at all. The
		// path-fill stencil route sets glColorMask(0,0,0,0) for its counting pass
		// and restores it for the cover; content landing between those writes
		// nothing - geometry and position correct, result invisible. No measurement
		// in this investigation has ever asked whether fragments are written.
		var mask [4]int32
		glGetIntegerv(0x0C23 /*GL_COLOR_WRITEMASK*/, uintptr(unsafe.Pointer(&mask[0])))
		var box [4]int32
		glGetIntegerv(0x0C10 /*GL_SCISSOR_BOX*/, uintptr(unsafe.Pointer(&box[0])))
		var stencilTest, scissorTest int32
		glGetIntegerv(0x0B90 /*GL_STENCIL_TEST*/, uintptr(unsafe.Pointer(&stencilTest)))
		glGetIntegerv(0x0C11 /*GL_SCISSOR_TEST*/, uintptr(unsafe.Pointer(&scissorTest)))
		if box[2] == 0 && box[3] == 0 {
			buf := make([]byte, 3000)
			n := runtime.Stack(buf, false)
			lines := strings.Split(string(buf[:n]), "\n")
			caller := ""
			for _, l := range lines {
				if strings.Contains(l, "meui-mobile/meui") {
					caller = strings.TrimSpace(l)
					break
				}
			}
			fmt.Printf("[SDF flush ZERO SCISSOR] via %s\n", caller)
		} else {
			// screen= is the value the SDF projection uniform is fed from, which is
			// the one half of the pixel->NDC mapping not covered by the scissor,
			// the viewport or the clips. If it holds an older size while the layout
			// and viewport are current, geometry maps into a smaller space,
			// magnifies and is cut at the viewport edge - the stuck-run signature.
			fmt.Printf("[SDF flush] scissor=(%d,%d %dx%d) screen=%.0fx%.0f mask=%d,%d,%d,%d\n",
				box[0], box[1], box[2], box[3], rectRenderer.screenWidth, rectRenderer.screenHeight,
				mask[0], mask[1], mask[2], mask[3])
		}
	}

	glDisable(GL_DEPTH_TEST)
	// Unified premultiplied compositing (shapes premultiply in-shader,
	// glyphs/images are premultiplied at the source). The erase pass
	// uses (ZERO, ONE_MINUS_SRC_ALPHA): dst *= (1 - alpha) clears the
	// rounded-outside region to transparent.
	if sdfErasePass {
		glBlendFunc(GL_ZERO, GL_ONE_MINUS_SRC_ALPHA)
	} else {
		// Premultiplied normal; an active blend mode overrides the factors.
		applyStraightBlend(GL_ONE, GL_ONE_MINUS_SRC_ALPHA)
	}
	cacheBindElemIBO(quadIBO)
	// Draw in index-buffer-sized chunks. The shared quadIBO holds quadIBOMaxQuads
	// quads; a larger batch makes glDrawElements read past it, and the result is
	// undefined geometry whose extent depends on how many quads the frame happened
	// to contain - i.e. state-dependent corruption rather than a clean failure. The
	// text batch already guards against this (backend.go); the SDF batch, which is
	// by far the largest, did not.
	if os.Getenv("MEUI_QUADS_TRACE") != "" && sdfBatch.quads > sdfQuadsMax {
		sdfQuadsMax = sdfBatch.quads
		fmt.Printf("[quads] max=%d iboMax=%d exceeds=%v\n", sdfQuadsMax, quadIBOMaxQuads, int(sdfQuadsMax) > int(quadIBOMaxQuads))
	}
	maxQ := int(quadIBOMaxQuads)
	if maxQ <= 0 || int(sdfBatch.quads) <= maxQ {
		if os.Getenv("MEUI_BATCH_CHECK") != "" {
			want := int(sdfBatch.quads) * 4
			have := len(sdfBatch.verts) / 32
			if want != have {
				fmt.Printf("[batchcheck] quads*4=%d vertices_in_array=%d MISMATCH (draw reads %d vertices of data)\n",
					want, have, want)
			}
		}
		// The draw call indexes vertex 4i..4i+3 for quad i, so the vertex count implied by
		// the quad counter must equal the number of vertices actually present. If it does
		// not, glDrawElements reads past the uploaded range and the GPU draws whatever the
		// buffer happens to hold - stale or garbage geometry - while every trace of the
		// Go-side array still reports correct values (P5fc's unverified link, checked here
		// without needing a new GL binding).
		sdfReportBatchSpan()
		glDrawElements(GL_TRIANGLES, uint32(sdfBatch.quads*6), GL_UNSIGNED_SHORT, unsafe.Pointer(nil))
		// MEUI_PEEK: read the framebuffer IMMEDIATELY after this draw, for the first
		// few frames only. Against the end-of-frame dump this decides the last two
		// shapes: the draw itself writes short (peek equals the dump), or something
		// erases between the draw and the readback (peek full, dump short).
		if os.Getenv("MEUI_PEEK") != "" && sdfPeekCount < 6 && sdfPostResize > 200 && rectRenderer != nil && rectRenderer.screenHeight >= 600 {
			sdfPeekCount++
			px := ReadScreenPixels(int32(rectRenderer.screenWidth), int32(rectRenderer.screenHeight))
			red := 0
			for _, v := range px {
				if v&0x00FFFFFF == 0x00FF0000 {
					red++
				}
			}
			fmt.Printf("[peek] after draw #%d screen=%.0fx%.0f red_pixels=%d\n",
				sdfPeekCount, rectRenderer.screenWidth, rectRenderer.screenHeight, red)
			// What the VBO actually holds, against what the array holds: if they differ, the GPU
			// is drawing something the Go side never sees - the last unverified link.
			var back [32]float32
			if readBackBatch(0, 128, unsafe.Pointer(&back[0])) {
				fmt.Printf("[vboback] array half.y=%.1f clipMax=(%.0f,%.0f) | buffer half.y=%.1f clipMax=(%.0f,%.0f)\n",
					sdfBatch.verts[5], sdfBatch.verts[15], sdfBatch.verts[16], back[5], back[15], back[16])
			} else {
				fmt.Printf("[vboback] unavailable (glGetBufferSubData not bound)\n")
			}
			// The scissor and viewport in force AT THE DRAW. The peek only fires post-resize
			// (screenHeight >= 600), so unlike P5ew its sample cannot be consumed by pre-resize
			// frames. The scissor is the last mechanism that can cut a correct write short.
			var sc, scOn, vp2 [4]int32
			glGetIntegerv(0x0C10 /*GL_SCISSOR_BOX*/, uintptr(unsafe.Pointer(&sc[0])))
			glGetIntegerv(0x0C11 /*GL_SCISSOR_TEST*/, uintptr(unsafe.Pointer(&scOn[0])))
			glGetIntegerv(0x0BA2 /*GL_VIEWPORT*/, uintptr(unsafe.Pointer(&vp2[0])))
			fmt.Printf("[peeksc] scissor=%d,%d %dx%d test=%d viewport=%dx%d+{%d,%d}\n",
				sc[0], sc[1], sc[2], sc[3], scOn[0], vp2[2], vp2[3], vp2[0], vp2[1])
		}
	} else {
		for done := 0; done < int(sdfBatch.quads); done += maxQ {
			n := int(sdfBatch.quads) - done
			if n > maxQ {
				n = maxQ
			}
			// 2 bytes per GL_UNSIGNED_SHORT index, 6 indices per quad.
			off := uintptr(done) * 6 * 2
			glDrawElements(GL_TRIANGLES, uint32(n*6), GL_UNSIGNED_SHORT, ptrOffsetBytes(off))
		}
	}
	// Restore straight-alpha blending for subsequent immediate draws
	// (gradients, path cover, meshes) — they assume it.
	if sdfErasePass {
		glBlendFunc(GL_ONE, GL_ONE_MINUS_SRC_ALPHA) // back to unified premultiplied
	} else {
		glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
	}
	glDisable(GL_SCISSOR_TEST) // clip is per-vertex: leave scissor off
	glErr := glGetError()
	runtime.UnlockOSThread()

	if glErr != 0 && os.Getenv("MEUI_FLUSH_DEBUG") != "" {
		fmt.Printf("[SDF flush GL ERROR] %x quads=%d\n", glErr, sdfBatch.quads)
	}
	atomic.AddInt64(&statDrawCalls, 1)
	atomic.AddInt64(&statFlushes, 1)
	if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
		fmt.Printf("[SDF flush] drew quads=%d verts=%d texs=%d\n", sdfBatch.quads, len(sdfBatch.verts)/21, sdfBatch.texCount)
	}
	sdfBatch.verts = sdfBatch.verts[:0]
	sdfBatch.quads = 0
	sdfBatch.dirty = false
	sdfBatch.texCount = 0
	// The plane table lives in uniforms, which are per-draw-call state: a new
	// draw call starts with an empty table and re-registers what it needs.
	sdfBatch.clipPlanes = sdfBatch.clipPlanes[:0]
	sdfBatch.clipRegGen = -1
}

// SDFBatchRect appends an axis-aligned rect / rounded rect / circle to the
// batch. x,y,w,h are SCREEN pixels; radius 0 = sharp box, radius = min(w,h)/2
// = circle/oval. Call on the GL thread.
func SDFBatchRect(x, y, w, h, radius float32, color Color, style int32, strokeW, angle float32) {
	if !sdfBatch.ready {
		return
	}
	if shapeBatch.dirty || triBatch.dirty || textBatch.dirty {
		flushBatches() // strict draw-order: earlier pending types go first
	}
	// NaN/huge guard: one bad rect rasterizes as full-screen garbage.
	if w != w || h != h || x != x || y != y || w > 4096 || h > 4096 {
		if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
			fmt.Printf("[SDF BAD RECT] x=%v y=%v w=%v h=%v\n", x, y, w, h)
		}
		return
	}
	// Pixel-align: integer origin and size give symmetric AA coverage
	// (fractional circle centers read as dirty double edges — same
	// reasoning as the text pipeline's snapped glyph origins).
	//
	// Only when AXIS-ALIGNED. A rotated rect's origin is not on the pixel grid
	// in any meaningful sense — the centre is what stays put under rotation, and
	// rounding a top-left corner would shift the shape.
	if angle == 0 {
		x = float32(math.Floor(float64(x) + 0.5))
		y = float32(math.Floor(float64(y) + 0.5))
		w = float32(math.Floor(float64(w) + 0.5))
		h = float32(math.Floor(float64(h) + 0.5))
	}
	hw := w * 0.5
	hh := h * 0.5
	if hw <= 0 || hh <= 0 {
		return
	}
	if radius > hw {
		radius = hw
	}
	if radius > hh {
		radius = hh
	}
	if radius < 0 {
		radius = 0
	}
	color = scaleAlpha(color)
	r := float32((color>>16)&0xFF) / 255
	g := float32((color>>8)&0xFF) / 255
	b := float32(color&0xFF) / 255
	a := float32((color>>24)&0xFF) / 255

	// One quad: TL, TR, BL, BR — indexed via the shared quad IBO. Local
	// coords relative to center. The QUAD is padded 1px beyond the shape
	// bounds so the AA ramp and the stroke's outer half have fragments
	// to write (the analytic edge sits exactly on the unpadded bounds;
	// an unpadded quad clips that outer half, cutting the ramp at the
	// cardinal points of a circle). aLocal spans the padded extent —
	// TRUE device-pixel offsets from the center — so the SDF evaluates
	// with d > 0 in the padded region. vHalf stays the true box.
	lx, ly := x+hw, y+hh
	pad := float32(1)
	corners := [4][2]float32{
		{-hw - pad, -hh - pad}, {hw + pad, -hh - pad},
		{-hw - pad, hh + pad}, {hw + pad, hh + pad},
	}
	// ROTATION: the quad's corners are rotated about the centre, but aLocal is
	// NOT. The shader evaluates its rounded-box distance from vLocal/vHalf — the
	// local frame — so an unrotated local frame with rotated positions is exactly
	// a rotated rounded rect, with the analytic edge ramp and the stroke intact.
	// (Tessellating instead would have lost both.)
	ca, sa := float32(1), float32(0)
	if angle != 0 {
		ca, sa = cosf(angle), sinf(angle)
	}
	c0, c1, c2, c3 := curClipQuad()
	cr0, cr1 := sdfClipRange()
	gv := sdfGradientVertex()
	for _, q := range corners {
		px, py := q[0], q[1]
		if angle != 0 {
			px, py = q[0]*ca-q[1]*sa, q[0]*sa+q[1]*ca
		}
		sdfBatch.verts = append(sdfBatch.verts,
			lx+px, ly+py, // aPos (padded, rotated about the centre)
			q[0], q[1], // aLocal (true offset at this fragment corner)
			hw, hh, // aHalf (true box)
			radius,                  // aRadius
			float32(style), strokeW, // aStyle, aStrokeW
			r, g, b, a, // aColor
			c0, c1, c2, c3, // aClipMin, aClipMax
			0, 0, // aUv
			0,        // aMode (SDF shape)
			gv.unit,  // aTexUnit (the gradient LUT for a gradient fill)
			curAA(),  // aAA
			cr0, cr1, // aClipRange
			gv.a[0], gv.a[1], gv.a[2], gv.a[3], // aGradA
			gv.b[0], gv.b[1], gv.b[2], gv.b[3], // aGradB
		)
	}
	atomic.AddInt64(&statPrimitives, 1)
	if os.Getenv("MEUI_FLUSH_DEBUG") != "" && w < 500 {
		fmt.Printf("[SDF rect] (%.0f,%.0f %.0fx%.0f r=%.0f color=%08x) frame-quad#=%d\n", x, y, w, h, radius, color, sdfBatch.quads)
	}
	sdfStampBatchStart()
	sdfBatch.quads++
	sdfBatch.dirty = true
	if sdfBatch.quads >= int(quadIBOMaxQuads) {
		flushSDFBatch() // index buffer full
	}
}

// sdfBatchTexUnit resolves a texture ID to its sampler unit inside the
// current batch, assigning the next free unit on first use. When all
// units are taken the batch flushes and the table restarts — the cost
// is one draw per sdfMaxTextures distinct textures, not one per
// primitive-type alternation.
func sdfBatchTexUnit(texID uint32) float32 {
	for i := 0; i < sdfBatch.texCount; i++ {
		if sdfBatch.texIDs[i] == texID {
			return float32(i)
		}
	}
	if sdfBatch.texCount >= sdfMaxTextures {
		flushSDFBatch()
	}
	sdfBatch.texIDs[sdfBatch.texCount] = texID
	sdfBatch.texCount++
	return float32(sdfBatch.texCount - 1)
}

// sdfClipRange registers the current path clip in this batch's plane table and
// returns the (base, count) pair the per-vertex aClipRange attribute carries.
// A set is registered once per batch: clipGen only changes when the effective
// clip changes, so the many primitives that share a clip share one range.
func sdfClipRange() (float32, float32) {
	if len(clipPlaneState) == 0 {
		return 0, 0
	}
	if sdfBatch.clipRegGen == clipGen {
		return float32(sdfBatch.clipRegBase), float32(sdfBatch.clipRegCount)
	}
	if len(sdfBatch.clipPlanes)/3+len(clipPlaneState) > sdfMaxClipPlanes {
		// Table full: flush, which empties it and restarts registration. This
		// is the same overflow policy SDFBatchTexUnit uses for sampler units.
		flushSDFBatch()
	}
	base := len(sdfBatch.clipPlanes) / 3
	for _, pl := range clipPlaneState {
		sdfBatch.clipPlanes = append(sdfBatch.clipPlanes, pl.NX, pl.NY, pl.D)
	}
	sdfBatch.clipRegGen = clipGen
	sdfBatch.clipRegBase = base
	sdfBatch.clipRegCount = len(clipPlaneState)
	return float32(base), float32(len(clipPlaneState))
}

// SDFBatchTriangles appends barycentric-AA triangles to the unified batch
// (mode 3). verts holds 9 floats per vertex: pos2 bary3 color4. Each
// triangle is padded to a 4-vertex quad so the shared quad index buffer
// applies: (v0,v1,v2) + degenerate (v1,v3,v2) with v3=v2 — the original
// triangle rasterizes unchanged and the degenerate half covers nothing.
func SDFBatchTriangles(verts []float32) {
	if !sdfBatch.ready || len(verts) < 27 {
		return
	}
	c0, c1, c2, c3 := curClipQuad()
	cr0, cr1 := sdfClipRange()
	gv := sdfGradientVertex()
	for i := 0; i+27 <= len(verts); i += 27 {
		for v := 0; v < 4; v++ {
			o := i + v*9
			if v == 3 {
				o = i + 2*9 // duplicate the third vertex
			}
			sdfBatch.verts = append(sdfBatch.verts,
				verts[o+0], verts[o+1], // aPos
				verts[o+2], verts[o+3], // aLocal = (bary0, bary1)
				verts[o+4], 0, // aHalf = (bary2, unused)
				0,    // aRadius
				0, 0, // aStyle, aStrokeW
				verts[o+5], verts[o+6], verts[o+7],
				scaleAlphaFloat(verts[o+8]), // aColor
				c0, c1, c2, c3,              // aClipMin, aClipMax
				0, 0, // aUv
				3,        // aMode (barycentric triangle)
				gv.unit,  // aTexUnit (the gradient LUT for a gradient path)
				curAA(),  // aAA
				cr0, cr1, // aClipRange
				gv.a[0], gv.a[1], gv.a[2], gv.a[3], // aGradA
				gv.b[0], gv.b[1], gv.b[2], gv.b[3], // aGradB
			)
		}
		atomic.AddInt64(&statPrimitives, 1)
		sdfStampBatchStart()
		sdfBatch.quads++
		if sdfBatch.quads >= int(quadIBOMaxQuads) {
			flushSDFBatch()
		}
	}
	sdfBatch.dirty = true
}

// SDFBatchTexturedQuads appends glyph (mode 1) or image (mode 2) quads
// to the unified SDF batch. verts holds 4 indexed vertices per quad,
// 8 floats each: pos2 uv2 color4. Blending is unified to premultiplied
// (ONE, ONE_MINUS_SRC_ALPHA) — the glyph/image shader paths output
// premultiplied color, and the SDF shape path premultiplies in the
// fragment shader, so one batch can interleave shapes, text and images
// in exact draw order with a single draw call.
func SDFBatchTexturedQuads(verts []float32, texID uint32, isImage bool) {
	if !sdfBatch.ready || len(verts) == 0 {
		return
	}
	mode := float32(1)
	if isImage {
		mode = 2
	}
	c0, c1, c2, c3 := curClipQuad()
	cr0, cr1 := sdfClipRange()
	for i := 0; i+32 <= len(verts); i += 32 {
		unit := sdfBatchTexUnit(texID)
		for v := 0; v < 4; v++ {
			o := i + v*8
			sdfBatch.verts = append(sdfBatch.verts,
				verts[o+0], verts[o+1], // aPos
				0, 0, // aLocal
				0, 0, // aHalf
				0,    // aRadius
				0, 0, // aStyle, aStrokeW
				verts[o+4], verts[o+5], verts[o+6],
				scaleAlphaFloat(verts[o+7]), // aColor
				c0, c1, c2, c3,              // aClipMin, aClipMax
				verts[o+2], verts[o+3], // aUv
				mode,     // aMode
				unit,     // aTexUnit
				1,        // aAA (glyph/image modes sample a texture; no edge ramp)
				cr0, cr1, // aClipRange
				0, 0, 0, 0, // aGradA (glyph/image modes take no gradient)
				0, 0, 0, 0, // aGradB
			)
		}
		atomic.AddInt64(&statPrimitives, 1)
		sdfStampBatchStart()
		sdfBatch.quads++
		if sdfBatch.quads >= int(quadIBOMaxQuads) {
			flushSDFBatch() // index buffer full
		}
	}
	sdfBatch.dirty = true
}

// sdfQuadsMax records the largest batch seen, for MEUI_QUADS_TRACE.
var sdfQuadsMax int

// sdfPeekCount bounds the MEUI_PEEK readbacks (glReadPixels is a sync point).
var sdfPeekCount int

// sdfVertexDumps bounds the MEUI_VERTEX_DUMP output.
var sdfVertexDumps int

// sdfPostResize counts post-resize flushes so the sample gates skip the first frames.
var sdfPostResize int

// readBackBatch reads GPU buffer contents back - the only way to see what the VBO actually
// holds after the upload, which is the one link never verified in the resize investigation.
// Bound lazily and guarded: an unbound symbol must not panic (registering a missing name is
// what killed an earlier instrument), so the lookup is checked first.
var glGetBufferSubDataRead func(target uint32, offset, size int, data unsafe.Pointer)

func readBackBatch(off, size int, dst unsafe.Pointer) bool {
	if glGetBufferSubDataRead == nil {
		for _, lib := range []uintptr{libGLES, libGL} {
			if lib == 0 {
				continue
			}
			if sym, err := purego.Dlsym(lib, "glGetBufferSubData"); err == nil && sym != 0 {
				purego.RegisterFunc(&glGetBufferSubDataRead, sym)
				break
			}
		}
	}
	if glGetBufferSubDataRead == nil {
		return false
	}
	glGetBufferSubDataRead(GL_ARRAY_BUFFER, off, size, dst)
	return true
}
