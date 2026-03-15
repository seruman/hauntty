package daemon

import (
	"math"
	"os/exec"
	"syscall"
	"testing"

	"code.selman.me/hauntty/internal/config"
	"gotest.tools/v3/assert"
)

func TestApplyResizePolicy(t *testing.T) {
	sizes := []termSize{
		{cols: 80, rows: 24, xpixel: 640, ypixel: 480},
		{cols: 120, rows: 40, xpixel: 960, ypixel: 800},
		{cols: 100, rows: 30, xpixel: 800, ypixel: 600},
	}

	t.Run("smallest", func(t *testing.T) {
		size := applyResizePolicy(config.ResizePolicySmallest, sizes)
		assert.Equal(t, size.cols, uint16(80))
		assert.Equal(t, size.rows, uint16(24))
		assert.Equal(t, size.xpixel, uint16(640))
		assert.Equal(t, size.ypixel, uint16(480))
	})

	t.Run("largest", func(t *testing.T) {
		size := applyResizePolicy(config.ResizePolicyLargest, sizes)
		assert.Equal(t, size.cols, uint16(120))
		assert.Equal(t, size.rows, uint16(40))
		assert.Equal(t, size.xpixel, uint16(960))
		assert.Equal(t, size.ypixel, uint16(800))
	})

	t.Run("first", func(t *testing.T) {
		size := applyResizePolicy(config.ResizePolicyFirst, sizes)
		assert.Equal(t, size.cols, uint16(80))
		assert.Equal(t, size.rows, uint16(24))
		assert.Equal(t, size.xpixel, uint16(640))
		assert.Equal(t, size.ypixel, uint16(480))
	})

	t.Run("last", func(t *testing.T) {
		size := applyResizePolicy(config.ResizePolicyLast, sizes)
		assert.Equal(t, size.cols, uint16(100))
		assert.Equal(t, size.rows, uint16(30))
		assert.Equal(t, size.xpixel, uint16(800))
		assert.Equal(t, size.ypixel, uint16(600))
	})

	t.Run("unknown defaults to smallest", func(t *testing.T) {
		size := applyResizePolicy("bogus", sizes)
		assert.Equal(t, size.cols, uint16(80))
		assert.Equal(t, size.rows, uint16(24))
		assert.Equal(t, size.xpixel, uint16(640))
		assert.Equal(t, size.ypixel, uint16(480))
	})
}

func TestApplyResizePolicySingleClient(t *testing.T) {
	sizes := []termSize{
		{cols: 100, rows: 50, xpixel: 800, ypixel: 600},
	}

	for _, policy := range []config.ResizePolicy{config.ResizePolicySmallest, config.ResizePolicyLargest, config.ResizePolicyFirst, config.ResizePolicyLast} {
		t.Run(string(policy), func(t *testing.T) {
			size := applyResizePolicy(policy, sizes)
			assert.Equal(t, size.cols, uint16(100))
			assert.Equal(t, size.rows, uint16(50))
			assert.Equal(t, size.xpixel, uint16(800))
			assert.Equal(t, size.ypixel, uint16(600))
		})
	}
}

func TestApplyResizePolicySmallestMixedDims(t *testing.T) {
	sizes := []termSize{
		{cols: 80, rows: 40, xpixel: 960, ypixel: 480},
		{cols: 120, rows: 24, xpixel: 640, ypixel: 800},
	}

	size := applyResizePolicy(config.ResizePolicySmallest, sizes)
	assert.Equal(t, size.cols, uint16(80))
	assert.Equal(t, size.rows, uint16(24))
	assert.Equal(t, size.xpixel, uint16(640))
	assert.Equal(t, size.ypixel, uint16(480))
}

func TestApplyResizePolicyLargestZeroValues(t *testing.T) {
	sizes := []termSize{
		{cols: 0, rows: 0, xpixel: 0, ypixel: 0},
		{cols: 80, rows: 24, xpixel: 640, ypixel: 480},
	}

	t.Run("largest", func(t *testing.T) {
		size := applyResizePolicy(config.ResizePolicyLargest, sizes)
		assert.Equal(t, size.cols, uint16(80))
		assert.Equal(t, size.rows, uint16(24))
		assert.Equal(t, size.xpixel, uint16(640))
		assert.Equal(t, size.ypixel, uint16(480))
	})

	t.Run("smallest", func(t *testing.T) {
		size := applyResizePolicy(config.ResizePolicySmallest, sizes)
		assert.Equal(t, size.cols, uint16(0))
		assert.Equal(t, size.rows, uint16(0))
		assert.Equal(t, size.xpixel, uint16(0))
		assert.Equal(t, size.ypixel, uint16(0))
	})
}

func TestApplyResizePolicySmallestStartsAtMaxUint16(t *testing.T) {
	sizes := []termSize{
		{cols: math.MaxUint16, rows: math.MaxUint16, xpixel: math.MaxUint16, ypixel: math.MaxUint16},
	}
	size := applyResizePolicy(config.ResizePolicySmallest, sizes)
	assert.Equal(t, size.cols, uint16(math.MaxUint16))
	assert.Equal(t, size.rows, uint16(math.MaxUint16))
	assert.Equal(t, size.xpixel, uint16(math.MaxUint16))
	assert.Equal(t, size.ypixel, uint16(math.MaxUint16))
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
