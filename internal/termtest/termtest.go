package termtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"code.selman.me/hauntty/libghostty"
	"github.com/creack/pty"
)

type DumpFormat int

const (
	DumpPlain DumpFormat = iota
	DumpVTFull
)

type ScreenDump struct {
	Data        []byte
	CursorRow   uint32
	CursorCol   uint32
	IsAltScreen bool
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
	t          testing.TB
	ptmx       *os.File
	term       *libghostty.Terminal
	keyEncoder *libghostty.KeyEncoder
	keyEvent   *libghostty.KeyEvent
	ctx        context.Context
	done       chan struct{}
	opts       options
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

	ctx := t.Context()

	term, err := libghostty.NewTerminal(
		libghostty.WithSize(uint16(o.cols), uint16(o.rows)),
		libghostty.WithMaxScrollbackLines(uint(o.scrollback)),
	)
	if err != nil {
		t.Fatalf("termtest: new terminal: %v", err)
	}

	keyEncoder, err := libghostty.NewKeyEncoder()
	if err != nil {
		term.Close()
		t.Fatalf("termtest: new key encoder: %v", err)
	}

	keyEvent, err := libghostty.NewKeyEvent()
	if err != nil {
		keyEncoder.Close()
		term.Close()
		t.Fatalf("termtest: new key event: %v", err)
	}

	binPath, err := lookPathIn(command[0], o.env)
	if err != nil {
		keyEvent.Close()
		keyEncoder.Close()
		term.Close()
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
		keyEvent.Close()
		keyEncoder.Close()
		term.Close()
		t.Fatalf("termtest: start pty: %v", err)
	}

	tm := &Term{
		t:          t,
		ptmx:       ptmx,
		term:       term,
		keyEncoder: keyEncoder,
		keyEvent:   keyEvent,
		ctx:        ctx,
		done:       make(chan struct{}),
		opts:       o,
	}

	go tm.readLoop()

	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Signal(os.Kill))
		_ = cmd.Wait()
		<-tm.done
		ptmx.Close()
		keyEvent.Close()
		keyEncoder.Close()
		term.Close()
	})

	return tm
}

func (tm *Term) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := tm.ptmx.Read(buf)
		if n > 0 {
			tm.term.VTWrite(buf[:n])
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

func (tm *Term) Key(key libghostty.Key, mods libghostty.Mods) {
	tm.t.Helper()

	tm.keyEvent.SetAction(libghostty.KeyActionPress)
	tm.keyEvent.SetKey(key)
	tm.keyEvent.SetMods(mods)
	tm.keyEvent.SetConsumedMods(0)
	tm.keyEvent.SetComposing(false)

	codepoint := rune(0)
	switch key {
	case libghostty.KeyBracketRight:
		codepoint = ']'
	case libghostty.KeyBackslash:
		codepoint = '\\'
	case libghostty.KeyEnter:
		codepoint = '\r'
	}

	tm.keyEvent.SetUnshiftedCodepoint(codepoint)
	tm.keyEvent.SetUTF8("")
	tm.keyEncoder.SetOptFromTerminal(tm.term)

	data, err := tm.keyEncoder.Encode(tm.keyEvent)
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

	dump := tm.Snapshot(DumpPlain)

	return string(dump.Data)
}

func (tm *Term) ScreenVT() []byte {
	tm.t.Helper()

	return tm.Snapshot(DumpVTFull).Data
}

func (tm *Term) Snapshot(format DumpFormat) *ScreenDump {
	tm.t.Helper()

	options := []libghostty.FormatterOption{
		libghostty.WithFormatterTrim(true),
	}

	if format == DumpPlain {
		cols, err := tm.term.Cols()
		if err != nil {
			tm.t.Fatalf("termtest: terminal columns: %v", err)
		}

		rows, err := tm.term.Rows()
		if err != nil {
			tm.t.Fatalf("termtest: terminal rows: %v", err)
		}

		start, err := tm.term.GridRef(libghostty.Point{Tag: libghostty.PointTagActive})
		if err != nil {
			tm.t.Fatalf("termtest: selection start: %v", err)
		}

		end, err := tm.term.GridRef(libghostty.Point{
			Tag: libghostty.PointTagActive,
			X:   cols - 1,
			Y:   uint32(rows - 1),
		})
		if err != nil {
			tm.t.Fatalf("termtest: selection end: %v", err)
		}

		selection := &libghostty.Selection{Start: *start, End: *end}
		options = append(options, libghostty.WithFormatterSelection(selection))
	}

	if format == DumpVTFull {
		options = append(
			options,
			libghostty.WithFormatterFormat(libghostty.FormatterFormatVT),
			libghostty.WithFormatterExtraModes(true),
			libghostty.WithFormatterExtraScrollingRegion(true),
			libghostty.WithFormatterExtraPwd(true),
			libghostty.WithFormatterExtraKeyboard(true),
			libghostty.WithFormatterExtraCursor(true),
			libghostty.WithFormatterExtraStyle(true),
			libghostty.WithFormatterExtraHyperlink(true),
			libghostty.WithFormatterExtraProtection(true),
			libghostty.WithFormatterExtraKittyKeyboard(true),
			libghostty.WithFormatterExtraCharsets(true),
		)
	}

	formatter, err := libghostty.NewFormatter(tm.term, options...)
	if err != nil {
		tm.t.Fatalf("termtest: snapshot formatter: %v", err)
	}
	defer formatter.Close()

	data, err := formatter.Format()
	if err != nil {
		tm.t.Fatalf("termtest: snapshot: %v", err)
	}

	cursorCol, err := tm.term.CursorX()
	if err != nil {
		tm.t.Fatalf("termtest: cursor column: %v", err)
	}

	cursorRow, err := tm.term.CursorY()
	if err != nil {
		tm.t.Fatalf("termtest: cursor row: %v", err)
	}

	activeScreen, err := tm.term.ActiveScreen()
	if err != nil {
		tm.t.Fatalf("termtest: active screen: %v", err)
	}

	return &ScreenDump{
		Data:        data,
		CursorRow:   uint32(cursorRow),
		CursorCol:   uint32(cursorCol),
		IsAltScreen: activeScreen == libghostty.ScreenAlternate,
	}
}

func (tm *Term) Resize(cols, rows uint32) {
	tm.t.Helper()

	ws := &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	if err := pty.Setsize(tm.ptmx, ws); err != nil {
		tm.t.Fatalf("termtest: setsize: %v", err)
	}

	if err := tm.term.Resize(uint16(cols), uint16(rows), 0, 0); err != nil {
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
			dump := tm.Snapshot(DumpPlain)
			last = string(dump.Data)

			if strings.Contains(last, substr) {
				return
			}
		}
	}
}

