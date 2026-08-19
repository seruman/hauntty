package libghostty

import "encoding/binary"

const (
	pointSize   = 24
	gridRefSize = 12
)

type GridRef struct {
	data [gridRefSize]byte
}

func (t *Terminal) GridRef(point Point) (*GridRef, error) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	pointPtr, err := t.rt.alloc(pointSize)
	if err != nil {
		return nil, err
	}
	defer t.rt.free(pointPtr, pointSize)

	pointData, err := t.rt.bytes(pointPtr, pointSize)
	if err != nil {
		return nil, err
	}

	clear(pointData)
	binary.LittleEndian.PutUint32(pointData, uint32(point.Tag))
	binary.LittleEndian.PutUint16(pointData[8:], point.X)
	binary.LittleEndian.PutUint32(pointData[12:], point.Y)

	refPtr, err := t.rt.alloc(gridRefSize)
	if err != nil {
		return nil, err
	}
	defer t.rt.free(refPtr, gridRefSize)

	refData, err := t.rt.bytes(refPtr, gridRefSize)
	if err != nil {
		return nil, err
	}

	clear(refData)
	binary.LittleEndian.PutUint32(refData, gridRefSize)

	result := t.rt.mod.Xghostty_terminal_grid_ref(
		int32(t.ptr),
		int32(pointPtr),
		int32(refPtr),
	)
	if err := resultError(result); err != nil {
		return nil, err
	}

	refData, err = t.rt.bytes(refPtr, gridRefSize)
	if err != nil {
		return nil, err
	}

	ref := &GridRef{}
	copy(ref.data[:], refData)

	return ref, nil
}
