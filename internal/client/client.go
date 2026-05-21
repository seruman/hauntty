package client

import (
	"cmp"
	"fmt"
	"net"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
)

type Client struct {
	conn    *protocol.Conn
	netConn net.Conn
}

func Connect(socketPath string) (*Client, error) {
	sock := cmp.Or(socketPath, config.SocketPath())
	nc, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	c := &Client{
		conn:    protocol.NewConn(nc),
		netConn: nc,
	}
	accepted, serverRev, err := c.conn.Handshake(protocol.ProtocolVersion, hauntty.Version())
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	if accepted != protocol.ProtocolVersion {
		nc.Close()
		clientRev := hauntty.Version()
		if serverRev != "" && serverRev != clientRev {
			return nil, fmt.Errorf("revision mismatch: client=%s server=%s (restart the daemon)", clientRev, serverRev)
		}
		return nil, fmt.Errorf("protocol version mismatch: server accepted %d, expected %d", accepted, protocol.ProtocolVersion)
	}
	return c, nil
}

func (c *Client) Close() error {
	return c.netConn.Close()
}

type SessionState = protocol.SessionState

const (
	SessionStateRunning = protocol.SessionStateRunning
	SessionStateDead    = protocol.SessionStateDead
)

type SessionClient struct {
	ClientID string
	ReadOnly bool
	Version  string
}

type Session struct {
	Name      string
	State     SessionState
	Cols      uint16
	Rows      uint16
	CWD       string
	PID       uint32
	CreatedAt uint32
	SavedAt   uint32
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

type Status struct {
	Daemon  DaemonStatus
	Session *SessionStatus
}

type DumpFormat = protocol.DumpFormat

const (
	DumpPlain          = protocol.DumpPlain
	DumpVT             = protocol.DumpVT
	DumpHTML           = protocol.DumpHTML
	DumpFormatMask     = protocol.DumpFormatMask
	DumpFlagUnwrap     = protocol.DumpFlagUnwrap
	DumpFlagScrollback = protocol.DumpFlagScrollback
)

type CreatedSession struct {
	Name string
	PID  uint32
}

type CreateSessionOpts struct {
	Name       string
	Command    []string
	Env        []string
	CWD        string
	Scrollback uint32
	Force      bool
}

func (c *Client) CreateSession(opts CreateSessionOpts) (*CreatedSession, error) {
	created, err := request[*protocol.Created](c, "create", &protocol.Create{
		Name:       opts.Name,
		Command:    opts.Command,
		Env:        opts.Env,
		CWD:        opts.CWD,
		Scrollback: opts.Scrollback,
		Force:      opts.Force,
	})
	if err != nil {
		return nil, err
	}
	return &CreatedSession{Name: created.Name, PID: created.PID}, nil
}

func (c *Client) attach(req *protocol.Attach) (*protocol.Attached, error) {
	return request[*protocol.Attached](c, "attach", req)
}

func (c *Client) ListSessions(includeClients bool) ([]Session, error) {
	resp, err := request[*protocol.Sessions](c, "list", &protocol.List{IncludeClients: includeClients})
	if err != nil {
		return nil, err
	}
	return sessionsFromProtocol(resp.Sessions), nil
}

func (c *Client) Kill(name string) error {
	return requestOK(c, "kill", &protocol.Kill{Name: name})
}

func (c *Client) Send(name string, data []byte) error {
	return requestOK(c, "send", &protocol.Send{Name: name, Data: data})
}

func (c *Client) SendKey(name string, keyCode KeyCode, mods Modifier) error {
	return requestOK(c, "send key", &protocol.SendKey{Name: name, Key: keyCode, Mods: mods})
}

func (c *Client) Dump(name string, format DumpFormat) ([]byte, error) {
	resp, err := request[*protocol.DumpResponse](c, "dump", &protocol.Dump{Name: name, Format: protocol.DumpFormat(format)})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) Prune() (uint32, error) {
	resp, err := request[*protocol.PruneResponse](c, "prune", &protocol.Prune{})
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

func (c *Client) Status(name string) (*Status, error) {
	resp, err := request[*protocol.StatusResponse](c, "status", &protocol.Status{Name: name})
	if err != nil {
		return nil, err
	}
	return statusFromProtocol(resp), nil
}

func (c *Client) Kick(name, clientID string) error {
	return requestOK(c, "kick", &protocol.Kick{Name: name, ClientID: clientID})
}

func (c *Client) Detach() error {
	return c.conn.WriteMessage(&protocol.Detach{})
}

func statusFromProtocol(resp *protocol.StatusResponse) *Status {
	status := &Status{
		Daemon: DaemonStatus{
			PID:          resp.Daemon.PID,
			Uptime:       resp.Daemon.Uptime,
			SocketPath:   resp.Daemon.SocketPath,
			RunningCount: resp.Daemon.RunningCount,
			DeadCount:    resp.Daemon.DeadCount,
			Version:      resp.Daemon.Version,
		},
	}
	if resp.Session != nil {
		status.Session = &SessionStatus{
			Name:    resp.Session.Name,
			State:   SessionState(resp.Session.State),
			Cols:    resp.Session.Cols,
			Rows:    resp.Session.Rows,
			PID:     resp.Session.PID,
			CWD:     resp.Session.CWD,
			Clients: sessionClientsFromProtocol(resp.Session.Clients),
		}
	}
	return status
}

func sessionsFromProtocol(sessions []protocol.Session) []Session {
	out := make([]Session, len(sessions))
	for i, session := range sessions {
		out[i] = Session{
			Name:      session.Name,
			State:     SessionState(session.State),
			Cols:      session.Cols,
			Rows:      session.Rows,
			CWD:       session.CWD,
			PID:       session.PID,
			CreatedAt: session.CreatedAt,
			SavedAt:   session.SavedAt,
			Clients:   sessionClientsFromProtocol(session.Clients),
		}
	}
	return out
}

func sessionClientsFromProtocol(clients []protocol.SessionClient) []SessionClient {
	out := make([]SessionClient, len(clients))
	for i, client := range clients {
		out[i] = SessionClient{
			ClientID: client.ClientID,
			ReadOnly: client.ReadOnly,
			Version:  client.Version,
		}
	}
	return out
}

func request[T protocol.Message](c *Client, op string, msg protocol.Message) (T, error) {
	var zero T
	if err := c.conn.WriteMessage(msg); err != nil {
		return zero, fmt.Errorf("send %s: %w", op, err)
	}
	resp, err := c.conn.ReadMessage()
	if err != nil {
		return zero, fmt.Errorf("read %s response: %w", op, err)
	}
	if serverErr, ok := resp.(*protocol.Error); ok {
		return zero, &ServerError{Op: op, Message: serverErr.Message}
	}
	typed, ok := resp.(T)
	if !ok {
		return zero, fmt.Errorf("unexpected response type: 0x%02x", resp.Type())
	}
	return typed, nil
}

func requestOK(c *Client, op string, msg protocol.Message) error {
	_, err := request[*protocol.OK](c, op, msg)
	return err
}
