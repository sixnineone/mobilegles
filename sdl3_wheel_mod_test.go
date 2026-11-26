package mobilegles

// sdl3_wheel_mod_test.go — the wheel event's modifiers.
//
// A wheel event has NO modifier field in SDL: SDL_MouseWheelEvent carries the deltas,
// the window, the mouse and the direction, and nothing else. The pump therefore has to
// QUERY the modifier state when it decodes one. It did not, so every wheel event
// reported "no modifiers" — which silently disabled shift+wheel and ctrl+wheel in every
// app built on this toolkit, with no error anywhere: the toolkit records the event's
// modifiers and the widgets read them, so the feature simply never fired.
//
// This drives the real pump with a synthetic SDL event, which is possible because
// SDL3_PollEvent and SDL3_GetModState are package-level function pointers.

import (
	"testing"
	"unsafe"
)

func TestWheelEventsCarryTheModifierState(t *testing.T) {
	const winID = 4242
	h := &sdlWin{id: winID, w: 800, h: 600}

	winMu.Lock()
	sdlWinReg[winID] = h
	winMu.Unlock()
	defer func() {
		winMu.Lock()
		delete(sdlWinReg, winID)
		winMu.Unlock()
	}()

	// Swap the SDL entry points and the primary-window global for the duration, and put
	// them back whatever happens.
	prevPoll, prevMod, prevWindow := SDL3_PollEvent, SDL3_GetModState, sdl3Window
	defer func() {
		SDL3_PollEvent, SDL3_GetModState, sdl3Window = prevPoll, prevMod, prevWindow
	}()
	sdl3Window = unsafe.Pointer(h)

	// One wheel event, then the queue is empty.
	polls := 0
	SDL3_PollEvent = func(event unsafe.Pointer) bool {
		if polls > 0 {
			return false
		}
		polls++
		buf := unsafe.Slice((*byte)(event), 128)
		for i := range buf {
			buf[i] = 0
		}
		*(*uint32)(unsafe.Pointer(&buf[0])) = SDL3_EVENT_MOUSE_WHEEL
		*(*uint32)(unsafe.Pointer(&buf[16])) = winID
		*(*float32)(unsafe.Pointer(&buf[sdl3OffsetWheelX])) = 0
		*(*float32)(unsafe.Pointer(&buf[sdl3OffsetWheelY])) = 3
		*(*float32)(unsafe.Pointer(&buf[sdl3OffsetWheelMouseX])) = 120
		*(*float32)(unsafe.Pointer(&buf[sdl3OffsetWheelMouseY])) = 80
		return true
	}
	SDL3_GetModState = func() uint16 { return SDL3_KMOD_LSHIFT | SDL3_KMOD_LCTRL }

	var got []Event
	pumpSDLEvents(func(_ uint32, ev Event) { got = append(got, ev) }, nil)

	if len(got) != 1 {
		t.Fatalf("the pump delivered %d events, want 1", len(got))
	}
	ev := got[0]
	if ev.Type != EventMouseWheel {
		t.Fatalf("the event is %v, want a wheel", ev.Type)
	}
	// The deltas still come through.
	if ev.WheelY != 3 || ev.X != 120 || ev.Y != 80 {
		t.Errorf("the wheel event is %+v, want dy=3 at (120,80)", ev)
	}
	// And so do the modifiers, which is the whole point: SDL does not put them in the
	// wheel event, so the pump has to ask.
	if ev.Mod&ModShift == 0 {
		t.Error("the wheel event lost shift: shift+wheel cannot work without it")
	}
	if ev.Mod&ModCtrl == 0 {
		t.Error("the wheel event lost ctrl: ctrl+wheel cannot work without it")
	}

	// With no modifiers held, none are reported — the query is not hardwired on.
	SDL3_GetModState = func() uint16 { return 0 }
	polls = 0
	got = nil
	pumpSDLEvents(func(_ uint32, ev Event) { got = append(got, ev) }, nil)
	if len(got) != 1 {
		t.Fatalf("the second pump delivered %d events, want 1", len(got))
	}
	if got[0].Mod != 0 {
		t.Errorf("the wheel event reports modifiers %v with none held", got[0].Mod)
	}
}
