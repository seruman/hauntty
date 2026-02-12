package protocol

// Message type constants.
const (
	// Client → Daemon
	TypeAttach uint8 = 0x01
	TypeInput  uint8 = 0x02
	TypeResize uint8 = 0x03
	TypeDetach uint8 = 0x04
	TypeList   uint8 = 0x05
	TypeKill   uint8 = 0x06
	TypeSend   uint8 = 0x07
	TypeDump   uint8 = 0x08

	// Daemon → Client
	TypeOK           uint8 = 0x80
	TypeError        uint8 = 0x81
	TypeOutput       uint8 = 0x82
	TypeState        uint8 = 0x83
	TypeSessions     uint8 = 0x84
	TypeExited       uint8 = 0x85
	TypeDumpResponse uint8 = 0x86
)

// Message is the interface implemented by all protocol messages.
type Message interface {
	Type() uint8
	encode(*Encoder) error
	decode(*Decoder) error
}

// Session is an element in the SESSIONS message array.
type Session struct {
	Name      string
	State     string
	Cols      uint16
	Rows      uint16
	PID       uint32
	CreatedAt uint32
}

// --- Client → Daemon messages ---

type Attach struct {
	Name            string
	Cols            uint16
	Rows            uint16
	Command         string
	Env             []string
	ScrollbackLines uint32
}

func (m *Attach) Type() uint8 { return TypeAttach }

func (m *Attach) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	if err := e.WriteU16(m.Rows); err != nil {
		return err
	}
	if err := e.WriteString(m.Command); err != nil {
		return err
	}
	if err := e.WriteU32(uint32(len(m.Env))); err != nil {
		return err
	}
	for _, s := range m.Env {
		if err := e.WriteString(s); err != nil {
			return err
		}
	}
	return e.WriteU32(m.ScrollbackLines)
}

func (m *Attach) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Rows, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Command, err = d.ReadString(); err != nil {
		return err
	}
	count, err := d.ReadU32()
	if err != nil {
		return err
	}
	m.Env = make([]string, count)
	for i := range m.Env {
		if m.Env[i], err = d.ReadString(); err != nil {
			return err
		}
	}
	m.ScrollbackLines, err = d.ReadU32()
	return err
}

type Input struct {
	Data []byte
}

func (m *Input) Type() uint8 { return TypeInput }

func (m *Input) encode(e *Encoder) error {
	return e.WriteBytes(m.Data)
}

func (m *Input) decode(d *Decoder) error {
	var err error
	m.Data, err = d.ReadBytes()
	return err
}

type Resize struct {
	Cols uint16
	Rows uint16
}

func (m *Resize) Type() uint8 { return TypeResize }

func (m *Resize) encode(e *Encoder) error {
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	return e.WriteU16(m.Rows)
}

func (m *Resize) decode(d *Decoder) error {
	var err error
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	m.Rows, err = d.ReadU16()
	return err
}

type Detach struct {
	Name string // Empty = detach self, non-empty = detach named session.
}

func (m *Detach) Type() uint8 { return TypeDetach }

func (m *Detach) encode(e *Encoder) error {
	return e.WriteString(m.Name)
}

func (m *Detach) decode(d *Decoder) error {
	var err error
	m.Name, err = d.ReadString()
	return err
}

type List struct{}

func (m *List) Type() uint8             { return TypeList }
func (m *List) encode(_ *Encoder) error { return nil }
func (m *List) decode(_ *Decoder) error { return nil }

type Kill struct {
	Name string
}

func (m *Kill) Type() uint8 { return TypeKill }

func (m *Kill) encode(e *Encoder) error {
	return e.WriteString(m.Name)
}

func (m *Kill) decode(d *Decoder) error {
	var err error
	m.Name, err = d.ReadString()
	return err
}

type Send struct {
	Name string
	Data []byte
}

func (m *Send) Type() uint8 { return TypeSend }

func (m *Send) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	return e.WriteBytes(m.Data)
}

func (m *Send) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	m.Data, err = d.ReadBytes()
	return err
}

type Dump struct {
	Name   string
	Format uint8
}

func (m *Dump) Type() uint8 { return TypeDump }

