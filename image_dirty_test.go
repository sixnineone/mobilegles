package mobilegles

import "testing"

// Writing into an image's pixel buffer has to invalidate the GPU texture, and
// the re-upload has to happen at draw time (ensureImageTexture), because that
// is where the GL context is current.
func TestImageDirtyLifecycle(t *testing.T) {
	img := NewImage(make([]uint32, 4), 2, 2)
	if img.Dirty() {
		t.Fatal("fresh image reported dirty")
	}

	img.MarkPixelsDirty()
	if !img.Dirty() {
		t.Fatal("MarkPixelsDirty did not mark the image dirty")
	}

	// No GL context in a unit test: ensureImageTexture must decline rather
	// than panic, and must leave the image dirty so a real draw retries.
	if ensureImageTexture(img) {
		t.Error("ensureImageTexture claimed success without a GL context")
	}
	if !img.Dirty() {
		t.Error("failed upload cleared the dirty flag; the write would be lost")
	}
}

func TestImageDirtyOnNilReceiver(t *testing.T) {
	var img *Image
	img.MarkPixelsDirty() // must not panic
	if img.Dirty() {
		t.Error("nil image reported dirty")
	}
}

func TestUploadWithoutGLIsNoOp(t *testing.T) {
	img := NewImage([]uint32{0xFFFFFFFF}, 1, 1)
	img.Upload() // must not panic without a GL context
	if got := img.TextureID(); got != 0 {
		t.Errorf("TextureID = %d, want 0 without a GL context", got)
	}
	if img.Width() != 1 || img.Height() != 1 {
		t.Errorf("size = %dx%d, want 1x1", img.Width(), img.Height())
	}
}
