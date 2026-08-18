//go:build windows

package rsts

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no termios. A COM port is opened with CreateFile and set up
// through a DCB, and the read timeouts stand in for the deadline that a
// poller would give us on Unix.

const (
	noParity   = 0
	oneStopBit = 0

	// Bits of DCB.Flags, which winbase.h declares as a packed bitfield.
	dcbBinary         = 0x0001
	dcbParityCheck    = 0x0002
	dcbOutxCtsFlow    = 0x0004
	dcbOutxDsrFlow    = 0x0008
	dcbDtrMask        = 0x0030
	dcbDtrEnable      = 0x0010
	dcbDsrSensitivity = 0x0040
	dcbOutX           = 0x0100
	dcbInX            = 0x0200
	dcbErrorChar      = 0x0400
	dcbNull           = 0x0800
	dcbRtsMask        = 0x3000
	dcbRtsEnable      = 0x1000
	dcbAbortOnError   = 0x4000

	maxDWORD = 0xFFFFFFFF
)

// A read waits for at least one character and no longer, so a job parked
// at the keyboard costs nothing.
var blockingTimeouts = windows.CommTimeouts{}

// A read returns at once with whatever has already arrived, which is how
// PollInterrupt looks for a Ctrl-C without stopping to wait for one.
var immediateTimeouts = windows.CommTimeouts{ReadIntervalTimeout: maxDWORD}

// comName puts a port into the form CreateFile wants. COM10 and above are
// only reachable through the \\.\ prefix, and it is harmless on COM1, so
// every bare name gets it.
func comName(path string) string {
	if strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `\\?\`) {
		return path
	}
	return `\\.\` + path
}

// openSerial opens a COM port at 9600 8N1.
func openSerial(path string) (*os.File, error) {
	u16, err := windows.UTF16PtrFromString(comName(path))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(u16,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, // a serial line is not shared
		nil,
		windows.OPEN_EXISTING,
		0, // not overlapped: the timeouts above do the waiting
		0)
	if err != nil {
		return nil, err
	}
	if err := applySerialMode(h); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func applySerialMode(h windows.Handle) error {
	// Start from what the port already has so anything not named here
	// keeps a sane value.
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		return err
	}
	dcb.BaudRate = 9600
	dcb.ByteSize = 8
	dcb.Parity = noParity
	dcb.StopBits = oneStopBit
	// No flow control of any kind, and no character translation: the
	// emulator wants the bytes exactly as they arrive. DTR and RTS are
	// asserted so a terminal that watches them sees us ready.
	dcb.Flags &^= dcbParityCheck | dcbOutxCtsFlow | dcbOutxDsrFlow |
		dcbDsrSensitivity | dcbOutX | dcbInX | dcbErrorChar | dcbNull |
		dcbAbortOnError | dcbDtrMask | dcbRtsMask
	dcb.Flags |= dcbBinary | dcbDtrEnable | dcbRtsEnable
	if err := windows.SetCommState(h, &dcb); err != nil {
		return err
	}
	return windows.SetCommTimeouts(h, &blockingTimeouts)
}

// pollRead takes whatever is already waiting on the line and returns
// straight away. os.ErrDeadlineExceeded means nothing had arrived, which
// on a communications port is not the end of the line.
func pollRead(f *os.File, buf []byte) (int, error) {
	h := windows.Handle(f.Fd())
	if err := windows.SetCommTimeouts(h, &immediateTimeouts); err != nil {
		return 0, err
	}
	defer windows.SetCommTimeouts(h, &blockingTimeouts)
	n, err := f.Read(buf)
	if n == 0 {
		return 0, os.ErrDeadlineExceeded
	}
	return n, err
}
