package mobilegles

import (
	"fmt"
	"runtime"
	"sync"
)

// EventType represents different event types
type EventType int

const (
	EventNone               EventType = iota
	EventQuit                         // window close requested
	EventTextEditing                  // IME composition in progress (not committed)
	EventResize                       // window resized (Width, Height)
	EventMouseDown                    // mouse button pressed
	EventMouseUp                      // mouse button released
	EventMouseMove                    // mouse moved
	EventMouseWheel                   // scroll wheel (WheelX, WheelY)
	EventKeyDown                      // physical key pressed
	EventKeyUp                        // physical key released
	EventTextInput                    // UTF-8 character input (Rune)
	EventTouchDown                    // finger touched screen (TouchID, X, Y)
	EventTouchUp                      // finger lifted
	EventTouchMove                    // finger moved
	EventTouchCancel                  // touch cancelled
	EventBack                         // the OS asked to go back (Android back gesture/button)
	EventJoystickAxis                 // joystick axis (JoystickID, Axis, Value)
	EventJoystickButtonDown           // joystick button pressed
	EventJoystickButtonUp             // joystick button released
	EventJoystickHat                  // joystick hat moved (JoystickID, Hat, Value)
	EventFocusGained                  // window/app gained focus
	EventFocusLost                    // window/app lost focus
	EventAppPause                     // mobile: app backgrounded
	EventAppResume                    // mobile: app foregrounded
	EventFileDrop                     // file dropped on window (Files)
)

// Mouse buttons
const (
	ButtonLeft   = 1
	ButtonRight  = 2
	ButtonMiddle = 3
)

// Keycode uses USB HID scancode values (subset)
type Keycode int

const (
	KeyUnknown    Keycode = 0
	KeyEnter      Keycode = 40
	KeyEscape     Keycode = 41
	KeyBackspace  Keycode = 42
	KeyTab        Keycode = 43
	KeySpace      Keycode = 44
	KeyLeftShift  Keycode = 225
	KeyRightShift Keycode = 229
	KeyLeftCtrl   Keycode = 224
	KeyRightCtrl  Keycode = 228
	KeyLeftAlt    Keycode = 226
	KeyRightAlt   Keycode = 230
	KeyLeftMeta   Keycode = 227
	KeyRightMeta  Keycode = 231
	KeyUp         Keycode = 200
	KeyDown       Keycode = 208
	KeyLeft       Keycode = 203
	KeyRight      Keycode = 205
	KeyA          Keycode = 4
	KeyB          Keycode = 5
	KeyC          Keycode = 6
	KeyD          Keycode = 7
	KeyE          Keycode = 8
	KeyF          Keycode = 9
	KeyG          Keycode = 10
	KeyH          Keycode = 11
	KeyI          Keycode = 12
	KeyJ          Keycode = 13
	KeyK          Keycode = 14
	KeyL          Keycode = 15
	KeyM          Keycode = 16
	KeyN          Keycode = 17
	KeyO          Keycode = 18
	KeyP          Keycode = 19
	KeyQ          Keycode = 20
	KeyR          Keycode = 21
	KeyS          Keycode = 22
	KeyT          Keycode = 23
	KeyU          Keycode = 24
	KeyV          Keycode = 25
	KeyW          Keycode = 26
	KeyX          Keycode = 27
	KeyY          Keycode = 28
	KeyZ          Keycode = 29
	Key0          Keycode = 39
	Key1          Keycode = 30
	Key2          Keycode = 31
	Key3          Keycode = 32
	Key4          Keycode = 33
	Key5          Keycode = 34
	Key6          Keycode = 35
	Key7          Keycode = 36
	Key8          Keycode = 37
	Key9          Keycode = 38
)

// KeyMod represents modifier key flags
type KeyMod int

// Modifiers helper methods (unison compatibility).
func (m KeyMod) ShiftDown() bool   { return m&ModShift != 0 }
func (m KeyMod) ControlDown() bool { return m&ModCtrl != 0 }
func (m KeyMod) AltDown() bool     { return m&ModAlt != 0 }
func (m KeyMod) MetaDown() bool    { return m&ModMeta != 0 }

// OptionDown is an alias for AltDown. "Option" is the Mac keyboard
// name for the Alt key; meui code uses both names.
func (m KeyMod) OptionDown() bool { return m&ModAlt != 0 }

