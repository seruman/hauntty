// Package libghostty wraps the translated Ghostty VT module used by hauntty.
package libghostty

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/url"
	"sync"

	"code.selman.me/hauntty/libghostty/wasmvt"
)

//go:generate sh -c "set -e; trap 'rm -f ghostty-vt-small.wasm' EXIT; curl -fsSL https://tip.files.ghostty.org/16833f5e5f58589c3d6ba876a73eb0c5f550231d/ghostty-vt-small.wasm -o ghostty-vt-small.wasm; go tool wasm2go -unsafe -nanbox -pkg wasmvt -o wasmvt/vt.generated.go ghostty-vt-small.wasm"

const (
	feedBufSize = 64 * 1024
	scratchSize = 256

	ghosttySuccess = 0
	ghosttyNoValue = -4

	terminalOptionScrollbackMaxLines   = 28
	terminalOptionContinuationMaxBytes = 31

	terminalDataCols         = 1
	terminalDataRows         = 2
	terminalDataCursorX      = 3
	terminalDataCursorY      = 4
	terminalDataActiveScreen = 6
	terminalDataPwd          = 13
	terminalDataVTGround     = 38

	terminalScreenAlternate = 1

	formatterFormatPlain = 0
	formatterFormatVT    = 1
	formatterFormatHTML  = 2

	// These sizes and offsets are the WebAssembly layouts reported by ghostty_type_json.
	formatterOptionsSize = 40
	formatterExtraSize   = 24
	formatterScreenSize  = 12
	gridRefSize          = 12
	selectionSize        = 32

	formatterOptionsOffset = 0
	formatterHandleOffset  = 40
	pointOffset            = 48
	startRefOffset         = 72
	endRefOffset           = 84
	selectionOffset        = 96
	outputPtrOffset        = 128
	outputLenOffset        = 132
	keyUTF8Offset          = 136

	continuationMaxBytes = 65 << 20 // Matches Ghostty's largest built-in APC buffer.
)

type wasmEnv struct{}

func (wasmEnv) Xlog(int32, int32) {}

type Runtime struct{}

func NewRuntime() (*Runtime, error) {
	return &Runtime{}, nil
}

