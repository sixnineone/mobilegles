package mobilegles

import "testing"

// The SDL_SystemCursor values are positional: a wrong one still creates a
// valid cursor, just the wrong shape. POINTER was 12 here once (which is
// NW_RESIZE in SDL 3.2+), so the hand over links was a resize arrow. These
// values are pinned against SDL_mouse.h (SDL 3.4.2).
func TestSystemCursorValuesMatchSDL(t *testing.T) {
	want := map[string]int32{
		"DEFAULT":     0,
		"TEXT":        1,
		"WAIT":        2,
		"CROSSHAIR":   3,
		"PROGRESS":    4,
		"NWSE_RESIZE": 5,
		"NESW_RESIZE": 6,
		"EW_RESIZE":   7,
		"NS_RESIZE":   8,
		"MOVE":        9,
		"NOT_ALLOWED": 10,
		"POINTER":     11,
	}
	got := map[string]int32{
		"DEFAULT":     sysCursorDefault,
		"TEXT":        sysCursorText,
		"WAIT":        sysCursorWait,
		"CROSSHAIR":   sysCursorCross,
		"PROGRESS":    sysCursorProgress,
		"NWSE_RESIZE": sysCursorNWSE,
		"NESW_RESIZE": sysCursorNESW,
		"EW_RESIZE":   sysCursorEW,
		"NS_RESIZE":   sysCursorNS,
		"MOVE":        sysCursorMove,
		"NOT_ALLOWED": sysCursorNotAllowed,
		"POINTER":     sysCursorPointer,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("SDL_SYSTEM_CURSOR_%s = %d, want %d", name, got[name], w)
		}
	}
}

// The diagonal names are a convention, not a spelling: the original cursor
// images define resizeLD as the NW-SE arrow and resizeRD as NE-SW, and the
// toolkit's MdiWindow and the window edge zones both assign them to the
// matching corners. Swapping these two lines flips every corner's shape.
func TestDiagonalCursorConvention(t *testing.T) {
	cases := []struct {
		name string
		want int32
	}{
		{"resizeLD", sysCursorNWSE},
		{"resizeRD", sysCursorNESW},
		{"resizeH", sysCursorEW},
		{"resizeV", sysCursorNS},
	}
	for _, c := range cases {
		got, ok := cursorTypeForName(c.name)
		if !ok {
			t.Fatalf("%s is not mapped", c.name)
		}
		if got != c.want {
			t.Errorf("%s -> SDL %d, want %d", c.name, got, c.want)
		}
	}
}

func TestCursorTypeForNameCoversEveryName(t *testing.T) {
	for _, name := range KnownCursorNames() {
		if _, ok := cursorTypeForName(name); !ok {
			t.Errorf("cursor %q is listed but has no SDL type", name)
		}
	}
	// Aliases the toolkit may use.
	for _, alias := range []string{"", "default", "hand", "ibeam"} {
		if _, ok := cursorTypeForName(alias); !ok {
			t.Errorf("alias %q has no SDL type", alias)
		}
	}
	if _, ok := cursorTypeForName("definitely-not-a-cursor"); ok {
		t.Error("an unknown name must report false")
	}
}
