package mobilegles

// sdl3_constants_test.go — guards against SDL2-era constant values.
//
// Twice now, wrong SDL constants caused hard-to-diagnose runtime
// breakage (window flags: borderless/resizable swapped; GL attributes:
// sharing never enabled, 2-bit-green visual requested). This test
// pins every SDL3_* numeric constant to the value from the installed
// SDL3 headers (/usr/include/SDL3/*.h). When bumping SDL versions,
// regenerate these expected values FROM THE HEADERS, never from
// memory, SDL2 docs, or blog posts.

import (
	"testing"

	"github.com/ebitengine/purego"
)

// TestSDL3EventAndFlagConstants pins SDL_InitFlags, SDL_WindowFlags,
// SDL_EventType and SDL_GLAttr values. Sources: SDL_init.h,
// SDL_video.h, SDL_events.h, SDL_hints.h.
func TestSDL3EventAndFlagConstants(t *testing.T) {
	cases := []struct {
		name string
		got  uint64
		want uint64
		src  string
	}{
		// SDL_init.h — SDL_InitFlags
		{"SDL_INIT_VIDEO", SDL3_INIT_VIDEO, 0x20, "SDL_init.h"},

		// SDL_video.h — SDL_WindowFlags
		{"SDL_WINDOW_OPENGL", SDL3_WINDOW_OPENGL, 0x2, "SDL_video.h"},
		{"SDL_WINDOW_OCCLUDED", SDL3_WINDOW_OCCLUDED, 0x4, "SDL_video.h"},
		{"SDL_WINDOW_HIDDEN", SDL3_WINDOW_HIDDEN, 0x8, "SDL_video.h"},
		{"SDL_WINDOW_BORDERLESS", SDL3_WINDOW_BORDERLESS, 0x10, "SDL_video.h"},
		{"SDL_WINDOW_RESIZABLE", SDL3_WINDOW_RESIZABLE, 0x20, "SDL_video.h"},
		{"SDL_WINDOW_OCCLUDED_MULTI", uint64(SDL3_WINDOW_OCCLUDED_MULTI), 0x4, "SDL_video.h"},

		// SDL_events.h — SDL_EventType
		{"SDL_EVENT_QUIT", uint64(SDL3_EVENT_QUIT), 0x100, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_SHOWN", uint64(SDL3_EVENT_WINDOW_SHOWN), 0x202, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_HIDDEN", uint64(SDL3_EVENT_WINDOW_HIDDEN), 0x203, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_EXPOSED", uint64(SDL3_EVENT_WINDOW_EXPOSED), 0x204, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_MOVED", uint64(SDL3_EVENT_WINDOW_MOVED), 0x205, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_RESIZED", uint64(SDL3_EVENT_WINDOW_RESIZED), 0x206, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_PIXEL_SIZE_CHANGED", uint64(SDL3_EVENT_WINDOW_PIXEL_SIZE_CHANGED), 0x207, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_METAL_VIEW_RESIZED", uint64(SDL3_EVENT_WINDOW_METAL_VIEW_RESIZED), 0x208, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_MINIMIZED", uint64(SDL3_EVENT_WINDOW_MINIMIZED), 0x209, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_MAXIMIZED", uint64(SDL3_EVENT_WINDOW_MAXIMIZED), 0x20A, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_RESTORED", uint64(SDL3_EVENT_WINDOW_RESTORED), 0x20B, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_MOUSE_ENTER", uint64(SDL3_EVENT_WINDOW_MOUSE_ENTER), 0x20C, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_MOUSE_LEAVE", uint64(SDL3_EVENT_WINDOW_MOUSE_LEAVE), 0x20D, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_FOCUS_GAINED", uint64(SDL3_EVENT_WINDOW_FOCUS_GAINED), 0x20E, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_FOCUS_LOST", uint64(SDL3_EVENT_WINDOW_FOCUS_LOST), 0x20F, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_CLOSE_REQUESTED", uint64(SDL3_EVENT_WINDOW_CLOSE_REQUESTED), 0x210, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_HIT_TEST", uint64(SDL3_EVENT_WINDOW_HIT_TEST), 0x211, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_ICCPROF_CHANGED", uint64(SDL3_EVENT_WINDOW_ICCPROF_CHANGED), 0x212, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_DISPLAY_CHANGED", uint64(SDL3_EVENT_WINDOW_DISPLAY_CHANGED), 0x213, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_DISPLAY_SCALE_CHANGED", uint64(SDL3_EVENT_WINDOW_DISPLAY_SCALE_CHANGED), 0x214, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_SAFE_AREA_CHANGED", uint64(SDL3_EVENT_WINDOW_SAFE_AREA_CHANGED), 0x215, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_OCCLUDED", uint64(SDL3_EVENT_WINDOW_OCCLUDED), 0x216, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_ENTER_FULLSCREEN", uint64(SDL3_EVENT_WINDOW_ENTER_FULLSCREEN), 0x217, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_LEAVE_FULLSCREEN", uint64(SDL3_EVENT_WINDOW_LEAVE_FULLSCREEN), 0x218, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_DESTROYED", uint64(SDL3_EVENT_WINDOW_DESTROYED), 0x219, "SDL_events.h"},
		{"SDL_EVENT_WINDOW_HDR_STATE_CHANGED", uint64(SDL3_EVENT_WINDOW_HDR_STATE_CHANGED), 0x21A, "SDL_events.h"},
		{"SDL_EVENT_KEY_DOWN", uint64(SDL3_EVENT_KEY_DOWN), 0x300, "SDL_events.h"},
		{"SDL_EVENT_KEY_UP", uint64(SDL3_EVENT_KEY_UP), 0x301, "SDL_events.h"},
		{"SDL_EVENT_TEXT_EDITING", uint64(SDL3_EVENT_TEXT_EDITING), 0x302, "SDL_events.h"},
		{"SDL_EVENT_TEXT_INPUT", uint64(SDL3_EVENT_TEXT_INPUT), 0x303, "SDL_events.h"},
		{"SDL_EVENT_KEYMAP_CHANGED", uint64(SDL3_EVENT_KEYMAP_CHANGED), 0x304, "SDL_events.h"},
		{"SDL_EVENT_KEYBOARD_ADDED", uint64(SDL3_EVENT_KEYBOARD_ADDED), 0x305, "SDL_events.h"},
		{"SDL_EVENT_KEYBOARD_REMOVED", uint64(SDL3_EVENT_KEYBOARD_REMOVED), 0x306, "SDL_events.h"},
		{"SDL_EVENT_TEXT_EDITING_CANDIDATES", uint64(SDL3_EVENT_TEXT_EDITING_CANDIDATES), 0x307, "SDL_events.h"},
		{"SDL_EVENT_SCREEN_KEYBOARD_SHOWN", uint64(SDL3_EVENT_SCREEN_KEYBOARD_SHOWN), 0x308, "SDL_events.h"},
		{"SDL_EVENT_SCREEN_KEYBOARD_HIDDEN", uint64(SDL3_EVENT_SCREEN_KEYBOARD_HIDDEN), 0x309, "SDL_events.h"},
		{"SDL_EVENT_MOUSE_MOTION", uint64(SDL3_EVENT_MOUSE_MOTION), 0x400, "SDL_events.h"},
		{"SDL_EVENT_MOUSE_BUTTON_DOWN", uint64(SDL3_EVENT_MOUSE_BUTTON_DOWN), 0x401, "SDL_events.h"},
		{"SDL_EVENT_MOUSE_BUTTON_UP", uint64(SDL3_EVENT_MOUSE_BUTTON_UP), 0x402, "SDL_events.h"},
		{"SDL_EVENT_MOUSE_WHEEL", uint64(SDL3_EVENT_MOUSE_WHEEL), 0x403, "SDL_events.h"},
		{"SDL_EVENT_MOUSE_ADDED", uint64(SDL3_EVENT_MOUSE_ADDED), 0x404, "SDL_events.h"},
		{"SDL_EVENT_MOUSE_REMOVED", uint64(SDL3_EVENT_MOUSE_REMOVED), 0x405, "SDL_events.h"},
		{"SDL_EVENT_JOYSTICK_AXIS", uint64(SDL3_EVENT_JOYSTICK_AXIS), 0x600, "SDL_events.h"},
		{"SDL_EVENT_JOYSTICK_BALL", uint64(SDL3_EVENT_JOYSTICK_BALL), 0x601, "SDL_events.h"},
		{"SDL_EVENT_JOYSTICK_HAT", uint64(SDL3_EVENT_JOYSTICK_HAT), 0x602, "SDL_events.h"},
		{"SDL_EVENT_JOYSTICK_BUTTON_DOWN", uint64(SDL3_EVENT_JOYSTICK_BUTTON_DOWN), 0x603, "SDL_events.h"},
		{"SDL_EVENT_JOYSTICK_BUTTON_UP", uint64(SDL3_EVENT_JOYSTICK_BUTTON_UP), 0x604, "SDL_events.h"},
		{"SDL_EVENT_FINGER_DOWN", uint64(SDL3_EVENT_FINGER_DOWN), 0x700, "SDL_events.h"},
		{"SDL_EVENT_FINGER_UP", uint64(SDL3_EVENT_FINGER_UP), 0x701, "SDL_events.h"},
		{"SDL_EVENT_FINGER_MOTION", uint64(SDL3_EVENT_FINGER_MOTION), 0x702, "SDL_events.h"},
		{"SDL_EVENT_FINGER_CANCELED", uint64(SDL3_EVENT_FINGER_CANCELED), 0x703, "SDL_events.h"},
		{"SDL_EVENT_DROP_FILE", uint64(SDL3_EVENT_DROP_FILE), 0x1000, "SDL_events.h"},
		{"SDL_EVENT_DROP_TEXT", uint64(SDL3_EVENT_DROP_TEXT), 0x1001, "SDL_events.h"},
		{"SDL_EVENT_DROP_BEGIN", uint64(SDL3_EVENT_DROP_BEGIN), 0x1002, "SDL_events.h"},
		{"SDL_EVENT_DROP_COMPLETE", uint64(SDL3_EVENT_DROP_COMPLETE), 0x1003, "SDL_events.h"},
		{"SDL_EVENT_DROP_POSITION", uint64(SDL3_EVENT_DROP_POSITION), 0x1004, "SDL_events.h"},

		// SDL_video.h — SDL_GLAttr enum (0-based order)
		{"SDL_GL_CONTEXT_MAJOR_VERSION", uint64(SDL3_GL_CONTEXT_MAJOR_VERSION), 17, "SDL_video.h"},
		{"SDL_GL_CONTEXT_MINOR_VERSION", uint64(SDL3_GL_CONTEXT_MINOR_VERSION), 18, "SDL_video.h"},
		{"SDL_GL_MULTISAMPLEBUFFERS", uint64(SDL3_GL_MULTISAMPLEBUFFERS), 13, "SDL_video.h"},
		{"SDL_GL_MULTISAMPLESAMPLES", uint64(SDL3_GL_MULTISAMPLESAMPLES), 14, "SDL_video.h"},
		{"SDL_GL_SHARE_WITH_CURRENT_CONTEXT", uint64(SDL3_GL_SHARE_WITH_CURRENT_CONTEXT), 21, "SDL_video.h"},
	}
	bad := 0
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %#x, SDL3 %s says %#x", c.name, c.got, c.src, c.want)
			bad++
		}
	}
	if bad > 0 {
		t.Errorf("%d SDL3 constant(s) diverge from the headers — re-transcribe from /usr/include/SDL3, not from SDL2 docs", bad)
	}
}

