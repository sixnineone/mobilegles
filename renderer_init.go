package mobilegles

import "unsafe"

// This file provides the Ctx variables that renderer.go expects.
// They are initialized by initCtxVars() which is called from InitRenderer()
// AFTER loadGL3Functions() has populated the module-level GL pointers.
// This avoids the "captures nil at package init" problem of var = var aliases.

var (
	glCreateShaderCtx            func(uint32) uint32
	glCompileShaderCtx           func(uint32)
	glShaderSourceCtx            func(uint32, int32, uintptr, unsafe.Pointer)
	glCreateProgramCtx           func() uint32
	glAttachShaderCtx            func(uint32, uint32)
	glLinkProgramCtx             func(uint32)
	glGetShaderivCtx             func(uint32, uint32, uintptr)
	glGetProgramivCtx            func(uint32, uint32, uintptr)
	glGetUniformLocationCtx      func(uint32, uintptr) int32
	glGenBuffersCtx              func(int32, unsafe.Pointer)
	glBindBufferCtx              func(uint32, uint32)
	glBufferDataCtx              func(uint32, int32, unsafe.Pointer, uint32)
	glUseProgramCtx              func(uint32)
	glEnableVertexAttribArrayCtx func(uint32)
	glVertexAttribPointerCtx     func(uint32, int32, uint32, uint8, int32, unsafe.Pointer)
	glUniform4fCtx               func(int32, float32, float32, float32, float32)
	glUniform2fCtx               func(int32, float32, float32)
	glUniform1fCtx               func(int32, float32)
	glDeleteShaderCtx            func(uint32)
	glGetAttribLocationCtx       func(uint32, uintptr) int32
	glEnableCtx                  func(uint32)
	glBlendFuncCtx               func(uint32, uint32)
	glBlendFuncSeparateCtx       func(uint32, uint32, uint32, uint32)
	glDrawArraysCtx              func(uint32, int32, int32)
	glStencilFuncCtx             func(uint32, int32, uint32)
	glStencilOpCtx               func(uint32, uint32, uint32)
	glStencilMaskCtx             func(uint32)
	glGetShaderInfoLogCtx        func(uint32, int32, uintptr, uintptr)
	glGetProgramInfoLogCtx       func(uint32, int32, uintptr, uintptr)
	glScissorCtx                 func(int32, int32, int32, int32)
	glUniform1iCtx               func(int32, int32)
	glDepthFuncCtx               func(uint32)
	glDepthMaskCtx               func(uint8)
	glUniform3fCtx               func(int32, float32, float32, float32)
	glUniformMatrix4fvCtx        func(int32, int32, uint8, *float32)
)

// glClearStencilCtx and glDisableCtx are no-ops since these functions
// are not declared/loaded in gles.go.
var glClearStencilCtx = func(int32) {}
var glDisableCtx = func(uint32) {}

func initCtxVars() {
	glCreateShaderCtx = glCreateShader
	glCompileShaderCtx = glCompileShader
	glShaderSourceCtx = glShaderSource
	glCreateProgramCtx = glCreateProgram
	glAttachShaderCtx = glAttachShader
	glLinkProgramCtx = glLinkProgram
	glGetShaderivCtx = glGetShaderiv
	glGetProgramivCtx = glGetProgramiv
	if glGetProgramInfoLog != nil {
		glGetProgramInfoLogCtx = glGetProgramInfoLog
	}
	glGetUniformLocationCtx = glGetUniformLocation
	glGenBuffersCtx = glGenBuffers
	glBindBufferCtx = glBindBuffer
	glBufferDataCtx = glBufferData
	glUseProgramCtx = glUseProgram
	glEnableVertexAttribArrayCtx = glEnableVertexAttribArray
	glVertexAttribPointerCtx = glVertexAttribPointer
	glUniform4fCtx = glUniform4f
	glUniform2fCtx = glUniform2f
	glUniform1fCtx = glUniform1f
	glDeleteShaderCtx = glDeleteShader
	glGetAttribLocationCtx = glGetAttribLocationFunc
	glEnableCtx = glEnable
	glBlendFuncCtx = glBlendFunc
	glBlendFuncSeparateCtx = glBlendFuncSeparate
	glDrawArraysCtx = glDrawArrays
	glStencilFuncCtx = glStencilFunc
	glStencilOpCtx = glStencilOp
	glStencilMaskCtx = glStencilMask
	glScissorCtx = glScissor
	glUniform1iCtx = glUniform1i
	glDepthFuncCtx = glDepthFunc
	glDepthMaskCtx = glDepthMask
	glUniform3fCtx = glUniform3f
	glUniformMatrix4fvCtx = glUniformMatrix4fv
	glGetShaderInfoLogCtx = glGetShaderInfoLog
}

func glGetAttribLocationFunc(program uint32, name uintptr) int32 {
	return glGetAttribLocation(program, name)
}

func tryRegister(name string, fnPtr interface{}) bool {
	_ = name
	_ = fnPtr
	return true
}
