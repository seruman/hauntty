package client

import (
	"fmt"
	"net"

	"github.com/selman/hauntty/protocol"
)

type Client struct {
	conn    *protocol.Conn
	netConn net.Conn
}

func Connect() (*Client, error) {
	sock := protocol.SocketPath()
	nc, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	c := &Client{
		conn:    protocol.NewConn(nc),
		netConn: nc,
	}
	accepted, err := c.conn.Handshake(protocol.ProtocolVersion)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	if accepted != protocol.ProtocolVersion {
		nc.Close()
		return nil, fmt.Errorf("protocol version mismatch: server accepted %d, expected %d", accepted, protocol.ProtocolVersion)
	}
	return c, nil
}

func (c *Client) Close() error {
	return c.netConn.Close()
}

func (c *Client) ReadMessage() (protocol.Message, error) {
	return c.conn.ReadMessage()
}

func (c *Client) WriteMessage(msg protocol.Message) error {
	return c.conn.WriteMessage(msg)
}

func (c *Client) Attach(name string, cols, rows, xpixel, ypixel uint16, command []string, env []string, scrollback uint32) (*protocol.OK, error) {
	err := c.conn.WriteMessage(&protocol.Attach{
		Name:            name,
		Cols:            cols,
		Rows:            rows,
		Xpixel:          xpixel,
		Ypixel:          ypixel,
		Command:         command,
		Env:             env,
		ScrollbackLines: scrollback,
	})
	if err != nil {
		return nil, fmt.Errorf("send attach: %w", err)
	}
	msg, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read attach response: %w", err)
	}
	switch m := msg.(type) {
	case *protocol.OK:
		return m, nil
	case *protocol.Error:
		return nil, fmt.Errorf("server error (%d): %s", m.Code, m.Message)
	default:
		return nil, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) List() (*protocol.Sessions, error) {
	if err := c.conn.WriteMessage(&protocol.List{}); err != nil {
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
		return nil, fmt.Errorf("server error (%d): %s", m.Code, m.Message)
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
		return fmt.Errorf("server error (%d): %s", m.Code, m.Message)
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
		return fmt.Errorf("server error (%d): %s", m.Code, m.Message)
	default:
		return fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) SendKey(name string, keyCode, mods uint32) error {
	if err := c.conn.WriteMessage(&protocol.SendKey{Name: name, KeyCode: keyCode, Mods: mods}); err != nil {
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
		return fmt.Errorf("server error (%d): %s", m.Code, m.Message)
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
		return nil, fmt.Errorf("server error (%d): %s", m.Code, m.Message)
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
		return 0, fmt.Errorf("server error (%d): %s", m.Code, m.Message)
	default:
		return 0, fmt.Errorf("unexpected response type: 0x%02x", msg.Type())
	}
}

func (c *Client) Detach() error {
	return c.conn.WriteMessage(&protocol.Detach{})
}

func (c *Client) DetachSession(name string) error {
	return c.conn.WriteMessage(&protocol.Detach{Name: name})
}
