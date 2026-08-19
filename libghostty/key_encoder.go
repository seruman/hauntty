package libghostty

import "encoding/binary"

type KeyEncoder struct {
	rt  *wasmRuntime
	ptr uint32
}

type KittyKeyFlags uint8

const (
	KittyKeyDisabled         KittyKeyFlags = 0
	KittyKeyDisambiguate     KittyKeyFlags = 1
	KittyKeyReportEvents     KittyKeyFlags = 2
	KittyKeyReportAlternates KittyKeyFlags = 4
	KittyKeyReportAll        KittyKeyFlags = 8
	KittyKeyReportAssociated KittyKeyFlags = 16
	KittyKeyAll              KittyKeyFlags = 31
)

type OptionAsAlt int

const (
	OptionAsAltFalse OptionAsAlt = 0
	OptionAsAltTrue  OptionAsAlt = 1
	OptionAsAltLeft  OptionAsAlt = 2
	OptionAsAltRight OptionAsAlt = 3
)

type KeyEncoderOption int

const (
	KeyEncoderOptCursorKeyApplication    KeyEncoderOption = 0
	KeyEncoderOptKeypadKeyApplication    KeyEncoderOption = 1
	KeyEncoderOptIgnoreKeypadWithNumlock KeyEncoderOption = 2
	KeyEncoderOptAltEscPrefix            KeyEncoderOption = 3
	KeyEncoderOptModifyOtherKeysState2   KeyEncoderOption = 4
	KeyEncoderOptKittyFlags              KeyEncoderOption = 5
	KeyEncoderOptMacOSOptionAsAlt        KeyEncoderOption = 6
	KeyEncoderOptBackarrowKeyMode        KeyEncoderOption = 7
)

func NewKeyEncoder() (*KeyEncoder, error) {
	rt := sharedRuntime()
	rt.mu.Lock()
	defer rt.mu.Unlock()

	ptr, err := rt.opaque(func(slot uint32) int32 {
		return rt.mod.Xghostty_key_encoder_new(0, int32(slot))
	})
	if err != nil {
		return nil, err
	}

	return &KeyEncoder{rt: rt, ptr: ptr}, nil
}

func (enc *KeyEncoder) Close() {
	if enc == nil || enc.ptr == 0 {
		return
	}

	enc.rt.mu.Lock()
	defer enc.rt.mu.Unlock()

	enc.rt.mod.Xghostty_key_encoder_free(int32(enc.ptr))
	enc.ptr = 0
}

func (enc *KeyEncoder) SetOptBool(option KeyEncoderOption, value bool) {
	enc.rt.mu.Lock()
	defer enc.rt.mu.Unlock()

	ptr, err := enc.rt.alloc(1)
	if err != nil {
		panic(err)
	}
	defer enc.rt.free(ptr, 1)

	data, err := enc.rt.bytes(ptr, 1)
	if err != nil {
		panic(err)
	}

	data[0] = boolByte(value)
	enc.rt.mod.Xghostty_key_encoder_setopt(int32(enc.ptr), int32(option), int32(ptr))
}

func (enc *KeyEncoder) SetOptKittyFlags(flags KittyKeyFlags) {
	enc.rt.mu.Lock()
	defer enc.rt.mu.Unlock()

	ptr, err := enc.rt.alloc(1)
	if err != nil {
		panic(err)
	}
	defer enc.rt.free(ptr, 1)

	data, err := enc.rt.bytes(ptr, 1)
	if err != nil {
		panic(err)
	}

	data[0] = byte(flags)
	enc.rt.mod.Xghostty_key_encoder_setopt(int32(enc.ptr), int32(KeyEncoderOptKittyFlags), int32(ptr))
}

func (enc *KeyEncoder) SetOptOptionAsAlt(value OptionAsAlt) {
	enc.rt.mu.Lock()
	defer enc.rt.mu.Unlock()

	ptr, err := enc.rt.alloc(4)
	if err != nil {
		panic(err)
	}
	defer enc.rt.free(ptr, 4)

	data, err := enc.rt.bytes(ptr, 4)
	if err != nil {
		panic(err)
	}

	binary.LittleEndian.PutUint32(data, uint32(value))
	enc.rt.mod.Xghostty_key_encoder_setopt(int32(enc.ptr), int32(KeyEncoderOptMacOSOptionAsAlt), int32(ptr))
}

func (enc *KeyEncoder) SetOptFromTerminal(t *Terminal) {
	enc.rt.mu.Lock()
	defer enc.rt.mu.Unlock()

	enc.rt.mod.Xghostty_key_encoder_setopt_from_terminal(int32(enc.ptr), int32(t.ptr))
}

func (enc *KeyEncoder) Encode(event *KeyEvent) ([]byte, error) {
	enc.rt.mu.Lock()
	defer enc.rt.mu.Unlock()

	lengthPtr, err := enc.rt.alloc(4)
	if err != nil {
		return nil, err
	}
	defer enc.rt.free(lengthPtr, 4)

	bufferLen := uint32(128)
	bufferPtr, err := enc.rt.alloc(bufferLen)
	if err != nil {
		return nil, err
	}
	defer func() { enc.rt.free(bufferPtr, bufferLen) }()

	result := enc.rt.mod.Xghostty_key_encoder_encode(
		int32(enc.ptr),
		int32(event.ptr),
		int32(bufferPtr),
		int32(bufferLen),
		int32(lengthPtr),
	)
	lengthData, err := enc.rt.bytes(lengthPtr, 4)
	if err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(lengthData)
	if result == int32(ResultOutOfSpace) {
		enc.rt.free(bufferPtr, bufferLen)
		bufferLen = length
		bufferPtr, err = enc.rt.alloc(bufferLen)
		if err != nil {
			return nil, err
		}

		result = enc.rt.mod.Xghostty_key_encoder_encode(
			int32(enc.ptr),
			int32(event.ptr),
			int32(bufferPtr),
			int32(bufferLen),
			int32(lengthPtr),
		)
		lengthData, err = enc.rt.bytes(lengthPtr, 4)
		if err != nil {
			return nil, err
		}

		length = binary.LittleEndian.Uint32(lengthData)
	}

	if err := resultError(result); err != nil {
		return nil, err
	}

	data, err := enc.rt.bytes(bufferPtr, length)
	if err != nil {
		return nil, err
	}

	return append([]byte(nil), data...), nil
}
