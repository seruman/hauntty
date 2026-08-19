package libghostty

import (
	"encoding/binary"
	"fmt"
	"sync"

	"code.selman.me/hauntty/libghostty/internal/wasmvt"
)

//go:generate sh -c "cd internal/wasmvt && shasum -a 256 -c ghostty-vt-small.wasm.sha256 && go tool wasm2go -unsafe -nanbox -pkg wasmvt -o vt.generated.go ghostty-vt-small.wasm"

type wasmRuntime struct {
	mu  sync.Mutex
	mod *wasmvt.Module
}

var runtimeState struct {
	once sync.Once
	rt   *wasmRuntime
}

func sharedRuntime() *wasmRuntime {
	runtimeState.once.Do(func() {
		runtimeState.rt = &wasmRuntime{mod: wasmvt.New()}
	})
	return runtimeState.rt
}

func (r *wasmRuntime) alloc(length uint32) (uint32, error) {
	if length == 0 {
		return 0, nil
	}

	ptr := uint32(r.mod.Xghostty_wasm_alloc(int32(length)))
	if ptr == 0 {
		return 0, &Error{Result: ResultOutOfMemory}
	}

	return ptr, nil
}

func (r *wasmRuntime) free(ptr, length uint32) {
	if ptr != 0 {
		r.mod.Xghostty_wasm_free(int32(ptr), int32(length))
	}
}

func (r *wasmRuntime) opaque(constructor func(uint32) int32) (uint32, error) {
	slot := uint32(r.mod.Xghostty_wasm_alloc_opaque())
	if slot == 0 {
		return 0, &Error{Result: ResultOutOfMemory}
	}
	defer r.mod.Xghostty_wasm_free_opaque(int32(slot))

	if err := resultError(constructor(slot)); err != nil {
		return 0, err
	}

	ptr := uint32(r.mod.Xghostty_wasm_take_opaque(int32(slot)))
	if ptr == 0 {
		return 0, &Error{Result: ResultOutOfMemory}
	}

	return ptr, nil
}

func (r *wasmRuntime) bytes(ptr, length uint32) ([]byte, error) {
	memory := *r.mod.Xmemory().Slice()
	end := uint64(ptr) + uint64(length)
	if end > uint64(len(memory)) {
		return nil, fmt.Errorf("libghostty: invalid wasm memory range")
	}

	return memory[int(ptr):int(end)], nil
}

func (r *wasmRuntime) put(ptr uint32, data []byte) error {
	buf, err := r.bytes(ptr, uint32(len(data)))
	if err != nil {
		return err
	}

	copy(buf, data)
	return nil
}

func (r *wasmRuntime) value32(value uint32, call func(uint32) int32) error {
	ptr, err := r.alloc(4)
	if err != nil {
		return err
	}
	defer r.free(ptr, 4)

	buf, err := r.bytes(ptr, 4)
	if err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(buf, value)
	return resultError(call(ptr))
}

func wasmUint(value uint) (uint32, error) {
	if uint64(value) > uint64(^uint32(0)) {
		return 0, &Error{Result: ResultLimitExceeded}
	}

	return uint32(value), nil
}

func wasmLength(length int) (uint32, error) {
	if length < 0 || uint64(length) > uint64(^uint32(0)) {
		return 0, &Error{Result: ResultLimitExceeded}
	}

	return uint32(length), nil
}

func boolByte(value bool) byte {
	if value {
		return 1
	}

	return 0
}
