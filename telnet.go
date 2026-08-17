package rsts

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	iac  = 255
	dont = 254
	do   = 253
	wont = 252
	will = 251
	sb   = 250
	se   = 240
	el   = 248
	ec   = 247
	ayt  = 246
	ao   = 245
	ip   = 244
	brk  = 243
	ga   = 249
	nop  = 241

	optEcho  = 1
	optSGA   = 3
	optTType = 24
	optNAWS  = 31

	ttypeIs   = 0
	ttypeSend = 1
)

// telnetConn is a Telnet NVT with local echo and VT52 (plus common ANSI) input.
type telnetConn struct {
	c       net.Conn
	r       *bufio.Reader
	wmu     sync.Mutex
	echo    bool
	ttype   string
	cols    int
	rows    int
	skipNL  bool
	kicked  bool
	pending []byte
}

func newTelnetConn(c net.Conn) *telnetConn {
	t := &telnetConn{
		c:    c,
		r:    bufio.NewReader(c),
		echo: true,
		cols: 80,
		rows: 24,
	}
	_ = t.send(
		iac, will, optEcho,
		iac, will, optSGA,
		iac, do, optTType,
		iac, do, optNAWS,
	)
	return t
}

func (t *telnetConn) InterruptRead() {
	t.wmu.Lock()
	t.kicked = true
	t.wmu.Unlock()
	_ = t.c.SetReadDeadline(time.Now())
}

// PollInterrupt consumes Ctrl-C waiting on the connection without blocking.
// Other bytes are queued for the next ReadLine.
func (t *telnetConn) PollInterrupt() bool {
	_ = t.c.SetReadDeadline(time.Now())
	defer t.c.SetReadDeadline(time.Time{})
	hit := false
	for {
		b, err := t.r.ReadByte()
		if err != nil {
			break
		}
		if b == 3 {
			hit = true
			continue
		}
		if b == iac {
			b2, err := t.r.ReadByte()
			if err != nil {
				t.pending = append(t.pending, iac)
				break
			}
			if b2 == ip || b2 == brk {
				hit = true
				continue
			}
			t.pending = append(t.pending, iac, b2)
			continue
		}
		t.pending = append(t.pending, b)
	}
	return hit
}

func (t *telnetConn) takeKick() bool {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if t.kicked {
		t.kicked = false
		return true
	}
	return false
}

func (t *telnetConn) send(b ...byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, err := t.c.Write(b)
	return err
}

