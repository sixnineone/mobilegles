package mobilegles

// sdl3_keyboard_test.go — the keyboard, end to end.
//
// The report this file answers: keys type the WRONG CHARACTER sometimes, and the suspicion is the SDL key
// mappings. Driving a synthetic keyboard event through the REAL pump (the same technique as
// `TestWheelEventsCarryTheModifierState`) tests the whole chain at once — the struct offsets, the
// scancode→Keycode mapping, and the modifier decoding — because a test that calls `mapSDL3Key` directly
// would pass while the offsets were wrong.
//
// WHAT THE PROBE AGAINST THE INSTALLED SDL3 HEADERS SHOWS, and it is the reason this file exists:
//
//	SDL_SCANCODE_A=4  Z=29  1=30  0=39  RETURN=40  ESCAPE=41  F1=58  F12=69
//	SDL_SCANCODE_KP_1=89  KP_0=98  KP_PERIOD=99
//	SDL_SCANCODE_UP=82  DOWN=81  LEFT=80  RIGHT=79
//	SDL_SCANCODE_DELETE=76  HOME=74  END=77  PAGEUP=75  PAGEDOWN=78  INSERT=73
//
// SDL3 made its scancodes the USB HID usage IDs, so most keys pass through unchanged and `mapSDL3Key`
// has only ever mapped the four ARROWS — where the two schemes DIVERGE (HID has Up=200, Delete=211,
// Home=199). Every other diverging key falls through `return Keycode(key)`, so it arrives as a bare SDL
// number that means nothing in the HID space the rest of the toolkit speaks. And the Keycode enum has no
// keypad constants at all, so a keypad digit is just the number 89.
//
// `TestTheWholeKeyboardMapsToDistinctToolkitKeycodes` is the test that catches the fall-through: it
// requires that two DIFFERENT physical keys never arrive as the same toolkit keycode, which is the failure
// a user sees as "the wrong character appeared".

import (
	"testing"
	"unsafe"
)

// keyCase is one physical key, as the OS reports it, and what the toolkit must answer.
type keyCase struct {
	name     string
	scancode int32
	want     Keycode
}

// keyHarness drives the real pump with synthetic SDL keyboard events and collects what it delivers.
type keyHarness struct {
	delivered []Event
	polls     int
	// repeat sets SDL_KeyboardEvent.repeat, the byte the pump reads to tell an OS auto-repeat from a
	// fresh press. The harness wrote only scancode and mod, which is why nothing exercised it.
	repeat bool
}

func (k *keyHarness) install(t *testing.T, scancode int32, mod uint16) func() {
	t.Helper()
	const winID = 5150
	h := &sdlWin{id: winID, w: 800, h: 600}

	winMu.Lock()
	sdlWinReg[winID] = h
	winMu.Unlock()

	prevPoll, prevMod, prevWindow := SDL3_PollEvent, SDL3_GetModState, sdl3Window
	SDL3_PollEvent = func(event unsafe.Pointer) bool {
		if k.polls > 0 {
			return false
		}
		k.polls++
		buf := unsafe.Slice((*byte)(event), 128)
		for i := range buf {
			buf[i] = 0
		}
		*(*uint32)(unsafe.Pointer(&buf[0])) = SDL3_EVENT_KEY_DOWN
		*(*uint32)(unsafe.Pointer(&buf[16])) = winID
		*(*int32)(unsafe.Pointer(&buf[sdl3OffsetKeyScancode])) = scancode
		*(*uint16)(unsafe.Pointer(&buf[sdl3OffsetKeyMod])) = mod
		if k.repeat {
			*(*uint8)(unsafe.Pointer(&buf[sdl3OffsetKeyRepeat])) = 1
		}
		return true
	}
	SDL3_GetModState = func() uint16 { return mod }

	return func() {
		SDL3_PollEvent, SDL3_GetModState, sdl3Window = prevPoll, prevMod, prevWindow
		winMu.Lock()
		delete(sdlWinReg, winID)
		winMu.Unlock()
	}
}

