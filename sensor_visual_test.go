//go:build visual

package mobilegles

import (
	"encoding/binary"
	"math"
	"testing"
)

// TestSensorEventDecodeIsWiredToTheHandler is the end-to-end check for the
// sensor path that no desktop can exercise with real hardware: it pushes a
// synthetic SDL_EVENT_SENSOR_UPDATE through the real SDL queue, runs the real
// pump, and requires the installed handler to receive the id and the values.
//
// It is the test that would have caught a wrong struct offset, which is the
// realistic failure here — SDL_SensorEvent puts `which` at offset 16 (the slot
// the event header calls windowID) and data[6] at 20, and reading either one
// four bytes off still produces plausible-looking floats.
func TestSensorEventDecodeIsWiredToTheHandler(t *testing.T) {
	win := InitWindow(320, 240)
	if win == nil {
		t.Skip("no SDL3 window; cannot exercise the pump")
	}

	type sample struct {
		id     uint32
		values [6]float32
	}
	var got []sample
	SetSensorHandler(func(id uint32, values [6]float32) {
		got = append(got, sample{id, values})
	})
	defer SetSensorHandler(nil)

	const sensorID = 42
	want := [6]float32{1.5, -2.25, 3.75, 4, 5, 6}

	// A 64-byte SDL_Event: type@0, reserved@4, timestamp@8, which@16,
	// data[6]@20 (float32 each), sensor_timestamp@44.
	buf := make([]byte, 64)
	binary.LittleEndian.PutUint32(buf[0:], 0x1200) // SDL_EVENT_SENSOR_UPDATE
	binary.LittleEndian.PutUint32(buf[16:], sensorID)
	for i, v := range want {
		binary.LittleEndian.PutUint32(buf[20+4*i:], math.Float32bits(v))
	}

	SDL3PushEvent(buf)
	PumpAll()

	if len(got) == 0 {
		t.Fatal("the pump delivered no sensor sample: either SDL rejected the pushed " +
			"event or the decode case is not reached")
	}
	if got[0].id != sensorID {
		t.Errorf("sensor id = %d, want %d (a wrong offset reads the header's windowID)", got[0].id, sensorID)
	}
	for i := range want {
		if got[0].values[i] != want[i] {
			t.Errorf("value[%d] = %v, want %v", i, got[0].values[i], want[i])
		}
	}
}
