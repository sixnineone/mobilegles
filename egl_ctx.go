//go:build !android

package mobilegles

func eglContextReady() bool {
	return eglCtx != nil && eglCtx.ctx != nil && eglGetProcAddress != nil
}
