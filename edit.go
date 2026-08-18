package rsts

import (
	"fmt"
	"strings"
)

// A screen editor in the spirit of VTEDIT, the macro package RSTS sites
// layered on TECO to get full-screen editing on a VT52. This one is not
// TECO underneath, and it cannot reach outside the emulator: it edits a
// buffer and hands the text back to the caller to store through the
// normal file and program paths, so protection codes and account rules
// still apply.

const (
	keyNone = iota + 0x1000
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPgUp
	keyPgDn
	keyDelete
)

type editBuffer struct {
	lines []string
	cx    int // column within the current line, in bytes
	cy    int // index into lines
	top   int // first line on screen
	dirty bool
}

func newEditBuffer(text string) *editBuffer {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// A trailing newline is a line terminator, not an empty last line.
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &editBuffer{lines: lines}
}

func (b *editBuffer) Text() string {
	return strings.Join(b.lines, "\n") + "\n"
}

func (b *editBuffer) line() string {
	if b.cy < 0 || b.cy >= len(b.lines) {
		return ""
	}
	return b.lines[b.cy]
}

func (b *editBuffer) setLine(s string) {
	if b.cy >= 0 && b.cy < len(b.lines) {
		b.lines[b.cy] = s
		b.dirty = true
	}
}

func (b *editBuffer) clamp() {
	if b.cy < 0 {
		b.cy = 0
	}
	if b.cy >= len(b.lines) {
		b.cy = len(b.lines) - 1
	}
	if b.cx < 0 {
		b.cx = 0
	}
	if n := len(b.line()); b.cx > n {
		b.cx = n
	}
}

func (b *editBuffer) insert(ch byte) {
	s := b.line()
	b.setLine(s[:b.cx] + string(ch) + s[b.cx:])
	b.cx++
}

func (b *editBuffer) newline() {
	s := b.line()
	head, tail := s[:b.cx], s[b.cx:]
	b.lines = append(b.lines, "")
	copy(b.lines[b.cy+2:], b.lines[b.cy+1:])
	b.lines[b.cy] = head
	b.lines[b.cy+1] = tail
	b.cy++
	b.cx = 0
	b.dirty = true
}

func (b *editBuffer) backspace() {
	if b.cx > 0 {
		s := b.line()
		b.setLine(s[:b.cx-1] + s[b.cx:])
		b.cx--
		return
	}
	if b.cy == 0 {
		return
	}
	prev := b.lines[b.cy-1]
	cur := b.line()
	b.cx = len(prev)
	b.lines[b.cy-1] = prev + cur
	b.lines = append(b.lines[:b.cy], b.lines[b.cy+1:]...)
	b.cy--
	b.dirty = true
}

func (b *editBuffer) deleteForward() {
	s := b.line()
	if b.cx < len(s) {
		b.setLine(s[:b.cx] + s[b.cx+1:])
		return
	}
	if b.cy+1 >= len(b.lines) {
		return
	}
	b.lines[b.cy] = s + b.lines[b.cy+1]
	b.lines = append(b.lines[:b.cy+1], b.lines[b.cy+2:]...)
	b.dirty = true
}

func (b *editBuffer) deleteLine() {
	if len(b.lines) == 1 {
		b.setLine("")
		b.cx = 0
		return
	}
	b.lines = append(b.lines[:b.cy], b.lines[b.cy+1:]...)
	if b.cy >= len(b.lines) {
		b.cy = len(b.lines) - 1
	}
	b.cx = 0
	b.dirty = true
}

// scroll keeps the cursor on screen, given how many text rows there are.
func (b *editBuffer) scroll(rows int) {
	if rows < 1 {
		rows = 1
	}
	if b.cy < b.top {
		b.top = b.cy
	}
	if b.cy >= b.top+rows {
		b.top = b.cy - rows + 1
	}
	if b.top < 0 {
		b.top = 0
	}
}

// vt writes screen control sequences. A VT52 gets the sequences a VT52
// understands; everything else gets ANSI, which is what a modern Telnet
// client speaks.
type vt struct {
	w    rawTerm
	vt52 bool
}