func (r *Runtime) NewTerminal(cols, rows, scrollback uint32) (*Terminal, error) {
	if err := validSize(cols, rows); err != nil {
		return nil, err
	}

	mod := wasmvt.New(wasmEnv{})
	t, err := newTerminal(mod)
	if err != nil {
		return nil, err
	}

	scratch, err := t.scratch()
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	if result := mod.Xghostty_terminal_new(0, int32(t.scratchPtr), int32(cols), int32(rows)); result != ghosttySuccess {
		_ = t.Close()
		return nil, ghosttyError("ghostty_terminal_new", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	t.terminal = int32(binary.LittleEndian.Uint32(scratch))
	if t.terminal == 0 {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: ghostty_terminal_new returned null")
	}

	if err := t.initialize(scrollback); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

func (r *Runtime) RestoreTerminal(snapshot []byte, scrollback uint32) (*Terminal, error) {
	if len(snapshot) == 0 || len(snapshot) > math.MaxInt32 {
		return nil, fmt.Errorf("wasm: invalid snapshot size %d", len(snapshot))
	}

	mod := wasmvt.New(wasmEnv{})
	t, err := newTerminal(mod)
	if err != nil {
		return nil, err
	}

	snapshotPtr := uint32(mod.Xghostty_alloc(0, int32(len(snapshot))))
	if snapshotPtr == 0 {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: allocate snapshot buffer")
	}
	defer mod.Xghostty_free(0, int32(snapshotPtr), int32(len(snapshot)))
	if !t.writeMemory(snapshotPtr, snapshot) {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: write snapshot buffer")
	}

	scratch, err := t.scratch()
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	clear(scratch)
	if result := mod.Xghostty_snapshot_decoder_new_buf(0, int32(t.scratchPtr), int32(snapshotPtr), int32(len(snapshot))); result != ghosttySuccess {
		_ = t.Close()
		return nil, ghosttyError("ghostty_snapshot_decoder_new_buf", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	decoder := int32(binary.LittleEndian.Uint32(scratch))
	if decoder == 0 {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: ghostty_snapshot_decoder_new_buf returned null")
	}
	defer mod.Xghostty_snapshot_decoder_free(decoder)

	clear(scratch)
	if result := mod.Xghostty_snapshot_decoder_decode(decoder, int32(t.scratchPtr)); result != ghosttySuccess {
		_ = t.Close()
		return nil, ghosttyError("ghostty_snapshot_decoder_decode", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		_ = t.Close()
		return nil, err
	}
	t.terminal = int32(binary.LittleEndian.Uint32(scratch))
	if t.terminal == 0 {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: ghostty_snapshot_decoder_decode returned null")
	}

	if err := t.initialize(scrollback); err != nil {
		_ = t.Close()
		return nil, err
	}
	return t, nil
}

func newTerminal(mod *wasmvt.Module) (*Terminal, error) {
	t := &Terminal{mod: mod, mem: mod.Xmemory().Slice()}
	t.feedPtr = uint32(mod.Xghostty_alloc(0, feedBufSize))
	if t.feedPtr == 0 {
		return nil, fmt.Errorf("wasm: allocate feed buffer")
	}
	t.feedLen = feedBufSize

	t.scratchPtr = uint32(mod.Xghostty_alloc(0, scratchSize))
	if t.scratchPtr == 0 {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: allocate scratch buffer")
	}
	t.scratchLen = scratchSize
	return t, nil
}

func (t *Terminal) initialize(scrollback uint32) error {
	scratch, err := t.scratch()
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(scratch, scrollback)
	if result := t.mod.Xghostty_terminal_set(t.terminal, terminalOptionScrollbackMaxLines, int32(t.scratchPtr)); result != ghosttySuccess {
		return ghosttyError("ghostty_terminal_set", result)
	}

	scratch, err = t.scratch()
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(scratch, continuationMaxBytes)
	if result := t.mod.Xghostty_terminal_set(t.terminal, terminalOptionContinuationMaxBytes, int32(t.scratchPtr)); result != ghosttySuccess {
		return ghosttyError("ghostty_terminal_set", result)
	}

	scratch, err = t.scratch()
	if err != nil {
		return err
	}
	clear(scratch)
	if result := t.mod.Xghostty_key_encoder_new(0, int32(t.scratchPtr)); result != ghosttySuccess {
		return ghosttyError("ghostty_key_encoder_new", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		return err
	}
	t.keyEncoder = int32(binary.LittleEndian.Uint32(scratch))
	if t.keyEncoder == 0 {
		return fmt.Errorf("wasm: ghostty_key_encoder_new returned null")
	}

	clear(scratch)
	if result := t.mod.Xghostty_key_event_new(0, int32(t.scratchPtr)); result != ghosttySuccess {
		return ghosttyError("ghostty_key_event_new", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		return err
	}
	t.keyEvent = int32(binary.LittleEndian.Uint32(scratch))
	if t.keyEvent == 0 {
		return fmt.Errorf("wasm: ghostty_key_event_new returned null")
	}

	cols, err := t.getUint16(terminalDataCols)
	if err != nil {
		return err
	}
	rows, err := t.getUint16(terminalDataRows)
	if err != nil {
		return err
	}
	t.cols = uint32(cols)
	t.rows = uint32(rows)
	return nil
}

var _ io.Closer = (*Runtime)(nil)

func (r *Runtime) Close() error {
	return nil
}

type Terminal struct {
	mu  sync.Mutex
	mod *wasmvt.Module
	mem *[]byte

	terminal   int32
	keyEncoder int32
	keyEvent   int32
	cols       uint32
	rows       uint32

	feedPtr    uint32
	feedLen    uint32
	scratchPtr uint32
	scratchLen uint32
}

func (t *Terminal) Snapshot() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	scratch, err := t.scratch()
	if err != nil {
		return nil, err
	}
	clear(scratch)
	if result := t.mod.Xghostty_snapshot_encode_alloc(
		t.terminal,
		0,
		int32(t.scratchPtr+outputPtrOffset),
		int32(t.scratchPtr+outputLenOffset),
	); result != ghosttySuccess {
		return nil, ghosttyError("ghostty_snapshot_encode_alloc", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		return nil, err
	}

	outputPtr := binary.LittleEndian.Uint32(scratch[outputPtrOffset:])
	outputLen := binary.LittleEndian.Uint32(scratch[outputLenOffset:])
	if outputPtr != 0 {
		defer t.mod.Xghostty_free(0, int32(outputPtr), int32(outputLen))
	}
	buf, ok := t.readMemory(outputPtr, outputLen)
	if !ok {
		return nil, fmt.Errorf("wasm: read snapshot")
	}
	return append([]byte(nil), buf...), nil
}

func (t *Terminal) Feed(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for len(data) > 0 {
		chunk := data
		if uint32(len(chunk)) > t.feedLen {
			chunk = chunk[:t.feedLen]
		}
		if !t.writeMemory(t.feedPtr, chunk) {
			return fmt.Errorf("wasm: write feed buffer")
		}
		t.mod.Xghostty_terminal_vt_write(t.terminal, int32(t.feedPtr), int32(len(chunk)))
		data = data[len(chunk):]
	}
	return nil
}

func (t *Terminal) Resize(cols, rows uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := validSize(cols, rows); err != nil {
		return err
	}
	if result := t.mod.Xghostty_terminal_resize(t.terminal, int32(cols), int32(rows), 0, 0); result != ghosttySuccess {
		return ghosttyError("ghostty_terminal_resize", result)
	}
	t.cols = cols
	t.rows = rows
	return nil
}

type ScreenDump struct {
	Data        []byte
	CursorRow   uint32
	CursorCol   uint32
	IsAltScreen bool
}

type DumpFormat uint32

const (
	DumpPlain          DumpFormat = 0
	DumpVTFull         DumpFormat = 1
	DumpVTSafe         DumpFormat = 2
	DumpHTML           DumpFormat = 3
	DumpFlagUnwrap     DumpFormat = 0x10
	DumpFlagScrollback DumpFormat = 0x20
	DumpFormatMask     DumpFormat = 0x0F
)

type KeyCode uint32

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
	KeyF11       KeyCode = 0x13A
	KeyF12       KeyCode = 0x13B
)

type Modifier uint32

const (
	ModShift Modifier = 0x01
	ModCtrl  Modifier = 0x02
	ModAlt   Modifier = 0x04
	ModSuper Modifier = 0x08
)

func (t *Terminal) DumpScreen(format DumpFormat) (*ScreenDump, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := t.format(format)
	if err != nil {
		return nil, err
	}
	cursorCol, err := t.getUint16(terminalDataCursorX)
	if err != nil {
		return nil, err
	}
	cursorRow, err := t.getUint16(terminalDataCursorY)
	if err != nil {
		return nil, err
	}
	activeScreen, err := t.getUint32(terminalDataActiveScreen)
	if err != nil {
		return nil, err
	}

	return &ScreenDump{
		Data:        data,
		CursorRow:   uint32(cursorRow),
		CursorCol:   uint32(cursorCol),
		IsAltScreen: activeScreen == terminalScreenAlternate,
	}, nil
}

func (t *Terminal) format(format DumpFormat) ([]byte, error) {
	emit := int32(formatterFormatPlain)
	switch format & DumpFormatMask {
	case DumpPlain:
	case DumpVTFull, DumpVTSafe:
		emit = formatterFormatVT
	case DumpHTML:
		emit = formatterFormatHTML
	default:
		return nil, fmt.Errorf("wasm: unsupported dump format %d", format&DumpFormatMask)
	}

	scratch, err := t.scratch()
	if err != nil {
		return nil, err
	}
	clear(scratch)

	selection := uint32(0)
	if format&DumpFormatMask != DumpVTFull && format&DumpFlagScrollback == 0 {
		selection, err = t.activeSelection(scratch)
		if err != nil {
			return nil, err
		}
	}

	options := scratch[formatterOptionsOffset : formatterOptionsOffset+formatterOptionsSize]
	binary.LittleEndian.PutUint32(options[0:], formatterOptionsSize)
	binary.LittleEndian.PutUint32(options[4:], uint32(emit))
	options[8] = boolByte(format&DumpFlagUnwrap != 0)
	options[9] = 1
	binary.LittleEndian.PutUint32(options[12:], formatterExtraSize)
	binary.LittleEndian.PutUint32(options[24:], formatterScreenSize)
	binary.LittleEndian.PutUint32(options[36:], selection)

	if format&DumpFormatMask == DumpHTML {
		options[16] = 1
	}
	if format&DumpFormatMask == DumpVTFull {
		// Palette output overrides host colors; tab stops move the cursor after CUP.
		options[17] = 1
		options[18] = 1
		options[20] = 1
		options[21] = 1
		for i := 28; i <= 33; i++ {
			options[i] = 1
		}
	}

	formatterPtr := t.scratchPtr + formatterHandleOffset
	if result := t.mod.Xghostty_formatter_terminal_new(0, int32(formatterPtr), t.terminal, int32(t.scratchPtr+formatterOptionsOffset)); result != ghosttySuccess {
		return nil, ghosttyError("ghostty_formatter_terminal_new", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		return nil, err
	}
	formatter := int32(binary.LittleEndian.Uint32(scratch[formatterHandleOffset:]))
	if formatter == 0 {
		return nil, fmt.Errorf("wasm: ghostty_formatter_terminal_new returned null")
	}
	defer t.mod.Xghostty_formatter_free(formatter)

	binary.LittleEndian.PutUint32(scratch[outputPtrOffset:], 0)
	binary.LittleEndian.PutUint32(scratch[outputLenOffset:], 0)
	if result := t.mod.Xghostty_formatter_format_alloc(
		formatter,
		0,
		int32(t.scratchPtr+outputPtrOffset),
		int32(t.scratchPtr+outputLenOffset),
	); result != ghosttySuccess {
		return nil, ghosttyError("ghostty_formatter_format_alloc", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		return nil, err
	}

	outputPtr := binary.LittleEndian.Uint32(scratch[outputPtrOffset:])
	outputLen := binary.LittleEndian.Uint32(scratch[outputLenOffset:])
	if outputPtr != 0 {
		defer t.mod.Xghostty_free(0, int32(outputPtr), int32(outputLen))
	}
	buf, ok := t.readMemory(outputPtr, outputLen)
	if !ok {
		return nil, fmt.Errorf("wasm: read formatted output")
	}
	out := append([]byte(nil), buf...)
	if format&DumpFormatMask == DumpVTSafe {
		out = append(out, "\x1b[0m"...)
	}
	return out, nil
}

func (t *Terminal) activeSelection(scratch []byte) (uint32, error) {
	point := scratch[pointOffset : pointOffset+24]
	startRef := scratch[startRefOffset : startRefOffset+gridRefSize]
	endRef := scratch[endRefOffset : endRefOffset+gridRefSize]

	binary.LittleEndian.PutUint16(point[8:], 0)
	binary.LittleEndian.PutUint32(point[12:], 0)
	binary.LittleEndian.PutUint32(startRef, gridRefSize)
	if result := t.mod.Xghostty_terminal_grid_ref(t.terminal, int32(t.scratchPtr+pointOffset), int32(t.scratchPtr+startRefOffset)); result != ghosttySuccess {
		return 0, ghosttyError("ghostty_terminal_grid_ref", result)
	}

	binary.LittleEndian.PutUint16(point[8:], uint16(t.cols-1))
	binary.LittleEndian.PutUint32(point[12:], t.rows-1)
	binary.LittleEndian.PutUint32(endRef, gridRefSize)
	if result := t.mod.Xghostty_terminal_grid_ref(t.terminal, int32(t.scratchPtr+pointOffset), int32(t.scratchPtr+endRefOffset)); result != ghosttySuccess {
		return 0, ghosttyError("ghostty_terminal_grid_ref", result)
	}

	selection := scratch[selectionOffset : selectionOffset+selectionSize]
	binary.LittleEndian.PutUint32(selection, selectionSize)
	copy(selection[4:], startRef)
	copy(selection[16:], endRef)
	return t.scratchPtr + selectionOffset, nil
}

func (t *Terminal) EncodeKey(keyCode KeyCode, mods Modifier) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key, codepoint, text, ok := keyEvent(keyCode, mods)
	if !ok {
		return nil, fmt.Errorf("wasm: unsupported key code %d", keyCode)
	}

	scratch, err := t.scratch()
	if err != nil {
		return nil, err
	}
	clear(scratch)

	t.mod.Xghostty_key_event_set_action(t.keyEvent, 1)
	t.mod.Xghostty_key_event_set_key(t.keyEvent, key)
	t.mod.Xghostty_key_event_set_mods(t.keyEvent, int32(mods&0xFFFF))
	consumed := Modifier(0)
	if mods&ModShift != 0 && keyCode >= 'a' && keyCode <= 'z' {
		consumed = ModShift
	}
	t.mod.Xghostty_key_event_set_consumed_mods(t.keyEvent, int32(consumed))
	t.mod.Xghostty_key_event_set_composing(t.keyEvent, 0)
	t.mod.Xghostty_key_event_set_unshifted_codepoint(t.keyEvent, int32(codepoint))

	textPtr := int32(0)
	if len(text) > 0 {
		copy(scratch[keyUTF8Offset:], text)
		textPtr = int32(t.scratchPtr + keyUTF8Offset)
	}
	t.mod.Xghostty_key_event_set_utf8(t.keyEvent, textPtr, int32(len(text)))
	t.mod.Xghostty_key_encoder_setopt_from_terminal(t.keyEncoder, t.terminal)

	if result := t.mod.Xghostty_key_encoder_encode(
		t.keyEncoder,
		t.keyEvent,
		int32(t.feedPtr),
		int32(t.feedLen),
		int32(t.scratchPtr+outputLenOffset),
	); result != ghosttySuccess {
		return nil, ghosttyError("ghostty_key_encoder_encode", result)
	}
	scratch, err = t.scratch()
	if err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(scratch[outputLenOffset:])
	buf, ok := t.readMemory(t.feedPtr, length)
	if !ok {
		return nil, fmt.Errorf("wasm: read encoded key")
	}
	return append([]byte(nil), buf...), nil
}

func keyEvent(keyCode KeyCode, mods Modifier) (key int32, codepoint uint32, text []byte, ok bool) {
	codepoint = uint32(keyCode)
	if keyCode >= 0x20 && keyCode <= 0x7E {
		key, ok = asciiKey(byte(keyCode))
		if !ok {
			return 0, 0, nil, false
		}
		ch := byte(keyCode)
		if mods&ModShift != 0 && ch >= 'a' && ch <= 'z' {
			text = []byte{ch - ('a' - 'A')}
		} else if mods&ModCtrl == 0 {
			text = []byte{ch}
		}
		return key, codepoint, text, true
	}

	switch keyCode {
	case KeyEnter:
		return 58, '\r', nil, true
	case KeyEscape:
		return 120, 0x1B, nil, true
	case KeyTab:
		return 64, '\t', nil, true
	case KeyBackspace:
		return 53, 0x7F, nil, true
	case KeyDown:
		return 75, 0, nil, true
	case KeyLeft:
		return 76, 0, nil, true
	case KeyRight:
		return 77, 0, nil, true
	case KeyUp:
		return 78, 0, nil, true
	case KeyDelete:
		return 68, 0, nil, true
	case KeyEnd:
		return 69, 0, nil, true
	case KeyHome:
		return 71, 0, nil, true
	case KeyInsert:
		return 72, 0, nil, true
	case KeyPageDown:
		return 73, 0, nil, true
	case KeyPageUp:
		return 74, 0, nil, true
	case KeyF1, KeyF2, KeyF3, KeyF4, KeyF5, KeyF6, KeyF7, KeyF8, KeyF9, KeyF10, KeyF11, KeyF12:
		return 121 + int32(keyCode-KeyF1), 0, nil, true
	default:
		return 0, 0, nil, false
	}
}

func asciiKey(ch byte) (int32, bool) {
	switch ch {
	case '`', '~':
		return 1, true
	case '\\', '|':
		return 2, true
	case '[', '{':
		return 3, true
	case ']', '}':
		return 4, true
	case ',', '<':
		return 5, true
	case '0', ')':
		return 6, true
	case '1', '!':
		return 7, true
	case '2', '@':
		return 8, true
	case '3', '#':
		return 9, true
	case '4', '$':
		return 10, true
	case '5', '%':
		return 11, true
	case '6', '^':
		return 12, true
	case '7', '&':
		return 13, true
	case '8', '*':
		return 14, true
	case '9', '(':
		return 15, true
	case '=', '+':
		return 16, true
	case 'a', 'A':
		return 20, true
	case 'b', 'B':
		return 21, true
	case 'c', 'C':
		return 22, true
	case 'd', 'D':
		return 23, true
	case 'e', 'E':
		return 24, true
	case 'f', 'F':
		return 25, true
	case 'g', 'G':
		return 26, true
	case 'h', 'H':
		return 27, true
	case 'i', 'I':
		return 28, true
	case 'j', 'J':
		return 29, true
	case 'k', 'K':
		return 30, true
	case 'l', 'L':
		return 31, true
	case 'm', 'M':
		return 32, true
	case 'n', 'N':
		return 33, true
	case 'o', 'O':
		return 34, true
	case 'p', 'P':
		return 35, true
	case 'q', 'Q':
		return 36, true
	case 'r', 'R':
		return 37, true
	case 's', 'S':
		return 38, true
	case 't', 'T':
		return 39, true
	case 'u', 'U':
		return 40, true
	case 'v', 'V':
		return 41, true
	case 'w', 'W':
		return 42, true
	case 'x', 'X':
		return 43, true
	case 'y', 'Y':
		return 44, true
	case 'z', 'Z':
		return 45, true
	case '-', '_':
		return 46, true
	case '.', '>':
		return 47, true
	case '\'', '"':
		return 48, true
	case ';', ':':
		return 49, true
	case '/', '?':
		return 50, true
	case ' ':
		return 63, true
	default:
		return 0, false
	}
}

func (t *Terminal) VTGround() (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	scratch, err := t.scratch()
	if err != nil {
		return false, err
	}
	clear(scratch)
	if result := t.mod.Xghostty_terminal_get(t.terminal, terminalDataVTGround, int32(t.scratchPtr)); result != ghosttySuccess {
		return false, ghosttyError("ghostty_terminal_get", result)
	}
	return scratch[0] != 0, nil
}

func (t *Terminal) GetCwd() (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	scratch, err := t.scratch()
	if err != nil {
		return "", false, err
	}
	clear(scratch)
	result := t.mod.Xghostty_terminal_get(t.terminal, terminalDataPwd, int32(t.scratchPtr))
	if result == ghosttyNoValue {
		return "", false, nil
	}
	if result != ghosttySuccess {
		return "", false, ghosttyError("ghostty_terminal_get", result)
	}

	ptr := binary.LittleEndian.Uint32(scratch)
	length := binary.LittleEndian.Uint32(scratch[4:])
	if length == 0 {
		return "", false, nil
	}
	buf, ok := t.readMemory(ptr, length)
	if !ok {
		return "", false, fmt.Errorf("wasm: read terminal pwd")
	}
	return stripFileURL(string(buf)), true, nil
}

func stripFileURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme != "" && u.Path != "" {
		return u.Path
	}
	return raw
}

func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.mod == nil {
		return nil
	}
	if t.keyEvent != 0 {
		t.mod.Xghostty_key_event_free(t.keyEvent)
		t.keyEvent = 0
	}
	if t.keyEncoder != 0 {
		t.mod.Xghostty_key_encoder_free(t.keyEncoder)
		t.keyEncoder = 0
	}
	if t.terminal != 0 {
		t.mod.Xghostty_terminal_free(t.terminal)
		t.terminal = 0
	}
	if t.feedPtr != 0 {
		t.mod.Xghostty_free(0, int32(t.feedPtr), int32(t.feedLen))
		t.feedPtr = 0
	}
	if t.scratchPtr != 0 {
		t.mod.Xghostty_free(0, int32(t.scratchPtr), int32(t.scratchLen))
		t.scratchPtr = 0
	}
	t.mod = nil
	t.mem = nil
	return nil
}

func (t *Terminal) getUint16(data int32) (uint16, error) {
	scratch, err := t.scratch()
	if err != nil {
		return 0, err
	}
	clear(scratch)
	if result := t.mod.Xghostty_terminal_get(t.terminal, data, int32(t.scratchPtr)); result != ghosttySuccess {
		return 0, ghosttyError("ghostty_terminal_get", result)
	}
	return binary.LittleEndian.Uint16(scratch), nil
}

func (t *Terminal) getUint32(data int32) (uint32, error) {
	scratch, err := t.scratch()
	if err != nil {
		return 0, err
	}
	clear(scratch)
	if result := t.mod.Xghostty_terminal_get(t.terminal, data, int32(t.scratchPtr)); result != ghosttySuccess {
		return 0, ghosttyError("ghostty_terminal_get", result)
	}
	return binary.LittleEndian.Uint32(scratch), nil
}

func (t *Terminal) scratch() ([]byte, error) {
	buf, ok := t.readMemory(t.scratchPtr, t.scratchLen)
	if !ok {
		return nil, fmt.Errorf("wasm: access scratch buffer")
	}
	return buf, nil
}

func (t *Terminal) readMemory(ptr, length uint32) ([]byte, bool) {
	if t.mem == nil {
		return nil, false
	}
	mem := *t.mem
	end := uint64(ptr) + uint64(length)
	if end > uint64(len(mem)) {
		return nil, false
	}
	return mem[int(ptr):int(end)], true
}

func (t *Terminal) writeMemory(ptr uint32, data []byte) bool {
	buf, ok := t.readMemory(ptr, uint32(len(data)))
	if !ok {
		return false
	}
	copy(buf, data)
	return true
}

func validSize(cols, rows uint32) error {
	if cols == 0 || rows == 0 || cols > math.MaxUint16 || rows > math.MaxUint16 {
		return fmt.Errorf("wasm: invalid terminal size %dx%d", cols, rows)
	}
	return nil
}

func ghosttyError(name string, result int32) error {
	return fmt.Errorf("wasm: %s returned %d", name, result)
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}
