package mobilegles

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	libSDL3    uintptr
	sdl3Window unsafe.Pointer // primary SDL window (compat: single-window path)
	sdl3GL     unsafe.Pointer // primary GL context
	sdl3WinW   int32          // primary real pixel size (post-scaling)
	sdl3WinH   int32

	SDL3_Init               func(uint64) int32
	SDL3_Quit               func()
	SDL3_CreateWindow       func(title uintptr, w int32, h int32, flags uint64) unsafe.Pointer
	SDL3_DestroyWindow      func(unsafe.Pointer)
	SDL3_PollEvent          func(event unsafe.Pointer) bool
	SDL3_GL_CreateContext   func(unsafe.Pointer) unsafe.Pointer
	SDL3_GL_MakeCurrent     func(unsafe.Pointer, unsafe.Pointer)
	SDL3_GL_SwapWindow      func(unsafe.Pointer)
	SDL3_GL_SetAttribute    func(uint32, int32)
	SDL3_GL_GetProcAddress  func(name uintptr) uintptr
	SDL3_CreateSystemCursor func(cursorType int32) unsafe.Pointer
	SDL3_SetCursor          func(cursor unsafe.Pointer)
	// SDL_GetModState returns the CURRENT modifier state. A wheel event carries no
	// modifier field of its own (SDL_MouseWheelEvent has none), so this is the only
	// way to know whether shift or ctrl was held when the wheel turned.
	SDL3_GetModState func() uint16
)

const (
	// SDL_INIT_VIDEO from SDL3 SDL_init.h (0x20 — 0x2 is not a valid
	// SDL3 subsystem flag; it only "worked" because SDL3 initializes
	// video on demand at window creation).
	SDL3_INIT_VIDEO uint64 = 0x00000020

	// SDL3 window flags — VERIFIED against /usr/include/SDL3/SDL_video.h
	// (SDL2's values differ: 0x4 is OCCLUDED, 0x10 is BORDERLESS!)
	SDL3_WINDOW_OPENGL     uint64 = 0x00000002
	SDL3_WINDOW_OCCLUDED   uint64 = 0x00000004
	SDL3_WINDOW_HIDDEN     uint64 = 0x00000008
	SDL3_WINDOW_BORDERLESS uint64 = 0x00000010
	SDL3_WINDOW_RESIZABLE  uint64 = 0x00000020
	// SDL_WINDOW_TRANSPARENT (SDL_video.h 0x40000000): window with a
	// transparent buffer — required for rounded window corners on
	// X11/XWayland (compositor needed).
	SDL3_WINDOW_TRANSPARENT uint64 = 0x40000000

	// SDL3 event type enum (SDL_EventType) — values verified against
	// /usr/include/SDL3/SDL_events.h. Sequential enums are written out
	// explicitly; NOTE: window events start at 0x202 and keyboard text
	// events differ from SDL2 (TEXT_INPUT=0x303 here, 0x303 was
	// SDL2's TEXT_EDITING-adjacent numbering).
	SDL3_EVENT_QUIT uint32 = 0x100

	// Window events (SDL_EVENT_WINDOW_*)
	SDL3_EVENT_WINDOW_SHOWN                 uint32 = 0x202
	SDL3_EVENT_WINDOW_HIDDEN                uint32 = 0x203
	SDL3_EVENT_WINDOW_EXPOSED               uint32 = 0x204
	SDL3_EVENT_WINDOW_MOVED                 uint32 = 0x205
	SDL3_EVENT_WINDOW_RESIZED               uint32 = 0x206
	SDL3_EVENT_WINDOW_PIXEL_SIZE_CHANGED    uint32 = 0x207
	SDL3_EVENT_WINDOW_METAL_VIEW_RESIZED    uint32 = 0x208
	SDL3_EVENT_WINDOW_MINIMIZED             uint32 = 0x209
	SDL3_EVENT_WINDOW_MAXIMIZED             uint32 = 0x20A
	SDL3_EVENT_WINDOW_RESTORED              uint32 = 0x20B
	SDL3_EVENT_WINDOW_MOUSE_ENTER           uint32 = 0x20C
	SDL3_EVENT_WINDOW_MOUSE_LEAVE           uint32 = 0x20D
	SDL3_EVENT_WINDOW_FOCUS_GAINED          uint32 = 0x20E
	SDL3_EVENT_WINDOW_FOCUS_LOST            uint32 = 0x20F
	SDL3_EVENT_WINDOW_CLOSE_REQUESTED       uint32 = 0x210
	SDL3_EVENT_WINDOW_HIT_TEST              uint32 = 0x211
	SDL3_EVENT_WINDOW_ICCPROF_CHANGED       uint32 = 0x212
	SDL3_EVENT_WINDOW_DISPLAY_CHANGED       uint32 = 0x213
	SDL3_EVENT_WINDOW_DISPLAY_SCALE_CHANGED uint32 = 0x214
	SDL3_EVENT_WINDOW_SAFE_AREA_CHANGED     uint32 = 0x215
	SDL3_EVENT_WINDOW_OCCLUDED              uint32 = 0x216
	SDL3_EVENT_WINDOW_ENTER_FULLSCREEN      uint32 = 0x217
	SDL3_EVENT_WINDOW_LEAVE_FULLSCREEN      uint32 = 0x218
	SDL3_EVENT_WINDOW_DESTROYED             uint32 = 0x219
	SDL3_EVENT_WINDOW_HDR_STATE_CHANGED     uint32 = 0x21A

	// Keyboard events (SDL_EVENT_KEY_*, SDL_EVENT_TEXT_*)
	SDL3_EVENT_KEY_DOWN                uint32 = 0x300
	SDL3_EVENT_KEY_UP                  uint32 = 0x301
	SDL3_EVENT_TEXT_EDITING            uint32 = 0x302 // IME composition in progress
	SDL3_EVENT_TEXT_INPUT              uint32 = 0x303 // committed text
	SDL3_EVENT_KEYMAP_CHANGED          uint32 = 0x304
	SDL3_EVENT_KEYBOARD_ADDED          uint32 = 0x305
	SDL3_EVENT_KEYBOARD_REMOVED        uint32 = 0x306
	SDL3_EVENT_TEXT_EDITING_CANDIDATES uint32 = 0x307
	SDL3_EVENT_SCREEN_KEYBOARD_SHOWN   uint32 = 0x308
	SDL3_EVENT_SCREEN_KEYBOARD_HIDDEN  uint32 = 0x309

	// Mouse events (SDL_EVENT_MOUSE_*)
	SDL3_EVENT_MOUSE_MOTION      uint32 = 0x400
	SDL3_EVENT_MOUSE_BUTTON_DOWN uint32 = 0x401
	SDL3_EVENT_MOUSE_BUTTON_UP   uint32 = 0x402
	SDL3_EVENT_MOUSE_WHEEL       uint32 = 0x403
	SDL3_EVENT_MOUSE_ADDED       uint32 = 0x404
	SDL3_EVENT_MOUSE_REMOVED     uint32 = 0x405

	// Joystick events (SDL_EVENT_JOYSTICK_*)
	SDL3_EVENT_JOYSTICK_AXIS        uint32 = 0x600
	SDL3_EVENT_JOYSTICK_BALL        uint32 = 0x601
	SDL3_EVENT_JOYSTICK_HAT         uint32 = 0x602
	SDL3_EVENT_JOYSTICK_BUTTON_DOWN uint32 = 0x603
	SDL3_EVENT_JOYSTICK_BUTTON_UP   uint32 = 0x604

	// Touch events (SDL_EVENT_FINGER_*)
	SDL3_EVENT_FINGER_DOWN     uint32 = 0x700
	SDL3_EVENT_FINGER_UP       uint32 = 0x701
	SDL3_EVENT_FINGER_MOTION   uint32 = 0x702
	SDL3_EVENT_FINGER_CANCELED uint32 = 0x703

	// Drop events (SDL_EVENT_DROP_*)
	SDL3_EVENT_DROP_FILE     uint32 = 0x1000
	SDL3_EVENT_DROP_TEXT     uint32 = 0x1001
	SDL3_EVENT_DROP_BEGIN    uint32 = 0x1002
	SDL3_EVENT_DROP_COMPLETE uint32 = 0x1003
	SDL3_EVENT_DROP_POSITION uint32 = 0x1004

	// SDL_EVENT_SENSOR_UPDATE (SDL_events.h). Sensors are NOT window events:
	// the pump hands them to the handler installed with SetSensorHandler
	// instead of routing them to a window.
	SDL3_EVENT_SENSOR_UPDATE uint32 = 0x1200

	// SDL3 SDL_GLAttr enum values (VERIFIED against SDL3 headers —
	// SDL2-era guesses silently set GREEN_SIZE/BLUE_SIZE instead!).
	SDL3_GL_CONTEXT_MAJOR_VERSION uint32 = 17
	SDL3_GL_CONTEXT_MINOR_VERSION uint32 = 18
	SDL3_GL_CONTEXT_PROFILE_MASK  uint32 = 20 // SDL_GL_CONTEXT_PROFILE_MASK

	// SDL_GLprofile (SDL_video.h): the value for SDL_GL_CONTEXT_PROFILE_MASK.
	// Android has OpenGL ES only, so it must ask for the ES profile; the
	// desktop path leaves the default (desktop GL). int32 because that is the
	// value type SDL_GL_SetAttribute takes (the attr itself is uint32).
	SDL3_GL_CONTEXT_PROFILE_ES int32 = 0x0004

	// SDL_WINDOW_FULLSCREEN (SDL_video.h). On Android the surface belongs to
	// the activity and SDL's video driver owns the geometry, so the desktop
	// BORDERLESS/RESIZABLE pair is meaningless there.
	SDL3_WINDOW_FULLSCREEN uint64 = 0x0000000000000001

	// SDL_GL attribute names (SDL_GL_Attr enum values)
	SDL3_GL_MULTISAMPLEBUFFERS uint32 = 0xD
	SDL3_GL_MULTISAMPLESAMPLES uint32 = 0xE
)

