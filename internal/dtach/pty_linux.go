//go:build linux

package dtach

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// openPTY allocates a master/slave PTY pair via /dev/ptmx: unlock the slave
// (TIOCSPTLCK), read its index (TIOCGPTN), open /dev/pts/N.
func openPTY() (ptmx, tty *os.File, err error) {
	ptmx, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}

	var unlock int32
	if err := ioctl(ptmx.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("unlockpt: %w", err)
	}
	var n uint32
	if err := ioctl(ptmx.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("ptsname: %w", err)
	}

	name := "/dev/pts/" + strconv.Itoa(int(n))
	tty, err = os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		ptmx.Close()
		return nil, nil, fmt.Errorf("open %s: %w", name, err)
	}
	return ptmx, tty, nil
}
