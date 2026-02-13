package client

import (
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

func makePipe() (r, w int, err error) {
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		return 0, 0, err
	}
	return fds[0], fds[1], nil
}
