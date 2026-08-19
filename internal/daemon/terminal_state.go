package daemon

import (
	"fmt"
	"net/url"
	"strings"
	"sync"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
)

const continuationMaxBytes = 65 << 20

type terminalState struct {
	mu         sync.Mutex
	term       *libghostty.Terminal
	keyEncoder *libghostty.KeyEncoder
	keyEvent   *libghostty.KeyEvent
}

type screenDump struct {
	Data        []byte
	CursorRow   uint32
	CursorCol   uint32
	IsAltScreen bool
}

type terminalFormat struct {
	emit       libghostty.FormatterFormat
	unwrap     bool
	scrollback bool
	full       bool
	safe       bool
}

var terminalFormatVTFull = terminalFormat{
	emit:       libghostty.FormatterFormatVT,
	scrollback: true,
	full:       true,
}

func newTerminalState(cols, rows, scrollback uint32) (*terminalState, error) {
	term, err := libghostty.NewTerminal(
		libghostty.WithSize(uint16(cols), uint16(rows)),
		libghostty.WithMaxScrollbackLines(uint(scrollback)),
		libghostty.WithContinuationMaxBytes(continuationMaxBytes),
	)
	if err != nil {
		return nil, err
	}
	return wrapTerminalState(term)
}

func wrapTerminalState(term *libghostty.Terminal) (*terminalState, error) {
	encoder, err := libghostty.NewKeyEncoder()
	if err != nil {
		term.Close()
		return nil, err
	}
	event, err := libghostty.NewKeyEvent()
	if err != nil {
		encoder.Close()
		term.Close()
		return nil, err
	}
	return &terminalState{term: term, keyEncoder: encoder, keyEvent: event}, nil
}

func restoreTerminalState(state *sessionState, size termSize, scrollback uint32) (*terminalState, error) {
	decoder, err := libghostty.NewSnapshotDecoderBytes(state.Snapshot)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	if err := decoder.SetMaxContinuationBytes(continuationMaxBytes); err != nil {
		return nil, err
	}
	if err := decoder.SetRetainContinuation(true); err != nil {
		return nil, err
	}
	restored, err := decoder.Decode()
	if err != nil {
		return nil, err
	}
	scrollbackLimit := uint(scrollback)
	if err := restored.SetScrollbackMaxLines(&scrollbackLimit); err != nil {
		restored.Close()
		return nil, err
	}
	term, err := wrapTerminalState(restored)
	if err != nil {
		return nil, err
	}

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
		term.feed([]byte("\x18"))
	}
	dump, err := term.dumpScreen(terminalFormat{emit: libghostty.FormatterFormatPlain})
	if err != nil {
		return nil, err
	}
	if dump.IsAltScreen {
		term.feed([]byte("\x1b[?1049l"))
	}
	term.feed([]byte("\x1b[!p"))
	if err := term.resize(uint32(size.cols), uint32(size.rows)); err != nil {
		return nil, fmt.Errorf("resize restored terminal state: %w", err)
	}

	cleanup = false
	return term, nil
}

func (t *terminalState) feed(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.term.VTWrite(data)
}

func (t *terminalState) resize(cols, rows uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.term.Resize(uint16(cols), uint16(rows), 0, 0)
}

func (t *terminalState) dumpScreen(format terminalFormat) (*screenDump, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	options := []libghostty.FormatterOption{
		libghostty.WithFormatterFormat(format.emit),
		libghostty.WithFormatterUnwrap(format.unwrap),
		libghostty.WithFormatterTrim(true),
	}
	if !format.full && !format.scrollback {
		cols, err := t.term.Cols()
		if err != nil {
			return nil, err
		}
		rows, err := t.term.Rows()
		if err != nil {
			return nil, err
		}
		start, err := t.term.GridRef(libghostty.Point{Tag: libghostty.PointTagActive})
		if err != nil {
			return nil, err
		}
		end, err := t.term.GridRef(libghostty.Point{Tag: libghostty.PointTagActive, X: cols - 1, Y: uint32(rows - 1)})
		if err != nil {
			return nil, err
		}
		options = append(options, libghostty.WithFormatterSelection(&libghostty.Selection{Start: *start, End: *end}))
	}
	if format.emit == libghostty.FormatterFormatHTML {
		options = append(options, libghostty.WithFormatterExtraPalette(true))
	}
	if format.full {
		options = append(
			options,
			libghostty.WithFormatterExtraModes(true),
			libghostty.WithFormatterExtraScrollingRegion(true),
			libghostty.WithFormatterExtraPwd(true),
			libghostty.WithFormatterExtraKeyboard(true),
			libghostty.WithFormatterExtraCursor(true),
			libghostty.WithFormatterExtraStyle(true),
			libghostty.WithFormatterExtraHyperlink(true),
			libghostty.WithFormatterExtraProtection(true),
			libghostty.WithFormatterExtraKittyKeyboard(true),
			libghostty.WithFormatterExtraCharsets(true),
		)
	}
	formatter, err := libghostty.NewFormatter(t.term, options...)
	if err != nil {
		return nil, err
	}
	defer formatter.Close()
	data, err := formatter.Format()
	if err != nil {
		return nil, err
	}
	if format.safe {
		data = append(data, "\x1b[0m"...)
	}
	cursorCol, err := t.term.CursorX()
	if err != nil {
		return nil, err
	}
	cursorRow, err := t.term.CursorY()
	if err != nil {
		return nil, err
	}
	activeScreen, err := t.term.ActiveScreen()
	if err != nil {
		return nil, err
	}
	return &screenDump{
		Data:        data,
		CursorRow:   uint32(cursorRow),
		CursorCol:   uint32(cursorCol),
		IsAltScreen: activeScreen == libghostty.ScreenAlternate,
	}, nil
}

