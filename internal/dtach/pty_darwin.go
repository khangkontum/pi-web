//go:build darwin

package dtach

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPTY allocates a master/slave PTY pair via /dev/ptmx: grant and unlock
// the slave (TIOCPTYGRANT/TIOCPTYUNLK), read its path (TIOCPTYGNAME), open it.
func openPTY() (ptmx, tty *os.File, err error) {
	ptmx, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	if err := ioctl(ptmx.Fd(), syscall.TIOCPTYGRANT, 0); err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("grantpt: %w", err)
	}
	if err := ioctl(ptmx.Fd(), syscall.TIOCPTYUNLK, 0); err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("unlockpt: %w", err)
	}

	// TIOCPTYGNAME fills a 128-byte NUL-terminated path buffer.
	var buf [128]byte
	if err := ioctl(ptmx.Fd(), syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&buf[0]))); err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("ptsname: %w", err)
	}
	name := string(buf[:])
	if i := bytes.IndexByte(buf[:], 0); i >= 0 {
		name = string(buf[:i])
	}

	tty, err = os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("open %s: %w", name, err)
	}
	return ptmx, tty, nil
}
