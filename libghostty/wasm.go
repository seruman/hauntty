// Package libghostty wraps the translated Ghostty VT module used by hauntty.
package libghostty

import (
	"fmt"
	"io"
	"net/url"
	"sync"

	"code.selman.me/hauntty/libghostty/wasmvt"
)

//go:generate sh -c "cd ../vt && DEVELOPER_DIR=/Library/Developer/CommandLineTools zig build -Doptimize=ReleaseSmall && cd ../libghostty && mkdir -p wasmvt && go tool wasm2go < hauntty-vt.wasm | sed 's/^package wasm2go$/package wasmvt/' > wasmvt/vt.generated.go"

const feedBufSize = 64 * 1024 // 64KB feed buffer

type Runtime struct{}

func NewRuntime() (*Runtime, error) {
	return &Runtime{}, nil
}

func (r *Runtime) NewTerminal(cols, rows, scrollback uint32) (*Terminal, error) {
	mod := wasmvt.New()
	mem := mod.Xmemory().Slice()

	t := &Terminal{
		mod: mod,
		mem: mem,
	}

	t.feedPtr = uint32(mod.Xgx_alloc(feedBufSize))
	if t.feedPtr == 0 {
		return nil, fmt.Errorf("wasm: gx_alloc returned null")
	}
	t.feedLen = feedBufSize

	if rc := int32(mod.Xgx_init(int32(cols), int32(rows), int32(scrollback))); rc != 0 {
		_ = t.Close()
		return nil, fmt.Errorf("wasm: gx_init returned %d", rc)
	}

	return t, nil
}

var _ io.Closer = (*Runtime)(nil)

func (r *Runtime) Close() error {
	return nil
}

type Terminal struct {
	mu  sync.Mutex
	mod *wasmvt.Module
	mem *[]byte

	feedPtr uint32
	feedLen uint32
}

// Feed writes terminal input, chunking data larger than the shared feed buffer.
func (t *Terminal) Feed(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for len(data) > 0 {
		chunk := data
		if uint32(len(chunk)) > t.feedLen {
			chunk = chunk[:t.feedLen]
		}
		if !t.writeMemory(t.feedPtr, chunk) {
			return fmt.Errorf("wasm: memory write failed")
		}
		if rc := int32(t.mod.Xgx_feed(int32(t.feedPtr), int32(len(chunk)))); rc != 0 {
			return fmt.Errorf("wasm: gx_feed returned %d", rc)
		}
		data = data[len(chunk):]
	}
	return nil
}

func (t *Terminal) Resize(cols, rows uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if rc := int32(t.mod.Xgx_resize(int32(cols), int32(rows))); rc != 0 {
		return fmt.Errorf("wasm: gx_resize returned %d", rc)
	}
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
	DumpPlain          DumpFormat = 0    // Plain text, no escape sequences.
	DumpVTFull         DumpFormat = 1    // Full VT with all extras (for reattach).
	DumpVTSafe         DumpFormat = 2    // Safe VT — colors but no palette/mode corruption.
	DumpHTML           DumpFormat = 3    // HTML with inline CSS colors.
	DumpFlagUnwrap     DumpFormat = 0x10 // Bit 4: join soft-wrapped lines.
	DumpFlagScrollback DumpFormat = 0x20 // Bit 5: include scrollback history.
	DumpFormatMask     DumpFormat = 0x0F // Bits 0-3: format selector.
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

func (t *Terminal) readResult(length int32) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	ptr := uint32(t.mod.Xgx_dump_ptr())
	buf, ok := t.readMemory(ptr, uint32(length))
	if !ok {
		return nil, fmt.Errorf("wasm: memory read failed")
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	return out, nil
}

func (t *Terminal) DumpScreen(format DumpFormat) (*ScreenDump, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	length := int32(t.mod.Xgx_dump_screen(int32(format)))
	if length < 0 {
		return nil, fmt.Errorf("wasm: gx_dump_screen returned %d", length)
	}

	vt, err := t.readResult(length)
	if err != nil {
		return nil, err
	}

	packed := uint32(t.mod.Xgx_get_cursor_pos())
	cursorCol := packed & 0xFFFF
	cursorRow := packed >> 16
	isAlt := uint32(t.mod.Xgx_is_alt_screen()) == 1

	return &ScreenDump{
		Data:        vt,
		CursorRow:   cursorRow,
		CursorCol:   cursorCol,
		IsAltScreen: isAlt,
	}, nil
}

func (t *Terminal) EncodeKey(keyCode KeyCode, mods Modifier) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	length := int32(t.mod.Xgx_encode_key(int32(keyCode), int32(mods)))
	if length < 0 {
		return nil, fmt.Errorf("wasm: gx_encode_key returned %d", length)
	}

	return t.readResult(length)
}

func (t *Terminal) GetCwd() (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	length := uint32(t.mod.Xgx_get_pwd_len())
	if length == 0 {
		return "", false, nil
	}

	ptr := uint32(t.mod.Xgx_get_pwd_ptr())
	if ptr == 0 {
		return "", false, fmt.Errorf("wasm: gx_get_pwd_ptr returned null")
	}

	buf, ok := t.readMemory(ptr, length)
	if !ok {
		return "", false, fmt.Errorf("wasm: memory read failed")
	}
	raw := make([]byte, len(buf))
	copy(raw, buf)
	return stripFileURL(string(raw)), true, nil
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
	t.mod.Xgx_deinit()
	if t.feedPtr != 0 {
		t.mod.Xgx_free(int32(t.feedPtr), int32(t.feedLen))
		t.feedPtr = 0
	}
	t.mod = nil
	t.mem = nil
	return nil
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
	if t.mem == nil {
		return false
	}
	mem := *t.mem
	end := uint64(ptr) + uint64(len(data))
	if end > uint64(len(mem)) {
		return false
	}
	copy(mem[int(ptr):int(end)], data)
	return true
}
