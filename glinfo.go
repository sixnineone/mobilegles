package mobilegles

import "unsafe"

// GL string and limit queries.
//
// These exist because "which driver am I actually rendering on, and what are its
// limits" is a question this project keeps having to answer, and guessing it from
// log noise has already put a wrong claim in MIGRATION.md once. Call them with a
// context current; they return empty/zero when there is none.

// GLInfo returns the driver's vendor, renderer and version strings.
func GLInfo() (vendor, renderer, version string) {
	return glString(0x1F00 /*GL_VENDOR*/),
		glString(0x1F01 /*GL_RENDERER*/),
		glString(0x1F02 /*GL_VERSION*/)
}

// glString reads a NUL-terminated GL string. GL returns a C string, so it has to
// be walked byte by byte; there is no length to query.
func glString(name uint32) string {
	if glGetString == nil {
		return ""
	}
	p := glGetString(name)
	if p == 0 {
		return ""
	}
	var b []byte
	for i := uintptr(0); i < 1<<16; i++ {
		c := *(*byte)(unsafe.Pointer(p + i))
		if c == 0 {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// MaxVertexAttribs reports GL_MAX_VERTEX_ATTRIBS, or 0 when it cannot be read.
// The unified SDF batch uses 16 attributes, so on a driver reporting fewer the
// gradient attributes would silently not bind.
func MaxVertexAttribs() int {
	if glGetIntegerv == nil {
		return 0
	}
	var n int32
	glGetIntegerv(0x8869 /*GL_MAX_VERTEX_ATTRIBS*/, uintptr(unsafe.Pointer(&n)))
	return int(n)
}

// MaxTextureUnits reports GL_MAX_COMBINED_TEXTURE_IMAGE_UNITS, which bounds how
// many distinct textures one batch can sample.
func MaxTextureUnits() int {
	if glGetIntegerv == nil {
		return 0
	}
	var n int32
	glGetIntegerv(0x8B4D /*GL_MAX_COMBINED_TEXTURE_IMAGE_UNITS*/, uintptr(unsafe.Pointer(&n)))
	return int(n)
}
