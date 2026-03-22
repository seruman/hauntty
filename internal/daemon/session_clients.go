package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"syscall"
	"time"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"github.com/creack/pty"
)

const (
	sessionClientOutBufferSize = 256
	slowClientGracePeriod      = 100 * time.Millisecond
)

func (c *sessionClient) writeLoop() {
	for msg := range c.outCh {
		if err := c.conn.WriteMessage(msg); err != nil {
			break
		}
	}
}

func (s *Session) attach(ctx context.Context, spec sessionAttachSpec) (*sessionClient, error) {
	ch := make(chan attachResp, 1)
	req := attachReq{spec: spec, result: ch}
	select {
	case s.actions <- req:
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Don't select on ctx.Done() here — if the action was accepted,
	// we must wait for the result to avoid orphaning the client.
	select {
	case resp := <-ch:
		return resp.client, resp.err
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	}
}

func (s *Session) detachClient(sc *sessionClient) {
	select {
	case s.actions <- detachReq{client: sc}:
	case <-s.done:
	}
}

func (s *Session) kickClient(clientID string) bool {
	ch := make(chan bool, 1)
	select {
	case s.actions <- kickReq{clientID: clientID, result: ch}:
	case <-s.done:
		return false
	}
	select {
	case found := <-ch:
		return found
	case <-s.done:
		return false
	}
}

func (s *Session) resizeClient(sc *sessionClient, size termSize) {
	select {
	case s.actions <- resizeReq{client: sc, size: size}:
	case <-s.done:
	}
}

func (s *Session) clientInfo() []protocol.SessionClient {
	ch := make(chan []protocol.SessionClient, 1)
	select {
	case s.actions <- clientInfoReq{result: ch}:
	case <-s.done:
		return nil
	}
	select {
	case info := <-ch:
		return info
	case <-s.done:
		return nil
	}
}

func (s *Session) resize(size termSize) {
	s.setSize(size.cols, size.rows)

	if err := pty.Setsize(s.ptmx, size.winsize()); err != nil {
		slog.Warn("pty setsize", "session", s.Name, "err", err)
	}
	_ = syscall.Kill(-int(s.PID), syscall.SIGWINCH)
	if err := s.term.Resize(s.ctx, uint32(size.cols), uint32(size.rows)); err != nil {
		slog.Warn("wasm resize", "session", s.Name, "err", err)
	}
}

func collectClientSizes(clients []*sessionClient) []termSize {
	sizes := make([]termSize, 0, len(clients))
	for _, c := range clients {
		if c.readOnly {
			continue
		}
		sizes = append(sizes, c.size)
	}
	return sizes
}

func (s *Session) arbitrateResize(clients []*sessionClient) {
	sizes := collectClientSizes(clients)
	if len(sizes) == 0 {
		return
	}
	target := applyResizePolicy(s.resizePolicy, sizes)
	curCols, curRows := s.size()
	if target.cols != curCols || target.rows != curRows {
		s.resize(target)
	}
}

func (s *Session) resizeForPending(clients []*sessionClient, size termSize) {
	sizes := append(collectClientSizes(clients), size)
	target := applyResizePolicy(s.resizePolicy, sizes)
	curCols, curRows := s.size()
	if target.cols != curCols || target.rows != curRows {
		s.resize(target)
	}
}

func applyResizePolicy(policy config.ResizePolicy, sizes []termSize) termSize {
	var size termSize
	switch policy {
	case config.ResizePolicyLargest:
		for _, candidate := range sizes {
			size.cols = max(size.cols, candidate.cols)
			size.rows = max(size.rows, candidate.rows)
			size.xpixel = max(size.xpixel, candidate.xpixel)
			size.ypixel = max(size.ypixel, candidate.ypixel)
		}
	case config.ResizePolicyLast:
		size = sizes[len(sizes)-1]
	case config.ResizePolicyFirst:
		size = sizes[0]
	default: // config.ResizePolicySmallest
		size = termSize{cols: math.MaxUint16, rows: math.MaxUint16, xpixel: math.MaxUint16, ypixel: math.MaxUint16}
		for _, candidate := range sizes {
			size.cols = min(size.cols, candidate.cols)
			size.rows = min(size.rows, candidate.rows)
			size.xpixel = min(size.xpixel, candidate.xpixel)
			size.ypixel = min(size.ypixel, candidate.ypixel)
		}
	}
	return size
}

func removeClient(clients []*sessionClient, target *sessionClient) []*sessionClient {
	for i, c := range clients {
		if c == target {
			return append(clients[:i], clients[i+1:]...)
		}
	}
	return clients
}

func broadcastOutput(clients []*sessionClient, name string, msg *protocol.Output) []*sessionClient {
	i := 0
	for _, c := range clients {
		select {
		case c.outCh <- msg:
			clients[i] = c
			i++
			continue
		default:
		}

		timer := time.NewTimer(slowClientGracePeriod)
		select {
		case c.outCh <- msg:
			if !timer.Stop() {
				<-timer.C
			}
			clients[i] = c
			i++
		case <-timer.C:
			slog.Debug("evicting slow client", "session", name, "grace", slowClientGracePeriod)
			close(c.outCh)
			_ = c.closeConn()
		}
	}
	return clients[:i]
}

func notifyClientsChanged(clients []*sessionClient, sizeFn func() (uint16, uint16)) {
	cols, rows := sizeFn()
	msg := &protocol.ClientsChanged{
		Count: uint16(len(clients)),
		Cols:  cols,
		Rows:  rows,
	}
	for _, c := range clients {
		select {
		case c.outCh <- msg:
		default:
		}
	}
}
