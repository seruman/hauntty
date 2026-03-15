package protocol

type Create struct {
	Name       string
	Command    []string
	Env        []string
	CWD        string
	Scrollback uint32
	Force      bool
}

func (m *Create) Type() MessageType { return TypeCreate }

func (m *Create) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	if err := e.WriteStringSlice(m.Command); err != nil {
		return err
	}
	if err := e.WriteStringSlice(m.Env); err != nil {
		return err
	}
	if err := e.WriteString(m.CWD); err != nil {
		return err
	}
	if err := e.WriteU32(m.Scrollback); err != nil {
		return err
	}
	return e.WriteBool(m.Force)
}

func (m *Create) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	if m.Command, err = d.ReadStringSlice(); err != nil {
		return err
	}
	if m.Env, err = d.ReadStringSlice(); err != nil {
		return err
	}
	if m.CWD, err = d.ReadString(); err != nil {
		return err
	}
	if m.Scrollback, err = d.ReadU32(); err != nil {
		return err
	}
	m.Force, err = d.ReadBool()
	return err
}

type Attach struct {
	Name       string
	Command    []string
	Env        []string
	CWD        string
	Cols       uint16
	Rows       uint16
	Xpixel     uint16
	Ypixel     uint16
	ReadOnly   bool
	Restore    bool
	Scrollback uint32
}

func (m *Attach) Type() MessageType { return TypeAttach }

func (m *Attach) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	if err := e.WriteStringSlice(m.Command); err != nil {
		return err
	}
	if err := e.WriteStringSlice(m.Env); err != nil {
		return err
	}
	if err := e.WriteString(m.CWD); err != nil {
		return err
	}
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	if err := e.WriteU16(m.Rows); err != nil {
		return err
	}
	if err := e.WriteU16(m.Xpixel); err != nil {
		return err
	}
	if err := e.WriteU16(m.Ypixel); err != nil {
		return err
	}
	if err := e.WriteBool(m.ReadOnly); err != nil {
		return err
	}
	if err := e.WriteBool(m.Restore); err != nil {
		return err
	}
	return e.WriteU32(m.Scrollback)
}

func (m *Attach) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	if m.Command, err = d.ReadStringSlice(); err != nil {
		return err
	}
	if m.Env, err = d.ReadStringSlice(); err != nil {
		return err
	}
	if m.CWD, err = d.ReadString(); err != nil {
		return err
	}
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Rows, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Xpixel, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Ypixel, err = d.ReadU16(); err != nil {
		return err
	}
	if m.ReadOnly, err = d.ReadBool(); err != nil {
		return err
	}
	if m.Restore, err = d.ReadBool(); err != nil {
		return err
	}
	m.Scrollback, err = d.ReadU32()
	return err
}

type Input struct {
	Data []byte
}

func (m *Input) Type() MessageType { return TypeInput }

func (m *Input) encode(e *Encoder) error {
	return e.WriteBytes(m.Data)
}

func (m *Input) decode(d *Decoder) error {
	var err error
	m.Data, err = d.ReadBytes()
	return err
}

type Resize struct {
	Cols   uint16
	Rows   uint16
	Xpixel uint16
	Ypixel uint16
}

func (m *Resize) Type() MessageType { return TypeResize }

func (m *Resize) encode(e *Encoder) error {
	if err := e.WriteU16(m.Cols); err != nil {
		return err
	}
	if err := e.WriteU16(m.Rows); err != nil {
		return err
	}
	if err := e.WriteU16(m.Xpixel); err != nil {
		return err
	}
	return e.WriteU16(m.Ypixel)
}

func (m *Resize) decode(d *Decoder) error {
	var err error
	if m.Cols, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Rows, err = d.ReadU16(); err != nil {
		return err
	}
	if m.Xpixel, err = d.ReadU16(); err != nil {
		return err
	}
	m.Ypixel, err = d.ReadU16()
	return err
}

type Detach struct{}

func (m *Detach) Type() MessageType       { return TypeDetach }
func (m *Detach) encode(_ *Encoder) error { return nil }
func (m *Detach) decode(_ *Decoder) error { return nil }

type List struct {
	IncludeClients bool
}

func (m *List) Type() MessageType { return TypeList }

func (m *List) encode(e *Encoder) error {
	return e.WriteBool(m.IncludeClients)
}

func (m *List) decode(d *Decoder) error {
	var err error
	m.IncludeClients, err = d.ReadBool()
	return err
}

type Kill struct {
	Name string
}

func (m *Kill) Type() MessageType { return TypeKill }

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

func (m *Send) Type() MessageType { return TypeSend }

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

// KeyCode identifies a key on the keyboard.
type KeyCode uint32

// KeyMods is a bitmask of keyboard modifier keys (shift, ctrl, alt, etc).
type KeyMods uint32

type SendKey struct {
	Name string
	Key  KeyCode
	Mods KeyMods
}

func (m *SendKey) Type() MessageType { return TypeSendKey }

func (m *SendKey) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	if err := e.WriteU32(uint32(m.Key)); err != nil {
		return err
	}
	return e.WriteU32(uint32(m.Mods))
}

func (m *SendKey) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	var key, mods uint32
	if key, err = d.ReadU32(); err != nil {
		return err
	}
	if mods, err = d.ReadU32(); err != nil {
		return err
	}
	m.Key = KeyCode(key)
	m.Mods = KeyMods(mods)
	return err
}

type Dump struct {
	Name   string
	Format DumpFormat
}

func (m *Dump) Type() MessageType { return TypeDump }

func (m *Dump) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	return e.WriteU8(uint8(m.Format))
}

func (m *Dump) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	var raw uint8
	raw, err = d.ReadU8()
	m.Format = DumpFormat(raw)
	return err
}

type Prune struct{}

func (m *Prune) Type() MessageType       { return TypePrune }
func (m *Prune) encode(_ *Encoder) error { return nil }
func (m *Prune) decode(_ *Decoder) error { return nil }

type Kick struct {
	Name     string
	ClientID string
}

func (m *Kick) Type() MessageType { return TypeKick }

func (m *Kick) encode(e *Encoder) error {
	if err := e.WriteString(m.Name); err != nil {
		return err
	}
	return e.WriteString(m.ClientID)
}

func (m *Kick) decode(d *Decoder) error {
	var err error
	if m.Name, err = d.ReadString(); err != nil {
		return err
	}
	m.ClientID, err = d.ReadString()
	return err
}

type Status struct {
	Name string
}

func (m *Status) Type() MessageType { return TypeStatus }

func (m *Status) encode(e *Encoder) error {
	return e.WriteString(m.Name)
}

func (m *Status) decode(d *Decoder) error {
	var err error
	m.Name, err = d.ReadString()
	return err
}
