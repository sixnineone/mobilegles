// Package mobilegles provides an OpenGL ES 2.0/3.0 backend for MeUi using purego.
// This enables mobile platform support without CGO dependencies.
//
// FFI: purego is the binding layer today, but the planned replacement is pureffi
// (purego's API on go-webgpu/goffi) - architecture decision D7 in
// meui-migration/design/meui-platform-architecture.md, with the measured trial in
// MIGRATION.md P6a/P6b. Two consequences for anyone touching this package:
//
//   - pureffi works only under CGO_ENABLED=0, and the race detector needs
//     CGO_ENABLED=1; that is why the switch is deferred rather than done; and
//   - do not add a cgo dependency to this module (or to meui) without checking D7 -
//     a third-party cgo package in the graph is what forces that mode.
//
// purego already needs CGO_ENABLED=1 on Android and iOS, and has no Windows
// Dlopen/Dlsym; that gap is what D7 plans to close.
package mobilegles

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	libEGL  uintptr
	libGLES uintptr
	libGL   uintptr
	libGLX  uintptr

	// EGL functions
	eglGetDisplay          func(unsafe.Pointer) unsafe.Pointer
	eglInitialize          func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) int32
	eglChooseConfig        func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, int32, unsafe.Pointer) int32
	eglGetConfigAttrib     func(unsafe.Pointer, unsafe.Pointer, int32, unsafe.Pointer) int32
	eglCreateWindowSurface func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) unsafe.Pointer
	eglDestroySurface      func(unsafe.Pointer, unsafe.Pointer) int32
	eglCreateContext       func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) unsafe.Pointer
	eglDestroyContext      func(unsafe.Pointer, unsafe.Pointer) int32
	eglMakeCurrent         func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) int32
	eglSwapBuffers         func(unsafe.Pointer, unsafe.Pointer) int32
	eglTerminate           func(unsafe.Pointer) int32
	eglQueryString         func(unsafe.Pointer, int32) uintptr
	eglGetError            func() int32
	eglQuerySurface        func(unsafe.Pointer, unsafe.Pointer, int32, unsafe.Pointer) int32
	eglGetProcAddress      func(uintptr) uintptr
	eglBindAPI             func(uint32) int32

	// Optional EGL functions (may not be available on all systems)
	eglGetPlatformDisplay          func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) unsafe.Pointer
	eglCreatePlatformWindowSurface func(unsafe.Pointer, unsafe.Pointer, unsafe.Pointer, unsafe.Pointer) unsafe.Pointer

	// GLES2 functions (only essential ones declared)
	glGetError    func() uint32
	glGetString   func(uint32) uintptr
	glGetIntegerv func(uint32, uintptr)

	// Desktop OpenGL functions - will be loaded via GetProcAddress
	glActiveTexture            func(uint32)
	glAttachShader             func(uint32, uint32)
	glBindBuffer               func(uint32, uint32)
	glBindFramebuffer          func(uint32, uint32)
	glBindTexture              func(uint32, uint32)
	glBlendFunc                func(uint32, uint32)
	glBlendFuncSeparate        func(uint32, uint32, uint32, uint32)
	glClear                    func(uint32)
	glClearColor               func(float32, float32, float32, float32)
	glCompileShader            func(uint32)
	glCreateProgram            func() uint32
	glCreateShader             func(uint32) uint32
	glDrawArrays               func(uint32, int32, int32)
	glDrawElements             func(uint32, uint32, uint32, unsafe.Pointer)
	glDepthFunc                func(uint32)
	glDepthMask                func(uint8)
	glDisable                  func(uint32)
	glEnable                   func(uint32)
	glEnableVertexAttribArray  func(uint32)
	glDisableVertexAttribArray func(uint32)
	glGenBuffers               func(int32, unsafe.Pointer)
	glGenFramebuffers          func(int32, unsafe.Pointer)
	glGenTextures              func(int32, unsafe.Pointer)
	glScissor                  func(int32, int32, int32, int32)
	glBufferData               func(uint32, int32, unsafe.Pointer, uint32)
	glBufferSubData            func(uint32, int32, int32, unsafe.Pointer)
	glGetShaderiv              func(uint32, uint32, uintptr)
	glGetShaderInfoLog         func(uint32, int32, uintptr, uintptr)
	glGetProgramiv             func(uint32, uint32, uintptr)
	glGetUniformLocation       func(uint32, uintptr) int32
	glGetAttribLocation        func(uint32, uintptr) int32
	glLinkProgram              func(uint32)
	glShaderSource             func(uint32, int32, uintptr, unsafe.Pointer)
	glTexImage2D               func(uint32, int32, int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	glTexSubImage2D            func(uint32, int32, int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	glTexParameteri            func(uint32, uint32, int32)
	glFramebufferTexture2D     func(uint32, uint32, uint32, uint32, int32)
	glCheckFramebufferStatus   func(uint32) uint32
	glGenQueries               func(int32, *uint32)
	glBeginQuery               func(uint32, uint32)
	glEndQuery                 func(uint32)
	glGetQueryObjectiv         func(uint32, uint32, *int32)
	glUniform1i                func(int32, int32)
	glUniform1f                func(int32, float32)
	glUniform2f                func(int32, float32, float32)
	glUniform3f                func(int32, float32, float32, float32)
	glUniform4f                func(int32, float32, float32, float32, float32)
	glUniformMatrix4fv         func(int32, int32, uint8, *float32)
	glUseProgram               func(uint32)
	glViewport                 func(int32, int32, int32, int32)
	// glVertexAttribPointer signature: GLuint index, GLint size, GLenum type, GLboolean normalized, GLsizei stride, const void *pointer
	glVertexAttribPointer func(uint32, int32, uint32, uint8, int32, unsafe.Pointer)

	glDeleteBuffers      func(int32, unsafe.Pointer)
	glDeleteFramebuffers func(int32, unsafe.Pointer)
	glDeleteProgram      func(uint32)
	glGetProgramInfoLog  func(uint32, int32, uintptr, uintptr)
	glDeleteShader       func(uint32)
	glDeleteTextures     func(int32, unsafe.Pointer)

	// Stencil functions
	glBindAttribLocation func(uint32, uint32, uintptr)
	glStencilFunc        func(uint32, int32, uint32)
	glStencilOp          func(uint32, uint32, uint32)
	glStencilMask        func(uint32)
	glColorMask          func(uint8, uint8, uint8, uint8)

	// Read pixels from framebuffer
	glReadPixels func(int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)

	// Block until all GL commands complete (multi-window shared-VBO sync)
	glFinish func()

	initOnce sync.Once
	initErr  error
)

const (
	EGL_DEFAULT_DISPLAY         = 0
	EGL_PLATFORM_X11_KHR        = 0x31D6
	EGL_PLATFORM_X11_SCREEN_EXT = 0x31D6
	EGL_OPENGL_ES_API           = 0x30A0
	EGL_OPENGL_ES2_BIT          = 4
	EGL_WINDOW_BIT              = 4
	EGL_RENDERABLE_TYPE         = 0x3040
	EGL_SURFACE_TYPE            = 0x3033
	EGL_RED_SIZE                = 0x3021
	EGL_GREEN_SIZE              = 0x3022
	EGL_BLUE_SIZE               = 0x3023
	EGL_ALPHA_SIZE              = 0x3024
	EGL_DEPTH_SIZE              = 0x3025
	EGL_NONE                    = 0x3038
	EGL_CONTEXT_CLIENT_VERSION  = 0x3098
	EGL_SUCCESS                 = 0x3000
)

// loadGLFunctions loads all OpenGL function pointers using purego.RegisterFunc
func getGLError() uint32 {
	if glGetError != nil {
		return glGetError()
	}
	return 0 // No error
}

func loadGLFunctions() error {
	procGetError := GetGL3ProcAddress("glGetError")
	if procGetError == 0 {
		return nil // No context yet, will be called later
	}

	purego.RegisterFunc(&glGetError, procGetError)
	purego.RegisterFunc(&glGetString, GetGL3ProcAddress("glGetString"))
	purego.RegisterFunc(&glGetIntegerv, GetGL3ProcAddress("glGetIntegerv"))
	purego.RegisterFunc(&glActiveTexture, GetGL3ProcAddress("glActiveTexture"))
	purego.RegisterFunc(&glAttachShader, GetGL3ProcAddress("glAttachShader"))
	purego.RegisterFunc(&glBindBuffer, GetGL3ProcAddress("glBindBuffer"))
	purego.RegisterFunc(&glBindFramebuffer, GetGL3ProcAddress("glBindFramebuffer"))
	purego.RegisterFunc(&glBindTexture, GetGL3ProcAddress("glBindTexture"))
	purego.RegisterFunc(&glBlendFunc, GetGL3ProcAddress("glBlendFunc"))
	purego.RegisterFunc(&glBlendFuncSeparate, GetGL3ProcAddress("glBlendFuncSeparate"))
	purego.RegisterFunc(&glClear, GetGL3ProcAddress("glClear"))
	purego.RegisterFunc(&glClearColor, GetGL3ProcAddress("glClearColor"))
	purego.RegisterFunc(&glCompileShader, GetGL3ProcAddress("glCompileShader"))
	purego.RegisterFunc(&glCreateProgram, GetGL3ProcAddress("glCreateProgram"))
	purego.RegisterFunc(&glCreateShader, GetGL3ProcAddress("glCreateShader"))
	purego.RegisterFunc(&glDrawArrays, GetGL3ProcAddress("glDrawArrays"))
	purego.RegisterFunc(&glDrawElements, GetGL3ProcAddress("glDrawElements"))
	purego.RegisterFunc(&glDepthFunc, GetGL3ProcAddress("glDepthFunc"))
	purego.RegisterFunc(&glDepthMask, GetGL3ProcAddress("glDepthMask"))
	purego.RegisterFunc(&glEnable, GetGL3ProcAddress("glEnable"))
	purego.RegisterFunc(&glDisable, GetGL3ProcAddress("glDisable"))
	purego.RegisterFunc(&glEnableVertexAttribArray, GetGL3ProcAddress("glEnableVertexAttribArray"))
	purego.RegisterFunc(&glDisableVertexAttribArray, GetGL3ProcAddress("glDisableVertexAttribArray"))
	purego.RegisterFunc(&glGenBuffers, GetGL3ProcAddress("glGenBuffers"))
	purego.RegisterFunc(&glGenFramebuffers, GetGL3ProcAddress("glGenFramebuffers"))
	purego.RegisterFunc(&glGenTextures, GetGL3ProcAddress("glGenTextures"))
	purego.RegisterFunc(&glBufferData, GetGL3ProcAddress("glBufferData"))
	purego.RegisterFunc(&glBufferSubData, GetGL3ProcAddress("glBufferSubData"))
	purego.RegisterFunc(&glGetShaderiv, GetGL3ProcAddress("glGetShaderiv"))
	purego.RegisterFunc(&glGetShaderInfoLog, GetGL3ProcAddress("glGetShaderInfoLog"))
	purego.RegisterFunc(&glGetProgramiv, GetGL3ProcAddress("glGetProgramiv"))
	purego.RegisterFunc(&glGetUniformLocation, GetGL3ProcAddress("glGetUniformLocation"))
	purego.RegisterFunc(&glGetAttribLocation, GetGL3ProcAddress("glGetAttribLocation"))
	purego.RegisterFunc(&glBindAttribLocation, GetGL3ProcAddress("glBindAttribLocation"))
	purego.RegisterFunc(&glLinkProgram, GetGL3ProcAddress("glLinkProgram"))
	purego.RegisterFunc(&glShaderSource, GetGL3ProcAddress("glShaderSource"))
	purego.RegisterFunc(&glTexImage2D, GetGL3ProcAddress("glTexImage2D"))
	purego.RegisterFunc(&glTexSubImage2D, GetGL3ProcAddress("glTexSubImage2D"))
	purego.RegisterFunc(&glTexParameteri, GetGL3ProcAddress("glTexParameteri"))
	purego.RegisterFunc(&glFramebufferTexture2D, GetGL3ProcAddress("glFramebufferTexture2D"))
	purego.RegisterFunc(&glCheckFramebufferStatus, GetGL3ProcAddress("glCheckFramebufferStatus"))
	purego.RegisterFunc(&glGenQueries, GetGL3ProcAddress("glGenQueries"))
	purego.RegisterFunc(&glBeginQuery, GetGL3ProcAddress("glBeginQuery"))
	purego.RegisterFunc(&glEndQuery, GetGL3ProcAddress("glEndQuery"))
	purego.RegisterFunc(&glGetQueryObjectiv, GetGL3ProcAddress("glGetQueryObjectiv"))
	purego.RegisterFunc(&glUniform1i, GetGL3ProcAddress("glUniform1i"))
	purego.RegisterFunc(&glUniform1f, GetGL3ProcAddress("glUniform1f"))
	purego.RegisterFunc(&glUniform2f, GetGL3ProcAddress("glUniform2f"))
	purego.RegisterFunc(&glUniform3f, GetGL3ProcAddress("glUniform3f"))
	purego.RegisterFunc(&glUniform4f, GetGL3ProcAddress("glUniform4f"))
	purego.RegisterFunc(&glUniformMatrix4fv, GetGL3ProcAddress("glUniformMatrix4fv"))
	purego.RegisterFunc(&glUseProgram, GetGL3ProcAddress("glUseProgram"))
	purego.RegisterFunc(&glViewport, GetGL3ProcAddress("glViewport"))
	purego.RegisterFunc(&glVertexAttribPointer, GetGL3ProcAddress("glVertexAttribPointer"))
	purego.RegisterFunc(&glDeleteBuffers, GetGL3ProcAddress("glDeleteBuffers"))
	purego.RegisterFunc(&glDeleteFramebuffers, GetGL3ProcAddress("glDeleteFramebuffers"))
	purego.RegisterFunc(&glDeleteProgram, GetGL3ProcAddress("glDeleteProgram"))
	purego.RegisterFunc(&glGetProgramInfoLog, GetGL3ProcAddress("glGetProgramInfoLog"))
	purego.RegisterFunc(&glDeleteShader, GetGL3ProcAddress("glDeleteShader"))
	purego.RegisterFunc(&glDeleteTextures, GetGL3ProcAddress("glDeleteTextures"))
	purego.RegisterFunc(&glReadPixels, GetGL3ProcAddress("glReadPixels"))
	purego.RegisterFunc(&glFinish, GetGL3ProcAddress("glFinish"))

	return nil
}

// Init loads OpenGL ES symbols from the system libraries using purego.
// This must be called before any GLES functions are used.
func Init() error {
	initOnce.Do(func() {
		initErr = doInit()
	})
	return initErr
}

func doInit() error {
	var err error

	// Try GLES first (for Android/mobile)
	libEGL, err = purego.Dlopen("libEGL.so", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		libEGL, err = purego.Dlopen("libEGL.so.1", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	}
	if err != nil {
		libEGL, err = purego.Dlopen("libEGL.so.1.0.0", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	}

	libGLES, err = purego.Dlopen("libGLESv2.so", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		libGLES, err = purego.Dlopen("libGLESv2.so.2.0.0", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	}
	if err != nil {
		libGLES, err = purego.Dlopen("libGLESv3.so", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	}

	// On desktop Linux, SDL_GL_GetProcAddress will be used for GL functions
	// libGL is loaded separately for GLES support on desktop
	libGL, err = purego.Dlopen("libGL.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		libGL = 0
	}

	// Load EGL functions only if available
	if libEGL != 0 {
		purego.RegisterLibFunc(&eglGetDisplay, libEGL, "eglGetDisplay")
		purego.RegisterLibFunc(&eglInitialize, libEGL, "eglInitialize")
		purego.RegisterLibFunc(&eglQueryString, libEGL, "eglQueryString")
		purego.RegisterLibFunc(&eglChooseConfig, libEGL, "eglChooseConfig")
		purego.RegisterLibFunc(&eglGetConfigAttrib, libEGL, "eglGetConfigAttrib")
		purego.RegisterLibFunc(&eglCreateWindowSurface, libEGL, "eglCreateWindowSurface")
		purego.RegisterLibFunc(&eglDestroySurface, libEGL, "eglDestroySurface")
		purego.RegisterLibFunc(&eglCreateContext, libEGL, "eglCreateContext")
		purego.RegisterLibFunc(&eglDestroyContext, libEGL, "eglDestroyContext")
		purego.RegisterLibFunc(&eglMakeCurrent, libEGL, "eglMakeCurrent")
		purego.RegisterLibFunc(&eglSwapBuffers, libEGL, "eglSwapBuffers")
		purego.RegisterLibFunc(&eglTerminate, libEGL, "eglTerminate")
		purego.RegisterLibFunc(&eglGetError, libEGL, "eglGetError")
		purego.RegisterLibFunc(&eglQuerySurface, libEGL, "eglQuerySurface")
		purego.RegisterLibFunc(&eglGetProcAddress, libEGL, "eglGetProcAddress")
		purego.RegisterLibFunc(&eglBindAPI, libEGL, "eglBindAPI")
	}

	// Load GLES functions only if available (for Android/mobile)
	if libGLES != 0 {
		purego.RegisterLibFunc(&glActiveTexture, libGLES, "glActiveTexture")
		purego.RegisterLibFunc(&glAttachShader, libGLES, "glAttachShader")
		purego.RegisterLibFunc(&glBindBuffer, libGLES, "glBindBuffer")
		purego.RegisterLibFunc(&glBindFramebuffer, libGLES, "glBindFramebuffer")
		purego.RegisterLibFunc(&glBindTexture, libGLES, "glBindTexture")
		purego.RegisterLibFunc(&glBlendFunc, libGLES, "glBlendFunc")
		purego.RegisterLibFunc(&glBlendFuncSeparate, libGLES, "glBlendFuncSeparate")
		purego.RegisterLibFunc(&glClear, libGLES, "glClear")
		purego.RegisterLibFunc(&glClearColor, libGLES, "glClearColor")
		purego.RegisterLibFunc(&glCreateProgram, libGLES, "glCreateProgram")
		purego.RegisterLibFunc(&glCreateShader, libGLES, "glCreateShader")
		purego.RegisterLibFunc(&glDrawArrays, libGLES, "glDrawArrays")
		purego.RegisterLibFunc(&glDrawElements, libGLES, "glDrawElements")
		purego.RegisterLibFunc(&glDepthFunc, libGLES, "glDepthFunc")
		purego.RegisterLibFunc(&glDepthMask, libGLES, "glDepthMask")
		purego.RegisterLibFunc(&glEnable, libGLES, "glEnable")
		purego.RegisterLibFunc(&glDisable, libGLES, "glDisable")
		purego.RegisterLibFunc(&glEnableVertexAttribArray, libGLES, "glEnableVertexAttribArray")
		purego.RegisterLibFunc(&glDisableVertexAttribArray, libGLES, "glDisableVertexAttribArray")
		purego.RegisterLibFunc(&glGenBuffers, libGLES, "glGenBuffers")
		purego.RegisterLibFunc(&glGenFramebuffers, libGLES, "glGenFramebuffers")
		purego.RegisterLibFunc(&glGenTextures, libGLES, "glGenTextures")
		purego.RegisterLibFunc(&glGetError, libGLES, "glGetError")
		purego.RegisterLibFunc(&glGetString, libGLES, "glGetString")
		purego.RegisterLibFunc(&glLinkProgram, libGLES, "glLinkProgram")
		purego.RegisterLibFunc(&glShaderSource, libGLES, "glShaderSource")
		purego.RegisterLibFunc(&glTexImage2D, libGLES, "glTexImage2D")
		purego.RegisterLibFunc(&glTexParameteri, libGLES, "glTexParameteri")
		purego.RegisterLibFunc(&glFramebufferTexture2D, libGLES, "glFramebufferTexture2D")
		purego.RegisterLibFunc(&glCheckFramebufferStatus, libGLES, "glCheckFramebufferStatus")
		// GPU timer queries are desktop-only (not in GLES 2.0 core)
		purego.RegisterLibFunc(&glUniform1i, libGLES, "glUniform1i")
		purego.RegisterLibFunc(&glUniform2f, libGLES, "glUniform2f")
		purego.RegisterLibFunc(&glUniform3f, libGLES, "glUniform3f")
		purego.RegisterLibFunc(&glUniform4f, libGLES, "glUniform4f")
		purego.RegisterLibFunc(&glUseProgram, libGLES, "glUseProgram")
		purego.RegisterLibFunc(&glViewport, libGLES, "glViewport")
		purego.RegisterLibFunc(&glVertexAttribPointer, libGLES, "glVertexAttribPointer")
		purego.RegisterLibFunc(&glDeleteBuffers, libGLES, "glDeleteBuffers")
		purego.RegisterLibFunc(&glDeleteFramebuffers, libGLES, "glDeleteFramebuffers")
		purego.RegisterLibFunc(&glDeleteProgram, libGLES, "glDeleteProgram")
		purego.RegisterLibFunc(&glGetProgramInfoLog, libGLES, "glGetProgramInfoLog")
		purego.RegisterLibFunc(&glDeleteShader, libGLES, "glDeleteShader")
		purego.RegisterLibFunc(&glDeleteTextures, libGLES, "glDeleteTextures")
		purego.RegisterLibFunc(&glCompileShader, libGLES, "glCompileShader")
		purego.RegisterLibFunc(&glGetUniformLocation, libGLES, "glGetUniformLocation")
		purego.RegisterLibFunc(&glGetAttribLocation, libGLES, "glGetAttribLocation")
		purego.RegisterLibFunc(&glGetShaderiv, libGLES, "glGetShaderiv")
		purego.RegisterLibFunc(&glGetProgramiv, libGLES, "glGetProgramiv")
		purego.RegisterLibFunc(&glScissor, libGLES, "glScissor")
		purego.RegisterLibFunc(&glReadPixels, libGLES, "glReadPixels")
	}

	return nil
}

// GetError returns the current OpenGL error (0 means no error).
func GetError() uint32 {
	if glGetError != nil {
		return glGetError()
	}
	return 0
}

// HasEGLSupport returns true if EGL/GLES libraries were loaded successfully
func HasEGLSupport() bool {
	return libEGL != 0 && libGLES != 0
}

// HasGLSupport returns true if desktop OpenGL is available
func HasGLSupport() bool {
	return libGL != 0 || libGLES != 0
}

// loadGL loads OpenGL functions from libGL (desktop OpenGL)
func loadGL() {
	// libGL is already loaded by SDL via SDL_GL_CreateContext
	// We just need to get function pointers via dlsym on libGL.so
	if libGL == 0 {
		return
	}
	// Functions already registered from libGLES if available
}

// registerGL3 registers a GL entry point only when the loader returned one.
//
// SDL_GL_GetProcAddress returns NULL for a symbol the CURRENT context does not
// provide — on Android's GLES 2.0 context that is glGenQueries and friends —
// and purego.RegisterFunc panics on a nil pointer ("purego: cfn is nil"), which
// is exactly how the first on-device run died. The functions the renderer
// cannot do without are checked by InitRenderer, so an absent optional entry
// point must be skipped, not fatal.
func registerGL3(fn any, name string) {
	if p := GetGL3ProcAddress(name); p != 0 {
		purego.RegisterFunc(fn, p)
	}
}

// loadGL3Functions loads GL functions using SDL3 GL proc address
func loadGL3Functions() {
	procGetError := GetGL3ProcAddress("glGetError")
	if procGetError == 0 {
		return // No context yet, will be called later
	}

	purego.RegisterFunc(&glGetError, procGetError)
	registerGL3(&glGetString, "glGetString")
	registerGL3(&glGetIntegerv, "glGetIntegerv")
	registerGL3(&glActiveTexture, "glActiveTexture")
	registerGL3(&glAttachShader, "glAttachShader")
	registerGL3(&glBindBuffer, "glBindBuffer")
	registerGL3(&glBindFramebuffer, "glBindFramebuffer")
	registerGL3(&glBindTexture, "glBindTexture")
	registerGL3(&glBlendFunc, "glBlendFunc")
	registerGL3(&glClear, "glClear")
	registerGL3(&glClearColor, "glClearColor")
	registerGL3(&glCompileShader, "glCompileShader")
	registerGL3(&glCreateProgram, "glCreateProgram")
	registerGL3(&glCreateShader, "glCreateShader")
	registerGL3(&glDrawArrays, "glDrawArrays")
	registerGL3(&glDrawElements, "glDrawElements")
	registerGL3(&glDepthFunc, "glDepthFunc")
	registerGL3(&glDepthMask, "glDepthMask")
	registerGL3(&glEnable, "glEnable")
	registerGL3(&glDisable, "glDisable")
	registerGL3(&glEnableVertexAttribArray, "glEnableVertexAttribArray")
	registerGL3(&glGenBuffers, "glGenBuffers")
	registerGL3(&glGenFramebuffers, "glGenFramebuffers")
	registerGL3(&glGenTextures, "glGenTextures")
	registerGL3(&glBufferData, "glBufferData")
	registerGL3(&glBufferSubData, "glBufferSubData")
	registerGL3(&glGetShaderiv, "glGetShaderiv")
	registerGL3(&glGetShaderInfoLog, "glGetShaderInfoLog")
	registerGL3(&glGetProgramiv, "glGetProgramiv")
	registerGL3(&glGetUniformLocation, "glGetUniformLocation")
	registerGL3(&glGetAttribLocation, "glGetAttribLocation")
	registerGL3(&glBindAttribLocation, "glBindAttribLocation")
	registerGL3(&glLinkProgram, "glLinkProgram")
	registerGL3(&glShaderSource, "glShaderSource")
	registerGL3(&glTexImage2D, "glTexImage2D")
	registerGL3(&glTexSubImage2D, "glTexSubImage2D")
	registerGL3(&glTexParameteri, "glTexParameteri")
	registerGL3(&glFramebufferTexture2D, "glFramebufferTexture2D")
	registerGL3(&glCheckFramebufferStatus, "glCheckFramebufferStatus")
	registerGL3(&glGenQueries, "glGenQueries")
	registerGL3(&glBeginQuery, "glBeginQuery")
	registerGL3(&glEndQuery, "glEndQuery")
	registerGL3(&glGetQueryObjectiv, "glGetQueryObjectiv")
	registerGL3(&glUniform1i, "glUniform1i")
	registerGL3(&glUniform1f, "glUniform1f")
	registerGL3(&glUniform2f, "glUniform2f")
	registerGL3(&glUniform3f, "glUniform3f")
	registerGL3(&glUniform4f, "glUniform4f")
	registerGL3(&glUniformMatrix4fv, "glUniformMatrix4fv")
	registerGL3(&glUseProgram, "glUseProgram")
	registerGL3(&glViewport, "glViewport")
	registerGL3(&glVertexAttribPointer, "glVertexAttribPointer")
	registerGL3(&glDeleteBuffers, "glDeleteBuffers")
	registerGL3(&glDeleteFramebuffers, "glDeleteFramebuffers")
	registerGL3(&glDeleteProgram, "glDeleteProgram")
	registerGL3(&glGetProgramInfoLog, "glGetProgramInfoLog")
	registerGL3(&glDeleteShader, "glDeleteShader")
	registerGL3(&glDeleteTextures, "glDeleteTextures")
	registerGL3(&glScissor, "glScissor")
	registerGL3(&glStencilFunc, "glStencilFunc")
	registerGL3(&glStencilOp, "glStencilOp")
	registerGL3(&glStencilMask, "glStencilMask")
	registerGL3(&glColorMask, "glColorMask")
	registerGL3(&glReadPixels, "glReadPixels")
	registerGL3(&glFinish, "glFinish")
}

// EGLContext holds EGL display, context, and surface
type EGLContext struct {
	disp    unsafe.Pointer
	ctx     unsafe.Pointer
	surface unsafe.Pointer
	config  unsafe.Pointer
}

var eglCtx *EGLContext

// InitEGLForAndroid initializes EGL for an Android ANativeWindow.
// window is the ANativeWindow pointer passed from JNI.
// After success, GL function pointers are loaded via eglGetProcAddress.
func InitEGLForAndroid(window unsafe.Pointer, width, height int32) error {
	if eglCtx != nil {
		return nil
	}
	runtime.LockOSThread()

	eglCtx = &EGLContext{}

	if eglGetDisplay == nil {
		runtime.UnlockOSThread()
		return fmt.Errorf("EGL not loaded")
	}

	eglCtx.disp = eglGetDisplay(unsafe.Pointer(nil))
	if eglCtx.disp == nil {
		runtime.UnlockOSThread()
		eglCtx = nil
		return fmt.Errorf("eglGetDisplay failed (eglGetError=%d)", eglGetError())
	}

	var major, minor int32
	if eglInitialize(eglCtx.disp, unsafe.Pointer(&major), unsafe.Pointer(&minor)) == 0 {
		runtime.UnlockOSThread()
		eglCtx = nil
		return fmt.Errorf("eglInitialize failed (eglGetError=%d)", eglGetError())
	}

	configAttribs := []int32{
		EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
		EGL_SURFACE_TYPE, EGL_WINDOW_BIT,
		EGL_RED_SIZE, 8,
		EGL_GREEN_SIZE, 8,
		EGL_BLUE_SIZE, 8,
		EGL_ALPHA_SIZE, 8,
		EGL_DEPTH_SIZE, 16,
		EGL_NONE,
	}
	var numConfigs int32
	if eglChooseConfig(eglCtx.disp, unsafe.Pointer(&configAttribs[0]),
		unsafe.Pointer(&eglCtx.config), 1, unsafe.Pointer(&numConfigs)) == 0 || numConfigs != 1 {
		runtime.UnlockOSThread()
		eglCtx = nil
		return fmt.Errorf("eglChooseConfig failed (eglGetError=%d, numConfigs=%d)", eglGetError(), numConfigs)
	}

	if eglCreateWindowSurface == nil {
		runtime.UnlockOSThread()
		eglCtx = nil
		return fmt.Errorf("eglCreateWindowSurface not available")
	}
	eglCtx.surface = eglCreateWindowSurface(eglCtx.disp, eglCtx.config, window, nil)
	if eglCtx.surface == nil {
		err := fmt.Errorf("eglCreateWindowSurface failed (eglGetError=%d)", eglGetError())
		runtime.UnlockOSThread()
		eglCtx = nil
		return err
	}

	if eglBindAPI(EGL_OPENGL_ES_API) == 0 {
		eglDestroySurface(eglCtx.disp, eglCtx.surface)
		runtime.UnlockOSThread()
		eglCtx = nil
		return fmt.Errorf("eglBindAPI failed (eglGetError=%d)", eglGetError())
	}

	ctxAttribs := []int32{EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE}
	eglCtx.ctx = eglCreateContext(eglCtx.disp, eglCtx.config, nil, unsafe.Pointer(&ctxAttribs[0]))
	if eglCtx.ctx == nil {
		err := fmt.Errorf("eglCreateContext failed (eglGetError=%d)", eglGetError())
		eglDestroySurface(eglCtx.disp, eglCtx.surface)
		runtime.UnlockOSThread()
		eglCtx = nil
		return err
	}

	if eglMakeCurrent(eglCtx.disp, eglCtx.surface, eglCtx.surface, eglCtx.ctx) == 0 {
		err := fmt.Errorf("eglMakeCurrent failed (eglGetError=%d)", eglGetError())
		eglDestroySurface(eglCtx.disp, eglCtx.surface)
		eglDestroyContext(eglCtx.disp, eglCtx.ctx)
		runtime.UnlockOSThread()
		eglCtx = nil
		return err
	}

	eglGetError()

	setViewport(0, 0, width, height)
	SetScreenSize(float32(width), float32(height))
	runtime.UnlockOSThread()
	return nil
}

// loadGLESFromEGL loads GL function pointers via eglGetProcAddress.
// Must be called after InitEGLForAndroid succeeds (context must be current).
func loadGLESFromEGL() {
	if eglGetProcAddress == nil {
		return
	}

	load := func(name string) uintptr {
		return eglGetProcAddress(uintptr(unsafe.Pointer(&[]byte(name + "\x00")[0])))
	}

	if proc := load("glGetError"); proc != 0 {
		purego.RegisterFunc(&glGetError, proc)
	}
	if proc := load("glGetString"); proc != 0 {
		purego.RegisterFunc(&glGetString, proc)
	}
	if proc := load("glGetIntegerv"); proc != 0 {
		purego.RegisterFunc(&glGetIntegerv, proc)
	}
	if proc := load("glActiveTexture"); proc != 0 {
		purego.RegisterFunc(&glActiveTexture, proc)
	}
	if proc := load("glAttachShader"); proc != 0 {
		purego.RegisterFunc(&glAttachShader, proc)
	}
	if proc := load("glBindBuffer"); proc != 0 {
		purego.RegisterFunc(&glBindBuffer, proc)
	}
	if proc := load("glBindFramebuffer"); proc != 0 {
		purego.RegisterFunc(&glBindFramebuffer, proc)
	}
	if proc := load("glBindTexture"); proc != 0 {
		purego.RegisterFunc(&glBindTexture, proc)
	}
	if proc := load("glBlendFunc"); proc != 0 {
		purego.RegisterFunc(&glBlendFunc, proc)
	}
	if proc := load("glClear"); proc != 0 {
		purego.RegisterFunc(&glClear, proc)
	}
	if proc := load("glClearColor"); proc != 0 {
		purego.RegisterFunc(&glClearColor, proc)
	}
	if proc := load("glCompileShader"); proc != 0 {
		purego.RegisterFunc(&glCompileShader, proc)
	}
	if proc := load("glCreateProgram"); proc != 0 {
		purego.RegisterFunc(&glCreateProgram, proc)
	}
	if proc := load("glCreateShader"); proc != 0 {
		purego.RegisterFunc(&glCreateShader, proc)
	}
	if proc := load("glDrawArrays"); proc != 0 {
		purego.RegisterFunc(&glDrawArrays, proc)
	}
	if proc := load("glDrawElements"); proc != 0 {
		purego.RegisterFunc(&glDrawElements, proc)
	}
	if proc := load("glDepthFunc"); proc != 0 {
		purego.RegisterFunc(&glDepthFunc, proc)
	}
	if proc := load("glDepthMask"); proc != 0 {
		purego.RegisterFunc(&glDepthMask, proc)
	}
	if proc := load("glEnable"); proc != 0 {
		purego.RegisterFunc(&glEnable, proc)
	}
	if proc := load("glDisable"); proc != 0 {
		purego.RegisterFunc(&glDisable, proc)
	}
	if proc := load("glDisableVertexAttribArray"); proc != 0 {
		purego.RegisterFunc(&glDisableVertexAttribArray, proc)
	}
	if proc := load("glEnableVertexAttribArray"); proc != 0 {
		purego.RegisterFunc(&glEnableVertexAttribArray, proc)
	}
	if proc := load("glGenBuffers"); proc != 0 {
		purego.RegisterFunc(&glGenBuffers, proc)
	}
	if proc := load("glGenFramebuffers"); proc != 0 {
		purego.RegisterFunc(&glGenFramebuffers, proc)
	}
	if proc := load("glGenTextures"); proc != 0 {
		purego.RegisterFunc(&glGenTextures, proc)
	}
	if proc := load("glBufferData"); proc != 0 {
		purego.RegisterFunc(&glBufferData, proc)
	}
	if proc := load("glBufferSubData"); proc != 0 {
		purego.RegisterFunc(&glBufferSubData, proc)
	}
	if proc := load("glGetShaderiv"); proc != 0 {
		purego.RegisterFunc(&glGetShaderiv, proc)
	}
	if proc := load("glGetShaderInfoLog"); proc != 0 {
		purego.RegisterFunc(&glGetShaderInfoLog, proc)
	}
	if proc := load("glGetProgramiv"); proc != 0 {
		purego.RegisterFunc(&glGetProgramiv, proc)
	}
	if proc := load("glGetUniformLocation"); proc != 0 {
		purego.RegisterFunc(&glGetUniformLocation, proc)
	}
	if proc := load("glGetAttribLocation"); proc != 0 {
		purego.RegisterFunc(&glGetAttribLocation, proc)
	}
	if proc := load("glLinkProgram"); proc != 0 {
		purego.RegisterFunc(&glLinkProgram, proc)
	}
	if proc := load("glShaderSource"); proc != 0 {
		purego.RegisterFunc(&glShaderSource, proc)
	}
	if proc := load("glTexImage2D"); proc != 0 {
		purego.RegisterFunc(&glTexImage2D, proc)
	}
	if proc := load("glTexParameteri"); proc != 0 {
		purego.RegisterFunc(&glTexParameteri, proc)
	}
	if proc := load("glFramebufferTexture2D"); proc != 0 {
		purego.RegisterFunc(&glFramebufferTexture2D, proc)
	}
	if proc := load("glCheckFramebufferStatus"); proc != 0 {
		purego.RegisterFunc(&glCheckFramebufferStatus, proc)
	}
	if proc := load("glGenQueries"); proc != 0 {
		purego.RegisterFunc(&glGenQueries, proc)
	}
	if proc := load("glBeginQuery"); proc != 0 {
		purego.RegisterFunc(&glBeginQuery, proc)
	}
	if proc := load("glEndQuery"); proc != 0 {
		purego.RegisterFunc(&glEndQuery, proc)
	}
	if proc := load("glGetQueryObjectiv"); proc != 0 {
		purego.RegisterFunc(&glGetQueryObjectiv, proc)
	}
	if proc := load("glUniform1i"); proc != 0 {
		purego.RegisterFunc(&glUniform1i, proc)
	}
	if proc := load("glUniform1f"); proc != 0 {
		purego.RegisterFunc(&glUniform1f, proc)
	}
	if proc := load("glUniform2f"); proc != 0 {
		purego.RegisterFunc(&glUniform2f, proc)
	}
	if proc := load("glUniform3f"); proc != 0 {
		purego.RegisterFunc(&glUniform3f, proc)
	}
	if proc := load("glUniform4f"); proc != 0 {
		purego.RegisterFunc(&glUniform4f, proc)
	}
	if proc := load("glUniformMatrix4fv"); proc != 0 {
		purego.RegisterFunc(&glUniformMatrix4fv, proc)
	}
	if proc := load("glUseProgram"); proc != 0 {
		purego.RegisterFunc(&glUseProgram, proc)
	}
	if proc := load("glViewport"); proc != 0 {
		purego.RegisterFunc(&glViewport, proc)
	}
	if proc := load("glVertexAttribPointer"); proc != 0 {
		purego.RegisterFunc(&glVertexAttribPointer, proc)
	}
	if proc := load("glDeleteBuffers"); proc != 0 {
		purego.RegisterFunc(&glDeleteBuffers, proc)
	}
	if proc := load("glDeleteFramebuffers"); proc != 0 {
		purego.RegisterFunc(&glDeleteFramebuffers, proc)
	}
	if proc := load("glDeleteProgram"); proc != 0 {
		purego.RegisterFunc(&glDeleteProgram, proc)
	}
	if proc := load("glGetProgramInfoLog"); proc != 0 {
		purego.RegisterFunc(&glGetProgramInfoLog, proc)
	}
	if proc := load("glDeleteShader"); proc != 0 {
		purego.RegisterFunc(&glDeleteShader, proc)
	}
	if proc := load("glDeleteTextures"); proc != 0 {
		purego.RegisterFunc(&glDeleteTextures, proc)
	}
	if proc := load("glScissor"); proc != 0 {
		purego.RegisterFunc(&glScissor, proc)
	}
	if proc := load("glReadPixels"); proc != 0 {
		purego.RegisterFunc(&glReadPixels, proc)
	}
	if proc := load("glStencilFunc"); proc != 0 {
		purego.RegisterFunc(&glStencilFunc, proc)
	}
	if proc := load("glStencilOp"); proc != 0 {
		purego.RegisterFunc(&glStencilOp, proc)
	}
	if proc := load("glStencilMask"); proc != 0 {
		purego.RegisterFunc(&glStencilMask, proc)
	}
	if proc := load("glColorMask"); proc != 0 {
		purego.RegisterFunc(&glColorMask, proc)
	}
}

// SwapEGLBuffers swaps the EGL buffers (call after rendering)
func SwapEGLBuffers() {
	if eglCtx != nil && eglCtx.disp != nil && eglCtx.surface != nil {
		eglSwapBuffers(eglCtx.disp, eglCtx.surface)
	}
}

// TerminateEGL cleans up EGL resources
func TerminateEGL() {
	if eglCtx != nil {
		if eglCtx.disp != nil {
			if eglCtx.ctx != nil {
				eglDestroyContext(eglCtx.disp, eglCtx.ctx)
			}
			if eglCtx.surface != nil {
				eglDestroySurface(eglCtx.disp, eglCtx.surface)
			}
			eglTerminate(eglCtx.disp)
		}
		eglCtx = nil
	}
}
