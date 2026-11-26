package mobilegles

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Multi-window SDL3 support. The first GL context is created normally;
// subsequent contexts are created with SDL_GL_SHARE_WITH_CURRENT_CONTEXT
// so all windows share the renderer's shaders/VBOs/atlas textures. The
// draw loop MakeCurrents each window before drawing and swapping.

// SDL3_GL_SetAttribute values not needed elsewhere.
const (
	// SDL3 SDL_GLAttr enum value 21 (VERIFIED against SDL3 headers —
	// the SDL2-era value 0x5 is DOUBLEBUFFER in SDL3, so context
	// sharing was NEVER enabled and secondary contexts had no shared
	// programs/VBOs/atlas — all their draws were invalid garbage).
	SDL3_GL_SHARE_WITH_CURRENT_CONTEXT uint32 = 21
)

// SDL3 window functions only used by multi-window paths.
var (
	SDL3_GL_SetAttributeMulti     func(attr uint32, value int32)
	SDL3_GetWindowID              func(win unsafe.Pointer) uint32
	SDL3_GL_MakeCurrentMulti      func(win unsafe.Pointer, ctx unsafe.Pointer) bool
	SDL3_GL_SwapWindowMulti       func(win unsafe.Pointer) bool
	SDL3_SetWindowTitleMulti      func(win unsafe.Pointer, title uintptr)
	SDL3_GetWindowSizeMulti       func(win unsafe.Pointer, w, h *int32)
	SDL3_SetWindowSizeMulti       func(win unsafe.Pointer, w, h int32)
	SDL3_SetWindowFullscreenMulti func(win unsafe.Pointer, fullscreen bool) bool
	SDL3_GetErrorMulti            func() uintptr
)

// sdlWin is one OS window + its GL context.
type sdlWin struct {
	win  unsafe.Pointer // SDL_Window*
	ctx  unsafe.Pointer // SDL_GLContext
	id   uint32         // SDL_WindowID (event routing key)
	w, h int32
	meui *Window // back-reference for event routing (set by OpenWindowNamed)

	hitFn frameHitFn // frame-chrome classifier (custom title bar)

	swapZero bool      // swap interval 0 applied to this context
	swaps    int64     // presents this window has completed (MEUI_SWAP_TRACE)
	lastSwap time.Time // when the present count was last reported

	// Last client-relative pointer position, updated from motion and
	// button events. SDL's wheel event mouse_x/y can be stale and — on
	// X11 with server-side decorations — is offset upward by the top
	// frame (~28px here), while motion/button coords are client-correct.
	mouseX, mouseY float32
	haveMouse      bool
}

// wheelPos returns the position a wheel event should be dispatched at:
// the last known client-relative pointer position when available,
// falling back to the wheel event's own coordinates.
func (h *sdlWin) wheelPos(evX, evY float32) (float32, float32) {
	if h != nil && h.haveMouse {
		return h.mouseX, h.mouseY
	}
	return evX, evY
}

var (
	winMu         sync.Mutex
	sdlWinReg     = map[uint32]*sdlWin{} // SDL windowID → handle
	sdlWinSeq     int                    // stable iteration order
	shareAttrOn   bool
	primaryHandle *sdlWin // legacy single-window path (globals)
	currentHandle *sdlWin // context currently bound on the GL thread
)

// loadMultiWindowFuncs registers the extra SDL3 entry points once.
func loadMultiWindowFuncs() {
	if SDL3_GetWindowID != nil {
		return
	}
	purego.RegisterLibFunc(&SDL3_GL_SetAttributeMulti, libSDL3, "SDL_GL_SetAttribute")
	purego.RegisterLibFunc(&SDL3_GetWindowID, libSDL3, "SDL_GetWindowID")
	purego.RegisterLibFunc(&SDL3_GL_MakeCurrentMulti, libSDL3, "SDL_GL_MakeCurrent")
	purego.RegisterLibFunc(&SDL3_GL_SwapWindowMulti, libSDL3, "SDL_GL_SwapWindow")
	purego.RegisterLibFunc(&SDL3_SetWindowTitleMulti, libSDL3, "SDL_SetWindowTitle")
	purego.RegisterLibFunc(&SDL3_GetWindowSizeMulti, libSDL3, "SDL_GetWindowSize")
	purego.RegisterLibFunc(&SDL3_SetWindowSizeMulti, libSDL3, "SDL_SetWindowSize")
	purego.RegisterLibFunc(&SDL3_ShowWindowMulti, libSDL3, "SDL_ShowWindow")
	purego.RegisterLibFunc(&SDL3_RaiseWindowMulti, libSDL3, "SDL_RaiseWindow")
	purego.RegisterLibFunc(&SDL3_SetWindowFullscreenMulti, libSDL3, "SDL_SetWindowFullscreen")
	purego.RegisterLibFunc(&SDL3_GetErrorMulti, libSDL3, "SDL_GetError")
}

// sdlError returns SDL's last error string.
func sdlError() string {
	if SDL3_GetErrorMulti == nil {
		return ""
	}
	ptr := SDL3_GetErrorMulti()
	if ptr == 0 {
		return ""
	}
	out := ""
	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			break
		}
		out += string(b)
		ptr++
		if len(out) > 512 {
			break
		}
	}
	return out
}

// QuerySDLWinSize returns the window's TRUE size in screen pixels
// (display scaling can make it differ from the requested size) and
// records it on the handle.
func QuerySDLWinSize(h *sdlWin) (int32, int32) {
	var w, h2 int32
	if h != nil && SDL3_GetWindowSizeMulti != nil {
		SDL3_GetWindowSizeMulti(h.win, &w, &h2)
		if w != h.w || h2 != h.h {
			eventDebug("window size change: %dx%d -> %dx%d", h.w, h.h, w, h2)
		}
		h.w, h.h = w, h2
	}
	return w, h2
}

