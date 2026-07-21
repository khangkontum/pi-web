// PTY allocation with stdlib syscalls only — the replacement for
// github.com/creack/pty in the shelley-derived code. openPTY is per-platform
// (pty_linux.go, pty_darwin.go); everything else is shared.

package dtach

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// startPTY starts cmd attached to a freshly allocated PTY of the given size
// and returns the master. The child gets the slave as stdin/stdout/stderr and
// as its controlling terminal (Setsid + Setctty; the slave is fd 0 in the
// child, which is what Ctty's zero value names).
func startPTY(cmd *exec.Cmd, cols, rows uint16) (*os.File, error) {
	ptmx, tty, err := openPTY()
	if err != nil {
		return nil, err
	}
	defer tty.Close()
	setWinsize(ptmx, cols, rows)

	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true

	if err := cmd.Start(); err != nil {
		ptmx.Close()
		return nil, err
	}
	return ptmx, nil
}

// setWinsize forwards a TIOCSWINSZ to the PTY.
func setWinsize(f *os.File, cols, rows uint16) {
	ws := struct {
		Rows, Cols, X, Y uint16
	}{Rows: rows, Cols: cols}
	_, _, _ = syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
}

// ioctl issues a request against fd, surfacing the errno as an error.
func ioctl(fd, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); errno != 0 {
		return errno
	}
	return nil
}
