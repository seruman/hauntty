package libghostty

import (
	"bytes"
	"encoding/binary"
)

const (
	formatterOptionsSize = 40
	formatterExtraSize   = 24
	formatterScreenSize  = 12
	selectionSize        = 32
)

type FormatterFormat int

const (
	FormatterFormatPlain FormatterFormat = 0
	FormatterFormatVT    FormatterFormat = 1
	FormatterFormatHTML  FormatterFormat = 2
)

type formatterOpts struct {
	format             FormatterFormat
	unwrap             bool
	trim               bool
	selection          *Selection
	extraPalette       bool
	extraModes         bool
	extraScrolling     bool
	extraTabstops      bool
	extraPwd           bool
	extraKeyboard      bool
	extraCursor        bool
	extraStyle         bool
	extraHyperlink     bool
	extraProtection    bool
	extraKittyKeyboard bool
	extraCharsets      bool
}

type FormatterOption func(*formatterOpts)

func WithFormatterFormat(format FormatterFormat) FormatterOption {
	return func(opts *formatterOpts) { opts.format = format }
}

func WithFormatterUnwrap(unwrap bool) FormatterOption {
	return func(opts *formatterOpts) { opts.unwrap = unwrap }
}

func WithFormatterTrim(trim bool) FormatterOption {
	return func(opts *formatterOpts) { opts.trim = trim }
}

func WithFormatterSelection(selection *Selection) FormatterOption {
	return func(opts *formatterOpts) { opts.selection = selection }
}

func WithFormatterExtraPalette(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraPalette = value }
}

func WithFormatterExtraModes(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraModes = value }
}

func WithFormatterExtraScrollingRegion(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraScrolling = value }
}

func WithFormatterExtraTabstops(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraTabstops = value }
}

func WithFormatterExtraPwd(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraPwd = value }
}

func WithFormatterExtraKeyboard(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraKeyboard = value }
}

func WithFormatterExtraCursor(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraCursor = value }
}

func WithFormatterExtraStyle(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraStyle = value }
}

func WithFormatterExtraHyperlink(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraHyperlink = value }
}

func WithFormatterExtraProtection(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraProtection = value }
}

func WithFormatterExtraKittyKeyboard(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraKittyKeyboard = value }
}

func WithFormatterExtraCharsets(value bool) FormatterOption {
	return func(opts *formatterOpts) { opts.extraCharsets = value }
}

type Formatter struct {
	rt  *wasmRuntime
	ptr uint32
}

func NewFormatter(t *Terminal, options ...FormatterOption) (*Formatter, error) {
	opts := formatterOpts{}
	for _, option := range options {
		option(&opts)
	}

	t.rt.mu.Lock()
	defer t.rt.mu.Unlock()

	optionsPtr, err := t.rt.alloc(formatterOptionsSize)
	if err != nil {
		return nil, err
	}
	defer t.rt.free(optionsPtr, formatterOptionsSize)

	data, err := t.rt.bytes(optionsPtr, formatterOptionsSize)
	if err != nil {
		return nil, err
	}

	clear(data)
	binary.LittleEndian.PutUint32(data[0:], formatterOptionsSize)
	binary.LittleEndian.PutUint32(data[4:], uint32(opts.format))
	data[8] = boolByte(opts.unwrap)
	data[9] = boolByte(opts.trim)
	binary.LittleEndian.PutUint32(data[12:], formatterExtraSize)
	data[16] = boolByte(opts.extraPalette)
	data[17] = boolByte(opts.extraModes)
	data[18] = boolByte(opts.extraScrolling)
	data[19] = boolByte(opts.extraTabstops)
	data[20] = boolByte(opts.extraPwd)
	data[21] = boolByte(opts.extraKeyboard)
	binary.LittleEndian.PutUint32(data[24:], formatterScreenSize)
	data[28] = boolByte(opts.extraCursor)
	data[29] = boolByte(opts.extraStyle)
	data[30] = boolByte(opts.extraHyperlink)
	data[31] = boolByte(opts.extraProtection)
	data[32] = boolByte(opts.extraKittyKeyboard)
	data[33] = boolByte(opts.extraCharsets)

	var selectionPtr uint32
	if opts.selection != nil {
		selectionPtr, err = t.rt.alloc(selectionSize)
		if err != nil {
			return nil, err
		}
		defer t.rt.free(selectionPtr, selectionSize)

		selection, err := t.rt.bytes(selectionPtr, selectionSize)
		if err != nil {
			return nil, err
		}

		clear(selection)
		binary.LittleEndian.PutUint32(selection, selectionSize)
		copy(selection[4:], opts.selection.Start.data[:])
		copy(selection[16:], opts.selection.End.data[:])
		selection[28] = boolByte(opts.selection.Rectangle)

		data, err = t.rt.bytes(optionsPtr, formatterOptionsSize)
		if err != nil {
			return nil, err
		}

		binary.LittleEndian.PutUint32(data[36:], selectionPtr)
	}

	ptr, err := t.rt.opaque(func(slot uint32) int32 {
		return t.rt.mod.Xghostty_formatter_terminal_new(0, int32(slot), int32(t.ptr), int32(optionsPtr))
	})
	if err != nil {
		return nil, err
	}

	return &Formatter{rt: t.rt, ptr: ptr}, nil
}

