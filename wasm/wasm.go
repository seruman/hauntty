package wasm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const feedBufSize = 64 * 1024 // 64KB feed buffer

// Runtime holds a compiled WASM module and can create Terminal instances.
type Runtime struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	counter  atomic.Uint64
}

// NewRuntime compiles the hauntty-vt WASM bytes and prepares the runtime.
func NewRuntime(ctx context.Context, wasmBytes []byte) (*Runtime, error) {
	rt := wazero.NewRuntime(ctx)

	// Provide the env.log host import.
	_, err := rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			ptr := api.DecodeU32(stack[0])
			length := api.DecodeU32(stack[1])
			if buf, ok := mod.Memory().Read(ptr, length); ok {
				slog.Debug("wasm", "msg", string(buf))
			}
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil).
		Export("log").
		Instantiate(ctx)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiate env module: %w", err)
	}

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("wasm: compile module: %w", err)
	}

	return &Runtime{rt: rt, compiled: compiled}, nil
}

// NewTerminal instantiates a new WASM module and initializes a terminal.
func (r *Runtime) NewTerminal(ctx context.Context, cols, rows, scrollback uint32) (*Terminal, error) {
	name := fmt.Sprintf("hauntty-vt-%d", r.counter.Add(1))

	mod, err := r.rt.InstantiateModule(ctx, r.compiled, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		return nil, fmt.Errorf("wasm: instantiate module: %w", err)
	}

	t := &Terminal{
		mod:          mod,
		gxAlloc:      mod.ExportedFunction("gx_alloc"),
		gxFree:       mod.ExportedFunction("gx_free"),
		gxInit:       mod.ExportedFunction("gx_init"),
		gxDeinit:     mod.ExportedFunction("gx_deinit"),
		gxFeed:       mod.ExportedFunction("gx_feed"),
		gxResize:     mod.ExportedFunction("gx_resize"),
		gxDumpScreen: mod.ExportedFunction("gx_dump_screen"),
		gxDumpPtr:    mod.ExportedFunction("gx_dump_ptr"),
		gxGetCursor:  mod.ExportedFunction("gx_get_cursor_pos"),
		gxIsAltScr:   mod.ExportedFunction("gx_is_alt_screen"),
		gxReset:      mod.ExportedFunction("gx_reset"),
	}

	// Validate all exports are present.
	for name, fn := range map[string]api.Function{
		"gx_alloc":          t.gxAlloc,
		"gx_free":           t.gxFree,
		"gx_init":           t.gxInit,
		"gx_deinit":         t.gxDeinit,
		"gx_feed":           t.gxFeed,
		"gx_resize":         t.gxResize,
		"gx_dump_screen":    t.gxDumpScreen,
		"gx_dump_ptr":       t.gxDumpPtr,
		"gx_get_cursor_pos": t.gxGetCursor,
		"gx_is_alt_screen":  t.gxIsAltScr,
		"gx_reset":          t.gxReset,
	} {
		if fn == nil {
			mod.Close(ctx)
			return nil, fmt.Errorf("wasm: missing export %q", name)
		}
	}

	// Allocate feed buffer.
	results, err := t.gxAlloc.Call(ctx, uint64(feedBufSize))
	if err != nil {
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm: gx_alloc feed buffer: %w", err)
	}
	t.feedPtr = uint32(results[0])
	if t.feedPtr == 0 {
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm: gx_alloc returned null")
	}
	t.feedLen = feedBufSize

	// Initialize terminal.
	results, err = t.gxInit.Call(ctx, uint64(cols), uint64(rows), uint64(scrollback))
	if err != nil {
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm: gx_init: %w", err)
	}
	if int32(results[0]) != 0 {
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm: gx_init returned %d", int32(results[0]))
	}

	return t, nil
}

// Close releases the runtime and compiled module.
func (r *Runtime) Close(ctx context.Context) error {
	return r.rt.Close(ctx)
}

// Terminal wraps a single WASM module instance with a virtual terminal.
type Terminal struct {
	mu  sync.Mutex
	mod api.Module

	gxAlloc      api.Function
	gxFree       api.Function
	gxInit       api.Function
	gxDeinit     api.Function
	gxFeed       api.Function
	gxResize     api.Function
	gxDumpScreen api.Function
	gxDumpPtr    api.Function
	gxGetCursor  api.Function
	gxIsAltScr   api.Function
	gxReset      api.Function

	feedPtr uint32
	feedLen uint32
}

