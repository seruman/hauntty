package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Encoder writes binary-encoded fields to an io.Writer.
type Encoder struct {
	w   io.Writer
	buf [8]byte
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) WriteU8(v uint8) error {
	e.buf[0] = v
	_, err := e.w.Write(e.buf[:1])
	return err
}

func (e *Encoder) WriteU16(v uint16) error {
	binary.BigEndian.PutUint16(e.buf[:2], v)
	_, err := e.w.Write(e.buf[:2])
	return err
}

func (e *Encoder) WriteU32(v uint32) error {
	binary.BigEndian.PutUint32(e.buf[:4], v)
	_, err := e.w.Write(e.buf[:4])
	return err
}

func (e *Encoder) WriteI32(v int32) error {
	binary.BigEndian.PutUint32(e.buf[:4], uint32(v))
	_, err := e.w.Write(e.buf[:4])
	return err
}

func (e *Encoder) WriteBool(v bool) error {
	if v {
		e.buf[0] = 0x01
	} else {
		e.buf[0] = 0x00
	}
	_, err := e.w.Write(e.buf[:1])
	return err
}

func (e *Encoder) WriteString(v string) error {
	if len(v) > 0xFFFF {
		return fmt.Errorf("string too long: %d bytes", len(v))
	}
	if err := e.WriteU16(uint16(len(v))); err != nil {
		return err
	}
	_, err := io.WriteString(e.w, v)
	return err
}

func (e *Encoder) WriteBytes(v []byte) error {
	if err := e.WriteU32(uint32(len(v))); err != nil {
		return err
	}
	_, err := e.w.Write(v)
	return err
}

// Decoder reads binary-encoded fields from an io.Reader.
type Decoder struct {
	r   io.Reader
	buf [8]byte
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

func (d *Decoder) ReadU8() (uint8, error) {
	if _, err := io.ReadFull(d.r, d.buf[:1]); err != nil {
		return 0, err
	}
	return d.buf[0], nil
}

func (d *Decoder) ReadU16() (uint16, error) {
	if _, err := io.ReadFull(d.r, d.buf[:2]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(d.buf[:2]), nil
}

func (d *Decoder) ReadU32() (uint32, error) {
	if _, err := io.ReadFull(d.r, d.buf[:4]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(d.buf[:4]), nil
}

func (d *Decoder) ReadI32() (int32, error) {
	if _, err := io.ReadFull(d.r, d.buf[:4]); err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(d.buf[:4])), nil
}

func (d *Decoder) ReadBool() (bool, error) {
	if _, err := io.ReadFull(d.r, d.buf[:1]); err != nil {
		return false, err
	}
	return d.buf[0] != 0, nil
}

func (d *Decoder) ReadString() (string, error) {
	n, err := d.ReadU16()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(d.r, b); err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *Decoder) ReadBytes() ([]byte, error) {
	n, err := d.ReadU32()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return []byte{}, nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(d.r, b); err != nil {
		return nil, err
	}
	return b, nil
}