// SDL3 event-struct field offsets (bytes). SDL_Event is read as raw
// bytes; each event struct shares the 16-byte header (type@0,
// reserved@4, timestamp@8, windowID@16) with payload fields at
// struct-specific offsets, per SDL_events.h.
const (
	sdl3OffsetWindowEventData1 = 20 // SDL_WindowEvent.data1 (resize: width)
	sdl3OffsetWindowEventData2 = 24 // SDL_WindowEvent.data2 (resize: height)

	// SDL_SensorEvent: type@0, reserved@4, timestamp@8, which@16 (SDL_SensorID,
	// uint32), data[6]@20 (float32), sensor_timestamp@44. Note `which` lands at
	// 16, the same offset the event header calls windowID — the sensor struct
	// reuses the header slot for its id.
	sdl3OffsetSensorWhich = 16
	sdl3OffsetSensorData  = 20

	sdl3OffsetKeyScancode = 24 // SDL_KeyboardEvent.scancode

	// SDL_KeyboardEvent.repeat (bool, 1 byte) — SDL sets it on a key-down the OS generated by
	// AUTO-REPEAT. It is a field of its own precisely because "held" is not "pressed again", and no
	// pump in this tree read it: platform.Event declared a Repeat field that nothing ever set.
	sdl3OffsetKeyRepeat = 37

	// SDL_SCANCODE_AC_BACK, the Android back button once SDL is asked to trap it
	// (SDL_HINT_ANDROID_TRAP_BACK_BUTTON). SDL's default is to let the system handle
	// it, which closes the app — so an app cannot participate at all until this is
	// set.
	sdl3ScancodeACBack = 282
	sdl3OffsetKeyMod   = 32 // SDL_KeyboardEvent.mod (SDL_Keymod, uint16)

	sdl3OffsetMouseButton = 24 // SDL_MouseButtonEvent.button (uint8)
	sdl3OffsetMouseX      = 28 // button/motion event x (float32)
	sdl3OffsetMouseY      = 32 // button/motion event y (float32)

	sdl3OffsetWheelX      = 24 // SDL_MouseWheelEvent.x (float32)
	sdl3OffsetWheelY      = 28 // SDL_MouseWheelEvent.y (float32)
	sdl3OffsetWheelMouseX = 36 // SDL_MouseWheelEvent.mouse_x (window-relative)
	sdl3OffsetWheelMouseY = 40 // SDL_MouseWheelEvent.mouse_y (window-relative)

	sdl3OffsetTextInputText = 24 // SDL_TextInputEvent.text (const char* — SDL3; SDL2 had inline char[32])
	// SDL_TextEditingEvent (IME composition): text@24, start@32 (Sint32), length@36 (Sint32); size 40.
	// Offsets taken from the installed headers with offsetof (imeprobe).
	sdl3OffsetTextEditingStart  = 32
	sdl3OffsetTextEditingLength = 36

	sdl3OffsetFingerID       = 24 // SDL_TouchFingerEvent.fingerID (Uint64)
	sdl3OffsetFingerX        = 32 // normalized [0,1]
	sdl3OffsetFingerY        = 36
	sdl3OffsetFingerPressure = 40
)