func (k *keyHarness) fire(t *testing.T, scancode int32, mod uint16) Event {
	t.Helper()
	k.delivered = nil
	k.polls = 0
	restore := k.install(t, scancode, mod)
	sdl3Window = unsafe.Pointer(&sdlWin{id: 5150})
	pumpSDLEvents(func(_ uint32, ev Event) { k.delivered = append(k.delivered, ev) }, nil)
	restore()
	if len(k.delivered) == 0 {
		t.Fatalf("scancode %d (mod %d) delivered no event at all", scancode, mod)
	}
	return k.delivered[0]
}

// press returns the toolkit keycode the pump produced for a physical key.
func press(t *testing.T, scancode int32) Keycode {
	t.Helper()
	var h keyHarness
	ev := h.fire(t, scancode, 0)
	if ev.Type != EventKeyDown {
		t.Fatalf("scancode %d produced a %v, want a key-down", scancode, ev.Type)
	}
	return ev.Key
}

// ── the keys the toolkit claims to speak ────────────────────────────────

func alphabetCases() []keyCase {
	var out []keyCase
	for i := 0; i < 26; i++ {
		// HID: A=4 … Z=29, which is also SDL3's numbering.
		out = append(out, keyCase{"letter", int32(4 + i), Keycode(4 + i)})
	}
	return out
}

func digitCases() []keyCase {
	var out []keyCase
	// HID: 1=30 … 9=38, 0=39 — note the order, which is NOT numeric order.
	for i := 1; i <= 9; i++ {
		out = append(out, keyCase{"digit", int32(29 + i), Keycode(29 + i)})
	}
	out = append(out, keyCase{"digit 0", 39, Key0})
	return out
}

func functionCases() []keyCase {
	var out []keyCase
	for i := 1; i <= 12; i++ {
		out = append(out, keyCase{"function", int32(57 + i), Keycode(57 + i)})
	}
	return out
}

func keypadCases() []keyCase {
	var out []keyCase
	// The keypad is where a wrong character is most likely to be noticed: the numbers sit on the
	// numeric keypad as well, so a mapping that collapses the two makes a key type something else.
	for _, c := range []struct {
		name string
		sc   int32
	}{
		{"keypad 1", 89}, {"keypad 2", 90}, {"keypad 3", 91},
		{"keypad 4", 92}, {"keypad 5", 93}, {"keypad 6", 94},
		{"keypad 7", 95}, {"keypad 8", 96}, {"keypad 9", 97},
		{"keypad 0", 98}, {"keypad .", 99},
		// The OPERATORS, whose real SDL3 scancodes are 84-88 — which are ALSO their HID usages, so they
		// do not diverge the way the arrows do. The first version of this table used 100-104, written
		// from memory rather than generated from the headers, and every one of them was wrong.
		{"keypad /", 84}, {"keypad *", 85}, {"keypad -", 86},
		{"keypad +", 87}, {"keypad enter", 88},
		{"keypad =", 103}, {"keypad ,", 133},
	} {
		// The digits and the dot are the keys where SDL3 and HID agree; the operators are NOT in SDL's
		// scancode set, so their toolkit keycode comes from the USB HID table and is stated here rather
		// than assumed.
		want := Keycode(c.sc)
		switch c.name {
		case "keypad /":
			want = KeypadSlash
		case "keypad *":
			want = KeypadAsterisk
		case "keypad -":
			want = KeypadMinus
		case "keypad +":
			want = KeypadPlus
		case "keypad enter":
			want = KeypadEnter
		case "keypad =":
			want = KeypadEquals
		case "keypad ,":
			want = KeypadComma
		}
		out = append(out, keyCase{c.name, c.sc, want})
	}
	return out
}

func editingCases() []keyCase {
	return []keyCase{
		{"escape", 41, KeyEscape},
		{"enter", 40, KeyEnter},
		{"backspace", 42, KeyBackspace},
		{"tab", 43, KeyTab},
		{"up", 82, KeyUp},
		{"down", 81, KeyDown},
		{"left", 80, KeyLeft},
		{"right", 79, KeyRight},
	}
}

