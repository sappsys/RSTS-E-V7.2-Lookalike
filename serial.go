package rsts

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// A serial line is a job like any other. There is no negotiation to do
// and no window size to learn, so this is the Telnet line without the
// Telnet: raw bytes in, echo and line editing done here, one job per
// line, and the line offered again when the user logs off.

const serialSpeed = "9600 8N1"

type serialTerm struct {
	f       *os.File
	mu      sync.Mutex
	echo    bool
	rawEcho bool
	cols    int
	rows    int
	pending []byte
	skipNL  bool
}

func newSerialTerm(f *os.File) *serialTerm {
	return &serialTerm{f: f, echo: true, cols: 80, rows: 24}
}

func (t *serialTerm) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var out []byte
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' && (i == 0 || p[i-1] != '\r') {
			out = append(out, '\r', '\n')
			continue
		}
		out = append(out, p[i])
	}
	t.mu.Lock()
	defer t.mu.Unlock()
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
	for i, b := range t.pending {
		if b == 3 {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			return true
		}
	}
	if err := t.f.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
		return false
	}
	defer t.f.SetReadDeadline(time.Time{})
	hit := false
	buf := make([]byte, 64)
	for {
		n, err := t.f.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == 3 {
				hit = true
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
		if _, err := t.Write([]byte(prompt)); err != nil {
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
				_, _ = t.Write([]byte("\r\n"))
			}
			return string(buf), nil
		case '\n':
			if echo {
				_, _ = t.Write([]byte("\r\n"))
			}
			return string(buf), nil
		case 3:
			return "", ErrInterrupt
		case 4, 26:
			if len(buf) == 0 {
				return "", io.EOF
			}
		case 21, 24: // Ctrl-U, kill the line
			buf = buf[:0]
			if echo {
				_, _ = t.Write([]byte("^U\r\n"))
				if prompt != "" {
					_, _ = t.Write([]byte(prompt))
				}
			}
		case 8, 127:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				if echo {
					_, _ = t.Write([]byte{8, ' ', 8})
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
				_, _ = t.Write([]byte{b})
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
