package mobilegles

// Backend API — exported functions that platform backends (android, ios) use
// to delegate rendering to the shared GL infrastructure.

import "sync/atomic"

func FlushBatches() { flushBatches() }

// PendingBatchVerts reports how many vertices are still queued in ANY of the
// process-global batches (SDF shapes/text/images, triangles, immediate shapes)
// and therefore not yet submitted to GL.
//
// The batches are process-global and their vertices are stamped with the
// CURRENT window's projection and clip, so a non-zero count at a frame
// boundary means work that belongs to the window that just drew: presenting
// the buffer with a non-empty batch defers that work into whatever frame
// (usually another window) flushes next. It must be 0 before every Swap.
func PendingBatchVerts() int {
	return len(sdfBatch.verts) + len(triBatch.verts) + len(textBatch.verts) + len(shapeBatch.verts)
}
func SetScissor(x, y, w, h int32) { setScissorTracked(x, y, w, h) }
func ClearScissor() {
	if glScissorGlobal != nil {
		glScissorGlobal(0, 0, int32(screenWidth), int32(screenHeight))
	}
}
func GLDepthTest(enabled bool) {
	if enabled {
		glEnable(GL_DEPTH_TEST)
	} else {
		glDisable(GL_DEPTH_TEST)
	}
}
func GLDepthWrite(enabled bool) {
	if enabled {
		glDepthMask(1)
	} else {
		glDepthMask(0)
	}
}
func GLClearDepth() { glClear(GL_DEPTH_BUFFER_BIT) }

// Drawing helpers for platform backends.
func DrawRectGL(x, y, w, h float32, color Color, angle float32) { drawRectGL(x, y, w, h, color, angle) }
func DrawCircleGL(cx, cy, radius float32, color Color)          { drawCircleGL(cx, cy, radius, color) }
func DrawOvalGL(x, y, w, h float32, color Color)                { drawOvalGL(x, y, w, h, color) }
func DrawLineGL(x1, y1, x2, y2 float32, color Color, strokeW float32) {
	drawLineGL(x1, y1, x2, y2, color, strokeW)
}

// DrawTrianglesBatch appends triangles to the frame batch (for AndroidRenderer).
func DrawTrianglesBatch(verts []float32, color Color) {
	flushSDFBatch()
	if textBatch.dirty {
		flushBatches()
	}
	atomic.AddInt64(&statPrimitives, 1)
	triBatch.verts = append(triBatch.verts, verts...)
	triBatch.dirty = true
}

// DrawTexturedQuadsBatch appends textured quads to the frame batch.
func DrawTexturedQuadsBatch(verts []float32, texID uint32, color Color, isImage bool) {
	flushSDFBatch()
	if triBatch.dirty {
		flushBatches()
	}
	flushKey := !textBatch.dirty || textBatch.texID != texID || textBatch.isImage != isImage
	if flushKey {
		flushBatches()
	}
	atomic.AddInt64(&statPrimitives, 1)
	textBatch.texID = texID
	textBatch.verts = append(textBatch.verts, verts...)
	textBatch.dirty = true
	textBatch.isImage = isImage
}
