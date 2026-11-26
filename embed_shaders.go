package mobilegles

import (
	_ "embed"
)

//go:embed shaders/rect_vs.glsl
var rectSrcVS string

//go:embed shaders/rect_fs.glsl
var rectSrcFS string

//go:embed shaders/tri_vs.glsl
var triSrcVS string

//go:embed shaders/tri_fs.glsl
var triSrcFS string

//go:embed shaders/shape_vs.glsl
var shapeSrcVS string

//go:embed shaders/shape_fs.glsl
var shapeSrcFS string

//go:embed shaders/sdf_vs.glsl
var sdfSrcVS string

//go:embed shaders/sdf_fs.glsl
var sdfSrcFS string

//go:embed shaders/text_vs.glsl
var textSrcVS string

//go:embed shaders/text_fs.glsl
var textSrcFS string

//go:embed shaders/blur_vs.glsl
var blurSrcVS string

//go:embed shaders/blur_fs.glsl
var blurSrcFS string
