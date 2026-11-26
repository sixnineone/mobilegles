package mobilegles

import "testing"

// blendFuncsFor is the mode mapping the batched draw paths consult. Normal
// must stay exactly the straight-alpha factors the renderer used before blend
// modes were implemented, or every existing scene changes.
func TestBlendFuncsFor(t *testing.T) {
	src, dst, ok := blendFuncsFor(BlendModeNormal)
	if !ok || src != GL_SRC_ALPHA || dst != GL_ONE_MINUS_SRC_ALPHA {
		t.Fatalf("Normal = (%#x,%#x,%v), want the straight-alpha factors", src, dst, ok)
	}
	if src, dst, ok = blendFuncsFor(BlendModeMultiply); !ok || src != GL_DST_COLOR || dst != GL_ONE_MINUS_SRC_ALPHA {
		t.Fatalf("Multiply = (%#x,%#x,%v)", src, dst, ok)
	}
	if src, dst, ok = blendFuncsFor(BlendModeScreen); !ok || src != GL_ONE || dst != GL_ONE_MINUS_SRC_COLOR {
		t.Fatalf("Screen = (%#x,%#x,%v)", src, dst, ok)
	}
	// Modes fixed-function blending cannot express must report not-ok, so the
	// flush falls back to normal instead of silently choosing something else.
	if _, _, ok = blendFuncsFor(BlendModeOverlay); ok {
		t.Fatal("Overlay should not claim to be expressible")
	}
	if _, _, ok = blendFuncsFor(BlendModeSoftLight); ok {
		t.Fatal("SoftLight should not claim to be expressible")
	}
}

func TestActiveBlendModeDefaultsToNormal(t *testing.T) {
	if activeBlendMode != BlendModeNormal {
		t.Fatalf("activeBlendMode = %v, want Normal", activeBlendMode)
	}
}