func initSDL3() error {
	var err error
	libSDL3, err = purego.Dlopen("libSDL3.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}

	// Prefer the X11 backend when an X server is present. Rationale
	// (verified 2026-09): on Wayland, presses in DRAGGABLE hit-test
	// regions are fully swallowed — no SDL event, no press-time
	// hit-test callback — so a title-bar double-click is
	// unimplementable there; X11 delivers every button event, which
	// also makes manual window moves (SDL_SetWindowPosition, a no-op
	// on Wayland) work. An explicit user choice is always respected.
	if os.Getenv("SDL_VIDEODRIVER") == "" && os.Getenv("DISPLAY") != "" {
		os.Setenv("SDL_VIDEODRIVER", "x11")
	}

	purego.RegisterLibFunc(&SDL3_PushEvent, libSDL3, "SDL_PushEvent")
	purego.RegisterLibFunc(&SDL3_StartTextInput, libSDL3, "SDL_StartTextInput")
	purego.RegisterLibFunc(&SDL3_StopTextInput, libSDL3, "SDL_StopTextInput")
	purego.RegisterLibFunc(&SDL3_SetTextInputArea, libSDL3, "SDL_SetTextInputArea")
	purego.RegisterLibFunc(&SDL3_GetTextInputArea, libSDL3, "SDL_GetTextInputArea")
	purego.RegisterLibFunc(&SDL3_GetWindowSafeArea, libSDL3, "SDL_GetWindowSafeArea")
	purego.RegisterLibFunc(&SDL3_SetClipboardText, libSDL3, "SDL_SetClipboardText")
	purego.RegisterLibFunc(&SDL3_GetClipboardText, libSDL3, "SDL_GetClipboardText")
	purego.RegisterLibFunc(&SDL3_free, libSDL3, "SDL_free")

	purego.RegisterLibFunc(&SDL3_Init, libSDL3, "SDL_Init")
	purego.RegisterLibFunc(&SDL3_Quit, libSDL3, "SDL_Quit")
	purego.RegisterLibFunc(&SDL3_CreateWindow, libSDL3, "SDL_CreateWindow")
	purego.RegisterLibFunc(&SDL3_DestroyWindow, libSDL3, "SDL_DestroyWindow")
	purego.RegisterLibFunc(&SDL3_PollEvent, libSDL3, "SDL_PollEvent")
	purego.RegisterLibFunc(&SDL3_GetModState, libSDL3, "SDL_GetModState")
	purego.RegisterLibFunc(&SDL3_GL_CreateContext, libSDL3, "SDL_GL_CreateContext")
	purego.RegisterLibFunc(&SDL3_GL_MakeCurrent, libSDL3, "SDL_GL_MakeCurrent")
	purego.RegisterLibFunc(&SDL3_GL_SwapWindow, libSDL3, "SDL_GL_SwapWindow")
	purego.RegisterLibFunc(&SDL3_GL_SetAttribute, libSDL3, "SDL_GL_SetAttribute")
	purego.RegisterLibFunc(&SDL3_GL_GetProcAddress, libSDL3, "SDL_GL_GetProcAddress")
	purego.RegisterLibFunc(&SDL3_ShowWindowFunc, libSDL3, "SDL_ShowWindow")
	purego.RegisterLibFunc(&SDL3_CreateSystemCursor, libSDL3, "SDL_CreateSystemCursor")
	purego.RegisterLibFunc(&SDL3_SetCursor, libSDL3, "SDL_SetCursor")

	return nil
}

func createSDL3Window(title string, width, height int32) error {
	if SDL3_Init == nil {
		if err := initSDL3(); err != nil {
			return err
		}
	}

	// Lock the current OS thread for all OpenGL calls
	runtime.LockOSThread()

	// NOTE: MSAA context attributes (SDL_GL_MULTISAMPLE*) break context
	// creation on this stack (shaders fail to compile — different
	// backend path). Primitives use analytic AA; leave MSAA off.
	if runtime.GOOS == "android" {
		// Android has OpenGL ES only, so the context must ask for the ES
		// profile — a desktop GL 2.1 request is not satisfiable there. SDL's
		// Android video driver owns the surface and the geometry (it comes
		// from the activity), so the desktop window flags do not apply and
		// GLX is not a thing.
		SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_PROFILE_MASK, SDL3_GL_CONTEXT_PROFILE_ES)
		SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_MAJOR_VERSION, 2)
		SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_MINOR_VERSION, 0)
	} else {
		// Request an OpenGL 2.1 context (SDL3 prefers desktop GL over GLES)
		SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_MAJOR_VERSION, 2)
		SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_MINOR_VERSION, 1)
		// GLX instead of EGL on X11/XWayland: EGL multi-window
		// MakeCurrent/swap is broken in SDL3 on XWayland (SDL#9072).
		forceGLXHint()
		trapBackButtonHint()
	}

	titlePtr := uintptr(unsafe.Pointer(&[]byte(title + "\x00")[0]))
	flags := uint64(SDL3_WINDOW_OPENGL | SDL3_WINDOW_BORDERLESS | SDL3_WINDOW_RESIZABLE)
	if runtime.GOOS == "android" {
		// The activity supplies the surface; fullscreen is the only mode.
		flags = SDL3_WINDOW_OPENGL | SDL3_WINDOW_FULLSCREEN
	} else if os.Getenv("MEUI_WINDOW_ROUNDING") == "1" {
		// Look-and-feel spike: transparent buffer so rounded window
		// corners can show the desktop through (compositor required).
		flags |= SDL3_WINDOW_TRANSPARENT
		log.Printf("[laf] transparent window requested (rounded corners)")
	}
	sdl3Window = SDL3_CreateWindow(titlePtr, width, height, flags)
	if sdl3Window == nil {
		runtime.UnlockOSThread()
		return fmt.Errorf("SDL3_CreateWindow returned nil")
	}

	sdl3GL = SDL3_GL_CreateContext(sdl3Window)
	if sdl3GL == nil {
		SDL3_DestroyWindow(sdl3Window)
		sdl3Window = nil
		runtime.UnlockOSThread()
		return fmt.Errorf("SDL3_GL_CreateContext failed")
	}

	SDL3_GL_MakeCurrent(sdl3Window, sdl3GL)

	// Register the primary window so multi-window features (hit-test
	// chrome, routing fallbacks) work on the legacy path too. Adopt the
	// REAL pixel size — display scaling can differ from the request.
	if primaryHandle == nil {
		loadMultiWindowFuncs()
		primaryHandle = &sdlWin{
			win: sdl3Window, ctx: sdl3GL,
			id: SDL3_GetWindowID(sdl3Window),
			// Bind the primary mobilegles window: the routed pump
			// drops events whose registry entry has no meui target,
			// so an unbound primary made the whole app input-dead
			// (rendering and hit-test drags still worked).
			meui: mainWindow,
		}
		QuerySDLWinSize(primaryHandle)
		winMu.Lock()
		sdlWinReg[primaryHandle.id] = primaryHandle
		currentHandle = primaryHandle // createSDL3Window made ctx current
		winMu.Unlock()
		sdl3WinW, sdl3WinH = primaryHandle.w, primaryHandle.h
		eventDebug("primary window real size: %dx%d", primaryHandle.w, primaryHandle.h)
	}

	// Enable text-input events for this window: without SDL_StartTextInput
	// SDL_EVENT_TEXT_INPUT never fires and text widgets never receive runes.
	SDL3_StartTextInput(sdl3Window)
	eventDebug("StartTextInput called on window %v", sdl3Window)

	// Load GL function pointers after context creation
	loadGL3Functions()

	// Make sure window is visible
	SDL3_ShowWindow(sdl3Window)

	return nil
}

