package protocol

import "fmt"

// MessageType identifies a protocol message on the wire.
type MessageType uint8

const (
	TypeAttach  MessageType = 0x01
	TypeInput   MessageType = 0x02
	TypeResize  MessageType = 0x03
	TypeDetach  MessageType = 0x04
	TypeList    MessageType = 0x05
	TypeKill    MessageType = 0x06
	TypeSend    MessageType = 0x07
	TypeDump    MessageType = 0x08
	TypePrune   MessageType = 0x09
	TypeSendKey MessageType = 0x0A
	TypeCreate  MessageType = 0x0B
	TypeStatus  MessageType = 0x0C
	TypeKick    MessageType = 0x0D

	TypeOK             MessageType = 0x80
	TypeError          MessageType = 0x81
	TypeOutput         MessageType = 0x82
	TypeAttached       MessageType = 0x83
	TypeSessions       MessageType = 0x84
	TypeExited         MessageType = 0x85
	TypeDumpResponse   MessageType = 0x86
	TypePruneResponse  MessageType = 0x87
	TypeClientsChanged MessageType = 0x88
	TypeStatusResponse MessageType = 0x89
	TypeCreated        MessageType = 0x8A
)

type Message interface {
	Type() MessageType
	encode(*Encoder) error
	decode(*Decoder) error
}

type SessionClient struct {
	ClientID string
	ReadOnly bool
	Version  string
}

type SessionState string

const (
	SessionStateRunning SessionState = "running"
	SessionStateDead    SessionState = "dead"
)

type Session struct {
	Name      string
	State     SessionState
	Cols      uint16
	Rows      uint16
	PID       uint32
	CreatedAt uint32
	SavedAt   uint32
	CWD       string
	Clients   []SessionClient
}

type DaemonStatus struct {
	PID          uint32
	Uptime       uint32
	SocketPath   string
	RunningCount uint32
	DeadCount    uint32
	Version      string
}

type SessionStatus struct {
	Name    string
	State   SessionState
	Cols    uint16
	Rows    uint16
	PID     uint32
	CWD     string
	Clients []SessionClient
}

func encodeSessionClients(e *Encoder, clients []SessionClient) error {
	if err := e.WriteU32(uint32(len(clients))); err != nil {
		return err
	}
	for i := range clients {
		c := &clients[i]
		if err := e.WriteString(c.ClientID); err != nil {
			return err
		}
		if err := e.WriteBool(c.ReadOnly); err != nil {
			return err
		}
		if err := e.WriteString(c.Version); err != nil {
			return err
		}
	}
	return nil
}

func decodeSessionClients(d *Decoder) ([]SessionClient, error) {
	count, err := d.ReadU32()
	if err != nil {
		return nil, err
	}
	if count > maxFrameSize {
		return nil, fmt.Errorf("client count %d exceeds maximum", count)
	}
	clients := make([]SessionClient, count)
	for i := range clients {
		c := &clients[i]
		if c.ClientID, err = d.ReadString(); err != nil {
			return nil, err
		}
		if c.ReadOnly, err = d.ReadBool(); err != nil {
			return nil, err
		}
		if c.Version, err = d.ReadString(); err != nil {
			return nil, err
		}
	}
	return clients, nil
}
