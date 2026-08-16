package client

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseKeyNotation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		code  KeyCode
		mods  Modifier
	}{
		{"single letter", "a", KeyCode('a'), 0},
		{"uppercase letter", "A", KeyCode('a'), 0},
		{"digit", "1", KeyCode('1'), 0},
		{"space", "space", KeyCode(' '), 0},
		{"enter", "enter", KeyEnter, 0},
		{"return alias", "return", KeyEnter, 0},
		{"escape", "escape", KeyEscape, 0},
		{"esc alias", "esc", KeyEscape, 0},
		{"tab", "tab", KeyTab, 0},
		{"backspace", "backspace", KeyBackspace, 0},
		{"up", "up", KeyUp, 0},
		{"down", "down", KeyDown, 0},
		{"left", "left", KeyLeft, 0},
		{"right", "right", KeyRight, 0},
		{"home", "home", KeyHome, 0},
		{"end", "end", KeyEnd, 0},
		{"pageup", "pageup", KeyPageUp, 0},
		{"pgup alias", "pgup", KeyPageUp, 0},
		{"pagedown", "pagedown", KeyPageDown, 0},
		{"pgdn alias", "pgdn", KeyPageDown, 0},
		{"insert", "insert", KeyInsert, 0},
		{"delete", "delete", KeyDelete, 0},
		{"del alias", "del", KeyDelete, 0},
		{"f1", "f1", KeyF1, 0},
		{"f12", "f12", KeyF12, 0},
		{"ctrl+c", "ctrl+c", KeyCode('c'), ModCtrl},
		{"control+c", "control+c", KeyCode('c'), ModCtrl},
		{"shift+up", "shift+up", KeyUp, ModShift},
		{"alt+a", "alt+a", KeyCode('a'), ModAlt},
		{"opt+a", "opt+a", KeyCode('a'), ModAlt},
		{"option+a", "option+a", KeyCode('a'), ModAlt},
		{"super+a", "super+a", KeyCode('a'), ModSuper},
		{"cmd+a", "cmd+a", KeyCode('a'), ModSuper},
		{"command+a", "command+a", KeyCode('a'), ModSuper},
		{"ctrl+shift+up", "ctrl+shift+up", KeyUp, ModCtrl | ModShift},
		{"case insensitive", "Ctrl+Enter", KeyEnter, ModCtrl},
		{"whitespace trimmed", "  ctrl+c  ", KeyCode('c'), ModCtrl},
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
		hasRaw  bool
		csiSeq  []byte
	}{
		{`ctrl+]`, `ctrl+]`, 0x1d, true, []byte("\x1b[93;5u")},
		{`ctrl+\`, `ctrl+\`, 0x1c, true, []byte("\x1b[92;5u")},
		{`ctrl+a`, `ctrl+a`, 0x01, true, []byte("\x1b[97;5u")},
		{`ctrl+c`, `ctrl+c`, 0x03, true, []byte("\x1b[99;5u")},
		{`ctrl+shift+z`, `ctrl+shift+z`, 0x1a, true, []byte("\x1b[122;6u")},
		{`ctrl+space`, `ctrl+space`, 0x00, true, []byte("\x1b[32;5u")},
		{`ctrl+[`, `ctrl+[`, 0x00, false, []byte("\x1b[91;5u")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dk, err := ParseDetachKey(tt.input)
			assert.NilError(t, err)
			assert.Equal(t, dk.rawByte, tt.rawByte)
			assert.Equal(t, dk.hasRaw, tt.hasRaw)
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