func (t *terminalState) snapshot() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.term.Snapshot()
}

func (t *terminalState) encodeClientKey(keyCode protocol.KeyCode, mods protocol.KeyMods) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key, codepoint, text, ok := terminalKeyEvent(keyCode, mods)
	if !ok {
		return nil, fmt.Errorf("unsupported key code %d", keyCode)
	}
	t.keyEvent.SetAction(libghostty.KeyActionPress)
	t.keyEvent.SetKey(key)
	t.keyEvent.SetMods(libghostty.Mods(mods))
	consumed := libghostty.Mods(0)
	if mods&protocol.KeyMods(libghostty.ModShift) != 0 {
		if _, ok := shiftedASCII(byte(keyCode)); ok {
			consumed = libghostty.ModShift
		}
	}
	t.keyEvent.SetConsumedMods(consumed)
	t.keyEvent.SetComposing(false)
	t.keyEvent.SetUnshiftedCodepoint(rune(codepoint))
	t.keyEvent.SetUTF8(string(text))
	t.keyEncoder.SetOptFromTerminal(t.term)
	return t.keyEncoder.Encode(t.keyEvent)
}

func (t *terminalState) cwd() (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, err := t.term.Pwd()
	if err != nil || raw == "" {
		return "", false, err
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" && u.Path != "" {
		raw = u.Path
	}
	return raw, true, nil
}

func (t *terminalState) vtGround() (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.term.VTGround()
}

func (t *terminalState) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keyEvent.Close()
	t.keyEncoder.Close()
	t.term.Close()
}

func dumpDeadTerminalState(state *sessionState, scrollback uint32, format protocol.DumpFormat) ([]byte, error) {
	decoder, err := libghostty.NewSnapshotDecoderBytes(state.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: restore terminal: %w", err)
	}
	defer decoder.Close()
	if err := decoder.SetMaxContinuationBytes(continuationMaxBytes); err != nil {
		return nil, fmt.Errorf("dump dead terminal state: restore terminal: %w", err)
	}
	restored, err := decoder.Decode()
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: restore terminal: %w", err)
	}
	scrollbackLimit := uint(scrollback)
	if err := restored.SetScrollbackMaxLines(&scrollbackLimit); err != nil {
		restored.Close()
		return nil, fmt.Errorf("dump dead terminal state: restore terminal: %w", err)
	}
	term, err := wrapTerminalState(restored)
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: restore terminal: %w", err)
	}
	defer term.close()

	dump, err := term.dumpScreen(terminalDumpFormat(format))
	if err != nil {
		return nil, fmt.Errorf("dump dead terminal state: dump screen: %w", err)
	}
	return dump.Data, nil
}

func terminalDumpFormat(format protocol.DumpFormat) terminalFormat {
	result := terminalFormat{
		unwrap:     format&protocol.DumpFlagUnwrap != 0,
		scrollback: format&protocol.DumpFlagScrollback != 0,
	}
	switch format & protocol.DumpFormatMask {
	case protocol.DumpVT:
		result.emit = libghostty.FormatterFormatVT
		result.safe = true
	case protocol.DumpHTML:
		result.emit = libghostty.FormatterFormatHTML
	default:
		result.emit = libghostty.FormatterFormatPlain
	}
	return result
}

