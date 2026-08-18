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

func TestSerialTermLineEditing(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	term := newSerialTerm(f)
	go func() {
		// "HELLO" with a typo rubbed out, then Return.
		peer.WriteString("HELXO\b\bLO\r")
	}()
	line, err := term.ReadLine("")
	if err != nil {
		t.Fatal(err)
	}
	if line != "HELLO" {
		t.Fatalf("read %q, want HELLO", line)
	}

	go peer.WriteString("SECRET\r")
	pw, err := term.ReadPassword("Password: ")
	if err != nil {
		t.Fatal(err)
	}
	if pw != "SECRET" {
		t.Fatalf("password %q", pw)
	}
	if echoed := peer.ReadAvailable(); strings.Contains(echoed, "SECRET") {
		t.Fatalf("the password was echoed back: %q", echoed)
	}
}

func TestSerialTermTranslatesNewlines(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	term := newSerialTerm(f)
	if _, err := term.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	got := peer.ReadAvailable()
	if got != "one\r\ntwo\r\n" {
		t.Fatalf("wrote %q, want CR LF endings", got)
	}
}

func TestSerialTermInterrupt(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	term := newSerialTerm(f)
	peer.WriteString("\x03")
	if !waitFor(func() bool { return term.PollInterrupt() }) {
		t.Fatal("Ctrl-C on the line was not seen")
	}

	// Other bytes are kept rather than eaten by the poll.
	peer.WriteString("AB\x03CD\r")
	if !waitFor(func() bool { return term.PollInterrupt() }) {
		t.Fatal("Ctrl-C in the middle of a line was not seen")
	}
	line, err := term.ReadLine("")
	if err != nil {
		t.Fatal(err)
	}
	if line != "ABCD" {
		t.Fatalf("kept %q, want ABCD", line)
	}
}

// A whole login over a real line, the same way a terminal on a serial
// port would drive it.
func TestSerialLineLogsIn(t *testing.T) {
	f, peer, cleanup := serialPair(t)
	defer cleanup()

	sys, err := NewSystem(t.TempDir(), Config{MaxUsers: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer waitJobs(sys, 0)
	defer sys.Close()

	job, err := sys.Attach("SERIAL test")
	if err != nil {
		t.Fatal(err)
	}
	term := newSerialTerm(f)
	go sys.runOnTerm(job, term, term, "SERIAL test", "", false)

	if !peer.WaitFor("Bye") {
		t.Fatalf("no Bye prompt on the line:\n%s", peer.Seen())
	}
	peer.WriteString("HELLO GUEST\r")
	if !peer.WaitFor("Password") {
		t.Fatalf("no password prompt:\n%s", peer.Seen())
	}
	peer.WriteString("GUEST\r")
	if !peer.WaitFor("100,100") {
		t.Fatalf("did not log in:\n%s", peer.Seen())
	}
	if !peer.WaitFor("Ready") {
		t.Fatalf("no Ready prompt:\n%s", peer.Seen())
	}

	peer.WriteString("PRINT 6*7\r")
	if !peer.WaitFor("42") {
		t.Fatalf("BASIC did not answer:\n%s", peer.Seen())
	}
	peer.WriteString("BYE\r")
	if !peer.WaitFor("logged off") {
		t.Fatalf("did not log off:\n%s", peer.Seen())
	}
}