func navigationCases() []keyCase {
	// THE DIVERGING SET. SDL3 numbers these by its own order (73–78); USB HID numbers them in the
	// 199–211 block. They are not the same keys, and a bare pass-through delivers the wrong identity.
	return []keyCase{
		{"insert", 73, 0xD2},  // HID usage 210
		{"home", 74, 0xC7},    // 199
		{"page up", 75, 0xD3}, // 211
		{"delete", 76, 0xD4},  // 212
		{"end", 77, 0xD5},     // 213
		{"page down", 78, 0xD6},
		{"pause", 72, KeyPause},
	}
}

func allKeyCases() []keyCase {
	var out []keyCase
	out = append(out, alphabetCases()...)
	out = append(out, digitCases()...)
	out = append(out, functionCases()...)
	out = append(out, keypadCases()...)
	out = append(out, editingCases()...)
	out = append(out, punctuationCases()...)
	out = append(out, navigationCases()...)
	return out
}

// ── the tests ───────────────────────────────────────────────────────────

// TestTheKeysTheToolkitNamesAllArriveCorrectly covers every key with a NAME in the toolkit: the letters,
// digits, function keys, the editing cluster and the arrows. These are the ones an app binds shortcuts to.
func TestTheKeysTheToolkitNamesAllArriveCorrectly(t *testing.T) {
	for _, c := range allKeyCases() {
		if c.name == "" || c.scancode == 0 {
			continue
		}
		// The editing and arrow keys are mapped explicitly by mapSDL3Key; the rest pass through because
		// SDL3 and HID agree on them.
		want := c.want
		if got := press(t, c.scancode); got != want {
			t.Errorf("%s: scancode %d arrived as keycode %d, want %d", c.name, c.scancode, got, want)
		}
	}
}

// punctuationCases is the main row: the map that keeps SDL3 and HID in agreement, and the keys the
// toolkit now NAMES so a widget can bind them (Ctrl+/ to comment, Ctrl+[ to indent). They used to
// arrive as KeyUnknown, and the text field's old ASCII fallback turned several of them into digits.
func punctuationCases() []keyCase {
	return []keyCase{
		{"space", 44, KeySpace},
		{"minus", 45, KeyMinus},
		{"equals", 46, KeyEqual},
		{"left bracket", 47, KeyOpenBracket},
		{"right bracket", 48, KeyCloseBracket},
		{"backslash", 49, KeyBackslash},
		{"semicolon", 51, KeySemicolon},
		{"apostrophe", 52, KeyApostrophe},
		{"grave", 53, KeyGrave},
		{"comma", 54, KeyComma},
		{"period", 55, KeyPeriod},
		{"slash", 56, KeySlash},
	}
}

// TestTheWholeKeyboardMapsToDistinctToolkitKeycodes is the test that catches the fall-through.
//
// Two DIFFERENT physical keys must never arrive as the same toolkit keycode. The bare
// `return Keycode(key)` in mapSDL3Key means every diverging key arrives as a raw SDL number, and those
// numbers are drawn from the SAME range as the HID values of other keys — so a textbox's Delete (SDL 76)
// can arrive indistinguishable from something the toolkit genuinely means.
func TestTheWholeKeyboardMapsToDistinctToolkitKeycodes(t *testing.T) {
	seen := map[Keycode]string{}
	for _, c := range allKeyCases() {
		if c.scancode == 0 {
			continue
		}
		got := press(t, c.scancode)
		if prev, dup := seen[got]; dup {
			t.Errorf("%s (scancode %d) and %s both arrive as keycode %d: two different keys are "+
				"indistinguishable to the toolkit, which is how one key types another's character",
				c.name, c.scancode, prev, got)
		}
		seen[got] = c.name
	}
}

