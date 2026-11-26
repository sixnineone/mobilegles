package mobilegles

func (c *Canvas) DrawCirclePath(cx, cy, r float32, p *Paint) {
	c.DrawCircle(cx, cy, r, p)
}

func (f *Font) Descender() float32 {
	if f == nil {
		return 0
	}
	return f.size * 0.15
}
