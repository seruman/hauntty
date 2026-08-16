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

func startSession(ctx context.Context, launch *sessionLaunch, term *terminalState, resizePolicy config.ResizePolicy, spec sessionStartSpec) *Session {
	s := &Session{
		Name:         spec.name,
		PID:          uint32(launch.cmd.Process.Pid),
		CreatedAt:    time.Now(),
		ptmx:         launch.ptmx,
		cmd:          launch.cmd,
		term:         term,
		feedCh:       make(chan feedItem, 64),
		feedDone:     make(chan struct{}),
		ptyDone:      make(chan struct{}),
		tempDir:      launch.tempDir,
		actions:      make(chan sessionAction, 16),
		ptyOut:       make(chan []byte, 64),
		clientReady:  make(chan struct{}, 1),
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
	term, err := newTerminalState(wasmRT, uint32(spec.size.cols), uint32(spec.size.rows), spec.scrollback)
	if err != nil {
		return nil, err
	}

	launch, err := launchSessionProcess(spec)
	if err != nil {
		term.close()
		return nil, err
	}

	return startSession(ctx, launch, term, resizePolicy, spec), nil
}

func restoreSession(ctx context.Context, wasmRT *libghostty.Runtime, state *sessionState, resizePolicy config.ResizePolicy, spec sessionStartSpec) (*Session, error) {
	term, err := restoreTerminalState(wasmRT, state, spec.size, spec.scrollback)
	if err != nil {
		return nil, err
	}

	cleanup := true
	defer func() {
		if cleanup {
			term.close()
		}
	}()

	launch, err := launchSessionProcess(spec)
	if err != nil {
		return nil, err
	}

	cleanup = false
	return startSession(ctx, launch, term, resizePolicy, spec), nil
}

func (s *Session) feedLoop(ctx context.Context) {
	defer close(s.feedDone)
	for item := range s.feedCh {
		if err := s.term.feed(*item.data); err != nil {
			slog.Debug("wasm feed error", "session", s.Name, "err", err)
		}
		if item.applied != nil {
			close(item.applied)
		}
		*item.data = (*item.data)[:cap(*item.data)]
		feedPool.Put(item.data)
	}
}

// waitFeedApplied blocks until feedLoop has applied the latest accepted PTY chunk.
func waitFeedApplied(applied <-chan struct{}) {
	if applied != nil {
		<-applied
	}
}

const (
	ptyReadSize       = 32 * 1024
	ptyBatchSize      = 64 * 1024
	ptyBatchThreshold = 1024
	ptyBatchWindow    = 3 * time.Millisecond
)

// ptyRead owns the gather stage. readPTY exits at process EOF or after
// Session.close closes the PTY, and ptyDone joins process reaping.
func (s *Session) ptyRead() {
	reads := make(chan []byte)
	go s.readPTY(reads)
	gatherPTYReads(reads, s.ptyOut, s.done)
}

func (s *Session) readPTY(reads chan<- []byte) {
	defer func() {
		_ = s.cmd.Wait()
		if s.cmd.ProcessState != nil {
			if ws, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
				s.exitCode = exitCodeFromWaitStatus(ws)
			}
		}
		close(reads)
		close(s.ptyDone)
	}()

	buf := make([]byte, ptyReadSize)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case reads <- data:
			case <-s.done:
				return
			}
		}
		if err != nil {
			break
		}
	}
}

