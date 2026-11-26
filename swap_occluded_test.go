package mobilegles

import (
	"testing"
	"unsafe"
)

// A window that another window covers must still be PRESENTED. SDL reports
// OCCLUDED for it, and that flag is advisory and sticky on XWayland: skipping
// the swap turns it into a permanently frozen window — the app keeps clearing
// and drawing into a back buffer nobody presents, so the window keeps showing
// its last presented frame and a later resize exposes that stale buffer as
// garbage. Covering a window must never stop it presenting.
func TestSwapPresentsWhenSDLReportsOccluded(t *testing.T) {
	savedSwap := SDL3_GL_SwapWindowMulti
	savedInterval := SDL3_GL_SetSwapIntervalMulti
	savedFlags := SDL3_GetWindowFlagsMulti
	defer func() {
		SDL3_GL_SwapWindowMulti = savedSwap
		SDL3_GL_SetSwapIntervalMulti = savedInterval
		SDL3_GetWindowFlagsMulti = savedFlags
	}()

	swaps := 0
	SDL3_GL_SwapWindowMulti = func(win unsafe.Pointer) bool { swaps++; return true }
	SDL3_GL_SetSwapIntervalMulti = func(interval int) bool { return true }
	SDL3_GetWindowFlagsMulti = func(win unsafe.Pointer) uint64 {
		return SDL3_WINDOW_OCCLUDED_MULTI
	}

	var win byte
	h := &sdlWin{win: unsafe.Pointer(&win), swapZero: true}
	SwapSDLWin(h)

	if swaps != 1 {
		t.Fatalf("occluded window presented %d times, want exactly 1 — the present was skipped", swaps)
	}
}

// The swap must also stay unconditional when SDL reports no flags at all, so
// the fix cannot be mistaken for "only present when the flag is absent".
func TestSwapPresentsWhenSDLReportsNothing(t *testing.T) {
	savedSwap := SDL3_GL_SwapWindowMulti
	savedInterval := SDL3_GL_SetSwapIntervalMulti
	savedFlags := SDL3_GetWindowFlagsMulti
	defer func() {
		SDL3_GL_SwapWindowMulti = savedSwap
		SDL3_GL_SetSwapIntervalMulti = savedInterval
		SDL3_GetWindowFlagsMulti = savedFlags
	}()

	swaps := 0
	SDL3_GL_SwapWindowMulti = func(win unsafe.Pointer) bool { swaps++; return true }
	SDL3_GL_SetSwapIntervalMulti = func(interval int) bool { return true }
	SDL3_GetWindowFlagsMulti = func(win unsafe.Pointer) uint64 { return 0 }

	var win byte
	h := &sdlWin{win: unsafe.Pointer(&win), swapZero: true}
	SwapSDLWin(h)

	if swaps != 1 {
		t.Fatalf("unoccluded window presented %d times, want exactly 1", swaps)
	}
}
