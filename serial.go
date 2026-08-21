package rsts

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// A serial line is a job like any other. There is no negotiation to do
// and no window size to learn, so this is the Telnet line without the
// Telnet: raw bytes in, echo and line editing done here, one job per
// line, and the line offered again when the user logs off.

const serialSpeed = "9600 8N1"

func canonicalBaud(n int) (int, bool) {
	switch n {
	case 50, 75, 110, 134, 150, 200, 300, 600, 1200, 1800, 2400, 4800, 9600, 19200, 38400, 57600, 115200:
		return n, true
	default:
		return 0, false
	}
}

type serialTerm struct {
	f       *os.File
	mu      sync.Mutex
	echo    bool
	rawEcho bool
	cols    int
	rows    int
	pending []byte
	skipNL  bool
	discard bool
	fill    int
	tab     bool
	form    bool
	speed   int
}

func newSerialTerm(f *os.File) *serialTerm {
	return &serialTerm{f: f, echo: true, cols: 80, rows: 24, tab: true, form: true, speed: 9600}
}

func (t *serialTerm) Write(p []byte) (int, error) {
	return t.writeBytes(p, false)
}

func (t *serialTerm) writeEcho(p []byte) (int, error) {
	return t.writeBytes(p, true)
}

func (t *serialTerm) toggleDiscard() {
	t.mu.Lock()
	t.discard = !t.discard
	t.mu.Unlock()
}

func (t *serialTerm) writeBytes(p []byte, always bool) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	t.mu.Lock()
	fill := t.fill
	tab := t.tab
	form := t.form
	t.mu.Unlock()
	if !tab || !form {
		s := string(p)
		if !tab {
			s = expandTabs(s, 8)
		}
		if !form {
			s = strings.ReplaceAll(s, "\f", "")
		}
		p = []byte(s)
	}
	var out []byte
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' && (i == 0 || p[i-1] != '\r') {
			out = append(out, '\r')
			for n := 0; n < fill; n++ {
				out = append(out, 0)
			}
			out = append(out, '\n')
			continue
		}
		out = append(out, p[i])
		if p[i] == '\r' && fill > 0 {
			for n := 0; n < fill; n++ {
				out = append(out, 0)
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.discard && !always {
		return len(p), nil
	}
	if _, err := t.f.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// ReadByte returns the next byte from the line, taking anything
// PollInterrupt has already pulled off it first.
func (t *serialTerm) ReadByte() (byte, error) {
	t.mu.Lock()
	if len(t.pending) > 0 {
		b := t.pending[0]
		t.pending = t.pending[1:]
		t.mu.Unlock()
		return b, nil
	}
	t.mu.Unlock()
	var buf [1]byte
	for {
		n, err := t.f.Read(buf[:])
		if n > 0 {
			if t.skipNL {
				t.skipNL = false
				if buf[0] == '\n' || buf[0] == 0 {
					continue
				}
			}
			return buf[0], nil
		}
		if err != nil {
			if os.IsTimeout(err) {
				continue
			}
			return 0, err
		}
	}
}

// PollInterrupt looks for a Ctrl-C without blocking, keeping any other
// bytes for the next read. A line that cannot take a deadline simply
// reports no interrupt rather than stalling the job.
func (t *serialTerm) PollInterrupt() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept := t.pending[:0]
	hit := false
	for _, b := range t.pending {
		if b == 3 {
			hit = true
			continue
		}
		if b == 15 {
			t.discard = !t.discard
			continue
		}
		kept = append(kept, b)
	}
	t.pending = kept
	buf := make([]byte, 64)
	for {
		n, err := pollRead(t.f, buf)
		for i := 0; i < n; i++ {
			if buf[i] == 3 {
				hit = true
				continue
			}
			if buf[i] == 15 {
				t.discard = !t.discard
				continue
			}
			t.pending = append(t.pending, buf[i])
		}
		if err != nil || n == 0 {
			return hit
		}
	}
}

func (t *serialTerm) ReadLine(prompt string) (string, error) {
	return t.readEdit(prompt, t.echoOn())
}

func (t *serialTerm) GetByte(wait time.Duration) (byte, error) {
	t.mu.Lock()
	if len(t.pending) > 0 {
		b := t.pending[0]
		t.pending = t.pending[1:]
		t.mu.Unlock()
		return b, nil
	}
	t.mu.Unlock()
	if wait == 0 {
		var buf [1]byte
		n, err := pollRead(t.f, buf[:])
		if n > 0 {
			if t.skipNL {
				t.skipNL = false
				if buf[0] == '\n' || buf[0] == 0 {
					return t.GetByte(0)
				}
			}
			return buf[0], nil
		}
		_ = err
		return 0, errWaitTimeout
	}
	if wait > 0 {
		_ = t.f.SetReadDeadline(time.Now().Add(wait))
		defer t.f.SetReadDeadline(time.Time{})
	}
	var buf [1]byte
	n, err := t.f.Read(buf[:])
	if n > 0 {
		if t.skipNL {
			t.skipNL = false
			if buf[0] == '\n' || buf[0] == 0 {
				return t.GetByte(wait)
			}
		}
		return buf[0], nil
	}
	if err != nil {
		if os.IsTimeout(err) {
			return 0, errWaitTimeout
		}
		return 0, err
	}
	return 0, errWaitTimeout
}

func (t *serialTerm) ReadPassword(prompt string) (string, error) {
	s, err := t.readEdit(prompt, false)
	if _, werr := t.Write([]byte("\r\n")); werr != nil && err == nil {
		err = werr
	}
	return s, err
}

func (t *serialTerm) echoOn() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.echo
}

