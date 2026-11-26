package mobilegles

import "testing"

// PendingBatchVerts is the frame-boundary invariant the window adapter relies
// on: the batches are process-global and their vertices carry the drawing
// window's projection and clip, so a Swap with anything still queued defers
// that window's tail into whichever window flushes next. The accessor must
// therefore account for EVERY batch — missing one hides exactly the leak it
// exists to catch.
func TestPendingBatchVertsCountsEveryBatch(t *testing.T) {
	savedSDF, savedTri, savedText, savedShape := sdfBatch.verts, triBatch.verts, textBatch.verts, shapeBatch.verts
	defer func() {
		sdfBatch.verts, triBatch.verts = savedSDF, savedTri
		textBatch.verts, shapeBatch.verts = savedText, savedShape
	}()

	if n := PendingBatchVerts(); n != 0 {
		t.Fatalf("fresh batches report %d pending vertices, want 0", n)
	}

	sdfBatch.verts = append(sdfBatch.verts, 0, 1, 2) // one queue entry
	if n := PendingBatchVerts(); n != 3 {
		t.Fatalf("sdf only: got %d, want 3", n)
	}
	triBatch.verts = append(triBatch.verts, 0, 1, 2, 3, 4)  // +5
	textBatch.verts = append(textBatch.verts, 0, 1)         // +2
	shapeBatch.verts = append(shapeBatch.verts, 0, 1, 2, 3) // +4
	if n := PendingBatchVerts(); n != 14 {
		t.Fatalf("all four batches: got %d, want 14 (3+5+2+4)", n)
	}
}
