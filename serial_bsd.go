//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package rsts

import (
	"os"

	"golang.org/x/sys/unix"
)

// openSerial opens a line and puts it in the raw 9600 8N1 state a V7-era
// terminal expects.
//
// O_NOCTTY keeps the line from becoming this process's controlling
// terminal, where a Ctrl-C on it would kill the emulator. O_NONBLOCK is
// what puts the file under Go's poller, so a read can carry a deadline
// and PollInterrupt can look for a Ctrl-C without stalling the job.
func openSerial(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if err := applySerialMode(fd); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func applySerialMode(fd int) error {
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return err
	}
	// Eight bits, no parity, one stop bit, receiver on, modem control
	// lines ignored. The BSDs keep the speed in its own fields rather
	// than in the control flags.
	t.Cflag = unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Iflag = unix.IGNPAR
	t.Oflag = 0
	t.Lflag = 0
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	t.Ispeed = 9600
	t.Ospeed = 9600
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, t)
}

func setSerialSpeed(f *os.File, baud int) error {
	n, ok := canonicalBaud(baud)
	if !ok {
		return nil
	}
	fd := int(f.Fd())
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return err
	}
	assignBaud(&t.Ispeed, n)
	assignBaud(&t.Ospeed, n)
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, t)
}

// Darwin keeps these as uint64; the other BSDs as uint32.
func assignBaud[T ~uint32 | ~uint64](dst *T, n int) {
	*dst = T(n)
}