// TestNoUnmappedKeyArrivesAsAKnownKey is the other direction, and it is the one that names the bug: a key
// the toolkit does not know must not be handed to the toolkit as a key it DOES know.
//
// The fall-through `return Keycode(key)` cannot distinguish "a key we do not have a name for" from "a key
// whose HID usage happens to equal this number", so a diverging key is silently delivered as a valid,
// wrong key.
func TestNoUnmappedKeyArrivesAsAKnownKey(t *testing.T) {
	// Every keycode the toolkit names, gathered from the cases above plus the modifier keys.
	known := map[Keycode]string{}
	for _, c := range allKeyCases() {
		if c.scancode != 0 {
			known[c.want] = c.name
		}
	}
	known[KeyLeftShift] = "left shift"
	known[KeyRightShift] = "right shift"
	known[KeyLeftCtrl] = "left ctrl"
	known[KeyRightCtrl] = "right ctrl"
	known[KeyLeftAlt] = "left alt"
	known[KeyRightAlt] = "right alt"
	known[KeyLeftMeta] = "left meta"
	known[KeyRightMeta] = "right meta"

	// Keys SDL can report that the toolkit has NO name for. Every one of them must arrive as
	// KeyUnknown, because delivering a bare scancode as a keycode is how a widget ends up acting on a key
	// the user did not press.
	unknownKeys := []struct {
		name string
		sc   int32
	}{
		// GENERATED FROM THE INSTALLED HEADERS, not from memory. The first version of this table used
		// CapsLock=57, ScrollLock=70, NumLock=88, PrintScreen=72 and Pause=73 — of which NumLock and
		// Pause were wrong (they are 83 and 72), and the wrong ones collided with keypad-enter and insert.
		// A table of key numbers written from memory is exactly the bug this file exists to pin.
		{"caps lock", 57},
		{"scroll lock", 71},
		{"num lock", 83},
		{"print screen", 70},
		{"application", 101},
		{"keypad (", 182},
		{"keypad )", 183},
		{"keypad backspace", 187},
		{"keypad = (400)", 134},
	}
	for _, c := range unknownKeys {
		got := press(t, c.sc)
		if got != KeyUnknown {
			if name, isKnown := known[got]; isKnown {
				t.Errorf("%s (scancode %d) arrives as keycode %d, which the toolkit calls %q: a key "+
					"with no name is being delivered as a DIFFERENT named key", c.name, c.sc, got, name)
				continue
			}
			t.Errorf("%s (scancode %d) arrives as keycode %d; a key the toolkit cannot name must "+
				"arrive as KeyUnknown, not as a bare scancode", c.name, c.sc, got)
		}
	}
}

// TestTheKeypadDoesNotCollideWithTheNumberRow is the one aimed straight at "the wrong character".
//
// The keypad's 0–9 are DIFFERENT physical keys from the number row's 0–9, and on a real keyboard they are
// distinct keys that a text editor must be able to tell apart (a shortcut bound to KP_1 must not fire for
// 1). If the two ranges ever collapse, every keypad digit silently becomes a number-row digit.
func TestTheKeypadDoesNotCollideWithTheNumberRow(t *testing.T) {
	numberRow := map[Keycode]bool{}
	for i := 1; i <= 9; i++ {
		numberRow[press(t, int32(29+i))] = true
	}
	numberRow[press(t, 39)] = true // 0

	for _, sc := range []int32{89, 90, 91, 92, 93, 94, 95, 96, 97, 98} {
		got := press(t, sc)
		if numberRow[got] {
			t.Errorf("keypad key at scancode %d arrives as keycode %d, which is also a number-row key: "+
				"the two are indistinguishable", sc, got)
		}
	}
}

