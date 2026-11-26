package mobilegles

// Mouse cursor support via SDL3 system cursors. Loaded with the same
// purego bindings as the rest of the SDL3 surface. SDL cursor state is
// process-global, so no window handle is needed.

import (
	"sync"
	"unsafe"
)

// SDL3 SDL_SystemCursor values, in enum order (SDL_mouse.h, SDL ≥ 3.2 where
// the enum gained the individual pane-resize entries). Verified against
// SDL 3.4.2: DEFAULT=0 … MOVE=9, NOT_ALLOWED=10, POINTER=11, NW_RESIZE=12.
// The values are positional, so a wrong one still creates a *valid* cursor —
// it just has the wrong shape (POINTER was 12 here once, which showed the
// top-left resize arrow over links).
const (
	sysCursorDefault    = 0
	sysCursorText       = 1
	sysCursorWait       = 2
	sysCursorCross      = 3
	sysCursorProgress   = 4
	sysCursorNWSE       = 5
	sysCursorNESW       = 6
	sysCursorEW         = 7
	sysCursorNS         = 8
	sysCursorMove       = 9
	sysCursorNotAllowed = 10
	sysCursorPointer    = 11
)

var (
	cursorMu        sync.Mutex
	systemCursors   = map[string]unsafe.Pointer{}
	currentCursorNm string
)

// SetCursorByName applies a system cursor by logical name. Names match the
// platform.Cursor constructors: "" / "arrow" (default), "text", "pointing",
// "move", "resizeH", "resizeV", "resizeLD", "resizeRD", "crosshair", "wait",
// "notAllowed". Unknown names and missing SDL entry points are silent no-ops.
func SetCursorByName(name string) {
	if SDL3_SetCursor == nil || SDL3_CreateSystemCursor == nil {
		return
	}
	cursorMu.Lock()
	defer cursorMu.Unlock()
	if name == currentCursorNm {
		return
	}
	t, known := cursorTypeForName(name)
	cur, cached := systemCursors[name]
	if !known {
		// Unknown name: the default arrow, and remember the decision.
		t = sysCursorDefault
	}
	if !cached {
		cur = SDL3_CreateSystemCursor(t)
		if cur == nil && t != sysCursorDefault {
			// This SDL build lacks the shape (e.g. POINTER on very old
			// versions): fall back to the arrow rather than leaving whatever
			// cursor was set before, which reads as "the wrong cursor stuck".
			cur = SDL3_CreateSystemCursor(sysCursorDefault)
			name = "arrow"
		}
		if cur == nil {
			// Even the default failed; do not retry on every mouse move.
			systemCursors[name] = nil
			return
		}
		systemCursors[name] = cur
	}
	if cur == nil {
		return
	}
	SDL3_SetCursor(cur)
	currentCursorNm = name
}

// cursorTypeForName maps a logical cursor name onto the SDL system-cursor
// enum. The second result is false for an unknown name (the caller then uses
// the default arrow).
func cursorTypeForName(name string) (int32, bool) {
	switch name {
	case "", "arrow", "default":
		return sysCursorDefault, true
	case "text", "ibeam":
		return sysCursorText, true
	case "pointing", "hand":
		return sysCursorPointer, true
	case "resizeH":
		return sysCursorEW, true
	case "resizeV":
		return sysCursorNS, true
	case "resizeLD":
		return sysCursorNWSE, true
	case "resizeRD":
		return sysCursorNESW, true
	case "move":
		return sysCursorMove, true
	case "crosshair":
		return sysCursorCross, true
	case "wait":
		return sysCursorWait, true
	case "progress":
		return sysCursorProgress, true
	case "notAllowed":
		return sysCursorNotAllowed, true
	}
	return sysCursorDefault, false
}

// KnownCursorNames returns the logical cursor names this backend maps to a
// system cursor. meui/platform asserts its constructors against this list.
func KnownCursorNames() []string {
	return []string{
		"arrow", "pointing", "text", "move", "crosshair", "wait", "notAllowed",
		"resizeH", "resizeV", "resizeLD", "resizeRD",
	}
}
