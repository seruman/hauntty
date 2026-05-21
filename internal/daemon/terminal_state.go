package daemon

import (
	"bytes"
	"fmt"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
)

type terminalState struct {
	term *libghostty.Terminal
}

func newTerminalState(rt *libghostty.Runtime, cols, rows, scrollback uint32) (*terminalState, error) {
	term, err := rt.NewTerminal(cols, rows, scrollback)
	if err != nil {
		return nil, err
	}
	return &terminalState{term: term}, nil
}

func restoreTerminalState(rt *libghostty.Runtime, state *sessionState, size termSize, scrollback uint32) (*terminalState, error) {
	term, err := newTerminalState(rt, uint32(state.Cols), uint32(state.Rows), scrollback)
	if err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			term.close()
		}
	}()

	if len(state.VT) > 0 {
		if err := term.feed(state.VT); err != nil {
			return nil, err
		}
	}
	if state.IsAltScreen {
		if err := term.feed([]byte("\x1b[?1049l")); err != nil {
			return nil, err
		}
	}
	if err := term.feed([]byte("\x1b[!p")); err != nil {
		return nil, err
	}
	if state.Cols != size.cols || state.Rows != size.rows {
		if err := term.resize(uint32(size.cols), uint32(size.rows)); err != nil {
			return nil, fmt.Errorf("resize restored terminal state: %w", err)
		}
	}

	cleanup = false
	return term, nil
}

func (t *terminalState) feed(data []byte) error {
	return t.term.Feed(data)
}

func (t *terminalState) resize(cols, rows uint32) error {
	return t.term.Resize(cols, rows)
}

func (t *terminalState) dumpScreen(format libghostty.DumpFormat) (*libghostty.ScreenDump, error) {
	return t.term.DumpScreen(format)
}

func (t *terminalState) encodeClientKey(keyCode protocol.KeyCode, mods protocol.KeyMods) ([]byte, error) {
	return t.term.EncodeKey(libghostty.KeyCode(keyCode), libghostty.Modifier(mods))
}

func (t *terminalState) cwd() (string, bool, error) {
	return t.term.GetCwd()
}

func (t *terminalState) close() error {
	return t.term.Close()
}

func dumpDeadTerminalState(rt *libghostty.Runtime, state *sessionState, scrollback uint32, format protocol.DumpFormat) ([]byte, error) {
	scrollback = max(scrollback, uint32(bytes.Count(state.VT, []byte{'\n'}))+uint32(state.Rows)+1)

	term, err := newTerminalState(rt, uint32(state.Cols), uint32(state.Rows), scrollback)
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: new terminal: %w", err)
	}
	defer term.close()

	if len(state.VT) > 0 {
		if err := term.feed(state.VT); err != nil {
			return nil, fmt.Errorf("dump dead terminal state: feed vt: %w", err)
		}
	}

	dump, err := term.dumpScreen(terminalDumpFormat(format))
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: dump screen: %w", err)
	}
	return dump.Data, nil
}

func terminalDumpFormat(format protocol.DumpFormat) libghostty.DumpFormat {
	flags := libghostty.DumpFormat(format) & ^libghostty.DumpFormatMask
	var wasmFmt libghostty.DumpFormat
	switch format & protocol.DumpFormatMask {
	case protocol.DumpVT:
		wasmFmt = libghostty.DumpVTSafe
	case protocol.DumpHTML:
		wasmFmt = libghostty.DumpHTML
	default:
		wasmFmt = libghostty.DumpPlain
	}
	return wasmFmt | flags
}
