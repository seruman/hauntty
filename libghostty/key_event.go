package libghostty

import "encoding/binary"

type KeyEvent struct {
	rt      *wasmRuntime
	ptr     uint32
	utf8Ptr uint32
	utf8Len uint32
}

type KeyAction int

const (
	KeyActionRelease KeyAction = 0
	KeyActionPress   KeyAction = 1
	KeyActionRepeat  KeyAction = 2
)

type Mods uint16

const (
	ModShift     Mods = 0x0001
	ModCtrl      Mods = 0x0002
	ModAlt       Mods = 0x0004
	ModSuper     Mods = 0x0008
	ModCapsLock  Mods = 0x0010
	ModNumLock   Mods = 0x0020
	ModShiftSide Mods = 0x0040
	ModCtrlSide  Mods = 0x0080
	ModAltSide   Mods = 0x0100
	ModSuperSide Mods = 0x0200
)

type Key int

const (
	KeyUnidentified       Key = 0
	KeyBackquote          Key = 1
	KeyBackslash          Key = 2
	KeyBracketLeft        Key = 3
	KeyBracketRight       Key = 4
	KeyComma              Key = 5
	KeyDigit0             Key = 6
	KeyDigit1             Key = 7
	KeyDigit2             Key = 8
	KeyDigit3             Key = 9
	KeyDigit4             Key = 10
	KeyDigit5             Key = 11
	KeyDigit6             Key = 12
	KeyDigit7             Key = 13
	KeyDigit8             Key = 14
	KeyDigit9             Key = 15
	KeyEqual              Key = 16
	KeyIntlBackslash      Key = 17
	KeyIntlRo             Key = 18
	KeyIntlYen            Key = 19
	KeyA                  Key = 20
	KeyB                  Key = 21
	KeyC                  Key = 22
	KeyD                  Key = 23
	KeyE                  Key = 24
	KeyF                  Key = 25
	KeyG                  Key = 26
	KeyH                  Key = 27
	KeyI                  Key = 28
	KeyJ                  Key = 29
	KeyK                  Key = 30
	KeyL                  Key = 31
	KeyM                  Key = 32
	KeyN                  Key = 33
	KeyO                  Key = 34
	KeyP                  Key = 35
	KeyQ                  Key = 36
	KeyR                  Key = 37
	KeyS                  Key = 38
	KeyT                  Key = 39
	KeyU                  Key = 40
	KeyV                  Key = 41
	KeyW                  Key = 42
	KeyX                  Key = 43
	KeyY                  Key = 44
	KeyZ                  Key = 45
	KeyMinus              Key = 46
	KeyPeriod             Key = 47
	KeyQuote              Key = 48
	KeySemicolon          Key = 49
	KeySlash              Key = 50
	KeyAltLeft            Key = 51
	KeyAltRight           Key = 52
	KeyBackspace          Key = 53
	KeyCapsLock           Key = 54
	KeyContextMenu        Key = 55
	KeyControlLeft        Key = 56
	KeyControlRight       Key = 57
	KeyEnter              Key = 58
	KeyMetaLeft           Key = 59
	KeyMetaRight          Key = 60
	KeyShiftLeft          Key = 61
	KeyShiftRight         Key = 62
	KeySpace              Key = 63
	KeyTab                Key = 64
	KeyConvert            Key = 65
	KeyKanaMode           Key = 66
	KeyNonConvert         Key = 67
	KeyDelete             Key = 68
	KeyEnd                Key = 69
	KeyHelp               Key = 70
	KeyHome               Key = 71
	KeyInsert             Key = 72
	KeyPageDown           Key = 73
	KeyPageUp             Key = 74
	KeyArrowDown          Key = 75
	KeyArrowLeft          Key = 76
	KeyArrowRight         Key = 77
	KeyArrowUp            Key = 78
	KeyNumLock            Key = 79
	KeyNumpad0            Key = 80
	KeyNumpad1            Key = 81
	KeyNumpad2            Key = 82
	KeyNumpad3            Key = 83
	KeyNumpad4            Key = 84
	KeyNumpad5            Key = 85
	KeyNumpad6            Key = 86
	KeyNumpad7            Key = 87
	KeyNumpad8            Key = 88
	KeyNumpad9            Key = 89
	KeyNumpadAdd          Key = 90
	KeyNumpadBackspace    Key = 91
	KeyNumpadClear        Key = 92
	KeyNumpadClearEntry   Key = 93
	KeyNumpadComma        Key = 94
	KeyNumpadDecimal      Key = 95
	KeyNumpadDivide       Key = 96
	KeyNumpadEnter        Key = 97
	KeyNumpadEqual        Key = 98
	KeyNumpadMemoryAdd    Key = 99
	KeyNumpadMemoryClear  Key = 100
	KeyNumpadMemoryRecall Key = 101
	KeyNumpadMemoryStore  Key = 102
	KeyNumpadMemorySub    Key = 103
	KeyNumpadMultiply     Key = 104
	KeyNumpadParenLeft    Key = 105
	KeyNumpadParenRight   Key = 106
	KeyNumpadSubtract     Key = 107
	KeyNumpadSeparator    Key = 108
	KeyNumpadUp           Key = 109
	KeyNumpadDown         Key = 110
	KeyNumpadRight        Key = 111
	KeyNumpadLeft         Key = 112
	KeyNumpadBegin        Key = 113
	KeyNumpadHome         Key = 114
	KeyNumpadEnd          Key = 115
	KeyNumpadInsert       Key = 116
	KeyNumpadDelete       Key = 117
	KeyNumpadPageUp       Key = 118
	KeyNumpadPageDown     Key = 119
	KeyEscape             Key = 120
	KeyF1                 Key = 121
	KeyF2                 Key = 122
	KeyF3                 Key = 123
	KeyF4                 Key = 124
	KeyF5                 Key = 125
	KeyF6                 Key = 126
	KeyF7                 Key = 127
	KeyF8                 Key = 128
	KeyF9                 Key = 129
	KeyF10                Key = 130
	KeyF11                Key = 131
	KeyF12                Key = 132
	KeyF13                Key = 133
	KeyF14                Key = 134
	KeyF15                Key = 135
	KeyF16                Key = 136
	KeyF17                Key = 137
	KeyF18                Key = 138
	KeyF19                Key = 139
	KeyF20                Key = 140
	KeyF21                Key = 141
	KeyF22                Key = 142
	KeyF23                Key = 143
	KeyF24                Key = 144
	KeyF25                Key = 145
	KeyFn                 Key = 146
	KeyFnLock             Key = 147
	KeyPrintScreen        Key = 148
	KeyScrollLock         Key = 149
	KeyPause              Key = 150
	KeyBrowserBack        Key = 151
	KeyBrowserFavorites   Key = 152
	KeyBrowserForward     Key = 153
	KeyBrowserHome        Key = 154
	KeyBrowserRefresh     Key = 155
	KeyBrowserSearch      Key = 156
	KeyBrowserStop        Key = 157
	KeyEject              Key = 158
	KeyLaunchApp1         Key = 159
	KeyLaunchApp2         Key = 160
	KeyLaunchMail         Key = 161
	KeyMediaPlayPause     Key = 162
	KeyMediaSelect        Key = 163
	KeyMediaStop          Key = 164
	KeyMediaTrackNext     Key = 165
	KeyMediaTrackPrevious Key = 166
	KeyPower              Key = 167
	KeySleep              Key = 168
	KeyAudioVolumeDown    Key = 169
	KeyAudioVolumeMute    Key = 170
	KeyAudioVolumeUp      Key = 171
	KeyWakeUp             Key = 172
	KeyCopy               Key = 173
	KeyCut                Key = 174
	KeyPaste              Key = 175
)

