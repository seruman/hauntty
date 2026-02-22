package protocol

import (
	"bytes"
	"fmt"
	"io"
)

const (
	ProtocolVersion uint8  = 6
	maxFrameSize    uint32 = 16 << 20 // 16MB
)

type Conn struct {
	rw io.ReadWriter
}

func NewConn(rw io.ReadWriter) *Conn {
	return &Conn{rw: rw}
}

// Frame: [u32 length][u8 type][payload...]
func (c *Conn) WriteMessage(msg Message) error {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	if err := enc.WriteU8(msg.Type()); err != nil {
		return err
	}
	if err := msg.encode(enc); err != nil {
		return err
	}

	frame := buf.Bytes()
	lenEnc := NewEncoder(c.rw)
	if err := lenEnc.WriteU32(uint32(len(frame))); err != nil {
		return err
	}
	_, err := c.rw.Write(frame)
	return err
}

func (c *Conn) ReadMessage() (Message, error) {
	dec := NewDecoder(c.rw)

	length, err := dec.ReadU32()
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, fmt.Errorf("empty message frame")
	}
	if length > maxFrameSize {
		return nil, fmt.Errorf("message frame too large: %d bytes", length)
	}

	// Read entire frame into buffer to prevent over-reading from the stream.
	frame := make([]byte, length)
	if _, err := io.ReadFull(c.rw, frame); err != nil {
		return nil, err
	}

	frameDec := NewDecoder(bytes.NewReader(frame))
	msgType, err := frameDec.ReadU8()
	if err != nil {
		return nil, err
	}

	msg, err := newMessage(msgType)
	if err != nil {
		return nil, err
	}
	if err := msg.decode(frameDec); err != nil {
		return nil, err
	}
	return msg, nil
}

func (c *Conn) Handshake(version uint8, revision string) (uint8, string, error) {
	enc := NewEncoder(c.rw)
	if err := enc.WriteU8(version); err != nil {
		return 0, "", err
	}
	if err := enc.WriteString(revision); err != nil {
		return 0, "", err
	}
	dec := NewDecoder(c.rw)
	serverVer, err := dec.ReadU8()
	if err != nil {
		return 0, "", err
	}
	serverRev, err := dec.ReadString()
	if err != nil {
		return 0, "", err
	}
	return serverVer, serverRev, nil
}

// Caller must check the version and call AcceptVersion or close.
func (c *Conn) AcceptHandshake() (uint8, string, error) {
	dec := NewDecoder(c.rw)
	version, err := dec.ReadU8()
	if err != nil {
		return 0, "", err
	}
	revision, err := dec.ReadString()
	if err != nil {
		return 0, "", err
	}
	return version, revision, nil
}

func (c *Conn) AcceptVersion(version uint8, revision string) error {
	enc := NewEncoder(c.rw)
	if err := enc.WriteU8(version); err != nil {
		return err
	}
	return enc.WriteString(revision)
}