func (f *Formatter) Close() {
	if f == nil || f.ptr == 0 {
		return
	}

	f.rt.mu.Lock()
	defer f.rt.mu.Unlock()

	f.rt.mod.Xghostty_formatter_free(int32(f.ptr))
	f.ptr = 0
}

func (f *Formatter) Format() ([]byte, error) {
	f.rt.mu.Lock()
	defer f.rt.mu.Unlock()

	outPtrSlot, err := f.rt.alloc(8)
	if err != nil {
		return nil, err
	}
	defer f.rt.free(outPtrSlot, 8)

	result := f.rt.mod.Xghostty_formatter_format_alloc(
		int32(f.ptr),
		0,
		int32(outPtrSlot),
		int32(outPtrSlot+4),
	)
	if err := resultError(result); err != nil {
		return nil, err
	}

	output, err := f.rt.bytes(outPtrSlot, 8)
	if err != nil {
		return nil, err
	}

	ptr := binary.LittleEndian.Uint32(output)
	length := binary.LittleEndian.Uint32(output[4:])
	defer f.rt.mod.Xghostty_free(0, int32(ptr), int32(length))

	data, err := f.rt.bytes(ptr, length)
	if err != nil {
		return nil, err
	}

	return bytes.Clone(data), nil
}

func (f *Formatter) FormatBuf(buf []byte) (int, error) {
	length, err := wasmLength(len(buf))
	if err != nil {
		return 0, err
	}

	f.rt.mu.Lock()
	defer f.rt.mu.Unlock()

	var ptr uint32
	if len(buf) > 0 {
		ptr, err = f.rt.alloc(length)
		if err != nil {
			return 0, err
		}
		defer f.rt.free(ptr, length)
	}

	writtenPtr, err := f.rt.alloc(4)
	if err != nil {
		return 0, err
	}
	defer f.rt.free(writtenPtr, 4)

	result := f.rt.mod.Xghostty_formatter_format_buf(
		int32(f.ptr),
		int32(ptr),
		int32(length),
		int32(writtenPtr),
	)
	writtenData, err := f.rt.bytes(writtenPtr, 4)
	if err != nil {
		return 0, err
	}

	written := binary.LittleEndian.Uint32(writtenData)
	if result == int32(ResultSuccess) && written > 0 {
		data, err := f.rt.bytes(ptr, written)
		if err != nil {
			return 0, err
		}

		copy(buf, data)
	}

	return int(written), resultError(result)
}

func (f *Formatter) FormatString() (string, error) {
	data, err := f.Format()

	return string(data), err
}
