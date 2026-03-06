package client

import (
	"cmp"
	"fmt"
	"net"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
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

func (c *Client) Create(name string, command, env []string, cwd string, scrollback uint32, force bool) (*protocol.Created, error) {
	err := c.conn.WriteMessage(&protocol.Create{
		Name:       name,
		Command:    command,
		Env:        env,
		CWD:        cwd,
		Scrollback: scrollback,
		Force:      force,
	})
	if err != nil {
		return nil, fmt.Errorf("send create: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read create response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.Created:
		return m, nil
	case *protocol.Error:
		return nil, &ServerError{Op: "create", Message: m.Message}
	default:
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Attach(name string, cols, rows, xpixel, ypixel uint16, command, env []string, scrollback uint32, cwd string, readOnly, restore bool) (*protocol.Attached, error) {
	err := c.conn.WriteMessage(&protocol.Attach{
		Name:       name,
		Command:    command,
		Env:        env,
		CWD:        cwd,
		Cols:       cols,
		Rows:       rows,
		Xpixel:     xpixel,
		Ypixel:     ypixel,
		ReadOnly:   readOnly,
		Restore:    restore,
		Scrollback: scrollback,
	})
	if err != nil {
		return nil, fmt.Errorf("send attach: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read attach response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.Attached:
		return m, nil
	case *protocol.Error:
		return nil, &ServerError{Op: "attach", Message: m.Message}
	default:
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) List(includeClients bool) (*protocol.Sessions, error) {
	if err := c.conn.WriteMessage(&protocol.List{IncludeClients: includeClients}); err != nil {
		return nil, fmt.Errorf("send list: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read list response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.Sessions:
		return m, nil
	case *protocol.Error:
		return nil, &ServerError{Op: "list", Message: m.Message}
	default:
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Kill(name string) error {
	if err := c.conn.WriteMessage(&protocol.Kill{Name: name}); err != nil {
		return fmt.Errorf("send kill: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read kill response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.OK:
		return nil
	case *protocol.Error:
		return &ServerError{Op: "kill", Message: m.Message}
	default:
		return fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Send(name string, data []byte) error {
	if err := c.conn.WriteMessage(&protocol.Send{Name: name, Data: data}); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read send response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.OK:
		return nil
	case *protocol.Error:
		return &ServerError{Op: "send", Message: m.Message}
	default:
		return fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) SendKey(name string, keyCode libghostty.KeyCode, mods libghostty.Modifier) error {
	if err := c.conn.WriteMessage(&protocol.SendKey{Name: name, Key: uint32(keyCode), Mods: uint32(mods)}); err != nil {
		return fmt.Errorf("send key: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read send key response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.OK:
		return nil
	case *protocol.Error:
		return &ServerError{Op: "send key", Message: m.Message}
	default:
		return fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Dump(name string, format uint8) ([]byte, error) {
	if err := c.conn.WriteMessage(&protocol.Dump{Name: name, Format: format}); err != nil {
		return nil, fmt.Errorf("send dump: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read dump response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.DumpResponse:
		return m.Data, nil
	case *protocol.Error:
		return nil, &ServerError{Op: "dump", Message: m.Message}
	default:
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Prune() (uint32, error) {
	if err := c.conn.WriteMessage(&protocol.Prune{}); err != nil {
		return 0, fmt.Errorf("send prune: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return 0, fmt.Errorf("read prune response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.PruneResponse:
		return m.Count, nil
	case *protocol.Error:
		return 0, &ServerError{Op: "prune", Message: m.Message}
	default:
		return 0, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Status(name string) (*protocol.StatusResponse, error) {
	if err := c.conn.WriteMessage(&protocol.Status{Name: name}); err != nil {
		return nil, fmt.Errorf("send status: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read status response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.StatusResponse:
		return m, nil
	case *protocol.Error:
		return nil, &ServerError{Op: "status", Message: m.Message}
	default:
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Kick(name, clientID string) error {
	if err := c.conn.WriteMessage(&protocol.Kick{Name: name, ClientID: clientID}); err != nil {
		return fmt.Errorf("send kick: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read kick response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.OK:
		return nil
	case *protocol.Error:
		return &ServerError{Op: "kick", Message: m.Message}
	default:
		return fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Detach() error {
	return c.conn.WriteMessage(&protocol.Detach{})
}