// ProcessSDL3Events drains SDL for the legacy single-window path.
func ProcessSDL3Events(win *Window) {
	pumpSDLEvents(func(_ uint32, ev Event) {
		win.PushEvent(ev)
	}, win)
}

// pumpSDLEvents drains the SDL queue once, decoding each event and
// delivering it with the SDL windowID it belongs to. Mouse/key events
// whose windowID is unknown go to the fallback window (if any).
// sensorHandler receives SDL_EVENT_SENSOR_UPDATE payloads. It is installed by
// the platform adapter's Sensors implementation; nil means nobody is listening,
// which is the normal state.
var sensorHandler func(sensorID uint32, values [6]float32)

// SetSensorHandler installs the receiver for sensor updates, or clears it with
// nil. The pump calls it on the UI thread, in the same pass as every other
// event, so a sensor sample arrives through the same serialized path as input —
// no polling goroutine and no second thread touching SDL.
func SetSensorHandler(fn func(sensorID uint32, values [6]float32)) { sensorHandler = fn }

func pumpSDLEvents(route func(winID uint32, ev Event), fallback *Window) {
	if SDL3_PollEvent == nil || sdl3Window == nil {
		return
	}

	var eventBuf [128]byte
	for {
		if !SDL3_PollEvent(unsafe.Pointer(&eventBuf[0])) {
			break
		}

		eventType := *(*uint32)(unsafe.Pointer(&eventBuf[0]))
		winID := *(*uint32)(unsafe.Pointer(&eventBuf[16]))
		eventDebug("sdl event type=0x%x winID=%d", eventType, winID)

		push := func(ev Event) {
			// Window-scoped events: only if the window is known; unknown
			// windows (destroyed) drop. Global events (quit) always pass.
			route(winID, ev)
		}
		_ = fallback

		switch eventType {
		case SDL3_EVENT_QUIT:
			route(0, Event{Type: EventQuit})
		case SDL3_EVENT_WINDOW_CLOSE_REQUESTED:
			push(Event{Type: EventQuit})
		case SDL3_EVENT_WINDOW_RESIZED, SDL3_EVENT_WINDOW_PIXEL_SIZE_CHANGED:
			w := *(*int32)(unsafe.Pointer(&eventBuf[sdl3OffsetWindowEventData1]))
			h := *(*int32)(unsafe.Pointer(&eventBuf[sdl3OffsetWindowEventData2]))
			push(Event{Type: EventResize, Width: w, Height: h})
		case SDL3_EVENT_WINDOW_FOCUS_GAINED:
			push(Event{Type: EventFocusGained})
		case SDL3_EVENT_WINDOW_FOCUS_LOST:
			push(Event{Type: EventFocusLost})
		case SDL3_EVENT_KEY_DOWN:
			scancode := *(*int32)(unsafe.Pointer(&eventBuf[sdl3OffsetKeyScancode]))
			keymod := *(*uint16)(unsafe.Pointer(&eventBuf[sdl3OffsetKeyMod]))
			// The Android back gesture arrives as an ordinary key press once SDL is
			// asked to trap it, and it is NOT a key to the toolkit: an app that sees it
			// as one has to guess that scancode 282 means "go back". It is delivered as
			// its own event so the window can offer it to the app and only close when
			// nothing handles it.
			if scancode == sdl3ScancodeACBack {
				push(Event{Type: EventBack})
				break
			}
			eventDebug("key-down scancode=%d -> key=%v mod=0x%x", scancode, mapSDL3Key(scancode), keymod)
			repeat := *(*uint8)(unsafe.Pointer(&eventBuf[sdl3OffsetKeyRepeat])) != 0
			push(Event{Type: EventKeyDown, Key: mapSDL3Key(scancode), Mod: mapSDL3Mod(keymod), Repeat: repeat})
		case SDL3_EVENT_KEY_UP:
			scancode := *(*int32)(unsafe.Pointer(&eventBuf[sdl3OffsetKeyScancode]))
			if scancode == sdl3ScancodeACBack {
				break // the press already delivered EventBack; do not also report a release
			}
			keymod := *(*uint16)(unsafe.Pointer(&eventBuf[sdl3OffsetKeyMod]))
			push(Event{Type: EventKeyUp, Key: mapSDL3Key(scancode), Mod: mapSDL3Mod(keymod)})
		case SDL3_EVENT_SENSOR_UPDATE:
			// Sensors are not window events: hand them to the installed
			// handler (the platform adapter's Sensors implementation) rather
			// than routing them to a window. SDL only delivers these once the
			// sensor subsystem is initialised and the sensor is open.
			if h := sensorHandler; h != nil {
				id := *(*uint32)(unsafe.Pointer(&eventBuf[sdl3OffsetSensorWhich]))
				var values [6]float32
				for i := range values {
					values[i] = *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetSensorData+4*i]))
				}
				h(id, values)
			}
		case SDL3_EVENT_TEXT_INPUT:
			// SDL3 SDL_TextInputEvent: const char *text at offset 24
			// (NUL-terminated UTF-8, allocated by SDL).
			strPtr := *(*uintptr)(unsafe.Pointer(&eventBuf[sdl3OffsetTextInputText]))
			eventDebug("text-input event: text ptr=0x%x", strPtr)
			if strPtr != 0 {
				text := sdl3CString(strPtr)
				decoded := []rune(text)
				eventDebug("text-input decoded: %q (%d runes)", text, len(decoded))
				for _, r := range decoded {
					push(Event{Type: EventTextInput, Rune: r})
				}
			}
		case SDL3_EVENT_TEXT_EDITING:
			// The IME's IN-PROGRESS text. It is not input yet: the application is expected to show it
			// (underlined) and REPLACE it as the composition changes, and to treat the TEXT_INPUT that
			// follows as the commit. start/length are the selection inside the composition (the IME's
			// caret), in bytes.
			strPtr := *(*uintptr)(unsafe.Pointer(&eventBuf[sdl3OffsetTextInputText]))
			start := *(*int32)(unsafe.Pointer(&eventBuf[sdl3OffsetTextEditingStart]))
			length := *(*int32)(unsafe.Pointer(&eventBuf[sdl3OffsetTextEditingLength]))
			text := ""
			if strPtr != 0 {
				text = sdl3CString(strPtr)
			}
			eventDebug("text-editing: %q start=%d length=%d", text, start, length)
			push(Event{Type: EventTextEditing, Text: text, CompStart: int(start), CompLength: int(length)})
		case SDL3_EVENT_MOUSE_MOTION:
			x := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseX]))
			y := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseY]))
			if h := lookupSDLWin(winID); h != nil {
				h.mouseX, h.mouseY, h.haveMouse = x, y, true
			}
			push(Event{Type: EventMouseMove, X: x, Y: y})
		case SDL3_EVENT_MOUSE_BUTTON_DOWN:
			button := *(*uint8)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseButton]))
			x := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseX]))
			y := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseY]))
			if h := lookupSDLWin(winID); h != nil {
				h.mouseX, h.mouseY, h.haveMouse = x, y, true
			}
			push(Event{Type: EventMouseDown, X: x, Y: y, Button: int(button)})
		case SDL3_EVENT_MOUSE_BUTTON_UP:
			button := *(*uint8)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseButton]))
			x := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseX]))
			y := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetMouseY]))
			if h := lookupSDLWin(winID); h != nil {
				h.mouseX, h.mouseY, h.haveMouse = x, y, true
			}
			push(Event{Type: EventMouseUp, X: x, Y: y, Button: int(button)})
		case SDL3_EVENT_MOUSE_WHEEL:
			wx := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetWheelX]))
			wy := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetWheelY]))
			// The cursor position rides in the wheel event itself —
			// without it the dispatch hit-tests at (0,0) and drops
			// the wheel wherever nothing scrollable sits.
			mx := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetWheelMouseX]))
			my := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetWheelMouseY]))
			// Prefer the tracked client-relative pointer position; the
			// wheel's own y is offset by the top frame on X11 and can be
			// stale right after a move.
			mx, my = lookupSDLWin(winID).wheelPos(mx, my)
			// The wheel event has no modifier field, so the state is queried here.
			// Without it every wheel event reported "no modifiers", which silently
			// disabled shift+wheel and ctrl+wheel in every app: the toolkit records the
			// event's modifiers and the widgets read them.
			var mod KeyMod
			if SDL3_GetModState != nil {
				mod = mapSDL3Mod(SDL3_GetModState())
			}
			push(Event{Type: EventMouseWheel, X: mx, Y: my, WheelX: wx, WheelY: wy, Mod: mod})
			eventDebug("wheel dx=%v dy=%v at (%v,%v)", wx, wy, mx, my)
		case SDL3_EVENT_FINGER_DOWN, SDL3_EVENT_FINGER_UP, SDL3_EVENT_FINGER_MOTION:
			fingerID := *(*int64)(unsafe.Pointer(&eventBuf[sdl3OffsetFingerID]))
			nx := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetFingerX])) // [0,1]
			ny := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetFingerY]))
			pressure := *(*float32)(unsafe.Pointer(&eventBuf[sdl3OffsetFingerPressure]))
			// Convert normalized to pixel coordinates using the target
			// window's size (fallback on the legacy path).
			tw, th := int32(1280), int32(720)
			if h := lookupSDLWin(winID); h != nil {
				tw, th = h.w, h.h
			} else if fallback != nil {
				tw, th = fallback.width, fallback.height
			}
			sx := nx * float32(tw)
			sy := ny * float32(th)
			var et EventType
			switch eventType {
			case SDL3_EVENT_FINGER_DOWN:
				et = EventTouchDown
			case SDL3_EVENT_FINGER_UP:
				et = EventTouchUp
			case SDL3_EVENT_FINGER_MOTION:
				et = EventTouchMove
			}
			push(Event{
				Type:    et,
				TouchID: fingerID,
				X:       sx,
				Y:       sy,
				Touches: []Touch{{ID: fingerID, X: sx, Y: sy, Pressure: pressure}},
			})
		case SDL3_EVENT_JOYSTICK_AXIS:
			which := *(*int32)(unsafe.Pointer(&eventBuf[16]))
			axis := *(*uint8)(unsafe.Pointer(&eventBuf[20]))
			value := *(*int16)(unsafe.Pointer(&eventBuf[22]))
			push(Event{Type: EventJoystickAxis, JoystickID: int(which), Axis: int(axis), Value: float32(value) / 32767.0})
		case SDL3_EVENT_JOYSTICK_BUTTON_DOWN:
			which := *(*int32)(unsafe.Pointer(&eventBuf[16]))
			btn := *(*uint8)(unsafe.Pointer(&eventBuf[20]))
			push(Event{Type: EventJoystickButtonDown, JoystickID: int(which), Button: int(btn)})
		case SDL3_EVENT_JOYSTICK_BUTTON_UP:
			which := *(*int32)(unsafe.Pointer(&eventBuf[16]))
			btn := *(*uint8)(unsafe.Pointer(&eventBuf[20]))
			push(Event{Type: EventJoystickButtonUp, JoystickID: int(which), Button: int(btn)})
		case SDL3_EVENT_JOYSTICK_HAT:
			which := *(*int32)(unsafe.Pointer(&eventBuf[16]))
			hat := *(*uint8)(unsafe.Pointer(&eventBuf[20]))
			value := *(*uint8)(unsafe.Pointer(&eventBuf[21]))
			push(Event{Type: EventJoystickHat, JoystickID: int(which), Hat: int(hat), Value: float32(value)})
		case SDL3_EVENT_DROP_FILE:
			// File path pointer at offset varies in SDL3; simplified for now
			push(Event{Type: EventFileDrop})
		}
	}
}