func (tm *Term) RowContains(row int, substr string) bool {
	tm.t.Helper()

	dump := tm.Snapshot(DumpPlain)

	return rowContainsDump(dump, row, substr)
}

func (tm *Term) CursorRowContains(substr string) bool {
	tm.t.Helper()

	dump := tm.Snapshot(DumpPlain)

	return rowContainsDump(dump, int(dump.CursorRow), substr)
}

func (tm *Term) WaitRowContains(row int, substr string, opts ...WaitOption) {
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
			tm.t.Fatalf(
				"termtest: WaitRowContains(row=%d, %q) timed out after %v\nlast screen:\n%s",
				row,
				substr,
				wo.timeout,
				last,
			)
		case <-ticker.C:
			dump := tm.Snapshot(DumpPlain)
			last = string(dump.Data)

			if rowContainsDump(dump, row, substr) {
				return
			}
		}
	}
}

func (tm *Term) WaitCursorRowContains(substr string, opts ...WaitOption) {
	tm.t.Helper()

	wo := waitOptions{timeout: tm.opts.timeout, interval: 50 * time.Millisecond}
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
			tm.t.Fatalf("termtest: WaitCursorRowContains(%q) timed out after %v\nlast screen:\n%s", substr, wo.timeout, last)
		case <-ticker.C:
			dump := tm.Snapshot(DumpPlain)
			last = string(dump.Data)

			if rowContainsDump(dump, int(dump.CursorRow), substr) {
				return
			}
		}
	}
}

func (tm *Term) PromptVisible() bool {
	tm.t.Helper()

	promptRE := regexp.MustCompile(`[#$>] ?$`)

	return tm.PromptVisibleMatch(promptRE.MatchString)
}

func (tm *Term) PromptVisibleMatch(match func(string) bool) bool {
	tm.t.Helper()

	dump := tm.Snapshot(DumpPlain)
	row := int(dump.CursorRow)

	return promptOnRowMatch(dump, row, match) || promptOnRowMatch(dump, row-1, match)
}

func (tm *Term) WaitPrompt(opts ...WaitOption) {
	tm.t.Helper()

	promptRE := regexp.MustCompile(`[#$>] ?$`)
	tm.WaitPromptMatch(promptRE.MatchString, opts...)
}

func (tm *Term) WaitPromptMatch(match func(string) bool, opts ...WaitOption) {
	tm.t.Helper()

	wo := waitOptions{timeout: tm.opts.timeout, interval: 50 * time.Millisecond}
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
			tm.t.Fatalf("termtest: WaitPromptMatch timed out after %v\nlast screen:\n%s", wo.timeout, last)
		case <-ticker.C:
			dump := tm.Snapshot(DumpPlain)
			last = string(dump.Data)
			row := int(dump.CursorRow)

			if promptOnRowMatch(dump, row, match) || promptOnRowMatch(dump, row-1, match) {
				return
			}
		}
	}
}

func (tm *Term) WaitStable(stableFor time.Duration, opts ...WaitOption) {
	tm.t.Helper()

	wo := waitOptions{timeout: tm.opts.timeout, interval: 50 * time.Millisecond}
	for _, fn := range opts {
		fn(&wo)
	}

	deadline := time.After(wo.timeout)
	ticker := time.NewTicker(wo.interval)
	defer ticker.Stop()

	var last string
	var sameSince time.Time
	for {
		select {
		case <-deadline:
			tm.t.Fatalf("termtest: WaitStable(%v) timed out after %v\nlast screen:\n%s", stableFor, wo.timeout, last)
		case <-ticker.C:
			dump := tm.Snapshot(DumpPlain)
			cur := string(dump.Data)

			if cur != last {
				last = cur
				sameSince = time.Now()
				continue
			}

			if !sameSince.IsZero() && time.Since(sameSince) >= stableFor {
				return
			}
		}
	}
}

func promptOnRowMatch(dump *ScreenDump, row int, match func(string) bool) bool {
	line, ok := rowLine(dump, row)
	if !ok {
		return false
	}

	return match(strings.TrimRight(line, "\r"))
}

func rowContainsDump(dump *ScreenDump, row int, substr string) bool {
	line, ok := rowLine(dump, row)
	if !ok {
		return false
	}

	return strings.Contains(line, substr)
}

func rowLine(dump *ScreenDump, row int) (string, bool) {
	lines := strings.Split(string(dump.Data), "\n")
	if row < 0 || row >= len(lines) {
		return "", false
	}

	return lines[row], true
}

func (tm *Term) Done() <-chan struct{} {
	return tm.done
}
