package protocol

import "fmt"

type OK struct{}

func (m *OK) Type() MessageType       { return TypeOK }
func (m *OK) encode(_ *Encoder) error { return nil }
func (m *OK) decode(_ *Decoder) error { return nil }

type Error struct {
	Message string
}

func (m *Error) Type() MessageType { return TypeError }

func (m *Error) encode(e *Encoder) error {
	return e.WriteString(m.Message)
}

func (m *Error) decode(d *Decoder) error {
	var err error
	m.Message, err = d.ReadString()
	return err
}

type Output struct {
	Data []byte
}

func (m *Output) Type() MessageType { return TypeOutput }

func (m *Output) encode(e *Encoder) error {
	return e.WriteBytes(m.Data)
}

func (m *Output) decode(d *Decoder) error {
	var err error
	m.Data, err = d.ReadBytes()
	return err
}

type Created struct {
	Name string
	PID  uint32
}

func (m *Created) Type() MessageType { return TypeCreated }

func (m *Created) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	return e.WriteU32(m.PID)
}

func (m *Created) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	m.PID, err = d.ReadU32()
	return err
}

type Attached struct {
	Name       string
	PID        uint32
	ClientID   string
	Cols       uint16
	Rows       uint16
	ScreenDump []byte
	CursorRow  uint32
	CursorCol  uint32
	AltScreen  bool
	Created    bool
}

func (m *Attached) Type() MessageType { return TypeAttached }

func (m *Attached) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	if err := e.WriteU32(m.PID); err != nil {
		return err
	}
	if err := e.WriteString(m.ClientID); err != nil {
		return err
	}
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	if err := e.WriteU16(m.Rows); err != nil {
		return err
	}
	if err := e.WriteBytes(m.ScreenDump); err != nil {
		return err
	}
	if err := e.WriteU32(m.CursorRow); err != nil {
		return err
	}
	if err := e.WriteU32(m.CursorCol); err != nil {
		return err
	}
	if err := e.WriteBool(m.AltScreen); err != nil {
		return err
	}
	return e.WriteBool(m.Created)
}

func (m *Attached) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	if m.PID, err = d.ReadU32(); err != nil {
		return err
	}
	if m.ClientID, err = d.ReadString(); err != nil {
		return err
	}
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Rows, err = d.ReadU16(); err != nil {
		return err
	}
	if m.ScreenDump, err = d.ReadBytes(); err != nil {
		return err
	}
	if m.CursorRow, err = d.ReadU32(); err != nil {
		return err
	}
	if m.CursorCol, err = d.ReadU32(); err != nil {
		return err
	}
	if m.AltScreen, err = d.ReadBool(); err != nil {
		return err
	}
	m.Created, err = d.ReadBool()
	return err
}

type Sessions struct {
	Sessions []Session
}

func (m *Sessions) Type() MessageType { return TypeSessions }

func (m *Sessions) encode(e *Encoder) error {
	if err := e.WriteU32(uint32(len(m.Sessions))); err != nil {
		return err
	}
	for i := range m.Sessions {
		s := &m.Sessions[i]
		if err := e.WriteString(s.Name); err != nil {
			return err
		}
		if err := e.WriteString(string(s.State)); err != nil {
			return err
		}
		if err := e.WriteU16(s.Cols); err != nil {
			return err
		}
		if err := e.WriteU16(s.Rows); err != nil {
			return err
		}
		if err := e.WriteU32(s.PID); err != nil {
			return err
		}
		if err := e.WriteU32(s.CreatedAt); err != nil {
			return err
		}
		if err := e.WriteU32(s.SavedAt); err != nil {
			return err
		}
		if err := e.WriteString(s.CWD); err != nil {
			return err
		}
		if err := encodeSessionClients(e, s.Clients); err != nil {
			return err
		}
	}
	return nil
}

func (m *Sessions) decode(d *Decoder) error {
	count, err := d.ReadU32()
	if err != nil {
		return err
	}
	if count > maxFrameSize {
		return fmt.Errorf("session count %d exceeds maximum", count)
	}
	m.Sessions = make([]Session, count)
	for i := range m.Sessions {
		s := &m.Sessions[i]
		if s.Name, err = d.ReadString(); err != nil {
			return err
		}
		state, err := d.ReadString()
		if err != nil {
			return err
		}
		s.State = SessionState(state)
		if s.Cols, err = d.ReadU16(); err != nil {
			return err
		}
		if s.Rows, err = d.ReadU16(); err != nil {
			return err
		}
		if s.PID, err = d.ReadU32(); err != nil {
			return err
		}
		if s.CreatedAt, err = d.ReadU32(); err != nil {
			return err
		}
		if s.SavedAt, err = d.ReadU32(); err != nil {
			return err
		}
		if s.CWD, err = d.ReadString(); err != nil {
			return err
		}
		if s.Clients, err = decodeSessionClients(d); err != nil {
			return err
		}
	}
	return nil
}

