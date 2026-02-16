package client

import (
	"fmt"
	"strings"

	"github.com/selman/hauntty/wasm"
)

type KeyInput struct {
	Code uint32
	Mods uint32
}

func ParseKeyNotation(notation string) (KeyInput, error) {
	notation = strings.TrimSpace(strings.ToLower(notation))

	parts := strings.Split(notation, "+")

	var mods uint32
	keyPart := parts[len(parts)-1]
	for _, mod := range parts[:len(parts)-1] {
		switch mod {
		case "ctrl", "control":
			mods |= wasm.ModCtrl
		case "shift":
			mods |= wasm.ModShift
		case "alt", "opt", "option":
			mods |= wasm.ModAlt
		case "super", "cmd", "command":
			mods |= wasm.ModSuper
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

func parseKeyName(name string) (uint32, error) {
	switch name {
	case "enter", "return":
		return wasm.KeyEnter, nil
	case "escape", "esc":
		return wasm.KeyEscape, nil
	case "tab":
		return wasm.KeyTab, nil
	case "backspace":
		return wasm.KeyBackspace, nil
	case "space":
		return uint32(' '), nil
	case "up":
		return wasm.KeyUp, nil
	case "down":
		return wasm.KeyDown, nil
	case "left":
		return wasm.KeyLeft, nil
	case "right":
		return wasm.KeyRight, nil
	case "home":
		return wasm.KeyHome, nil
	case "end":
		return wasm.KeyEnd, nil
	case "pageup", "pgup":
		return wasm.KeyPageUp, nil
	case "pagedown", "pgdn":
		return wasm.KeyPageDown, nil
	case "insert":
		return wasm.KeyInsert, nil
	case "delete", "del":
		return wasm.KeyDelete, nil
	case "f1":
		return wasm.KeyF1, nil
	case "f2":
		return wasm.KeyF2, nil
	case "f3":
		return wasm.KeyF3, nil
	case "f4":
		return wasm.KeyF4, nil
	case "f5":
		return wasm.KeyF5, nil
	case "f6":
		return wasm.KeyF6, nil
	case "f7":
		return wasm.KeyF7, nil
	case "f8":
		return wasm.KeyF8, nil
	case "f9":
		return wasm.KeyF9, nil
	case "f10":
		return wasm.KeyF10, nil
	case "f11":
		return wasm.KeyF11, nil
	case "f12":
		return wasm.KeyF12, nil
	}

	if len(name) == 1 {
		ch := name[0]
		if ch >= 0x20 && ch <= 0x7e {
			return uint32(ch), nil
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
	if ki.Mods&wasm.ModCtrl == 0 {
		return DetachKey{}, fmt.Errorf("detach keybind must include ctrl modifier")
	}
	if ki.Code < 0x20 || ki.Code > 0x7e {
		return DetachKey{}, fmt.Errorf("detach keybind must be ctrl+<printable key>")
	}
	kittyMods := uint32(1)
	if ki.Mods&wasm.ModShift != 0 {
		kittyMods += 1
	}
	if ki.Mods&wasm.ModCtrl != 0 {
		kittyMods += 4
	}
	if ki.Mods&wasm.ModAlt != 0 {
		kittyMods += 2
	}
	if ki.Mods&wasm.ModSuper != 0 {
		kittyMods += 8
	}
	raw := byte(ki.Code & 0x1f)
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