// Feed writes PTY output data into the terminal. Data larger than the feed
// buffer is automatically chunked.
func (t *Terminal) Feed(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for len(data) > 0 {
		chunk := data
		if uint32(len(chunk)) > t.feedLen {
			chunk = chunk[:t.feedLen]
		}
		if !t.mod.Memory().Write(t.feedPtr, chunk) {
			return fmt.Errorf("wasm: memory write failed")
		}
		results, err := t.gxFeed.Call(ctx, uint64(t.feedPtr), uint64(len(chunk)))
		if err != nil {
			return fmt.Errorf("wasm: gx_feed: %w", err)
		}
		if int32(results[0]) != 0 {
			return fmt.Errorf("wasm: gx_feed returned %d", int32(results[0]))
		}
		data = data[len(chunk):]
	}
	return nil
}

// Resize changes the terminal dimensions.
func (t *Terminal) Resize(ctx context.Context, cols, rows uint32) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	results, err := t.gxResize.Call(ctx, uint64(cols), uint64(rows))
	if err != nil {
		return fmt.Errorf("wasm: gx_resize: %w", err)
	}
	if int32(results[0]) != 0 {
		return fmt.Errorf("wasm: gx_resize returned %d", int32(results[0]))
	}
	return nil
}

// Reset performs a full terminal reset.
func (t *Terminal) Reset(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	results, err := t.gxReset.Call(ctx)
	if err != nil {
		return fmt.Errorf("wasm: gx_reset: %w", err)
	}
	if int32(results[0]) != 0 {
		return fmt.Errorf("wasm: gx_reset returned %d", int32(results[0]))
	}
	return nil
}

// ScreenDump holds the result of a DumpScreen call.
type ScreenDump struct {
	VT          []byte
	CursorRow   uint32
	CursorCol   uint32
	IsAltScreen bool
}

// DumpScreen serializes the terminal screen to VT escape sequences and
// returns the result along with cursor position and alt screen state.
func (t *Terminal) DumpScreen(ctx context.Context) (*ScreenDump, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Dump screen into internal WASM buffer.
	results, err := t.gxDumpScreen.Call(ctx)
	if err != nil {
		return nil, fmt.Errorf("wasm: gx_dump_screen: %w", err)
	}
	length := int32(results[0])
	if length < 0 {
		return nil, fmt.Errorf("wasm: gx_dump_screen returned %d", length)
	}

	// Get pointer to dump.
	results, err = t.gxDumpPtr.Call(ctx)
	if err != nil {
		return nil, fmt.Errorf("wasm: gx_dump_ptr: %w", err)
	}
	ptr := uint32(results[0])

	// Read the VT bytes from WASM memory.
	var vt []byte
	if length > 0 {
		buf, ok := t.mod.Memory().Read(ptr, uint32(length))
		if !ok {
			return nil, fmt.Errorf("wasm: reading dump buffer failed")
		}
		vt = make([]byte, len(buf))
		copy(vt, buf)
	}

	// Get cursor position (packed: col | row<<16).
	results, err = t.gxGetCursor.Call(ctx)
	if err != nil {
		return nil, fmt.Errorf("wasm: gx_get_cursor_pos: %w", err)
	}
	packed := uint32(results[0])
	cursorCol := packed & 0xFFFF
	cursorRow := packed >> 16

	// Get alt screen state.
	results, err = t.gxIsAltScr.Call(ctx)
	if err != nil {
		return nil, fmt.Errorf("wasm: gx_is_alt_screen: %w", err)
	}
	isAlt := uint32(results[0]) == 1

	return &ScreenDump{
		VT:          vt,
		CursorRow:   cursorRow,
		CursorCol:   cursorCol,
		IsAltScreen: isAlt,
	}, nil
}

// Close destroys the terminal and frees resources.
func (t *Terminal) Close(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.gxDeinit != nil {
		t.gxDeinit.Call(ctx)
	}
	if t.feedPtr != 0 {
		t.gxFree.Call(ctx, uint64(t.feedPtr), uint64(t.feedLen))
		t.feedPtr = 0
	}
	return t.mod.Close(ctx)
}
