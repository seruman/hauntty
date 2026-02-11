package protocol

import (
	"bytes"
	"testing"
)

func TestEncoderDecoderU8(t *testing.T) {
	for _, v := range []uint8{0, 1, 127, 255} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteU8(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadU8()
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("u8 round-trip: got %d, want %d", got, v)
		}
	}
}

func TestEncoderDecoderU16(t *testing.T) {
	for _, v := range []uint16{0, 1, 0x00FF, 0xFF00, 0xFFFF} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteU16(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadU16()
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("u16 round-trip: got %d, want %d", got, v)
		}
	}
}

func TestEncoderDecoderU32(t *testing.T) {
	for _, v := range []uint32{0, 1, 0xDEADBEEF, 0xFFFFFFFF} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteU32(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadU32()
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("u32 round-trip: got %d, want %d", got, v)
		}
	}
}

func TestEncoderDecoderI32(t *testing.T) {
	for _, v := range []int32{0, 1, -1, 2147483647, -2147483648} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteI32(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadI32()
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("i32 round-trip: got %d, want %d", got, v)
		}
	}
}

func TestEncoderDecoderBool(t *testing.T) {
	for _, v := range []bool{true, false} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteBool(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadBool()
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("bool round-trip: got %v, want %v", got, v)
		}
	}
}

func TestEncoderDecoderString(t *testing.T) {
	for _, v := range []string{"", "hello", "hello world", "\x00binary\xff"} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteString(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadString()
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Errorf("string round-trip: got %q, want %q", got, v)
		}
	}
}

func TestEncoderDecoderBytes(t *testing.T) {
	for _, v := range [][]byte{{}, {0x00}, {0xDE, 0xAD, 0xBE, 0xEF}} {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).WriteBytes(v); err != nil {
			t.Fatal(err)
		}
		got, err := NewDecoder(&buf).ReadBytes()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, v) {
			t.Errorf("bytes round-trip: got %x, want %x", got, v)
		}
	}
}

func TestBigEndianEncoding(t *testing.T) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).WriteU16(0x0102); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if b[0] != 0x01 || b[1] != 0x02 {
		t.Errorf("u16 big-endian: got %x, want 0102", b)
	}

	buf.Reset()
	if err := NewEncoder(&buf).WriteU32(0x01020304); err != nil {
		t.Fatal(err)
	}
	b = buf.Bytes()
	if b[0] != 0x01 || b[1] != 0x02 || b[2] != 0x03 || b[3] != 0x04 {
		t.Errorf("u32 big-endian: got %x, want 01020304", b)
	}
}
