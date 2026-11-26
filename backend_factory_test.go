package mobilegles

import "testing"

// The renderer is chosen through a factory so a second backend can be
// installed without NewCanvas naming a concrete renderer.
func TestRendererFactoryIsPluggable(t *testing.T) {
	if NewRenderer() == nil {
		t.Fatal("default factory returned nil")
	}
	custom := &DesktopRenderer{}
	SetRendererFactory(func() Renderer { return custom })
	if NewRenderer() != Renderer(custom) {
		t.Fatal("NewRenderer did not use the installed factory")
	}
	// A nil factory must not disable the default.
	SetRendererFactory(nil)
	if NewRenderer() == nil {
		t.Fatal("nil factory wiped the default")
	}
	SetRendererFactory(func() Renderer { return &DesktopRenderer{} })
}
