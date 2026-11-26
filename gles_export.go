package mobilegles

import "github.com/ebitengine/purego"

// LoadGLESFromLib registers all GLES function pointers from a loaded library handle.
// Used by platform backends (e.g. Android) to load GL functions after EGL init.
func LoadGLESFromLib(lib uintptr) {
	purego.RegisterLibFunc(&glActiveTexture, lib, "glActiveTexture")
	purego.RegisterLibFunc(&glAttachShader, lib, "glAttachShader")
	purego.RegisterLibFunc(&glBindBuffer, lib, "glBindBuffer")
	purego.RegisterLibFunc(&glBindFramebuffer, lib, "glBindFramebuffer")
	purego.RegisterLibFunc(&glBindTexture, lib, "glBindTexture")
	purego.RegisterLibFunc(&glBlendFunc, lib, "glBlendFunc")
	purego.RegisterLibFunc(&glClear, lib, "glClear")
	purego.RegisterLibFunc(&glClearColor, lib, "glClearColor")
	purego.RegisterLibFunc(&glCreateProgram, lib, "glCreateProgram")
	purego.RegisterLibFunc(&glCreateShader, lib, "glCreateShader")
	purego.RegisterLibFunc(&glDrawArrays, lib, "glDrawArrays")
	purego.RegisterLibFunc(&glDrawElements, lib, "glDrawElements")
	purego.RegisterLibFunc(&glDepthFunc, lib, "glDepthFunc")
	purego.RegisterLibFunc(&glDepthMask, lib, "glDepthMask")
	purego.RegisterLibFunc(&glEnable, lib, "glEnable")
	purego.RegisterLibFunc(&glDisable, lib, "glDisable")
	purego.RegisterLibFunc(&glEnableVertexAttribArray, lib, "glEnableVertexAttribArray")
	purego.RegisterLibFunc(&glGenBuffers, lib, "glGenBuffers")
	purego.RegisterLibFunc(&glGenFramebuffers, lib, "glGenFramebuffers")
	purego.RegisterLibFunc(&glGenTextures, lib, "glGenTextures")
	purego.RegisterLibFunc(&glGetError, lib, "glGetError")
	purego.RegisterLibFunc(&glGetString, lib, "glGetString")
	purego.RegisterLibFunc(&glLinkProgram, lib, "glLinkProgram")
	purego.RegisterLibFunc(&glShaderSource, lib, "glShaderSource")
	purego.RegisterLibFunc(&glTexImage2D, lib, "glTexImage2D")
	purego.RegisterLibFunc(&glTexSubImage2D, lib, "glTexSubImage2D")
	purego.RegisterLibFunc(&glTexParameteri, lib, "glTexParameteri")
	purego.RegisterLibFunc(&glFramebufferTexture2D, lib, "glFramebufferTexture2D")
	purego.RegisterLibFunc(&glCheckFramebufferStatus, lib, "glCheckFramebufferStatus")
	purego.RegisterLibFunc(&glUniform1i, lib, "glUniform1i")
	purego.RegisterLibFunc(&glUniform1f, lib, "glUniform1f")
	purego.RegisterLibFunc(&glUniform2f, lib, "glUniform2f")
	purego.RegisterLibFunc(&glUniform3f, lib, "glUniform3f")
	purego.RegisterLibFunc(&glUniform4f, lib, "glUniform4f")
	purego.RegisterLibFunc(&glUniformMatrix4fv, lib, "glUniformMatrix4fv")
	purego.RegisterLibFunc(&glUseProgram, lib, "glUseProgram")
	purego.RegisterLibFunc(&glViewport, lib, "glViewport")
	purego.RegisterLibFunc(&glVertexAttribPointer, lib, "glVertexAttribPointer")
	purego.RegisterLibFunc(&glDeleteBuffers, lib, "glDeleteBuffers")
	purego.RegisterLibFunc(&glDeleteFramebuffers, lib, "glDeleteFramebuffers")
	purego.RegisterLibFunc(&glDeleteProgram, lib, "glDeleteProgram")
	purego.RegisterLibFunc(&glDeleteShader, lib, "glDeleteShader")
	purego.RegisterLibFunc(&glDeleteTextures, lib, "glDeleteTextures")
	purego.RegisterLibFunc(&glScissor, lib, "glScissor")
	purego.RegisterLibFunc(&glStencilFunc, lib, "glStencilFunc")
	purego.RegisterLibFunc(&glStencilOp, lib, "glStencilOp")
	purego.RegisterLibFunc(&glStencilMask, lib, "glStencilMask")
	purego.RegisterLibFunc(&glGetIntegerv, lib, "glGetIntegerv")
	purego.RegisterLibFunc(&glGetShaderiv, lib, "glGetShaderiv")
	purego.RegisterLibFunc(&glGetProgramiv, lib, "glGetProgramiv")
	purego.RegisterLibFunc(&glGetUniformLocation, lib, "glGetUniformLocation")
	purego.RegisterLibFunc(&glGetAttribLocation, lib, "glGetAttribLocation")
	purego.RegisterLibFunc(&glGetShaderInfoLog, lib, "glGetShaderInfoLog")
	purego.RegisterLibFunc(&glBufferData, lib, "glBufferData")
	purego.RegisterLibFunc(&glBufferSubData, lib, "glBufferSubData")
	purego.RegisterLibFunc(&glCompileShader, lib, "glCompileShader")
	purego.RegisterLibFunc(&glReadPixels, lib, "glReadPixels")

	// Optional GPU timer query functions (not in GLES 2.0 core)
	sym, _ := purego.Dlsym(lib, "glGenQueries")
	if sym != 0 {
		purego.RegisterFunc(&glGenQueries, sym)
	}
	sym, _ = purego.Dlsym(lib, "glBeginQuery")
	if sym != 0 {
		purego.RegisterFunc(&glBeginQuery, sym)
	}
	sym, _ = purego.Dlsym(lib, "glEndQuery")
	if sym != 0 {
		purego.RegisterFunc(&glEndQuery, sym)
	}
	sym, _ = purego.Dlsym(lib, "glGetQueryObjectiv")
	if sym != 0 {
		purego.RegisterFunc(&glGetQueryObjectiv, sym)
	}
}
