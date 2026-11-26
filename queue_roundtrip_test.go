package mobilegles

import "testing"

func TestEventQueueRoundTrip(t *testing.T) {
	w := &Window{queue: NewEventQueue()}
	w.PushEvent(Event{Type: EventMouseDown, X: 100, Y: 50, Button: 1})
	got := w.Poll()
	if got.Type != EventMouseDown || got.X != 100 || got.Y != 50 {
		t.Fatalf("round-trip failed: %+v", got)
	}
	if e := w.Poll(); e.Type != EventNone {
		t.Fatalf("queue not empty: %+v", e)
	}
	// and through PollEvents dispatch
	w.PushEvent(Event{Type: EventMouseDown, X: 7, Y: 8, Button: 1})
	var seen Event
	w.PollEvents(func(e Event) { seen = e })
	if seen.X != 7 || seen.Y != 8 {
		t.Fatalf("PollEvents dispatch failed: %+v", seen)
	}
}