func newVT(t rawTerm) *vt {
	return &vt{w: t, vt52: strings.Contains(strings.ToUpper(t.TermType()), "VT52")}
}

func (v *vt) out(s string) { _, _ = v.w.Write([]byte(s)) }

func (v *vt) moveTo(row, col int) {
	if v.vt52 {
		v.out(fmt.Sprintf("\x1bY%c%c", byte(31+row), byte(31+col)))
		return
	}
	v.out(fmt.Sprintf("\x1b[%d;%dH", row, col))
}

func (v *vt) clearEOL() {
	if v.vt52 {
		v.out("\x1bK")
		return
	}
	v.out("\x1b[K")
}

func (v *vt) clearAll() {
	if v.vt52 {
		v.out("\x1bH\x1bJ")
		return
	}
	v.out("\x1b[H\x1b[2J")
}

func (v *vt) reverse(on bool) {
	if v.vt52 {
		return
	}
	if on {
		v.out("\x1b[7m")
	} else {
		v.out("\x1b[0m")
	}
}

type editor struct {
	term    rawTerm
	vt      *vt
	buf     *editBuffer
	title   string
	status  string
	cols    int
	rows    int
	afterCR bool
	// save is called for Ctrl-W and Ctrl-X. Returning an error leaves the
	// editor open with the error on the status line, so a program with a
	// syntax error is not lost.
	save func(text string) error
}

func newEditor(t rawTerm, title, text string, save func(string) error) *editor {
	cols, rows := t.Size()
	if cols < 20 {
		cols = 80
	}
	if rows < 4 {
		rows = 24
	}
	return &editor{
		term:  t,
		vt:    newVT(t),
		buf:   newEditBuffer(text),
		title: title,
		cols:  cols,
		rows:  rows,
		save:  save,
	}
}

func (e *editor) textRows() int { return e.rows - 1 }

func (e *editor) draw() {
	e.buf.scroll(e.textRows())
	e.vt.clearAll()
	for i := 0; i < e.textRows(); i++ {
		n := e.buf.top + i
		if n >= len(e.buf.lines) {
			break
		}
		line := e.buf.lines[n]
		if len(line) > e.cols {
			line = line[:e.cols]
		}
		e.vt.moveTo(i+1, 1)
		e.vt.clearEOL()
		e.vt.out(line)
	}
	e.drawStatus()
	e.vt.moveTo(e.buf.cy-e.buf.top+1, e.buf.cx+1)
}

func (e *editor) drawStatus() {
	mark := " "
	if e.buf.dirty {
		mark = "*"
	}
	left := fmt.Sprintf("%s%s  line %d of %d  col %d",
		mark, e.title, e.buf.cy+1, len(e.buf.lines), e.buf.cx+1)
	right := e.status
	if right == "" {
		right = "^W write  ^X write+exit  ^C quit  ^K kill line"
	}
	gap := e.cols - len(left) - len(right) - 1
	if gap < 1 {
		gap = 1
		if n := e.cols - len(left) - gap - 1; n > 0 && n < len(right) {
			right = right[:n]
		} else if n <= 0 {
			right = ""
		}
	}
	text := left + strings.Repeat(" ", gap) + right
	if len(text) > e.cols {
		text = text[:e.cols]
	}
	e.vt.moveTo(e.rows, 1)
	e.vt.clearEOL()
	e.vt.reverse(true)
	e.vt.out(text)
	e.vt.reverse(false)
}

// nextByte reads one byte, dropping the LF or NUL that a Telnet client
// sends after a CR. Without this, one press of Return would open two
// lines.
func (e *editor) nextByte() (byte, error) {
	for {
		b, err := e.term.ReadByte()
		if err != nil {
			return 0, err
		}
		if e.afterCR {
			e.afterCR = false
			if b == '\n' || b == 0 {
				continue
			}
		}
		if b == '\r' {
			e.afterCR = true
		}
		if b == 0 {
			continue
		}
		return b, nil
	}
}

