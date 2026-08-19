package libghostty

import "encoding/binary"

const (
	terminalDataCols         = 1
	terminalDataRows         = 2
	terminalDataCursorX      = 3
	terminalDataCursorY      = 4
	terminalDataActiveScreen = 6
	terminalDataPwd          = 13
	terminalDataVTGround     = 38
)

type TerminalScreen int

const (
	ScreenPrimary   TerminalScreen = 0
	ScreenAlternate TerminalScreen = 1
)

func (t *Terminal) Cols() (uint16, error) {
	return t.getUint16(terminalDataCols)
}

func (t *Terminal) Rows() (uint16, error) {
	return t.getUint16(terminalDataRows)
}

func (t *Terminal) CursorX() (uint16, error) {
	return t.getUint16(terminalDataCursorX)
}

func (t *Terminal) CursorY() (uint16, error) {
	return t.getUint16(terminalDataCursorY)
}

func (t *Terminal) ActiveScreen() (TerminalScreen, error) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	value, err := t.getUint32Locked(terminalDataActiveScreen)

	return TerminalScreen(value), err
}

func (t *Terminal) Pwd() (string, error) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	ptr, err := t.rt.alloc(8)
	if err != nil {
		return "", err
	}
	defer t.rt.free(ptr, 8)

	result := t.rt.mod.Xghostty_terminal_get(int32(t.ptr), terminalDataPwd, int32(ptr))
	if err := resultError(result); err != nil {
		if ghosttyErr, ok := err.(*Error); ok && ghosttyErr.Result == ResultNoValue {
			return "", nil
		}

		return "", err
	}

	value, err := t.rt.bytes(ptr, 8)
	if err != nil {
		return "", err
	}

	strPtr := binary.LittleEndian.Uint32(value)
	strLen := binary.LittleEndian.Uint32(value[4:])
	data, err := t.rt.bytes(strPtr, strLen)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (t *Terminal) VTGround() (bool, error) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	ptr, err := t.rt.alloc(1)
	if err != nil {
		return false, err
	}
	defer t.rt.free(ptr, 1)

	result := t.rt.mod.Xghostty_terminal_get(int32(t.ptr), terminalDataVTGround, int32(ptr))
	if err := resultError(result); err != nil {
		return false, err
	}

	value, err := t.rt.bytes(ptr, 1)
	if err != nil {
		return false, err
	}

	return value[0] != 0, nil
}

func (t *Terminal) getUint16(data int32) (uint16, error) {
	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	ptr, err := t.rt.alloc(2)
	if err != nil {
		return 0, err
	}
	defer t.rt.free(ptr, 2)

	result := t.rt.mod.Xghostty_terminal_get(int32(t.ptr), data, int32(ptr))
	if err := resultError(result); err != nil {
		return 0, err
	}

	value, err := t.rt.bytes(ptr, 2)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint16(value), nil
}

func (t *Terminal) getUint32Locked(data int32) (uint32, error) {
	ptr, err := t.rt.alloc(4)
	if err != nil {
		return 0, err
	}
	defer t.rt.free(ptr, 4)

	result := t.rt.mod.Xghostty_terminal_get(int32(t.ptr), data, int32(ptr))
	if err := resultError(result); err != nil {
		return 0, err
	}

	value, err := t.rt.bytes(ptr, 4)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(value), nil
}
