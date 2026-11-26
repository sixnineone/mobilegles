package mobilegles

import (
	"fmt"
	"unsafe"
)

// OS clipboard access through SDL3. SDL_GetClipboardText returns a
// malloc'ed C string the caller must free with SDL_free.

var (
	SDL3_StartTextInput   func(win unsafe.Pointer)
	SDL3_PushEvent        func(ev uintptr) int32
	SDL3_StopTextInput    func(win unsafe.Pointer)
	SDL3_SetClipboardText func(text uintptr) int32
	SDL3_GetClipboardText func() uintptr
	SDL3_free             func(ptr uintptr)
)

// clipboardReady reports whether the SDL3 library is loaded and the
// clipboard functions registered.
func clipboardReady() bool {
	return libSDL3 != 0 && SDL3_SetClipboardText != nil && SDL3_GetClipboardText != nil
}

func cString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	// Read until NUL.
	out := make([]byte, 0, 64)
	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			break
		}
		out = append(out, b)
		ptr++
		if len(out) > 1<<20 { // sanity cap: 1MiB
			break
		}
	}
	return string(out)
}

// SetClipboardText puts text on the system clipboard (SDL3).
func SetClipboardText(text string) {
	if !clipboardReady() {
		return
	}
	p := uintptr(unsafe.Pointer(&[]byte(text + "\x00")[0]))
	SDL3_SetClipboardText(p)
}

// SDL3PushEvent pushes a raw 64-byte SDL3 event into the SDL queue
// (test/diagnostic injection).
func SDL3PushEvent(buf []byte) {
	if SDL3_PushEvent == nil {
		return
	}
	SDL3_PushEvent(uintptr(unsafe.Pointer(&buf[0])))
}

// GetClipboardText reads the system clipboard (SDL3). Empty string when
// unavailable.
func GetClipboardText() string {
	if !clipboardReady() {
		return ""
	}
	ptr := SDL3_GetClipboardText()
	if ptr == 0 {
		return ""
	}
	text := cString(ptr)
	SDL3_free(ptr)
	return text
}

var _ = fmt.Sprintf
