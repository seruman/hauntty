package daemon

import (
	"math"
	"os/exec"
	"syscall"
	"testing"

	"gotest.tools/v3/assert"
)

func TestApplyResizePolicy(t *testing.T) {
	dims := []clientDims{
		{cols: 80, rows: 24, xpixel: 640, ypixel: 480},
		{cols: 120, rows: 40, xpixel: 960, ypixel: 800},
		{cols: 100, rows: 30, xpixel: 800, ypixel: 600},
	}

	t.Run("smallest", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("smallest", dims)
		assert.Equal(t, cols, uint16(80))
		assert.Equal(t, rows, uint16(24))
		assert.Equal(t, xpixel, uint16(640))
		assert.Equal(t, ypixel, uint16(480))
	})

	t.Run("largest", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("largest", dims)
		assert.Equal(t, cols, uint16(120))
		assert.Equal(t, rows, uint16(40))
		assert.Equal(t, xpixel, uint16(960))
		assert.Equal(t, ypixel, uint16(800))
	})

	t.Run("first", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("first", dims)
		assert.Equal(t, cols, uint16(80))
		assert.Equal(t, rows, uint16(24))
		assert.Equal(t, xpixel, uint16(640))
		assert.Equal(t, ypixel, uint16(480))
	})

	t.Run("last", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("last", dims)
		assert.Equal(t, cols, uint16(100))
		assert.Equal(t, rows, uint16(30))
		assert.Equal(t, xpixel, uint16(800))
		assert.Equal(t, ypixel, uint16(600))
	})

	t.Run("unknown defaults to smallest", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("bogus", dims)
		assert.Equal(t, cols, uint16(80))
		assert.Equal(t, rows, uint16(24))
		assert.Equal(t, xpixel, uint16(640))
		assert.Equal(t, ypixel, uint16(480))
	})
}

func TestApplyResizePolicySingleClient(t *testing.T) {
	dims := []clientDims{
		{cols: 100, rows: 50, xpixel: 800, ypixel: 600},
	}

	for _, policy := range []string{"smallest", "largest", "first", "last"} {
		t.Run(policy, func(t *testing.T) {
			cols, rows, xpixel, ypixel := applyResizePolicy(policy, dims)
			assert.Equal(t, cols, uint16(100))
			assert.Equal(t, rows, uint16(50))
			assert.Equal(t, xpixel, uint16(800))
			assert.Equal(t, ypixel, uint16(600))
		})
	}
}

func TestApplyResizePolicySmallestMixedDims(t *testing.T) {
	dims := []clientDims{
		{cols: 80, rows: 40, xpixel: 960, ypixel: 480},
		{cols: 120, rows: 24, xpixel: 640, ypixel: 800},
	}

	cols, rows, xpixel, ypixel := applyResizePolicy("smallest", dims)
	assert.Equal(t, cols, uint16(80))
	assert.Equal(t, rows, uint16(24))
	assert.Equal(t, xpixel, uint16(640))
	assert.Equal(t, ypixel, uint16(480))
}

func TestApplyResizePolicyLargestZeroValues(t *testing.T) {
	dims := []clientDims{
		{cols: 0, rows: 0, xpixel: 0, ypixel: 0},
		{cols: 80, rows: 24, xpixel: 640, ypixel: 480},
	}

	t.Run("largest", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("largest", dims)
		assert.Equal(t, cols, uint16(80))
		assert.Equal(t, rows, uint16(24))
		assert.Equal(t, xpixel, uint16(640))
		assert.Equal(t, ypixel, uint16(480))
	})

	t.Run("smallest", func(t *testing.T) {
		cols, rows, xpixel, ypixel := applyResizePolicy("smallest", dims)
		assert.Equal(t, cols, uint16(0))
		assert.Equal(t, rows, uint16(0))
		assert.Equal(t, xpixel, uint16(0))
		assert.Equal(t, ypixel, uint16(0))
	})
}

func TestApplyResizePolicySmallestStartsAtMaxUint16(t *testing.T) {
	dims := []clientDims{
		{cols: math.MaxUint16, rows: math.MaxUint16, xpixel: math.MaxUint16, ypixel: math.MaxUint16},
	}
	cols, rows, xpixel, ypixel := applyResizePolicy("smallest", dims)
	assert.Equal(t, cols, uint16(math.MaxUint16))
	assert.Equal(t, rows, uint16(math.MaxUint16))
	assert.Equal(t, xpixel, uint16(math.MaxUint16))
	assert.Equal(t, ypixel, uint16(math.MaxUint16))
}

func TestExitCodeFromWaitStatusExited(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 17")
	err := cmd.Run()
	assert.Assert(t, err != nil)

	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	assert.Assert(t, ok)

	code := exitCodeFromWaitStatus(ws)
	assert.Equal(t, code, int32(17))
}

func TestExitCodeFromWaitStatusSignaled(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "kill -TERM $$")
	err := cmd.Run()
	assert.Assert(t, err != nil)

	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	assert.Assert(t, ok)

	code := exitCodeFromWaitStatus(ws)
	assert.Equal(t, code, int32(128+syscall.SIGTERM))
}
