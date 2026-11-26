package mobilegles

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	libSDL    uintptr
	sdlWindow unsafe.Pointer
	sdlGL     unsafe.Pointer

	SDL_Init              func(uint32) int32
	SDL_Quit              func()
	SDL_CreateWindow      func(uintptr, int32, int32, int32, int32, uint32) unsafe.Pointer
	SDL_DestroyWindow     func(unsafe.Pointer)
	SDL_PollEvent         func(unsafe.Pointer) int32
	SDL_GL_CreateContext  func(unsafe.Pointer) unsafe.Pointer
	SDL_GL_MakeCurrent    func(unsafe.Pointer, unsafe.Pointer)
	SDL_GL_SwapWindow     func(unsafe.Pointer)
	SDL_GL_SetAttribute   func(uint32, int32)
	SDL_GL_GetProcAddress func(uintptr) uintptr
)

const (
	SDL_INIT_VIDEO               = 0x00000020
	SDL_WINDOW_OPENGL            = 0x00000002
	SDL_WINDOW_SHOWN             = 0x00000004
	SDL_KEYDOWN                  = 0x300
	SDL_KEYUP                    = 0x301
	SDL_MOUSEMOTION              = 0x400
	SDL_MOUSEBUTTONDOWN          = 0x401
	SDL_MOUSEBUTTONUP            = 0x402
	SDL_QUIT                     = 0x100
	SDL_WINDOWEVENT              = 0x200
	SDL_GL_CONTEXT_MAJOR_VERSION = 0x1
	SDL_GL_CONTEXT_MINOR_VERSION = 0x2
	SDL_GL_CONTEXT_PROFILE_MASK  = 0x3
	SDL_GL_CONTEXT_PROFILE_ES    = 0x2
)

func initSDL() error {
	var err error
	libSDL, err = purego.Dlopen("libSDL2-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}

	purego.RegisterLibFunc(&SDL_Init, libSDL, "SDL_Init")
	purego.RegisterLibFunc(&SDL_Quit, libSDL, "SDL_Quit")
	purego.RegisterLibFunc(&SDL_CreateWindow, libSDL, "SDL_CreateWindow")
	purego.RegisterLibFunc(&SDL_DestroyWindow, libSDL, "SDL_DestroyWindow")
	purego.RegisterLibFunc(&SDL_PollEvent, libSDL, "SDL_PollEvent")
	purego.RegisterLibFunc(&SDL_GL_CreateContext, libSDL, "SDL_GL_CreateContext")
	purego.RegisterLibFunc(&SDL_GL_MakeCurrent, libSDL, "SDL_GL_MakeCurrent")
	purego.RegisterLibFunc(&SDL_GL_SwapWindow, libSDL, "SDL_GL_SwapWindow")
	purego.RegisterLibFunc(&SDL_GL_SetAttribute, libSDL, "SDL_GL_SetAttribute")
	purego.RegisterLibFunc(&SDL_GL_GetProcAddress, libSDL, "SDL_GL_GetProcAddress")

	return nil
}

func createSDLWindow(title string, width, height int32) error {
	if SDL_Init == nil {
		if err := initSDL(); err != nil {
			return err
		}
	}

	// Request an OpenGL ES 2.0 context for better compatibility with mobile
	SDL_GL_SetAttribute(0x1, 2)   // major version 2
	SDL_GL_SetAttribute(0x2, 0)   // minor version 0
	SDL_GL_SetAttribute(0x3, 0x2) // profile = ES

	titlePtr := uintptr(unsafe.Pointer(&[]byte(title)[0]))
	sdlWindow = SDL_CreateWindow(titlePtr, 100, 100, width, height, SDL_WINDOW_OPENGL|SDL_WINDOW_SHOWN)
	if sdlWindow == nil {
		return fmt.Errorf("SDL_CreateWindow returned nil")
	}

	sdlGL = SDL_GL_CreateContext(sdlWindow)
	if sdlGL == nil {
		SDL_DestroyWindow(sdlWindow)
		sdlWindow = nil
		return fmt.Errorf("SDL_GL_CreateContext failed")
	}

	SDL_GL_MakeCurrent(sdlWindow, sdlGL)

	// Load GL function pointers after context creation
	loadGLFunctions()

	return nil
}

func ProcessSDLEvents(win *Window) {
	if SDL_PollEvent == nil || sdlWindow == nil {
		return
	}

	var eventBuf [56]byte

	for {
		ret := SDL_PollEvent(unsafe.Pointer(&eventBuf[0]))
		if ret == 0 {
			break
		}

		eventType := *(*uint32)(unsafe.Pointer(&eventBuf[0]))

		switch eventType {
		case 0x100: // SDL_QUIT
			win.PushEvent(Event{Type: EventQuit})
		case 0x200: // SDL_WINDOWEVENT
			eventData := *(*uint8)(unsafe.Pointer(&eventBuf[12]))
			if eventData == 13 { // SDL_WINDOWEVENT_CLOSE
				win.PushEvent(Event{Type: EventQuit})
			}
		case 0x300: // SDL_KEYDOWN
			key := *(*int32)(unsafe.Pointer(&eventBuf[16]))
			win.PushEvent(Event{Type: EventKeyDown, Key: mapSDLKey(key)})
		case 0x301: // SDL_KEYUP
			key := *(*int32)(unsafe.Pointer(&eventBuf[16]))
			win.PushEvent(Event{Type: EventKeyUp, Key: mapSDLKey(key)})
		case 0x400: // SDL_MOUSEMOTION
			x := *(*int32)(unsafe.Pointer(&eventBuf[20]))
			y := *(*int32)(unsafe.Pointer(&eventBuf[24]))
			win.PushEvent(Event{Type: EventMouseMove, X: float32(x), Y: float32(y)})
		case 0x401: // SDL_MOUSEBUTTONDOWN
			button := *(*uint8)(unsafe.Pointer(&eventBuf[16]))
			x := *(*int32)(unsafe.Pointer(&eventBuf[20]))
			y := *(*int32)(unsafe.Pointer(&eventBuf[24]))
			win.PushEvent(Event{Type: EventMouseDown, X: float32(x), Y: float32(y), Button: int(button)})
		case 0x402: // SDL_MOUSEBUTTONUP
			button := *(*uint8)(unsafe.Pointer(&eventBuf[16]))
			x := *(*int32)(unsafe.Pointer(&eventBuf[20]))
			y := *(*int32)(unsafe.Pointer(&eventBuf[24]))
			win.PushEvent(Event{Type: EventMouseUp, X: float32(x), Y: float32(y), Button: int(button)})
		}
	}
}

func mapSDLKey(key int32) Keycode {
	switch key {
	case 41:
		return KeyEscape
	case 40:
		return KeyEnter
	case 82:
		return KeyUp
	case 81:
		return KeyDown
	case 80:
		return KeyLeft
	case 79:
		return KeyRight
	default:
		return Keycode(key)
	}
}

func SwapSDLWindow() {
	if SDL_GL_SwapWindow != nil && sdlWindow != nil {
		SDL_GL_SwapWindow(sdlWindow)
	}
}

// GetGLProcAddress retrieves an OpenGL function pointer from the SDL context
func GetGLProcAddress(name string) uintptr {
	if SDL_GL_GetProcAddress != nil {
		return SDL_GL_GetProcAddress(uintptr(unsafe.Pointer(&[]byte(name + "\x00")[0])))
	}
	return 0
}
