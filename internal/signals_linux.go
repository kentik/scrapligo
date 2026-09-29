//go:build linux && (amd64 || arm64)

package internal

import (
	"syscall"
	"unsafe"
)

const (
	saOnStack = 0x08000000
	sigDfl    = 0
	sigIgn    = 1
)

// kernelSigaction mirrors the kernel's struct sigaction on linux amd64/arm64.
type kernelSigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

// EnsureSignalHandlersOnStack adds SA_ONSTACK to the SIGPIPE/SIGIO handlers that libscrapli
// (zig's std.Io.Threaded) installs without it. Go requires SA_ONSTACK on every handler; without it
// the kernel pushes the signal frame onto the interrupted goroutine's stack, overflowing it into
// neighbouring goroutine stacks.
func EnsureSignalHandlersOnStack() {
	for _, sig := range []syscall.Signal{syscall.SIGPIPE, syscall.SIGIO} {
		ensureSignalHandlerOnStack(sig)
	}
}

func ensureSignalHandlerOnStack(sig syscall.Signal) {
	act, err := getSigaction(sig)
	if err != nil || act.handler == sigDfl || act.handler == sigIgn || act.flags&saOnStack != 0 {
		return
	}

	act.flags |= saOnStack

	_ = setSigaction(sig, &act)
}

func getSigaction(sig syscall.Signal) (kernelSigaction, error) {
	var act kernelSigaction

	_, _, errno := syscall.RawSyscall6(
		syscall.SYS_RT_SIGACTION,
		uintptr(sig),
		0,
		uintptr(unsafe.Pointer(&act)),
		unsafe.Sizeof(act.mask),
		0,
		0,
	)
	if errno != 0 {
		return act, errno
	}

	return act, nil
}

func setSigaction(sig syscall.Signal, act *kernelSigaction) error {
	_, _, errno := syscall.RawSyscall6(
		syscall.SYS_RT_SIGACTION,
		uintptr(sig),
		uintptr(unsafe.Pointer(act)),
		0,
		unsafe.Sizeof(act.mask),
		0,
		0,
	)
	if errno != 0 {
		return errno
	}

	return nil
}
