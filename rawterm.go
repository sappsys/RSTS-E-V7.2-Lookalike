package rsts

import (
	"os"

	"golang.org/x/term"
)

// rawTerm is a terminal the editor can drive directly: one byte in at a
// time with no line discipline, escape sequences out. The console and a
// Telnet line both provide this; anything else (a pipe, a test harness
// without a screen) does not, and EDIT declines rather than scribbling
// control codes at it.
type rawTerm interface {
	ReadByte() (byte, error)
	Write(p []byte) (int, error)
	Size() (cols, rows int)
	TermType() string
	StartRaw() error
	StopRaw()
}

func (t *telnetConn) ReadByte() (byte, error) { return t.readData() }

func (t *telnetConn) Size() (int, int) {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.cols, t.rows
}

func (t *telnetConn) TermType() string {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	return t.ttype
}

// The editor draws every character itself, so the local echo the Telnet
// line normally provides has to stop for the duration.
func (t *telnetConn) StartRaw() error {
	t.wmu.Lock()
	t.rawEcho = t.echo
	t.echo = false
	t.wmu.Unlock()
	return nil
}

func (t *telnetConn) StopRaw() {
	t.wmu.Lock()
	t.echo = t.rawEcho
	t.wmu.Unlock()
}

func (t *stdTerm) ReadByte() (byte, error) { return t.in.ReadByte() }

func (t *stdTerm) Size() (int, int) {
	if f, ok := t.out.(*os.File); ok {
		if c, r, err := term.GetSize(int(f.Fd())); err == nil && c > 0 && r > 0 {
			return c, r
		}
	}
	return 80, 24
}

func (t *stdTerm) TermType() string { return os.Getenv("TERM") }

func (t *stdTerm) StartRaw() error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return fsErr("Not a terminal")
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	t.raw = state
	return nil
}

func (t *stdTerm) StopRaw() {
	if t.raw != nil {
		_ = term.Restore(int(os.Stdin.Fd()), t.raw)
		t.raw = nil
	}
}