// CreateSDLWindowMulti opens a new OS window with its own GL context
// sharing the primary renderer state. Must be called with the GL thread
// locked (callers run inside InitWindow-style setup on the locked
// thread). Returns the registry handle.
func CreateSDLWindowMulti(title string, width, height int32) (*sdlWin, error) {
	if SDL3_Init == nil {
		if err := initSDL3(); err != nil {
			return nil, err
		}
	}
	forceGLXHint() // GLX: EGL multi-window is broken on XWayland (SDL#9072)

	loadMultiWindowFuncs()

	// All contexts after the first share with the current one so the
	// renderer's shaders/VBOs/atlas are valid in every window.
	if !shareAttrOn && sdl3GL != nil {
		SDL3_GL_SetAttributeMulti(SDL3_GL_SHARE_WITH_CURRENT_CONTEXT, 1)
		shareAttrOn = true
	}

	runtime.LockOSThread()

	SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_MAJOR_VERSION, 2)
	SDL3_GL_SetAttribute(SDL3_GL_CONTEXT_MINOR_VERSION, 1)

	titlePtr := uintptr(unsafe.Pointer(&[]byte(title + "\x00")[0]))
	win := SDL3_CreateWindow(titlePtr, width, height,
		SDL3_WINDOW_OPENGL|SDL3_WINDOW_BORDERLESS|SDL3_WINDOW_RESIZABLE)
	if win == nil {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("SDL3_CreateWindow (multi) returned nil")
	}
	// Per-window context sharing the primary's GL objects (programs,
	// VBOs, atlas textures). The GL state-cache invalidation on every
	// MakeCurrent switch makes this safe (the original New Window
	// crash). Per-context swap-interval 0 keeps presents from blocking
	// on occluded windows.
	SDL3_GL_SetAttributeMulti(SDL3_GL_SHARE_WITH_CURRENT_CONTEXT, 1)
	ctx := SDL3_GL_CreateContext(win)
	if ctx == nil {
		SDL3_DestroyWindow(win)
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("SDL3_GL_CreateContext (multi) failed")
	}
	SDL3_GL_MakeCurrentMulti(win, ctx)
	eventDebug("post-create MakeCurrent(win2): %s", sdlError())
	SDL3_StartTextInput(win)

	h := &sdlWin{win: win, ctx: ctx, id: SDL3_GetWindowID(win), w: width, h: height}
	winMu.Lock()
	sdlWinReg[h.id] = h
	currentHandle = h
	winMu.Unlock()
	sdlWinSeq++

	// Restore the PRIMARY's binding ON THIS THREAD. Creation must run
	// on the primary's thread (its ctx1 current) so the share
	// attribute actually shares with ctx1 — SDL shares with the
	// context current on the CALLING thread, and a context created on
	// a thread with no current context silently shares with nothing
	// (fresh isolated context: no programs, VBOs, atlas — every batch
	// draw on it is garbage). After creation ctx1 must be current
	// here again; the new context migrates to its own window thread
	// (OpenWindowNamed binds it there).
	SDL3_GL_MakeCurrentMulti(sdl3Window, sdl3GL)
	InvalidateGLStateCache() // fresh context: rebind all GL state

	eventDebug("multi-window created id=%d win=%v", h.id, win)
	return h, nil
}

// PrimarySDLWinHandle returns the legacy primary window's handle
// (nil before the first window exists). Multi-window features fall
// back to it so the first window gets chrome too.
func PrimarySDLWinHandle() *sdlWin { return primaryHandle }

// PrimarySDLWindowPointer returns the raw SDL_Window* for the primary window, or
// nil when there is none.
//
// The platform adapter needs it to PARENT a native dialog: SDL_ShowOpenFileDialog
// takes the owning window, and on mobile it is required rather than optional.
// The handle's own win field is unexported, so this is the accessor.
func PrimarySDLWindowPointer() unsafe.Pointer {
	if sdl3Window != nil {
		return sdl3Window
	}
	if primaryHandle != nil {
		return primaryHandle.win
	}
	return nil
}

// lookupSDLWin finds the registry entry for an SDL windowID.
func lookupSDLWin(id uint32) *sdlWin {
	winMu.Lock()
	defer winMu.Unlock()
	return sdlWinReg[id]
}

// MakeCurrentSDLWin binds a window's GL context on this thread.
// Falls back to the primary window's context on the legacy path.
// MakeCurrentSDLWin binds the window's context and reports success.
// Returns false when the bind failed — callers must NOT draw/swap for
// the window this frame (mismatched context+drawable hangs XWayland).
func MakeCurrentSDLWin(h *sdlWin) bool {
	if h == nil {
		h = primaryHandle
		if h == nil {
			return true // headless/legacy: nothing to enforce
		}
	}
	if h == nil || SDL3_GL_MakeCurrentMulti == nil {
		return false
	}
	loadSwapExtras() // for the swap-interval clamp below
	// ALWAYS rebind. A process-global "already current" check is
	// meaningless with one thread per window (each thread's context
	// can be silently unbound between frames on XWayland — observed:
	// glDrawElements segfaulting with no current context). Frames are
	// serialized by the render lock, so an explicit unbind+bind per
	// frame is safe and makes every frame self-healing.
	// Unbind via (target window, NULL context) — per SDL#9072 this is
	// the release that works on EGL; (NULL, NULL) is a silent no-op.
	SDL3_GL_MakeCurrentMulti(h.win, nil)
	if ok := SDL3_GL_MakeCurrentMulti(h.win, h.ctx); !ok {
		eventDebug("MakeCurrent win=%v ctx=%v FAILED: %s", h.win, h.ctx, sdlError())
		return false
	}
	currentHandle = h
	applyBaseContextState()  // per-context state (BLEND etc.) — see renderer.go
	InvalidateGLStateCache() // GL state is per-context — rebind all
	// Swap interval is PER-CONTEXT and resets on bind. Interval 0 keeps
	// occluded-window presents moving (SDL#4335 hang class) — the
	// compositor inhibits frame callbacks for invisible windows and a
	// non-zero interval would block forever waiting for them.
	SDL3_GL_SetSwapIntervalMulti(0)
	h.swapZero = true
	return true
}

