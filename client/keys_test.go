package client

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/selman/hauntty/wasm"
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
		{"enter", "enter", wasm.KeyEnter, 0},
		{"return alias", "return", wasm.KeyEnter, 0},
		{"escape", "escape", wasm.KeyEscape, 0},
		{"esc alias", "esc", wasm.KeyEscape, 0},
		{"tab", "tab", wasm.KeyTab, 0},
		{"backspace", "backspace", wasm.KeyBackspace, 0},
		{"up", "up", wasm.KeyUp, 0},
		{"down", "down", wasm.KeyDown, 0},
		{"left", "left", wasm.KeyLeft, 0},
		{"right", "right", wasm.KeyRight, 0},
		{"home", "home", wasm.KeyHome, 0},
		{"end", "end", wasm.KeyEnd, 0},
		{"pageup", "pageup", wasm.KeyPageUp, 0},
		{"pgup alias", "pgup", wasm.KeyPageUp, 0},
		{"pagedown", "pagedown", wasm.KeyPageDown, 0},
		{"pgdn alias", "pgdn", wasm.KeyPageDown, 0},
		{"insert", "insert", wasm.KeyInsert, 0},
		{"delete", "delete", wasm.KeyDelete, 0},
		{"del alias", "del", wasm.KeyDelete, 0},
		{"f1", "f1", wasm.KeyF1, 0},
		{"f12", "f12", wasm.KeyF12, 0},
		{"ctrl+c", "ctrl+c", uint32('c'), wasm.ModCtrl},
		{"control+c", "control+c", uint32('c'), wasm.ModCtrl},
		{"shift+up", "shift+up", wasm.KeyUp, wasm.ModShift},
		{"alt+a", "alt+a", uint32('a'), wasm.ModAlt},
		{"opt+a", "opt+a", uint32('a'), wasm.ModAlt},
		{"option+a", "option+a", uint32('a'), wasm.ModAlt},
		{"super+a", "super+a", uint32('a'), wasm.ModSuper},
		{"cmd+a", "cmd+a", uint32('a'), wasm.ModSuper},
		{"command+a", "command+a", uint32('a'), wasm.ModSuper},
		{"ctrl+shift+up", "ctrl+shift+up", wasm.KeyUp, wasm.ModCtrl | wasm.ModShift},
		{"case insensitive", "Ctrl+Enter", wasm.KeyEnter, wasm.ModCtrl},
		{"whitespace trimmed", "  ctrl+c  ", uint32('c'), wasm.ModCtrl},
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
