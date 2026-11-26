package mobilegles

import "unsafe"

// SDL3_GetWindowSafeArea is registered with the rest of the SDL3 entry points.
var SDL3_GetWindowSafeArea func(win unsafe.Pointer, rect unsafe.Pointer) bool

// SafeArea reports the region of the window that is NOT covered by system UI — a
// notch, a status bar, a gesture bar, a rounded corner. It is in window
// coordinates.
//
// ok is false when SDL cannot say (no window, or a platform that does not
// report one), and the caller must then treat the WHOLE window as safe. That is
// the right default: insetting on a guess moves content on every device that did
// not need it.
func (w *Window) SafeArea() (x, y, width, height int32, ok bool) {
	h := w.osHandle()
	if h == nil || h.win == nil || SDL3_GetWindowSafeArea == nil {
		return 0, 0, 0, 0, false
	}
	var rect [4]int32
	if !SDL3_GetWindowSafeArea(h.win, unsafe.Pointer(&rect[0])) {
		return 0, 0, 0, 0, false
	}
	return rect[0], rect[1], rect[2], rect[3], true
}