var (
	SDL3_GL_SetSwapIntervalMulti func(interval int) bool
	SDL3_GetWindowFlagsMulti     func(win unsafe.Pointer) uint64
	SDL3_ShowWindowMulti         func(win unsafe.Pointer)
	SDL3_RaiseWindowMulti        func(win unsafe.Pointer)
	SDL3_SetHintMulti            func(name, value uintptr) bool
)

// SDL_HINT_VIDEO_FORCE_EGL: "0" selects GLX on X11/XWayland. EGL
// multi-window MakeCurrent/swap is broken in SDL3 on XWayland
// (SDL#9072, SDL#10023) — GLX is the classic working pattern.
const SDL3_HINT_VIDEO_FORCE_EGL = "SDL_VIDEO_FORCE_EGL"

func forceGLXHint() {
	if SDL3_SetHintMulti == nil {
		purego.RegisterLibFunc(&SDL3_SetHintMulti, libSDL3, "SDL_SetHint")
	}
	name := uintptr(unsafe.Pointer(&[]byte(SDL3_HINT_VIDEO_FORCE_EGL + "\x00")[0]))
	val := uintptr(unsafe.Pointer(&[]byte("0\x00")[0]))
	SDL3_SetHintMulti(name, val)
}

// SDL_HINT_ANDROID_TRAP_BACK_BUTTON: "1" makes the Android back gesture arrive as a
// key press instead of the system closing the app.
//
// SDL's default is "0" — "Back button will be handled as usual for system" — which
// means the app is closed and can never participate in its own navigation. With it
// trapped, the press shows up as SDL_SCANCODE_AC_BACK, which this package turns into
// EventBack so the window can offer it to the app and close only if nothing handles it.
//
// Harmless on desktop: no SDL backend reads it there.
const SDL3_HINT_ANDROID_TRAP_BACK_BUTTON = "SDL_ANDROID_TRAP_BACK_BUTTON"

func trapBackButtonHint() {
	if SDL3_SetHintMulti == nil {
		purego.RegisterLibFunc(&SDL3_SetHintMulti, libSDL3, "SDL_SetHint")
	}
	name := uintptr(unsafe.Pointer(&[]byte(SDL3_HINT_ANDROID_TRAP_BACK_BUTTON + "\x00")[0]))
	val := uintptr(unsafe.Pointer(&[]byte("1\x00")[0]))
	SDL3_SetHintMulti(name, val)
}

// SDL window flags.
const SDL3_WINDOW_OCCLUDED_MULTI uint64 = 0x0000000000000004

// SDL3_WINDOW_MAXIMIZED — SDL_video.h:
// #define SDL_WINDOW_MAXIMIZED SDL_UINT64_C(0x0000000000000080)
const SDL3_WINDOW_MAXIMIZED uint64 = 0x0000000000000080

// ToggleMaximizeSDLWin maximizes the window, or restores it when it is
// already maximized (title-bar maximize button behavior).
func ToggleMaximizeSDLWin(h *sdlWin) {
	if h == nil {
		h = primaryHandle // legacy single-window path
	}
	if h == nil {
		return
	}
	if SDL3_GetWindowFlagsMulti != nil && h.win != nil &&
		SDL3_GetWindowFlagsMulti(h.win)&SDL3_WINDOW_MAXIMIZED != 0 {
		RestoreSDLWin(h)
		return
	}
	MaximizeSDLWin(h)
}

// WindowMaximized reports whether the OS window is maximized.
func WindowMaximized(w *Window) bool {
	if w == nil || w.sdl == nil || w.sdl.win == nil || SDL3_GetWindowFlagsMulti == nil {
		return false
	}
	return SDL3_GetWindowFlagsMulti(w.sdl.win)&SDL3_WINDOW_MAXIMIZED != 0
}

func loadSwapExtras() {
	if SDL3_GL_SetSwapIntervalMulti != nil {
		return
	}
	purego.RegisterLibFunc(&SDL3_GL_SetSwapIntervalMulti, libSDL3, "SDL_GL_SetSwapInterval")
	purego.RegisterLibFunc(&SDL3_GetWindowFlagsMulti, libSDL3, "SDL_GetWindowFlags")
}

