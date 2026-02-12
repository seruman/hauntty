package protocol

import (
	"bytes"
	"testing"

	"gotest.tools/v3/assert"
)

func TestEncoderDecoderU8(t *testing.T) {
	for _, v := range []uint8{0, 1, 127, 255} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteU8(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadU8()
		assert.NilError(t, err)
		assert.Equal(t, got, v)
	}
}

func TestEncoderDecoderU16(t *testing.T) {
	for _, v := range []uint16{0, 1, 0x00FF, 0xFF00, 0xFFFF} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteU16(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadU16()
		assert.NilError(t, err)
		assert.Equal(t, got, v)
	}
}

func TestEncoderDecoderU32(t *testing.T) {
	for _, v := range []uint32{0, 1, 0xDEADBEEF, 0xFFFFFFFF} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteU32(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadU32()
		assert.NilError(t, err)
		assert.Equal(t, got, v)
	}
}

func TestEncoderDecoderI32(t *testing.T) {
	for _, v := range []int32{0, 1, -1, 2147483647, -2147483648} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteI32(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadI32()
		assert.NilError(t, err)
		assert.Equal(t, got, v)
	}
}

func TestEncoderDecoderBool(t *testing.T) {
	for _, v := range []bool{true, false} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteBool(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadBool()
		assert.NilError(t, err)
		assert.Equal(t, got, v)
	}
}

func TestEncoderDecoderString(t *testing.T) {
	for _, v := range []string{"", "hello", "hello world", "\x00binary\xff"} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteString(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadString()
		assert.NilError(t, err)
		assert.Equal(t, got, v)
	}
}

func TestEncoderDecoderBytes(t *testing.T) {
	for _, v := range [][]byte{{}, {0x00}, {0xDE, 0xAD, 0xBE, 0xEF}} {
		var buf bytes.Buffer
		err := NewEncoder(&buf).WriteBytes(v)
		assert.NilError(t, err)
		got, err := NewDecoder(&buf).ReadBytes()
		assert.NilError(t, err)
		assert.DeepEqual(t, got, v)
	}
}

func TestBigEndianEncoding(t *testing.T) {
	var buf bytes.Buffer
	err := NewEncoder(&buf).WriteU16(0x0102)
	assert.NilError(t, err)
	assert.DeepEqual(t, buf.Bytes(), []byte{0x01, 0x02})

	buf.Reset()
	err = NewEncoder(&buf).WriteU32(0x01020304)
	assert.NilError(t, err)
	assert.DeepEqual(t, buf.Bytes(), []byte{0x01, 0x02, 0x03, 0x04})
}