func terminalKeyEvent(keyCode protocol.KeyCode, mods protocol.KeyMods) (libghostty.Key, uint32, []byte, bool) {
	codepoint := uint32(keyCode)
	if keyCode >= 0x20 && keyCode <= 0x7e {
		key, ok := asciiPhysicalKey(byte(keyCode))
		if !ok {
			return 0, 0, nil, false
		}
		ch := byte(keyCode)
		if mods&protocol.KeyMods(libghostty.ModShift) != 0 {
			if shifted, ok := shiftedASCII(ch); ok {
				return key, codepoint, []byte{shifted}, true
			}
		}
		if mods&protocol.KeyMods(libghostty.ModCtrl) == 0 {
			return key, codepoint, []byte{ch}, true
		}
		return key, codepoint, nil, true
	}

	switch keyCode {
	case 0x100:
		return libghostty.KeyEnter, '\r', nil, true
	case 0x101:
		return libghostty.KeyEscape, 0x1b, nil, true
	case 0x102:
		return libghostty.KeyTab, '\t', nil, true
	case 0x103:
		return libghostty.KeyBackspace, 0x7f, nil, true
	case 0x110:
		return libghostty.KeyArrowUp, 0, nil, true
	case 0x111:
		return libghostty.KeyArrowDown, 0, nil, true
	case 0x112:
		return libghostty.KeyArrowLeft, 0, nil, true
	case 0x113:
		return libghostty.KeyArrowRight, 0, nil, true
	case 0x120:
		return libghostty.KeyHome, 0, nil, true
	case 0x121:
		return libghostty.KeyEnd, 0, nil, true
	case 0x122:
		return libghostty.KeyPageUp, 0, nil, true
	case 0x123:
		return libghostty.KeyPageDown, 0, nil, true
	case 0x124:
		return libghostty.KeyInsert, 0, nil, true
	case 0x125:
		return libghostty.KeyDelete, 0, nil, true
	case 0x130, 0x131, 0x132, 0x133, 0x134, 0x135, 0x136, 0x137, 0x138, 0x139, 0x13a, 0x13b:
		return libghostty.KeyF1 + libghostty.Key(keyCode-0x130), 0, nil, true
	default:
		return 0, 0, nil, false
	}
}

func shiftedASCII(ch byte) (byte, bool) {
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

func asciiPhysicalKey(ch byte) (libghostty.Key, bool) {
	switch ch {
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
	case '0', ')':
		return libghostty.KeyDigit0, true
	case '1', '!':
		return libghostty.KeyDigit1, true
	case '2', '@':
		return libghostty.KeyDigit2, true
	case '3', '#':
		return libghostty.KeyDigit3, true
	case '4', '$':
		return libghostty.KeyDigit4, true
	case '5', '%':
		return libghostty.KeyDigit5, true
	case '6', '^':
		return libghostty.KeyDigit6, true
	case '7', '&':
		return libghostty.KeyDigit7, true
	case '8', '*':
		return libghostty.KeyDigit8, true
	case '9', '(':
		return libghostty.KeyDigit9, true
	case '=', '+':
		return libghostty.KeyEqual, true
	case 'a', 'A':
		return libghostty.KeyA, true
	case 'b', 'B':
		return libghostty.KeyB, true
	case 'c', 'C':
		return libghostty.KeyC, true
	case 'd', 'D':
		return libghostty.KeyD, true
	case 'e', 'E':
		return libghostty.KeyE, true
	case 'f', 'F':
		return libghostty.KeyF, true
	case 'g', 'G':
		return libghostty.KeyG, true
	case 'h', 'H':
		return libghostty.KeyH, true
	case 'i', 'I':
		return libghostty.KeyI, true
	case 'j', 'J':
		return libghostty.KeyJ, true
	case 'k', 'K':
		return libghostty.KeyK, true
	case 'l', 'L':
		return libghostty.KeyL, true
	case 'm', 'M':
		return libghostty.KeyM, true
	case 'n', 'N':
		return libghostty.KeyN, true
	case 'o', 'O':
		return libghostty.KeyO, true
	case 'p', 'P':
		return libghostty.KeyP, true
	case 'q', 'Q':
		return libghostty.KeyQ, true
	case 'r', 'R':
		return libghostty.KeyR, true
	case 's', 'S':
		return libghostty.KeyS, true
	case 't', 'T':
		return libghostty.KeyT, true
	case 'u', 'U':
		return libghostty.KeyU, true
	case 'v', 'V':
		return libghostty.KeyV, true
	case 'w', 'W':
		return libghostty.KeyW, true
	case 'x', 'X':
		return libghostty.KeyX, true
	case 'y', 'Y':
		return libghostty.KeyY, true
	case 'z', 'Z':
		return libghostty.KeyZ, true
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