// SwapSDLWin presents a window's back buffer.
//
// The one XWayland safeguard this needs is swap interval 0: a vsync'd swap
// waits for the compositor's frame callback, which never arrives for a fully
// occluded/unmapped window — an infinite block that froze the whole composite
// loop. Forcing interval 0 on the context (below) removes that hazard at its
// source.
//
// It deliberately does NOT skip the present when SDL reports the window
// OCCLUDED. That flag is advisory and on XWayland it is sticky: once the
// window manager or compositor reports a window covered, it can stay set after
// the window is uncovered again. Skipping the swap turns the flag into a frozen
// window: the app keeps clearing and drawing into a back buffer that is never
// presented, so the window keeps showing its last presented frame, and a later
// resize exposes the never-presented buffer as undefined memory (garbage from
// any previous frame) in the newly exposed area. Covering a window with another
// one is therefore enough to freeze it permanently — reported as "all the other
// windows stop working and the Analysis window is the only one that works".
func SwapSDLWin(h *sdlWin) {
	if h == nil || SDL3_GL_SwapWindowMulti == nil {
		return
	}
	loadSwapExtras()
	if !h.swapZero {
		// Swap interval is PER-CONTEXT: disable vsync on this context
		// or the first swap blocks forever when the window is occluded
		// or the compositor withholds frame callbacks.
		vok := SDL3_GL_SetSwapIntervalMulti(0)
		eventDebug("SetSwapInterval(0) win=%v -> ok=%v (%s)", h.win, vok, sdlError())
		h.swapZero = true
	}
	flushBatches()
	if SDL3_GL_SwapWindowMulti(h.win) {
		// MEUI_SWAP_TRACE: one line per second per window. This is the only
		// way to see whether a window that LOOKS frozen is still presenting
		// (fault below us, in the compositor/drawable) or has stopped
		// presenting (fault above us, in the frame loop).
		if os.Getenv("MEUI_SWAP_TRACE") != "" {
			h.swaps++
			if time.Since(h.lastSwap) >= time.Second {
				h.lastSwap = time.Now()
				// ctx and win are the two identities that decide WHERE the pixels go. If
				// two windows report the same ctx, every window's frames are drawn
				// into one buffer (all windows show the last window drawn). If two
				// report the same win, they present the same drawable. Distinct on
				// both, yet the screen frozen, puts the fault below us in the
				// compositor's view of those windows.
				// xwin is the X window these presents land on. Compare it
				// with the window the user actually sees (`xdotool search
				// --name`): if they differ, the app is presenting a drawable
				// nobody is looking at, while the visible window keeps
				// whatever it was last drawn with.
				fmt.Fprintf(os.Stderr, "[swap] id=%d ctx=%p win=%p total=%d\n", h.id, h.ctx, h.win, h.swaps)
			}
		}
	} else {
		// The present reported failure. That is invisible otherwise — the
		// window keeps its last presented frame while input keeps working,
		// which is reported as "the render froze but I can still click".
		reportSwapFailure(h)
	}
	// MEUI_GLFINISH=1: block until THIS window's GL work completes
	// before another window's frame touches the shared VBOs. The batch
	// buffers are shared GL objects across contexts; without sync,
	// window N+1's buffer uploads race window N's in-flight draws
	// (observed: window 2's border line rendered as a garbage triangle
	// built from stale vertex data). Diagnosed via PaintBar bisect.
	if os.Getenv("MEUI_GLFINISH") == "1" && glFinish != nil {
		glFinish()
	}
}

// DestroySDLWin removes a window from the registry and destroys it.
// The caller must not use the handle afterwards.
func DestroySDLWin(h *sdlWin) {
	if h == nil {
		return
	}
	winMu.Lock()
	delete(sdlWinReg, h.id)
	winMu.Unlock()
	SDL3_GL_MakeCurrentMulti(h.win, nil)
	SDL3_DestroyWindow(h.win)
	h.win = nil
	h.ctx = nil
}

// SetSDLWinTitle updates a window's OS title text.
func SetSDLWinTitle(h *sdlWin, title string) {
	if h == nil || SDL3_SetWindowTitleMulti == nil {
		return
	}
	titlePtr := uintptr(unsafe.Pointer(&[]byte(title + "\x00")[0]))
	SDL3_SetWindowTitleMulti(h.win, titlePtr)
}

// minimize/restore/maximize entry points for the WindowFrame buttons.
var (
	SDL3_MinimizeWindow func(win unsafe.Pointer) bool
	SDL3_MaximizeWindow func(win unsafe.Pointer) bool
	SDL3_RestoreWindow  func(win unsafe.Pointer) bool
)

func loadWindowStateFuncs() {
	if SDL3_MinimizeWindow != nil {
		return
	}
	purego.RegisterLibFunc(&SDL3_MinimizeWindow, libSDL3, "SDL_MinimizeWindow")
	purego.RegisterLibFunc(&SDL3_MaximizeWindow, libSDL3, "SDL_MaximizeWindow")
	purego.RegisterLibFunc(&SDL3_RestoreWindow, libSDL3, "SDL_RestoreWindow")
}

// RaiseSDLWindow brings the window to the front.
func RaiseSDLWindow(h *sdlWin) {
	if h == nil {
		h = primaryHandle
	}
	if h != nil {
		loadWindowStateFuncs()
		SDL3_RaiseWindowMulti(h.win)
	}
}

// MinimizeSDLWin / MaximizeSDLWin / RestoreSDLWin drive the window
// state from the custom title bar buttons.
func MinimizeSDLWin(h *sdlWin) {
	if h == nil {
		h = primaryHandle // legacy single-window path
	}
	if h != nil {
		loadWindowStateFuncs()
		ok := SDL3_MinimizeWindow(h.win)
		if !ok {
			eventDebug("MinimizeSDLWin win=%v -> FAILED: %s", h.win, sdlError())
		} else {
			eventDebug("MinimizeSDLWin -> ok")
		}
	}
}

func MaximizeSDLWin(h *sdlWin) {
	if h == nil {
		h = primaryHandle // legacy single-window path
	}
	if h != nil {
		loadWindowStateFuncs()
		ok := SDL3_MaximizeWindow(h.win)
		if !ok {
			eventDebug("MaximizeSDLWin win=%v -> FAILED: %s", h.win, sdlError())
		} else {
			eventDebug("MaximizeSDLWin -> ok")
		}
	}
}

func RestoreSDLWin(h *sdlWin) {
	if h == nil {
		h = primaryHandle // legacy single-window path
	}
	if h != nil {
		loadWindowStateFuncs()
		ok := SDL3_RestoreWindow(h.win)
		if !ok {
			eventDebug("RestoreSDLWin win=%v -> FAILED: %s", h.win, sdlError())
		} else {
			eventDebug("RestoreSDLWin -> ok")
		}
	}
}

// ── Hit testing for custom window frames ────────────────────────────
//
// SDL calls the registered hit-test callback on mouse motion; returning
// DRAGGABLE/RESIZE_* makes the window manager move/resize the window
// natively (works on borderless windows, keeps compositor snapping).

