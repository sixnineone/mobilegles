package mobilegles

import "unsafe"

// Text input control, per window.
//
// Why this exists at all: SDL_StartTextInput is called once when a window is
// created, which is enough on the desktop (it is what makes the platform deliver
// text events at all). On mobile it is not, because the SOFT KEYBOARD is raised
// by the OS only while text input is active — so leaving it on permanently gives
// a keyboard that never goes away, and turning it on only at startup gives one
// that never appears when a field is actually focused.
//
// And there is a second half that is easy to miss: Android refuses to serve the
// IME unless the input area is a non-degenerate rect. SDL implements its Android
// text input with a hidden "dummy edit" view sized from SDL_SetTextInputArea, and
// with no area that view is 0x0 — the InputMethodManager logs
//
//	Ignoring showSoftInput() as view=SDLDummyEdit{... 0,0-0,0} is not served.
//
// and the keyboard silently never shows. So the area is not a nicety for
// avoiding the keyboard covering the field; it is what makes the keyboard appear
// at all.

// SDL3_SetTextInputArea is registered with the rest of the SDL3 entry points.
var SDL3_SetTextInputArea func(win unsafe.Pointer, rect unsafe.Pointer, cursor int32) bool

// SetTextInput turns text input on or off for this window and says where the
// text is being edited.
//
// area is in window coordinates. A zero area is passed through: on desktop the
// area is advisory, and on mobile the caller is responsible for having a real
// one, which is why the seam's callers compute it from the focused widget.
func (w *Window) SetTextInput(active bool, x, y, width, height int32) {
	h := w.osHandle()
	if h == nil || h.win == nil {
		return
	}
	if !active {
		if SDL3_StopTextInput != nil {
			SDL3_StopTextInput(h.win)
		}
		return
	}
	// The area must be set BEFORE starting: SDL's Android backend sizes the dummy
	// edit from it inside showTextInput.
	if SDL3_SetTextInputArea != nil {
		rect := [4]int32{x, y, width, height}
		SDL3_SetTextInputArea(h.win, unsafe.Pointer(&rect[0]), 0)
	}
	if SDL3_StartTextInput != nil {
		SDL3_StartTextInput(h.win)
	}
}

// TextInputArea reports the window's current text-input area as SDL has it, for
// diagnostics. It returns false when SDL cannot be asked.
func (w *Window) TextInputArea() (x, y, width, height int32, ok bool) {
	h := w.osHandle()
	if h == nil || h.win == nil || SDL3_GetTextInputArea == nil {
		return 0, 0, 0, 0, false
	}
	var rect [4]int32
	if !SDL3_GetTextInputArea(h.win, unsafe.Pointer(&rect[0]), nil) {
		return 0, 0, 0, 0, false
	}
	return rect[0], rect[1], rect[2], rect[3], true
}

// SDL3_GetTextInputArea is registered with the rest of the SDL3 entry points.
var SDL3_GetTextInputArea func(win unsafe.Pointer, rect unsafe.Pointer, cursor *int32) bool