const (
	ModNone  KeyMod = 0
	ModShift KeyMod = 1 << 0
	ModCtrl  KeyMod = 1 << 1
	ModAlt   KeyMod = 1 << 2
	ModMeta  KeyMod = 1 << 3
	// The KEYPAD, named. The toolkit had NO keypad constants at all, so every keypad key arrived as a bare
	// scancode and no widget could bind one. These are the USB HID usage IDs, which is what SDL3 reports.
	// The keypad DIGITS agree between SDL3 and HID (89-99), which is why they passed through
	// correctly for years. The OPERATORS are not in SDL's scancode set at all, so their HID usages come
	// from the USB HID table — and the first version of these constants was written from memory and was
	// wrong for four of the six: `TestTheWholeKeyboardMapsToDistinctToolkitKeycodes` caught it as four
	// keys collapsing onto one, which is precisely the "the wrong character appeared" symptom.
	KeypadSlash    Keycode = 75  // 0x4B
	KeypadAsterisk Keycode = 85  // 0x55
	KeypadMinus    Keycode = 78  // 0x4E
	KeypadPlus     Keycode = 87  // 0x57
	KeypadEnter    Keycode = 88  // 0x58
	KeypadPercent  Keycode = 104 // 0x68
	KeypadDot      Keycode = 99
	Keypad0        Keycode = 98
	Keypad1        Keycode = 89
	Keypad2        Keycode = 90
	Keypad3        Keycode = 91
	Keypad4        Keycode = 92
	Keypad5        Keycode = 93
	Keypad6        Keycode = 94
	Keypad7        Keycode = 95
	Keypad8        Keycode = 96
	Keypad9        Keycode = 97
	KeypadEquals   Keycode = 103 // 0x67
	KeypadComma    Keycode = 126 // 0x7E

	// The PUNCTUATION of the main row. SDL3 and HID agree on all of them (47-56), so nothing has to be
	// remapped — they simply had no NAME, and a key the toolkit cannot name arrives as KeyUnknown,
	// which no widget can bind (and which is why Ctrl+/ , Ctrl+[ and Ctrl+\ were dead in the editor).
	// These are the USB HID usage IDs, which is what SDL3 reports.
	KeyOpenBracket  Keycode = 47 // [{
	KeyCloseBracket Keycode = 48 // ]}
	KeyBackslash    Keycode = 49 // \|
	KeySemicolon    Keycode = 51 // ;:
	KeyApostrophe   Keycode = 52 // '"
	KeyGrave        Keycode = 53 // `~
	KeyComma        Keycode = 54 // ,<
	KeyPeriod       Keycode = 55 // .>
	KeySlash        Keycode = 56 // /?
	KeyMinus        Keycode = 45 // -_
	KeyEqual        Keycode = 46 // =+

	// The NAVIGATION cluster, named. SDL3 numbers these 73-78 while USB HID numbers them in the 199-214
	// block, so they are the keys where the two schemes diverge — and where the bare pass-through
	// delivered a number that means nothing. A text box's Delete key was arriving as 76.
	KeyInsert   Keycode = 210
	KeyHome     Keycode = 199
	KeyPageUp   Keycode = 211
	KeyDelete   Keycode = 212
	KeyEnd      Keycode = 213
	KeyPageDown Keycode = 214
	KeyPause    Keycode = 215

	// The FUNCTION keys, named. HID F1-F12 are 58-69, which coincides with SDL3's numbering, so these were
	// never wrong — but they were unnamed, so a binding had to write a literal.
	KeyF1  Keycode = 58
	KeyF2  Keycode = 59
	KeyF3  Keycode = 60
	KeyF4  Keycode = 61
	KeyF5  Keycode = 62
	KeyF6  Keycode = 63
	KeyF7  Keycode = 64
	KeyF8  Keycode = 65
	KeyF9  Keycode = 66
	KeyF10 Keycode = 67
	KeyF11 Keycode = 68
	KeyF12 Keycode = 69

	// ModCaps, ModNum and ModScroll are LOCKS, not modifiers: they stay on while other keys are released.
	//
	// They are here because the keypad DEPENDS on them — keypad 1 is '1' with NumLock on and End with it
	// off — and a toolkit that cannot report the lock state cannot tell those two apart. ModCaps was
	// declared here long before anything ever SET it: the pump decoded only the four held modifiers, so
	// the constant was exported, re-exported through platform, and promised a capability the toolkit did
	// not deliver. `TestTheLockKeyStateIsCarried` is what caught that.
	ModCaps   KeyMod = 1 << 4
	ModNum    KeyMod = 1 << 5
	ModScroll KeyMod = 1 << 6
)

