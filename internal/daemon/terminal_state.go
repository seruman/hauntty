package daemon

import (
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
	restored, err := rt.RestoreTerminal(state.Snapshot, scrollback)
	if err != nil {
		return nil, err
	}
	term := &terminalState{term: restored}

	cleanup := true
	defer func() {
		if cleanup {
			term.close()
		}
	}()

	ground, err := term.vtGround()
	if err != nil {
		return nil, err
	}
	if !ground {
		// Cancel any unfinished sequence before injecting restore controls.
		if err := term.feed([]byte("\x18")); err != nil {
			return nil, err
		}
	}
	dump, err := term.dumpScreen(libghostty.DumpPlain)
	if err != nil {
		return nil, err
	}
	if dump.IsAltScreen {
		if err := term.feed([]byte("\x1b[?1049l")); err != nil {
			return nil, err
		}
	}
	if err := term.feed([]byte("\x1b[!p")); err != nil {
		return nil, err
	}
	if err := term.resize(uint32(size.cols), uint32(size.rows)); err != nil {
		return nil, fmt.Errorf("resize restored terminal state: %w", err)
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

func (t *terminalState) snapshot() ([]byte, error) {
	return t.term.Snapshot()
}

func (t *terminalState) encodeClientKey(keyCode protocol.KeyCode, mods protocol.KeyMods) ([]byte, error) {
	return t.term.EncodeKey(libghostty.KeyCode(keyCode), libghostty.Modifier(mods))
}

func (t *terminalState) cwd() (string, bool, error) {
	return t.term.GetCwd()
}

func (t *terminalState) vtGround() (bool, error) {
	return t.term.VTGround()
}

func (t *terminalState) close() error {
	return t.term.Close()
}

func dumpDeadTerminalState(rt *libghostty.Runtime, state *sessionState, scrollback uint32, format protocol.DumpFormat) ([]byte, error) {
	restored, err := rt.RestoreTerminal(state.Snapshot, scrollback)
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: restore terminal: %w", err)
	}
	term := &terminalState{term: restored}
	defer term.close()

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
