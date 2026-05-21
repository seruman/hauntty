package client

import (
	"fmt"
	"strings"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
)

type KeyCode = protocol.KeyCode

const (
	KeyEnter     KeyCode = KeyCode(libghostty.KeyEnter)
	KeyEscape    KeyCode = KeyCode(libghostty.KeyEscape)
	KeyTab       KeyCode = KeyCode(libghostty.KeyTab)
	KeyBackspace KeyCode = KeyCode(libghostty.KeyBackspace)
	KeyUp        KeyCode = KeyCode(libghostty.KeyUp)
	KeyDown      KeyCode = KeyCode(libghostty.KeyDown)
	KeyLeft      KeyCode = KeyCode(libghostty.KeyLeft)
	KeyRight     KeyCode = KeyCode(libghostty.KeyRight)
	KeyHome      KeyCode = KeyCode(libghostty.KeyHome)
	KeyEnd       KeyCode = KeyCode(libghostty.KeyEnd)
	KeyPageUp    KeyCode = KeyCode(libghostty.KeyPageUp)
	KeyPageDown  KeyCode = KeyCode(libghostty.KeyPageDown)
	KeyInsert    KeyCode = KeyCode(libghostty.KeyInsert)
	KeyDelete    KeyCode = KeyCode(libghostty.KeyDelete)
	KeyF1        KeyCode = KeyCode(libghostty.KeyF1)
	KeyF2        KeyCode = KeyCode(libghostty.KeyF2)
	KeyF3        KeyCode = KeyCode(libghostty.KeyF3)
	KeyF4        KeyCode = KeyCode(libghostty.KeyF4)
	KeyF5        KeyCode = KeyCode(libghostty.KeyF5)
	KeyF6        KeyCode = KeyCode(libghostty.KeyF6)
	KeyF7        KeyCode = KeyCode(libghostty.KeyF7)
	KeyF8        KeyCode = KeyCode(libghostty.KeyF8)
	KeyF9        KeyCode = KeyCode(libghostty.KeyF9)
	KeyF10       KeyCode = KeyCode(libghostty.KeyF10)
	KeyF11       KeyCode = KeyCode(libghostty.KeyF11)
	KeyF12       KeyCode = KeyCode(libghostty.KeyF12)
)

type Modifier = protocol.KeyMods

const (
	ModShift Modifier = Modifier(libghostty.ModShift)
	ModCtrl  Modifier = Modifier(libghostty.ModCtrl)
	ModAlt   Modifier = Modifier(libghostty.ModAlt)
	ModSuper Modifier = Modifier(libghostty.ModSuper)
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
	kittyMods := uint32(1)
	if ki.Mods&ModShift != 0 {
		kittyMods += 1
	}
	if ki.Mods&ModCtrl != 0 {
		kittyMods += 4
	}
	if ki.Mods&ModAlt != 0 {
		kittyMods += 2
	}
	if ki.Mods&ModSuper != 0 {
		kittyMods += 8
	}
	raw := byte(uint32(ki.Code) & 0x1f)
	if raw == 0x1b {
		raw = 0
	}
	return DetachKey{
		rawByte: raw,
		csiSeq:  fmt.Appendf(nil, "\x1b[%d;%du", ki.Code, kittyMods),
	}, nil
}