// SDL3 SDL_Keymod bit values.
const (
	SDL3_KMOD_LSHIFT uint16 = 0x001
	SDL3_KMOD_RSHIFT uint16 = 0x002
	SDL3_KMOD_LCTRL  uint16 = 0x040
	SDL3_KMOD_RCTRL  uint16 = 0x080
	SDL3_KMOD_LALT   uint16 = 0x100
	SDL3_KMOD_RALT   uint16 = 0x200
	SDL3_KMOD_LGUI   uint16 = 0x400
	SDL3_KMOD_RGUI   uint16 = 0x800
	// The LOCKS. Not held-key modifiers: CapsLock stays on until pressed again, and NumLock decides
	// whether the keypad's 1 is a '1' or an End. They are separate bits in SDL3's SDL_Keymod and they
	// were missing here, so a keypad key could not be interpreted at all.
	SDL3_KMOD_NUM    uint16 = 0x1000
	SDL3_KMOD_CAPS   uint16 = 0x2000
	SDL3_KMOD_SCROLL uint16 = 0x4000
)

// mapSDL3Mod converts SDL3 SDL_Keymod bits to our KeyMod bits.
func mapSDL3Mod(m uint16) KeyMod {
	var out KeyMod
	if m&(SDL3_KMOD_LSHIFT|SDL3_KMOD_RSHIFT) != 0 {
		out |= ModShift
	}
	if m&(SDL3_KMOD_LCTRL|SDL3_KMOD_RCTRL) != 0 {
		out |= ModCtrl
	}
	if m&(SDL3_KMOD_LALT|SDL3_KMOD_RALT) != 0 {
		out |= ModAlt
	}
	if m&(SDL3_KMOD_LGUI|SDL3_KMOD_RGUI) != 0 {
		out |= ModMeta
	}
	// THE LOCKS, which the keypad depends on. ModCaps was declared in the toolkit's enum and never set by
	// anything; this is the line that makes it mean what it says.
	if m&SDL3_KMOD_CAPS != 0 {
		out |= ModCaps
	}
	if m&SDL3_KMOD_NUM != 0 {
		out |= ModNum
	}
	if m&SDL3_KMOD_SCROLL != 0 {
		out |= ModScroll
	}
	return out
}

