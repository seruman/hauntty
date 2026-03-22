package client

import (
	"fmt"
	"strings"

	"code.selman.me/hauntty/libghostty"
)

type KeyInput struct {
	Code libghostty.KeyCode
	Mods libghostty.Modifier
}

func ParseKeyNotation(notation string) (KeyInput, error) {
	notation = strings.TrimSpace(strings.ToLower(notation))

	parts := strings.Split(notation, "+")

	var mods libghostty.Modifier
	keyPart := parts[len(parts)-1]
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "ctrl", "control":
			mods |= libghostty.ModCtrl
		case "shift":
			mods |= libghostty.ModShift
		case "alt", "opt", "option":
			mods |= libghostty.ModAlt
		case "super", "cmd", "command":
			mods |= libghostty.ModSuper
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

func parseKeyName(name string) (libghostty.KeyCode, error) {
	switch name {
	case "enter", "return":
		return libghostty.KeyEnter, nil
	case "escape", "esc":
		return libghostty.KeyEscape, nil
	case "tab":
		return libghostty.KeyTab, nil
	case "backspace":
		return libghostty.KeyBackspace, nil
	case "space":
		return libghostty.KeyCode(' '), nil
	case "up":
		return libghostty.KeyUp, nil
	case "down":
		return libghostty.KeyDown, nil
	case "left":
		return libghostty.KeyLeft, nil
	case "right":
		return libghostty.KeyRight, nil
	case "home":
		return libghostty.KeyHome, nil
	case "end":
		return libghostty.KeyEnd, nil
	case "pageup", "pgup":
		return libghostty.KeyPageUp, nil
	case "pagedown", "pgdn":
		return libghostty.KeyPageDown, nil
	case "insert":
		return libghostty.KeyInsert, nil
	case "delete", "del":
		return libghostty.KeyDelete, nil
	case "f1":
		return libghostty.KeyF1, nil
	case "f2":
		return libghostty.KeyF2, nil
	case "f3":
		return libghostty.KeyF3, nil
	case "f4":
		return libghostty.KeyF4, nil
	case "f5":
		return libghostty.KeyF5, nil
	case "f6":
		return libghostty.KeyF6, nil
	case "f7":
		return libghostty.KeyF7, nil
	case "f8":
		return libghostty.KeyF8, nil
	case "f9":
		return libghostty.KeyF9, nil
	case "f10":
		return libghostty.KeyF10, nil
	case "f11":
		return libghostty.KeyF11, nil
	case "f12":
		return libghostty.KeyF12, nil
	}

	if len(name) == 1 {
		ch := name[0]
		if ch >= 0x20 && ch <= 0x7e {
			return libghostty.KeyCode(ch), nil
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
	if ki.Mods&libghostty.ModCtrl == 0 {
		return DetachKey{}, fmt.Errorf("detach keybind must include ctrl modifier")
	}
	if ki.Code < 0x20 || ki.Code > 0x7e {
		return DetachKey{}, fmt.Errorf("detach keybind must be ctrl+<printable key>")
	}
	kittyMods := uint32(1)
	if ki.Mods&libghostty.ModShift != 0 {
		kittyMods += 1
	}
	if ki.Mods&libghostty.ModCtrl != 0 {
		kittyMods += 4
	}
	if ki.Mods&libghostty.ModAlt != 0 {
		kittyMods += 2
	}
	if ki.Mods&libghostty.ModSuper != 0 {
		kittyMods += 8
	}
	raw := byte(uint32(ki.Code) & 0x1f)
	if raw == 0x1b {
		// The raw ctrl byte collides with ESC (e.g. ctrl+; or ctrl+[).
		// Only match the CSI u sequence from the kitty keyboard protocol.
		raw = 0
	}
	return DetachKey{
		rawByte: raw,
		csiSeq:  fmt.Appendf(nil, "\x1b[%d;%du", ki.Code, kittyMods),
	}, nil
}
