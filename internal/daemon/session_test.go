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
		{cols: 100, rows: 30, xpixel: 1000, ypixel: 600},
		{cols: 80, rows: 24, xpixel: 800, ypixel: 480},
		{cols: 120, rows: 40, xpixel: 1200, ypixel: 800},
	}

	size := applyResizePolicy(config.ResizePolicySmallest, sizes)
	assert.Equal(t, size.cols, uint16(80))
	assert.Equal(t, size.rows, uint16(24))
	assert.Equal(t, size.xpixel, uint16(800))
	assert.Equal(t, size.ypixel, uint16(480))

	size = applyResizePolicy(config.ResizePolicyLargest, sizes)
	assert.Equal(t, size.cols, uint16(120))
	assert.Equal(t, size.rows, uint16(40))
	assert.Equal(t, size.xpixel, uint16(1200))
	assert.Equal(t, size.ypixel, uint16(800))

	size = applyResizePolicy(config.ResizePolicyFirst, sizes)
	assert.Equal(t, size.cols, uint16(100))
	assert.Equal(t, size.rows, uint16(30))
	assert.Equal(t, size.xpixel, uint16(1000))
	assert.Equal(t, size.ypixel, uint16(600))

	size = applyResizePolicy(config.ResizePolicyLast, sizes)
	assert.Equal(t, size.cols, uint16(120))
	assert.Equal(t, size.rows, uint16(40))
	assert.Equal(t, size.xpixel, uint16(1200))
	assert.Equal(t, size.ypixel, uint16(800))
}

func TestApplyResizePolicySingleClient(t *testing.T) {
	sizes := []termSize{{cols: 90, rows: 27, xpixel: 900, ypixel: 540}}
	size := applyResizePolicy(config.ResizePolicySmallest, sizes)
	assert.Equal(t, size.cols, uint16(90))
	assert.Equal(t, size.rows, uint16(27))
	assert.Equal(t, size.xpixel, uint16(900))
	assert.Equal(t, size.ypixel, uint16(540))

	size = applyResizePolicy(config.ResizePolicyLargest, sizes)
	assert.Equal(t, size.cols, uint16(90))
	assert.Equal(t, size.rows, uint16(27))
	assert.Equal(t, size.xpixel, uint16(900))
	assert.Equal(t, size.ypixel, uint16(540))

	size = applyResizePolicy(config.ResizePolicyFirst, sizes)
	assert.Equal(t, size.cols, uint16(90))
	assert.Equal(t, size.rows, uint16(27))
	assert.Equal(t, size.xpixel, uint16(900))
	assert.Equal(t, size.ypixel, uint16(540))

	size = applyResizePolicy(config.ResizePolicyLast, sizes)
	assert.Equal(t, size.cols, uint16(90))
	assert.Equal(t, size.rows, uint16(27))
	assert.Equal(t, size.xpixel, uint16(900))
	assert.Equal(t, size.ypixel, uint16(540))
}

func TestApplyResizePolicySmallestMixedDims(t *testing.T) {
	sizes := []termSize{
		{cols: 120, rows: 30, xpixel: 500, ypixel: 800},
		{cols: 80, rows: 40, xpixel: 900, ypixel: 300},
	}
	size := applyResizePolicy(config.ResizePolicySmallest, sizes)
	assert.Equal(t, size.cols, uint16(80))
	assert.Equal(t, size.rows, uint16(30))
	assert.Equal(t, size.xpixel, uint16(500))
	assert.Equal(t, size.ypixel, uint16(300))
}

func TestApplyResizePolicyLargestZeroValues(t *testing.T) {
	sizes := []termSize{
		{cols: 0, rows: 24, xpixel: 0, ypixel: 600},
		{cols: 80, rows: 0, xpixel: 800, ypixel: 0},
	}
	size := applyResizePolicy(config.ResizePolicyLargest, sizes)
	assert.Equal(t, size.cols, uint16(80))
	assert.Equal(t, size.rows, uint16(24))
	assert.Equal(t, size.xpixel, uint16(800))
	assert.Equal(t, size.ypixel, uint16(600))
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
	assert.Error(t, err, "exit status 17")

	waitStatus := cmd.ProcessState.Sys().(syscall.WaitStatus)
	code := exitCodeFromWaitStatus(waitStatus)
	assert.Equal(t, code, int32(17))
}

func TestExitCodeFromWaitStatusSignaled(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "kill -TERM $$")
	err := cmd.Run()
	assert.Error(t, err, "signal: terminated")

	waitStatus := cmd.ProcessState.Sys().(syscall.WaitStatus)
	code := exitCodeFromWaitStatus(waitStatus)
	assert.Equal(t, code, int32(128+syscall.SIGTERM))
}