func (m *Dump) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	return e.WriteU8(m.Format)
}

func (m *Dump) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	m.Format, err = d.ReadU8()
	return err
}

// --- Daemon → Client messages ---

type OK struct {
	SessionName string
	Cols        uint16
	Rows        uint16
	PID         uint32
	Created     bool
}

func (m *OK) Type() uint8 { return TypeOK }

func (m *OK) encode(e *Encoder) error {
	if err := e.WriteString(m.SessionName); err != nil {
		return err
	}
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	if err := e.WriteU16(m.Rows); err != nil {
		return err
	}
	if err := e.WriteU32(m.PID); err != nil {
		return err
	}
	return e.WriteBool(m.Created)
}

func (m *OK) decode(d *Decoder) error {
	var err error
	if m.SessionName, err = d.ReadString(); err != nil {
		return err
	}
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Rows, err = d.ReadU16(); err != nil {
		return err
	}
	if m.PID, err = d.ReadU32(); err != nil {
		return err
	}
	m.Created, err = d.ReadBool()
	return err
}

type Error struct {
	Code    uint16
	Message string
}

func (m *Error) Type() uint8 { return TypeError }

func (m *Error) encode(e *Encoder) error {
	if err := e.WriteU16(m.Code); err != nil {
		return err
	}
	return e.WriteString(m.Message)
}

func (m *Error) decode(d *Decoder) error {
	var err error
	if m.Code, err = d.ReadU16(); err != nil {
		return err
	}
	m.Message, err = d.ReadString()
	return err
}

type Output struct {
	Data []byte
}

func (m *Output) Type() uint8 { return TypeOutput }

func (m *Output) encode(e *Encoder) error {
	return e.WriteBytes(m.Data)
}

func (m *Output) decode(d *Decoder) error {
	var err error
	m.Data, err = d.ReadBytes()
	return err
}

type State struct {
	ScreenDump        []byte
	CursorRow         uint32
	CursorCol         uint32
	IsAlternateScreen bool
}

func (m *State) Type() uint8 { return TypeState }

func (m *State) encode(e *Encoder) error {
	if err := e.WriteBytes(m.ScreenDump); err != nil {
		return err
	}
	if err := e.WriteU32(m.CursorRow); err != nil {
		return err
	}
	if err := e.WriteU32(m.CursorCol); err != nil {
		return err
	}
	return e.WriteBool(m.IsAlternateScreen)
}

func (m *State) decode(d *Decoder) error {
	var err error
	if m.ScreenDump, err = d.ReadBytes(); err != nil {
		return err
	}
	if m.CursorRow, err = d.ReadU32(); err != nil {
		return err
	}
	if m.CursorCol, err = d.ReadU32(); err != nil {
		return err
	}
	m.IsAlternateScreen, err = d.ReadBool()
	return err
}

type Sessions struct {
	Sessions []Session
}

func (m *Sessions) Type() uint8 { return TypeSessions }

func (m *Sessions) encode(e *Encoder) error {
	if err := e.WriteU32(uint32(len(m.Sessions))); err != nil {
		return err
	}
	for i := range m.Sessions {
		s := &m.Sessions[i]
		if err := e.WriteString(s.Name); err != nil {
			return err
		}
		if err := e.WriteString(s.State); err != nil {
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
	}
	return nil
}

func (m *Sessions) decode(d *Decoder) error {
	count, err := d.ReadU32()
	if err != nil {
		return err
	}
	m.Sessions = make([]Session, count)
	for i := range m.Sessions {
		s := &m.Sessions[i]
		if s.Name, err = d.ReadString(); err != nil {
			return err
		}
		if s.State, err = d.ReadString(); err != nil {
			return err
		}
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
	}
	return nil
}

type Exited struct {
	ExitCode int32
}

func (m *Exited) Type() uint8 { return TypeExited }

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

func (m *DumpResponse) Type() uint8 { return TypeDumpResponse }

func (m *DumpResponse) encode(e *Encoder) error {
	return e.WriteBytes(m.Data)
}

func (m *DumpResponse) decode(d *Decoder) error {
	var err error
	m.Data, err = d.ReadBytes()
	return err
}