// Touch represents a single touch point
type Touch struct {
	ID       int64
	X, Y     float32
	Pressure float32
}

// Event represents an input event
type Event struct {
	Type EventType

	// Window
	Width, Height int32 // for EventResize

	// Mouse
	X, Y           float32
	DX, DY         float32 // relative motion
	Button         int     // ButtonLeft/Right/Middle
	WheelX, WheelY float32

	// Keyboard
	Key Keycode
	Mod KeyMod
	// Repeat is set on a key-down that the OS generated by auto-repeat, not by a fresh press. It is
	// SEPARATE from the key deliberately: a widget that acts on a key press must not act again every time
	// the user holds it, and a widget that wants the repeats (a text field, a scrub slider) needs to know
	// which is which. Nothing in the tree set this before — platform.Event declared and documented a
	// Repeat field that no pump ever filled.
	Repeat bool
	Rune   rune // decoded character for EventTextInput

	// Text editing (IME composition): the in-progress text, and the IME's selection inside it
	// (byte offsets). TEXT_INPUT is the commit; this is what to show until then.
	Text       string
	CompStart  int
	CompLength int

	// Touch
	TouchID int64
	Touches []Touch

	// Joystick
	JoystickID int
	Axis       int
	Value      float32
	Hat        int

	// Misc
	Files []string
}

// EventSource is the interface for any input device or event producer.
// Each source produces typed Event values via Poll().
// Sources are registered on EventQueue and polled automatically.
type EventSource interface {
	Poll() []Event
	Name() string
}

// EventQueue is the central hub for event processing.
// Sources push events to it; the app polls from it.
type EventQueue struct {
	mu      sync.Mutex
	events  []Event
	sources []EventSource
	dummy   [256]Event // inline ring buffer backing
	head    int
	tail    int
}

// NewEventQueue creates an event queue with a 256-event ring buffer.
func NewEventQueue() *EventQueue {
	return &EventQueue{}
}

// RegisterSource adds an event source. Sources are polled in registration order.
func (q *EventQueue) RegisterSource(src EventSource) {
	q.mu.Lock()
	q.sources = append(q.sources, src)
	q.mu.Unlock()
}

// PollSources drains all registered sources and queues their events.
func (q *EventQueue) PollSources() {
	q.mu.Lock()
	srcs := q.sources
	q.mu.Unlock()
	for _, src := range srcs {
		events := src.Poll()
		for _, e := range events {
			q.push(e)
		}
	}
}

// Poll returns the next event, or EventNone if the queue is empty.
func (q *EventQueue) Poll() Event {
	q.mu.Lock()
	if q.head == q.tail {
		q.mu.Unlock()
		return Event{Type: EventNone}
	}
	e := q.dummy[q.tail]
	q.tail = (q.tail + 1) % len(q.dummy)
	q.mu.Unlock()
	return e
}

// PollEvents drains all queued events, dispatches to window state,
// and calls handler for each event. Returns true if any were processed.
func (w *Window) PollEvents(handler func(Event)) bool {
	processed := false
	for {
		e := w.queue.Poll()
		if e.Type == EventNone {
			break
		}
		processed = true
		w.dispatch(e)
		handler(e)
	}
	return processed
}

// PushEvent adds a synthetic event (e.g., from tests).
func (q *EventQueue) PushEvent(e Event) {
	q.push(e)
}

func (q *EventQueue) push(e Event) {
	q.mu.Lock()
	next := (q.head + 1) % len(q.dummy)
	if next != q.tail {
		q.dummy[q.head] = e
		q.head = next
	}
	q.mu.Unlock()
}

// Window represents a platform window
type Window struct {
	width, height  int32
	running        bool
	queue          *EventQueue
	mouseX, mouseY float32
	mouseButtons   [4]bool
	keys           [256]bool

	// Request/ack matching for geometry (see EffectiveSize): reqNano
	// stamps our last move/resize request, ackNano + ackW/ackH the last
	// ConfigureNotify ack. While req is newer, the optimistic cache is
	// authoritative; once acks catch up, ground truth is adopted.
	reqNano    int64
	ackNano    int64
	ackW, ackH int32

	// OwnsEventPump: this window's RunFrame drives the global SDL pump
	// (exactly one window per process).
	OwnsEventPump bool

	taskMu       sync.Mutex
	pendingTasks []func()

	// sdl is set for windows created through OpenWindow (multi-window
	// path). Nil for the legacy primary window, which uses the globals.
	sdl *sdlWin
}