func NewKeyEvent() (*KeyEvent, error) {
	rt := sharedRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	ptr, err := rt.opaque(func(slot uint32) int32 {
		return rt.mod.Xghostty_key_event_new(0, int32(slot))
	})
	if err != nil {
		return nil, err
	}

	return &KeyEvent{rt: rt, ptr: ptr}, nil
}

func (e *KeyEvent) Close() {
	if e == nil || e.ptr == 0 {
		return
	}

	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_free(int32(e.ptr))
	e.rt.free(e.utf8Ptr, e.utf8Len)
	e.ptr = 0
	e.utf8Ptr = 0
	e.utf8Len = 0
}

func (e *KeyEvent) SetAction(action KeyAction) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_set_action(int32(e.ptr), int32(action))
}

func (e *KeyEvent) Action() KeyAction {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	return KeyAction(e.rt.mod.Xghostty_key_event_get_action(int32(e.ptr)))
}

func (e *KeyEvent) SetKey(key Key) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_set_key(int32(e.ptr), int32(key))
}

func (e *KeyEvent) Key() Key {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	return Key(e.rt.mod.Xghostty_key_event_get_key(int32(e.ptr)))
}

func (e *KeyEvent) SetMods(mods Mods) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_set_mods(int32(e.ptr), int32(mods))
}

func (e *KeyEvent) Mods() Mods {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	return Mods(e.rt.mod.Xghostty_key_event_get_mods(int32(e.ptr)))
}

func (e *KeyEvent) SetConsumedMods(mods Mods) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_set_consumed_mods(int32(e.ptr), int32(mods))
}

func (e *KeyEvent) ConsumedMods() Mods {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	return Mods(e.rt.mod.Xghostty_key_event_get_consumed_mods(int32(e.ptr)))
}

func (e *KeyEvent) SetComposing(composing bool) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_set_composing(int32(e.ptr), int32(boolByte(composing)))
}

func (e *KeyEvent) Composing() bool {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	return e.rt.mod.Xghostty_key_event_get_composing(int32(e.ptr)) != 0
}

func (e *KeyEvent) SetUTF8(value string) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.free(e.utf8Ptr, e.utf8Len)
	e.utf8Ptr = 0
	e.utf8Len = 0

	if value != "" {
		length, err := wasmLength(len(value))
		if err != nil {
			panic(err)
		}

		ptr, err := e.rt.alloc(length)
		if err != nil {
			panic(err)
		}

		if err := e.rt.put(ptr, []byte(value)); err != nil {
			panic(err)
		}

		e.utf8Ptr = ptr
		e.utf8Len = length
	}

	e.rt.mod.Xghostty_key_event_set_utf8(int32(e.ptr), int32(e.utf8Ptr), int32(e.utf8Len))
}

func (e *KeyEvent) UTF8() string {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	lengthPtr, err := e.rt.alloc(4)
	if err != nil {
		panic(err)
	}
	defer e.rt.free(lengthPtr, 4)

	ptr := uint32(e.rt.mod.Xghostty_key_event_get_utf8(int32(e.ptr), int32(lengthPtr)))
	lengthData, err := e.rt.bytes(lengthPtr, 4)
	if err != nil {
		panic(err)
	}

	length := binary.LittleEndian.Uint32(lengthData)
	data, err := e.rt.bytes(ptr, length)
	if err != nil {
		panic(err)
	}

	return string(data)
}

func (e *KeyEvent) SetUnshiftedCodepoint(codepoint rune) {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	e.rt.mod.Xghostty_key_event_set_unshifted_codepoint(int32(e.ptr), int32(codepoint))
}

func (e *KeyEvent) UnshiftedCodepoint() rune {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()

	return rune(e.rt.mod.Xghostty_key_event_get_unshifted_codepoint(int32(e.ptr)))
}
