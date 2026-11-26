package mobilegles

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	GL_VERTEX_SHADER          = 0x8B31
	GL_FRAGMENT_SHADER        = 0x8B30
	GL_TRIANGLES              = 0x0004
	GL_FLOAT                  = 0x1406
	GL_ARRAY_BUFFER           = 0x8892
	GL_ELEMENT_ARRAY_BUFFER   = 0x8893
	GL_UNSIGNED_INT           = 0x1405
	GL_STATIC_DRAW            = 0x88E4
	GL_STREAM_DRAW            = 0x88E0
	GL_COLOR_BUFFER_BIT       = 0x00004000
	GL_SCISSOR_TEST           = 0x0C11
	GL_TEXTURE_2D             = 0x0DE1
	GL_ALPHA                  = 0x1906
	GL_RGBA                   = 0x1908
	GL_UNSIGNED_BYTE          = 0x1401
	GL_LINEAR                 = 0x2601
	GL_LINEAR_MIPMAP_LINEAR   = 0x2703
	GL_CLAMP_TO_EDGE          = 0x812F
	GL_TEXTURE_MIN_FILTER     = 0x2801
	GL_TEXTURE_MAG_FILTER     = 0x2800
	GL_TEXTURE_WRAP_S         = 0x2802
	GL_TEXTURE_WRAP_T         = 0x2803
	GL_DEPTH_TEST             = 0x0B71
	GL_DEPTH_BUFFER_BIT       = 0x00000100
	GL_STENCIL_TEST           = 0x0B90
	GL_STENCIL_BUFFER_BIT     = 0x00000400
	GL_ALWAYS                 = 0x0207
	GL_NOTEQUAL               = 0x0205
	GL_KEEP                   = 0x1E00
	GL_INCR                   = 0x1E02
	GL_LEQUAL                 = 0x0203
	GL_BLEND                  = 0x0BE2
	GL_SRC_ALPHA              = 0x0302
	GL_ONE_MINUS_SRC_ALPHA    = 0x0303
	GL_ONE_MINUS_SRC_COLOR    = 0x0301
	GL_ZERO                   = 0x0000
	GL_SRC_COLOR              = 0x0300
	GL_DST_COLOR              = 0x0306
	GL_ONE                    = 0x0001
	GL_FRAMEBUFFER            = 0x8D40
	GL_COLOR_ATTACHMENT0      = 0x8CE0
	GL_FRAMEBUFFER_COMPLETE   = 0x8CD5
	GL_TIME_ELAPSED           = 0x88BF
	GL_QUERY_RESULT           = 0x8866
	GL_QUERY_RESULT_AVAILABLE = 0x8867
)

const (
	shapeTypeRect   = 0
	shapeTypeCircle = 1
)

type ShapeRenderer struct {
	program       uint32
	vbo           uint32
	vboCap        int32
	centerLoc     int32
	localLoc      int32
	halfLoc       int32
	angleLoc      int32
	colorLoc      int32
	shapeTypeLoc  int32
	clipMinLoc    int32
	clipMaxLoc    int32
	clipEnLoc     int32
	screenSizeLoc int32
	ready         bool
}

type TextRenderer struct {
	program       uint32
	vbo           uint32
	posLoc        int32
	uvLoc         int32
	colorAttrLoc  int32
	clipMinLoc    int32
	clipMaxLoc    int32
	clipEnLoc     int32
	colorLoc      int32
	screenSizeLoc int32
	textureLoc    int32
	isImageLoc    int32
	ready         bool
}

type RectRenderer struct {
	program         uint32
	vbo             uint32
	circleVBO       uint32
	colorLoc        int32
	color2Loc       int32
	gradientTypeLoc int32
	posLoc          int32
	sizeLoc         int32
	screenSizeLoc   int32
	angleLoc        int32
	blendModeLoc    int32
	shapeTypeLoc    int32
	ready           bool
	screenWidth     float32
	screenHeight    float32
	instanceVBO     uint32
	maxInstances    int
	instanceCount   int
	posAttrLoc      uint32

	triProgram       uint32
	triVBO           uint32
	triPosAttrLoc    int32
	triBaryAttrLoc   int32
	triColorAttrLoc  int32
	triClipMinLoc    int32
	triClipMaxLoc    int32
	triClipEnLoc     int32
	triScreenSizeLoc int32
}

type BlurRenderer struct {
	program      uint32
	vbo          uint32
	fbo          uint32
	texture      uint32
	fbo2         uint32
	texture2     uint32
	ready        bool
	screenWidth  float32
	screenHeight float32
	texLoc       int32
	pixelSizeLoc int32
	dirLoc       int32
}

var rectRenderer *RectRenderer
var shapeRenderer *ShapeRenderer
var textRenderer *TextRenderer
var blurRenderer *BlurRenderer
var fbo uint32
var blurTexture uint32

var quadVertices = [12]float32{
	-0.5, -0.5, 0.5, -0.5, 0.5, 0.5,
	-0.5, -0.5, 0.5, 0.5, -0.5, 0.5,
}

// initCircleVertices generates a circle mesh (120 triangles)
func initCircleVertices() []float32 {
	verts := make([]float32, 0, 720) // 120 triangles * 6 vertices
	for i := 0; i < 120; i++ {
		angle1 := float32(i) * 2 * 3.14159265 / 120
		angle2 := float32(i+1) * 2 * 3.14159265 / 120
		verts = append(verts, 0, 0)
		verts = append(verts, cosf(angle1), sinf(angle1))
		verts = append(verts, cosf(angle2), sinf(angle2))
	}
	return verts
}

// InstanceData holds per-instance attributes for batched rendering
type InstanceData struct {
	PosX, PosY     float32
	SizeX, SizeY   float32
	Angle          float32
	ColorR, ColorG float32
	ColorB, ColorA float32
	BlendMode      int32
	_padding       [3]float32
}

func SetScreenSize(w, h float32) {
	screenWidth = w
	screenHeight = h
	if rectRenderer != nil {
		rectRenderer.screenWidth = w
		rectRenderer.screenHeight = h
	}
}

var screenWidth, screenHeight float32

// setViewport is the ONE place glViewport is called. The vertical-resize fault
// was traced to the fragment coordinate space lagging a resize (gl_FragCoord.y
// measured 380 in a 620-tall window), i.e. a stale viewport IN FORCE at draw
// time even though glGetIntegerv at another moment reported the new size. A
// stale viewport can only come from a call site that recomputes it from one of
// the several screen-size copies (rectRenderer, blurRenderer, the offscreen
// save/restore), so every call goes through here and MEUI_VP_TRACE names the
// source of each change.
var vpPrev = [4]int32{-1, -1, -1, -1}

func setViewport(x, y, w, h int32) {
	if os.Getenv("MEUI_VP_TRACE") != "" {
		if vpPrev[0] != x || vpPrev[1] != y || vpPrev[2] != w || vpPrev[3] != h {
			vpPrev = [4]int32{x, y, w, h}
			where := "?"
			if _, file, line, ok := runtime.Caller(1); ok {
				where = fmt.Sprintf("%s:%d", filepath.Base(file), line)
			}
			fmt.Printf("[vp] -> %d,%d %dx%d via %s\n", x, y, w, h, where)
		}
	}
	glViewport(x, y, w, h)
}

// applyBaseContextState sets the per-context GL state the renderer
// relies on. GL enable bits and stencil state are PER-CONTEXT: a
// freshly created (or newly bound) context starts with BLEND OFF —
// translucent draws then write raw src (0,0,0,0) texels instead of
// blending, punching transparent holes in the window (observed: all
// of window 2's widgets/text invisible, opaque draws fine). InitRenderer
// runs on the primary context only, so every context rebind
// (MakeCurrentSDLWin) re-applies this. Scissor stays DISABLED: batch
// clipping is per-vertex and flushes leave scissor off.
func applyBaseContextState() {
	glEnable(GL_BLEND)
	glDisable(0x0B90) // GL_STENCIL_TEST
	glDisable(GL_SCISSOR_TEST)
}

// renderMu serializes WHOLE frames (batch accumulation through flush
// through swap) across windows. The batch arrays, screen-size uniform
// and VBOs are process-global and the VBOs are shared between
// contexts, so two windows drawing concurrently corrupt each other:
// wrong projections (one window's widgets scale by the other's aspect),
// garbage geometry, flashing. Each window's thread acquires the lock
// for its render section; contexts stay per-thread.
var renderMu sync.Mutex