// SDL3 SDL_Scancode values, GENERATED FROM THE INSTALLED HEADERS rather than typed by hand.
//
// Every value here was printed by compiling against /usr/include/SDL3 and running the program, because a
// hand-typed scancode is exactly the sort of thing that is wrong by one and nobody notices. Regenerate with
// the probe in MIGRATION.md M47 after an SDL upgrade — a scancode that moves silently changes which key
// every shortcut in every app is bound to.
const (
	SDL3_SCANCODE_A            int32 = 4
	SDL3_SCANCODE_B            int32 = 5
	SDL3_SCANCODE_C            int32 = 6
	SDL3_SCANCODE_D            int32 = 7
	SDL3_SCANCODE_E            int32 = 8
	SDL3_SCANCODE_F            int32 = 9
	SDL3_SCANCODE_G            int32 = 10
	SDL3_SCANCODE_H            int32 = 11
	SDL3_SCANCODE_I            int32 = 12
	SDL3_SCANCODE_J            int32 = 13
	SDL3_SCANCODE_K            int32 = 14
	SDL3_SCANCODE_L            int32 = 15
	SDL3_SCANCODE_M            int32 = 16
	SDL3_SCANCODE_N            int32 = 17
	SDL3_SCANCODE_O            int32 = 18
	SDL3_SCANCODE_P            int32 = 19
	SDL3_SCANCODE_Q            int32 = 20
	SDL3_SCANCODE_R            int32 = 21
	SDL3_SCANCODE_S            int32 = 22
	SDL3_SCANCODE_T            int32 = 23
	SDL3_SCANCODE_U            int32 = 24
	SDL3_SCANCODE_V            int32 = 25
	SDL3_SCANCODE_W            int32 = 26
	SDL3_SCANCODE_X            int32 = 27
	SDL3_SCANCODE_Y            int32 = 28
	SDL3_SCANCODE_Z            int32 = 29
	SDL3_SCANCODE_0            int32 = 39
	SDL3_SCANCODE_1            int32 = 30
	SDL3_SCANCODE_2            int32 = 31
	SDL3_SCANCODE_3            int32 = 32
	SDL3_SCANCODE_4            int32 = 33
	SDL3_SCANCODE_5            int32 = 34
	SDL3_SCANCODE_6            int32 = 35
	SDL3_SCANCODE_7            int32 = 36
	SDL3_SCANCODE_8            int32 = 37
	SDL3_SCANCODE_9            int32 = 38
	SDL3_SCANCODE_F1           int32 = 58
	SDL3_SCANCODE_F2           int32 = 59
	SDL3_SCANCODE_F3           int32 = 60
	SDL3_SCANCODE_F4           int32 = 61
	SDL3_SCANCODE_F5           int32 = 62
	SDL3_SCANCODE_F6           int32 = 63
	SDL3_SCANCODE_F7           int32 = 64
	SDL3_SCANCODE_F8           int32 = 65
	SDL3_SCANCODE_F9           int32 = 66
	SDL3_SCANCODE_F10          int32 = 67
	SDL3_SCANCODE_F11          int32 = 68
	SDL3_SCANCODE_F12          int32 = 69
	SDL3_SCANCODE_RETURN       int32 = 40
	SDL3_SCANCODE_ESCAPE       int32 = 41
	SDL3_SCANCODE_BACKSPACE    int32 = 42
	SDL3_SCANCODE_TAB          int32 = 43
	SDL3_SCANCODE_SPACE        int32 = 44
	SDL3_SCANCODE_MINUS        int32 = 45
	SDL3_SCANCODE_EQUAL        int32 = 46
	SDL3_SCANCODE_LEFTBRACKET  int32 = 47
	SDL3_SCANCODE_RIGHTBRACKET int32 = 48
	SDL3_SCANCODE_BACKSLASH    int32 = 49
	SDL3_SCANCODE_SEMICOLON    int32 = 51
	SDL3_SCANCODE_APOSTROPHE   int32 = 52
	SDL3_SCANCODE_GRAVE        int32 = 53
	SDL3_SCANCODE_COMMA        int32 = 54
	SDL3_SCANCODE_PERIOD       int32 = 55
	SDL3_SCANCODE_SLASH        int32 = 56
	SDL3_SCANCODE_UP           int32 = 82
	SDL3_SCANCODE_DOWN         int32 = 81
	SDL3_SCANCODE_LEFT         int32 = 80
	SDL3_SCANCODE_RIGHT        int32 = 79
	SDL3_SCANCODE_INSERT       int32 = 73
	SDL3_SCANCODE_HOME         int32 = 74
	SDL3_SCANCODE_PAGEUP       int32 = 75
	SDL3_SCANCODE_DELETE       int32 = 76
	SDL3_SCANCODE_END          int32 = 77
	SDL3_SCANCODE_PAGEDOWN     int32 = 78
	SDL3_SCANCODE_PAUSE        int32 = 72
	SDL3_SCANCODE_KP_1         int32 = 89
	SDL3_SCANCODE_KP_2         int32 = 90
	SDL3_SCANCODE_KP_3         int32 = 91
	SDL3_SCANCODE_KP_4         int32 = 92
	SDL3_SCANCODE_KP_5         int32 = 93
	SDL3_SCANCODE_KP_6         int32 = 94
	SDL3_SCANCODE_KP_7         int32 = 95
	SDL3_SCANCODE_KP_8         int32 = 96
	SDL3_SCANCODE_KP_9         int32 = 97
	SDL3_SCANCODE_KP_0         int32 = 98
	SDL3_SCANCODE_KP_PERIOD    int32 = 99
	SDL3_SCANCODE_KP_DIVIDE    int32 = 84
	SDL3_SCANCODE_KP_MULTIPLY  int32 = 85
	SDL3_SCANCODE_KP_MINUS     int32 = 86
	SDL3_SCANCODE_KP_PLUS      int32 = 87
	SDL3_SCANCODE_KP_ENTER     int32 = 88
	SDL3_SCANCODE_KP_EQUALS    int32 = 103
	SDL3_SCANCODE_KP_COMMA     int32 = 133
	SDL3_SCANCODE_LSHIFT       int32 = 225
	SDL3_SCANCODE_RSHIFT       int32 = 229
	SDL3_SCANCODE_LCTRL        int32 = 224
	SDL3_SCANCODE_RCTRL        int32 = 228
	SDL3_SCANCODE_LALT         int32 = 226
	SDL3_SCANCODE_RALT         int32 = 230
	SDL3_SCANCODE_LGUI         int32 = 227
	SDL3_SCANCODE_RGUI         int32 = 231
)

