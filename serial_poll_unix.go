//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package rsts

import (
	"os"
	"time"
)

// pollRead takes whatever is already waiting on the line. The device was
// opened non-blocking, so it sits under Go's poller and a deadline is
// enough to keep this from waiting for input that may never come.
func pollRead(f *os.File, buf []byte) (int, error) {
	if err := f.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
		return 0, err
	}
	defer f.SetReadDeadline(time.Time{})
	return f.Read(buf)
}