// SDL_HitTestResult values (SDL_video.h).
const (
	SDL_HITTEST_NORMAL         uint32 = 0
	SDL_HITTEST_DRAGGABLE      uint32 = 1
	SDL_HITTEST_RESIZE_TOPLEFT uint32 = 2
	// …through SDL_HITTEST_RESIZE_LEFT = 9
	SDL_HITTEST_RESIZE_LEFT uint32 = 9
)

var (
	SDL3_SetWindowHitTest func(win unsafe.Pointer, callback uintptr, data unsafe.Pointer) bool
	hitTestTramp          uintptr
)

// frameHitFn classifies a window-relative point for the frame chrome.
type frameHitFn func(x, y int32) uint32

func loadHitTestFuncs() {
	if SDL3_SetWindowHitTest != nil {
		return
	}
	purego.RegisterLibFunc(&SDL3_SetWindowHitTest, libSDL3, "SDL_SetWindowHitTest")
	hitTestFired.Store(false)
	hitTestTramp = purego.NewCallback(func(win, pt, data uintptr) uintptr {
		// SDL hands us a C SDL_Point as a uintptr. Index a typed pointer once
		// rather than doing uintptr arithmetic (pt+4): the arithmetic form is
		// flagged by -race's checkptr ("pointer arithmetic result points to
		// invalid allocation"), which is how the P5dc/P5go harness caught it.
		ppt := (*[2]int32)(unsafe.Pointer(pt))
		x, y := ppt[0], ppt[1]
		if !hitTestFired.Swap(true) {
			eventDebug("hit-test callback FIRST FIRE at (%d,%d)", x, y)
		}
		h := (*sdlWin)(unsafe.Pointer(data))
		if h == nil || h.hitFn == nil {
			eventDebug("hit-test (%d,%d) -> NORMAL (no handler)", x, y)
			return uintptr(SDL_HITTEST_NORMAL)
		}
		r := h.hitFn(x, y)
		if os.Getenv("MEUI_HITTEST_DEBUG") != "" {
			fmt.Printf("[hittest] (%d,%d) -> %d btn=%d\n", x, y, r, mouseButtonMask())
		}
		return uintptr(r)
	})
}

// hitTestFired reports (via eventDebug) whether SDL ever invoked the
// callback — diagnoses silent install failures.
var hitTestFired atomic.Bool

// SDL3_GetMouseState reports live button state; the hit-test uses it
// to tell real presses (button down) from cursor-update calls.
var SDL3_GetMouseState func(x, y *float32) uint32

// SDL3_GetCurrentVideoDriver reports the active video backend name.
var SDL3_GetCurrentVideoDriver func() uintptr

// VideoDriver returns the active SDL video backend ("x11",
// "wayland", ...) — cached after the first query. Empty when SDL is
// unavailable (tests, tools).
func VideoDriver() string {
	videoDriverOnce.Do(func() {
		if libSDL3 == 0 {
			return
		}
		purego.RegisterLibFunc(&SDL3_GetCurrentVideoDriver, libSDL3, "SDL_GetCurrentVideoDriver")
		drvPtr := SDL3_GetCurrentVideoDriver()
		if drvPtr == 0 {
			return
		}
		var buf []byte
		for i := uintptr(0); ; i++ {
			b := *(*byte)(unsafe.Pointer(drvPtr + i))
			if b == 0 {
				break
			}
			buf = append(buf, b)
		}
		videoDriverName = string(buf)
	})
	return videoDriverName
}

var (
	videoDriverOnce sync.Once
	videoDriverName string
)

func mouseButtonMask() uint32 {
	if SDL3_GetMouseState == nil {
		if libSDL3 == 0 {
			return 0
		}
		purego.RegisterLibFunc(&SDL3_GetMouseState, libSDL3, "SDL_GetMouseState")
	}
	return SDL3_GetMouseState(nil, nil)
}

// MouseLeftDown reports whether the left button is currently held.
// Ground truth for hit-test press detection (motion calls have the
// button up; a call coinciding with a press has it down). Returns
// false when SDL is unavailable (tests, tools).
func MouseLeftDown() bool {
	return mouseButtonMask()&1 != 0 // SDL_BUTTON_LMASK
}

// SDL3_GetGlobalMouseState reports the asynchronous desktop-relative
// cursor position (immune to window-position feedback, unlike the
// window-relative motion events).
var SDL3_GetGlobalMouseState func(x, y *float32) uint32

// GlobalMousePos returns the desktop-relative cursor position.
// Used for manual window moves so the drag math can't feed back on
// itself while the window is moving underneath the cursor.
func GlobalMousePos() (float32, float32) {
	if SDL3_GetGlobalMouseState == nil {
		if libSDL3 == 0 {
			return 0, 0
		}
		purego.RegisterLibFunc(&SDL3_GetGlobalMouseState, libSDL3, "SDL_GetGlobalMouseState")
	}
	var x, y float32
	SDL3_GetGlobalMouseState(&x, &y)
	return x, y
}

// SetSDLWinHitTestHandler installs a Go classifier for the window's
// chrome regions. fn returns an SDL_HITTEST_* value.
func SetSDLWinHitTestHandler(h *sdlWin, fn frameHitFn) {
	if h == nil {
		return
	}
	loadHitTestFuncs()
	h.hitFn = fn
	ok := SDL3_SetWindowHitTest(h.win, hitTestTramp, unsafe.Pointer(h))
	eventDebug("SetWindowHitTest win=%v -> ok=%v (driver %s)", h.win, ok, VideoDriver())
}

// SetWindowHitHandler installs a chrome classifier on a mobilegles
// Window (convenience over SetSDLWinHitTestHandler). Falls back to the
// primary window's handle on the legacy single-window path.
func SetWindowHitHandler(w *Window, fn func(x, y int32) uint32) {
	h := w.sdl
	if h == nil {
		h = primaryHandle
	}
	if h == nil {
		return
	}
	SetSDLWinHitTestHandler(h, fn)
}

