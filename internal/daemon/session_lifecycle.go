package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"github.com/creack/pty"
)

func launchSessionProcess(spec sessionStartSpec) (*sessionLaunch, error) {
	env := mergeEnv(os.Environ(), spec.env)
	command := resolveShellCommand(spec.command, env)

	shellArgs, shellEnv, tempDir, err := prepareShellLaunch(command, env, spec.name)
	if err != nil {
		slog.Warn("shell integration setup failed, continuing without it", "err", err)
		shellArgs = command
		shellEnv = env
	}

	cmd := exec.Command(shellArgs[0], shellArgs[1:]...)
	cmd.Env = shellEnv
	if spec.cwd != "" {
		cmd.Dir = spec.cwd
	}

	ptmx, err := pty.StartWithSize(cmd, spec.size.winsize())
	if err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, err
	}

	return &sessionLaunch{ptmx: ptmx, cmd: cmd, tempDir: tempDir}, nil
}

func startSession(ctx context.Context, launch *sessionLaunch, term *libghostty.Terminal, resizePolicy config.ResizePolicy, spec sessionStartSpec) *Session {
	s := &Session{
		Name:         spec.name,
		PID:          uint32(launch.cmd.Process.Pid),
		CreatedAt:    time.Now(),
		ptmx:         launch.ptmx,
		cmd:          launch.cmd,
		term:         term,
		feedCh:       make(chan feedItem, 64),
		tempDir:      launch.tempDir,
		actions:      make(chan sessionAction, 16),
		ptyOut:       make(chan []byte, 64),
		done:         make(chan struct{}),
		resizePolicy: resizePolicy,
		ctx:          ctx,
	}
	s.setSize(spec.size.cols, spec.size.rows)

	go s.feedLoop(ctx)
	go s.ptyRead()
	go s.run()
	return s
}

func newSession(ctx context.Context, wasmRT *libghostty.Runtime, resizePolicy config.ResizePolicy, spec sessionStartSpec) (*Session, error) {
	term, err := wasmRT.NewTerminal(ctx, uint32(spec.size.cols), uint32(spec.size.rows), spec.scrollback)
	if err != nil {
		return nil, err
	}

	launch, err := launchSessionProcess(spec)
	if err != nil {
		term.Close(ctx)
		return nil, err
	}

	return startSession(ctx, launch, term, resizePolicy, spec), nil
}

func restoreSession(ctx context.Context, wasmRT *libghostty.Runtime, state *sessionState, resizePolicy config.ResizePolicy, spec sessionStartSpec) (*Session, error) {
	term, err := wasmRT.NewTerminal(ctx, uint32(state.Cols), uint32(state.Rows), spec.scrollback)
	if err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			term.Close(ctx)
		}
	}()

	if len(state.VT) > 0 {
		if err := term.Feed(ctx, state.VT); err != nil {
			return nil, err
		}
	}
	if state.IsAltScreen {
		if err := term.Feed(ctx, []byte("\x1b[?1049l")); err != nil {
			return nil, err
		}
	}
	if err := term.Feed(ctx, []byte("\x1b[!p")); err != nil {
		return nil, err
	}
	if state.Cols != spec.size.cols || state.Rows != spec.size.rows {
		if err := term.Resize(ctx, uint32(spec.size.cols), uint32(spec.size.rows)); err != nil {
			slog.Warn("wasm resize on restore", "session", spec.name, "err", err)
		}
	}

	launch, err := launchSessionProcess(spec)
	if err != nil {
		return nil, err
	}

	cleanup = false
	return startSession(ctx, launch, term, resizePolicy, spec), nil
}

func (s *Session) feedLoop(ctx context.Context) {
	for item := range s.feedCh {
		if err := s.term.Feed(ctx, *item.data); err != nil {
			slog.Debug("wasm feed error", "session", s.Name, "err", err)
		}
		s.feedApplied.Store(item.seq)
		*item.data = (*item.data)[:cap(*item.data)]
		feedPool.Put(item.data)
	}
}

// waitFeedApplied blocks until feedLoop has applied every PTY chunk up to target.
func (s *Session) waitFeedApplied(target uint64) {
	for s.feedApplied.Load() < target {
		time.Sleep(100 * time.Microsecond)
	}
}

func (s *Session) ptyRead() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case s.ptyOut <- data:
			case <-s.done:
				return
			}
		}
		if err != nil {
			break
		}
	}

	_ = s.cmd.Wait()
	if ws, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		s.exitCode = exitCodeFromWaitStatus(ws)
	}
	close(s.ptyOut)
}