// AcquireRenderer locks the global render pipeline. Call before any
// batch accumulation and hold through Swap/flush. See renderMu.
func AcquireRenderer() { renderMu.Lock() }

// ReleaseRenderer unlocks the global render pipeline.
func ReleaseRenderer() { renderMu.Unlock() }

// glesShaders reports whether the shader sources must be translated from
// desktop GLSL 1.20 to GLSL ES 1.00. It is a property of the CONTEXT, not of a
// platform name: the mobile targets have OpenGL ES only (the ES profile is
// requested in createSDL3Window), while desktop gets desktop GL. iOS needs the
// same translation as Android, which is why this is not `GOOS == "android"`.
func glesShaders() bool {
	return runtime.GOOS == "android" || runtime.GOOS == "ios"
}

func glesVertexShader(src string) string {
	if !glesShaders() {
		return src
	}
	src = strings.Replace(src, "#version 120",
		"#version 100\nprecision highp float;\n", 1)
	return src
}

func glesFragmentShader(src string) string {
	if !glesShaders() {
		return src
	}
	src = strings.Replace(src, "#version 120",
		"#version 100\n#extension GL_OES_standard_derivatives : enable\nprecision mediump float;\n", 1)
	return src
}

// buildRectProgram compiles the rect renderer's shaders and links its program once. Called up to
// three times by InitRenderer because a first-launch link failure is usually the driver's shader
// compiler being cold, not a rejection — and the difference is only visible in the program log,
// which the old code did not read at all.
func buildRectProgram() (uint32, error) {
	vs := glCreateShaderCtx(GL_VERTEX_SHADER)
	if vs == 0 {
		return 0, fmt.Errorf("glCreateShader returned 0")
	}
	fs := glCreateShaderCtx(GL_FRAGMENT_SHADER)
	if fs == 0 {
		return 0, fmt.Errorf("glCreateShader for fragment returned 0")
	}
	defer glDeleteShaderCtx(vs)
	defer glDeleteShaderCtx(fs)

	vertexSrc := glesVertexShader(rectSrcVS) + "\x00"
	fragmentSrc := glesFragmentShader(rectSrcFS) + "\x00"

	vsrc := []byte(vertexSrc)
	fsrc := []byte(fragmentSrc)

	vsrcPtr := uintptr(0)
	fsrcPtr := uintptr(0)
	if len(vsrc) > 0 {
		vsrcPtr = uintptr(unsafe.Pointer(&vsrc[0]))
	}
	if len(fsrc) > 0 {
		fsrcPtr = uintptr(unsafe.Pointer(&fsrc[0]))
	}

	vlen := int32(len(vertexSrc) - 1)
	flen := int32(len(fragmentSrc) - 1)

	glShaderSourceCtx(vs, 1, uintptr(unsafe.Pointer(&vsrcPtr)), unsafe.Pointer(&vlen))
	glCompileShaderCtx(vs)

	if !shaderCompiled(vs, "vertex") {
		return 0, fmt.Errorf("vertex shader failed: %s", shaderLog(vs))
	}

	glShaderSourceCtx(fs, 1, uintptr(unsafe.Pointer(&fsrcPtr)), unsafe.Pointer(&flen))
	glCompileShaderCtx(fs)
	if !shaderCompiled(fs, "fragment") {
		return 0, fmt.Errorf("fragment shader failed: %s", shaderLog(fs))
	}

	prog := glCreateProgramCtx()
	glAttachShaderCtx(prog, vs)
	glAttachShaderCtx(prog, fs)
	glLinkProgramCtx(prog)

	var linkStatus int32
	glGetProgramivCtx(prog, 0x8B82, uintptr(unsafe.Pointer(&linkStatus)))
	if linkStatus == 0 {
		glErr := glGetError()
		return 0, fmt.Errorf("program failed to link: log=%q glError=0x%x",
			programLog(prog), glErr)
	}
	return prog, nil
}

// shaderCompiled reports whether a shader compiled, and absorbs the read itself so a failed read
// (which leaves the caller's value untouched) cannot be mistaken for a status.
func shaderCompiled(sh uint32, what string) bool {
	var status int32
	glGetShaderivCtx(sh, 0x8B81, uintptr(unsafe.Pointer(&status)))
	if status == 0 {
		fmt.Printf("mobilegles: %s shader failed to compile: %s\n", what, shaderLog(sh))
	}
	return status != 0
}

// shaderLog reads a shader's info log. The old link-failure path never read the program's log, so a
// link failure carried no reason at all.
func shaderLog(sh uint32) string {
	if glGetProgramInfoLogCtx == nil {
		return "(glGetProgramInfoLog not loaded)"
	}
	var logLen int32
	glGetShaderInfoLogCtx(sh, 0, uintptr(unsafe.Pointer(&logLen)), 0)
	if logLen <= 0 {
		return "(empty)"
	}
	buf := make([]byte, logLen+1)
	glGetShaderInfoLogCtx(sh, logLen+1, 0, uintptr(unsafe.Pointer(&buf[0])))
	return string(buf)
}

// programLog reads a program's info log.
func programLog(prog uint32) string {
	if glGetProgramInfoLogCtx == nil {
		return "(glGetProgramInfoLog not loaded)"
	}
	var logLen int32
	glGetProgramInfoLogCtx(prog, 0, uintptr(unsafe.Pointer(&logLen)), 0)
	if logLen <= 0 {
		return "(empty)"
	}
	buf := make([]byte, logLen+1)
	glGetProgramInfoLogCtx(prog, logLen+1, 0, uintptr(unsafe.Pointer(&buf[0])))
	return string(buf)
}