// Request/ack matching for geometry (replaces wall-clock quiet
// periods, which flap on bursty drag input): while our requests are
// newer than the last ConfigureNotify ack, the optimistic cache is
// fresher than anything SDL can report — adopting the lagging probe
// rewinds every frame (drag jitter/flash). Once acks catch up, ground
// truth (WM constraints, external resizes) is adopted. Quiet fallback:
// no requests for a while with no newer ack either (drag ended without
// a final ack, missed events) → adopt the live probe to converge.
func noteGeometryRequest(w *Window) {
	if w == nil {
		return
	}
	w.reqNano = time.Now().UnixNano()
	if os.Getenv("MEUI_GEODEBUG") != "" {
		fmt.Printf("[geo] req cache=(%d,%d)\n", w.width, w.height)
	}
}

// NoteResizeAck records a ConfigureNotify-acked size for request/ack
// matching. It does NOT touch the cached size — EffectiveSize decides.
func NoteResizeAck(w *Window, width, height int32) {
	if w == nil {
		return
	}
	w.ackNano = time.Now().UnixNano()
	w.ackW, w.ackH = width, height
	if os.Getenv("MEUI_GEODEBUG") != "" {
		fmt.Printf("[geo] ack=(%d,%d) cache=(%d,%d)\n", width, height, w.width, w.height)
	}
}

// ResizeSDLWin sets a window's pixel size (manual edge-resize drag).
func ResizeSDLWin(h *sdlWin, w, hgt int32) {
	if h == nil {
		h = primaryHandle
	}
	if h != nil && SDL3_SetWindowSizeMulti != nil {
		SDL3_SetWindowSizeMulti(h.win, w, hgt)
	}
}

// Edge zone masks (window-relative resize borders).
const (
	EdgeNone  uint32 = 0
	EdgeTop   uint32 = 1 << 0
	EdgeBot   uint32 = 1 << 1
	EdgeLeft  uint32 = 1 << 2
	EdgeRight uint32 = 1 << 3
)

// EdgeZone classifies a window-relative point against the resize
// border (6px). Returns an Edge* bitmask, 0 when not on an edge.
// MUST stay in sync with widgets.FrameEdgeSize (the frame hit-test).
func EdgeZone(x, y, w, h float32) uint32 {
	const es = float32(6)
	var e uint32
	if y >= 0 && y < es {
		e |= EdgeTop
	} else if y > h-es && y <= h {
		e |= EdgeBot
	}
	if x >= 0 && x < es {
		e |= EdgeLeft
	} else if x > w-es && x <= w {
		e |= EdgeRight
	}
	return e
}

// SetSwapInterval0 sets interval 0 on the currently bound context.
func SetSwapInterval0() {
	loadSwapExtras()
	SDL3_GL_SetSwapIntervalMulti(0)
}

// ResizeWindow resizes a mobilegles window's OS surface and updates
// the logical size.
func ResizeWindow(w *Window, width, height int32) {
	if w == nil {
		return
	}
	noteGeometryRequest(w)
	ResizeSDLWin(w.sdl, width, height)
	w.SetSize(width, height)
}

var SDL3_GetWindowSizeInPixelsMulti func(win unsafe.Pointer, w, h *int32)

// WindowSize returns the window's TRUE current pixel size — ground
// truth for layout (queried live; resize events can lag/misreport).
func WindowSize(w *Window) (int32, int32) {
	if w == nil {
		return 0, 0
	}
	// osHandle, not w.sdl: the PRIMARY window's handle lives in the
	// package-level primaryHandle (mainWindow.sdl is never assigned — see
	// InitWindow), so reading w.sdl here returned 0,0 for the primary.
	// EffectiveSize skips its probe branch on a zero size, which left the
	// primary with only the ack fallback — and that fallback is disqualified
	// as soon as the session has issued one client resize (w.reqNano != 0).
	// The result was a primary window that stopped adopting WM-driven
	// geometry changes such as maximize after any drag: the window maximized
	// and the render stayed at the old size.
	return QuerySDLWinSize(w.osHandle())
}

// SizeProbe logs every size notion SDL and GL have — diagnostic for
// coordinate-space mismatches (logical vs pixels vs GL viewport).
var lastProbe string

// probeWindowSize is EffectiveSize's ground-truth seam: the live SDL
// size in production, stubbed in tests to pin adoption priority.
var probeWindowSize = WindowSize

// ResetSizeProbe makes the next SizeProbe print unconditionally, so two reads in
// one frame (before and after the clear that sets the viewport) can be told apart.
func ResetSizeProbe() { lastProbe = "" }

func SizeProbe(w *Window) {
	if w == nil {
		return
	}
	if SDL3_GetWindowSizeInPixelsMulti == nil {
		purego.RegisterLibFunc(&SDL3_GetWindowSizeInPixelsMulti, libSDL3, "SDL_GetWindowSizeInPixels")
	}
	h := w.sdl
	if h == nil {
		h = primaryHandle
	}
	if h == nil {
		return
	}
	var lw, lh, pw, ph int32
	SDL3_GetWindowSizeMulti(h.win, &lw, &lh)
	SDL3_GetWindowSizeInPixelsMulti(h.win, &pw, &ph)
	var vp [4]int32
	glGetIntegerv(0x0BA2, uintptr(unsafe.Pointer(&vp[0]))) // GL_VIEWPORT
	probe := fmt.Sprintf("logical=%dx%d pixels=%dx%d viewport=%dx%d+{%d,%d}",
		lw, lh, pw, ph, vp[2], vp[3], vp[0], vp[1])
	if probe != lastProbe {
		eventDebug("size probe: %s", probe)
		lastProbe = probe
	}
}

// MoveSDLWin places the window's top-left corner (left/top resize).
var SDL3_SetWindowPositionMulti func(win unsafe.Pointer, x, y int32)

