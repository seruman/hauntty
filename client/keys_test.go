package client

import (
	"testing"

	"gotest.tools/v3/assert"

	"code.selman.me/hauntty/libghostty"
)

func TestParseKeyNotation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		code  uint32
		mods  uint32
	}{
		{"single letter", "a", uint32('a'), 0},
		{"uppercase letter", "A", uint32('a'), 0},
		{"digit", "1", uint32('1'), 0},
		{"space", "space", uint32(' '), 0},
		{"enter", "enter", libghostty.KeyEnter, 0},
		{"return alias", "return", libghostty.KeyEnter, 0},
		{"escape", "escape", libghostty.KeyEscape, 0},
		{"esc alias", "esc", libghostty.KeyEscape, 0},
		{"tab", "tab", libghostty.KeyTab, 0},
		{"backspace", "backspace", libghostty.KeyBackspace, 0},
		{"up", "up", libghostty.KeyUp, 0},
		{"down", "down", libghostty.KeyDown, 0},
		{"left", "left", libghostty.KeyLeft, 0},
		{"right", "right", libghostty.KeyRight, 0},
		{"home", "home", libghostty.KeyHome, 0},
		{"end", "end", libghostty.KeyEnd, 0},
		{"pageup", "pageup", libghostty.KeyPageUp, 0},
		{"pgup alias", "pgup", libghostty.KeyPageUp, 0},
		{"pagedown", "pagedown", libghostty.KeyPageDown, 0},
		{"pgdn alias", "pgdn", libghostty.KeyPageDown, 0},
		{"insert", "insert", libghostty.KeyInsert, 0},
		{"delete", "delete", libghostty.KeyDelete, 0},
		{"del alias", "del", libghostty.KeyDelete, 0},
		{"f1", "f1", libghostty.KeyF1, 0},
		{"f12", "f12", libghostty.KeyF12, 0},
		{"ctrl+c", "ctrl+c", uint32('c'), libghostty.ModCtrl},
		{"control+c", "control+c", uint32('c'), libghostty.ModCtrl},
		{"shift+up", "shift+up", libghostty.KeyUp, libghostty.ModShift},
		{"alt+a", "alt+a", uint32('a'), libghostty.ModAlt},
		{"opt+a", "opt+a", uint32('a'), libghostty.ModAlt},
		{"option+a", "option+a", uint32('a'), libghostty.ModAlt},
		{"super+a", "super+a", uint32('a'), libghostty.ModSuper},
		{"cmd+a", "cmd+a", uint32('a'), libghostty.ModSuper},
		{"command+a", "command+a", uint32('a'), libghostty.ModSuper},
		{"ctrl+shift+up", "ctrl+shift+up", libghostty.KeyUp, libghostty.ModCtrl | libghostty.ModShift},
		{"case insensitive", "Ctrl+Enter", libghostty.KeyEnter, libghostty.ModCtrl},
		{"whitespace trimmed", "  ctrl+c  ", uint32('c'), libghostty.ModCtrl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ki, err := ParseKeyNotation(tt.input)
			assert.NilError(t, err)
			assert.Equal(t, ki.Code, tt.code)
			assert.Equal(t, ki.Mods, tt.mods)
		})
	}
}

func TestParseDetachKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		rawByte byte
		csiSeq  []byte
	}{
		{`ctrl+]`, `ctrl+]`, 0x1d, []byte("\x1b[93;5u")},
		{`ctrl+\`, `ctrl+\`, 0x1c, []byte("\x1b[92;5u")},
		{`ctrl+a`, `ctrl+a`, 0x01, []byte("\x1b[97;5u")},
		{`ctrl+c`, `ctrl+c`, 0x03, []byte("\x1b[99;5u")},
		{`ctrl+shift+z`, `ctrl+shift+z`, 0x1a, []byte("\x1b[122;6u")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dk, err := ParseDetachKey(tt.input)
			assert.NilError(t, err)
			assert.Equal(t, dk.rawByte, tt.rawByte)
			assert.DeepEqual(t, dk.csiSeq, tt.csiSeq)
		})
	}
}

func TestParseDetachKeyErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		err   string
	}{
		{"no ctrl", "a", "detach keybind must include ctrl modifier"},
		{"shift only", "shift+a", "detach keybind must include ctrl modifier"},
		{"ctrl+named key", "ctrl+enter", "detach keybind must be ctrl+<printable key>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDetachKey(tt.input)
			assert.Error(t, err, tt.err)
		})
	}
}

func TestParseKeyNotationErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		err   string
	}{
		{"unknown key", "bogus", `unknown key: "bogus"`},
		{"unknown modifier", "foo+a", `unknown modifier: "foo"`},
		{"control char", "\x01", `unknown key: "\x01"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseKeyNotation(tt.input)
			assert.Error(t, err, tt.err)
		})
	}
}