// mapSDL3Key turns an SDL3 scancode into the toolkit's Keycode.
//
// SDL3 deliberately numbers its scancodes as USB HID usage IDs, so MOST keys pass through unchanged — a
// letter's SDL scancode IS the HID usage for that letter. The two schemes DIVERGE for a documented set,
// and that is the whole bug this function used to have: it mapped the four ARROWS and then returned
// `Keycode(key)` for everything else, so every other diverging key arrived as a bare SDL number that
// means nothing in the HID space the rest of the toolkit speaks.
//
// The divergence, verified against the installed SDL3 headers with offsetof/SDL_SCANCODE:
//
//	SDL_SCANCODE_UP=82  DOWN=81  LEFT=80  RIGHT=79   vs HID Up=200 Down=208 Left=203 Right=205
//	SDL_SCANCODE_INSERT=73 HOME=74 PAGEUP=75 DELETE=76 END=77 PAGEDOWN=78
//	                                        vs HID Insert=210 Home=199 PageUp=211 Delete=212 End=213 PageDown=214
//
// A text box's Delete key was arriving as keycode 76 — a number the toolkit has no meaning for, drawn from
// the same range as keys it does.
//
// THE UNNAMED CASE IS `KeyUnknown`, deliberately. The alternative — keep passing the raw scancode through
// — is what made this bug invisible: a key the toolkit cannot name is delivered as an arbitrary keycode,
// and if that number happens to equal a named key the user is pressing something else entirely. A
// shortcut bound to `KeyUnknown` cannot fire; a shortcut bound to a stray 76 fires on the wrong key.
// If an app needs a key this table does not name, the fix is to NAME it here, not to pass the number
// through.
func mapSDL3Key(key int32) Keycode {
	// The diverging set, spelled out. Everything NOT listed is one where SDL3 and HID agree, and is
	// mapped by the table below rather than by an assumption.
	switch key {
	case SDL3_SCANCODE_UP:
		return KeyUp
	case SDL3_SCANCODE_DOWN:
		return KeyDown
	case SDL3_SCANCODE_LEFT:
		return KeyLeft
	case SDL3_SCANCODE_RIGHT:
		return KeyRight
	case SDL3_SCANCODE_INSERT:
		return KeyInsert
	case SDL3_SCANCODE_HOME:
		return KeyHome
	case SDL3_SCANCODE_PAGEUP:
		return KeyPageUp
	case SDL3_SCANCODE_DELETE:
		return KeyDelete
	case SDL3_SCANCODE_END:
		return KeyEnd
	case SDL3_SCANCODE_PAGEDOWN:
		return KeyPageDown
	case SDL3_SCANCODE_PAUSE:
		return KeyPause
	}
	// The keys the toolkit NAMES. Listed explicitly rather than passed through, because a pass-through is
	// only correct by coincidence and the coincidence is what broke: it holds for letters, digits, F-keys
	// and the keypad, and fails for everything above. A table is longer and it is right by construction.
	switch key {
	case SDL3_SCANCODE_A:
		return KeyA
	case SDL3_SCANCODE_B:
		return KeyB
	case SDL3_SCANCODE_C:
		return KeyC
	case SDL3_SCANCODE_D:
		return KeyD
	case SDL3_SCANCODE_E:
		return KeyE
	case SDL3_SCANCODE_F:
		return KeyF
	case SDL3_SCANCODE_G:
		return KeyG
	case SDL3_SCANCODE_H:
		return KeyH
	case SDL3_SCANCODE_I:
		return KeyI
	case SDL3_SCANCODE_J:
		return KeyJ
	case SDL3_SCANCODE_K:
		return KeyK
	case SDL3_SCANCODE_L:
		return KeyL
	case SDL3_SCANCODE_M:
		return KeyM
	case SDL3_SCANCODE_N:
		return KeyN
	case SDL3_SCANCODE_O:
		return KeyO
	case SDL3_SCANCODE_P:
		return KeyP
	case SDL3_SCANCODE_Q:
		return KeyQ
	case SDL3_SCANCODE_R:
		return KeyR
	case SDL3_SCANCODE_S:
		return KeyS
	case SDL3_SCANCODE_T:
		return KeyT
	case SDL3_SCANCODE_U:
		return KeyU
	case SDL3_SCANCODE_V:
		return KeyV
	case SDL3_SCANCODE_W:
		return KeyW
	case SDL3_SCANCODE_X:
		return KeyX
	case SDL3_SCANCODE_Y:
		return KeyY
	case SDL3_SCANCODE_Z:
		return KeyZ
	case SDL3_SCANCODE_0:
		return Key0
	case SDL3_SCANCODE_1:
		return Key1
	case SDL3_SCANCODE_2:
		return Key2
	case SDL3_SCANCODE_3:
		return Key3
	case SDL3_SCANCODE_4:
		return Key4
	case SDL3_SCANCODE_5:
		return Key5
	case SDL3_SCANCODE_6:
		return Key6
	case SDL3_SCANCODE_7:
		return Key7
	case SDL3_SCANCODE_8:
		return Key8
	case SDL3_SCANCODE_9:
		return Key9
	case SDL3_SCANCODE_RETURN:
		return KeyEnter
	case SDL3_SCANCODE_ESCAPE:
		return KeyEscape
	case SDL3_SCANCODE_BACKSPACE:
		return KeyBackspace
	case SDL3_SCANCODE_TAB:
		return KeyTab
	case SDL3_SCANCODE_SPACE:
		return KeySpace
	case SDL3_SCANCODE_MINUS:
		return KeyMinus
	case SDL3_SCANCODE_EQUAL:
		return KeyEqual
	case SDL3_SCANCODE_LEFTBRACKET:
		return KeyOpenBracket
	case SDL3_SCANCODE_RIGHTBRACKET:
		return KeyCloseBracket
	case SDL3_SCANCODE_BACKSLASH:
		return KeyBackslash
	case SDL3_SCANCODE_SEMICOLON:
		return KeySemicolon
	case SDL3_SCANCODE_APOSTROPHE:
		return KeyApostrophe
	case SDL3_SCANCODE_GRAVE:
		return KeyGrave
	case SDL3_SCANCODE_COMMA:
		return KeyComma
	case SDL3_SCANCODE_PERIOD:
		return KeyPeriod
	case SDL3_SCANCODE_SLASH:
		return KeySlash
	case SDL3_SCANCODE_F1:
		return KeyF1
	case SDL3_SCANCODE_F2:
		return KeyF2
	case SDL3_SCANCODE_F3:
		return KeyF3
	case SDL3_SCANCODE_F4:
		return KeyF4
	case SDL3_SCANCODE_F5:
		return KeyF5
	case SDL3_SCANCODE_F6:
		return KeyF6
	case SDL3_SCANCODE_F7:
		return KeyF7
	case SDL3_SCANCODE_F8:
		return KeyF8
	case SDL3_SCANCODE_F9:
		return KeyF9
	case SDL3_SCANCODE_F10:
		return KeyF10
	case SDL3_SCANCODE_F11:
		return KeyF11
	case SDL3_SCANCODE_F12:
		return KeyF12
	case SDL3_SCANCODE_KP_1:
		return Keypad1
	case SDL3_SCANCODE_KP_2:
		return Keypad2
	case SDL3_SCANCODE_KP_3:
		return Keypad3
	case SDL3_SCANCODE_KP_4:
		return Keypad4
	case SDL3_SCANCODE_KP_5:
		return Keypad5
	case SDL3_SCANCODE_KP_6:
		return Keypad6
	case SDL3_SCANCODE_KP_7:
		return Keypad7
	case SDL3_SCANCODE_KP_8:
		return Keypad8
	case SDL3_SCANCODE_KP_9:
		return Keypad9
	case SDL3_SCANCODE_KP_0:
		return Keypad0
	case SDL3_SCANCODE_KP_PERIOD:
		return KeypadDot
	case SDL3_SCANCODE_KP_DIVIDE:
		return KeypadSlash
	case SDL3_SCANCODE_KP_MULTIPLY:
		return KeypadAsterisk
	case SDL3_SCANCODE_KP_MINUS:
		return KeypadMinus
	case SDL3_SCANCODE_KP_PLUS:
		return KeypadPlus
	case SDL3_SCANCODE_KP_ENTER:
		return KeypadEnter
	case SDL3_SCANCODE_KP_EQUALS:
		return KeypadEquals
	case SDL3_SCANCODE_KP_COMMA:
		return KeypadComma
	case SDL3_SCANCODE_LSHIFT:
		return KeyLeftShift
	case SDL3_SCANCODE_RSHIFT:
		return KeyRightShift
	case SDL3_SCANCODE_LCTRL:
		return KeyLeftCtrl
	case SDL3_SCANCODE_RCTRL:
		return KeyRightCtrl
	case SDL3_SCANCODE_LALT:
		return KeyLeftAlt
	case SDL3_SCANCODE_RALT:
		return KeyRightAlt
	case SDL3_SCANCODE_LGUI:
		return KeyLeftMeta
	case SDL3_SCANCODE_RGUI:
		return KeyRightMeta
	}
	return KeyUnknown
}