func InitRenderer() error {
	runtime.LockOSThread()

	if rectRenderer != nil && rectRenderer.ready {
		runtime.UnlockOSThread()
		return nil
	}

	rectRenderer = &RectRenderer{}

	// Ensure GL functions are loaded. SDL_GL_GetProcAddress routes
	// through whichever API the window created (GLX or EGL) — always
	// prefer it; the GLES/EGL-direct path is Android-only.
	if glUseProgram == nil {
		if SDL3_GL_GetProcAddress != nil && sdl3Window != nil {
			loadGL3Functions()
		} else if eglContextReady() {
			loadGLESFromEGL()
		} else if SDL_GL_GetProcAddress != nil && sdlWindow != nil {
			loadGLFunctions()
		}
	}

	if glUseProgram == nil {
		runtime.UnlockOSThread()
		// Name the layer that actually failed. "GL functions not loaded" alone
		// sent the reader looking at shaders when the usual cause is upstream:
		// SDL could not load its library, could not open a window, or could not
		// create the context, so there is no current context to resolve
		// glUseProgram against. SDL's own message is the one that says which.
		if msg := sdlError(); msg != "" {
			return fmt.Errorf("GL functions not loaded (SDL: %s)", msg)
		}
		return fmt.Errorf("GL functions not loaded: no current GL context")
	}

	// Initialize Ctx wrappers now that module-level GL pointers are populated
	initCtxVars()

	if !tryRegister("glCreateShader", &glCreateShaderCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glCreateShader")
	}
	if !tryRegister("glCompileShader", &glCompileShaderCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glCompileShader")
	}
	if !tryRegister("glShaderSource", &glShaderSourceCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glShaderSource")
	}
	if !tryRegister("glCreateProgram", &glCreateProgramCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glCreateProgram")
	}
	if !tryRegister("glAttachShader", &glAttachShaderCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glAttachShader")
	}
	if !tryRegister("glLinkProgram", &glLinkProgramCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glLinkProgram")
	}
	if !tryRegister("glGetShaderiv", &glGetShaderivCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glGetShaderiv")
	}
	if !tryRegister("glGetProgramiv", &glGetProgramivCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glGetProgramiv")
	}
	if !tryRegister("glGetUniformLocation", &glGetUniformLocationCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glGetUniformLocation")
	}
	if !tryRegister("glGenBuffers", &glGenBuffersCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glGenBuffers")
	}
	if !tryRegister("glBindBuffer", &glBindBufferCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glBindBuffer")
	}
	if !tryRegister("glBufferData", &glBufferDataCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glBufferData")
	}
	if !tryRegister("glUseProgram", &glUseProgramCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glUseProgram")
	}
	if !tryRegister("glEnableVertexAttribArray", &glEnableVertexAttribArrayCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glEnableVertexAttribArray")
	}
	if !tryRegister("glVertexAttribPointer", &glVertexAttribPointerCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glVertexAttribPointer")
	}
	if !tryRegister("glUniform4f", &glUniform4fCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glUniform4f")
	}
	if !tryRegister("glUniform2f", &glUniform2fCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glUniform2f")
	}
	if !tryRegister("glUniform1f", &glUniform1fCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glUniform1f")
	}
	if !tryRegister("glDeleteShader", &glDeleteShaderCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glDeleteShader")
	}
	if !tryRegister("glGetAttribLocation", &glGetAttribLocationCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glGetAttribLocation")
	}
	if !tryRegister("glEnable", &glEnableCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glEnable")
	}
	if !tryRegister("glBlendFuncSeparate", &glBlendFuncSeparateCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glBlendFunc")
	}
	if !tryRegister("glDrawArrays", &glDrawArraysCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glDrawArrays")
	}
	if !tryRegister("glClearStencil", &glClearStencilCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glClearStencil")
	}
	if !tryRegister("glStencilFunc", &glStencilFuncCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glStencilFunc")
	}
	if !tryRegister("glStencilOp", &glStencilOpCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glStencilOp")
	}
	if !tryRegister("glStencilMask", &glStencilMaskCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glStencilMask")
	}
	if !tryRegister("glScissor", &glScissorCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glScissor")
	}
	if !tryRegister("glDisable", &glDisableCtx) {
		runtime.UnlockOSThread()
		return fmt.Errorf("failed to load glDisable")
	}

	// A first-launch failure here that a restart clears is a TRANSIENT failure — most often the
	// driver's shader compiler or its shader cache being cold/unavailable for a moment. The old code
	// reported only "program failed to link" with no log and no GL error, so a transient failure could
	// not be told apart from a genuine rejection, and the app died instead of retrying.
	//
	// Retry the whole build, and carry the driver's own link log plus the GL error in the message so
	// a persistent failure names its reason.
	prog, buildErr := buildRectProgram()
	for attempt := 2; buildErr != nil && attempt <= 3; attempt++ {
		time.Sleep(time.Duration(attempt*100) * time.Millisecond)
		p, e := buildRectProgram()
		prog, buildErr = p, e
	}
	if buildErr != nil {
		runtime.UnlockOSThread()
		return fmt.Errorf("%w (persistent after retries)", buildErr)
	}
	rectRenderer.program = prog

	rectRenderer.colorLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uColor\x00")[0])))
	if rectRenderer.colorLoc < 0 {
		runtime.UnlockOSThread()
		return fmt.Errorf("uColor uniform not found")
	}
	rectRenderer.posLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uPos\x00")[0])))
	rectRenderer.sizeLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uSize\x00")[0])))
	rectRenderer.screenSizeLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uScreenSize\x00")[0])))
	rectRenderer.angleLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uAngle\x00")[0])))
	rectRenderer.blendModeLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uBlendMode\x00")[0])))
	rectRenderer.shapeTypeLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uShapeType\x00")[0])))
	rectRenderer.color2Loc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uColor2\x00")[0])))
	rectRenderer.gradientTypeLoc = glGetUniformLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("uGradientType\x00")[0])))

	glGenBuffersCtx(1, unsafe.Pointer(&rectRenderer.vbo))
	glBindBufferCtx(GL_ARRAY_BUFFER, rectRenderer.vbo)

	glBufferDataCtx(GL_ARRAY_BUFFER, 12*4, unsafe.Pointer(&quadVertices[0]), GL_STATIC_DRAW)

	errCode := uint32(0) // Placeholder for error check using ctx func
	_ = errCode

	// Enable vertex attribute and set up pointer while rectVBO is bound
	rectRenderer.posAttrLoc = uint32(glGetAttribLocationCtx(rectRenderer.program, uintptr(unsafe.Pointer(&[]byte("pos\x00")[0]))))
	if rectRenderer.posAttrLoc == 0xFFFFFFFF { // -1 cast to uint32
		rectRenderer.posAttrLoc = 0
	}
	glEnableVertexAttribArrayCtx(rectRenderer.posAttrLoc)
	glVertexAttribPointerCtx(rectRenderer.posAttrLoc, 2, GL_FLOAT, 0, 0, unsafe.Pointer(nil))

	// Create circle VBO
	circleVerts := initCircleVertices()
	glGenBuffersCtx(1, unsafe.Pointer(&rectRenderer.circleVBO))
	glBindBufferCtx(GL_ARRAY_BUFFER, rectRenderer.circleVBO)
	glBufferDataCtx(GL_ARRAY_BUFFER, int32(len(circleVerts))*4, unsafe.Pointer(&circleVerts[0]), GL_STATIC_DRAW)

	rectRenderer.screenWidth = screenWidth
	rectRenderer.screenHeight = screenHeight

	// Create screen-space triangle shader
	triVs := glCreateShaderCtx(GL_VERTEX_SHADER)
	triFs := glCreateShaderCtx(GL_FRAGMENT_SHADER)

	triVertexSrc := glesVertexShader(triSrcVS) + "\x00"
	triFragmentSrc := glesFragmentShader(triSrcFS) + "\x00"

	triVsrc := []byte(triVertexSrc)
	triFsrc := []byte(triFragmentSrc)
	var triVsrcPtr, triFsrcPtr uintptr
	if len(triVsrc) > 0 {
		triVsrcPtr = uintptr(unsafe.Pointer(&triVsrc[0]))
	}
	if len(triFsrc) > 0 {
		triFsrcPtr = uintptr(unsafe.Pointer(&triFsrc[0]))
	}

	triVlen := int32(len(triVertexSrc) - 1)
	triFlen := int32(len(triFragmentSrc) - 1)

	glShaderSourceCtx(triVs, 1, uintptr(unsafe.Pointer(&triVsrcPtr)), unsafe.Pointer(&triVlen))
	glCompileShaderCtx(triVs)
	glShaderSourceCtx(triFs, 1, uintptr(unsafe.Pointer(&triFsrcPtr)), unsafe.Pointer(&triFlen))
	glCompileShaderCtx(triFs)

	rectRenderer.triProgram = glCreateProgramCtx()
	glAttachShaderCtx(rectRenderer.triProgram, triVs)
	glAttachShaderCtx(rectRenderer.triProgram, triFs)
	// Explicit binding (glGetAttribLocation is unreliable in this env).
	glBindAttribLocation(rectRenderer.triProgram, 0, uintptr(unsafe.Pointer(&[]byte("pos\x00")[0])))
	glBindAttribLocation(rectRenderer.triProgram, 1, uintptr(unsafe.Pointer(&[]byte("bary\x00")[0])))
	glBindAttribLocation(rectRenderer.triProgram, 2, uintptr(unsafe.Pointer(&[]byte("color\x00")[0])))
	glBindAttribLocation(rectRenderer.triProgram, 3, uintptr(unsafe.Pointer(&[]byte("clipMin\x00")[0])))
	glBindAttribLocation(rectRenderer.triProgram, 4, uintptr(unsafe.Pointer(&[]byte("clipMax\x00")[0])))
	glLinkProgramCtx(rectRenderer.triProgram)

	rectRenderer.triPosAttrLoc = 0
	rectRenderer.triBaryAttrLoc = 1
	rectRenderer.triColorAttrLoc = 2
	rectRenderer.triClipMinLoc = 3
	rectRenderer.triClipMaxLoc = 4
	rectRenderer.triClipEnLoc = glGetUniformLocationCtx(rectRenderer.triProgram, uintptr(unsafe.Pointer(&[]byte("uClipEnabled\x00")[0])))
	rectRenderer.triScreenSizeLoc = glGetUniformLocationCtx(rectRenderer.triProgram, uintptr(unsafe.Pointer(&[]byte("uScreenSize\x00")[0])))

	if rectRenderer.triPosAttrLoc < 0 {
		rectRenderer.triPosAttrLoc = 0
	}
	if rectRenderer.triBaryAttrLoc < 0 {
		rectRenderer.triBaryAttrLoc = 1
	}
	if rectRenderer.triColorAttrLoc < 0 {
		rectRenderer.triColorAttrLoc = 2
	}
	if rectRenderer.triScreenSizeLoc < 0 {
		rectRenderer.triScreenSizeLoc = 0
	}

	glGenBuffersCtx(1, unsafe.Pointer(&rectRenderer.triVBO))

	initTextRenderer()
	initShapeRenderer()
	initSDFRenderer()

	glDeleteShaderCtx(triVs)
	glDeleteShaderCtx(triFs)

	applyBaseContextState()

	// Clear stencil to default state (disabled)
	glClearStencilCtx(0)
	glStencilFuncCtx(0x0000, 0, 0xFFFFFFFF) // GL_ALWAYS, 0, all bits
	glStencilOpCtx(0x1E00, 0x1E00, 0x1E00)  // GL_KEEP, GL_KEEP, GL_KEEP
	glStencilMaskCtx(0xFFFFFFFF)

	// Set scissor to full window - OpenGL defaults to (0,0,0,0) which blocks rendering
	glScissorCtx(0, 0, int32(screenWidth), int32(screenHeight))
	scissorState.x, scissorState.y = 0, 0
	scissorState.w, scissorState.h = int32(screenWidth), int32(screenHeight)
	scissorState.active = true

	glBlendFuncCtx(0x0302, 0x0303)

	initTextRenderer()
	initBlurRenderer()

	// Set up depth state
	glDepthFuncCtx(GL_LEQUAL)
	glDepthMaskCtx(1)

	// Store global function pointers
	glScissorGlobal = glScissorCtx
	glEnableGlobal = glEnableCtx

	rectRenderer.ready = true
	defaultRenderer = NewRenderer()
	runtime.UnlockOSThread()
	return nil
}

