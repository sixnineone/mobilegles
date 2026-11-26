package mobilegles

import "testing"

// TestWheelPosPrefersTrackedPointer: SDL's wheel event mouse_y is offset
// by the window's top frame on X11 (and can be stale right after a
// move), so the dispatch position comes from the tracked client-relative
// pointer when one has been seen.
func TestWheelPosPrefersTrackedPointer(t *testing.T) {
	h := &sdlWin{}
	if x, y := h.wheelPos(10, 20); x != 10 || y != 20 {
		t.Errorf("no tracked pointer: got (%v,%v), want the event coords (10,20)", x, y)
	}
	h.mouseX, h.mouseY, h.haveMouse = 100, 200, true
	if x, y := h.wheelPos(10, 20); x != 100 || y != 200 {
		t.Errorf("tracked pointer: got (%v,%v), want (100,200)", x, y)
	}
	if x, y := (*sdlWin)(nil).wheelPos(7, 8); x != 7 || y != 8 {
		t.Errorf("nil window: got (%v,%v), want (7,8)", x, y)
	}
}