// RegisterTask queues f to run once on the render thread at the start
// of the next frame — the safe way to defer work (e.g. opening a new
// window) out of an event callback.
func (w *Window) RegisterTask(f func()) {
	if f == nil {
		return
	}
	w.taskMu.Lock()
	w.pendingTasks = append(w.pendingTasks, f)
	w.taskMu.Unlock()
}

// RunPendingTasks executes and clears queued render-thread tasks.
func (w *Window) RunPendingTasks() {
	w.taskMu.Lock()
	tasks := w.pendingTasks
	w.pendingTasks = nil
	w.taskMu.Unlock()
	for _, f := range tasks {
		f()
	}
}

// Global window instance
var (
	mainWindow *Window
	windowOnce sync.Once
	initMu     sync.Mutex

	// initWindowErr is why the last InitWindow could not open an OS window.
	//
	// InitWindow cannot report it the usual way: its signature predates error
	// returns, and its callers — meui-gl's window.New among them — treat a
	// non-nil window as success. So the reason is kept here and read through
	// InitWindowError rather than thrown away. Without it, a missing
	// libSDL3.so, an unavailable display or a failed GL context all came out
	// as "a window exists", and the app started, painted nothing and exited 0
	// with no diagnostic at all.
	initWindowErr error
)

// InitWindow initializes the main window
func InitWindow(width, height int32) *Window {
	windowOnce.Do(func() {
		mainWindow = &Window{
			width:   width,
			height:  height,
			running: true,
			queue:   NewEventQueue(),
		}
		SetScreenSize(float32(width), float32(height))
	})

	// Try to create SDL3 window first, fall back to SDL2
	err3 := createSDL3Window("MeUi-Mobile SDL3", width, height)
	if err3 == nil {
		// Adopt the REAL pixel size (display scaling may scale the
		// requested size).
		if primaryHandle != nil {
			rw, rh := QuerySDLWinSize(primaryHandle)
			mainWindow.width, mainWindow.height = rw, rh
		}
	} else if err2 := createSDLWindow("MeUi-Mobile", width, height); err2 == nil {
		err3 = nil
	} else if runtime.GOOS != "android" {
		// Both backends refused. On Android this is not a failure: mainWindow
		// is already created above and the EGL context arrives from the
		// activity's surface via android.SetWindow(), so the reason is only
		// recorded where the platform itself owns the window.
		err3 = fmt.Errorf("SDL3: %v; SDL2: %v", err3, err2)
	} else {
		err3 = nil
	}

	initMu.Lock()
	initWindowErr = err3
	initMu.Unlock()

	return mainWindow
}

// InitWindowError reports why the platform window could not be created, or nil
// when it was. It answers for the most recent InitWindow call.
func InitWindowError() error {
	initMu.Lock()
	defer initMu.Unlock()
	return initWindowErr
}

// ProcessEvents polls all event sources and dispatches events to window state.
// Only the designated pump owner drives the global SDL queue — with
// multiple windows each on its own thread, SDL_PollEvent must be
// called from exactly one place (events are routed by windowID).
func ProcessEvents(win *Window) {
	win.RunPendingTasks() // deferred render-thread work first

	if win != nil && win.OwnsEventPump {
		if sdl3Window != nil {
			PumpAll()
		} else {
			ProcessSDLEvents(win)
		}
	}
	// Poll any additional registered sources
	win.queue.PollSources()
}

// RunFrameTasks drains per-window deferred tasks and registered event
// sources — called by render loops that do NOT own the global SDL pump
// (the dedicated pump goroutine routes SDL events by windowID).
// RunFrameTasks drains per-window deferred tasks, registered event
// sources, and — when this window owns the SDL pump — the platform
// event queue (routed to per-window queues by windowID).
func (w *Window) RunFrameTasks() {
	w.RunPendingTasks()
	if w.OwnsEventPump {
		if sdl3Window != nil {
			PumpAll()
		} else {
			ProcessSDLEvents(w)
		}
	}
	w.queue.PollSources()
}

// PumpAll drains the platform event queue once, routing each event to
// the window that owns it.
func PumpAll() {
	if sdl3Window != nil {
		pumpSDL3Routed()
	} else {
		ProcessSDLEvents(mainWindow)
	}
}

