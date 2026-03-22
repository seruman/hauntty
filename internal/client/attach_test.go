package client

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"gotest.tools/v3/assert"
)

func TestPrepareInteractiveAttach(t *testing.T) {
	master, slave, err := pty.Open()
	assert.NilError(t, err)
	defer master.Close()
	defer slave.Close()

	assert.NilError(t, pty.Setsize(slave, &pty.Winsize{Cols: 132, Rows: 47, X: 900, Y: 700}))

	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("COLORTERM", "truecolor")

	req, err := prepareInteractiveAttach(int(slave.Fd()), []string{"COLORTERM"})
	assert.NilError(t, err)
	assert.Equal(t, req.Cols, uint16(132))
	assert.Equal(t, req.Rows, uint16(47))
	assert.Equal(t, req.Xpixel, uint16(900))
	assert.Equal(t, req.Ypixel, uint16(700))
	assert.DeepEqual(t, req.Env, []string{"TERM=xterm-256color", "SHELL=/bin/bash", "COLORTERM=truecolor"})
	assert.Equal(t, req.CWD, cwd)
	assert.Equal(t, req.Scrollback, uint32(0))
}

func TestDrainStdin(t *testing.T) {
	t.Run("consumes pending bytes", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("\x1b[I\x1b[48;58;191;2088;3438t"))
		assert.NilError(t, err)

		drainStdin(r, 20*time.Millisecond)

		// Nothing should remain — select with zero timeout.
		var fds unix.FdSet
		fds.Set(r)
		tv := unix.NsecToTimeval(0)
		n, _ := unix.Select(r+1, &fds, nil, nil, &tv)
		assert.Equal(t, n, 0)
	})

	t.Run("does not consume bytes arriving after drain", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		drainStdin(r, 20*time.Millisecond)

		_, err = unix.Write(w, []byte("hello"))
		assert.NilError(t, err)

		buf := make([]byte, 16)
		n, err := unix.Read(r, buf)
		assert.NilError(t, err)
		assert.Equal(t, string(buf[:n]), "hello")
	})
}

func TestReadCursorRow(t *testing.T) {
	t.Run("parses standard DSR response", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("\x1b[10;1R"))
		assert.NilError(t, err)

		row := readCursorRow(r, 58)
		assert.Equal(t, row, 10)
	})

	t.Run("parses large row number", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("\x1b[58;120R"))
		assert.NilError(t, err)

		row := readCursorRow(r, 24)
		assert.Equal(t, row, 58)
	})

	t.Run("parses row 1", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("\x1b[1;1R"))
		assert.NilError(t, err)

		row := readCursorRow(r, 24)
		assert.Equal(t, row, 1)
	})

	t.Run("returns fallback on timeout", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		row := readCursorRow(r, 42)
		assert.Equal(t, row, 42)
	})

	t.Run("returns fallback on garbage input", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("garbage\n"))
		assert.NilError(t, err)

		row := readCursorRow(r, 42)
		assert.Equal(t, row, 42)
	})

	t.Run("returns fallback on missing semicolon", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("\x1b[10R"))
		assert.NilError(t, err)

		row := readCursorRow(r, 42)
		assert.Equal(t, row, 42)
	})

	t.Run("returns fallback on non-numeric row", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		_, err = unix.Write(w, []byte("\x1b[ab;1R"))
		assert.NilError(t, err)

		row := readCursorRow(r, 42)
		assert.Equal(t, row, 42)
	})

	t.Run("ignores leading bytes before ESC", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		// Terminal might send other responses before DSR reply.
		_, err = unix.Write(w, []byte("\x1b[I\x1b[5;3R"))
		assert.NilError(t, err)

		row := readCursorRow(r, 42)
		assert.Equal(t, row, 5)
	})
}

func TestRestoreHostTerminalWritesDetachSequence(t *testing.T) {
	master, slave, err := pty.Open()
	assert.NilError(t, err)
	defer master.Close()
	defer slave.Close()

	stdoutR, stdoutW, err := os.Pipe()
	assert.NilError(t, err)
	defer stdoutR.Close()

	oldStdout := os.Stdout
	os.Stdout = stdoutW
	defer func() {
		os.Stdout = oldStdout
	}()

	oldState, err := term.MakeRaw(int(slave.Fd()))
	assert.NilError(t, err)

	restoreHostTerminal(int(slave.Fd()), oldState, "")
	assert.NilError(t, stdoutW.Close())

	out, err := io.ReadAll(stdoutR)
	assert.NilError(t, err)
	assert.DeepEqual(t, out, []byte(
		"\x1b[?1047;1;1000;1002;1003;1006;1004;2004;2048;2026l"+
			"\x1b[?25h"+
			"\x1b[<u"+
			"\x1b[0m"+
			"\x1b[J"))
}

func makePipe() (r, w int, err error) {
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		return 0, 0, err
	}
	return fds[0], fds[1], nil
}