func (t *telnetConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var out []byte
	for i := 0; i < len(p); i++ {
		b := p[i]
		if b == '\n' && (i == 0 || p[i-1] != '\r') {
			out = append(out, '\r', '\n')
			continue
		}
		if b == iac {
			out = append(out, iac, iac)
			continue
		}
		out = append(out, b)
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, err := t.c.Write(out)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *telnetConn) ReadLine(prompt string) (string, error) {
	return t.readEdit(prompt, true)
}

func (t *telnetConn) ReadPassword(prompt string) (string, error) {
	s, err := t.readEdit(prompt, false)
	if _, werr := t.Write([]byte("\r\n")); werr != nil && err == nil {
		err = werr
	}
	return s, err
}

func (t *telnetConn) readEdit(prompt string, echo bool) (string, error) {
	if prompt != "" {
		if _, err := t.Write([]byte(prompt)); err != nil {
			return "", err
		}
	}
	var buf []byte
	esc := 0
	csi := ""
	yNeed := 0
	for {
		b, err := t.readData()
		if err != nil {
			if len(buf) > 0 && err == io.EOF {
				return string(buf), nil
			}
			return "", err
		}
		if esc > 0 {
			esc, csi, yNeed = t.feedESC(b, esc, csi, yNeed)
			continue
		}
		switch b {
		case 0:
			continue
		case 27:
			esc = 1
			csi = ""
			yNeed = 0
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
		case 3: // Ctrl-C
			return "", ErrInterrupt
		case 4, 26: // Ctrl-D / Ctrl-Z
			if len(buf) == 0 {
				return "", io.EOF
			}
		case 21, 24: // Ctrl-U / Ctrl-X kill line
			if echo {
				_, _ = t.Write([]byte("^U\r\n"))
				if prompt != "" {
					_, _ = t.Write([]byte(prompt))
				}
			}
			buf = buf[:0]
		case 8, 127:
			if len(buf) == 0 {
				continue
			}
			_, size := utf8.DecodeLastRune(buf)
			if size < 1 {
				size = 1
			}
			buf = buf[:len(buf)-size]
			if echo {
				_, _ = t.Write([]byte{8, ' ', 8})
			}
		case 17, 19: // XON/XOFF
			continue
		case 15: // Ctrl-O
			continue
		default:
			if b < 32 {
				continue
			}
			if len(buf) >= 255 {
				continue
			}
			buf = append(buf, b)
			if echo {
				_, _ = t.Write([]byte{b})
			}
		}
	}
}

func (t *telnetConn) feedESC(b byte, esc int, csi string, yNeed int) (int, string, int) {
	switch esc {
	case 1:
		switch b {
		case '[':
			return 2, "", 0
		case 'O':
			return 4, "", 0
		case 'Y':
			return 3, "", 2
		case 'Z':
			_, _ = t.Write([]byte{27, '/', 'K'}) // VT52 identify: no copier
			return 0, "", 0
		case 'A', 'B', 'C', 'D', 'H', 'I', 'J', 'K', '7', '8':
			return 0, "", 0
		default:
			return 0, "", 0
		}
	case 2: // CSI
		if b >= '0' && b <= '9' || b == ';' || b == '?' {
			if len(csi) < 16 {
				csi += string(b)
			}
			return 2, csi, 0
		}
		return 0, "", 0
	case 3: // VT52 ESC Y row col
		yNeed--
		if yNeed <= 0 {
			return 0, "", 0
		}
		return 3, "", yNeed
	case 4: // SS3
		return 0, "", 0
	}
	return 0, "", 0
}

func (t *telnetConn) readData() (byte, error) {
	if len(t.pending) > 0 {
		b := t.pending[0]
		t.pending = t.pending[1:]
		return b, nil
	}
	state := 0
	var sbBuf []byte
	var cmd byte
	for {
		b, err := t.r.ReadByte()
		if err != nil {
			if t.takeKick() {
				_ = t.c.SetReadDeadline(time.Time{})
				return 0, errForced
			}
			return 0, err
		}
		switch state {
		case 0:
			if b == iac {
				state = 1
				continue
			}
			if t.skipNL {
				t.skipNL = false
				if b == '\n' || b == 0 {
					continue
				}
			}
			return b, nil
		case 1: // IAC
			switch b {
			case iac:
				return 255, nil
			case will, wont, do, dont:
				cmd = b
				state = 2
			case sb:
				sbBuf = sbBuf[:0]
				state = 3
			case se, nop, ga, el, ec:
				state = 0
			case ip, brk:
				state = 0
				return 3, nil // treat as Ctrl-C
			case ayt:
				_, _ = t.Write([]byte("[" + SystemName + "]\r\n"))
				state = 0
			case ao:
				state = 0
			default:
				state = 0
			}
		case 2: // option
			t.handleOpt(cmd, b)
			state = 0
		case 3: // SB
			if b == iac {
				state = 4
				continue
			}
			sbBuf = append(sbBuf, b)
		case 4: // SB IAC
			if b == se {
				t.handleSB(sbBuf)
				state = 0
				continue
			}
			if b == iac {
				sbBuf = append(sbBuf, iac)
				state = 3
				continue
			}
			sbBuf = append(sbBuf, b)
			state = 3
		}
	}
}

func (t *telnetConn) handleOpt(cmd, opt byte) {
	switch cmd {
	case do:
		switch opt {
		case optEcho, optSGA:
			// already WILL
		default:
			_ = t.send(iac, wont, opt)
		}
	case dont:
		// ignore
	case will:
		switch opt {
		case optTType:
			_ = t.send(iac, sb, optTType, ttypeSend, iac, se)
		case optNAWS, optSGA, optEcho:
		default:
			_ = t.send(iac, dont, opt)
		}
	case wont:
		// ignore
	}
}

func (t *telnetConn) handleSB(buf []byte) {
	if len(buf) < 1 {
		return
	}
	switch buf[0] {
	case optTType:
		if len(buf) >= 2 && buf[1] == ttypeIs {
			t.ttype = string(buf[2:])
		}
	case optNAWS:
		if len(buf) >= 5 {
			cols := int(buf[1])<<8 | int(buf[2])
			rows := int(buf[3])<<8 | int(buf[4])
			if cols >= 40 && cols <= 255 {
				t.cols = cols
			}
			if rows >= 12 && rows <= 100 {
				t.rows = rows
			}
		}
	}
}

func (sys *System) StartTelnet() (string, error) {
	if !sys.Config.Telnet {
		return "", nil
	}
	addr := net.JoinHostPort(sys.Config.TelnetBind, strconv.Itoa(sys.Config.TelnetPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	sys.mu.Lock()
	sys.listener = ln
	sys.mu.Unlock()
	go sys.acceptLoop(ln)
	return ln.Addr().String(), nil
}

func (sys *System) acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-sys.shutdown:
				return
			default:
				return
			}
		}
		go sys.handleTelnet(c)
	}
}

func (sys *System) handleTelnet(c net.Conn) {
	defer c.Close()
	remote := c.RemoteAddr().String()
	job, err := sys.Attach(remote)
	if err != nil {
		t := newTelnetConn(c)
		fmt.Fprintf(t, "\r\n%s\r\n\r\n?%s\r\n", SystemName, err.Error())
		return
	}
	term := newTelnetConn(c)
	sys.runOnTerm(job, term, term, remote, "", false)
}

func (sys *System) Wait() {
	<-sys.shutdown
}