// OpenWindowNamed opens an ADDITIONAL OS window with its own GL
// context (sharing the primary renderer state) bound to a fresh
// mobilegles Window. The renderer must already be initialized (call
// InitRenderer after the first window exists). Events are routed to
// this window by SDL windowID; Swap presents this window's buffer.
//
// The window+context are created ON THE PRIMARY'S THREAD via a render
// task: SDL's share attribute shares with the context current on the
// CALLING thread, so creation must happen where ctx1 is current. The
// new context is then bound on THIS (the caller's) thread — its
// permanent home for the frame loop.
func OpenWindowNamed(title string, width, height int32) (*Window, error) {
	if mainWindow == nil {
		return nil, fmt.Errorf("OpenWindowNamed: no primary window")
	}
	type created struct {
		h   *sdlWin
		err error
	}
	done := make(chan created, 1)
	mainWindow.RegisterTask(func() {
		h, err := CreateSDLWindowMulti(title, width, height)
		done <- created{h, err}
	})
	res := <-done
	if res.err != nil {
		return nil, res.err
	}
	h := res.h
	// Bind the new context on THIS thread — it was unbound on the
	// primary's thread after creation, so it migrates cleanly here.
	if !MakeCurrentSDLWin(h) {
		DestroySDLWin(h)
		return nil, fmt.Errorf("OpenWindowNamed: bind new context on caller thread failed")
	}
	rw, rh := QuerySDLWinSize(h) // real pixel size (display scaling)
	w := &Window{
		width:   rw,
		height:  rh,
		running: true,
		queue:   NewEventQueue(),
		sdl:     h,
	}
	h.meui = w
	return w, nil
}

// OpenWindowNamedInline creates an ADDITIONAL window directly on the calling
// thread, instead of queueing the work onto the primary's task queue and
// waiting for it the way OpenWindowNamed does.
//
// It must be called on the UI thread, with the primary context current there —
// which is where App.CreateWindow runs (from main). OpenWindowNamed cannot be
// used from that thread: it blocks on a task that only that same thread could
// drain, so it deadlocks. Creating inline also keeps the new context on the UI
// thread, which is what lets every window be painted from one thread.
func OpenWindowNamedInline(title string, width, height int32) (*Window, error) {
	if mainWindow == nil {
		return nil, fmt.Errorf("OpenWindowNamedInline: no primary window")
	}
	h, err := CreateSDLWindowMulti(title, width, height)
	if err != nil {
		return nil, err
	}
	if !MakeCurrentSDLWin(h) {
		DestroySDLWin(h)
		return nil, fmt.Errorf("OpenWindowNamedInline: MakeCurrent on the UI thread failed")
	}
	rw, rh := QuerySDLWinSize(h)
	w := &Window{
		width:   rw,
		height:  rh,
		running: true,
		queue:   NewEventQueue(),
		sdl:     h,
	}
	h.meui = w
	return w, nil
}

// Destroy tears down this window's OS surface and GL context and
// removes it from the registry. Must be called on the window's OWN
// thread (its context is current there) — i.e., from the window's
// frame-loop goroutine after the loop exits. A window closed without
// Destroy leaks its SDL surface and keeps receiving routed events.
func (w *Window) Destroy() {
	if w.sdl != nil {
		DestroySDLWin(w.sdl)
		w.sdl = nil
	}
	w.running = false
}

// Minimize/Maximize/Restore drive the OS window state (title-bar buttons).
func (w *Window) Minimize() { MinimizeSDLWin(w.osHandle()) }
func (w *Window) Maximize() { MaximizeSDLWin(w.osHandle()) }
func (w *Window) Restore()  { RestoreSDLWin(w.osHandle()) }

// ToggleMaximize maximizes the window, or restores it when already
// maximized (title-bar maximize-button semantics).
func (w *Window) ToggleMaximize() { ToggleMaximizeSDLWin(w.osHandle()) }

// Maximized reports whether the OS window is currently maximized.
func (w *Window) Maximized() bool { return WindowMaximized(w) }

// osHandle returns the SDL window this mobilegles window drives: its own
// additional-window handle, or the process primary's when it IS the primary.
// The primary's handle lives in primaryHandle rather than in w.sdl, so every
// method reaching for OS window state has to go through here — otherwise it
// silently no-ops on the primary (both the title and Raise did).
func (w *Window) osHandle() *sdlWin {
	if w != nil && w.sdl != nil {
		return w.sdl
	}
	return primaryHandle
}

// SetTitle updates the OS title bar text.
func (w *Window) SetTitle(title string) { SetSDLWinTitle(w.osHandle(), title) }

// Raise brings the window to the front of the stacking order.
func (w *Window) Raise() { RaiseSDLWindow(w.osHandle()) }

