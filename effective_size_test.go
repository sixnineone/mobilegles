package mobilegles

import (
	"testing"
	"time"
)

// TestEffectiveSizeQuietPrefersProbeOverStaleAck pins the post-drag
// flash fix: during drags, ConfigureNotify events are PROCESSED after
// requests are SENT, so the newest-recorded ack can carry a mid-drag
// size while its timestamp looks current. After the quiet period the
// live probe (SDL's own pumped cache) must win; the stale ack must
// never revert the cache (that revert was the 1s-after-release flash).
func TestEffectiveSizeQuietPrefersProbeOverStaleAck(t *testing.T) {
	restore := probeWindowSize
	defer func() { probeWindowSize = restore }()

	w := &Window{}
	w.SetSize(1000, 700) // optimistic cache == final requested size
	old := time.Now().Add(-2 * time.Second)
	w.reqNano = old.UnixNano() // drag ended >1s ago
	w.ackNano = old.Add(10 * time.Millisecond).UnixNano()
	w.ackW, w.ackH = 997, 700 // stale mid-drag ack, "newer" than req

	probeWindowSize = func(*Window) (int32, int32) { return 1000, 700 } // real window

	gw, gh := EffectiveSize(w)
	if gw != 1000 || gh != 700 {
		t.Fatalf("EffectiveSize()=(%d,%d); want (1000,700) — stale ack reverted the cache", gw, gh)
	}
}

// TestEffectiveSizeQuietAckFresherThanProbe keeps the maximize fix: when
// the session never issued a client request (reqNano==0 — pure WM
// geometry, e.g. maximize on a fresh window), the ack event data is
// adopted even when the probe agrees with the stale cache (SDL's cache
// lagging its own event).
func TestEffectiveSizeQuietAckFresherThanProbe(t *testing.T) {
	restore := probeWindowSize
	defer func() { probeWindowSize = restore }()

	w := &Window{}
	w.SetSize(1100, 750)
	w.reqNano = 0 // no client request this session — pure WM action
	w.ackNano = time.Now().UnixNano()
	w.ackW, w.ackH = 1920, 1056 // maximize ack, SDL cache not caught up

	probeWindowSize = func(*Window) (int32, int32) { return 1100, 750 }

	gw, gh := EffectiveSize(w)
	if gw != 1920 || gh != 1056 {
		t.Fatalf("EffectiveSize()=(%d,%d); want (1920,1056) — maximize ack not adopted", gw, gh)
	}
}

// TestEffectiveSizeDuringDragTrustedCache pins that no adoption happens
// within the quiet window (mid-drag probe/ack values must not rewind
// the optimistic cache).
func TestEffectiveSizeDuringDragTrustedCache(t *testing.T) {
	restore := probeWindowSize
	defer func() { probeWindowSize = restore }()

	w := &Window{}
	w.SetSize(900, 640) // mid-drag optimistic size
	w.reqNano = time.Now().UnixNano()
	w.ackNano = w.reqNano
	w.ackW, w.ackH = 903, 640

	probeWindowSize = func(*Window) (int32, int32) { return 1100, 750 } // lagging SDL cache

	gw, gh := EffectiveSize(w)
	if gw != 900 || gh != 640 {
		t.Fatalf("EffectiveSize()=(%d,%d); want (900,640) — mid-drag adoption rewound cache", gw, gh)
	}
}
