package mobilegles

// RunHeadlessDemo runs the headless demo with simulated input.
func RunHeadlessDemo(win *Window) {
	// Simulate mouse and keyboard events
	win.PushEvent(Event{Type: EventMouseMove, X: 400, Y: 300})
	win.PushEvent(Event{Type: EventMouseDown, X: 400, Y: 300, Button: ButtonLeft})
	win.PushEvent(Event{Type: EventKeyDown, Key: KeyEscape})
}