func (t *serialTerm) readEdit(prompt string, echo bool) (string, error) {
	if prompt != "" {
		if _, err := t.writeEcho([]byte(prompt)); err != nil {
			return "", err
		}
	}
	var buf []byte
	esc := 0
	for {
		b, err := t.ReadByte()
		if err != nil {
			if len(buf) > 0 && err == io.EOF {
				return string(buf), nil
			}
			return "", err
		}
		if esc > 0 {
			// Swallow a cursor key rather than letting its letter through.
			if b == '[' || b == 'O' {
				esc = 2
				continue
			}
			esc = 0
			continue
		}
		switch b {
		case 0:
			continue
		case 27:
			esc = 1
		case '\r':
			t.skipNL = true
			if echo {
				_, _ = t.writeEcho([]byte("\r\n"))
			}
			return string(buf), nil
		case '\n':
			if echo {
				_, _ = t.writeEcho([]byte("\r\n"))
			}
			return string(buf), nil
		case 3:
			return "", ErrInterrupt
		case 4, 26:
			if len(buf) == 0 {
				return "", io.EOF
			}
		case 15: // Ctrl-O
			t.toggleDiscard()
		case 18: // Ctrl-R
			_, _ = t.writeEcho([]byte("\r\n"))
			if prompt != "" {
				_, _ = t.writeEcho([]byte(prompt))
			}
			if len(buf) > 0 {
				_, _ = t.writeEcho(buf)
			}
		case 21, 24: // Ctrl-U, kill the line
			buf = buf[:0]
			if echo {
				_, _ = t.writeEcho([]byte("^U\r\n"))
				if prompt != "" {
					_, _ = t.writeEcho([]byte(prompt))
				}
			}
		case 8, 127:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				if echo {
					_, _ = t.writeEcho([]byte{8, ' ', 8})
				}
			}
		case 17, 19: // XON/XOFF
			continue
		default:
			if b < 32 || len(buf) >= 255 {
				continue
			}
			buf = append(buf, b)
			if echo {
				_, _ = t.writeEcho([]byte{b})
			}
		}
	}
}

func (t *serialTerm) SetEcho(on bool) {
	t.mu.Lock()
	t.echo = on
	t.mu.Unlock()
}

func (t *serialTerm) SetWidth(n int) {
	if n < 16 {
		n = 16
	}
	if n > 255 {
		n = 255
	}
	t.mu.Lock()
	t.cols = n
	t.mu.Unlock()
}

func (t *serialTerm) Size() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cols, t.rows
}

// A serial line cannot say what it is, so the editor draws ANSI. A real
// VT52 on the far end still takes the cursor keys, which it sends as
// ESC A through ESC D.
func (t *serialTerm) TermType() string { return "" }

func (t *serialTerm) SetTTY(tab, form bool, fill int) {
	t.mu.Lock()
	t.tab = tab
	t.form = form
	t.fill = fill
	t.mu.Unlock()
}

func (t *serialTerm) SetSpeed(n int) error {
	if n <= 0 {
		return nil
	}
	t.mu.Lock()
	t.speed = n
	f := t.f
	t.mu.Unlock()
	if f == nil {
		return nil
	}
	return setSerialSpeed(f, n)
}

func (t *serialTerm) StartRaw() error {
	t.mu.Lock()
	t.rawEcho = t.echo
	t.echo = false
	t.mu.Unlock()
	return nil
}

func (t *serialTerm) StopRaw() {
	t.mu.Lock()
	t.echo = t.rawEcho
	t.mu.Unlock()
}

// StartSerial answers every line named in the configuration. It reports
// the ones it opened and an error for each one it could not, so a bad
// device name does not stop the rest of the system coming up.
func (sys *System) StartSerial() ([]string, []error) {
	var up []string
	var errs []error
	for _, path := range sys.Config.Serial {
		f, err := openSerial(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		up = append(up, path)
		go sys.serveSerial(path, f)
	}
	return up, errs
}

func (sys *System) serveSerial(path string, f *os.File) {
	defer f.Close()
	for {
		select {
		case <-sys.shutdown:
			return
		default:
		}
		job, err := sys.Attach("SERIAL " + path)
		if err != nil {
			term := newSerialTerm(f)
			fmt.Fprintf(term, "\r\n%s\r\n\r\n?%s\r\n", SystemName, err.Error())
			if sys.sleepOrStop(5 * time.Second) {
				return
			}
			continue
		}
		term := newSerialTerm(f)
		started := time.Now()
		sys.runOnTerm(job, term, term, "SERIAL "+path, "", false)
		// A line with nothing on the far end returns end of file at once.
		// Pause before offering it again so a dead cable cannot spin.
		if time.Since(started) < time.Second {
			if sys.sleepOrStop(2 * time.Second) {
				return
			}
		}
	}
}

func (sys *System) sleepOrStop(d time.Duration) bool {
	select {
	case <-sys.shutdown:
		return true
	case <-time.After(d):
		return false
	}
}