func loadSetPositionFunc() {
	if SDL3_SetWindowPositionMulti != nil {
		return
	}
	purego.RegisterLibFunc(&SDL3_SetWindowPositionMulti, libSDL3, "SDL_SetWindowPosition")
	purego.RegisterLibFunc(&SDL3_GetWindowPositionMulti, libSDL3, "SDL_GetWindowPosition")
}

var SDL3_GetWindowPositionMulti func(win unsafe.Pointer, x, y *int32)

// WindowPos returns the window's top-left position in screen coords.
func WindowPos(w *Window) (int32, int32) {
	h := w.sdl
	if h == nil {
		h = primaryHandle
	}
	var x, y int32
	if h != nil {
		loadSetPositionFunc()
		SDL3_GetWindowPositionMulti(h.win, &x, &y)
	}
	return x, y
}

// X11 atomic move+resize -------------------------------------------------
// Left/top resizes need origin+size changed together. Sent as two
// separate requests (MoveSDLWinBy + ResizeWindow), KWin can apply them
// in different frames — a 1-frame tear perceived as flicker (right/
// bottom edges send a single request and stay clean). XConfigureWindow
// with CWX|CWY|CWWidth|CWHeight applies all four atomically. Handles
// come from SDL3's window properties; falls back to the split path
// when unavailable (non-X11 backends, missing libX11).

var (
	SDL3_GetWindowProperties func(win unsafe.Pointer) uint32
	SDL3_GetPointerProperty  func(props uint32, name uintptr, def uintptr) uintptr

	libX11Handle         uintptr
	xConfigureWindowFunc func(dpy, win uintptr, mask uint32, changes unsafe.Pointer) int32
	xSyncFunc            func(dpy uintptr, discard int32) int32
)

// The raw-X11 window lookup and the XConfigureWindow/XSync resize it
// supported are gone: resizes go through SDL now, which keeps its GL drawable in
// step with the window. See MoveResizeSDLWin.

// EffectiveSize returns the size to render and lay out at. Three cases:
//   - our requests are newer than the last ack, or very recent:
//     trust the optimistic cache (SDL can only report lagging values
//     mid-drag; adopting them rewinds every frame → jitter/flash);
//   - quiet (no requests for >1s): converge to ground truth. The LIVE
//     PROBE is checked FIRST — during drags, ConfigureNotify events are
//     processed later than requests are sent, so the newest-recorded ack
//     can carry a mid-drag size a few px behind the final request while
//     still looking "newer". Adopting it 1s after release reverts the
//     cache below the real window → the post-drag flash strip. SDL's own
//     cache (the probe) is pumped synchronously and is authoritative once
//     quiet. The ack is only used when the probe AGREES with the cache
//     but the ack differs (SDL's cache lagging its own event — the
//     maximize case, where event data was observed fresher than the
//     cache).
//
// External resizes (maximize button, WM actions) issue no request
// through us, so ground truth is adopted after the quiet period,
// including missed resize events.
func EffectiveSize(w *Window) (int32, int32) {
	if w == nil {
		return 0, 0
	}
	sinceReq := time.Since(time.Unix(0, w.reqNano))
	// MEUI_EFFSIZE_TRACE: what this decides with, and whether the live window
	// disagrees with the cached size it is about to return. A resize the seam did
	// not request (a WM action or maximise) leaves the optimistic cache holding
	// the previous size, and the frame is then rendered at the cached height while
	// the window is the new one - an offset of exactly the resize delta that clears
	// only when the height matches the cache again.
	if os.Getenv("MEUI_EFFSIZE_TRACE") != "" {
		prw, prh := probeWindowSize(w)
		log.Printf("[effsize] cache=%dx%d probe=%dx%d ack=%dx%d sinceReq=%v req=%v",
			w.Width(), w.Height(), prw, prh, w.ackW, w.ackH, sinceReq.Round(time.Millisecond), w.reqNano != 0)
	}
	// Only adopt ground truth after a full second of inactivity.
	// During a drag (even with pauses < 1s), the optimistic
	// cache from MoveResizeSDLWin/ResizeWindow is trusted
	// because intermediate ConfigureNotify events carry stale
	// sizes that would revert the cache and cause a flash strip.
	if sinceReq > time.Second {
		if rw, rh := probeWindowSize(w); rw > 0 && rh > 0 && (rw != w.Width() || rh != w.Height()) {
			if os.Getenv("MEUI_GEODEBUG") != "" {
				fmt.Printf("[geo] adopt probe=(%d,%d) cache=(%d,%d)\n", rw, rh, w.Width(), w.Height())
			}
			w.SetSize(rw, rh)
		} else if w.ackW > 0 && w.ackH > 0 &&
			(w.ackW != w.Width() || w.ackH != w.Height()) &&
			syncWindowState(w.osHandle()) {
			// A resize event says the window is a different size while SDL's own
			// query still agrees with the cache, i.e. the query is lagging the
			// event it delivered. Finalising the pending state and asking again is
			// the only thing that can resolve it: the probe cannot be trusted
			// (it is behind) and the ack cannot be either once a request has been
			// made (it may be a mid-drag value). If the probe still disagrees
			// after the sync, the ack really was stale and the cache is left alone.
			if rw, rh := probeWindowSize(w); rw > 0 && rh > 0 &&
				(rw != w.Width() || rh != w.Height()) {
				if os.Getenv("MEUI_GEODEBUG") != "" {
					fmt.Printf("[geo] adopt after sync=(%d,%d) cache=(%d,%d) ack=(%d,%d)\n",
						rw, rh, w.Width(), w.Height(), w.ackW, w.ackH)
				}
				w.SetSize(rw, rh)
			}
		} else if w.reqNano == 0 && w.ackW > 0 && w.ackH > 0 &&
			(w.ackW != w.Width() || w.ackH != w.Height()) {
			// Pure-external geometry (reqNano==0: this session never
			// issued a client request — e.g. WM maximize on a fresh
			// window). Ack event data is authoritative even when
			// SDL's own cache lags a frame. Any client request in
			// history disqualifies the ack: post-drag acks can be
			// mid-drag sizes with current timestamps (processing
			// lag) and adopting them reverts the cache — the
			// post-drag flash strip.
			if os.Getenv("MEUI_GEODEBUG") != "" {
				fmt.Printf("[geo] adopt ack=(%d,%d) cache=(%d,%d)\n", w.ackW, w.ackH, w.Width(), w.Height())
			}
			w.SetSize(w.ackW, w.ackH)
		}
	}
	return w.Width(), w.Height()
}