// readKey turns a byte, or an arrow key's escape sequence, into one code.
func (e *editor) readKey() (int, error) {
	b, err := e.nextByte()
	if err != nil {
		return 0, err
	}
	if b != 27 {
		return int(b), nil
	}
	b, err = e.term.ReadByte()
	if err != nil {
		return 0, err
	}
	switch b {
	case '[', 'O':
		// ANSI or SS3: ESC [ A, and ESC [ 3 ~ for Delete.
		c, err := e.term.ReadByte()
		if err != nil {
			return 0, err
		}
		if c >= '0' && c <= '9' {
			num := int(c - '0')
			for {
				d, err := e.term.ReadByte()
				if err != nil {
					return 0, err
				}
				if d >= '0' && d <= '9' {
					num = num*10 + int(d-'0')
					continue
				}
				break
			}
			switch num {
			case 1, 7:
				return keyHome, nil
			case 3:
				return keyDelete, nil
			case 4, 8:
				return keyEnd, nil
			case 5:
				return keyPgUp, nil
			case 6:
				return keyPgDn, nil
			}
			return keyNone, nil
		}
		return ansiFinal(c), nil
	default:
		// VT52 sends ESC A directly.
		return ansiFinal(b), nil
	}
}

func ansiFinal(c byte) int {
	switch c {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	case 'C':
		return keyRight
	case 'D':
		return keyLeft
	case 'H':
		return keyHome
	case 'F', 'K':
		return keyEnd
	}
	return keyNone
}

// Run drives the editor until the user leaves it. It reports whether the
// text was saved.
func (e *editor) Run() (bool, error) {
	if err := e.term.StartRaw(); err != nil {
		return false, err
	}
	defer func() {
		e.term.StopRaw()
		e.vt.reverse(false)
		e.vt.clearAll()
	}()

	saved := false
	confirm := false
	for {
		e.draw()
		k, err := e.readKey()
		if err != nil {
			return saved, err
		}
		if k != 3 {
			confirm = false
		}
		switch k {
		case 23: // Ctrl-W, write
			if err := e.doSave(); err == nil {
				saved = true
			}
		case 24: // Ctrl-X, write and exit
			if err := e.doSave(); err != nil {
				continue
			}
			return true, nil
		case 3: // Ctrl-C, quit
			if e.buf.dirty && !confirm {
				confirm = true
				e.status = "Ctrl-C again to lose changes"
				continue
			}
			return saved, nil
		case 12: // Ctrl-L, redraw
			e.status = ""
		case 11: // Ctrl-K, kill line
			e.buf.deleteLine()
		case 1: // Ctrl-A
			e.buf.cx = 0
		case 5: // Ctrl-E
			e.buf.cx = len(e.buf.line())
		case 4, keyDelete:
			e.buf.deleteForward()
		case 8, 127:
			e.buf.backspace()
		case '\r', '\n':
			e.buf.newline()
		case '\t':
			for i := 0; i < 4; i++ {
				e.buf.insert(' ')
			}
		case 16, keyUp:
			e.buf.cy--
		case 14, keyDown:
			e.buf.cy++
		case 2, keyLeft:
			if e.buf.cx == 0 && e.buf.cy > 0 {
				e.buf.cy--
				e.buf.cx = len(e.buf.line())
			} else {
				e.buf.cx--
			}
		case 6, keyRight:
			if e.buf.cx >= len(e.buf.line()) && e.buf.cy+1 < len(e.buf.lines) {
				e.buf.cy++
				e.buf.cx = 0
			} else {
				e.buf.cx++
			}
		case keyHome:
			e.buf.cx = 0
		case keyEnd:
			e.buf.cx = len(e.buf.line())
		case keyPgUp:
			e.buf.cy -= e.textRows()
		case keyPgDn:
			e.buf.cy += e.textRows()
		case keyNone:
			// an escape sequence we do not use
		default:
			if k >= 32 && k < 127 {
				e.buf.insert(byte(k))
			}
		}
		e.buf.clamp()
	}
}

func (e *editor) doSave() error {
	if e.save == nil {
		e.status = "nowhere to write"
		return fsErr("I/O error")
	}
	if err := e.save(e.buf.Text()); err != nil {
		e.status = strings.TrimPrefix(err.Error(), "?")
		return err
	}
	e.buf.dirty = false
	e.status = "written"
	return nil
}
