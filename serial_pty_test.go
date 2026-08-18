//go:build linux

package rsts

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// serialPair gives back a line opened exactly as a real serial device is,
// termios and all, with the other end of a pty standing in for whatever
// terminal would be plugged into it.
func serialPair(t *testing.T) (*os.File, *peerTerm, func()) {
	t.Helper()
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx here: %v", err)
	}
	fd := int(ptmx.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		ptmx.Close()
		t.Skipf("cannot unlock a pty: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		ptmx.Close()
		t.Skipf("cannot name the pty: %v", err)
	}
	line, err := openSerial(fmt.Sprintf("/dev/pts/%d", n))
	if err != nil {
		ptmx.Close()
		t.Fatalf("openSerial: %v", err)
	}
	peer := newPeerTerm(ptmx)
	return line, peer, func() {
		line.Close()
		ptmx.Close()
	}
}

// peerTerm is the terminal at the far end of the cable.
type peerTerm struct {
	f    *os.File
	mu   sync.Mutex
	seen strings.Builder
}

func newPeerTerm(f *os.File) *peerTerm {
	p := &peerTerm{f: f}
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.seen.Write(buf[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}

func (p *peerTerm) WriteString(s string) {
	_, _ = p.f.WriteString(s)
}

func (p *peerTerm) Seen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen.String()
}

// ReadAvailable takes what has arrived so far and forgets it.
func (p *peerTerm) ReadAvailable() string {
	time.Sleep(50 * time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.seen.String()
	p.seen.Reset()
	return s
}

func (p *peerTerm) WaitFor(want string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.Seen(), want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
