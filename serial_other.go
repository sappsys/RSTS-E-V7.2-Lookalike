//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package rsts

import "os"

// Setting a line to 9600 8N1 needs termios, which this platform does not
// provide. Telnet and the console still work.
func openSerial(path string) (*os.File, error) {
	return nil, fsErr("Serial lines are not supported on this platform")
}