// Shutdown tears down SDL cleanly (window + subsystems). Call after
// the last window closes — e.g. windowed test harnesses before the
// process exits. Safe to call when SDL was never initialized.
func Shutdown() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if libSDL3 != 0 {
		SDL3_Quit() // implicitly destroys all windows and subsystems
		libSDL3 = 0
	}
}

func SwapSDL3Window() {
	if SDL3_GL_SwapWindow != nil && sdl3Window != nil {
		SDL3_GL_SwapWindow(sdl3Window)
	}
}

var glFlushFunc func()

func GetGL3ProcAddress(name string) uintptr {
	if SDL3_GL_GetProcAddress == nil {
		return 0
	}
	addr := SDL3_GL_GetProcAddress(uintptr(unsafe.Pointer(&[]byte(name + "\x00")[0])))
	return addr
}

// SDL3_ShowWindow makes a window visible
var SDL3_ShowWindowFunc func(unsafe.Pointer)

func SDL3_ShowWindow(window unsafe.Pointer) {
	if SDL3_ShowWindowFunc != nil {
		SDL3_ShowWindowFunc(window)
	}
}

// eventDebug logs SDL-level event details when MEUI_EVENT_DEBUG is set.
// sdl3CString reads a NUL-terminated UTF-8 string SDL allocated. Both text event types carry one, so
// the walk lives in one place (with the same sanity cap).
func sdl3CString(ptr uintptr) string {
	var textBytes []byte
	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			break
		}
		textBytes = append(textBytes, b)
		ptr++
		if len(textBytes) > 4096 {
			break
		}
	}
	return string(textBytes)
}

func eventDebug(format string, args ...any) {
	if os.Getenv("MEUI_EVENT_DEBUG") != "" {
		log.Printf("[sdl3-event] "+format, args...)
	}
}

// pumpSDL3Routed drains the SDL queue, routing each event to the
// registered multi-window it belongs to.
func pumpSDL3Routed() {
	pumpSDLEvents(func(winID uint32, ev Event) {
		if winID == 0 {
			// Global event (quit) — deliver to every open window.
			for _, h := range allSDLWins() {
				if w := h.meui; w != nil {
					w.PushEvent(ev)
				}
			}
			return
		}
		if h := lookupSDLWin(winID); h != nil && h.meui != nil {
			h.meui.PushEvent(ev)
		}
	}, nil)
}

// allSDLWins snapshots the registry (with meui bindings).
func allSDLWins() []*sdlWin {
	winMu.Lock()
	defer winMu.Unlock()
	out := make([]*sdlWin, 0, len(sdlWinReg))
	for _, h := range sdlWinReg {
		out = append(out, h)
	}
	return out
}
