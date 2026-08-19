package libghostty

import (
	"io"
)

const (
	terminalOptionScrollbackMaxLines   = 28
	terminalOptionContinuationMaxBytes = 31

	feedBufferSize = 64 * 1024
)

type Terminal struct {
	rt      *wasmRuntime
	ptr     uint32
	feedPtr uint32
}

type TerminalOption func(*TerminalConfig)

type TerminalConfig struct {
	Cols                 uint16
	Rows                 uint16
	MaxScrollbackLines   *uint
	ContinuationMaxBytes *uint
}

func WithSize(cols, rows uint16) TerminalOption {
	return func(c *TerminalConfig) {
		c.Cols = cols
		c.Rows = rows
	}
}

func WithMaxScrollbackLines(lines uint) TerminalOption {
	return func(c *TerminalConfig) {
		c.MaxScrollbackLines = &lines
	}
}

func WithContinuationMaxBytes(limit uint) TerminalOption {
	return func(c *TerminalConfig) {
		c.ContinuationMaxBytes = &limit
	}
}

func NewTerminal(opts ...TerminalOption) (*Terminal, error) {
	cfg := TerminalConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	rt := sharedRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	ptr, err := rt.opaque(func(slot uint32) int32 {
		return rt.mod.Xghostty_terminal_new(0, int32(slot), int32(cfg.Cols), int32(cfg.Rows))
	})
	if err != nil {
		return nil, err
	}

	t, err := wrapTerminalLocked(rt, ptr)
	if err != nil {
		rt.mod.Xghostty_terminal_free(int32(ptr))
		return nil, err
	}

	if cfg.MaxScrollbackLines != nil {
		if err := t.setScrollbackMaxLinesLocked(cfg.MaxScrollbackLines); err != nil {
			t.closeLocked()
			return nil, err
		}
	}

	if cfg.ContinuationMaxBytes != nil {
		if err := t.setContinuationMaxBytesLocked(*cfg.ContinuationMaxBytes); err != nil {
			t.closeLocked()
			return nil, err
		}
	}

	return t, nil
}

func wrapTerminalLocked(rt *wasmRuntime, ptr uint32) (*Terminal, error) {
	feedPtr, err := rt.alloc(feedBufferSize)
	if err != nil {
		return nil, err
	}

	return &Terminal{rt: rt, ptr: ptr, feedPtr: feedPtr}, nil
}

func (t *Terminal) Close() {
	if t == nil || t.rt == nil {
		return
	}

	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	t.closeLocked()
}

func (t *Terminal) closeLocked() {
	if t.ptr != 0 {
		t.rt.mod.Xghostty_terminal_free(int32(t.ptr))
		t.ptr = 0
	}

	t.rt.free(t.feedPtr, feedBufferSize)
	t.feedPtr = 0
}

func (t *Terminal) Reset() {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	t.rt.mod.Xghostty_terminal_reset(int32(t.ptr))
}

func (t *Terminal) Resize(cols, rows uint16, cellWidthPx, cellHeightPx uint32) error {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	return resultError(t.rt.mod.Xghostty_terminal_resize(
		int32(t.ptr),
		int32(cols),
		int32(rows),
		int32(cellWidthPx),
		int32(cellHeightPx),
	))
}

func (t *Terminal) VTWrite(data []byte) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	for len(data) > 0 {
		length := min(len(data), feedBufferSize)
		if err := t.rt.put(t.feedPtr, data[:length]); err != nil {
			panic(err)
		}

		t.rt.mod.Xghostty_terminal_vt_write(int32(t.ptr), int32(t.feedPtr), int32(length))
		data = data[length:]
	}
}

func (t *Terminal) Write(p []byte) (int, error) {
	t.VTWrite(p)

	return len(p), nil
}

func (t *Terminal) SetContinuationMaxBytes(limit uint) error {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	return t.setContinuationMaxBytesLocked(limit)
}

func (t *Terminal) setContinuationMaxBytesLocked(limit uint) error {
	value, err := wasmUint(limit)
	if err != nil {
		return err
	}

	return t.rt.value32(value, func(ptr uint32) int32 {
		return t.rt.mod.Xghostty_terminal_set(int32(t.ptr), terminalOptionContinuationMaxBytes, int32(ptr))
	})
}

func (t *Terminal) SetScrollbackMaxLines(limit *uint) error {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	return t.setScrollbackMaxLinesLocked(limit)
}

func (t *Terminal) setScrollbackMaxLinesLocked(limit *uint) error {
	if limit == nil {
		return resultError(t.rt.mod.Xghostty_terminal_set(int32(t.ptr), terminalOptionScrollbackMaxLines, 0))
	}

	value, err := wasmUint(*limit)
	if err != nil {
		return err
	}

	return t.rt.value32(value, func(ptr uint32) int32 {
		return t.rt.mod.Xghostty_terminal_set(int32(t.ptr), terminalOptionScrollbackMaxLines, int32(ptr))
	})
}

var _ io.Writer = (*Terminal)(nil)