// run owns client state; connection writes go through per-client outCh.
func (s *Session) run() {
	defer close(s.done)

	var clients []*sessionClient
	var nextClientID uint64

	// pendingFeed holds data waiting to be sent to feedCh. While
	// non-nil, we stop reading ptyOut (backpressure) but keep
	// processing actions so detach/kick/list don't stall.
	var pendingFeed *feedItem
	var nextFeedSeq uint64

	for {
		// Nil-channel trick: only one of ptyCh/feedSend is active
		// at a time. When pendingFeed is nil, read ptyOut. When
		// non-nil, send to feedCh. Actions are always processed.
		var ptyCh <-chan []byte
		var feedSend chan<- feedItem
		var feedItemToSend feedItem
		if pendingFeed != nil {
			feedSend = s.feedCh
			feedItemToSend = *pendingFeed
		} else {
			ptyCh = s.ptyOut
		}

		select {
		case data, ok := <-ptyCh:
			if !ok {
				close(s.feedCh)
				exitMsg := &protocol.Exited{ExitCode: s.exitCode}
				for _, c := range clients {
					select {
					case c.outCh <- exitMsg:
						close(c.outCh)
					default:
						close(c.outCh)
						_ = c.closeConn()
					}
				}
				return
			}

			msg := &protocol.Output{Data: data}
			before := len(clients)
			clients = broadcastOutput(clients, s.Name, msg)
			if len(clients) != before {
				notifyClientsChanged(clients, s.size)
			}

			bp := feedPool.Get().(*[]byte)
			d := (*bp)[:len(data)]
			copy(d, data)
			*bp = d
			nextFeedSeq++
			pendingFeed = &feedItem{data: bp, seq: nextFeedSeq}

		case feedSend <- feedItemToSend:
			pendingFeed = nil

		case action := <-s.actions:
			switch a := action.(type) {
			case attachReq:
				if !a.spec.readOnly {
					s.resizeForPending(clients, a.spec.size)
				}
				if pendingFeed != nil {
					s.feedCh <- *pendingFeed
					pendingFeed = nil
				}
				// Attach dumps must reflect every PTY chunk we've already accepted.
				s.waitFeedApplied(nextFeedSeq)

				dump, err := s.term.DumpScreen(s.ctx, libghostty.DumpVTFull)
				if err != nil {
					a.result <- attachResp{err: err}
					continue
				}

				nextClientID++
				clientID := fmt.Sprintf("%d", nextClientID)
				cols, rows := s.size()

				sc := &sessionClient{
					id:        clientID,
					conn:      a.spec.conn,
					closeConn: a.spec.closeConn,
					size:      a.spec.size,
					version:   a.spec.version,
					readOnly:  a.spec.readOnly,
					outCh:     make(chan protocol.Message, 64),
				}
				go sc.writeLoop()

				// Attached is the first message on outCh, guaranteed
				// to precede any Output since the client isn't in the
				// clients list yet.
				sc.outCh <- &protocol.Attached{
					Name:       s.Name,
					PID:        s.PID,
					ClientID:   clientID,
					Cols:       cols,
					Rows:       rows,
					ScreenDump: dump.Data,
					CursorRow:  dump.CursorRow,
					CursorCol:  dump.CursorCol,
					AltScreen:  dump.IsAltScreen,
					Created:    a.spec.created,
				}

				clients = append(clients, sc)
				if !a.spec.readOnly {
					s.arbitrateResize(clients)
				}
				notifyClientsChanged(clients, s.size)

				a.result <- attachResp{client: sc}

			case detachReq:
				before := len(clients)
				clients = removeClient(clients, a.client)
				if len(clients) == before {
					continue // already removed (e.g., kicked)
				}
				close(a.client.outCh)
				s.arbitrateResize(clients)
				notifyClientsChanged(clients, s.size)

			case kickReq:
				var target *sessionClient
				for _, c := range clients {
					if c.id == a.clientID {
						target = c
						break
					}
				}
				if target == nil {
					a.result <- false
					continue
				}
				clients = removeClient(clients, target)
				close(target.outCh)
				_ = target.closeConn()
				s.arbitrateResize(clients)
				notifyClientsChanged(clients, s.size)
				a.result <- true

			case resizeReq:
				a.client.size = a.size
				s.arbitrateResize(clients)

			case clientInfoReq:
				info := make([]protocol.SessionClient, len(clients))
				for i, c := range clients {
					info[i] = protocol.SessionClient{
						ClientID: c.id,
						ReadOnly: c.readOnly,
						Version:  c.version,
					}
				}
				a.result <- info

			case stopReq:
				// Force-close: disconnect all clients, close feedCh, return.
				// Clients see connection close (EOF), not Exited — this is
				// the kill/shutdown path.
				if pendingFeed != nil {
					feedPool.Put(pendingFeed.data)
					pendingFeed = nil
				}
				close(s.feedCh)
				for _, c := range clients {
					close(c.outCh)
					_ = c.closeConn()
				}
				return
			}
		}
	}
}

func (s *Session) close(ctx context.Context) {
	select {
	case s.actions <- stopReq{}:
	case <-s.done:
	}

	s.kill()
	s.ptmx.Close() // unblock ptyRead if blocked on Read
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		slog.Warn("child ignored SIGHUP, sending SIGKILL", "session", s.Name)
		_ = syscall.Kill(-int(s.PID), syscall.SIGKILL)
		<-s.done
	}
	s.term.Close(ctx)
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

func (s *Session) kill() {
	_ = syscall.Kill(-int(s.PID), syscall.SIGHUP)
}

func (s *Session) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *Session) dumpScreen(ctx context.Context, format libghostty.DumpFormat) (*libghostty.ScreenDump, error) {
	return s.term.DumpScreen(ctx, format)
}

func exitCodeFromWaitStatus(ws syscall.WaitStatus) int32 {
	if ws.Exited() {
		return int32(ws.ExitStatus())
	}
	if ws.Signaled() {
		return int32(128 + ws.Signal())
	}
	return 1
}