// TestSDL3ScancodeConstants pins the SDL_Scancode values used for key
// translation (SDL_scancode.h). Letters A..Z are consecutive 4..29;
// digits are 1=30..0=39 (0 comes AFTER 9!).
func TestSDL3ScancodeConstants(t *testing.T) {
	cases := []struct {
		name string
		got  int32
		want int32
	}{
		{"SDL_SCANCODE_ESCAPE", SDL3_SCANCODE_ESCAPE, 41},
		{"SDL_SCANCODE_RETURN", SDL3_SCANCODE_RETURN, 40},
		{"SDL_SCANCODE_UP", SDL3_SCANCODE_UP, 82},
		{"SDL_SCANCODE_DOWN", SDL3_SCANCODE_DOWN, 81},
		{"SDL_SCANCODE_LEFT", SDL3_SCANCODE_LEFT, 80},
		{"SDL_SCANCODE_RIGHT", SDL3_SCANCODE_RIGHT, 79},
		{"SDL_SCANCODE_BACKSPACE", SDL3_SCANCODE_BACKSPACE, 42},
		{"SDL_SCANCODE_TAB", SDL3_SCANCODE_TAB, 43},
		{"SDL_SCANCODE_SPACE", SDL3_SCANCODE_SPACE, 44},
		{"SDL_SCANCODE_DELETE", SDL3_SCANCODE_DELETE, 76},
		{"SDL_SCANCODE_HOME", SDL3_SCANCODE_HOME, 74},
		{"SDL_SCANCODE_END", SDL3_SCANCODE_END, 77},
		{"SDL_SCANCODE_A", SDL3_SCANCODE_A, 4},
		{"SDL_SCANCODE_Z", SDL3_SCANCODE_Z, 29},
		{"SDL_SCANCODE_0", SDL3_SCANCODE_0, 39},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, SDL_scancode.h says %d", c.name, c.got, c.want)
		}
	}
}

