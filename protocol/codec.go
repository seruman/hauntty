package protocol

import (
	"bytes"
	"fmt"
	"io"
)

const ProtocolVersion uint8 = 1

// Conn provides framed message reading and writing over an io.ReadWriter.
type Conn struct {
	rw io.ReadWriter
}

func NewConn(rw io.ReadWriter) *Conn {
	return &Conn{rw: rw}
}

// WriteMessage encodes a message with a length-prefixed frame:
// [u32 message_length][u8 message_type][payload...]
func (c *Conn) WriteMessage(msg Message) error {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	// Encode the payload (type byte + fields).
	if err := enc.WriteU8(msg.Type()); err != nil {
		return err
	}
	if err := msg.encode(enc); err != nil {
		return err
	}

	// Write length prefix + payload to the underlying writer.
	frame := buf.Bytes()
	lenEnc := NewEncoder(c.rw)
	if err := lenEnc.WriteU32(uint32(len(frame))); err != nil {
		return err
	}
	_, err := c.rw.Write(frame)
	return err
}

// ReadMessage reads a length-prefixed frame and decodes the message.
func (c *Conn) ReadMessage() (Message, error) {
	dec := NewDecoder(c.rw)

	length, err := dec.ReadU32()
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, fmt.Errorf("empty message frame")
	}

	// Read the entire frame into a buffer to prevent over-reading.
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

// Handshake performs the client side of the version handshake.
// It sends the proposed version and reads back the accepted version.
func (c *Conn) Handshake(version uint8) (uint8, error) {
	enc := NewEncoder(c.rw)
	if err := enc.WriteU8(version); err != nil {
		return 0, err
	}
	dec := NewDecoder(c.rw)
	return dec.ReadU8()
}

// AcceptHandshake performs the server side of the version handshake.
// It reads the client's proposed version and returns it. The caller
// should check the version and either write back an accepted version
// or close the connection.
func (c *Conn) AcceptHandshake() (uint8, error) {
	dec := NewDecoder(c.rw)
	return dec.ReadU8()
}

// AcceptVersion writes the accepted version back to the client.
func (c *Conn) AcceptVersion(version uint8) error {
	enc := NewEncoder(c.rw)
	return enc.WriteU8(version)
}

func newMessage(t uint8) (Message, error) {
	switch t {
	case TypeAttach:
		return &Attach{}, nil
	case TypeInput:
		return &Input{}, nil
	case TypeResize:
		return &Resize{}, nil
	case TypeDetach:
		return &Detach{}, nil
	case TypeList:
		return &List{}, nil
	case TypeKill:
		return &Kill{}, nil
	case TypeSend:
		return &Send{}, nil
	case TypeDump:
		return &Dump{}, nil
	case TypeOK:
		return &OK{}, nil
	case TypeError:
		return &Error{}, nil
	case TypeOutput:
		return &Output{}, nil
	case TypeState:
		return &State{}, nil
	case TypeSessions:
		return &Sessions{}, nil
	case TypeExited:
		return &Exited{}, nil
	case TypeDumpResponse:
		return &DumpResponse{}, nil
	default:
		return nil, fmt.Errorf("unknown message type: 0x%02x", t)
	}
}
