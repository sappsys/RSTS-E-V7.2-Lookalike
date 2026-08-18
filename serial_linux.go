//go:build linux

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
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	// 9600 baud, eight bits, no parity, one stop bit. CREAD enables the
	// receiver and CLOCAL ignores modem control lines, so a three-wire
	// cable with no handshaking still works.
	t.Cflag = unix.CS8 | unix.CREAD | unix.CLOCAL | unix.B9600
	// Raw: no translation, no flow control, no canonical line editing and
	// no signals. The emulator does its own echo and line editing.
	t.Iflag = unix.IGNPAR
	t.Oflag = 0
	t.Lflag = 0
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(fd, unix.TCSETS, t)
}

func setSerialSpeed(f *os.File, baud int) error {
	flag, ok := linuxBaudFlag(baud)
	if !ok {
		return nil
	}
	fd := int(f.Fd())
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	t.Cflag &^= unix.CBAUD
	t.Cflag |= flag
	return unix.IoctlSetTermios(fd, unix.TCSETS, t)
}

func linuxBaudFlag(baud int) (uint32, bool) {
	switch baud {
	case 50:
		return unix.B50, true
	case 75:
		return unix.B75, true
	case 110:
		return unix.B110, true
	case 134:
		return unix.B134, true
	case 150:
		return unix.B150, true
	case 200:
		return unix.B200, true
	case 300:
		return unix.B300, true
	case 600:
		return unix.B600, true
	case 1200:
		return unix.B1200, true
	case 1800:
		return unix.B1800, true
	case 2400:
		return unix.B2400, true
	case 4800:
		return unix.B4800, true
	case 9600:
		return unix.B9600, true
	case 19200:
		return unix.B19200, true
	case 38400:
		return unix.B38400, true
	case 57600:
		return unix.B57600, true
	case 115200:
		return unix.B115200, true
	default:
		return 0, false
	}
}