func initTextRenderer() {
	textVs := glCreateShaderCtx(GL_VERTEX_SHADER)
	textFs := glCreateShaderCtx(GL_FRAGMENT_SHADER)

	textVertexSrc := glesVertexShader(textSrcVS) + "\x00"
	textFragmentSrc := glesFragmentShader(textSrcFS) + "\x00"

	tVsrc := []byte(textVertexSrc)
	tFsrc := []byte(textFragmentSrc)
	var tVsrcPtr, tFsrcPtr uintptr
	if len(tVsrc) > 0 {
		tVsrcPtr = uintptr(unsafe.Pointer(&tVsrc[0]))
	}
	if len(tFsrc) > 0 {
		tFsrcPtr = uintptr(unsafe.Pointer(&tFsrc[0]))
	}
	tVlen := int32(len(textVertexSrc) - 1)
	tFlen := int32(len(textFragmentSrc) - 1)

	glShaderSourceCtx(textVs, 1, uintptr(unsafe.Pointer(&tVsrcPtr)), unsafe.Pointer(&tVlen))
	glCompileShaderCtx(textVs)
	glShaderSourceCtx(textFs, 1, uintptr(unsafe.Pointer(&tFsrcPtr)), unsafe.Pointer(&tFlen))
	glCompileShaderCtx(textFs)

	tr := &TextRenderer{}
	tr.program = glCreateProgramCtx()
	glAttachShaderCtx(tr.program, textVs)
	glAttachShaderCtx(tr.program, textFs)
	// Explicit binding (glGetAttribLocation is unreliable in this env).
	glBindAttribLocation(tr.program, 0, uintptr(unsafe.Pointer(&[]byte("pos\x00")[0])))
	glBindAttribLocation(tr.program, 1, uintptr(unsafe.Pointer(&[]byte("uv\x00")[0])))
	glBindAttribLocation(tr.program, 2, uintptr(unsafe.Pointer(&[]byte("color\x00")[0])))
	glBindAttribLocation(tr.program, 3, uintptr(unsafe.Pointer(&[]byte("clipMin\x00")[0])))
	glBindAttribLocation(tr.program, 4, uintptr(unsafe.Pointer(&[]byte("clipMax\x00")[0])))
	glLinkProgramCtx(tr.program)

	tr.posLoc = 0
	tr.uvLoc = 1
	tr.colorAttrLoc = 2
	tr.clipMinLoc = 3
	tr.clipMaxLoc = 4
	tr.clipEnLoc = glGetUniformLocationCtx(tr.program, uintptr(unsafe.Pointer(&[]byte("uClipEnabled\x00")[0])))
	tr.colorLoc = glGetUniformLocationCtx(tr.program, uintptr(unsafe.Pointer(&[]byte("uColor\x00")[0])))
	tr.screenSizeLoc = glGetUniformLocationCtx(tr.program, uintptr(unsafe.Pointer(&[]byte("uScreenSize\x00")[0])))
	tr.textureLoc = glGetUniformLocationCtx(tr.program, uintptr(unsafe.Pointer(&[]byte("uTexture\x00")[0])))
	tr.isImageLoc = glGetUniformLocationCtx(tr.program, uintptr(unsafe.Pointer(&[]byte("uIsImage\x00")[0])))

	if tr.posLoc < 0 {
		tr.posLoc = 0
	}
	if tr.uvLoc < 0 {
		tr.uvLoc = 1
	}
	if tr.colorLoc < 0 {
		tr.colorLoc = 0
	}
	if tr.screenSizeLoc < 0 {
		tr.screenSizeLoc = 0
	}
	if tr.textureLoc < 0 {
		tr.textureLoc = 0
	}

	glGenBuffersCtx(1, unsafe.Pointer(&tr.vbo))
	glDeleteShaderCtx(textVs)
	glDeleteShaderCtx(textFs)

	textRenderer = tr
}

func initBlurRenderer() {
	br := &BlurRenderer{}

	sw := int32(screenWidth)
	sh := int32(screenHeight)
	if sw <= 0 {
		sw = 1280
	}
	if sh <= 0 {
		sh = 720
	}

	// FBO texture
	glGenTextures(1, unsafe.Pointer(&br.texture))
	glBindTexture(GL_TEXTURE_2D, br.texture)
	glTexImage2D(GL_TEXTURE_2D, 0, int32(0x1908), sw, sh, 0, 0x1908, GL_UNSIGNED_BYTE, nil)
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, int32(GL_CLAMP_TO_EDGE))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, int32(GL_CLAMP_TO_EDGE))

	// FBO
	glGenFramebuffers(1, unsafe.Pointer(&br.fbo))
	glBindFramebuffer(GL_FRAMEBUFFER, br.fbo)
	glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, br.texture, 0)
	glCheckFramebufferStatus(GL_FRAMEBUFFER)
	glBindFramebuffer(GL_FRAMEBUFFER, 0)

	// Second FBO for ping-pong blur
	glGenTextures(1, unsafe.Pointer(&br.texture2))
	glBindTexture(GL_TEXTURE_2D, br.texture2)
	glTexImage2D(GL_TEXTURE_2D, 0, int32(0x1908), sw, sh, 0, 0x1908, GL_UNSIGNED_BYTE, nil)
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, int32(GL_LINEAR))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, int32(GL_CLAMP_TO_EDGE))
	glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, int32(GL_CLAMP_TO_EDGE))

	glGenFramebuffers(1, unsafe.Pointer(&br.fbo2))
	glBindFramebuffer(GL_FRAMEBUFFER, br.fbo2)
	glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, br.texture2, 0)
	glCheckFramebufferStatus(GL_FRAMEBUFFER)
	glBindFramebuffer(GL_FRAMEBUFFER, 0)

	// Shader
	vs := glCreateShaderCtx(GL_VERTEX_SHADER)
	fs := glCreateShaderCtx(GL_FRAGMENT_SHADER)

	vsSrc := glesVertexShader(blurSrcVS) + "\x00"
	fsSrc := glesFragmentShader(blurSrcFS) + "\x00"

	vsrc := []byte(vsSrc)
	fsrc := []byte(fsSrc)
	var vsrcPtr, fsrcPtr uintptr
	if len(vsrc) > 0 {
		vsrcPtr = uintptr(unsafe.Pointer(&vsrc[0]))
	}
	if len(fsrc) > 0 {
		fsrcPtr = uintptr(unsafe.Pointer(&fsrc[0]))
	}
	vlen := int32(len(vsSrc) - 1)
	flen := int32(len(fsSrc) - 1)

	glShaderSourceCtx(vs, 1, uintptr(unsafe.Pointer(&vsrcPtr)), unsafe.Pointer(&vlen))
	glCompileShaderCtx(vs)
	glShaderSourceCtx(fs, 1, uintptr(unsafe.Pointer(&fsrcPtr)), unsafe.Pointer(&flen))
	glCompileShaderCtx(fs)

	br.program = glCreateProgramCtx()
	glAttachShaderCtx(br.program, vs)
	glAttachShaderCtx(br.program, fs)
	glLinkProgramCtx(br.program)

	br.texLoc = glGetUniformLocationCtx(br.program, uintptr(unsafe.Pointer(&[]byte("uTexture\x00")[0])))
	br.pixelSizeLoc = glGetUniformLocationCtx(br.program, uintptr(unsafe.Pointer(&[]byte("uPixelSize\x00")[0])))
	br.dirLoc = glGetUniformLocationCtx(br.program, uintptr(unsafe.Pointer(&[]byte("uDir\x00")[0])))

	quad := []float32{-1, -1, 1, -1, 1, 1, -1, 1}
	glGenBuffers(1, unsafe.Pointer(&br.vbo))
	cacheBindVBO(br.vbo)
	glBufferData(GL_ARRAY_BUFFER, int32(len(quad))*4, unsafe.Pointer(&quad[0]), GL_STATIC_DRAW)

	glDeleteShaderCtx(vs)
	glDeleteShaderCtx(fs)
	br.screenWidth = float32(sw)
	br.screenHeight = float32(sh)
	br.ready = true
	blurRenderer = br
}