// TestTheKeypadModifiersAndNavigationArriveDistinct is the combination half: every key, with and without
// each modifier, must deliver the SAME key and the modifiers the event actually carried.
//
// A key that changes identity when shift is held is the specific defect that makes "sometimes the wrong
// value is displayed" — the user's shift is being read as part of the key.
func TestTheKeypadModifiersAndNavigationArriveDistinct(t *testing.T) {
	mods := []struct {
		name string
		mod  uint16
		want KeyMod
	}{
		{"none", 0, ModNone},
		{"shift", SDL3_KMOD_LSHIFT, ModShift},
		{"ctrl", SDL3_KMOD_LCTRL, ModCtrl},
		{"alt", SDL3_KMOD_LALT, ModAlt},
		{"meta", SDL3_KMOD_LGUI, ModMeta},
		{"shift+ctrl", SDL3_KMOD_LSHIFT | SDL3_KMOD_LCTRL, ModShift | ModCtrl},
		{"ctrl+alt", SDL3_KMOD_LCTRL | SDL3_KMOD_LALT, ModCtrl | ModAlt},
		{"shift+ctrl+alt", SDL3_KMOD_LSHIFT | SDL3_KMOD_LCTRL | SDL3_KMOD_LALT, ModShift | ModCtrl | ModAlt},
		{"all four", SDL3_KMOD_LSHIFT | SDL3_KMOD_LCTRL | SDL3_KMOD_LALT | SDL3_KMOD_LGUI,
			ModShift | ModCtrl | ModAlt | ModMeta},
	}
	// A spread of keys, so the assertion is about the KEY as well as the modifiers.
	keys := []struct {
		name string
		sc   int32
	}{
		{"A", 4}, {"Z", 29}, {"1", 30}, {"0", 39}, {"space", 44},
		{"up", 82}, {"down", 81}, {"left", 80}, {"right", 79},
		{"keypad 1", 89}, {"keypad 0", 98}, {"keypad enter", 104},
	}

	for _, k := range keys {
		bare := press(t, k.sc)
		for _, m := range mods {
			var h keyHarness
			ev := h.fire(t, k.sc, m.mod)
			if ev.Key != bare {
				t.Errorf("%s with %s arrives as keycode %d, but alone it is %d: a modifier must not "+
					"change WHICH key was pressed", k.name, m.name, ev.Key, bare)
			}
			if ev.Mod != m.want {
				t.Errorf("%s with %s reports modifiers %v, want %v", k.name, m.name, ev.Mod, m.want)
			}
		}
	}
}

// TestTheLockKeyStateIsCarried: CapsLock and NumLock are LOCKS, not modifiers — they are "held" while
// other keys are not — and the keypad's meaning depends on NumLock (KP_1 is '1' with it on and End with it
// off). A pump that drops them cannot tell those two apart, which is a wrong character on the keypad with
// nothing to show for it.
func TestTheLockKeyStateIsCarried(t *testing.T) {
	var h keyHarness
	for _, c := range []struct {
		name string
		mod  uint16
	}{
		{"caps lock", SDL3_KMOD_CAPS},
		{"num lock", SDL3_KMOD_NUM},
		{"scroll lock", SDL3_KMOD_SCROLL},
	} {
		ev := h.fire(t, 89, c.mod) // keypad 1
		if ev.Mod&ModCaps == 0 && c.mod == SDL3_KMOD_CAPS {
			t.Errorf("%s: the lock state was dropped (modifiers %v); the keypad's meaning depends on it",
				c.name, ev.Mod)
		}
		if ev.Mod&ModNum == 0 && c.mod == SDL3_KMOD_NUM {
			t.Errorf("%s: the num-lock state was dropped (modifiers %v); keypad 1 is '1' with it on and "+
				"End with it off, so without it the two are the same key", c.name, ev.Mod)
		}
	}
}

// TestBothShiftKeysAndBothCtrlKeysAreEquivalent is the asymmetry that catches a per-key bit test: the two
// shift keys set different bits in SDL and the same bit in the toolkit, so a pump that forwarded SDL's bits
// would report shift differently depending on which hand used it.
func TestBothShiftKeysAndBothCtrlKeysAreEquivalent(t *testing.T) {
	for _, c := range []struct {
		name  string
		a, b  uint16
		shift bool
	}{
		{"shift", SDL3_KMOD_LSHIFT, SDL3_KMOD_RSHIFT, true},
		{"ctrl", SDL3_KMOD_LCTRL, SDL3_KMOD_RCTRL, false},
	} {
		var h keyHarness
		left := h.fire(t, 4, c.a)
		right := h.fire(t, 4, c.b)
		if left.Mod != right.Mod {
			t.Errorf("%s: the left key reports %v and the right %v; a user cannot tell which hand "+
				"they used and neither should the app", c.name, left.Mod, right.Mod)
		}
		if c.shift && left.Mod&ModShift == 0 {
			t.Errorf("%s: the %s key did not report shift (modifiers %v)", c.name, c.name, left.Mod)
		}
		if !c.shift && left.Mod&ModCtrl == 0 {
			t.Errorf("%s: the %s key did not report ctrl (modifiers %v)", c.name, c.name, left.Mod)
		}
	}
}

