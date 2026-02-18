package termtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"code.selman.me/hauntty/libghostty"
	"github.com/creack/pty"
)

var (
	sharedRT   *libghostty.Runtime
	sharedOnce sync.Once
)

func getRuntime(t testing.TB) *libghostty.Runtime {
	t.Helper()
	sharedOnce.Do(func() {
		rt, err := libghostty.NewRuntime(context.Background())
		if err != nil {
			panic(fmt.Sprintf("termtest: init wasm runtime: %v", err))
		}
		sharedRT = rt
	})
	return sharedRT
}

type options struct {
	cols, rows uint32
	scrollback uint32
	env        []string
	dir        string
	timeout    time.Duration
}

type Option func(*options)

func WithSize(cols, rows uint32) Option {
	return func(o *options) {
		o.cols = cols
		o.rows = rows
	}
}

func WithScrollback(n uint32) Option {
	return func(o *options) {
		o.scrollback = n
	}
}

func WithEnv(env ...string) Option {
	return func(o *options) {
		o.env = append(o.env, env...)
	}
}

func WithDir(dir string) Option {
	return func(o *options) {
		o.dir = dir
	}
}

func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

type Term struct {
	t    testing.TB
	ptmx *os.File
	term *libghostty.Terminal
	ctx  context.Context
	done chan struct{}
	opts options
}

// exec.Command uses exec.LookPath which reads PATH from parent process.
// To isolate tests from parent's PATH, resolve commands using provided env.
func lookPathIn(name string, env []string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	var path string
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			path = e[5:]
			break
		}
	}
	if path == "" {
		return "", fmt.Errorf("PATH not set in environment")
	}
	for dir := range strings.SplitSeq(path, ":") {
		p := dir + "/" + name
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%q not found in PATH", name)
}

func New(t testing.TB, command []string, opts ...Option) *Term {
	t.Helper()

	o := options{
		cols:       80,
		rows:       24,
		scrollback: 1000,
		timeout:    5 * time.Second,
	}
	for _, fn := range opts {
		fn(&o)
	}

	rt := getRuntime(t)
	ctx := t.Context()

	term, err := rt.NewTerminal(ctx, o.cols, o.rows, o.scrollback)
	if err != nil {
		t.Fatalf("termtest: new terminal: %v", err)
	}

	binPath, err := lookPathIn(command[0], o.env)
	if err != nil {
		term.Close(ctx)
		t.Fatalf("termtest: lookup %q: %v", command[0], err)
	}
	cmd := &exec.Cmd{
		Path: binPath,
		Args: command,
		Env:  o.env,
	}
	if o.dir != "" {
		cmd.Dir = o.dir
	}

	ws := &pty.Winsize{Cols: uint16(o.cols), Rows: uint16(o.rows)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		term.Close(ctx)
		t.Fatalf("termtest: start pty: %v", err)
	}

	tm := &Term{
		t:    t,
		ptmx: ptmx,
		term: term,
		ctx:  ctx,
		done: make(chan struct{}),
		opts: o,
	}

	go tm.readLoop()

	t.Cleanup(func() {
		cmd.Process.Signal(os.Signal(os.Kill))
		cmd.Wait()
		<-tm.done
		ptmx.Close()
		term.Close(ctx)
	})

	return tm
}

func (tm *Term) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := tm.ptmx.Read(buf)
		if n > 0 {
			tm.term.Feed(tm.ctx, buf[:n])
		}
		if err != nil {
			break
		}
	}
	close(tm.done)
}

func (tm *Term) Type(s string) {
	tm.t.Helper()
	if _, err := tm.ptmx.Write([]byte(s)); err != nil {
		tm.t.Fatalf("termtest: type: %v", err)
	}
}

func (tm *Term) Key(keyCode libghostty.KeyCode, mods libghostty.Modifier) {
	tm.t.Helper()
	data, err := tm.term.EncodeKey(tm.ctx, keyCode, mods)
	if err != nil {
		tm.t.Fatalf("termtest: encode key: %v", err)
	}
	if len(data) > 0 {
		if _, err := tm.ptmx.Write(data); err != nil {
			tm.t.Fatalf("termtest: write key: %v", err)
		}
	}
}

func (tm *Term) Screen() string {
	tm.t.Helper()
	dump, err := tm.term.DumpScreen(tm.ctx, libghostty.DumpPlain)
	if err != nil {
		tm.t.Fatalf("termtest: dump screen: %v", err)
	}
	return string(dump.VT)
}

func (tm *Term) ScreenVT() []byte {
	tm.t.Helper()
	dump, err := tm.term.DumpScreen(tm.ctx, libghostty.DumpVTFull)
	if err != nil {
		tm.t.Fatalf("termtest: dump screen vt: %v", err)
	}
	return dump.VT
}

func (tm *Term) Snapshot(format libghostty.DumpFormat) *libghostty.ScreenDump {
	tm.t.Helper()
	dump, err := tm.term.DumpScreen(tm.ctx, format)
	if err != nil {
		tm.t.Fatalf("termtest: snapshot: %v", err)
	}
	return dump
}

func (tm *Term) Resize(cols, rows uint32) {
	tm.t.Helper()
	ws := &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	if err := pty.Setsize(tm.ptmx, ws); err != nil {
		tm.t.Fatalf("termtest: setsize: %v", err)
	}
	if err := tm.term.Resize(tm.ctx, cols, rows); err != nil {
		tm.t.Fatalf("termtest: wasm resize: %v", err)
	}
}

type waitOptions struct {
	timeout  time.Duration
	interval time.Duration
}

type WaitOption func(*waitOptions)

func WaitTimeout(d time.Duration) WaitOption {
	return func(o *waitOptions) {
		o.timeout = d
	}
}

func WaitInterval(d time.Duration) WaitOption {
	return func(o *waitOptions) {
		o.interval = d
	}
}

func (tm *Term) WaitFor(substr string, opts ...WaitOption) {
	tm.t.Helper()

	wo := waitOptions{
		timeout:  tm.opts.timeout,
		interval: 50 * time.Millisecond,
	}
	for _, fn := range opts {
		fn(&wo)
	}

	deadline := time.After(wo.timeout)
	ticker := time.NewTicker(wo.interval)
	defer ticker.Stop()

	var last string
	for {
		select {
		case <-deadline:
			tm.t.Fatalf("termtest: WaitFor(%q) timed out after %v\nlast screen:\n%s", substr, wo.timeout, last)
		case <-ticker.C:
			dump, err := tm.term.DumpScreen(tm.ctx, libghostty.DumpPlain)
			if err != nil {
				continue
			}
			last = string(dump.VT)
			if strings.Contains(last, substr) {
				return
			}
		}
	}
}

func (tm *Term) Done() <-chan struct{} {
	return tm.done
}