// Frame batch accumulators — collect draw calls and flush once per Present.
// Clip state rides PER VERTEX (clipMin/clipMax attributes, GL window
// coords) so batches survive clip changes without flushing.
var triBatch struct {
	verts  []float32
	vboCap int32
	dirty  bool
}

var textBatch struct {
	texID   uint32
	verts   []float32
	quads   int
	vboCap  int32
	dirty   bool
	isImage bool
}

var shapeBatch struct {
	verts []float32
	dirty bool
}

// initShapeRenderer builds the batched shape program (attribute-driven
// version of the immediate rect shader).
func initShapeRenderer() {
	vs := glCreateShaderCtx(GL_VERTEX_SHADER)
	fs := glCreateShaderCtx(GL_FRAGMENT_SHADER)

	vSrc := glesVertexShader(shapeSrcVS) + "\x00"
	fSrc := glesFragmentShader(shapeSrcFS) + "\x00"
	vp := []byte(vSrc)
	fp := []byte(fSrc)
	var vpPtr, fpPtr uintptr
	if len(vp) > 0 {
		vpPtr = uintptr(unsafe.Pointer(&vp[0]))
	}
	if len(fp) > 0 {
		fpPtr = uintptr(unsafe.Pointer(&fp[0]))
	}
	vLen := int32(len(vSrc) - 1)
	fLen := int32(len(fSrc) - 1)
	glShaderSourceCtx(vs, 1, uintptr(unsafe.Pointer(&vpPtr)), unsafe.Pointer(&vLen))
	glCompileShaderCtx(vs)
	glShaderSourceCtx(fs, 1, uintptr(unsafe.Pointer(&fpPtr)), unsafe.Pointer(&fLen))
	glCompileShaderCtx(fs)

	sr := &ShapeRenderer{}
	sr.program = glCreateProgramCtx()
	glAttachShaderCtx(sr.program, vs)
	glAttachShaderCtx(sr.program, fs)
	glBindAttribLocation(sr.program, 0, uintptr(unsafe.Pointer(&[]byte("aCenter\x00")[0])))
	glBindAttribLocation(sr.program, 1, uintptr(unsafe.Pointer(&[]byte("aLocal\x00")[0])))
	glBindAttribLocation(sr.program, 2, uintptr(unsafe.Pointer(&[]byte("aHalf\x00")[0])))
	glBindAttribLocation(sr.program, 3, uintptr(unsafe.Pointer(&[]byte("aAngle\x00")[0])))
	glBindAttribLocation(sr.program, 4, uintptr(unsafe.Pointer(&[]byte("aColor\x00")[0])))
	glBindAttribLocation(sr.program, 5, uintptr(unsafe.Pointer(&[]byte("aShapeType\x00")[0])))
	glBindAttribLocation(sr.program, 6, uintptr(unsafe.Pointer(&[]byte("aClipMin\x00")[0])))
	glBindAttribLocation(sr.program, 7, uintptr(unsafe.Pointer(&[]byte("aClipMax\x00")[0])))
	glLinkProgramCtx(sr.program)

	sr.centerLoc = 0
	sr.localLoc = 1
	sr.halfLoc = 2
	sr.angleLoc = 3
	sr.colorLoc = 4
	sr.shapeTypeLoc = 5
	sr.clipMinLoc = 6
	sr.clipMaxLoc = 7
	sr.clipEnLoc = glGetUniformLocationCtx(sr.program, uintptr(unsafe.Pointer(&[]byte("uClipEnabled\x00")[0])))
	sr.screenSizeLoc = glGetUniformLocationCtx(sr.program, uintptr(unsafe.Pointer(&[]byte("uScreenSize\x00")[0])))
	if sr.screenSizeLoc < 0 {
		sr.screenSizeLoc = 0
	}

	glGenBuffersCtx(1, unsafe.Pointer(&sr.vbo))
	glDeleteShaderCtx(vs)
	glDeleteShaderCtx(fs)
	shapeRenderer = sr
	sr.ready = true
}

// clipDiscardEnabled gates the per-vertex clip discard in the batch
// fragment shaders. Offscreen (cached-widget/icon) rendering runs with
// an unclipped full target, where the discard is unnecessary — and a
// driver quirk was observed delivering zero clip attributes there.
var clipDiscardEnabled = true

// SetClipDiscardEnabled toggles the batch shaders' per-vertex clip
// discard. Offscreen rendering turns it off.
func SetClipDiscardEnabled(on bool) { clipDiscardEnabled = on }

// scissorState tracks the current clip on the Go side so batch
// submission can stamp per-vertex clip rects without glGet calls.
//
// Three states are distinct:
//   - active=false: no clip — every fragment passes (frame base state).
//   - active=true, empty=false: clip to the (x,y,w,h) rect.
//   - active=true, empty=true: zero-area clip — every fragment is
//     discarded. An empty INTERSECTION (a row scrolled fully under a
//     header, say) must land here, not in active=false: treating it as
//     "no clip" stamped the fully-hidden draws full-screen and leaked
//     them (DataTable rows painted above the header).
var scissorState struct {
	active bool
	empty  bool
	x, y   int32 // GL window coords (bottom-left origin)
	w, h   int32
}

// setScissorTracked updates the GL scissor and the Go-side mirror.
func setScissorTracked(x, y, w, h int32) {
	if glScissorGlobal != nil {
		glScissorGlobal(x, y, w, h)
	}
	scissorState.x, scissorState.y, scissorState.w, scissorState.h = x, y, w, h
	scissorState.active = true
	scissorState.empty = w <= 0 || h <= 0
}

// ResetClipState clears the Go-side clip mirror at the start of a
// window's frame. scissorState is process-global and is stamped into
// every batched vertex (clipMin/clipMax); without a per-frame reset a
// window's early draws (title bar, panel background) inherit the
// PREVIOUS window's last clip rect — everything outside it is
// shader-discarded and edge primitives clip into garbage shapes
// (observed: window 2 rendered exactly one triangle — the stale rect).
func ResetClipState() {
	scissorState.active = false
	scissorState.empty = false
	scissorState.x, scissorState.y = 0, 0
	scissorState.w, scissorState.h = 0, 0
}

// clipCanvasW/H is the surface the current canvas covers, recorded when a canvas
// is cleared. The unclipped per-vertex clip stamp (curClipQuad) is bounded by it
// so it neither depends on another window's global screen size nor uses an
// unbounded value the shader may overflow.
var clipCanvasW, clipCanvasH float32

func clipEnabledUniform() float32 {
	// MEUI_NO_CLIP=1: experiment. The per-vertex clip test compares
	// gl_FragCoord (bottom-up GL window coords) against clip values that are
	// produced in top-down canvas space - two coordinate paths for one rect.
	// A stale or non-full-canvas rect therefore cuts content VERTICALLY and
	// leaves horizontal drawing untouched, which is the reported asymmetry.
	// Forcing the test off decides whether the fragment clip, rather than the
	// rasteriser or the surface, bounds a short frame.
	if os.Getenv("MEUI_NO_CLIP") != "" {
		return 0
	}
	if clipDiscardEnabled {
		return 1
	}
	return 0
}