// TestTheOffsetsAreTheOnesTheHeadersSay pins the three offsets the pump reads for a key event. An offset
// is silent when it is wrong: the field read is another field, the value is plausible, and the symptom is
// "sometimes the wrong key". This test fails the moment one moves, with the C probe's numbers as the
// oracle.
func TestTheOffsetsAreTheOnesTheHeadersSay(t *testing.T) {
	// From offsetof() against the installed SDL3 headers:
	//   SDL_KeyboardEvent.scancode = 24, .mod = 32; SDL_TextInputEvent.text = 24.
	if sdl3OffsetKeyScancode != 24 {
		t.Errorf("the key scancode is read from offset %d, want 24", sdl3OffsetKeyScancode)
	}
	if sdl3OffsetKeyMod != 32 {
		t.Errorf("the key modifiers are read from offset %d, want 32", sdl3OffsetKeyMod)
	}
	// SDL_KeyboardEvent.repeat is the 1-byte bool at 37, between state (36) and padding.
	if sdl3OffsetKeyRepeat != 37 {
		t.Errorf("the key repeat flag is read from offset %d, want 37", sdl3OffsetKeyRepeat)
	}
	// SDL_TextEditingEvent (IME composition): text@24 (the same slot as TextInput), start@32,
	// length@36. From offsetof() against the installed headers.
	if sdl3OffsetTextEditingStart != 32 {
		t.Errorf("the composition start is read from offset %d, want 32", sdl3OffsetTextEditingStart)
	}
	if sdl3OffsetTextEditingLength != 36 {
		t.Errorf("the composition length is read from offset %d, want 36", sdl3OffsetTextEditingLength)
	}
	if sdl3OffsetTextInputText != 24 {
		t.Errorf("the text-input string pointer is read from offset %d, want 24", sdl3OffsetTextInputText)
	}
	// And the pump must actually use them, which is why the offset is a constant and not a literal.
	var h keyHarness
	ev := h.fire(t, 4, SDL3_KMOD_LSHIFT)
	if ev.Key != KeyA || ev.Mod&ModShift == 0 {
		t.Errorf("a key event at the pinned offsets decoded as key %v mod %v, want 'a' with shift", ev.Key, ev.Mod)
	}
}

// TestAKeyUpCarriesTheSameKeyAndModifiers as the down: a key-up that reports a different key than its
// key-down leaves a widget holding a key it never pressed.
func TestAKeyUpCarriesTheSameKeyAndModifiers(t *testing.T) {
	const winID = 5252
	h := &sdlWin{id: winID, w: 800, h: 600}
	winMu.Lock()
	sdlWinReg[winID] = h
	winMu.Unlock()
	defer func() {
		winMu.Lock()
		delete(sdlWinReg, winID)
		winMu.Unlock()
	}()

	prevPoll, prevMod, prevWindow := SDL3_PollEvent, SDL3_GetModState, sdl3Window
	defer func() {
		SDL3_PollEvent, SDL3_GetModState, sdl3Window = prevPoll, prevMod, prevWindow
	}()
	sdl3Window = unsafe.Pointer(h)

	var got []Event
	polls := 0
	SDL3_PollEvent = func(event unsafe.Pointer) bool {
		if polls >= 2 {
			return false
		}
		polls++
		buf := unsafe.Slice((*byte)(event), 128)
		for i := range buf {
			buf[i] = 0
		}
		typ := uint32(SDL3_EVENT_KEY_DOWN)
		if polls == 2 {
			typ = SDL3_EVENT_KEY_UP
		}
		*(*uint32)(unsafe.Pointer(&buf[0])) = typ
		*(*uint32)(unsafe.Pointer(&buf[16])) = winID
		*(*int32)(unsafe.Pointer(&buf[sdl3OffsetKeyScancode])) = 4 // A
		*(*uint16)(unsafe.Pointer(&buf[sdl3OffsetKeyMod])) = SDL3_KMOD_LCTRL
		return true
	}
	SDL3_GetModState = func() uint16 { return SDL3_KMOD_LCTRL }

	pumpSDLEvents(func(_ uint32, ev Event) { got = append(got, ev) }, nil)
	if len(got) != 2 {
		t.Fatalf("the pump delivered %d events, want a down and an up", len(got))
	}
	if got[0].Type != EventKeyDown || got[1].Type != EventKeyUp {
		t.Fatalf("the events are %v and %v, want down then up", got[0].Type, got[1].Type)
	}
	if got[0].Key != got[1].Key {
		t.Errorf("the key-up reports %v but the key-down reported %v", got[1].Key, got[0].Key)
	}
	if got[0].Mod != got[1].Mod {
		t.Errorf("the key-up reports modifiers %v but the key-down reported %v", got[1].Mod, got[0].Mod)
	}
}