// Swap presents THIS window's back buffer.
//
// It must go through osHandle(), not w.sdl: the PRIMARY window's handle lives in
// primaryHandle, so testing w.sdl alone sent the primary down the legacy
// SwapWindow() path, which presents a process-global window rather than its own.
// In a multi-window session that meant the primary's frames were never presented
// to the primary's surface — it kept showing one stale frame while other windows
// presented correctly. (The same nil-w.sdl trap made SetTitle/Raise/window-state
// no-ops until they were routed through osHandle too.)
func (w *Window) Swap() {
	flushBatches()
	if h := w.osHandle(); h != nil {
		SwapSDLWin(h)
		return
	}
	SwapWindow()
}

// MakeCurrent binds THIS window's GL context, via osHandle() for the same reason
// Swap does. Returns false when the bind failed — the caller must skip
// drawing/swap for this window.
func (w *Window) MakeCurrent() bool { return MakeCurrentSDLWin(w.osHandle()) }

// PumpEvents polls the platform event sources and routes each event to
// the window that owns it (multi-window) or to the given window
// (legacy). Returns after all pending events are drained.
func PumpEvents(fallback *Window) {
	if fallback != nil && fallback.sdl != nil {
		pumpSDL3Routed()
		return
	}
	// Legacy: all events go to the single window.
	if fallback != nil {
		ProcessEvents(fallback)
	}
}

// ProcessEventsAndDispatch polls event sources and dispatches to window state.
func ProcessEventsAndDispatch(win *Window) {
	win.queue.PollSources()
	for {
		e := win.queue.Poll()
		if e.Type == EventNone {
			break
		}
		win.dispatch(e)
	}
}

// dispatch handles state tracking for events.
func (w *Window) dispatch(e Event) {
	switch e.Type {
	case EventQuit:
		w.running = false
	case EventMouseDown:
		if e.Button >= 1 && e.Button <= 3 {
			w.mouseButtons[e.Button-1] = true
		}
		w.mouseX, w.mouseY = e.X, e.Y
	case EventMouseUp:
		if e.Button >= 1 && e.Button <= 3 {
			w.mouseButtons[e.Button-1] = false
		}
	case EventMouseMove:
		w.mouseX, w.mouseY = e.X, e.Y
	case EventKeyDown:
		if e.Key >= 0 && int(e.Key) < 256 {
			w.keys[e.Key] = true
		}
	case EventKeyUp:
		if e.Key >= 0 && int(e.Key) < 256 {
			w.keys[e.Key] = false
		}
	}
}

// PushEvent adds a synthetic event to the window's event queue.
func (w *Window) PushEvent(e Event) {
	w.queue.PushEvent(e)
}

func (w *Window) Poll() Event {
	return w.queue.Poll()
}

// Events returns the event queue.
func (w *Window) Events() *EventQueue {
	return w.queue
}

// SetSize updates the window's logical size (called on resize events
// so drawing and hit-test geometry track the real OS window size).
func (w *Window) SetSize(width, height int32) {
	w.width, w.height = width, height
	if w.sdl != nil {
		w.sdl.w, w.sdl.h = width, height
	}
}

// Width returns window width
func (w *Window) Width() int32 { return w.width }

// Height returns window height
func (w *Window) Height() int32 { return w.height }

// ShouldClose returns whether the window should close
func (w *Window) ShouldClose() bool { return !w.running }

// Close closes the window
func (w *Window) Close() { w.running = false }

// GetMainWindow returns the main window instance
func GetMainWindow() *Window { return mainWindow }

// MousePos returns the current mouse position
func (w *Window) MousePos() (float32, float32) { return w.mouseX, w.mouseY }

// MouseButtonPressed returns true if the specified mouse button is pressed
func (w *Window) MouseButtonPressed(button int) bool {
	if button >= 1 && button <= 3 {
		return w.mouseButtons[button-1]
	}
	return false
}

// KeyPressed returns true if the specified key is pressed
func (w *Window) KeyPressed(key int) bool {
	if key >= 0 && key < 256 {
		return w.keys[key]
	}
	return false
}

// SwapEGL is set by the Android backend to redirect SwapWindow to eglSwapBuffers.
var SwapEGL func()

// SwapWindow swaps the window buffer (SDL3, SDL2, or EGL).
func SwapWindow() {
	flushBatches()
	if SwapEGL != nil {
		SwapEGL()
	} else if sdl3Window != nil {
		SwapSDL3Window()
	} else {
		SwapSDLWindow()
	}
}