// curClipQuad returns the active clip as (minX, minY, maxX, maxY) in GL
// window coords for stamping into per-vertex clip attributes.
// CurClipQuad exposes the per-vertex clip stamp for diagnostics — the same value
// appendWithClip and SDFBatchTexturedQuads put on every vertex. Exported like
// DefaultReadFBO, for probes that need to see what a draw was stamped with.
func CurClipQuad() (float32, float32, float32, float32) { return curClipQuad() }

func curClipQuad() (float32, float32, float32, float32) {
	if !scissorState.active {
		// No clip: bound the stamp by the CURRENT CANVAS, not by an unbounded
		// constant and not by the global screen size.
		//
		// The global (rectRenderer.screenWidth/Height) belongs to whichever window
		// last called SetScreenSize, so most primitives - resetClipState clears
		// scissorState.active every frame - were stamped with another window's or an
		// older size. An unbounded quad (-1e6..1e6) removed that dependency but
		// measured WORSE (P5dj: 620/700 correct-rate 50% -> 0%), which is what a
		// shader that normalises the clip quad would do with an overflowing value.
		// clipCanvasW/H are set by the canvas itself, so this is bounded and
		// belongs to the surface being drawn.
		return 0, 0, clipCanvasW, clipCanvasH
	}
	if scissorState.empty {
		// Zero-area clip: return a degenerate quad so the shader's
		// clip test discards every fragment of the draw.
		return float32(scissorState.x), float32(scissorState.y),
			float32(scissorState.x), float32(scissorState.y)
	}
	return float32(scissorState.x), float32(scissorState.y),
		float32(scissorState.x + scissorState.w), float32(scissorState.y + scissorState.h)
}

// Current antialiasing preference, stamped into each batched vertex because a
// batch spans paint changes — the same reason the clip travels per vertex.
// Set from Paint by Canvas.applyBlend; it defaults to on, which is the SDF
// pipeline's inherent behaviour.
var aaEnabled = true

// SetAntialiasPreference sets the antialiasing stamped into subsequently
// batched primitives. Text and image quads ignore it: they sample a texture
// and have no edge ramp of their own.
func SetAntialiasPreference(on bool) { aaEnabled = on }

// AntialiasPreference reports the current preference.
func AntialiasPreference() bool { return aaEnabled }

// curAA is the per-vertex antialiasing value for the unified batch.
func curAA() float32 {
	if aaEnabled {
		return 1
	}
	return 0
}

// Current global alpha, multiplied into the alpha of every batched
// primitive's colour. Like AA it is a per-draw preference baked into the
// vertices rather than a uniform, because one batch spans paint changes.
var globalAlpha float32 = 1

// SetGlobalAlphaPreference sets the alpha multiplier applied to subsequently
// batched primitives. The legacy immediate paths (gradient rects) ignore it.
func SetGlobalAlphaPreference(a float32) {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	globalAlpha = a
}

// GlobalAlphaPreference reports the current alpha multiplier.
func GlobalAlphaPreference() float32 { return globalAlpha }

// scaleAlphaFloat applies the global alpha to a per-vertex alpha channel.
func scaleAlphaFloat(a float32) float32 {
	if globalAlpha >= 1 {
		return a
	}
	return a * globalAlpha
}

// Current path clip: the half-planes of a convex path clip, in device space,
// intersected with the rect clip. A batch spans clip changes, so these travel
// per vertex (see sdfClipRange) instead of as uniforms, exactly like the rect.
var clipPlaneState []ClipPlane

// clipGen counts changes to clipPlaneState so the batch can register the
// current plane set in its per-batch uniform table once, rather than per
// primitive.
var clipGen int

// SetClipPlanes replaces the path clip. It is the renderer-side mirror the
// batched shaders read; the toolkit updates it whenever the effective clip
// changes.
func SetClipPlanes(planes []ClipPlane) {
	clipPlaneState = planes
	clipGen++
}

// ClipPlanes returns the current path clip (nil when only the rect applies).
func ClipPlanes() []ClipPlane { return clipPlaneState }

// currentGradient is the gradient the next batched primitive carries, with its
// geometry already transformed into device space. Like the clip and the alpha
// it is baked into the vertices rather than left as a uniform, because one
// batch interleaves gradient and solid primitives in draw order.
var currentGradient Gradient

// SetCurrentGradient publishes the gradient for subsequently batched
// primitives; the zero Gradient means solid colour.
func SetCurrentGradient(g Gradient) { currentGradient = g }

// CurrentGradient returns the published gradient.
func CurrentGradient() Gradient { return currentGradient }

// scaleAlpha applies the global alpha to a packed colour, leaving the colour
// channels untouched (they are premultiplied by alpha in the shader, so
// scaling alpha alone scales the whole contribution).
func scaleAlpha(color Color) Color {
	if globalAlpha >= 1 {
		return color
	}
	a := float32((color>>24)&0xFF) * globalAlpha
	if a < 0 {
		a = 0
	}
	return (color & 0x00FFFFFF) | Color(a+0.5)<<24
}