// TestThePumpReportsKeyRepeat: the OS tells SDL that a key-down is an AUTO-REPEAT, and the toolkit
// promises to distinguish it from a fresh press — platform.Event.Repeat is documented, the window's
// repeat dispatch and every widget's OnKeyRepeat depend on it. Nothing produced it: the pump built the
// event without reading SDL's repeat byte, so in a running app the flag was always false and the whole
// repeat path was reachable only from tests.
func TestThePumpReportsKeyRepeat(t *testing.T) {
	var held keyHarness
	held.repeat = true
	ev := held.fire(t, 4 /* A */, 0)
	if ev.Type != EventKeyDown {
		t.Fatalf("the held key produced a %v, want a key-down", ev.Type)
	}
	if !ev.Repeat {
		t.Error("the pump dropped SDL's repeat flag: a held key is reported as a fresh press")
	}

	var fresh keyHarness
	if ev := fresh.fire(t, 4, 0); ev.Repeat {
		t.Error("a fresh press is reported as a repeat")
	}
}

// TestThePumpReportsImeComposition: SDL's composition event is a first-class event now. It carries the
// IN-PROGRESS text and the IME's selection inside it, and it is what lets a widget show a preedit —
// before this the event was never decoded, so an IME user saw nothing until the commit.
func TestThePumpReportsImeComposition(t *testing.T) {
	const winID = 4242
	h := &sdlWin{id: winID, w: 800, h: 600}
	winMu.Lock()
	sdlWinReg[winID] = h
	winMu.Unlock()

	text := append([]byte("にほ"), 0)
	prevPoll, prevWindow := SDL3_PollEvent, sdl3Window
	polls := 0
	SDL3_PollEvent = func(event unsafe.Pointer) bool {
		if polls > 0 {
			return false
		}
		polls++
		buf := unsafe.Slice((*byte)(event), 64)
		for i := range buf {
			buf[i] = 0
		}
		*(*uint32)(unsafe.Pointer(&buf[0])) = SDL3_EVENT_TEXT_EDITING
		*(*uint32)(unsafe.Pointer(&buf[16])) = winID
		*(*uintptr)(unsafe.Pointer(&buf[sdl3OffsetTextInputText])) = uintptr(unsafe.Pointer(&text[0]))
		*(*int32)(unsafe.Pointer(&buf[sdl3OffsetTextEditingStart])) = 1
		*(*int32)(unsafe.Pointer(&buf[sdl3OffsetTextEditingLength])) = 1
		return true
	}
	sdl3Window = unsafe.Pointer(&sdlWin{id: winID})
	var delivered []Event
	pumpSDLEvents(func(_ uint32, ev Event) { delivered = append(delivered, ev) }, nil)
	SDL3_PollEvent, sdl3Window = prevPoll, prevWindow
	winMu.Lock()
	delete(sdlWinReg, winID)
	winMu.Unlock()

	if len(delivered) != 1 {
		t.Fatalf("the composition delivered %d events, want 1", len(delivered))
	}
	ev := delivered[0]
	if ev.Type != EventTextEditing {
		t.Fatalf("the event is %v, want EventTextEditing", ev.Type)
	}
	if ev.Text != "にほ" {
		t.Errorf("the composition text is %q, want %q", ev.Text, "にほ")
	}
	if ev.CompStart != 1 || ev.CompLength != 1 {
		t.Errorf("the IME selection is start=%d length=%d, want 1/1", ev.CompStart, ev.CompLength)
	}
}
