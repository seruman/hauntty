package libghostty

import (
	"bytes"
	"encoding/binary"
)

const (
	snapshotDecoderOptionMaxContinuationBytes = 0
	snapshotDecoderOptionRetainContinuation   = 1
)

func (t *Terminal) Snapshot() ([]byte, error) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	outputSlot, err := t.rt.alloc(8)
	if err != nil {
		return nil, err
	}
	defer t.rt.free(outputSlot, 8)

	result := t.rt.mod.Xghostty_snapshot_encode_alloc(
		int32(t.ptr),
		0,
		int32(outputSlot),
		int32(outputSlot+4),
	)
	if err := resultError(result); err != nil {
		return nil, err
	}

	output, err := t.rt.bytes(outputSlot, 8)
	if err != nil {
		return nil, err
	}

	ptr := binary.LittleEndian.Uint32(output)
	length := binary.LittleEndian.Uint32(output[4:])
	defer t.rt.mod.Xghostty_free(0, int32(ptr), int32(length))

	data, err := t.rt.bytes(ptr, length)
	if err != nil {
		return nil, err
	}

	return bytes.Clone(data), nil
}

type SnapshotDecoder struct {
	rt        *wasmRuntime
	ptr       uint32
	sourcePtr uint32
	sourceLen uint32
}

func NewSnapshotDecoderBytes(data []byte) (*SnapshotDecoder, error) {
	return newSnapshotDecoderBytes(data)
}

func NewSnapshotDecoderBytesCopy(data []byte) (*SnapshotDecoder, error) {
	return newSnapshotDecoderBytes(data)
}

func newSnapshotDecoderBytes(data []byte) (*SnapshotDecoder, error) {
	length, err := wasmLength(len(data))
	if err != nil {
		return nil, err
	}

	rt := sharedRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	sourcePtr, err := rt.alloc(length)
	if err != nil {
		return nil, err
	}

	if err := rt.put(sourcePtr, data); err != nil {
		rt.free(sourcePtr, length)
		return nil, err
	}

	ptr, err := rt.opaque(func(slot uint32) int32 {
		return rt.mod.Xghostty_snapshot_decoder_new_buf(0, int32(slot), int32(sourcePtr), int32(length))
	})
	if err != nil {
		rt.free(sourcePtr, length)
		return nil, err
	}

	return &SnapshotDecoder{
		rt:        rt,
		ptr:       ptr,
		sourcePtr: sourcePtr,
		sourceLen: length,
	}, nil
}

func (d *SnapshotDecoder) Close() {
	if d == nil || d.rt == nil {
		return
	}

	d.rt.mu.Lock()
	defer d.rt.mu.Unlock()

	if d.ptr != 0 {
		d.rt.mod.Xghostty_snapshot_decoder_free(int32(d.ptr))
		d.ptr = 0
	}

	d.releaseSourceLocked()
}

func (d *SnapshotDecoder) SetMaxContinuationBytes(limit uint) error {
	value, err := wasmUint(limit)
	if err != nil {
		return err
	}

	d.rt.mu.Lock()
	defer d.rt.mu.Unlock()

	return d.rt.value32(value, func(ptr uint32) int32 {
		return d.rt.mod.Xghostty_snapshot_decoder_set(int32(d.ptr), snapshotDecoderOptionMaxContinuationBytes, int32(ptr))
	})
}

func (d *SnapshotDecoder) SetRetainContinuation(retain bool) error {
	d.rt.mu.Lock()
	defer d.rt.mu.Unlock()

	ptr, err := d.rt.alloc(1)
	if err != nil {
		return err
	}
	defer d.rt.free(ptr, 1)

	value, err := d.rt.bytes(ptr, 1)
	if err != nil {
		return err
	}

	value[0] = boolByte(retain)

	result := d.rt.mod.Xghostty_snapshot_decoder_set(
		int32(d.ptr),
		snapshotDecoderOptionRetainContinuation,
		int32(ptr),
	)

	return resultError(result)
}

func (d *SnapshotDecoder) Decode() (*Terminal, error) {
	d.rt.mu.Lock()
	defer d.rt.mu.Unlock()

	ptr, err := d.rt.opaque(func(slot uint32) int32 {
		return d.rt.mod.Xghostty_snapshot_decoder_decode(int32(d.ptr), int32(slot))
	})
	if err != nil {
		return nil, err
	}

	t, err := wrapTerminalLocked(d.rt, ptr)
	if err != nil {
		d.rt.mod.Xghostty_terminal_free(int32(ptr))
		return nil, err
	}

	d.releaseSourceLocked()
	return t, nil
}

func (d *SnapshotDecoder) releaseSourceLocked() {
	d.rt.free(d.sourcePtr, d.sourceLen)
	d.sourcePtr = 0
	d.sourceLen = 0
}