// flushBatches uploads and draws all accumulated geometry for the frame.
// Called at the start of SwapWindow.
func flushBatches() {
	flushSDFBatch()
	if os.Getenv("MEUI_SKIP_TRI") == "1" {
		triBatch.verts = triBatch.verts[:0]
		triBatch.dirty = false
		return2 := true
		_ = return2
	}
	// Shape batch: attribute-driven rects/circles/lines, straight-alpha.
	if shapeRenderer != nil && shapeRenderer.ready && shapeBatch.dirty && len(shapeBatch.verts) > 0 {
		runtime.LockOSThread()
		glDisable(GL_SCISSOR_TEST) // clip is per-vertex
		cacheUseProgram(shapeRenderer.program)

		stride := int32(64) // 16 floats: center2 local2 half2 angle1 color4 shapeType1 clipMin2 clipMax2
		cacheBindVBO(shapeRenderer.vbo)
		uploadBatchData(shapeRenderer.vbo, &shapeRenderer.vboCap, shapeBatch.verts)

		syncAttribMask(0xFF) // attribs 0..7
		glVertexAttribPointer(uint32(shapeRenderer.centerLoc), 2, GL_FLOAT, 0, stride, unsafe.Pointer(nil))
		glVertexAttribPointer(uint32(shapeRenderer.localLoc), 2, GL_FLOAT, 0, stride, ptrOffset(8))
		glVertexAttribPointer(uint32(shapeRenderer.halfLoc), 2, GL_FLOAT, 0, stride, ptrOffset(16))
		glVertexAttribPointer(uint32(shapeRenderer.angleLoc), 1, GL_FLOAT, 0, stride, ptrOffset(24))
		glVertexAttribPointer(uint32(shapeRenderer.colorLoc), 4, GL_FLOAT, 0, stride, ptrOffset(28))
		glVertexAttribPointer(uint32(shapeRenderer.shapeTypeLoc), 1, GL_FLOAT, 0, stride, ptrOffset(44))
		glVertexAttribPointer(uint32(shapeRenderer.clipMinLoc), 2, GL_FLOAT, 0, stride, ptrOffset(48))
		glVertexAttribPointer(uint32(shapeRenderer.clipMaxLoc), 2, GL_FLOAT, 0, stride, ptrOffset(56))

		glUniform2f(shapeRenderer.screenSizeLoc, rectRenderer.screenWidth, rectRenderer.screenHeight)
		if shapeRenderer.clipEnLoc >= 0 {
			glUniform1f(shapeRenderer.clipEnLoc, clipEnabledUniform())
		}

		applyStraightBlend(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
		glDrawArrays(GL_TRIANGLES, 0, int32(len(shapeBatch.verts)/16))
		glDisable(GL_SCISSOR_TEST) // clip is per-vertex: leave scissor off
		runtime.UnlockOSThread()

		atomic.AddInt64(&statDrawCalls, 1)
		atomic.AddInt64(&statFlushes, 1)
		shapeBatch.verts = shapeBatch.verts[:0]
		shapeBatch.dirty = false
	}

	if os.Getenv("MEUI_SKIP_TRI") != "1" {
		if triBatch.dirty && len(triBatch.verts) > 0 {
			if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
				n := len(triBatch.verts)
				show := n
				if show > 45 {
					show = 45
				}
				fmt.Printf("[triBatch] verts=%d first=%v\n", n, triBatch.verts[:show])
			}
			runtime.LockOSThread()
			// Scissor is global state; batched prims carry their clip per
			// vertex, so disable it to keep stale scissor rects from
			// clobbering prims submitted under an earlier clip.
			glDisable(GL_SCISSOR_TEST)
			cacheUseProgram(rectRenderer.triProgram)

			stride := int32(52) // 13 floats: pos2 bary3 color4 clipMin2 clipMax2
			cacheBindVBO(rectRenderer.triVBO)
			uploadBatchData(rectRenderer.triVBO, &triBatch.vboCap, triBatch.verts)

			syncAttribMask(0x1F) // attribs 0..4: pos, bary, color, clipMin, clipMax
			glVertexAttribPointer(uint32(rectRenderer.triPosAttrLoc), 2, GL_FLOAT, 0, stride, unsafe.Pointer(nil))
			glVertexAttribPointer(uint32(rectRenderer.triBaryAttrLoc), 3, GL_FLOAT, 0, stride, ptrOffset(8))
			glVertexAttribPointer(uint32(rectRenderer.triColorAttrLoc), 4, GL_FLOAT, 0, stride, ptrOffset(20))
			glVertexAttribPointer(uint32(rectRenderer.triClipMinLoc), 2, GL_FLOAT, 0, stride, ptrOffset(36))
			glVertexAttribPointer(uint32(rectRenderer.triClipMaxLoc), 2, GL_FLOAT, 0, stride, ptrOffset(44))

			glUniform2f(rectRenderer.triScreenSizeLoc, rectRenderer.screenWidth, rectRenderer.screenHeight)

			// MEUI_PRIM_DEBUG: dump first primitive (pos + clip).
			if os.Getenv("MEUI_PRIM_DEBUG") == "1" && len(triBatch.verts) >= 13 {
				v := triBatch.verts
				fmt.Printf("[prim] tri n=%d first=(%.1f,%.1f) color=(%.2f,%.2f,%.2f,%.2f) clip=(%.0f,%.0f)-(%.0f,%.0f)\n",
					len(v)/13, v[0], v[1], v[5], v[6], v[7], v[8], v[9], v[10], v[11], v[12])
			}

			// Tri shader outputs straight-alpha color — restore the straight
			// blend (the text/image batch switches to premultiplied).
			applyStraightBlend(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
			glDrawArrays(GL_TRIANGLES, 0, int32(len(triBatch.verts)/13))
			glDisable(GL_SCISSOR_TEST) // clip is per-vertex: leave scissor off
			runtime.UnlockOSThread()

			atomic.AddInt64(&statDrawCalls, 1)
			atomic.AddInt64(&statFlushes, 1)
			triBatch.verts = triBatch.verts[:0]
			triBatch.dirty = false
		}
	}

	if textBatch.dirty && len(textBatch.verts) > 0 && textRenderer != nil && os.Getenv("MEUI_SKIP_TEXT") != "1" {
		if os.Getenv("MEUI_PRIM_DEBUG") == "1" {
			v := textBatch.verts
			fmt.Printf("[prim] text n=%d first=(%.1f,%.1f) color=(%.2f,%.2f,%.2f,%.2f) clip=(%.0f,%.0f)-(%.0f,%.0f)\n",
				len(v)/12, v[0], v[1], v[4], v[5], v[6], v[7], v[8], v[9], v[10], v[11])
		}
		runtime.LockOSThread()
		glDisable(GL_SCISSOR_TEST) // clip is per-vertex
		cacheUseProgram(textRenderer.program)
		glBindTexture(GL_TEXTURE_2D, textBatch.texID)
		// Text and image textures are PREMULTIPLIED (see text_fs.glsl):
		// composite with (ONE, ONE_MINUS_SRC_ALPHA). With straight-alpha
		// blending the FBO alpha squared (dst.a = a*a) and cached content
		// rendered dim.
		// LCD subpixel text is premultiplied PER CHANNEL, so the color
		// factor must be ONE_MINUS_SRC_COLOR; the separate alpha spec
		// keeps the framebuffer's opacity compositing sane.
		glBlendFuncSeparate(GL_ONE, GL_ONE_MINUS_SRC_COLOR, GL_ONE, GL_ONE_MINUS_SRC_ALPHA)

		cacheBindVBO(textRenderer.vbo)
		uploadBatchData(textRenderer.vbo, &textBatch.vboCap, textBatch.verts)

		// MEUI_PRIM_DEBUG: verify the atlas bind actually took. A name
		// that is not a texture in THIS context's share group silently
		// binds an empty (opaque-black) texture — glyph quads render as
		// black rects. GL_TEXTURE_BINDING_2D = 0x8069.
		if os.Getenv("MEUI_PRIM_DEBUG") == "1" {
			var bound int32
			glGetIntegerv(0x8069, uintptr(unsafe.Pointer(&bound)))
			if int32(textBatch.texID) != bound {
				fmt.Printf("[prim] TEXTURE BIND MISMATCH: want %d bound %d\n", textBatch.texID, bound)
			}
		}

		stride := int32(48)  // 12 floats: pos2 uv2 color4 clipMin2 clipMax2
		syncAttribMask(0x1F) // attribs 0..4: pos, uv, color, clipMin, clipMax
		glVertexAttribPointer(uint32(textRenderer.posLoc), 2, GL_FLOAT, 0, stride, unsafe.Pointer(nil))
		glVertexAttribPointer(uint32(textRenderer.uvLoc), 2, GL_FLOAT, 0, stride, ptrOffset(8))
		glVertexAttribPointer(uint32(textRenderer.colorAttrLoc), 4, GL_FLOAT, 0, stride, ptrOffset(16))
		glVertexAttribPointer(uint32(textRenderer.clipMinLoc), 2, GL_FLOAT, 0, stride, ptrOffset(32))
		glVertexAttribPointer(uint32(textRenderer.clipMaxLoc), 2, GL_FLOAT, 0, stride, ptrOffset(40))

		glUniform2f(textRenderer.screenSizeLoc, rectRenderer.screenWidth, rectRenderer.screenHeight)
		if textRenderer.clipEnLoc >= 0 {
			glUniform1f(textRenderer.clipEnLoc, clipEnabledUniform())
		}
		if os.Getenv("MEUI_PRIM_DEBUG") == "1" {
			pre := glGetError()
			fmt.Printf("[prim] text tex=%d preErr=%x\n", textBatch.texID, pre)
		}
		glUniform1i(textRenderer.textureLoc, 0)
		isImage := 0
		if textBatch.isImage {
			isImage = 1
		}
		glUniform1i(textRenderer.isImageLoc, int32(isImage))

		cacheBindElemIBO(quadIBO)
		if os.Getenv("MEUI_FLUSH_DEBUG") != "" {
			fmt.Printf("[textBatch] quads=%d verts=%d ibo=%d vboCap=%d\n",
				textBatch.quads, len(textBatch.verts), quadIBO, textBatch.vboCap)
		}
		glDrawElements(GL_TRIANGLES, uint32(textBatch.quads*6), GL_UNSIGNED_SHORT, unsafe.Pointer(nil))
		// Restore straight-alpha blending for the rest of the frame.
		glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
		glDisable(GL_SCISSOR_TEST) // clip is per-vertex: leave scissor off
		runtime.UnlockOSThread()

		atomic.AddInt64(&statDrawCalls, 1)
		atomic.AddInt64(&statFlushes, 1)
		textBatch.verts = textBatch.verts[:0]
		textBatch.quads = 0
		textBatch.dirty = false
	}
}

// glScissorGlobal is the cached scissor function pointer for use outside InitRenderer
var glScissorGlobal func(int32, int32, int32, int32)
var glEnableGlobal func(uint32)

// ---- GPU Timing ----

// GPUTimer provides GPU-side timing for profiling shader performance.
type GPUTimer struct {
	id uint32
}

// NewGPUTimer creates a new GPU timer query.
func NewGPUTimer() *GPUTimer {
	if glGenQueries == nil {
		return nil
	}
	var id uint32
	glGenQueries(1, &id)
	return &GPUTimer{id: id}
}

// Begin starts the GPU timing measurement.
func (t *GPUTimer) Begin() {
	if t == nil || t.id == 0 {
		return
	}
	glBeginQuery(GL_TIME_ELAPSED, t.id)
}

// End stops the GPU timing measurement.
func (t *GPUTimer) End() {
	if t == nil || t.id == 0 {
		return
	}
	glEndQuery(GL_TIME_ELAPSED)
}

// Nanoseconds returns the elapsed GPU time in nanoseconds.
// Returns 0 if the result is not yet available.
func (t *GPUTimer) Nanoseconds() int64 {
	if t == nil || t.id == 0 {
		return 0
	}
	var available int32
	glGetQueryObjectiv(t.id, GL_QUERY_RESULT_AVAILABLE, &available)
	if available == 0 {
		return 0
	}
	var result int32
	glGetQueryObjectiv(t.id, GL_QUERY_RESULT, &result)
	return int64(result)
}

// ---- GL State Cache ----

var (
	cachedProgram   uint32
	cachedBlendMode int32 = -1
	cachedDepthTest int32 = -1
)

// UseProgramCached calls glUseProgram only if the program has changed.
func UseProgramCached(program uint32) {
	if program != cachedProgram {
		cacheUseProgram(program)
		cachedProgram = program
	}
}

// activeBlendMode is the seam blend mode applied to batched 2D draws. It is
// process-global because the GL blend state is global.
var activeBlendMode = BlendModeNormal

// blendFuncsFor maps a seam blend mode onto GL blend factors, written for a
// straight-alpha source. ok is false for modes that fixed-function blending
// cannot express (overlay, soft-light, hue/saturation/luminosity and friends),
// which fall back to the path's normal factors.
func blendFuncsFor(mode BlendMode) (src, dst uint32, ok bool) {
	switch mode {
	case BlendModeNormal:
		return GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA, true
	case BlendModeMultiply:
		return GL_DST_COLOR, GL_ONE_MINUS_SRC_ALPHA, true
	case BlendModeScreen:
		return GL_ONE, GL_ONE_MINUS_SRC_COLOR, true
	default:
		return 0, 0, false
	}
}

// applyStraightBlend installs a batch's own normal factors unless a non-normal
// blend mode is active, in which case the mode's factors win. An inexpressible
// mode falls back to the path's normal factors rather than silently doing
// something else.
func applyStraightBlend(normalSrc, normalDst uint32) {
	if activeBlendMode == BlendModeNormal {
		glBlendFunc(normalSrc, normalDst)
		return
	}
	src, dst, ok := blendFuncsFor(activeBlendMode)
	if !ok {
		glBlendFunc(normalSrc, normalDst)
		return
	}
	glBlendFunc(src, dst)
}

// SetBlendModeCached sets the blend mode only if it has changed.
func SetBlendModeCached(mode int32) {
	if mode != cachedBlendMode {
		if cachedBlendMode < 0 {
			glEnable(GL_BLEND)
		}
		if mode == 0 {
			glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
		} else if mode == 1 {
			glBlendFunc(GL_ZERO, GL_SRC_COLOR)
		} else if mode == 2 {
			glBlendFunc(GL_ONE, GL_ONE)
		} else if mode == 3 {
			glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
		}
		cachedBlendMode = mode
	}
}

// ResetGLCache resets all cached GL state (call after any external GL code).
func ResetGLCache() {
	cachedProgram = 0
	cachedBlendMode = -1
	cachedDepthTest = -1
}

// initRenderData ensures GL function pointers are loaded (called once)
func initRenderData() {
	// Functions are already loaded by loadGL3Functions after SDL3 context creation
	// This function is no longer needed but kept for compatibility
}

func ClearScreen(col uint32) {
	runtime.LockOSThread()

	r := float32((col>>16)&0xFF) / 255
	g := float32((col>>8)&0xFF) / 255
	b := float32(col&0xFF) / 255
	a := float32((col>>24)&0xFF) / 255

	glClearColor(r, g, b, a)
	glClear(GL_COLOR_BUFFER_BIT)

	runtime.UnlockOSThread()
}

// Blur applies a separable Gaussian blur to the current framebuffer contents
func Blur(radius float32) {
	if blurRenderer == nil || !blurRenderer.ready {
		return
	}
	sw := blurRenderer.screenWidth
	sh := blurRenderer.screenHeight

	cacheUseProgram(blurRenderer.program)
	cacheBindVBO(blurRenderer.vbo)
	glVertexAttribPointer(0, 2, GL_FLOAT, 0, 0, unsafe.Pointer(nil))
	cacheEnableAttrib(0)
	glUniform1i(blurRenderer.texLoc, 0)
	glUniform2f(blurRenderer.pixelSizeLoc, 1.0/sw, 1.0/sh)

	// Pass 1: horizontal blur from FBO_A → FBO_B
	glUniform2f(blurRenderer.dirLoc, 1.0, 0.0)
	glBindFramebuffer(GL_FRAMEBUFFER, blurRenderer.fbo2)
	setViewport(0, 0, int32(sw), int32(sh))
	glClear(GL_COLOR_BUFFER_BIT)
	glBindTexture(GL_TEXTURE_2D, blurRenderer.texture)
	glDrawArrays(GL_TRIANGLES, 0, 6)

	// Pass 2: vertical blur from FBO_B → screen
	glUniform2f(blurRenderer.dirLoc, 0.0, 1.0)
	glBindFramebuffer(GL_FRAMEBUFFER, 0)
	setViewport(0, 0, int32(sw), int32(sh))
	glBindTexture(GL_TEXTURE_2D, blurRenderer.texture2)
	glDrawArrays(GL_TRIANGLES, 0, 6)
}

// BeginBlur redirects rendering to the FBO texture for offscreen capture.
func BeginBlur() {
	if blurRenderer == nil || !blurRenderer.ready {
		return
	}
	glBindFramebuffer(GL_FRAMEBUFFER, blurRenderer.fbo)
	setViewport(0, 0, int32(blurRenderer.screenWidth), int32(blurRenderer.screenHeight))
	glClear(GL_COLOR_BUFFER_BIT)
}

// EndBlur unbinds the FBO and blurs the result to the screen.
func EndBlur(radius float32) {
	if blurRenderer == nil || !blurRenderer.ready {
		return
	}
	glBindFramebuffer(GL_FRAMEBUFFER, 0)
	setViewport(0, 0, int32(blurRenderer.screenWidth), int32(blurRenderer.screenHeight))
	Blur(radius)
}

// Gaussian calculates a Gaussian weight for the given distance and sigma
func Gaussian(x, sigma float32) float32 {
	coef := 1.0 / (sigma * 2.506628274631) // 1 / (sigma * sqrt(2*PI))
	exp := -(x * x) / (2 * sigma * sigma)
	return coef * float32(expf(float64(exp)))
}

func expf(x float64) float64 {
	// Taylor series approximation for e^x around 0
	// e^x = 1 + x + x^2/2! + x^3/3! + x^4/4! + ...
	return x*x*x*x/24 + x*x*x/6 + x*x/2 + x + 1
}

// GaussianWeights calculates normalized Gaussian weights for blur kernel
func GaussianWeights(radius int, sigma float32) []float32 {
	weights := make([]float32, radius*2+1)
	total := float32(0)
	for i := -radius; i <= radius; i++ {
		w := Gaussian(float32(i), sigma)
		weights[i+radius] = w
		total += w
	}
	// Normalize
	for i := range weights {
		weights[i] /= total
	}
	return weights
}

// DumpRenderState prints the process-global render state that is neither
// per-context GL state nor covered by InvalidateGLStateCache: the screen-size
// uniforms, the FBO currently bound, and the clip-discard flag and plane table.
//
// The offscreen pass (RenderOffscreen) runs in the middle of a window's frame
// and mutates all of these; the window that performed it then looks fine while
// the windows painted after it break, which points at state consumed at the
// START of the next frame. Comparing this line across an offscreen pass and the
// next ClearWithViewport is how that state is found. Gated by MEUI_STATE_TRACE.
func DumpRenderState(where string) {
	if os.Getenv("MEUI_STATE_TRACE") == "" {
		return
	}
	var fbo int32
	if glGetIntegerv != nil {
		glGetIntegerv(0x8CA6 /*GL_FRAMEBUFFER_BINDING*/, uintptr(unsafe.Pointer(&fbo)))
	}
	rw, rh := float32(0), float32(0)
	if rectRenderer != nil {
		rw, rh = rectRenderer.screenWidth, rectRenderer.screenHeight
	}
	log.Printf("[state] %-22s screen=%.0fx%.0f rect=%.0fx%.0f fbo=%d clipDiscard=%v planes=%d gen=%d",
		where, screenWidth, screenHeight, rw, rh, fbo, clipDiscardEnabled, len(clipPlaneState), clipGen)
}