func gatherPTYReads(reads <-chan []byte, batches chan<- []byte, done <-chan struct{}) {
	defer close(batches)

	var pending []byte
	readsClosed := false
	for {
		var first []byte
		if len(pending) > 0 {
			first = pending
			pending = nil
		} else {
			if readsClosed {
				return
			}
			var ok bool
			select {
			case first, ok = <-reads:
				if !ok {
					return
				}
			case <-done:
				return
			}
		}

		if len(first) < ptyBatchThreshold {
			select {
			case batches <- first:
			case <-done:
				return
			}
			continue
		}

		batch := make([]byte, 0, ptyBatchSize)
		if len(first) > ptyBatchSize {
			batch = append(batch, first[:ptyBatchSize]...)
			pending = first[ptyBatchSize:]
		} else {
			batch = append(batch, first...)
		}

		timer := time.NewTimer(ptyBatchWindow)
	gather:
		for len(batch) < ptyBatchSize && len(pending) == 0 && !readsClosed {
			select {
			case next, ok := <-reads:
				if !ok {
					readsClosed = true
					break gather
				}
				remaining := ptyBatchSize - len(batch)
				if len(next) > remaining {
					batch = append(batch, next[:remaining]...)
					pending = next[remaining:]
					break gather
				}
				batch = append(batch, next...)
			case <-timer.C:
				timer = nil
				break gather
			case <-done:
				timer.Stop()
				return
			}
		}
		if timer != nil {
			timer.Stop()
		}

		select {
		case batches <- batch:
		case <-done:
			return
		}
	}
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
	var lastFeedApplied <-chan struct{}
	var pendingOutput *protocol.Output
	var pendingClients []*sessionClient

	for {
		var clientsChanged bool
		clients, pendingClients, clientsChanged = pruneFinishedClients(clients, pendingClients)
		if clientsChanged {
			notifyClientsChanged(clients, s.size)
		}
		if len(pendingClients) == 0 {
			pendingOutput = nil
		}
		if pendingOutput != nil {
			pendingClients = queueOutput(pendingClients, pendingOutput)
			if len(pendingClients) == 0 {
				pendingOutput = nil
			}
		}

		// Nil channels stop PTY intake while terminal feed or client
		// delivery is backpressured. Session actions remain responsive.
		var ptyCh <-chan []byte
		var feedSend chan<- feedItem
		var feedItemToSend feedItem
		var clientReady <-chan struct{}
		if pendingFeed != nil {
			feedSend = s.feedCh
			feedItemToSend = *pendingFeed
		}
		if pendingOutput != nil {
			clientReady = s.clientReady
		}
		if pendingFeed == nil && pendingOutput == nil {
			ptyCh = s.ptyOut
		}

		select {
		case data, ok := <-ptyCh:
			if !ok {
				waitFeedApplied(lastFeedApplied)
				close(s.feedCh)
				<-s.feedDone
				exitMsg := &protocol.Exited{ExitCode: s.exitCode}
				for _, c := range clients {
					c.final = exitMsg
					close(c.outCh)
				}
				return
			}

			msg := &protocol.Output{Data: data}
			pendingClients = queueOutput(clients, msg)
			if len(pendingClients) > 0 {
				pendingOutput = msg
			}

			bp := feedPool.Get().(*[]byte)
			d := (*bp)[:len(data)]
			copy(d, data)
			*bp = d
			applied := make(chan struct{})
			pendingFeed = &feedItem{data: bp, applied: applied}
			lastFeedApplied = applied

		case feedSend <- feedItemToSend:
			pendingFeed = nil

		case <-clientReady:

		case action := <-s.actions:
			clients, pendingClients, clientsChanged = pruneFinishedClients(clients, pendingClients)
			if clientsChanged {
				notifyClientsChanged(clients, s.size)
			}
			if len(pendingClients) == 0 {
				pendingOutput = nil
			}
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
				waitFeedApplied(lastFeedApplied)

				dump, err := s.term.dumpScreen(libghostty.DumpVTFull)
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
					outCh:     make(chan protocol.Message, sessionClientOutBufferSize),
					ready:     s.clientReady,
					writeDone: make(chan struct{}),
				}
				s.clientWriters.Go(func() {
					sc.writeLoop()
				})

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
				pendingClients = removeClient(pendingClients, a.client)
				if len(pendingClients) == 0 {
					pendingOutput = nil
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
				pendingClients = removeClient(pendingClients, target)
				if len(pendingClients) == 0 {
					pendingOutput = nil
				}
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
					if pendingFeed.applied != nil {
						close(pendingFeed.applied)
					}
					feedPool.Put(pendingFeed.data)
					pendingFeed = nil
				}
				close(s.feedCh)
				<-s.feedDone
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
	<-s.done
	select {
	case <-s.ptyDone:
	case <-time.After(5 * time.Second):
		slog.Warn("child ignored SIGHUP, sending SIGKILL", "session", s.Name)
		_ = syscall.Kill(-int(s.PID), syscall.SIGKILL)
		<-s.ptyDone
	}
	s.clientWriters.Wait()
	s.term.close()
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

func (s *Session) waitClients() {
	s.clientWriters.Wait()
}

func (s *Session) kill() {
	_ = syscall.Kill(-int(s.PID), syscall.SIGHUP)
}

func (s *Session) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *Session) dumpScreen(ctx context.Context, format libghostty.DumpFormat) (*libghostty.ScreenDump, error) {
	return s.term.dumpScreen(format)
}

func (s *Session) snapshot(ctx context.Context) ([]byte, error) {
	return s.term.snapshot()
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
