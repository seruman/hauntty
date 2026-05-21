package client

import (
	"io"
	"os"
	"testing"
	"time"

	"code.selman.me/hauntty/internal/protocol"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"gotest.tools/v3/assert"
)

func TestAttachRequestFromOpts(t *testing.T) {
	opts := AttachOpts{
		Name:     "demo",
		Command:  []string{"sh", "-lc", "echo hi"},
		ReadOnly: true,
		Restore:  true,
		Metadata: func(fd int) (AttachMetadata, error) {
			assert.Equal(t, fd, 7)
			return AttachMetadata{
				Cols:   100,
				Rows:   40,
				Xpixel: 900,
				Ypixel: 700,
				Env:    []string{"TERM=xterm-256color"},
				CWD:    "/tmp/demo",
			}, nil
		},
	}

	got, err := attachRequestFromOpts(7, opts)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, &protocol.Attach{
		Name:       "demo",
		Command:    []string{"sh", "-lc", "echo hi"},
		Cols:       100,
		Rows:       40,
		Xpixel:     900,
		Ypixel:     700,
		Env:        []string{"TERM=xterm-256color"},
		CWD:        "/tmp/demo",
		Scrollback: 0,
		ReadOnly:   true,
		Restore:    true,
	})
}

func TestAttachRequestFromOptsRequiresMetadata(t *testing.T) {
	got, err := attachRequestFromOpts(7, AttachOpts{})
	assert.Error(t, err, "attach metadata function is required")
	assert.DeepEqual(t, got, (*protocol.Attach)(nil))
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