// MoveResizeSDLWin moves+resizes in one atomic X11 request. Returns
// false when the atomic path is unavailable (caller falls back to the
// split MoveSDLWinBy + ResizeWindow path).
// SDL3_SyncWindow blocks until SDL has finalised pending window state. It is the
// way out of a lagging size query: SDL can deliver a resize event before its own
// cached size catches up, and while that is true the probe agrees with the stale
// cache while the event holds the truth - the state in which a vertical resize
// leaves the seam's size unchanged and the frame is drawn at the old height.
var SDL3_SyncWindow func(win unsafe.Pointer) bool

// syncWindowState finalises pending window state. The symbol is looked up before
// it is registered, because registering a name this SDL build does not export
// panics the process (SDL_GL_GetDrawableSize did exactly that - it is SDL2-era).
func syncWindowState(h *sdlWin) bool {
	if h == nil {
		return false
	}
	if SDL3_SyncWindow == nil {
		sym, err := purego.Dlsym(libSDL3, "SDL_SyncWindow")
		if err != nil || sym == 0 {
			return false
		}
		purego.RegisterFunc(&SDL3_SyncWindow, sym)
	}
	return SDL3_SyncWindow(h.win)
}

// MoveResizeSDLWin moves and resizes a window through SDL.
//
// This used to issue XConfigureWindow + XSync directly, which paired origin and
// size in one request - but the GLX drawable follows the X window LAZILY, so a
// frame rendered at the new size could land on a surface that had not caught up.
// With GL's origin at the bottom left that offsets the whole frame vertically by
// the resize delta, which is the reported corruption on vertical resizes and on
// maximise. SDL's own calls keep its drawable in step with the window, so the
// raw-X11 path is gone.
func MoveResizeSDLWin(w *Window, x, y, width, height int32) bool {
	if w == nil {
		return false
	}
	h := w.osHandle()
	if h == nil {
		return false
	}
	loadSetPositionFunc()
	if SDL3_SetWindowPositionMulti == nil || SDL3_SetWindowSizeMulti == nil {
		return false
	}
	noteGeometryRequest(w)
	SDL3_SetWindowPositionMulti(h.win, x, y)
	SDL3_SetWindowSizeMulti(h.win, width, height)
	w.SetSize(width, height)
	return true
}

// SetWindowSizeSDLWin asks the OS for a new window SIZE without moving it. The request comes
// back through SDL_EVENT_WINDOW_RESIZED, so the whole resize path runs — drawable,
// viewport, projection and the toolkit's EventResize — exactly as it does when a user
// drags an edge. A host that wants to set a size programmatically (and a test that wants
// to exercise a real resize without a pointer) needs this; MoveResizeSDLWin also moves.
func SetWindowSizeSDLWin(w *Window, width, height int32) bool {
	if w == nil {
		return false
	}
	h := w.osHandle()
	if h == nil {
		return false
	}
	loadSetPositionFunc()
	if SDL3_SetWindowSizeMulti == nil {
		return false
	}
	noteGeometryRequest(w)
	SDL3_SetWindowSizeMulti(h.win, width, height)
	w.SetSize(width, height)
	return true
}

// MoveSDLWinBy repositions the window (used by left/top resize so the
// grabbed corner follows the mouse).
func MoveSDLWinBy(w *Window, x, y int32) {
	if w == nil {
		return
	}
	h := w.sdl
	if h == nil {
		h = primaryHandle
	}
	if h == nil {
		return
	}
	loadSetPositionFunc()
	noteGeometryRequest(w)
	SDL3_SetWindowPositionMulti(h.win, x, y)
}

// SetFullscreenSDLWin toggles desktop-resolution fullscreen.
func SetFullscreenSDLWin(h *sdlWin, fullscreen bool) {
	if h == nil {
		h = primaryHandle
	}
	if h != nil && SDL3_SetWindowFullscreenMulti != nil {
		ok := SDL3_SetWindowFullscreenMulti(h.win, fullscreen)
		eventDebug("SetWindowFullscreen %v -> ok=%v", fullscreen, ok)
	}
}

// SetFullscreen toggles fullscreen on a mobilegles window.
func SetFullscreen(w *Window, fullscreen bool) {
	if w == nil {
		return
	}
	SetFullscreenSDLWin(w.sdl, fullscreen)
}

// prevHandleOf finds the registry handle for a window/context pair
// (the currentHandle bookkeeping after a context restore).
func prevHandleOf(win, ctx unsafe.Pointer) *sdlWin {
	winMu.Lock()
	defer winMu.Unlock()
	for _, h := range sdlWinReg {
		if h.win == win && h.ctx == ctx {
			return h
		}
	}
	return nil
}

// swapFailures counts presents SDL refused.
var swapFailures atomic.Int64

// reportSwapFailure logs a refused present the first time and then rarely, so a
// window that never presents leaves a readable trace instead of a flood.
func reportSwapFailure(h *sdlWin) {
	n := swapFailures.Add(1)
	if n == 1 || n%600 == 0 {
		eventDebug("SwapWindow win=%v refused the present (%d total): %s", h.win, n, sdlError())
		fmt.Fprintf(os.Stderr, "[render] window id=%d present refused (%d total): %s\n", h.id, n, sdlError())
	}
}