// TestSDL3HintNames pins SDL3 hint name strings (SDL_hints.h).
func TestSDL3HintNames(t *testing.T) {
	if SDL3_HINT_VIDEO_FORCE_EGL != "SDL_VIDEO_FORCE_EGL" {
		t.Errorf("SDL3_HINT_VIDEO_FORCE_EGL = %q, SDL_hints.h says %q",
			SDL3_HINT_VIDEO_FORCE_EGL, "SDL_VIDEO_FORCE_EGL")
	}
}

// TestSDL3SymbolsExist dlopens libSDL3 and verifies every SDL_*
// symbol name used by RegisterLibFunc actually exists — a missing or
// renamed symbol would otherwise fail silently at first use (the
// function pointer stays nil). Skipped where libSDL3 is not installed.
func TestSDL3SymbolsExist(t *testing.T) {
	h, err := purego.Dlopen("libSDL3.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Skipf("libSDL3.so not loadable on this machine: %v", err)
	}
	names := []string{
		"SDL_CreateSystemCursor", "SDL_CreateWindow", "SDL_DestroyWindow",
		"SDL_free", "SDL_GetClipboardText", "SDL_GetError",
		"SDL_GetWindowFlags", "SDL_GetWindowID", "SDL_GetWindowPosition",
		"SDL_GetWindowSize", "SDL_GetWindowSizeInPixels",
		"SDL_GL_CreateContext", "SDL_GL_GetProcAddress", "SDL_GL_MakeCurrent",
		"SDL_GL_SetAttribute", "SDL_GL_SetSwapInterval", "SDL_GL_SwapWindow",
		"SDL_Init", "SDL_MaximizeWindow", "SDL_MinimizeWindow",
		"SDL_PollEvent", "SDL_PushEvent", "SDL_Quit", "SDL_RaiseWindow",
		"SDL_RestoreWindow", "SDL_SetClipboardText", "SDL_SetCursor",
		"SDL_SetHint", "SDL_SetWindowFullscreen", "SDL_SetWindowHitTest",
		"SDL_SetWindowPosition", "SDL_SetWindowSize", "SDL_SetWindowTitle",
		"SDL_ShowWindow", "SDL_StartTextInput", "SDL_StopTextInput",
	}
	missing := 0
	for _, n := range names {
		if _, err := purego.Dlsym(h, n); err != nil {
			t.Errorf("libSDL3 missing symbol %s (renamed in this SDL version?)", n)
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d SDL3 symbol(s) not found — update the RegisterLibFunc names", missing)
	}
}
