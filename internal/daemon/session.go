package daemon

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"github.com/creack/pty"
)

var feedPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

type feedItem struct {
	data *[]byte
	seq  uint64
}

type termSize struct {
	cols   uint16
	rows   uint16
	xpixel uint16
	ypixel uint16
}

func (s termSize) winsize() *pty.Winsize {
	return &pty.Winsize{Rows: s.rows, Cols: s.cols, X: s.xpixel, Y: s.ypixel}
}

// All fields are owned exclusively by the session's run loop.
type sessionClient struct {
	id        string
	conn      *protocol.Conn
	closeConn func() error
	size      termSize
	version   string
	readOnly  bool
	outCh     chan protocol.Message
}

type sessionAction interface {
	isSessionAction()
}

type sessionAttachSpec struct {
	conn      *protocol.Conn
	closeConn func() error
	size      termSize
	version   string
	readOnly  bool
	created   bool
}

type attachReq struct {
	spec   sessionAttachSpec
	result chan<- attachResp
}

func (attachReq) isSessionAction() {}

type attachResp struct {
	client *sessionClient
	err    error
}

type detachReq struct {
	client *sessionClient
}

func (detachReq) isSessionAction() {}

type resizeReq struct {
	client *sessionClient
	size   termSize
}

func (resizeReq) isSessionAction() {}

type kickReq struct {
	clientID string
	result   chan<- bool
}

func (kickReq) isSessionAction() {}

type clientInfoReq struct {
	result chan<- []protocol.SessionClient
}

func (clientInfoReq) isSessionAction() {}

type stopReq struct{}

func (stopReq) isSessionAction() {}

type Session struct {
	Name      string
	PID       uint32
	CreatedAt time.Time

	ptmx    *os.File
	cmd     *exec.Cmd
	term    *libghostty.Terminal
	feedCh  chan feedItem
	tempDir string

	actions     chan sessionAction
	ptyOut      chan []byte
	done        chan struct{}
	exitCode    int32
	feedApplied atomic.Uint64

	// sizeVal packs cols|rows as (cols<<16)|rows for lock-free reads.
	sizeVal atomic.Uint32

	resizePolicy config.ResizePolicy
	ctx          context.Context
}

func (s *Session) size() (uint16, uint16) {
	v := s.sizeVal.Load()
	return uint16(v >> 16), uint16(v)
}

func (s *Session) setSize(cols, rows uint16) {
	s.sizeVal.Store(uint32(cols)<<16 | uint32(rows))
}

func (s *Session) isRunning() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func resolveShellCommand(command []string, env []string) []string {
	if len(command) > 0 {
		return command
	}
	for _, e := range env {
		if len(e) > 6 && e[:6] == "SHELL=" {
			return []string{e[6:]}
		}
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}

type sessionStartSpec struct {
	name       string
	command    []string
	env        []string
	cwd        string
	size       termSize
	scrollback uint32
}

type sessionLaunch struct {
	ptmx    *os.File
	cmd     *exec.Cmd
	tempDir string
}
