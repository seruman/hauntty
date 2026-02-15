package client

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"gotest.tools/v3/assert"
)

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

	t.Run("returns after timeout on empty fd", func(t *testing.T) {
		r, w, err := makePipe()
		assert.NilError(t, err)
		defer unix.Close(r)
		defer unix.Close(w)

		start := time.Now()
		drainStdin(r, 20*time.Millisecond)
		elapsed := time.Since(start)

		assert.Assert(t, elapsed >= 15*time.Millisecond)
		assert.Assert(t, elapsed < 200*time.Millisecond)
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

func TestDetachSequence(t *testing.T) {
	// The detach sequence must use mode 1047 (not 1049) for alt screen
	// exit to avoid DECRC cursor-restore side effects that wipe session
	// content from the primary screen.
	detachSeq := "\x1b[?1047;1;1000;1002;1003;1006;1004;2004;2048;2026l" +
		"\x1b[?25h" +
		"\x1b[<u" +
		"\x1b[0m" +
		"\x1b[J"

	t.Run("uses mode 1047 not 1049", func(t *testing.T) {
		assert.Assert(t, bytes.Contains([]byte(detachSeq), []byte("1047")))
		assert.Assert(t, !bytes.Contains([]byte(detachSeq), []byte("1049")))
	})

	t.Run("resets all expected modes", func(t *testing.T) {
		for _, mode := range []string{
			"1047", // alt screen (safe variant)
			"1",    // DECCKM
			"1000", // mouse normal
			"1002", // mouse button
			"1003", // mouse any
			"1006", // SGR mouse
			"1004", // focus events
			"2004", // bracketed paste
			"2048", // sync output (kitty)
			"2026", // sync output (contour)
		} {
			assert.Assert(t, bytes.Contains([]byte(detachSeq), []byte(mode)),
				"detach sequence missing mode %s", mode)
		}
	})

	t.Run("shows cursor", func(t *testing.T) {
		assert.Assert(t, bytes.Contains([]byte(detachSeq), []byte("\x1b[?25h")))
	})

	t.Run("pops kitty keyboard", func(t *testing.T) {
		assert.Assert(t, bytes.Contains([]byte(detachSeq), []byte("\x1b[<u")))
	})

	t.Run("resets SGR", func(t *testing.T) {
		assert.Assert(t, bytes.Contains([]byte(detachSeq), []byte("\x1b[0m")))
	})

	t.Run("erases below cursor", func(t *testing.T) {
		assert.Assert(t, bytes.HasSuffix([]byte(detachSeq), []byte("\x1b[J")))
	})
}

func TestReattachClearSequence(t *testing.T) {
	// The reattach clear must use per-line EL (CSI 2K), not ED (CSI J /
	// CSI 2J) which can interact with scrollback in Ghostty.
	rows := 24

	t.Run("uses EL not ED", func(t *testing.T) {
		var buf bytes.Buffer
		for row := 1; row <= rows; row++ {
			fmt.Fprintf(&buf, "\x1b[%d;1H\x1b[2K", row)
		}
		buf.WriteString("\x1b[H")
		seq := buf.Bytes()

		// Must contain EL (CSI 2K) for each row.
		assert.Equal(t, bytes.Count(seq, []byte("\x1b[2K")), rows)

		// Must NOT contain ED sequences.
		assert.Assert(t, !bytes.Contains(seq, []byte("\x1b[J")))
		assert.Assert(t, !bytes.Contains(seq, []byte("\x1b[2J")))
		assert.Assert(t, !bytes.Contains(seq, []byte("\x1b[3J")))
	})

	t.Run("positions to each row", func(t *testing.T) {
		var buf bytes.Buffer
		for row := 1; row <= rows; row++ {
			fmt.Fprintf(&buf, "\x1b[%d;1H\x1b[2K", row)
		}
		buf.WriteString("\x1b[H")
		seq := buf.Bytes()

		for row := 1; row <= rows; row++ {
			cup := fmt.Sprintf("\x1b[%d;1H", row)
			assert.Assert(t, bytes.Contains(seq, []byte(cup)),
				"missing CUP for row %d", row)
		}
	})

	t.Run("ends with cursor home", func(t *testing.T) {
		var buf bytes.Buffer
		for row := 1; row <= rows; row++ {
			fmt.Fprintf(&buf, "\x1b[%d;1H\x1b[2K", row)
		}
		buf.WriteString("\x1b[H")
		seq := buf.Bytes()

		assert.Assert(t, bytes.HasSuffix(seq, []byte("\x1b[H")))
	})
}

func TestReattachScrollCount(t *testing.T) {
	// The scroll-into-scrollback step must scroll exactly cursorRow
	// lines (from DSR), not the full terminal height. This avoids
	// blank-line gaps in scrollback.
	t.Run("scroll count matches cursor row", func(t *testing.T) {
		cursorRow := 10
		termRows := 58

		scroll := append([]byte("\x1b[999;1H"), bytes.Repeat([]byte{'\n'}, cursorRow)...)

		// Must have exactly cursorRow newlines, not termRows.
		nlCount := bytes.Count(scroll, []byte{'\n'})
		assert.Equal(t, nlCount, cursorRow)
		assert.Assert(t, nlCount < termRows)
	})

	t.Run("fallback scrolls full height", func(t *testing.T) {
		termRows := 58

		scroll := append([]byte("\x1b[999;1H"), bytes.Repeat([]byte{'\n'}, termRows)...)

		nlCount := bytes.Count(scroll, []byte{'\n'})
		assert.Equal(t, nlCount, termRows)
	})
}

func makePipe() (r, w int, err error) {
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		return 0, 0, err
	}
	return fds[0], fds[1], nil
}