type Exited struct {
	ExitCode int32
}

func (m *Exited) Type() MessageType { return TypeExited }

func (m *Exited) encode(e *Encoder) error {
	return e.WriteI32(m.ExitCode)
}

func (m *Exited) decode(d *Decoder) error {
	var err error
	m.ExitCode, err = d.ReadI32()
	return err
}

type DumpResponse struct {
	Data []byte
}

func (m *DumpResponse) Type() MessageType { return TypeDumpResponse }

func (m *DumpResponse) encode(e *Encoder) error {
	return e.WriteBytes(m.Data)
}

func (m *DumpResponse) decode(d *Decoder) error {
	var err error
	m.Data, err = d.ReadBytes()
	return err
}

type PruneResponse struct {
	Count uint32
}

func (m *PruneResponse) Type() MessageType { return TypePruneResponse }

func (m *PruneResponse) encode(e *Encoder) error {
	return e.WriteU32(m.Count)
}

func (m *PruneResponse) decode(d *Decoder) error {
	var err error
	m.Count, err = d.ReadU32()
	return err
}

type ClientsChanged struct {
	Count uint16
	Cols  uint16
	Rows  uint16
}

func (m *ClientsChanged) Type() MessageType { return TypeClientsChanged }

func (m *ClientsChanged) encode(e *Encoder) error {
	if err := e.WriteU16(m.Count); err != nil {
		return err
	}
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	return e.WriteU16(m.Rows)
}

func (m *ClientsChanged) decode(d *Decoder) error {
	var err error
	if m.Count, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	m.Rows, err = d.ReadU16()
	return err
}

type StatusResponse struct {
	Daemon  DaemonStatus
	Session *SessionStatus
}

func (m *StatusResponse) Type() MessageType { return TypeStatusResponse }

func (m *StatusResponse) encode(e *Encoder) error {
	if err := e.WriteU32(m.Daemon.PID); err != nil {
		return err
	}
	if err := e.WriteU32(m.Daemon.Uptime); err != nil {
		return err
	}
	if err := e.WriteString(m.Daemon.SocketPath); err != nil {
		return err
	}
	if err := e.WriteU32(m.Daemon.RunningCount); err != nil {
		return err
	}
	if err := e.WriteU32(m.Daemon.DeadCount); err != nil {
		return err
	}
	if err := e.WriteString(m.Daemon.Version); err != nil {
		return err
	}
	if m.Session == nil {
		return e.WriteU8(0)
	}
	if err := e.WriteU8(1); err != nil {
		return err
	}
	if err := e.WriteString(m.Session.Name); err != nil {
		return err
	}
	if err := e.WriteString(string(m.Session.State)); err != nil {
		return err
	}
	if err := e.WriteU16(m.Session.Cols); err != nil {
		return err
	}
	if err := e.WriteU16(m.Session.Rows); err != nil {
		return err
	}
	if err := e.WriteU32(m.Session.PID); err != nil {
		return err
	}
	if err := e.WriteString(m.Session.CWD); err != nil {
		return err
	}
	return encodeSessionClients(e, m.Session.Clients)
}

func (m *StatusResponse) decode(d *Decoder) error {
	var err error
	if m.Daemon.PID, err = d.ReadU32(); err != nil {
		return err
	}
	if m.Daemon.Uptime, err = d.ReadU32(); err != nil {
		return err
	}
	if m.Daemon.SocketPath, err = d.ReadString(); err != nil {
		return err
	}
	if m.Daemon.RunningCount, err = d.ReadU32(); err != nil {
		return err
	}
	if m.Daemon.DeadCount, err = d.ReadU32(); err != nil {
		return err
	}
	if m.Daemon.Version, err = d.ReadString(); err != nil {
		return err
	}
	flag, err := d.ReadU8()
	if err != nil {
		return err
	}
	if flag == 0 {
		return nil
	}
	m.Session = &SessionStatus{}
	if m.Session.Name, err = d.ReadString(); err != nil {
		return err
	}
	state, err := d.ReadString()
	if err != nil {
		return err
	}
	m.Session.State = SessionState(state)
	if m.Session.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Session.Rows, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Session.PID, err = d.ReadU32(); err != nil {
		return err
	}
	if m.Session.CWD, err = d.ReadString(); err != nil {
		return err
	}
	m.Session.Clients, err = decodeSessionClients(d)
	return err
}
