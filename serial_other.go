//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd && !windows

package rsts

import "os"

// Setting a line to 9600 8N1 needs termios or a Windows COM handle, and
// this platform has neither. Telnet and the console still work.
func openSerial(path string) (*os.File, error) {
	return nil, fsErr("Serial lines are not supported on this platform")
}

func pollRead(f *os.File, buf []byte) (int, error) {
	return 0, os.ErrDeadlineExceeded
}

func setSerialSpeed(*os.File, int) error { return nil }
