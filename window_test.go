package mobilegles

import "testing"

func TestSetIconValidatesInput(t *testing.T) {
	w := &Window{}
	if w.SetIcon(nil, 0, 0) {
		t.Fatal("SetIcon accepted empty pixels")
	}
	if w.SetIcon([]uint32{0}, 4, 4) {
		t.Fatal("SetIcon accepted a short buffer")
	}
}
