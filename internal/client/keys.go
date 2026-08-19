package client

import (
	"fmt"
	"strings"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
)

type KeyCode = protocol.KeyCode

const (
	KeyEnter     KeyCode = 0x100
	KeyEscape    KeyCode = 0x101
	KeyTab       KeyCode = 0x102
	KeyBackspace KeyCode = 0x103
	KeyUp        KeyCode = 0x110
	KeyDown      KeyCode = 0x111
	KeyLeft      KeyCode = 0x112
	KeyRight     KeyCode = 0x113
	KeyHome      KeyCode = 0x120
	KeyEnd       KeyCode = 0x121
	KeyPageUp    KeyCode = 0x122
	KeyPageDown  KeyCode = 0x123
	KeyInsert    KeyCode = 0x124
	KeyDelete    KeyCode = 0x125
	KeyF1        KeyCode = 0x130
	KeyF2        KeyCode = 0x131
	KeyF3        KeyCode = 0x132
	KeyF4        KeyCode = 0x133
	KeyF5        KeyCode = 0x134
	KeyF6        KeyCode = 0x135
	KeyF7        KeyCode = 0x136
	KeyF8        KeyCode = 0x137
	KeyF9        KeyCode = 0x138
	KeyF10       KeyCode = 0x139
	KeyF11       KeyCode = 0x13a
	KeyF12       KeyCode = 0x13b
)

type Modifier = protocol.KeyMods

const (
	ModShift Modifier = 0x01
	ModCtrl  Modifier = 0x02
	ModAlt   Modifier = 0x04
	ModSuper Modifier = 0x08
)

type KeyInput struct {
	Code KeyCode
	Mods Modifier
}

func ParseKeyNotation(notation string) (KeyInput, error) {
	notation = strings.TrimSpace(strings.ToLower(notation))

	parts := strings.Split(notation, "+")

	var mods Modifier
	keyPart := parts[len(parts)-1]
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "ctrl", "control":
			mods |= ModCtrl
		case "shift":
			mods |= ModShift
		case "alt", "opt", "option":
			mods |= ModAlt
		case "super", "cmd", "command":
			mods |= ModSuper
		default:
			return KeyInput{}, fmt.Errorf("unknown modifier: %q", mod)
		}
	}

	code, err := parseKeyName(keyPart)
	if err != nil {
		return KeyInput{}, err
	}

	return KeyInput{Code: code, Mods: mods}, nil
}

func parseKeyName(name string) (KeyCode, error) {
	switch name {
	case "enter", "return":
		return KeyEnter, nil
	case "escape", "esc":
		return KeyEscape, nil
	case "tab":
		return KeyTab, nil
	case "backspace":
		return KeyBackspace, nil
	case "space":
		return KeyCode(' '), nil
	case "up":
		return KeyUp, nil
	case "down":
		return KeyDown, nil
	case "left":
		return KeyLeft, nil
	case "right":
		return KeyRight, nil
	case "home":
		return KeyHome, nil
	case "end":
		return KeyEnd, nil
	case "pageup", "pgup":
		return KeyPageUp, nil
	case "pagedown", "pgdn":
		return KeyPageDown, nil
	case "insert":
		return KeyInsert, nil
	case "delete", "del":
		return KeyDelete, nil
	case "f1":
		return KeyF1, nil
	case "f2":
		return KeyF2, nil
	case "f3":
		return KeyF3, nil
	case "f4":
		return KeyF4, nil
	case "f5":
		return KeyF5, nil
	case "f6":
		return KeyF6, nil
	case "f7":
		return KeyF7, nil
	case "f8":
		return KeyF8, nil
	case "f9":
		return KeyF9, nil
	case "f10":
		return KeyF10, nil
	case "f11":
		return KeyF11, nil
	case "f12":
		return KeyF12, nil
	}

	if len(name) == 1 {
		ch := name[0]
		if ch >= 0x20 && ch <= 0x7e {
			return KeyCode(ch), nil
		}
	}

	return 0, fmt.Errorf("unknown key: %q", name)
}

type DetachKey struct {
	rawByte byte
	hasRaw  bool
	csiSeq  []byte
}

func ParseDetachKey(notation string) (DetachKey, error) {
	ki, err := ParseKeyNotation(notation)
	if err != nil {
		return DetachKey{}, err
	}
	if ki.Mods&ModCtrl == 0 {
		return DetachKey{}, fmt.Errorf("detach keybind must include ctrl modifier")
	}
	if ki.Code < 0x20 || ki.Code > 0x7e {
		return DetachKey{}, fmt.Errorf("detach keybind must be ctrl+<printable key>")
	}
	term, err := libghostty.NewTerminal(libghostty.WithSize(1, 1))
	if err != nil {
		return DetachKey{}, fmt.Errorf("encode detach key: %w", err)
	}
	defer term.Close()
	encoder, err := libghostty.NewKeyEncoder()
	if err != nil {
		return DetachKey{}, fmt.Errorf("encode detach key: %w", err)
	}
	defer encoder.Close()
	event, err := libghostty.NewKeyEvent()
	if err != nil {
		return DetachKey{}, fmt.Errorf("encode detach key: %w", err)
	}
	defer event.Close()

	legacy, err := encodeDetachKey(encoder, event, term, ki.Code, ki.Mods&ModCtrl)
	if err != nil {
		return DetachKey{}, fmt.Errorf("encode legacy detach key: %w", err)
	}
	var raw byte
	hasRaw := len(legacy) == 1 && legacy[0] != 0x1b
	if hasRaw {
		raw = legacy[0]
	}

	term.VTWrite([]byte("\x1b[>1u"))
	kitty, err := encodeDetachKey(encoder, event, term, ki.Code, ki.Mods)
	if err != nil {
		return DetachKey{}, fmt.Errorf("encode kitty detach key: %w", err)
	}
	return DetachKey{rawByte: raw, hasRaw: hasRaw, csiSeq: kitty}, nil
}

func encodeDetachKey(encoder *libghostty.KeyEncoder, event *libghostty.KeyEvent, term *libghostty.Terminal, code KeyCode, mods Modifier) ([]byte, error) {
	key, ok := detachPhysicalKey(byte(code))
	if !ok {
		return nil, fmt.Errorf("unsupported key code %d", code)
	}
	event.SetAction(libghostty.KeyActionPress)
	event.SetKey(key)
	event.SetMods(libghostty.Mods(mods))
	consumed := libghostty.Mods(0)
	text := ""
	if mods&ModShift != 0 {
		if shifted, ok := shiftedKeyByte(byte(code)); ok {
			consumed = libghostty.ModShift
			text = string(shifted)
		} else if mods&ModCtrl == 0 {
			text = string(rune(code))
		}
	} else if mods&ModCtrl == 0 {
		text = string(rune(code))
	}
	event.SetConsumedMods(consumed)
	event.SetComposing(false)
	event.SetUnshiftedCodepoint(rune(code))
	event.SetUTF8(text)
	encoder.SetOptFromTerminal(term)
	return encoder.Encode(event)
}

func shiftedKeyByte(ch byte) (byte, bool) {
	if ch >= 'a' && ch <= 'z' {
		return ch - ('a' - 'A'), true
	}
	const unshifted = "`1234567890-=[]\\;',./"
	const shifted = "~!@#$%^&*()_+{}|:\"<>?"
	if index := strings.IndexByte(unshifted, ch); index >= 0 {
		return shifted[index], true
	}
	return 0, false
}

func detachPhysicalKey(ch byte) (libghostty.Key, bool) {
	if ch >= 'a' && ch <= 'z' {
		return libghostty.KeyA + libghostty.Key(ch-'a'), true
	}
	if ch >= '0' && ch <= '9' {
		return libghostty.KeyDigit0 + libghostty.Key(ch-'0'), true
	}
	switch ch {
	case ')':
		return libghostty.KeyDigit0, true
	case '!':
		return libghostty.KeyDigit1, true
	case '@':
		return libghostty.KeyDigit2, true
	case '#':
		return libghostty.KeyDigit3, true
	case '$':
		return libghostty.KeyDigit4, true
	case '%':
		return libghostty.KeyDigit5, true
	case '^':
		return libghostty.KeyDigit6, true
	case '&':
		return libghostty.KeyDigit7, true
	case '*':
		return libghostty.KeyDigit8, true
	case '(':
		return libghostty.KeyDigit9, true
	case '`', '~':
		return libghostty.KeyBackquote, true
	case '\\', '|':
		return libghostty.KeyBackslash, true
	case '[', '{':
		return libghostty.KeyBracketLeft, true
	case ']', '}':
		return libghostty.KeyBracketRight, true
	case ',', '<':
		return libghostty.KeyComma, true
	case '=', '+':
		return libghostty.KeyEqual, true
	case '-', '_':
		return libghostty.KeyMinus, true
	case '.', '>':
		return libghostty.KeyPeriod, true
	case '\'', '"':
		return libghostty.KeyQuote, true
	case ';', ':':
		return libghostty.KeySemicolon, true
	case '/', '?':
		return libghostty.KeySlash, true
	case ' ':
		return libghostty.KeySpace, true
	default:
		return 0, false
	}
}
